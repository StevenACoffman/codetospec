package concept_test

import (
	"maps"
	"slices"
	"testing"

	"github.com/StevenACoffman/codetospec/internal/concept"
	"github.com/StevenACoffman/codetospec/internal/graph"
)

// TestRegroup: a placed rule moves into its context's domain, named after
// the scope, or with "-context" when a code domain of that name keeps a
// rule; a reviewed rule and one placed in an unknown model stay; a
// context depends on the contexts its model imports.
func TestRegroup(t *testing.T) {
	const reviewedID, grading = "rule.read.reviewed", "grading"
	rule := func(id, domain, concept string) graph.Node {
		n := graph.Node{
			ID: id, Type: graph.TypeRule, Title: id,
			Edges: []graph.Edge{{Type: graph.EdgeBelongsTo, To: "domain." + domain}},
			Extra: map[string]string{conceptKey: concept},
		}
		if id == reviewedID {
			n.Status = graph.StatusReviewed
		}
		return n
	}
	domain := func(slug string) graph.Node {
		return graph.Node{
			ID: "domain." + slug, Type: graph.TypeDomain, Body: "## Rules",
			Extra: map[string]string{graph.ExtraCoverage: graph.CoverageComplete},
		}
	}
	nodes := []graph.Node{
		domain("read"), domain("work"), domain(grading),
		rule("rule.read.assignment", "read", "work.Assignment"),
		rule("rule.grading.score", grading, "grading.Score"),
		rule(reviewedID, "read", "work.Assignment"),
		rule("rule.read.other", "read", "elsewhere.Thing"),
		rule("rule.work.helper", "work", ""),
	}
	contexts := []concept.Context{
		{Scope: "work", Summary: "Coursework.", Imports: []string{grading}},
		{Scope: grading, Summary: "Scores."},
	}
	out, moved := concept.Regroup(nodes, contexts, "en")

	if want := map[string]int{"work-context": 1, grading: 1}; !maps.Equal(moved, want) {
		t.Errorf("moved = %v, want %v", moved, want)
	}
	domainOf := map[string]string{}
	var domains []string
	for i := range out {
		switch out[i].Type {
		case graph.TypeRule:
			domainOf[out[i].ID] = graph.RuleDomain(&out[i])
		case graph.TypeDomain:
			domains = append(domains, out[i].ID)
		}
		if out[i].ID == "domain.work-context" && !slices.Contains(out[i].Edges,
			graph.Edge{Type: graph.EdgeDependsOn, To: "domain.grading"}) {
			t.Errorf("work-context edges = %v, want depends_on grading", out[i].Edges)
		}
	}
	for id, want := range map[string]string{
		"rule.read.assignment": "work-context", "rule.grading.score": grading,
		reviewedID: "read", "rule.read.other": "read", "rule.work.helper": "work",
	} {
		if domainOf[id] != want {
			t.Errorf("%s in %s, want %s", id, domainOf[id], want)
		}
	}
	// grading's only rule moved into the grading context, which takes its name.
	want := []string{"domain.grading", "domain.read", "domain.work", "domain.work-context"}
	if !slices.Equal(domains, want) {
		t.Errorf("domains = %v, want %v", domains, want)
	}
	if again, _ := concept.Regroup(nodes, nil, "en"); len(again) != len(nodes) {
		t.Error("no context should move nothing")
	}
}

// conceptKey is the Extra key Assign records a rule's concept under.
const conceptKey = concept.ExtraConcept
