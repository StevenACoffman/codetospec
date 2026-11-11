package alignment_test

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/StevenACoffman/codetospec/internal/alignment"
	"github.com/StevenACoffman/codetospec/internal/concept"
	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/graph"
)

const (
	assignment = "assignments.Assignment"
	student    = "assignments.AssignmentStudent"
	config     = "assignments.Config"
	event      = "assignments.ChangeEvent"
)

// The files of the recovery tests.
const (
	f1 = "f1.go"
	f2 = "f2.go"
	f3 = "f3.go"
	f4 = "f4.go"
)

func placed(id, name string, cites ...string) graph.Node {
	n := graph.Node{
		ID: id, Type: graph.TypeRule,
		Extra: map[string]string{concept.ExtraConcept: name},
	}
	for _, c := range cites {
		path, lines, _ := strings.Cut(c, ":")
		n.Sources = append(n.Sources, extract.Ref{Path: path, Lines: lines})
	}
	return n
}

func call(from, to string, dispatch bool) extract.Fact {
	fromPath, fromLines, _ := strings.Cut(from, ":")
	toPath, toLines, _ := strings.Cut(to, ":")
	f := extract.Fact{
		Kind: extract.KindCall, ID: "call." + from + "#" + to,
		Attrs:  map[string]string{extract.AttrCalleePath: toPath, extract.AttrCalleeLines: toLines},
		Source: extract.Ref{Path: fromPath, Lines: fromLines},
	}
	if dispatch {
		f.Attrs[extract.AttrDispatch] = "interface"
	}
	return f
}

func table(name, references string) extract.Fact {
	return extract.Fact{
		Kind: extract.KindTable, ID: "table." + name,
		Attrs: map[string]string{extract.AttrName: name, extract.AttrReferences: references},
	}
}

// concepts are the model's concepts the pair tests read.
func concepts() []concept.Concept {
	return []concept.Concept{
		{Name: assignment, Table: "assignments"},
		{Name: student, Table: "assignment_students"},
		{Name: config, Table: "configs"},
		{Name: event},
	}
}

// TestMeasurePairs: a pair converges when declared and shown, diverges
// when shown only, is absent when declared and observable but not shown,
// and unobservable when one concept could not be shown; calls through an
// interface and calls within one concept show nothing.
func TestMeasurePairs(t *testing.T) {
	nodes := []graph.Node{
		placed("r1", assignment, "a/create.go:10-20"),
		placed("r2", assignment, "a/store.go:1-9"),
		placed("r3", student, "s/enroll.go:5-15"),
		placed("r4", config, "c/config.go:1-30"),
	}
	facts := []extract.Fact{
		call("a/create.go:10-20", "a/store.go:1-9", false),    // within Assignment
		call("a/create.go:10-20", "c/config.go:12-14", false), // Assignment → Config
		call("s/enroll.go:5-15", "c/config.go:1-3", true),     // through an interface
		table("assignment_students", "assignment_id:assignments:not_null"),
		table("assignments", ""),
		table("configs", ""),
	}
	declared := []alignment.Relation{
		{From: assignment, To: student},
		{From: assignment, To: event},
		{From: student, To: config},
	}
	want := []alignment.Pair{
		{
			A: assignment, B: student, Outcome: alignment.Converges,
			Evidence: []string{
				student + " → " + assignment + ": fk assignment_students(assignment_id) → assignments",
			},
		},
		{A: assignment, B: event, Outcome: alignment.Unobservable},
		{
			A: assignment, B: config, Outcome: alignment.Diverges,
			Evidence: []string{
				assignment + " → " + config + ": call a/create.go:10-20 → c/config.go:12-14",
			},
		},
		{A: student, B: config, Outcome: alignment.Absent},
	}
	cases := map[string]struct {
		nodes    []graph.Node
		facts    []extract.Fact
		declared []alignment.Relation
	}{
		"in order": {nodes, facts, declared},
		"reversed": {reversed(nodes), reversed(facts), reversed(declared)},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := alignment.Measure(&alignment.Input{
				Nodes: tc.nodes, Facts: tc.facts, Concepts: concepts(), Declared: tc.declared,
			}).Pairs
			if !reflect.DeepEqual(got, want) {
				t.Errorf("pairs =\n%+v\nwant\n%+v", got, want)
			}
		})
	}
}

// TestMeasureSpread: a concept's rules over directories, a directory's
// rules over concepts; a rule counts once per directory.
func TestMeasureSpread(t *testing.T) {
	nodes := []graph.Node{
		placed("r1", assignment, "a/create.go:1-5", "a/create.go:9-12"),
		placed("r2", assignment, "a/store.go:1-9", "shared/db.go:1-4"),
		placed("r3", student, "shared/db.go:5-8"),
		{ID: "r4", Type: graph.TypeRule}, // no concept
	}
	got := alignment.Measure(&alignment.Input{Nodes: nodes})
	wantScattering := []alignment.Spread{
		{Name: assignment, Rules: 2, Places: 2, Top: "a", TopRules: 2},
		{Name: student, Rules: 1, Places: 1, Top: "shared", TopRules: 1},
	}
	wantTangling := []alignment.Spread{
		{Name: "shared", Rules: 2, Places: 2, Top: assignment, TopRules: 1},
		{Name: "a", Rules: 2, Places: 1, Top: assignment, TopRules: 2},
	}
	if !reflect.DeepEqual(got.Scattering, wantScattering) {
		t.Errorf("scattering = %+v, want %+v", got.Scattering, wantScattering)
	}
	if !reflect.DeepEqual(got.Tangling, wantTangling) {
		t.Errorf("tangling = %+v, want %+v", got.Tangling, wantTangling)
	}
}

// TestMeasureRecovery: the adjusted Rand index of the files' concepts
// against their communities.
func TestMeasureRecovery(t *testing.T) {
	four := []graph.Node{
		placed("r1", assignment, f1+":1-2"),
		placed("r2", assignment, f2+":1-2"),
		placed("r3", student, f3+":1-2"),
		placed("r4", student, f4+":1-2"),
	}
	cases := map[string]struct {
		nodes       []graph.Node
		communities map[string]int
		want        alignment.Agreement
	}{
		"same partition": {
			four,
			map[string]int{f1: 0, f2: 0, f3: 1, f4: 1},
			alignment.Agreement{Files: 4, Concepts: 2, Communities: 2, ARI: 1, Defined: true},
		},
		"chance": {
			// cells (A,0)=2 (B,0)=1 (B,1)=1: index 1, expected 2·3/6 = 1.
			four,
			map[string]int{f1: 0, f2: 0, f3: 0, f4: 1},
			alignment.Agreement{Files: 4, Concepts: 2, Communities: 2, ARI: 0, Defined: true},
		},
		"tied file left out, too few files": {
			[]graph.Node{
				placed("r1", assignment, f1+":1-2"),
				placed("r2", student, f1+":3-4"),
				placed("r3", student, f2+":1-2"),
			},
			map[string]int{f1: 0, f2: 1},
			alignment.Agreement{Files: 1, Concepts: 1, Communities: 1},
		},
		"no community": {
			four, nil, alignment.Agreement{},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := alignment.Measure(&alignment.Input{Nodes: tc.nodes, Communities: tc.communities})
			if got.Recovery != tc.want {
				t.Errorf("recovery = %+v, want %+v", got.Recovery, tc.want)
			}
		})
	}
}

func reversed[T any](s []T) []T {
	out := slices.Clone(s)
	slices.Reverse(out)
	return out
}
