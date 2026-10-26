// Package drift detects when the code a rule cites has changed since the
// spec was generated. Each rule carries a digest of its cited source; the
// drift command recomputes it against the current tree and flags mismatches.
// Fully deterministic, no LLM.
package drift

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"

	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/graph"
)

// Status classifies a rule against the current source tree.
type Status string

const (
	StatusOK      Status = "ok"      // cited code is unchanged
	StatusStale   Status = "stale"   // cited code changed
	StatusBroken  Status = "broken"  // a citation no longer resolves
	StatusUnknown Status = "unknown" // rule has no stored digest
	// StatusAffected: the cited code is unchanged but code it calls changed
	// (or no longer resolves), so the rule may no longer hold. Reported,
	// never a failure: a callee edit is often unrelated to the rule.
	StatusAffected Status = "affected"
	// StatusMoved: the cited code is unchanged but sits at other lines (a
	// line was added or removed above it), so the citation needs updating
	// while the rule still holds. Reported, never a failure.
	StatusMoved Status = "moved"
)

// RuleStatus is the drift result for one rule.
type RuleStatus struct {
	RuleID string `json:"id"`
	Status Status `json:"status"`
	Detail string `json:"detail,omitempty"`
}

// Report is the outcome of a drift check.
type Report struct {
	Rules    []RuleStatus
	OK       int
	Stale    int
	Broken   int
	Unknown  int
	Affected int
	Moved    int
}

// Drifted reports whether any rule is stale or broken.
func (r Report) Drifted() bool { return r.Stale > 0 || r.Broken > 0 }

// Digest hashes the exact cited source lines of a set of references, read
// through roots, so an edit inside any cited span changes the digest.
// Sources are sorted for a stable result; a reference outside --src hashes
// its root too, one inside hashes as it always has. Returns an error only
// for an unreadable file, an unknown root or a bad range.
func Digest(roots extract.Roots, sources []extract.Ref) (string, error) {
	ordered := append([]extract.Ref(nil), sources...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].File() != ordered[j].File() {
			return ordered[i].File() < ordered[j].File()
		}
		return ordered[i].Lines < ordered[j].Lines
	})
	h := sha256.New()
	for _, src := range ordered {
		cited, err := citedText(roots, src)
		if err != nil {
			return "", err
		}
		// A hash never fails to write.
		_, _ = fmt.Fprintf(h, "%s|%s\n%s\n", src.File(), src.Lines, cited)
	}
	return hex.EncodeToString(h.Sum(nil))[:16], nil
}

// citedText returns the text of the lines src cites.
func citedText(roots extract.Roots, src extract.Ref) (string, error) {
	start, end, lines, err := readSpan(roots, src)
	if err != nil {
		return "", err
	}
	if end > len(lines) {
		return "", fmt.Errorf(
			"%s: lines %s exceed file (%d lines)",
			src.File(),
			src.Lines,
			len(lines),
		)
	}
	return strings.Join(lines[start-1:end], "\n"), nil
}

// readSpan parses src's range and reads its file as lines.
func readSpan(roots extract.Roots, src extract.Ref) (int, int, []string, error) {
	start, end, err := extract.ParseLines(src.Lines)
	if err != nil {
		return 0, 0, nil, fmt.Errorf("%s: %w", src.File(), err)
	}
	data, err := roots.ReadFile(src)
	if err != nil {
		return 0, 0, nil, err
	}
	return start, end, strings.Split(string(data), "\n"), nil
}

// textHash is the hash source_hashes stores for one span's text: no path,
// no line numbers, so the same code hashes the same wherever it sits.
func textHash(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])[:16]
}

// sourceHashes is the source_hashes value of sources: each span's text
// hash, comma-separated in sources order.
func sourceHashes(roots extract.Roots, sources []extract.Ref) (string, error) {
	hashes := make([]string, 0, len(sources))
	for _, src := range sources {
		cited, err := citedText(roots, src)
		if err != nil {
			return "", err
		}
		hashes = append(hashes, textHash(cited))
	}
	return strings.Join(hashes, ","), nil
}

// Stamp returns nodes with the drift fingerprint of each rule set from the
// current source: the digest and source hashes of its cited code, and its
// callees (one hop through calls) with their digest when it has any.
//
// Requires: calls holds the run's facts (only call facts are read).
// Ensures:  the input nodes and their Extra maps are not modified; a rule
// whose citation does not resolve is left unstamped (verify reports it).
func Stamp(roots extract.Roots, nodes []graph.Node, calls []extract.Fact) []graph.Node {
	out := slices.Clone(nodes)
	for i := range out {
		n := &out[i]
		if n.Type != graph.TypeRule {
			continue
		}
		digest, err := Digest(roots, n.Sources)
		if err != nil {
			continue
		}
		hashes, err := sourceHashes(roots, n.Sources)
		if err != nil {
			continue
		}
		n.Extra = maps.Clone(n.Extra)
		if n.Extra == nil {
			n.Extra = map[string]string{}
		}
		n.Extra[graph.ExtraDigest] = digest
		n.Extra[graph.ExtraSourceHashes] = hashes
		callees := Callees(n.Sources, calls)
		if calleesDigest, calleeErr := Digest(
			roots,
			callees,
		); calleeErr == nil &&
			len(callees) > 0 {
			n.Callees = callees
			n.Extra[graph.ExtraCalleesDigest] = calleesDigest
		}
	}
	return out
}

// locate finds where each cited span of n now sits, by its stored text
// hash: the same-length window of its file with that hash nearest its old
// start (the earlier one on a tie). It returns the moves ("calc.go 3-5 →
// 7-9"), or nil when a span's text is found nowhere, when n stores no
// source hashes, or when none moved.
func locate(roots extract.Roots, n *graph.Node) []string {
	hashes := strings.Split(n.Extra[graph.ExtraSourceHashes], ",")
	if n.Extra[graph.ExtraSourceHashes] == "" || len(hashes) != len(n.Sources) {
		return nil
	}
	var moves []string
	for i, src := range n.Sources {
		start, end, lines, err := readSpan(roots, src)
		if err != nil {
			return nil
		}
		at := nearestWindow(lines, end-start+1, start, hashes[i])
		if at < 0 {
			return nil
		}
		if at != start {
			moves = append(moves, fmt.Sprintf("%s %s → %d-%d",
				src.File(), src.Lines, at, at+end-start))
		}
	}
	return moves
}

// nearestWindow returns the 1-based start of the size-line window of lines
// whose text hashes to hash, nearest to from (the earlier on a tie), or -1.
func nearestWindow(lines []string, size, from int, hash string) int {
	best := -1
	for at := 1; at+size-1 <= len(lines); at++ {
		if textHash(strings.Join(lines[at-1:at-1+size], "\n")) != hash {
			continue
		}
		if best < 0 || abs(at-from) < abs(best-from) {
			best = at
		}
	}
	return best
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// Callees returns the code the cited sources call, one hop: the callee of
// every call fact whose caller overlaps a cited range, minus code a source
// already cites. One hop, because a rule's closure would reach most of the
// module and flag every rule on any edit.
//
// Requires: calls are call facts (others are ignored).
// Ensures:  sorted by file then lines, no duplicates; nil when none. Only
// sources and callers under --src (no Root) are matched: call facts are
// emitted for the analyzed module alone.
func Callees(sources []extract.Ref, calls []extract.Fact) []extract.Ref {
	seen := map[extract.Ref]bool{}
	var callees []extract.Ref
	for i := range calls {
		f := &calls[i]
		if f.Kind != extract.KindCall || f.Source.Root != "" || !overlapsAny(sources, f.Source) {
			continue
		}
		callee := extract.Ref{
			Path:  f.Attrs[extract.AttrCalleePath],
			Lines: f.Attrs[extract.AttrCalleeLines],
		}
		if seen[callee] || overlapsAny(sources, callee) {
			continue
		}
		seen[callee] = true
		callees = append(callees, callee)
	}
	slices.SortFunc(callees, func(a, b extract.Ref) int {
		return cmp.Or(cmp.Compare(a.File(), b.File()), cmp.Compare(a.Lines, b.Lines))
	})
	return callees
}

// overlapsAny reports whether ref shares a line with one of refs in the
// same file; a malformed range overlaps nothing.
func overlapsAny(refs []extract.Ref, ref extract.Ref) bool {
	start, end, err := extract.ParseLines(ref.Lines)
	if err != nil {
		return false
	}
	for _, r := range refs {
		if r.File() != ref.File() {
			continue
		}
		rStart, rEnd, rErr := extract.ParseLines(r.Lines)
		if rErr == nil && rStart <= end && start <= rEnd {
			return true
		}
	}
	return false
}

// Check recomputes each rule's digest against the current source and
// compares it to the digest stored in the rule node. A rule whose digest
// differs is moved when each cited span's text is found in its file (see
// locate), stale otherwise; one whose cited code is unchanged is then
// checked against its callees' digest.
func Check(nodes []graph.Node, roots extract.Roots) Report {
	var report Report
	for i := range nodes {
		n := &nodes[i]
		if n.Type != graph.TypeRule {
			continue
		}
		rs := check(n, roots)
		switch rs.Status {
		case StatusOK:
			report.OK++
		case StatusStale:
			report.Stale++
		case StatusBroken:
			report.Broken++
		case StatusUnknown:
			report.Unknown++
		case StatusAffected:
			report.Affected++
		case StatusMoved:
			report.Moved++
		}
		report.Rules = append(report.Rules, rs)
	}
	return report
}

// check is the drift status of one rule.
func check(n *graph.Node, roots extract.Roots) RuleStatus {
	stored := n.Extra[graph.ExtraDigest]
	if stored == "" {
		return RuleStatus{
			n.ID, StatusUnknown,
			"no stored digest (regenerate the spec to enable drift checks)",
		}
	}
	current, err := Digest(roots, n.Sources)
	switch {
	case err == nil && current == stored && calleesChanged(n, roots):
		return RuleStatus{
			n.ID, StatusAffected,
			"code the cited lines call changed since generation",
		}
	case err == nil && current == stored:
		return RuleStatus{n.ID, StatusOK, ""}
	}
	if moves := locate(roots, n); len(moves) > 0 {
		return RuleStatus{n.ID, StatusMoved, "cited code moved: " + strings.Join(moves, "; ")}
	}
	if err != nil {
		return RuleStatus{n.ID, StatusBroken, err.Error()}
	}
	return RuleStatus{n.ID, StatusStale, "cited code changed since generation"}
}

// calleesChanged reports whether the rule stores a callees digest that no
// longer matches its callees; a callee that no longer resolves counts as
// changed.
func calleesChanged(n *graph.Node, roots extract.Roots) bool {
	stored := n.Extra[graph.ExtraCalleesDigest]
	if stored == "" {
		return false
	}
	current, err := Digest(roots, n.Callees)
	return err != nil || current != stored
}
