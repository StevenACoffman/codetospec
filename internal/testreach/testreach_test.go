package testreach_test

import (
	"maps"
	"slices"
	"testing"

	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/graph"
	"github.com/StevenACoffman/codetospec/internal/testreach"
)

// The analyzed file and the test file of the call chain.
const (
	codeFile = "calc.go"
	testFile = "calc_test.go"
)

// call is a call fact from one span of codeFile to another.
func call(caller, callee string) extract.Fact {
	return extract.Fact{
		Kind:   extract.KindCall,
		Source: extract.Ref{Path: codeFile, Lines: caller},
		Attrs: map[string]string{
			extract.AttrCalleePath:  codeFile,
			extract.AttrCalleeLines: callee,
		},
	}
}

// use is a test fact: test name uses the codeFile span callee.
func use(name, callee string) extract.Fact {
	return extract.Fact{
		Kind:   extract.KindTest,
		Source: extract.Ref{Path: testFile, Lines: "1-9"},
		Attrs: map[string]string{
			extract.AttrName:        name,
			extract.AttrCalleePath:  codeFile,
			extract.AttrCalleeLines: callee,
		},
	}
}

// viaInterface marks facts as uses through an interface.
func viaInterface(facts ...extract.Fact) []extract.Fact {
	for i := range facts {
		facts[i].Attrs[extract.AttrDispatch] = extract.DispatchInterface
	}
	return facts
}

// rule cites refs.
func rule(refs ...extract.Ref) graph.Node {
	return graph.Node{ID: "rule.billing.r", Type: graph.TypeRule, Sources: refs}
}

// TestAnnotate: a rule counts the tests reaching its cited code within
// testreach.MaxDepth definitions, names the nearest first, and says 0
// when none does; the tests reaching it only through an interface are
// counted apart; a rule citing another root, or a run without test
// facts, gets nothing.
func TestAnnotate(t *testing.T) {
	// TestTotal uses 1-5, which calls 10-15, which calls 20-25, which
	// calls 30-35; TestRound uses 20-25 directly.
	chain := []extract.Fact{
		call("1-5", "10-15"), call("10-15", "20-25"), call("20-25", "30-35"),
		use("TestTotal", "1-5"), use("TestRound", "20-25"),
	}
	at := func(lines string) extract.Ref { return extract.Ref{Path: codeFile, Lines: lines} }
	cases := map[string]struct {
		facts []extract.Fact
		rule  graph.Node
		want  map[string]string
	}{
		"used directly": {
			facts: chain,
			rule:  rule(at("2-3")),
			want:  reach("1", "1", testFile+"#TestTotal"),
		},
		"nearest first": {
			facts: chain,
			rule:  rule(at("21-22")),
			want:  reach("2", "1", testFile+"#TestRound, "+testFile+"#TestTotal"),
		},
		"beyond the depth": {
			facts: chain,
			rule:  rule(at("30-35")),
			want:  reach("1", "2", testFile+"#TestRound"),
		},
		"only through an interface": {
			facts: append(
				slices.Clone(chain),
				viaInterface(call("10-15", "50-55"), use("TestStore", "50-55"))...,
			),
			rule: rule(at("50-55")),
			want: map[string]string{
				graph.ExtraTests:             "0",
				graph.ExtraTestsViaInterface: "2", // TestTotal (depth 3), TestStore
			},
		},
		"directly and through an interface": {
			facts: append(slices.Clone(chain), viaInterface(call("1-5", "30-35"))...),
			rule:  rule(at("30-35")),
			want: with(
				reach("1", "2", testFile+"#TestRound"),
				graph.ExtraTestsViaInterface, "1", // TestTotal, beyond the depth directly
			),
		},
		"untested": {
			facts: chain,
			rule:  rule(extract.Ref{Path: "other.go", Lines: "1-2"}),
			want:  map[string]string{graph.ExtraTests: "0"},
		},
		"another root": {
			facts: chain,
			rule:  rule(extract.Ref{Path: codeFile, Lines: "2-3", Root: "pkg"}),
		},
		"no test facts": {
			facts: chain[:3],
			rule:  rule(at("2-3")),
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			nodes := []graph.Node{tc.rule}
			got := testreach.Annotate(nodes, tc.facts)
			if !maps.Equal(got[0].Extra, tc.want) {
				t.Errorf("extra = %v, want %v", got[0].Extra, tc.want)
			}
			if nodes[0].Extra != nil {
				t.Errorf("input modified: %v", nodes[0].Extra)
			}
		})
	}
}

// with is extra with key set to value.
func with(extra map[string]string, key, value string) map[string]string {
	extra[key] = value
	return extra
}

// reach is the Extra of a rule reached by count tests, the nearest at
// depth, named in testedBy.
func reach(count, depth, testedBy string) map[string]string {
	return map[string]string{
		graph.ExtraTests:     count,
		graph.ExtraTestDepth: depth,
		graph.ExtraTestedBy:  testedBy,
	}
}

// ran is an executed fact of codeFile.
func ran(executed, unexecuted string) extract.Fact {
	return extract.Fact{
		Kind:   extract.KindExecuted,
		Source: extract.Ref{Path: codeFile, Lines: "1-99"},
		Attrs: map[string]string{
			extract.AttrExecuted:   executed,
			extract.AttrUnexecuted: unexecuted,
		},
	}
}

// TestAnnotateExecuted: a rule gets how many statement lines of its cited
// code the profiled run executed, a line two citations share counted
// once; a citation in a file the run has no record of, or holding no
// statement, leaves it unknown; executed facts alone are enough.
func TestAnnotateExecuted(t *testing.T) {
	facts := []extract.Fact{ran("3-4,7-7", "5-6")}
	at := func(lines string) extract.Ref { return extract.Ref{Path: codeFile, Lines: lines} }
	cases := map[string]struct {
		rule graph.Node
		want string // "" for no field
	}{
		"partly executed":   {rule(at("1-9")), "3/5"},
		"shared lines once": {rule(at("3-5"), at("4-6")), "2/4"},
		"no statement":      {rule(at("8-9")), ""},
		"file without a record": {
			rule(at("3-4"), extract.Ref{Path: "other.go", Lines: "1-2"}),
			"",
		},
		"another root is skipped": {
			rule(at("7-7"), extract.Ref{Path: codeFile, Lines: "1-9", Root: "pkg"}),
			"1/1",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := testreach.Annotate([]graph.Node{tc.rule}, facts)[0].Extra[graph.ExtraExecuted]
			if got != tc.want {
				t.Errorf("executed = %q, want %q", got, tc.want)
			}
		})
	}
}
