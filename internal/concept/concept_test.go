package concept_test

import (
	"reflect"
	"testing"

	"github.com/StevenACoffman/codetospec/internal/concept"
	"github.com/StevenACoffman/codetospec/internal/graph"
	"github.com/StevenACoffman/codetospec/internal/position"
)

// The concepts of testConcepts the tests name: assignment owns the others
// but one, configConcept among them.
const (
	assignment    = "a.Assignment"
	configConcept = "a.Config"
)

// testConcepts: assignment owns its config and its students; classroom
// stands alone.
func testConcepts() []concept.Concept {
	return []concept.Concept{
		{Name: assignment, Entity: "entity.assignments", Table: "assignments"},
		{Name: configConcept, Entity: "entity.configs", Table: "configs", Owner: assignment},
		{Name: "a.Student", Entity: "entity.students", Table: "students", Owner: assignment},
		{Name: "c.Classroom", Entity: "entity.classrooms", Table: "classrooms"},
	}
}

// assigned is what Assign wrote on n: its concept fields that are set.
func assigned(n *graph.Node) map[string]string {
	got := map[string]string{}
	for _, key := range []string{
		concept.ExtraConcept, concept.ExtraConceptEvidence, concept.ExtraConceptConflict,
	} {
		if v := n.Extra[key]; v != "" {
			got[key] = v
		}
	}
	return got
}

// TestAssign: one concept's evidence names it; several resolve to their
// nearest common owner; unrelated ones are a conflict listing each; a rule
// whose evidence is not business, or names no concept, gets nothing.
func TestAssign(t *testing.T) {
	touches := func(ids ...string) []graph.Edge {
		var edges []graph.Edge
		for _, id := range ids {
			edges = append(edges, graph.Edge{Type: graph.EdgeTouches, To: id})
		}
		return edges
	}
	business := map[string]string{graph.ExtraNatureByEvidence: position.Business}
	cases := map[string]struct {
		edges []graph.Edge
		extra map[string]string // business when nil
		want  map[string]string
	}{
		"one entity": {
			edges: touches("entity.configs"),
			want: map[string]string{
				concept.ExtraConcept:         configConcept,
				concept.ExtraConceptEvidence: "a.Config: touches entity.configs",
			},
		},
		"condition on a table": {
			extra: map[string]string{
				graph.ExtraNatureByEvidence: position.Business,
				graph.ExtraCondition:        `{"var":"students.grade","op":"greater_than","value":3}`,
			},
			want: map[string]string{
				concept.ExtraConcept:         "a.Student",
				concept.ExtraConceptEvidence: "a.Student: condition students.grade",
			},
		},
		"siblings → owner": {
			edges: touches("entity.configs", "entity.students"),
			want: map[string]string{
				concept.ExtraConcept: assignment,
				concept.ExtraConceptEvidence: "a.Config: touches entity.configs; " +
					"a.Student: touches entity.students",
			},
		},
		"child and owner → owner": {
			edges: touches("entity.assignments", "entity.students"),
			want: map[string]string{
				concept.ExtraConcept: assignment,
				concept.ExtraConceptEvidence: "a.Assignment: touches entity.assignments; " +
					"a.Student: touches entity.students",
			},
		},
		"unrelated → conflict": {
			edges: touches("entity.configs", "entity.classrooms"),
			want: map[string]string{
				concept.ExtraConceptConflict: "a.Config: touches entity.configs; " +
					"c.Classroom: touches entity.classrooms",
			},
		},
		"technical evidence": {
			edges: touches("entity.configs"),
			extra: map[string]string{graph.ExtraNatureByEvidence: position.Technical},
			want:  map[string]string{},
		},
		"no concept evidence": {
			edges: touches("entity.unmodeled"),
			want:  map[string]string{},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			extra := tc.extra
			if extra == nil {
				extra = business
			}
			rule := graph.Node{ID: "rule.x.y", Type: graph.TypeRule, Edges: tc.edges, Extra: extra}
			out, _ := concept.Assign([]graph.Node{rule}, testConcepts())
			if got := assigned(&out[0]); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
			if _, set := rule.Extra[concept.ExtraConcept]; set {
				t.Error("the input node was modified")
			}
		})
	}
}

// TestAssignReport: per concept, its rules and the domains they come from,
// most first; conflicts and unplaced business rules counted.
func TestAssignReport(t *testing.T) {
	rule := func(id, domain string, entities ...string) graph.Node {
		n := graph.Node{
			ID: id, Type: graph.TypeRule,
			Edges: []graph.Edge{{Type: graph.EdgeBelongsTo, To: "domain." + domain}},
			Extra: map[string]string{graph.ExtraNatureByEvidence: position.Business},
		}
		for _, e := range entities {
			n.Edges = append(n.Edges, graph.Edge{Type: graph.EdgeTouches, To: e})
		}
		return n
	}
	nodes := []graph.Node{
		rule("rule.resolvers.a", "resolvers", "entity.assignments"),
		rule("rule.models.b", "models", "entity.configs", "entity.students"),
		rule("rule.models.c", "models", "entity.assignments"),
		rule("rule.models.d", "models", "entity.configs", "entity.classrooms"),
		rule("rule.models.e", "models"),
	}
	_, got := concept.Assign(nodes, testConcepts())
	want := concept.Report{
		Concepts: []concept.Count{
			{Concept: assignment, Rules: 3, Domains: []string{"models (2)", "resolvers (1)"}},
		},
		Conflicts: 1,
		Unplaced:  1,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("report = %+v\nwant     %+v", got, want)
	}
}

// TestSubjects: the actor owning a rule's concept, or its whole model, is
// its subject, the concept's owner first; a reviewed rule keeps its own.
func TestSubjects(t *testing.T) {
	rule := func(id, name, status string) graph.Node {
		return graph.Node{
			ID: id, Type: graph.TypeRule, Status: status,
			Body:  "**Requirement (EARS)**: The system shall keep the assignment's due date.",
			Extra: map[string]string{concept.ExtraConcept: name, "ears": "ubiquitous"},
		}
	}
	owners := map[string]string{"a": "the Assignment_Manager", configConcept: "the Config_Store"}
	nodes := []graph.Node{
		rule("rule.x.concept", configConcept, graph.StatusGenerated),
		rule("rule.x.scope", "a.Student", graph.StatusGenerated),
		rule("rule.x.reviewed", "a.Student", graph.StatusReviewed),
		rule("rule.x.none", "", graph.StatusGenerated),
	}
	out, changed := concept.Subjects(nodes, owners)
	want := map[string]string{
		"rule.x.concept":  "The Config_Store shall keep the assignment's due date.",
		"rule.x.scope":    "The Assignment_Manager shall keep the assignment's due date.",
		"rule.x.reviewed": "The system shall keep the assignment's due date.",
		"rule.x.none":     "The system shall keep the assignment's due date.",
	}
	for i := range out {
		if got := graph.RuleRequirement(out[i].Body); got != want[out[i].ID] {
			t.Errorf("%s: %q, want %q", out[i].ID, got, want[out[i].ID])
		}
	}
	if changed != 2 {
		t.Errorf("changed = %d, want 2", changed)
	}
}
