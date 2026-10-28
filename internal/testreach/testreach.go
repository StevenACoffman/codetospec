// Package testreach tells which tests reach each rule's cited code, from
// the extractor's test facts (what each test uses) and call facts (what
// each definition uses), and how much of it a profiled test run executed
// (executed facts). Mechanical, no LLM: a rule reached by no test is a
// rule nothing verifies, which a reviewer wants to know first. Reach is
// derived from the code; execution is observed: they are kept apart.
package testreach

import (
	"cmp"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/graph"
)

// MaxDepth is how many definitions deep a test reaches: 1 when it uses
// the cited definition itself. Deeper reach is weak evidence: an
// end-to-end test reaches most of a module without asserting on it.
const MaxDepth = 3

// maxListed caps the nearest tests a rule names.
const maxListed = 3

// test is one test function, by file and name.
type test struct {
	path, name string
}

// String is how a rule names the test.
func (t test) String() string { return t.path + "#" + t.name }

// reached is a test reaching a definition, at a depth.
type reached struct {
	test  test
	depth int
}

// Annotate sets, on each rule citing code of --src, how many tests reach
// that code (ExtraTests), the depth of the nearest (ExtraTestDepth) and
// the nearest few (ExtraTestedBy, by depth then name), through direct
// uses alone; how many more reach it only through a call of an interface
// the cited code implements (ExtraTestsViaInterface, when any): a test
// that may run the code, not one known to; and, from a coverage profile,
// how much of the cited code a test run did execute (ExtraExecuted).
//
// Requires: facts are the run's merged facts.
// Ensures:  nodes is not modified; without test facts no reach field is
// set, and without executed facts for every cited file no ExtraExecuted
// (no facts means unknown, not untested); a rule citing only other roots
// gets no field (the facts cover --src).
func Annotate(nodes []graph.Node, facts []extract.Fact) []graph.Node {
	reach := reachOf(facts)
	profiled := runsOf(facts)
	if reach == nil && profiled == nil {
		return nodes
	}
	out := slices.Clone(nodes)
	for i := range out {
		if out[i].Type != graph.TypeRule {
			continue
		}
		fields := map[string]string{}
		if reach != nil {
			reach.fields(out[i].Sources, fields)
		}
		if executed, known := profiled.executed(out[i].Sources); known {
			fields[graph.ExtraExecuted] = executed
		}
		if len(fields) == 0 {
			continue
		}
		out[i].Extra = maps.Clone(out[i].Extra)
		if out[i].Extra == nil {
			out[i].Extra = map[string]string{}
		}
		maps.Copy(out[i].Extra, fields)
	}
	return out
}

// testReach is which tests reach each definition: through direct uses,
// and through uses via an interface too.
type testReach struct {
	direct, possible         map[extract.Ref][]reached
	directDefs, possibleDefs map[string][]span
}

// reachOf computes the test reach of facts; nil without any test fact.
func reachOf(facts []extract.Fact) *testReach {
	static, all := usesOf(facts, false), usesOf(facts, true)
	if len(all.tests) == 0 {
		return nil
	}
	r := &testReach{direct: static.reach(), possible: all.reach()}
	r.directDefs, r.possibleDefs = definitionsByFile(r.direct), definitionsByFile(r.possible)
	return r
}

// fields sets the reach fields of a rule citing sources, none when no
// source is under --src.
func (r *testReach) fields(sources []extract.Ref, fields map[string]string) {
	nearest, cited := testsCiting(sources, r.directDefs, r.direct)
	if !cited {
		return
	}
	fields[graph.ExtraTests] = strconv.Itoa(len(nearest))
	if len(nearest) > 0 {
		fields[graph.ExtraTestDepth] = strconv.Itoa(nearest[0].depth)
		names := make([]string, 0, maxListed)
		for _, t := range nearest[:min(maxListed, len(nearest))] {
			names = append(names, t.test.String())
		}
		fields[graph.ExtraTestedBy] = strings.Join(names, ", ")
	}
	maybe, _ := testsCiting(sources, r.possibleDefs, r.possible)
	if more := len(maybe) - len(nearest); more > 0 {
		fields[graph.ExtraTestsViaInterface] = strconv.Itoa(more)
	}
}

// uses is what the module's tests and definitions use: one edge per call
// or test fact of --src.
type uses struct {
	calls map[extract.Ref][]extract.Ref // caller → callees
	tests map[test][]extract.Ref        // test → what it uses
}

// usesOf reads the call and test facts of --src: the direct uses, and
// with viaInterface the uses through an interface too.
func usesOf(facts []extract.Fact, viaInterface bool) uses {
	u := uses{calls: map[extract.Ref][]extract.Ref{}, tests: map[test][]extract.Ref{}}
	for i := range facts {
		f := &facts[i]
		if f.Source.Root != "" || (f.ViaDispatch() && !viaInterface) {
			continue
		}
		callee := extract.Ref{
			Path:  f.Attrs[extract.AttrCalleePath],
			Lines: f.Attrs[extract.AttrCalleeLines],
		}
		switch f.Kind {
		case extract.KindCall:
			u.calls[f.Source] = append(u.calls[f.Source], callee)
		case extract.KindTest:
			t := test{path: f.Source.Path, name: f.Attrs[extract.AttrName]}
			u.tests[t] = append(u.tests[t], callee)
		}
	}
	return u
}

// reach maps each definition (a call or test fact span) to the tests
// reaching it within MaxDepth, at their least depth.
func (u *uses) reach() map[extract.Ref][]reached {
	byDef := map[extract.Ref][]reached{}
	for t, used := range u.tests {
		depth := map[extract.Ref]int{}
		frontier := used
		for d := 1; d <= MaxDepth && len(frontier) > 0; d++ {
			var next []extract.Ref
			for _, def := range frontier {
				if _, seen := depth[def]; seen {
					continue
				}
				depth[def] = d
				next = append(next, u.calls[def]...)
			}
			frontier = next
		}
		for def, d := range depth {
			byDef[def] = append(byDef[def], reached{test: t, depth: d})
		}
	}
	return byDef
}

// span is a definition's lines, parsed once.
type span struct {
	ref        extract.Ref
	start, end int
}

// definitionsByFile indexes the reached definitions by file.
func definitionsByFile(byDef map[extract.Ref][]reached) map[string][]span {
	byFile := map[string][]span{}
	for def := range byDef {
		if start, end, err := extract.ParseLines(def.Lines); err == nil {
			byFile[def.Path] = append(byFile[def.Path], span{def, start, end})
		}
	}
	return byFile
}

// testsCiting returns the tests reaching a definition that overlaps one of
// sources, each at its least depth, sorted by depth then name, and whether
// any source is under --src (otherwise test reach is unknown).
func testsCiting(
	sources []extract.Ref,
	defs map[string][]span,
	byDef map[extract.Ref][]reached,
) ([]reached, bool) {
	least := map[test]int{}
	cited := false
	for _, src := range sources {
		if src.Root != "" {
			continue
		}
		cited = true
		start, end, err := extract.ParseLines(src.Lines)
		if err != nil {
			continue
		}
		for _, s := range defs[src.Path] {
			if s.start > end || start > s.end {
				continue
			}
			for _, r := range byDef[s.ref] {
				if d, seen := least[r.test]; !seen || r.depth < d {
					least[r.test] = r.depth
				}
			}
		}
	}
	nearest := make([]reached, 0, len(least))
	for t, d := range least {
		nearest = append(nearest, reached{test: t, depth: d})
	}
	slices.SortFunc(nearest, func(a, b reached) int {
		return cmp.Or(cmp.Compare(a.depth, b.depth), cmp.Compare(a.test.String(), b.test.String()))
	})
	return nearest, cited
}
