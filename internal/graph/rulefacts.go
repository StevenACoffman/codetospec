package graph

import (
	"fmt"
	"strings"

	"codetospec/internal/extract"
	"codetospec/internal/locale"
	"codetospec/internal/mapper"
)

// ExtraExtractedBy is the Extra key naming the extractor that produced a
// mechanical rule (see extract.KindRule); absent on LLM rules.
const ExtraExtractedBy = "extracted_by"

// ExtraGroundingWidened is the Extra key listing the lines grounding added to
// a rule's citations ("path:a-b, …"), because the value it states sat just
// outside them (see grounding.Annotate).
const ExtraGroundingWidened = "grounding_widened"

// ValidateRuleFacts drops extract.KindRule facts that do not satisfy the rule
// fact contract, so an extractor bug cannot put a malformed rule in the
// graph. Facts of other kinds pass through unchanged.
//
// Requires: nothing.
// Ensures:  kept preserves the order of facts minus the invalid rule facts;
// rejected holds one "<fact id>: <reason>" per dropped fact.
func ValidateRuleFacts(facts []extract.Fact) ([]extract.Fact, []string) {
	var (
		kept     []extract.Fact
		rejected []string
	)
	routes := routesByID(facts)
	for _, f := range facts {
		if f.Kind != extract.KindRule {
			kept = append(kept, f)
			continue
		}
		reason := invalidRuleFact(&f)
		if route := f.Attrs[extract.AttrRoute]; reason == "" && route != "" &&
			routes[route] == nil {
			reason = fmt.Sprintf("unknown route %q", route)
		}
		if reason != "" {
			rejected = append(rejected, f.ID+": "+reason)
			continue
		}
		kept = append(kept, f)
	}
	return kept, rejected
}

// invalidRuleFact returns why f breaks the rule fact contract, or "".
func invalidRuleFact(f *extract.Fact) string {
	a := f.Attrs
	switch {
	case strings.TrimSpace(a[extract.AttrRequirement]) == "":
		return "empty requirement"
	case strings.TrimSpace(a[extract.AttrTitle]) == "":
		return "empty title"
	case a[extract.AttrSlug] == "" || extract.Slugify(a[extract.AttrSlug]) != a[extract.AttrSlug]:
		return fmt.Sprintf("slug %q is not a slug", a[extract.AttrSlug])
	case a[extract.AttrDomain] == "" || extract.Slugify(a[extract.AttrDomain]) != a[extract.AttrDomain]:
		return fmt.Sprintf("domain %q is not a slug", a[extract.AttrDomain])
	case !mapper.EARSKinds[a[extract.AttrEARS]]:
		return fmt.Sprintf("unknown EARS kind %q", a[extract.AttrEARS])
	case !mapper.Natures[a[extract.AttrNature]]:
		return fmt.Sprintf("unknown nature %q", a[extract.AttrNature])
	case f.Source.Path == "":
		return "no citation path"
	}
	if _, _, err := extract.ParseLines(f.Source.Lines); err != nil {
		return err.Error()
	}
	return ""
}

// routesByID indexes the route facts by fact ID.
func routesByID(facts []extract.Fact) map[string]*extract.Fact {
	routes := make(map[string]*extract.Fact)
	for i := range facts {
		if facts[i].Kind == extract.KindRoute {
			routes[facts[i].ID] = &facts[i]
		}
	}
	return routes
}

// mechanicalRuleNode turns a valid rule fact into a rule node; id is its
// final, collision-free node id, and routes resolves its optional route.
func mechanicalRuleNode(
	f *extract.Fact,
	id string,
	routes map[string]*extract.Fact,
	text *locale.Text,
) Node {
	edges := []Edge{{Type: EdgeBelongsTo, To: "domain." + f.Attrs[extract.AttrDomain]}}
	if route := routes[f.Attrs[extract.AttrRoute]]; route != nil {
		edges = append(edges, Edge{Type: EdgeExposedBy, To: EndpointID(*route)})
		sortEdges(edges)
	}
	return Node{
		ID:      id,
		Type:    TypeRule,
		Status:  StatusGenerated,
		Sources: []extract.Ref{f.Source},
		Edges:   edges,
		Title:   f.Attrs[extract.AttrTitle],
		Body:    text.Requirement + f.Attrs[extract.AttrRequirement],
		Extra: map[string]string{
			"ears":           f.Attrs[extract.AttrEARS],
			"nature":         f.Attrs[extract.AttrNature],
			"origin":         "explicit",
			ExtraExtractedBy: f.Origin,
		},
	}
}
