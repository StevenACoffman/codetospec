// Package policyview builds a service's access views from its policy
// model (export --format views): an access matrix, which callers each
// exported operation allows, decided by the exported policies in the
// Cedar policy language (cedarpolicy.Decider), and one decision diagram
// per guarded operation (D2: the checks in rule order, each a question).
//
// The views follow the diagram-design skill: a matrix of a hundred rows
// is a table, not a diagram; callers the policies decide alike merge into
// one column; a diagram's one focal node is where the request is
// allowed, never a denial.
package policyview

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/StevenACoffman/codetospec/internal/cedarpolicy"
	"github.com/StevenACoffman/codetospec/internal/d2"
	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/locale"
	"github.com/StevenACoffman/codetospec/internal/policy"
)

// Input is what the views are built from.
type Input struct {
	Service string
	Model   *policy.Model
	Decider *cedarpolicy.Decider
	// Skipped are the operations the exports leave out, each with the
	// reasons its guards don't translate.
	Skipped  map[string][]string
	Registry policy.Registry
	Text     *locale.Text
}

// Views are one service's access views, before their diagrams are drawn.
type Views struct {
	Service  string
	Columns  []Column
	Rows     []Row
	Skipped  []SkippedRow
	Personas int // before merging
	// Diagrams are the decision diagrams, by name ("operations/<slug>"),
	// Names their names in row order.
	Diagrams map[string]*d2.Diagram
	Names    []string
	text     *locale.Text
}

// Column is the callers the policies decide alike, labelled.
type Column struct {
	Label    string
	Personas []string // persona IDs
}

// Row is one exported operation, or one item kind's view, and what the
// policies decide for each column.
type Row struct {
	Label   string
	Limited string // its rate limits, "" for none
	Diagram string // its diagram's name, "" for none (no deny)
	Cells   []Cell
}

// Cell is what the policies decide for one column: an Outcome
// (cedarpolicy.Allowed…) and its words.
type Cell struct {
	Outcome, Text string
}

// SkippedRow is an operation left out of the exports, and why.
type SkippedRow struct {
	Operation string
	Reasons   []string
}

// Build decides every persona's request on every exported operation and
// item kind, merges the personas decided alike, and lays out a decision
// diagram per operation with a deny.
//
// Ensures: deterministic.
func Build(in *Input) *Views {
	t := in.Text
	goNames := map[string]string{}
	for _, c := range in.Model.Capabilities {
		goNames[c.Name] = c.GoName
	}
	ph := phraser{goNames: goNames, reg: in.Registry, t: t}
	personas := policy.Personas(in.Model)
	v := &Views{
		Service: in.Service, Personas: len(personas), Diagrams: map[string]*d2.Diagram{}, text: t,
	}

	var cells [][]Cell // by row, then persona
	slugs := map[string]bool{}
	add := func(op *policy.Operation, action, resourceType, label string) {
		row := Row{Label: label, Limited: op.ObligationText()}
		if len(op.Denies) > 0 {
			row.Diagram = "operations/" + uniqueSlug(action, slugs)
			v.Diagrams[row.Diagram] = decisionDiagram(ph, op, label)
			v.Names = append(v.Names, row.Diagram)
		}
		decided := make([]Cell, len(personas))
		for j := range personas {
			decided[j] = ph.cell(in.Decider.Decide(op, action, resourceType, &personas[j]))
		}
		v.Rows = append(v.Rows, row)
		cells = append(cells, decided)
	}
	for i := range in.Model.Operations {
		op := &in.Model.Operations[i]
		add(op, op.Action, "Target", strings.Join(op.Actions(), ", "))
	}
	for i := range in.Model.Items {
		item := &in.Model.Items[i]
		add(&item.Operation, cedarpolicy.ViewAction(item.Kind), cedarpolicy.ItemType(item.Kind),
			fmt.Sprintf(t.AccessItemFmt, item.Kind))
	}
	v.merge(personas, cells, ph)

	for _, op := range slices.Sorted(maps.Keys(in.Skipped)) {
		v.Skipped = append(v.Skipped, SkippedRow{Operation: op, Reasons: in.Skipped[op]})
	}
	return v
}

// merge groups the personas whose cells agree on every row into columns,
// in the order of each group's first persona, and fills the rows' cells.
func (v *Views) merge(personas []policy.Persona, cells [][]Cell, ph phraser) {
	index := map[string]int{} // a persona's cells, joined → its column
	var members [][]policy.Persona
	for j := range personas {
		var key strings.Builder
		for i := range cells {
			key.WriteString(cells[i][j].Text + "\x00")
		}
		k, ok := index[key.String()]
		if !ok {
			k = len(members)
			index[key.String()] = k
			members = append(members, nil)
			for i := range v.Rows {
				v.Rows[i].Cells = append(v.Rows[i].Cells, cells[i][j])
			}
		}
		members[k] = append(members[k], personas[j])
	}
	for _, group := range members {
		c := Column{Label: ph.personas(group)}
		for _, p := range group {
			c.Personas = append(c.Personas, p.ID)
		}
		v.Columns = append(v.Columns, c)
	}
}

// uniqueSlug is action's file slug, numbered past the ones taken.
func uniqueSlug(action string, taken map[string]bool) string {
	base := cmp.Or(extract.Slugify(action), "operation")
	slug := base
	for n := 2; taken[slug]; n++ {
		slug = fmt.Sprintf("%s-%d", base, n)
	}
	taken[slug] = true
	return slug
}
