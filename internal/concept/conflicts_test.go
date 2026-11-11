package concept_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/StevenACoffman/codetospec/internal/concept"
	"github.com/StevenACoffman/codetospec/internal/graph"
)

// TestConflictSets replays the shape of assignments' 11 conflicts: six
// concept sets, one containing another, answered in four questions.
func TestConflictSets(t *testing.T) {
	const (
		a    = "m.Assignments"
		st   = "m.AssignmentStudents"
		sma  = "m.SubjectMastery"
		cme  = "m.CourseEvent"
		ume  = "m.UnitEvent"
		conf = "m.Config"
	)
	conflict := func(names ...string) graph.Node {
		options := make([]string, len(names))
		for i, n := range names {
			options[i] = n + ": touches entity.x"
		}
		return graph.Node{
			Type:  graph.TypeRule,
			Extra: map[string]string{concept.ExtraConceptConflict: strings.Join(options, "; ")},
		}
	}
	var nodes []graph.Node
	for range 4 {
		nodes = append(nodes, conflict(cme, sma))
	}
	for range 3 {
		nodes = append(nodes, conflict(a, ume))
	}
	nodes = append(nodes,
		conflict(a, st, sma),
		conflict(cme, conf),
		conflict(a, sma),
		conflict(a, cme, ume),
		graph.Node{Type: graph.TypeRule}, // placed or unplaced: no conflict
	)
	want := []concept.ConflictSet{
		{Concepts: []string{cme, sma}, Rules: 4, Settles: 4},
		{Concepts: []string{a, cme, ume}, Rules: 1, Settles: 4}, // and the 3 of {a, ume}
		{
			Concepts: []string{st, a, sma},
			Rules:    1,
			Settles:  2,
		}, // and {a, sma}; "AssignmentS" < "Assignments"
		{Concepts: []string{conf, cme}, Rules: 1, Settles: 1},
	}
	cases := map[string][]graph.Node{
		"in order": nodes,
		"reversed": reversedNodes(nodes),
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			if got := concept.ConflictSets(in); !reflect.DeepEqual(got, want) {
				t.Errorf("sets =\n%+v\nwant\n%+v", got, want)
			}
		})
	}
}

func reversedNodes(nodes []graph.Node) []graph.Node {
	out := make([]graph.Node, len(nodes))
	for i, n := range nodes {
		out[len(nodes)-1-i] = n
	}
	return out
}
