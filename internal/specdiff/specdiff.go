// Package specdiff compares two rendered spec graphs of one code base, rule
// by rule: which rules appeared, disappeared or were renamed, and why a
// kept rule changed — its wording, its code or its verdict. A reviewer
// reads it after a re-run (did a prompt change reword everything?) and
// after a code change (which rules does it touch?). Deterministic, no LLM.
package specdiff

import (
	"cmp"
	"maps"
	"slices"

	"github.com/StevenACoffman/codetospec/internal/graph"
)

// Kinds of Change.
const (
	Added   = "added"
	Removed = "removed"
	Renamed = "renamed" // a rule restated under another id (graph.MatchRules)
	Changed = "changed" // same id, see Reasons
)

// Reasons a kept or renamed rule differs.
const (
	Reworded = "reworded" // its requirement reads otherwise
	Code     = "code"     // its cited code changed
	Moved    = "moved"    // its cited code is the same text at other lines
	Verdict  = "verdict"  // its crosscheck verdict changed
)

// Change is one rule that differs between the graphs.
type Change struct {
	Kind    string   `json:"kind"`
	ID      string   `json:"id"`               // the new id; the old one when removed
	OldID   string   `json:"old_id,omitempty"` // when renamed
	Reasons []string `json:"reasons,omitempty"`
}

// Report is the outcome of a comparison.
type Report struct {
	Changes   []Change `json:"changes"`
	Unchanged int      `json:"unchanged"`
}

// Count returns how many changes are of kind.
func (r *Report) Count(kind string) int {
	n := 0
	for _, c := range r.Changes {
		if c.Kind == kind {
			n++
		}
	}
	return n
}

// Diff compares the rules of old and current: those sharing an id are
// compared; of the others, a current rule restating an old one (see
// graph.MatchRules) is a rename; the rest are added or removed.
//
// Requires: both are graphs as render.LoadDir reads them.
// Ensures:  changes sorted by id then kind; a renamed rule lists its
// reasons too; Unchanged counts rules with the same id and no reason.
func Diff(old, current []graph.Node) Report {
	oldRules, newRules := rulesByID(old), rulesByID(current)
	var report Report
	var gone, fresh []graph.Node
	for _, id := range slices.Sorted(maps.Keys(newRules)) {
		n := newRules[id]
		o, kept := oldRules[id]
		if !kept {
			fresh = append(fresh, n)
			continue
		}
		if reasons := compare(&o, &n); len(reasons) > 0 {
			report.Changes = append(report.Changes, Change{Kind: Changed, ID: id, Reasons: reasons})
		} else {
			report.Unchanged++
		}
	}
	for _, id := range slices.Sorted(maps.Keys(oldRules)) {
		if _, kept := newRules[id]; !kept {
			gone = append(gone, oldRules[id])
		}
	}

	renames := graph.MatchRules(gone, fresh)
	renamedFrom := map[string]bool{}
	for _, n := range fresh {
		oldID, renamed := renames[n.ID]
		if !renamed {
			report.Changes = append(report.Changes, Change{Kind: Added, ID: n.ID})
			continue
		}
		renamedFrom[oldID] = true
		o := oldRules[oldID]
		report.Changes = append(report.Changes, Change{
			Kind: Renamed, ID: n.ID, OldID: oldID, Reasons: compare(&o, &n),
		})
	}
	for _, o := range gone {
		if !renamedFrom[o.ID] {
			report.Changes = append(report.Changes, Change{Kind: Removed, ID: o.ID})
		}
	}
	slices.SortFunc(report.Changes, func(a, b Change) int {
		return cmp.Or(cmp.Compare(a.ID, b.ID), cmp.Compare(a.Kind, b.Kind))
	})
	return report
}

// compare lists why rule n differs from rule o, in Reasons order. The code
// is compared by the hash of each cited span's text when both graphs store
// one, so code that only moved is told from code that changed; otherwise
// by digest, which a move changes too.
func compare(o, n *graph.Node) []string {
	var reasons []string
	if graph.RuleRequirement(o.Body) != graph.RuleRequirement(n.Body) {
		reasons = append(reasons, Reworded)
	}
	oldHashes, newHashes := o.Extra[graph.ExtraSourceHashes], n.Extra[graph.ExtraSourceHashes]
	switch {
	case oldHashes != "" && newHashes != "" && oldHashes != newHashes:
		reasons = append(reasons, Code)
	case oldHashes != "" && newHashes != "":
		if !slices.Equal(o.Sources, n.Sources) {
			reasons = append(reasons, Moved)
		}
	case o.Extra[graph.ExtraDigest] != n.Extra[graph.ExtraDigest]:
		reasons = append(reasons, Code)
	}
	if o.Extra["crosscheck"] != n.Extra["crosscheck"] {
		reasons = append(reasons, Verdict)
	}
	return reasons
}

// rulesByID indexes the rule nodes of nodes.
func rulesByID(nodes []graph.Node) map[string]graph.Node {
	rules := map[string]graph.Node{}
	for _, n := range nodes {
		if n.Type == graph.TypeRule {
			rules[n.ID] = n
		}
	}
	return rules
}
