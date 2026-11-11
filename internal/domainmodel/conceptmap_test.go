package domainmodel_test

import (
	"slices"
	"testing"

	"github.com/StevenACoffman/codetospec/internal/d2"
	"github.com/StevenACoffman/codetospec/internal/domainmodel"
	"github.com/StevenACoffman/codetospec/internal/locale"
	"github.com/StevenACoffman/indomain/pkg/model"
)

func parsed(t *testing.T, scope, src string) domainmodel.Model {
	t.Helper()
	m, err := model.Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	return domainmodel.Model{Scope: scope, Model: m}
}

// TestConceptMap: a frame per model, its entities inside with their
// placed rules, owns / references / is-a edges (a reference declared
// from both sides once), an entity of a model not given outside every
// frame as external; a proposal says so in its title.
func TestConceptMap(t *testing.T) {
	work := parsed(t, "work", `kind: DomainModel
version: v1
title: Coursework
entities:
  Assignment:
    definition: Work a teacher gives.
    relationships:
      - {entity: Submission, cardinality: "1:n", ownership: owned}
      - {entity: grading.Score, cardinality: "1:n"}
  Submission:
    definition: A student's answer.
    relationships:
      - {entity: users.User, cardinality: "n:1"}
  Quiz:
    definition: A short assignment.
    subtypeOf: Assignment
`)
	grading := parsed(t, "grading", `kind: DomainModel
version: v1
entities:
  Score:
    definition: A grade.
    relationships:
      - {entity: work.Assignment, cardinality: "n:1"}
`)
	text := locale.For("en")
	d := domainmodel.ConceptMap([]domainmodel.Model{work, grading}, false,
		map[string]int{"work.Assignment": 12}, text)

	label := map[string]d2.Node{}
	for _, n := range d.Nodes {
		label[n.ID] = n
	}
	if n := label["model:work"]; n.Label != "Coursework" || n.Class != d2.ClassGroup {
		t.Errorf("work frame = %+v", n)
	}
	if n := label["model:grading"]; n.Label != "grading" {
		t.Errorf("an untitled model's frame = %q, want its scope", n.Label)
	}
	if n := label["work.Assignment"]; n.Label != "Assignment\n12 rules" || n.In != "model:work" {
		t.Errorf("Assignment = %+v", n)
	}
	if n := label["users.User"]; n.Class != d2.ClassExternal || n.In != "" {
		t.Errorf("users.User = %+v, want external, outside every frame", n)
	}
	var edges []string
	for _, e := range d.Edges {
		edges = append(edges, e.From+" -"+e.Label+"-> "+e.To)
	}
	want := []string{
		"work.Assignment -owns-> work.Submission",
		"work.Assignment -references-> grading.Score",
		"work.Quiz -is a-> work.Assignment",
		"work.Submission -references-> users.User",
	}
	if !slices.Equal(edges, want) {
		t.Errorf("edges = %q\nwant    %q", edges, want)
	}

	proposal := domainmodel.ConceptMap([]domainmodel.Model{work}, true, nil, text)
	if got := proposal.Nodes[0].Label; got != "Coursework (proposal)" {
		t.Errorf("proposal frame = %q", got)
	}
	if domainmodel.ConceptMap(nil, false, nil, text) != nil {
		t.Error("no model should draw no map")
	}
}
