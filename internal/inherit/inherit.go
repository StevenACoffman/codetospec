// Package inherit brings into a spec the rules of another spec, written for
// shared code (a workspace's pkg/), that the analyzed code reaches. The
// analyzed code's extractors say what they reach (reach facts, in any
// language); the shared spec says what its code does. A rule of the shared
// spec becomes the analyzed code's when its citation lies in reached code.
package inherit

import (
	"slices"
	"strings"

	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/graph"
)

// sharedDomainPrefix places inherited rules in their own domains,
// shared-<domain of the shared spec>, apart from the analyzed code's.
const sharedDomainPrefix = "shared-"

// Rules returns the rules of spec, the spec of the shared root name, that
// the reach facts cover, as inherited rule facts cited under name. Only the
// spec's own LLM rules with a single citation are inherited: a mechanical
// rule is its extractor's to derive again; one inherited from yet another
// spec stays there; a fact holds one citation.
//
// Requires: spec holds the nodes of a rendered spec, its citations relative
// to the shared root; reach facts cite that root as name.
// Ensures:  facts sorted by id; each keeps the rule's verdict, origin and
// confidence and is labeled inherited_from name.
func Rules(spec []graph.Node, name string, reach []extract.Fact) []extract.Fact {
	spans := reachedSpans(name, reach)
	var facts []extract.Fact
	for i := range spec {
		n := &spec[i]
		if !inheritable(n) {
			continue
		}
		src := n.Sources[0]
		start, end, err := extract.ParseLines(src.Lines)
		if err != nil || !spans.overlap(src.Path, start, end) {
			continue
		}
		facts = append(facts, ruleFact(n, name))
	}
	slices.SortFunc(facts, func(a, b extract.Fact) int { return strings.Compare(a.ID, b.ID) })
	return facts
}

// inheritable reports whether a spec node is a rule Rules may inherit.
func inheritable(n *graph.Node) bool {
	return n.Type == graph.TypeRule &&
		n.Extra[graph.ExtraExtractedBy] == "" &&
		n.Extra[graph.ExtraInheritedFrom] == "" &&
		len(n.Sources) == 1 && n.Sources[0].Root == ""
}

// ruleFact is the inherited rule fact of a spec rule node.
func ruleFact(n *graph.Node, name string) extract.Fact {
	rest := strings.TrimPrefix(n.ID, "rule.")
	domain, slug, found := strings.Cut(rest, ".")
	if !found {
		domain, slug = name, rest
	}
	src := n.Sources[0]
	return extract.Fact{
		Kind: extract.KindRule,
		ID:   "rule.inherited." + name + "." + rest,
		Attrs: map[string]string{
			extract.AttrSlug:          slug,
			extract.AttrTitle:         n.Title,
			extract.AttrRequirement:   graph.RuleRequirement(n.Body),
			extract.AttrEARS:          n.Extra["ears"],
			extract.AttrNature:        n.Extra["nature"],
			extract.AttrDomain:        extract.Slugify(sharedDomainPrefix + domain),
			extract.AttrInheritedFrom: name,
			extract.AttrCrosscheck:    n.Extra["crosscheck"],
			extract.AttrOrigin:        n.Extra["origin"],
			extract.AttrConfidence:    n.Extra["confidence"],
		},
		Source:    extract.Ref{Path: src.Path, Lines: src.Lines, Root: name},
		Origin:    "inherit",
		Certainty: "static",
	}
}

// spans are the reached line ranges of the shared root, by file.
type spans map[string][][2]int

// reachedSpans collects the spans of the reach facts citing root name.
func reachedSpans(name string, reach []extract.Fact) spans {
	s := spans{}
	for i := range reach {
		f := &reach[i]
		if f.Kind != extract.KindReach || f.Source.Root != name {
			continue
		}
		if start, end, err := extract.ParseLines(f.Source.Lines); err == nil {
			s[f.Source.Path] = append(s[f.Source.Path], [2]int{start, end})
		}
	}
	return s
}

// overlap reports whether lines start-end of file intersect a reached span.
func (s spans) overlap(file string, start, end int) bool {
	return slices.ContainsFunc(s[file], func(r [2]int) bool { return start <= r[1] && end >= r[0] })
}
