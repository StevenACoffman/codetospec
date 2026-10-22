package corpus_test

import (
	"slices"
	"testing"

	"github.com/StevenACoffman/codetospec/internal/corpus"
	"github.com/StevenACoffman/codetospec/internal/drift"
	"github.com/StevenACoffman/codetospec/internal/graph"
)

// TestMeasure: each node counts once, in the measure its type and extras
// name; only grounded rules with a trusted verdict are proven.
func TestMeasure(t *testing.T) {
	rule := func(grounding, crosscheck string) graph.Node {
		return graph.Node{
			Type: graph.TypeRule,
			Body: "**Requirement (EARS)**: The system shall round amounts.",
			Extra: map[string]string{
				"ears": "ubiquitous", "grounding": grounding, "crosscheck": crosscheck,
			},
		}
	}
	malformed := rule("unchecked", "unchecked")
	malformed.Body = "**Requirement (EARS)**: The system does not check access yet."
	nodes := []graph.Node{
		rule("grounded", "supported"),
		rule("grounded", "mechanical"),
		rule("grounded", "unchecked"),
		rule("ungrounded", "supported"),
		rule("unchecked", "repaired"),
		malformed,
		{
			Type:  graph.TypeDomain,
			Extra: map[string]string{graph.ExtraCoverage: graph.CoveragePartial},
		},
		{
			Type:  graph.TypeDomain,
			Extra: map[string]string{graph.ExtraCoverage: graph.CoverageComplete},
		},
	}

	got := corpus.Measure(nodes, 2, drift.Report{Stale: 1, Broken: 1, Affected: 4})

	want := corpus.Measures{
		Rules: 6, Grounded: 3, Ungrounded: 1, Proven: 2,
		PartialDomains: 1, Violations: 2, Drifted: 2, EARSIssues: 1,
	}
	if got != want {
		t.Errorf("Measure = %+v, want %+v", got, want)
	}
}

// TestCompare: only a count that got worse is listed.
func TestCompare(t *testing.T) {
	baseline := corpus.Measures{Rules: 10, Grounded: 8, Ungrounded: 2, Proven: 6}
	cases := []struct {
		name string
		got  corpus.Measures
		want []string
	}{
		{"same", baseline, nil},
		{"better", corpus.Measures{Rules: 10, Grounded: 9, Ungrounded: 1, Proven: 7}, nil},
		{
			"fewer grounded",
			corpus.Measures{Rules: 10, Grounded: 7, Ungrounded: 3, Proven: 6},
			[]string{
				"grounded: 7, baseline 8", "ungrounded: 3, baseline 2",
			},
		},
		{
			"a violation",
			corpus.Measures{Rules: 10, Grounded: 8, Ungrounded: 2, Proven: 6, Violations: 1},
			[]string{
				"verify violations: 1, baseline 0",
			},
		},
		{
			"another graph",
			corpus.Measures{Rules: 11, Grounded: 8, Ungrounded: 2, Proven: 6},
			[]string{
				"rules: 11, baseline 10 (another graph: re-record the baseline)",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := corpus.Compare(baseline, tc.got); !slices.Equal(got, tc.want) {
				t.Errorf("Compare = %q, want %q", got, tc.want)
			}
		})
	}
}
