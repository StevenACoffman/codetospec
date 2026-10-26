package graph_test

import (
	"testing"

	"github.com/StevenACoffman/codetospec/internal/graph"
)

// TestAnnotateCoverage: a domain is partial exactly when a gap names it,
// its gaps are listed in a stable order, and nothing else changes.
func TestAnnotateCoverage(t *testing.T) {
	billingGaps := []graph.CoverageGap{
		{Domain: billingDomain, Reason: graph.GapMapFailed, Path: "b.go", Lines: "1-40"},
		{Domain: billingDomain, Reason: graph.GapGenerated, Path: "a_gen.go"},
		{Domain: billingDomain, Reason: graph.GapMapFailed, Path: "a.go", Lines: "9-12"},
	}
	cases := []struct {
		name         string
		gaps         []graph.CoverageGap
		wantCoverage string
		wantGaps     string
	}{
		{"no gaps", nil, graph.CoverageComplete, ""},
		{"another domain's gap", []graph.CoverageGap{
			{Domain: "users", Reason: graph.GapReduceFailed},
		}, graph.CoverageComplete, ""},
		{
			"own gaps, sorted",
			billingGaps,
			graph.CoveragePartial,
			"generated a_gen.go; map-failed a.go:9-12; map-failed b.go:1-40",
		},
		{"whole domain", []graph.CoverageGap{
			{Domain: billingDomain, Reason: graph.GapReduceFailed},
		}, graph.CoveragePartial, "reduce-failed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			nodes := []graph.Node{
				{ID: "domain.billing", Type: graph.TypeDomain, Extra: map[string]string{"k": "v"}},
				{ID: "rule.billing.x", Type: graph.TypeRule},
			}

			got := graph.AnnotateCoverage(nodes, tc.gaps)

			domain := got[0]
			if domain.Extra[graph.ExtraCoverage] != tc.wantCoverage {
				t.Errorf(
					"coverage = %q, want %q",
					domain.Extra[graph.ExtraCoverage],
					tc.wantCoverage,
				)
			}
			if domain.Extra[graph.ExtraCoverageGaps] != tc.wantGaps {
				t.Errorf("gaps = %q, want %q", domain.Extra[graph.ExtraCoverageGaps], tc.wantGaps)
			}
			if domain.Extra["k"] != "v" {
				t.Error("an existing extra was lost")
			}
			if got[1].Extra != nil {
				t.Errorf("a rule node was annotated: %v", got[1].Extra)
			}
			if len(nodes[0].Extra) != 1 {
				t.Errorf("the input node was modified: %v", nodes[0].Extra)
			}
		})
	}
}
