package domainmodel

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/StevenACoffman/codetospec/internal/d2"
	"github.com/StevenACoffman/codetospec/internal/locale"
	"github.com/StevenACoffman/indomain/pkg/model"
)

// ConceptMap is the concept map of models: a frame per model (a business
// context; titled as a proposal when proposed), a node per entity (with
// how many rules are placed in it, by qualified name in rules, when any),
// and an edge per relationship: "owns" for an owned one, "references"
// otherwise (once for a pair declared from both sides), "is a" for a
// subtype. An entity of another model none of models is, named by a
// relationship, is drawn outside every frame, as external.
//
// Ensures: deterministic; nil for no entity.
func ConceptMap(models []Model, proposed bool, rules map[string]int, t *locale.Text) *d2.Diagram {
	b := conceptMapBuilder{
		d: &d2.Diagram{Direction: "right"}, t: t,
		known: map[string]bool{}, external: map[string]bool{}, seen: map[d2.Edge]bool{},
	}
	for _, m := range models {
		for _, name := range m.Model.EntityNames() {
			b.known[m.Scope+"."+name] = true
		}
	}
	if len(b.known) == 0 {
		return nil
	}
	for i := range models {
		b.frame(&models[i], proposed, rules)
	}
	for _, id := range slices.Sorted(maps.Keys(b.external)) {
		b.d.Nodes = append(b.d.Nodes, d2.Node{ID: id, Label: id, Class: d2.ClassExternal})
	}
	return b.d
}

// conceptMapBuilder adds a concept map's frames, nodes and edges.
type conceptMapBuilder struct {
	d               *d2.Diagram
	t               *locale.Text
	known, external map[string]bool // entities of the models; others edges name
	seen            map[d2.Edge]bool
}

// frame adds model m's frame, its entities (with their rules) and their
// relationships.
func (b *conceptMapBuilder) frame(m *Model, proposed bool, rules map[string]int) {
	id := "model:" + m.Scope
	title := cmp.Or(m.Model.Title, m.Scope)
	if proposed {
		title = fmt.Sprintf(b.t.ConceptProposalFmt, title)
	}
	b.d.Nodes = append(b.d.Nodes, d2.Node{ID: id, Label: title, Class: d2.ClassGroup})
	for _, name := range m.Model.EntityNames() {
		entity := m.Scope + "." + name
		label := name
		if n := rules[entity]; n > 0 {
			label += "\n" + fmt.Sprintf(b.t.ConceptRulesFmt, n)
		}
		b.d.Nodes = append(b.d.Nodes, d2.Node{ID: entity, Label: label, In: id})
		e := m.Model.Entities[name]
		for _, rel := range e.Relationships {
			kind := b.t.ConceptReferences
			if rel.Ownership == ownershipOwned {
				kind = b.t.ConceptOwns
			}
			b.edge(entity, qualify(m.Scope, rel.Entity), kind)
		}
		if e.SubtypeOf != "" {
			b.edge(entity, qualify(m.Scope, e.SubtypeOf), b.t.ConceptIsA)
		}
	}
}

// edge adds an edge once, a reference once for both directions, and
// records a target no model has as external.
func (b *conceptMapBuilder) edge(from, to, label string) {
	e := d2.Edge{From: from, To: to, Label: label}
	reverse := d2.Edge{From: to, To: from, Label: label}
	if b.seen[e] || (label == b.t.ConceptReferences && b.seen[reverse]) {
		return
	}
	b.seen[e] = true
	b.d.Edges = append(b.d.Edges, e)
	if !b.known[to] {
		b.external[to] = true
	}
}

// Proposal is the proposed concepts model Enhance wrote (ConceptsFile),
// parsed; false when it wrote none.
func Proposal(e *Enhanced) (*Model, bool, error) {
	for _, f := range e.Files {
		if f.Path != ConceptsFile {
			continue
		}
		m, err := model.Parse(f.Data)
		if err != nil {
			return nil, false, fmt.Errorf("parse the proposed concepts: %w", err)
		}
		return &Model{
			Path:   f.Path,
			Scope:  model.ScopeFromPath(f.Path),
			Model:  m,
			Source: f.Data,
		}, true, nil
	}
	return nil, false, nil
}

// qualify is an entity name as "<scope>.<Entity>": a bare name is in
// scope's model.
func qualify(scope, name string) string {
	if strings.Contains(name, ".") {
		return name
	}
	return scope + "." + name
}
