package specdiff_test

import (
	"reflect"
	"testing"

	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/graph"
	"github.com/StevenACoffman/codetospec/internal/specdiff"
)

// rule is an LLM rule of domain billing citing calc.php, stamped with a
// source hash and a digest.
func rule(id, requirement, lines, hash, verdict string) graph.Node {
	return graph.Node{
		ID:      id,
		Type:    graph.TypeRule,
		Body:    "**Requirement (EARS)**: " + requirement,
		Sources: []extract.Ref{{Path: "calc.php", Lines: lines}},
		Edges:   []graph.Edge{{Type: graph.EdgeBelongsTo, To: "domain.billing"}},
		Extra: map[string]string{
			graph.ExtraSourceHashes: hash,
			graph.ExtraDigest:       hash + lines,
			"crosscheck":            verdict,
		},
	}
}

// Requirements and verdicts the cases share.
const (
	round     = "The system shall round every invoice amount to the cent."
	reworded  = "The system shall round each invoice amount to the cent."
	refund    = "The system shall refund the unused days of a cancelled plan."
	supported = "supported"
	roundID   = "rule.billing.round"
)

// TestDiff: each way a rule can differ is named, and an identical graph
// has no change.
func TestDiff(t *testing.T) {
	base := rule(roundID, round, "10-20", "h1", supported)
	cases := map[string]struct {
		current graph.Node
		want    []specdiff.Change
	}{
		"unchanged": {current: base},
		"reworded": {
			current: rule(roundID, reworded, "10-20", "h1", supported),
			want: []specdiff.Change{
				{Kind: specdiff.Changed, ID: roundID, Reasons: []string{specdiff.Reworded}},
			},
		},
		"code changed, verdict too": {
			current: rule(roundID, round, "10-20", "h2", "partial"),
			want: []specdiff.Change{{
				Kind: specdiff.Changed, ID: roundID,
				Reasons: []string{specdiff.Code, specdiff.Verdict},
			}},
		},
		"moved": {
			current: rule(roundID, round, "12-22", "h1", supported),
			want: []specdiff.Change{
				{Kind: specdiff.Changed, ID: roundID, Reasons: []string{specdiff.Moved}},
			},
		},
		"renamed and reworded": {
			current: rule("rule.billing.cents", reworded, "10-20", "h1", supported),
			want: []specdiff.Change{{
				Kind: specdiff.Renamed, ID: "rule.billing.cents", OldID: roundID,
				Reasons: []string{specdiff.Reworded},
			}},
		},
		"replaced": {
			current: rule("rule.billing.refund", refund, "40-50", "h3", supported),
			want: []specdiff.Change{
				{Kind: specdiff.Added, ID: "rule.billing.refund"},
				{Kind: specdiff.Removed, ID: roundID},
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := specdiff.Diff([]graph.Node{base}, []graph.Node{tc.current})
			if !reflect.DeepEqual(got.Changes, tc.want) {
				t.Errorf("changes = %+v\nwant      %+v", got.Changes, tc.want)
			}
			if wantUnchanged := len(tc.want) == 0; (got.Unchanged == 1) != wantUnchanged {
				t.Errorf("unchanged = %d", got.Unchanged)
			}
		})
	}
}

// TestDiffWithoutSourceHashes: a graph stamped before source hashes is
// compared by digest, so moved code reads as changed code.
func TestDiffWithoutSourceHashes(t *testing.T) {
	old := rule(roundID, round, "10-20", "h1", supported)
	moved := rule(roundID, round, "12-22", "h1", supported)
	delete(old.Extra, graph.ExtraSourceHashes)

	got := specdiff.Diff([]graph.Node{old}, []graph.Node{moved})

	want := []specdiff.Change{
		{Kind: specdiff.Changed, ID: roundID, Reasons: []string{specdiff.Code}},
	}
	if !reflect.DeepEqual(got.Changes, want) {
		t.Errorf("changes = %+v, want %+v", got.Changes, want)
	}
}
