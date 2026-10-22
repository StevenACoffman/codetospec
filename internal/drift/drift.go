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
)

// RuleStatus is the drift result for one rule.
type RuleStatus struct {
	RuleID string
	Status Status
	Detail string
}

// Report is the outcome of a drift check.
type Report struct {
	Rules    []RuleStatus
	OK       int
	Stale    int
	Broken   int
	Unknown  int
	Affected int
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
		start, end, err := extract.ParseLines(src.Lines)
		if err != nil {
			return "", fmt.Errorf("%s: %w", src.File(), err)
		}
		data, err := roots.ReadFile(src)
		if err != nil {
			return "", err
		}
		lines := strings.Split(string(data), "\n")
		if end > len(lines) {
			return "", fmt.Errorf(
				"%s: lines %s exceed file (%d lines)",
				src.File(),
				src.Lines,
				len(lines),
			)
		}
		// A hash never fails to write.
		cited := strings.Join(lines[start-1:end], "\n")
		_, _ = fmt.Fprintf(h, "%s|%s\n%s\n", src.File(), src.Lines, cited)
	}
	return hex.EncodeToString(h.Sum(nil))[:16], nil
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
// compares it to the digest stored in the rule node; a rule whose cited
// code is unchanged is then checked against its callees' digest.
func Check(nodes []graph.Node, roots extract.Roots) Report {
	var report Report
	for _, n := range nodes {
		if n.Type != graph.TypeRule {
			continue
		}
		stored := n.Extra["digest"]
		rs := RuleStatus{RuleID: n.ID}
		switch stored {
		case "":
			rs.Status = StatusUnknown
			rs.Detail = "no stored digest (regenerate the spec to enable drift checks)"
			report.Unknown++
		default:
			current, err := Digest(roots, n.Sources)
			switch {
			case err != nil:
				rs.Status = StatusBroken
				rs.Detail = err.Error()
				report.Broken++
			case current != stored:
				rs.Status = StatusStale
				rs.Detail = "cited code changed since generation"
				report.Stale++
			case calleesChanged(&n, roots):
				rs.Status = StatusAffected
				rs.Detail = "code the cited lines call changed since generation"
				report.Affected++
			default:
				rs.Status = StatusOK
				report.OK++
			}
		}
		report.Rules = append(report.Rules, rs)
	}
	return report
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
