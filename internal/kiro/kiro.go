// Package kiro exports a graph's rules as Kiro specs, so a team can carry
// a spec recovered from existing code into Kiro's spec workflow.
//
// The format is Kiro's convention, not a schema: one directory per spec
// under .kiro/specs/<name>/, with requirements.md — Introduction,
// Glossary (bold terms), then "### Requirement N: <title>" each with
// "#### Acceptance Criteria" in EARS ("WHEN [condition/event] THE SYSTEM
// SHALL [expected behavior]", kiro.dev/docs/specs/concepts) — and
// design.md, whose "Correctness Properties" each end "**Validates:
// Requirements N.M**". Modeled on the spec Kiro generated in
// kirodotdev/Kiro at bfe7ff3 (.kiro/specs/github-issue-automation). A
// later Kiro may change it; this package is the one place that knows it.
//
// One spec per domain; one requirement per rule, its EARS sentence the
// one acceptance criterion (N.1), in Kiro's capitals; its acceptance
// criteria become the correctness property validating N.1. No user story
// (the code states none, and one is not invented) and no tasks.md (the
// code already exists). The rule id, crosscheck verdict and cited code go
// in an HTML comment, which Kiro does not render. Rules the crosscheck
// refuted are left out.
package kiro

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/StevenACoffman/codetospec/internal/crosscheck"
	"github.com/StevenACoffman/codetospec/internal/ears"
	"github.com/StevenACoffman/codetospec/internal/graph"
)

// Directory is where the specs go, relative to the export root: Kiro's
// layout.
const Directory = ".kiro/specs"

// unassigned names the spec of rules that belong to no domain.
const unassigned = "unassigned"

// Spec is the exported specs: file contents by path relative to the
// export root, the number of requirements written, and the rules left
// out because the crosscheck refuted them.
type Spec struct {
	Files        map[string][]byte
	Requirements int
	Refuted      []string // rule ids, sorted
}

// requirement is one rule as a Kiro requirement.
type requirement struct {
	rule     *graph.Node
	criteria []string // the rule's acceptance criteria, unnumbered
	actor    string   // the actor its sentence names; "" for the system
}

// Export renders the rules of nodes as one Kiro spec per domain, rules in
// id order; actors describes the actors rules name (render.LoadActors).
//
// Requires: nodes as render.LoadDir reads them.
// Ensures:  requirements.md for each domain with a rule not refuted, and
// design.md when one of them has acceptance criteria; the same input
// gives the same bytes.
func Export(nodes []graph.Node, actors map[string]string) Spec {
	summaries := map[string]string{}
	byDomain := map[string][]requirement{}
	export := Spec{Files: map[string][]byte{}}
	for i := range nodes {
		n := &nodes[i]
		switch {
		case n.Type == graph.TypeDomain:
			summaries[strings.TrimPrefix(n.ID, graph.TypeDomain+".")] = summary(n.Body)
		case n.Type != graph.TypeRule:
		case n.Extra["crosscheck"] == crosscheck.VerdictUnsupported:
			export.Refuted = append(export.Refuted, n.ID)
		default:
			domain := cmp.Or(graph.RuleDomain(n), unassigned)
			byDomain[domain] = append(byDomain[domain], newRequirement(n))
		}
	}
	slices.Sort(export.Refuted)
	for _, domain := range slices.Sorted(maps.Keys(byDomain)) {
		reqs := byDomain[domain]
		slices.SortFunc(
			reqs,
			func(a, b requirement) int { return cmp.Compare(a.rule.ID, b.rule.ID) },
		)
		dir := Directory + "/" + domain + "/"
		export.Files[dir+"requirements.md"] = requirementsDoc(
			domain,
			summaries[domain],
			reqs,
			actors,
		)
		if design := designDoc(domain, summaries[domain], reqs); design != nil {
			export.Files[dir+"design.md"] = design
		}
		export.Requirements += len(reqs)
	}
	return export
}

// newRequirement reads what a requirement needs from a rule node.
func newRequirement(n *graph.Node) requirement {
	r := requirement{rule: n}
	for _, line := range graph.RuleCriteria(n.Body) {
		r.criteria = append(r.criteria, graph.CriterionText(line))
	}
	if c, ok := graph.RuleComponents(n); ok {
		r.actor = c.System
	}
	return r
}

// requirementsDoc is a domain's requirements.md.
func requirementsDoc(
	domain, summary string,
	reqs []requirement,
	actors map[string]string,
) []byte {
	var b strings.Builder
	b.WriteString("# Requirements Document\n\n## Introduction\n\n")
	if summary != "" {
		b.WriteString(summary + "\n\n")
	}
	fmt.Fprintf(&b, "Recovered by codetospec from the code of the `%s` domain: each "+
		"requirement states behavior the cited code implements.\n\n", domain)

	b.WriteString("## Glossary\n\n")
	fmt.Fprintf(&b, "- **System**: the code of the `%s` domain.\n", domain)
	var named []string
	for _, r := range reqs {
		if r.actor != "" && !slices.Contains(named, r.actor) {
			named = append(named, r.actor)
		}
	}
	slices.Sort(named)
	for _, actor := range named {
		description := cmp.Or(actors[actor], "Named by these requirements; the glossary "+
			"does not describe it.")
		fmt.Fprintf(&b, "- **%s**: %s\n", ears.ActorName(actor), oneLine(description))
	}

	b.WriteString("\n## Requirements\n")
	for i, r := range reqs {
		fmt.Fprintf(&b, "\n### Requirement %d: %s\n\n", i+1, oneLine(r.rule.Title))
		fmt.Fprintf(&b, "<!-- codetospec: %s · crosscheck: %s · code: %s -->\n\n",
			r.rule.ID, cmp.Or(r.rule.Extra["crosscheck"], "unchecked"), citations(r.rule))
		fmt.Fprintf(&b, "#### Acceptance Criteria\n\n1. %s\n", criterion(r.rule))
	}
	return []byte(b.String())
}

// designDoc is a domain's design.md: a correctness property per rule
// with acceptance criteria; nil when none has any.
func designDoc(domain, summary string, reqs []requirement) []byte {
	var properties strings.Builder
	n := 0
	for i, r := range reqs {
		if len(r.criteria) == 0 {
			continue
		}
		n++
		fmt.Fprintf(&properties, "\n### Property %d: %s\n\n", n, oneLine(r.rule.Title))
		for _, c := range r.criteria {
			fmt.Fprintf(&properties, "- %s\n", oneLine(c))
		}
		fmt.Fprintf(&properties, "\n**Validates: Requirements %d.1**\n", i+1)
	}
	if n == 0 {
		return nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# Design Document: %s\n\n## Overview\n\n", domain)
	if summary != "" {
		b.WriteString(summary + "\n\n")
	}
	b.WriteString("The design is the existing code; the properties below are the " +
		"acceptance criteria codetospec recovered for each requirement.\n\n" +
		"## Correctness Properties\n")
	b.WriteString(properties.String())
	return []byte(b.String())
}

// criterion is a rule's EARS sentence as a Kiro acceptance criterion:
// in Kiro's capitals when it parses, as written otherwise; without the
// final period, as Kiro writes them.
func criterion(rule *graph.Node) string {
	sentence := graph.RuleRequirement(rule.Body)
	if capitals, err := ears.InCapitals(sentence); err == nil {
		sentence = capitals
	}
	return strings.TrimSuffix(oneLine(sentence), ".")
}

// citations are a rule's cited spans, "path:lines" comma-separated.
func citations(rule *graph.Node) string {
	refs := make([]string, len(rule.Sources))
	for i, src := range rule.Sources {
		refs[i] = src.File() + ":" + src.Lines
	}
	return strings.Join(refs, ", ")
}

// summary is a domain node's summary: the paragraph after its "# "
// title, as LoadDir reads the body; "" when the body has none (a heading
// or a list comes first).
func summary(body string) string {
	paragraphs := strings.Split(strings.TrimSpace(body), "\n\n")
	if len(paragraphs) > 0 && strings.HasPrefix(paragraphs[0], "# ") {
		paragraphs = paragraphs[1:]
	}
	if len(paragraphs) == 0 || strings.HasPrefix(paragraphs[0], "#") ||
		strings.HasPrefix(paragraphs[0], "- ") {
		return ""
	}
	return oneLine(paragraphs[0])
}

// oneLine joins text's lines with spaces.
func oneLine(text string) string {
	return strings.Join(strings.Fields(text), " ")
}
