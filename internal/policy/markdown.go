package policy

import (
	"cmp"
	"fmt"
	"strconv"
	"strings"

	"github.com/StevenACoffman/codetospec/internal/extract"
)

// Markdown renders the inventory for a person: the enforcement points and
// their guards, each check and what it reads, then the gaps (undeclared
// inputs, registry entries no check reads, open, todo and unguarded
// handlers) and the limits the inventory is complete within.
//
// Ensures: deterministic for a given inventory.
func (inv *Inventory) Markdown() []byte {
	var b strings.Builder
	b.WriteString("# Policy inventory\n\n")
	fmt.Fprintf(&b, "%d enforcement points, %d checks, %d open, %d todo, %d unguarded; "+
		"inputs looked for %d calls deep.\n", len(inv.Endpoints), len(inv.Checks),
		len(inv.Open), len(inv.Todo), len(inv.Unguarded), inv.Depth)

	b.WriteString(
		"\n## Enforcement points\n\n| Endpoint | Guard | Checks called |\n|---|---|---|\n",
	)
	for _, e := range inv.Endpoints {
		for i := range e.Guards {
			fmt.Fprintf(
				&b,
				"| %s | %s | %s |\n",
				e.Endpoint,
				e.Guards[i].RuleID,
				callsText(&e.Guards[i]),
			)
		}
	}

	writeAccess(&b, inv.Access)
	b.WriteString("\n## Checks\n")
	for i := range inv.Checks {
		writeCheck(&b, &inv.Checks[i], inv.Conditions)
	}

	if len(inv.Joined) > 0 {
		b.WriteString("\n## Checks of other services\n\n")
		for _, j := range inv.Joined {
			text := "pinned as " + j.Answer
			if j.Reason != "" {
				text = "not pinned: " + j.Reason
			}
			fmt.Fprintf(&b, "- %s, via %s: %s\n", j.Check, j.Via, text)
		}
	}
	writeList(&b, "Undeclared inputs", inv.Undeclared)
	writeList(&b, "Registry entries no check reads", inv.Unread)
	writeGuards(&b, "Open access", inv.Open)
	writeGuards(&b, "Unchecked (marked todo)", inv.Todo)
	if len(inv.Unguarded) > 0 {
		b.WriteString("\n## Unguarded handlers\n\n")
		for _, u := range inv.Unguarded {
			fmt.Fprintf(&b, "- %s (%s)\n", u.Operation, refText(u.Source))
		}
	}
	fmt.Fprintf(&b, "\n## Limits\n\n"+
		"- Complete relative to the checks the extractor recognizes; a handler with none is listed as unguarded.\n"+
		"- Inputs are looked for %d calls deep, interface dispatch left out.\n"+
		"- Reads through maps, reflection or encoded blobs are not field reads.\n"+
		"- \"Declared\" means the same source as a registry entry, not the same meaning.\n", inv.Depth)
	return []byte(b.String())
}

// writeAccess renders who can do what, by concept: each operation and
// the statement of each of its guards, or why it has none.
func writeAccess(b *strings.Builder, access []ConceptAccess) {
	if len(access) == 0 {
		return
	}
	b.WriteString("\n## Who can do what\n\nEvery guard an operation lists must allow.\n")
	for _, c := range access {
		fmt.Fprintf(b, "\n### %s\n\n", cmp.Or(c.Concept, "No concept"))
		for _, op := range c.Operations {
			fmt.Fprintf(b, "- %s\n", op.Operation)
			for _, g := range op.Guards {
				text := g.Statement
				switch {
				case text == "":
					text = g.Reason
				case g.Reason != "":
					text += " (" + g.Reason + ")"
				}
				if g.Undecided {
					text = "undecided: " + text
				}
				fmt.Fprintf(b, "  - %s: %s\n", cmp.Or(strings.Join(g.Checks, ", "), g.Rule), text)
			}
		}
	}
}

// writeCheck renders one check: its citation, inputs (each declaring
// entry with its condition), definition and descriptions.
func writeCheck(b *strings.Builder, c *Check, conditions map[string]string) {
	fmt.Fprintf(b, "\n### %s\n\n%s", c.Name, c.Form)
	if c.Source.Path != "" {
		fmt.Fprintf(b, ", %s", refText(c.Source))
	}
	b.WriteString("\n")
	if len(c.Inputs) > 0 {
		b.WriteString("\n| Kind | Input | Detail | Declared by |\n|---|---|---|---|\n")
		for _, in := range c.Inputs {
			declared := in.DeclaredBy
			if cond := conditions[declared]; cond != "" {
				declared += ", " + cond
			}
			fmt.Fprintf(b, "| %s | %s | %s | %s |\n", in.Kind, in.Name, in.Detail, declared)
		}
	}
	writeDefinition(b, c)
	for _, d := range c.Descriptions {
		fmt.Fprintf(b, "\n- %s: %s", d.RuleID, d.Title)
		if d.Crosscheck != "" {
			fmt.Fprintf(b, " (crosscheck: %s)", d.Crosscheck)
		}
	}
	if len(c.Descriptions) > 0 {
		b.WriteString("\n")
	}
}

// writeDefinition renders a check's definition: its pin's state, the
// code's allowing paths, the pin's leaves with their grounding and the
// coverage of one by the other; nothing for a check with neither paths
// nor pin.
func writeDefinition(b *strings.Builder, c *Check) {
	d := &c.Definition
	if !d.Pinned && !d.Unpinnable && !d.NotAnalyzable && len(d.Paths) == 0 {
		return
	}
	if d.Statement != "" {
		fmt.Fprintf(b, "\n> %s\n", d.Statement)
	}
	fmt.Fprintf(b, "\nDefinition: %s.\n", definitionState(d))
	writePaths(b, c)
	if len(d.Leaves) > 0 {
		leaves := make([]string, len(d.Leaves))
		for i, l := range d.Leaves {
			leaves[i] = strings.TrimSpace(l.Means+" "+l.Operand) + " (" + l.Grounding + ")"
		}
		fmt.Fprintf(b, "\nLeaves: %s.\n", strings.Join(leaves, ", "))
	}
	if d.Coverage != nil {
		writeCoverage(b, d.Coverage)
	}
	if len(d.GlossaryMissing) > 0 {
		fmt.Fprintf(b, "\nNot in the registry: %s.\n", strings.Join(d.GlossaryMissing, ", "))
	}
	if d.Comparison != nil {
		fmt.Fprintf(b, "\nSecond reader: %s.\n", comparisonText(d.Comparison))
	}
	for _, q := range d.Questions {
		fmt.Fprintf(b, "\nQuestion %s (%s): %s\n", q.ID, q.Kind, q.Detail)
	}
}

// comparisonText says how a second reader's pin compares.
func comparisonText(c *Comparison) string {
	switch {
	case c.Equal:
		return "equivalent"
	case c.Missing != "":
		return "no pin in the " + c.Missing + " file"
	case c.Reason != "":
		return "different: " + c.Reason
	}
	truths := make([]string, len(c.Counterexample))
	for i, l := range c.Counterexample {
		truths[i] = fmt.Sprintf("`%s` %t", l.Leaf, l.Truth)
	}
	return "different: only the " + c.AllowedBy + " pin allows when " + strings.Join(truths, ", ")
}

// definitionState says whether d is pinned, approximate and stale.
func definitionState(d *Definition) string {
	var state []string
	switch {
	case d.Pinned:
		state = append(state, "pinned")
	case d.Unpinnable:
		state = append(state, "unpinnable ("+d.UnpinnableReason+")")
	default:
		state = append(state, "not pinned")
	}
	if d.Approx {
		state = append(state, "approximate: stricter than the code")
	}
	if d.Stale {
		state = append(state, "stale: the code changed since the pin")
	}
	return strings.Join(state, "; ")
}

// writePaths renders check c's allowing paths, or why they weren't read.
func writePaths(b *strings.Builder, c *Check) {
	d := &c.Definition
	if d.NotAnalyzable {
		fmt.Fprintf(b, "\nAllowing paths: not analyzable (%s).\n", d.NotAnalyzableReason)
		return
	}
	if len(d.Paths) == 0 {
		return
	}
	b.WriteString("\nAllowing paths:\n\n")
	for i := range d.Paths {
		p := &d.Paths[i]
		cond := "always"
		if p.Condition != "" {
			cond = "`" + p.Condition + "`"
		}
		fmt.Fprintf(b, "%d. %s (%s)\n", i+1, cond, pathRef(c, p))
	}
}

// writeCoverage renders the pin's alternatives and what matches nothing.
func writeCoverage(b *strings.Builder, cov *Coverage) {
	fmt.Fprintf(b, "\nPin alternatives: %s.\n", strings.Join(cov.Parts, "; "))
	if cov.Covered {
		b.WriteString("Coverage: every path matches an alternative and every alternative a path.\n")
	}
	if n := len(cov.UncoveredPaths); n > 0 {
		fmt.Fprintf(b, "Coverage: %s %s %s no alternative.\n", counted(n, "path", "paths"),
			indexes(cov.UncoveredPaths), counted(n, "matches", "match"))
	}
	if n := len(cov.UnmatchedParts); n > 0 {
		fmt.Fprintf(b, "Coverage: %s %s %s no path.\n", counted(n, "alternative", "alternatives"),
			indexes(cov.UnmatchedParts), counted(n, "matches", "match"))
	}
}

// counted is one when n is 1, else many.
func counted(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// callsText lists a guard's check calls with their operands.
func callsText(g *Guard) string {
	if len(g.Calls) == 0 {
		if g.DelegatesTo != "" {
			return "checked as " + g.DelegatesTo
		}
		return "none named"
	}
	parts := make([]string, 0, len(g.Calls))
	for i, c := range g.Calls {
		var ops []string
		for _, op := range []string{c.Capability, c.Matcher, c.MatcherArg, c.Subject} {
			if op != "" {
				ops = append(ops, op)
			}
		}
		if c.Rate != nil {
			ops = append(ops, strings.TrimSpace(c.Rate.Key+" "+c.Rate.Count+" "+c.Rate.Window))
		}
		text := c.Check + " [" + g.Kinds[i] + "]"
		if len(ops) > 0 {
			text += " (" + strings.Join(ops, ", ") + ")"
		}
		parts = append(parts, text)
	}
	if g.Scope != "" {
		parts = append(parts, "drops one "+g.Scope)
	}
	return strings.Join(parts, "; ")
}

// writeList renders a titled list of names, nothing when empty.
func writeList(b *strings.Builder, title string, names []string) {
	if len(names) == 0 {
		return
	}
	fmt.Fprintf(b, "\n## %s\n\n", title)
	for _, n := range names {
		fmt.Fprintf(b, "- %s\n", n)
	}
}

// writeGuards renders a titled list of guards, nothing when empty.
func writeGuards(b *strings.Builder, title string, guards []Guard) {
	if len(guards) == 0 {
		return
	}
	fmt.Fprintf(b, "\n## %s\n\n", title)
	for i := range guards {
		fmt.Fprintf(b, "- %s (%s)\n", guards[i].RuleID, refText(guards[i].Source))
	}
}

// refText is a citation as path:lines, with its root.
func refText(r extract.Ref) string {
	return r.File() + ":" + r.Lines
}

// pathRef is a path's citation in check c's file, for the markdown.
func pathRef(c *Check, p *Path) string {
	return c.Source.File() + ":" + p.Lines
}

// indexes renders indexes from 1, comma-separated.
func indexes(is []int) string {
	out := make([]string, len(is))
	for k, i := range is {
		out[k] = strconv.Itoa(i + 1)
	}
	return strings.Join(out, ", ")
}
