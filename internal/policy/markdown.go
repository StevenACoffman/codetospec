package policy

import (
	"fmt"
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

	b.WriteString("\n## Checks\n")
	for i := range inv.Checks {
		writeCheck(&b, &inv.Checks[i], inv.Conditions)
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

// writeCheck renders one check: its definition, inputs (each declaring
// entry with its condition) and descriptions.
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
