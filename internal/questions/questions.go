// Package questions turns the run's findings into open questions with
// stable ids, which a person resolves in <out>/resolutions.yaml: a finding
// checked by hand stops being listed, run after run, until the code it is
// about changes. Pure but for Load; codetospec never writes the file.
//
// The findings are those about one rule or one domain: a rule stating a
// value absent from its citation (KindUngrounded), one whose requirement
// lacks the form of its EARS kind (KindEARSIssue), one the crosscheck
// refuted (KindRefuted), an inherited rule whose shared code changed since
// its verdict (KindStaleVerdict), a non-functional rule stating a quantity
// whose code states no threshold (KindNFRTarget: the target is the
// person's to set), a rule
// whose nature its code's layer contradicts, with its own evidence
// backing the layer (KindNatureMismatch), one whose
// condition and EARS text state different checks (KindConditionMismatch),
// one whose condition contradicts an example (KindExampleContradicted), one
// whose observable the review refuted (KindObservableRefuted), one
// whose target a person set for code that has changed since
// (KindStaleTarget), and code of a domain that produced no rule
// (KindCoverageGap). Pair findings (near-duplicates) and the domain-model
// gaps are not questions yet. A target (KindNFRTarget's answer) is set in
// targets.yaml (package nfrtarget), not resolved here: it closes the
// question by setting the threshold.
package questions

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"

	"github.com/StevenACoffman/codetospec/internal/alignment"
	"github.com/StevenACoffman/codetospec/internal/concept"
	"github.com/StevenACoffman/codetospec/internal/crosscheck"
	"github.com/StevenACoffman/codetospec/internal/graph"
	"github.com/StevenACoffman/codetospec/internal/position"
	"github.com/StevenACoffman/codetospec/internal/quality"
)

// Question kinds.
const (
	KindUngrounded     = "ungrounded"
	KindEARSIssue      = "ears_issue"
	KindRefuted        = "refuted"
	KindStaleVerdict   = "stale_verdict"
	KindNFRTarget      = "nfr_target"
	KindNatureMismatch = "nature_mismatch"
	// KindConditionMismatch: a rule's condition and its EARS text state
	// different checks (graph.ExtraConditionDisagreement).
	KindConditionMismatch = "condition_mismatch"
	// KindExampleContradicted: an example of the acceptance pass that the
	// rule's condition contradicts (graph.ExtraExamplesContradicted): the
	// requirement or the condition is wrong.
	KindExampleContradicted = "example_contradicted"
	// KindObservableRefuted: the adversarial review found that the cited
	// code does not produce a rule's observable
	// (graph.ExtraObservableRefuted, the review's reason): the observable
	// or the rule is wrong.
	KindObservableRefuted = "observable_refuted"
	// KindStaleTarget: a non-functional rule's target was set for other
	// code than it cites now (graph.ExtraTargetStale): check it still
	// holds, then update its digest in targets.yaml.
	KindStaleTarget = "stale_target"
	// KindConceptConflict: a business rule whose evidence names concepts of
	// the confirmed model that share no owner (concept.ExtraConceptConflict,
	// each option with its evidence): the rule belongs to one of them, or
	// the model lacks an owner.
	KindConceptConflict = "concept_conflict"
	// KindConceptDivergence: the code relates two concepts of the
	// confirmed models that the models don't relate (alignment.Diverges,
	// with the foreign keys and calls that show it): a relationship the
	// model lacks, or code on the wrong side of a boundary. Its id has no
	// digest: it is about the model, which only a person changes, so new
	// evidence for the same pair keeps the question.
	KindConceptDivergence = "concept_divergence"
	KindCoverageGap       = "coverage_gap"
)

// shownEvidence is how many evidence lines a divergence's detail quotes.
const shownEvidence = 3

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
// set); gaps from graph.AnnotateCoverage; pairs from alignment.Measure.
// Ensures:  sorted by id, unresolved; the same findings on the same cited
// code give the same ids.
func List(nodes []graph.Node, gaps []graph.CoverageGap, pairs []alignment.Pair) []Question {
	var qs []Question
	add := func(kind, subject, detail, key, digest string) {
		qs = append(qs, Question{
			ID:   ID(kind, subject, key, digest),
			Kind: kind, Subject: subject, Detail: detail,
			finding: kind + "\x00" + subject,
		})
	}
	for i := range nodes {
		n := &nodes[i]
		if n.Type != graph.TypeRule {
			continue
		}
		for _, f := range ruleFindings(n) {
			add(f.kind, n.ID, f.detail, f.key, n.Extra[graph.ExtraDigest])
		}
	}
	for _, g := range gaps {
		qs = append(qs, Question{
			ID:   ID(KindCoverageGap, g.Domain, g.String(), ""),
			Kind: KindCoverageGap, Subject: g.Domain, Detail: g.String(),
			finding: gapFinding(&g),
		})
	}
	for i := range pairs {
		if p := &pairs[i]; p.Outcome == alignment.Diverges {
			subject := PairSubject(p)
			add(KindConceptDivergence, subject, divergenceDetail(p), subject, "")
		}
	}
	slices.SortFunc(qs, func(a, b Question) int { return cmp.Compare(a.ID, b.ID) })
	return qs
}

// PairSubject is the subject of a pair's question: "A ~ B".
func PairSubject(p *alignment.Pair) string { return p.A + " ~ " + p.B }

// divergenceDetail quotes a divergence's first evidence lines and counts
// the rest.
func divergenceDetail(p *alignment.Pair) string {
	shown := p.Evidence[:min(len(p.Evidence), shownEvidence)]
	detail := strings.Join(shown, "; ")
	if more := len(p.Evidence) - len(shown); more > 0 {
		detail += fmt.Sprintf("; +%d", more)
	}
	return detail
}

// finding is one question a rule raises: its kind, detail and stable key.
type finding struct{ kind, detail, key string }

// ruleFindings lists the questions rule n raises.
func ruleFindings(n *graph.Node) []finding {
	var found []finding
	if n.Extra["grounding"] == "ungrounded" {
		missing := n.Extra["grounding_missing"]
		found = append(found, finding{KindUngrounded, missing, missingValues(missing)})
	}
	// The findings a frontmatter key states as is: its value is both the
	// detail and the stable key.
	for _, k := range []struct{ kind, extra string }{
		{KindEARSIssue, graph.ExtraEARSIssue},
		{KindStaleVerdict, graph.ExtraStaleVerdict},
		{KindConditionMismatch, graph.ExtraConditionDisagreement},
		{KindExampleContradicted, graph.ExtraExamplesContradicted},
		{KindStaleTarget, graph.ExtraTargetStale},
	} {
		if v := n.Extra[k.extra]; v != "" {
			found = append(found, finding{k.kind, v, v})
		}
	}
	if n.Extra["crosscheck"] == crosscheck.VerdictUnsupported {
		found = append(found, finding{KindRefuted, n.Extra["crosscheck"], ""})
	}
	// The reason is the detail, not the key: a rerun of the review words
	// it anew.
	if reason := n.Extra[graph.ExtraObservableRefuted]; reason != "" {
		found = append(found, finding{KindObservableRefuted, reason, ""})
	}
	// The evidence is the detail, not the key: an edge the reduce adds
	// rewords it, the disagreement stays.
	if position.Asks(n) {
		positional := n.Extra[graph.ExtraNatureByPosition]
		detail := positional + " by position, " + n.Extra["nature"] + " by text; " +
			n.Extra[graph.ExtraNatureEvidence]
		found = append(found, finding{KindNatureMismatch, detail, positional})
	}
	// The options are the key, not their evidence: an edge the reduce adds
	// rewords the evidence, the choice stays.
	if options := n.Extra[concept.ExtraConceptConflict]; options != "" {
		found = append(found, finding{
			KindConceptConflict, options,
			strings.Join(concept.ConflictConcepts(options), ", "),
		})
	}
	if quality.IsNFR(n) && n.Extra[graph.ExtraThreshold] == "" &&
		n.Extra[graph.ExtraQuantity] == graph.QuantityStated {
		attribute := n.Extra[graph.ExtraQualityAttribute]
		found = append(found, finding{KindNFRTarget, attribute, attribute})
	}
	return found
}

// gapFinding identifies a coverage gap among its domain's.
func gapFinding(g *graph.CoverageGap) string {
	return KindCoverageGap + "\x00" + g.Domain + "\x00" + g.String()
}

// ID identifies a finding by what it is about, not by how it is worded:
// its kind, subject and stable key, and the digest of the cited code, so
// that a resolution lapses when that code changes. Every question id is
// one, the policy inventory's included.
func ID(kind, subject, key, digest string) string {
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
