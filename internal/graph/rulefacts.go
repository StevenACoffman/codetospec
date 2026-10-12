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
	for _, f := range facts {
		if f.Kind != extract.KindRule {
			kept = append(kept, f)
			continue
		}
		if reason := invalidRuleFact(&f); reason != "" {
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

// mechanicalRuleNode turns a valid rule fact into a rule node; id is its
// final, collision-free node id.
func mechanicalRuleNode(f *extract.Fact, id string, text *locale.Text) Node {
	return Node{
		ID:      id,
		Type:    TypeRule,
		Status:  StatusGenerated,
		Sources: []extract.Ref{f.Source},
		Edges:   []Edge{{Type: EdgeBelongsTo, To: "domain." + f.Attrs[extract.AttrDomain]}},
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
