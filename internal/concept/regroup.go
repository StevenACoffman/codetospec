package concept

import (
	"maps"
	"slices"
	"strings"

	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/graph"
)

// Context is one confirmed model as the regrouping reads it: a business
// context, whose concepts' rules form one domain. Scope is the model's
// scope (the "<scope>" of a concept's qualified name), Summary what it is
// (the model's description, or its title), Imports the scopes of the
// models it imports.
type Context struct {
	Scope, Summary string
	Imports        []string
}

// contextSuffix sets a context's domain apart from a code domain of the
// same name that keeps rules.
const contextSuffix = "-context"

// Regroup applies Assign's placement (DOMAIN_PLAN.md recommendation 3,
// "only confirmed concepts reorganize the output"): each rule with a
// concept moves into the domain of its concept's context, its rules
// listed under their concepts (graph.Move), and depends on the contexts
// its model imports. A rule a person reviewed stays where it is: they own
// its file. A context's domain is named after its scope, with
// contextSuffix when a code domain of that name keeps rules. It returns
// the nodes and, per context domain, how many rules moved into it.
//
// Requires: nodes annotated by Assign; contexts with distinct scopes.
// Ensures: nodes is not modified; rule ids unchanged; deterministic.
func Regroup(nodes []graph.Node, contexts []Context, lang string) ([]graph.Node, map[string]int) {
	byScope := map[string]Context{}
	for _, c := range contexts {
		byScope[c.Scope] = c
	}
	scopeOf := map[string]string{} // rule id → its context's scope
	kept := map[string]bool{}      // domains keeping a rule
	for i := range nodes {
		n := &nodes[i]
		if n.Type != graph.TypeRule {
			continue
		}
		scope, _, placed := strings.Cut(n.Extra[ExtraConcept], ".")
		if _, known := byScope[scope]; placed && known && n.Status != graph.StatusReviewed {
			scopeOf[n.ID] = scope
			continue
		}
		kept[graph.RuleDomain(n)] = true
	}
	if len(scopeOf) == 0 {
		return nodes, nil
	}

	slugOf := map[string]string{}
	for _, scope := range slices.Sorted(maps.Values(scopeOf)) {
		slug := extract.Slugify(scope)
		if kept[slug] {
			slug += contextSuffix
		}
		slugOf[scope] = slug
	}
	moves := map[string]string{}
	moved := map[string]int{}
	for id, scope := range scopeOf {
		moves[id] = slugOf[scope]
		moved[slugOf[scope]]++
	}
	targets := make([]graph.Target, 0, len(slugOf))
	for _, scope := range slices.Sorted(maps.Keys(slugOf)) {
		c := byScope[scope]
		t := graph.Target{Slug: slugOf[scope], Summary: c.Summary, GroupBy: ExtraConcept}
		for _, imported := range c.Imports {
			if slug, ok := slugOf[imported]; ok {
				t.DependsOn = append(t.DependsOn, "domain."+slug)
			}
		}
		targets = append(targets, t)
	}
	return graph.Move(nodes, moves, targets, lang), moved
}
