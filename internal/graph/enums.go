package graph

import (
	"cmp"
	"fmt"
	"maps"
	pathpkg "path"
	"slices"
	"strconv"
	"strings"

	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/locale"
)

// TypeEnum is the Node.Type of an enumeration: a set of named values,
// with the transitions between them that the code guards.
const TypeEnum = "enum"

// ExtraTransitions is the frontmatter key of how many transitions an
// enumeration's code guards; absent when none.
const ExtraTransitions = "transitions"

// enumDraft collects one enumeration's facts.
type enumDraft struct {
	fact        *extract.Fact
	values      []*extract.Fact
	transitions []*extract.Fact
}

// enumNodes builds one node per extract.KindEnum fact with at least one
// value: its values in declaration order, then its transitions sorted by
// from, to and citation. It cites the type, each value and each
// transition, so drift sees any of them change.
func enumNodes(facts []extract.Fact, text *locale.Text) []Node {
	drafts := map[string]*enumDraft{}
	for i := range facts {
		if f := &facts[i]; f.Kind == extract.KindEnum {
			drafts[f.ID] = &enumDraft{fact: f}
		}
	}
	for i := range facts {
		f := &facts[i]
		d := drafts[f.Attrs[extract.AttrEnum]]
		switch {
		case d == nil:
		case f.Kind == extract.KindEnumValue:
			d.values = append(d.values, f)
		case f.Kind == extract.KindTransition:
			d.transitions = append(d.transitions, f)
		}
	}

	var nodes []Node
	for _, id := range slices.Sorted(maps.Keys(drafts)) {
		d := drafts[id]
		if len(d.values) == 0 {
			continue
		}
		nodes = append(nodes, d.node(text))
	}
	return nodes
}

// enumID derives the node id of an enum fact from its type's directory
// and name, unique since one directory holds one package.
func enumID(f *extract.Fact) string {
	name := f.Attrs[extract.AttrName]
	if _, typeName, found := strings.Cut(name, "."); found {
		name = typeName
	}
	dir := pathpkg.Dir(f.Source.Path)
	if dir == "." {
		return TypeEnum + "." + extract.Slugify(name)
	}
	return TypeEnum + "." + extract.Slugify(dir+" "+name)
}

// node renders the draft.
func (d *enumDraft) node(text *locale.Text) Node {
	sources := []extract.Ref{d.fact.Source}
	// Facts arrive sorted by id; the code's order is the citations'.
	slices.SortStableFunc(
		d.values,
		func(a, b *extract.Fact) int { return compareRefs(a.Source, b.Source) },
	)
	var body strings.Builder
	body.WriteString(text.EnumValues + "\n")
	for _, v := range d.values {
		fmt.Fprintf(&body, "\n- `%s` = `%s`", v.Attrs[extract.AttrName], v.Attrs[extract.AttrValue])
		sources = append(sources, v.Source)
	}
	if len(d.transitions) > 0 {
		slices.SortFunc(d.transitions, func(a, b *extract.Fact) int {
			return cmp.Or(
				cmp.Compare(a.Attrs[extract.AttrFrom], b.Attrs[extract.AttrFrom]),
				cmp.Compare(a.Attrs[extract.AttrTo], b.Attrs[extract.AttrTo]),
				compareRefs(a.Source, b.Source),
			)
		})
		body.WriteString("\n\n" + text.EnumTransitions + "\n\n" + text.EnumTransitionHeader)
		for _, t := range d.transitions {
			fmt.Fprintf(&body, "\n| `%s` | `%s` | `%s:%s` |",
				t.Attrs[extract.AttrFrom], t.Attrs[extract.AttrTo], t.Source.Path, t.Source.Lines)
			sources = append(sources, t.Source)
		}
	}
	slices.SortFunc(sources, compareRefs)
	n := Node{
		ID:      enumID(d.fact),
		Type:    TypeEnum,
		Status:  StatusGenerated,
		Sources: slices.Compact(sources),
		Title:   d.fact.Attrs[extract.AttrName],
		Body:    body.String(),
	}
	if len(d.transitions) > 0 {
		n.Extra = map[string]string{ExtraTransitions: strconv.Itoa(len(d.transitions))}
	}
	return n
}

// compareRefs orders refs by file, then by first line.
func compareRefs(a, b extract.Ref) int {
	aStart, _, _ := extract.ParseLines(a.Lines)
	bStart, _, _ := extract.ParseLines(b.Lines)
	return cmp.Or(
		cmp.Compare(a.File(), b.File()),
		cmp.Compare(aStart, bStart),
		cmp.Compare(a.Lines, b.Lines),
	)
}
