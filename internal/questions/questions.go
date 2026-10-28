// Package questions turns the run's findings into open questions with
// stable ids, which a person resolves in <out>/resolutions.yaml: a finding
// checked by hand stops being listed, run after run, until the code it is
// about changes. Pure but for Load; codetospec never writes the file.
//
// The findings are those about one rule or one domain: a rule stating a
// value absent from its citation (KindUngrounded), one whose requirement
// lacks the form of its EARS kind (KindEARSIssue), one the crosscheck
// refuted (KindRefuted), an inherited rule whose shared code changed since
// its verdict (KindStaleVerdict), and code of a domain that produced no
// rule (KindCoverageGap). Pair findings (near-duplicates) and the
// domain-model gaps are not questions yet.
package questions

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strings"

	"github.com/StevenACoffman/codetospec/internal/crosscheck"
	"github.com/StevenACoffman/codetospec/internal/graph"
)

// Question kinds.
const (
	KindUngrounded   = "ungrounded"
	KindEARSIssue    = "ears_issue"
	KindRefuted      = "refuted"
	KindStaleVerdict = "stale_verdict"
	KindCoverageGap  = "coverage_gap"
)

// Question is one finding a person may resolve.
type Question struct {
	ID      string `json:"id"`      // "q-" + 12 hex digits; see id
	Kind    string `json:"kind"`    // one of the Kind constants
	Subject string `json:"subject"` // the rule id, or the domain
	Detail  string `json:"detail"`  // the finding as the README shows it
	// Resolution is set when resolutions.yaml resolves the question.
	Resolution *Resolution `json:"resolution,omitempty"`
	finding    string      // what Ledger.ResolvedRule and ResolvedGap look up
}

// List lists the questions the nodes and coverage gaps raise.
//
// Requires: nodes as the run renders them (verdicts, grounding and digests
// set); gaps from graph.AnnotateCoverage.
// Ensures:  sorted by id, unresolved; the same findings on the same cited
// code give the same ids.
func List(nodes []graph.Node, gaps []graph.CoverageGap) []Question {
	var qs []Question
	add := func(kind, subject, detail, key, digest string) {
		qs = append(qs, Question{
			ID:   id(kind, subject, key, digest),
			Kind: kind, Subject: subject, Detail: detail,
			finding: kind + "\x00" + subject,
		})
	}
	for i := range nodes {
		n := &nodes[i]
		if n.Type != graph.TypeRule {
			continue
		}
		digest := n.Extra[graph.ExtraDigest]
		if n.Extra["grounding"] == "ungrounded" {
			missing := n.Extra["grounding_missing"]
			add(KindUngrounded, n.ID, missing, missingValues(missing), digest)
		}
		if issue := n.Extra[graph.ExtraEARSIssue]; issue != "" {
			add(KindEARSIssue, n.ID, issue, issue, digest)
		}
		if n.Extra["crosscheck"] == crosscheck.VerdictUnsupported {
			add(KindRefuted, n.ID, n.Extra["crosscheck"], "", digest)
		}
		if stale := n.Extra[graph.ExtraStaleVerdict]; stale != "" {
			add(KindStaleVerdict, n.ID, stale, stale, digest)
		}
	}
	for _, g := range gaps {
		qs = append(qs, Question{
			ID:   id(KindCoverageGap, g.Domain, g.String(), ""),
			Kind: KindCoverageGap, Subject: g.Domain, Detail: g.String(),
			finding: gapFinding(&g),
		})
	}
	slices.SortFunc(qs, func(a, b Question) int { return cmp.Compare(a.ID, b.ID) })
	return qs
}

// gapFinding identifies a coverage gap among its domain's.
func gapFinding(g *graph.CoverageGap) string {
	return KindCoverageGap + "\x00" + g.Domain + "\x00" + g.String()
}

// id identifies a finding by what it is about, not by how it is worded:
// its kind, subject and stable key, and the digest of the cited code, so
// that a resolution lapses when that code changes.
func id(kind, subject, key, digest string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{kind, subject, key, digest}, "\x00")))
	return "q-" + hex.EncodeToString(sum[:])[:idDigits]
}

// idDigits is how many hex digits of the hash a question id keeps: 48
// bits, enough for the few thousand findings of a spec.
const idDigits = 12

// missingValues is the values of a grounding_missing summary without where
// they occur elsewhere (`30 @app/X.php:42; "PRO"` → `30; "PRO"`): other
// edits of the file move those locations, not the finding.
func missingValues(summary string) string {
	parts := strings.Split(summary, "; ")
	for i, part := range parts {
		parts[i], _, _ = strings.Cut(part, " @")
	}
	return strings.Join(parts, "; ")
}
