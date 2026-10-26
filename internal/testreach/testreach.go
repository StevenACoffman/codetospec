// Package testreach tells which tests reach each rule's cited code, from
// the extractor's test facts (what each test uses) and call facts (what
// each definition uses). Mechanical, no LLM: a rule reached by no test is
// a rule nothing verifies, which a reviewer wants to know first.
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
// the nearest few (ExtraTestedBy, by depth then name).
//
// Requires: facts are the run's merged facts.
// Ensures:  nodes is not modified; without any test fact, the nodes are
// returned as they are (no test facts means unknown, not untested); a
// rule citing only other roots gets no field (the facts cover --src).
func Annotate(nodes []graph.Node, facts []extract.Fact) []graph.Node {
	byDef := reachedDefinitions(facts)
	if byDef == nil {
		return nodes
	}
	defs := definitionsByFile(byDef)
	out := slices.Clone(nodes)
	for i := range out {
		if out[i].Type != graph.TypeRule {
			continue
		}
		nearest, cited := testsCiting(out[i].Sources, defs, byDef)
		if !cited {
			continue
		}
		out[i].Extra = maps.Clone(out[i].Extra)
		if out[i].Extra == nil {
			out[i].Extra = map[string]string{}
		}
		out[i].Extra[graph.ExtraTests] = strconv.Itoa(len(nearest))
		if len(nearest) > 0 {
			out[i].Extra[graph.ExtraTestDepth] = strconv.Itoa(nearest[0].depth)
			names := make([]string, 0, maxListed)
			for _, r := range nearest[:min(maxListed, len(nearest))] {
				names = append(names, r.test.String())
			}
			out[i].Extra[graph.ExtraTestedBy] = strings.Join(names, ", ")
		}
	}
	return out
}

// reachedDefinitions maps each definition (a call or test fact span of
// --src) to the tests reaching it within MaxDepth, at their least depth;
// nil when facts hold no test fact.
func reachedDefinitions(facts []extract.Fact) map[extract.Ref][]reached {
	calls := map[extract.Ref][]extract.Ref{} // caller → callees
	direct := map[test][]extract.Ref{}       // test → what it uses
	for i := range facts {
		f := &facts[i]
		if f.Source.Root != "" {
			continue
		}
		callee := extract.Ref{
			Path:  f.Attrs[extract.AttrCalleePath],
			Lines: f.Attrs[extract.AttrCalleeLines],
		}
		switch f.Kind {
		case extract.KindCall:
			calls[f.Source] = append(calls[f.Source], callee)
		case extract.KindTest:
			t := test{path: f.Source.Path, name: f.Attrs[extract.AttrName]}
			direct[t] = append(direct[t], callee)
		}
	}
	if len(direct) == 0 {
		return nil
	}

	byDef := map[extract.Ref][]reached{}
	for t, uses := range direct {
		depth := map[extract.Ref]int{}
		frontier := uses
		for d := 1; d <= MaxDepth && len(frontier) > 0; d++ {
			var next []extract.Ref
			for _, def := range frontier {
				if _, seen := depth[def]; seen {
					continue
				}
				depth[def] = d
				next = append(next, calls[def]...)
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
