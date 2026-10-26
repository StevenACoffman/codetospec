package testreach_test

import (
	"maps"
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

// rule cites refs.
func rule(refs ...extract.Ref) graph.Node {
	return graph.Node{ID: "rule.billing.r", Type: graph.TypeRule, Sources: refs}
}

// TestAnnotate: a rule counts the tests reaching its cited code within
// testreach.MaxDepth definitions, names the nearest first, and says 0
// when none does; a rule citing another root, or a run without test
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

// reach is the Extra of a rule reached by count tests, the nearest at
// depth, named in testedBy.
func reach(count, depth, testedBy string) map[string]string {
	return map[string]string{
		graph.ExtraTests:     count,
		graph.ExtraTestDepth: depth,
		graph.ExtraTestedBy:  testedBy,
	}
}
