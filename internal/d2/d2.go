// Package d2 draws codetospec's diagrams in D2 (d2lang.com): the one place
// that knows the language, the palette and the renderer's protocol.
// Callers build a Diagram (nodes by role, edges, containers) and never
// write D2 themselves.
//
// The look is the diagram-design skill's default skin (the repository's
// .diagram-design profile, codetospec): one accent for at most two focal
// nodes, everything else ink and muted, no shadows. D2 takes only opaque
// colours, so the skin's translucent ones are composited over the paper,
// and it cannot switch explicit colours with its dark theme, so a dark
// rendering is a second source in the Dark palette. Layout is ELK, whose
// right-angle connectors the skill requires.
//
// SVG comes from codetospec-d2 (renderers/d2, a module of its own: the D2
// library's dependencies stay out of the main module), run like an
// extractor: a batch of sources as JSON on stdin, their SVGs as JSON on
// stdout (Request, Result).
package d2

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// Classes of a node, by role: how it is drawn. A diagram uses a class
// for what the node is, never for emphasis, except ClassFocal.
const (
	ClassStep     = "step"     // an action, a component
	ClassDecision = "decision" // a question with labelled exits
	ClassTerminal = "terminal" // where a flow starts or ends
	ClassFocal    = "focal"    // the one or two nodes the diagram is about
	ClassGroup    = "group"    // a container of other nodes
	ClassStore    = "store"    // state, a data store
	ClassExternal = "external" // outside the system drawn
)

// Diagram is one picture: nodes, the edges between them, laid out in
// Direction ("down" when empty, or "right").
type Diagram struct {
	Direction string
	Nodes     []Node
	Edges     []Edge
}

// Node is one shape. ID names it among its siblings; In is the ID of the
// ClassGroup node containing it ("" at the top level), declared before it.
// Columns, on a container, lays its members out in a grid of that many
// columns instead of by their edges: for members no edge joins.
type Node struct {
	ID, Label, Class, In string
	Columns              int
}

// Edge is one connection, by node ID, with an optional label.
type Edge struct {
	From, To, Label string
}

// Source is d in D2, drawn in p: deterministic for a given d and p.
//
// Requires: every In names a node of d declared before it. An edge end
// that names no node becomes a top-level node of that name.
func Source(d *Diagram, p *Palette) string {
	var b strings.Builder
	b.WriteString("vars: {d2-config: {layout-engine: elk}}\n")
	fmt.Fprintf(&b, "direction: %s\n", cmp.Or(d.Direction, "down"))
	fmt.Fprintf(&b, "style: {fill: %q}\n", p.Paper)
	writeClasses(&b, p, d)

	paths := map[string]string{} // node ID → its quoted path
	children := map[string][]Node{}
	for _, n := range d.Nodes {
		children[n.In] = append(children[n.In], n)
	}
	var write func(in, prefix, indent string)
	write = func(in, prefix, indent string) {
		for _, n := range children[in] {
			path := prefix + quote(n.ID)
			paths[n.ID] = path
			fmt.Fprintf(&b, "%s%s: %s {class: %s", indent, quote(n.ID), quote(n.Label),
				cmp.Or(n.Class, ClassStep))
			if len(children[n.ID]) == 0 {
				b.WriteString("}\n")
				continue
			}
			b.WriteString("\n")
			if n.Columns > 0 {
				fmt.Fprintf(&b, "%s  grid-columns: %d\n", indent, n.Columns)
			}
			write(n.ID, path+".", indent+"  ")
			b.WriteString(indent)
			b.WriteString("}\n")
		}
	}
	write("", "", "")

	path := func(id string) string {
		if p, ok := paths[id]; ok {
			return p
		}
		return quote(id) // drawn where the edge names it, as D2 does
	}
	for _, e := range d.Edges {
		fmt.Fprintf(&b, "%s -> %s", path(e.From), path(e.To))
		if e.Label != "" {
			b.WriteString(": ")
			b.WriteString(quote(e.Label))
		}
		b.WriteString("\n")
	}
	return b.String()
}

// writeClasses declares the node classes d uses, and the edges' look,
// in p: a reader of the source sees only what the diagram draws.
func writeClasses(b *strings.Builder, p *Palette, d *Diagram) {
	classes := map[string]string{
		ClassStep: fmt.Sprintf(
			"style: {fill: %q; stroke: %q; font-color: %q; border-radius: 6; stroke-width: 1; font-size: 12}",
			p.Node,
			p.Ink,
			p.Ink,
		),
		ClassDecision: fmt.Sprintf(
			"shape: diamond; style: {fill: %q; stroke: %q; font-color: %q; stroke-width: 1; font-size: 12}",
			p.Node,
			p.Ink,
			p.Ink,
		),
		ClassTerminal: fmt.Sprintf(
			"shape: oval; style: {fill: %q; stroke: %q; font-color: %q; stroke-width: 1; font-size: 12}",
			p.Input,
			p.Soft,
			p.Ink,
		),
		ClassFocal: fmt.Sprintf(
			"style: {fill: %q; stroke: %q; font-color: %q; border-radius: 6; stroke-width: 1; font-size: 12}",
			p.AccentTint,
			p.Accent,
			p.Ink,
		),
		ClassGroup: fmt.Sprintf(
			"style: {fill: %q; stroke: %q; font-color: %q; border-radius: 8; stroke-width: 1; font-size: 12}",
			p.Group,
			p.Rule,
			p.Muted,
		),
		ClassStore: fmt.Sprintf(
			"shape: cylinder; style: {fill: %q; stroke: %q; font-color: %q; stroke-width: 1; font-size: 12}",
			p.Store,
			p.Muted,
			p.Ink,
		),
		ClassExternal: fmt.Sprintf(
			"style: {fill: %q; stroke: %q; font-color: %q; border-radius: 6; stroke-width: 1; font-size: 12}",
			p.External,
			p.ExternalStroke,
			p.Ink,
		),
	}
	used := map[string]bool{}
	for _, n := range d.Nodes {
		used[cmp.Or(n.Class, ClassStep)] = true
	}
	if len(used) > 0 {
		b.WriteString("classes: {\n")
		for _, name := range slices.Sorted(maps.Keys(used)) {
			fmt.Fprintf(b, "  %s: {%s}\n", name, classes[name])
		}
		b.WriteString("}\n")
	}
	if len(d.Edges) > 0 {
		fmt.Fprintf(b,
			"(** -> **)[*].style: {stroke: %q; stroke-width: 1; font-color: %q; font-size: 12}\n",
			p.Muted, p.Soft)
	}
}

// quote is s as a D2 double-quoted string: a key or label, whatever its
// characters (D2 reads "." as nesting and "${" as a variable outside one).
func quote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "$", `\$`).Replace(s) + `"`
}
