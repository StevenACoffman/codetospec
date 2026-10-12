package graph_test

import (
	"strings"
	"testing"

	"codetospec/internal/extract"
	"codetospec/internal/graph"
)

// ruleFact is a valid mechanical rule fact; tests override single fields.
func ruleFact(id string) extract.Fact {
	return extract.Fact{
		Kind: extract.KindRule,
		ID:   id,
		Attrs: map[string]string{
			extract.AttrSlug:        "authz-mutation-addcoachrole",
			extract.AttrTitle:       "Authorization: Mutation.addCoachRole",
			extract.AttrRequirement: "IF the caller is not the user `kaid`, THEN the system shall reject Mutation.addCoachRole.",
			extract.AttrEARS:        "unwanted",
			extract.AttrNature:      "business",
			extract.AttrDomain:      "role",
		},
		Source: extract.Ref{Path: "resolvers/add_coach_role.go", Lines: "37-39"},
		Origin: "webapp",
	}
}

func TestValidateRuleFacts(t *testing.T) {
	cases := map[string]struct {
		mutate     func(f *extract.Fact)
		wantReason string // "" when the fact is valid
	}{
		"valid": {mutate: func(*extract.Fact) {}},
		"empty requirement": {
			mutate:     func(f *extract.Fact) { f.Attrs[extract.AttrRequirement] = " " },
			wantReason: "empty requirement",
		},
		"empty title": {
			mutate:     func(f *extract.Fact) { f.Attrs[extract.AttrTitle] = "" },
			wantReason: "empty title",
		},
		"bad slug": {
			mutate:     func(f *extract.Fact) { f.Attrs[extract.AttrSlug] = "Not A Slug" },
			wantReason: "is not a slug",
		},
		"bad domain": {
			mutate:     func(f *extract.Fact) { f.Attrs[extract.AttrDomain] = "" },
			wantReason: "domain",
		},
		"unknown ears": {
			mutate:     func(f *extract.Fact) { f.Attrs[extract.AttrEARS] = "sometimes" },
			wantReason: "EARS",
		},
		"unknown nature": {
			mutate:     func(f *extract.Fact) { f.Attrs[extract.AttrNature] = "magic" },
			wantReason: "nature",
		},
		"no path": {
			mutate:     func(f *extract.Fact) { f.Source.Path = "" },
			wantReason: "path",
		},
		"bad lines": {
			mutate:     func(f *extract.Fact) { f.Source.Lines = "37" },
			wantReason: "line range",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := ruleFact("rule.x")
			tc.mutate(&f)
			other := extract.Fact{Kind: "route", ID: "route.x"}
			kept, rejected := graph.ValidateRuleFacts([]extract.Fact{other, f})
			if tc.wantReason == "" {
				if len(kept) != 2 || len(rejected) != 0 {
					t.Fatalf("valid fact: kept=%d rejected=%v", len(kept), rejected)
				}
				return
			}
			if len(kept) != 1 || kept[0].ID != "route.x" {
				t.Fatalf("kept = %v, want only the non-rule fact", kept)
			}
			if len(rejected) != 1 || !strings.Contains(rejected[0], tc.wantReason) {
				t.Fatalf("rejected = %v, want a reason containing %q", rejected, tc.wantReason)
			}
		})
	}
}

func TestBuildMechanicalRules(t *testing.T) {
	second := ruleFact("rule.y") // same slug and domain: must get a distinct id
	nodes := graph.Build(
		[]extract.Fact{ruleFact("rule.x"), second},
		nil,
		extract.DomainResolver{},
		"en",
	)

	byID := make(map[string]graph.Node)
	for _, n := range nodes {
		byID[n.ID] = n
	}
	first, ok := byID["rule.role.authz-mutation-addcoachrole"]
	if !ok {
		t.Fatalf("no mechanical rule node; got %v", nodeIDs(nodes))
	}
	if _, renumbered := byID["rule.role.authz-mutation-addcoachrole-2"]; !renumbered {
		t.Fatalf("colliding slug not renumbered; got %v", nodeIDs(nodes))
	}
	if first.Extra[graph.ExtraExtractedBy] != "webapp" || first.Extra["origin"] != "explicit" {
		t.Errorf("extra = %v, want extracted_by webapp and origin explicit", first.Extra)
	}
	if got := graph.RuleRequirement(first.Body); !strings.Contains(got, "Mutation.addCoachRole") {
		t.Errorf("requirement does not parse back: %q", got)
	}
	domain, ok := byID["domain.role"]
	if !ok {
		t.Fatalf("no domain node for a mechanical-only domain; got %v", nodeIDs(nodes))
	}
	if !strings.Contains(domain.Body, "(../rules/role.authz-mutation-addcoachrole.md)") {
		t.Errorf("domain body does not link the rule:\n%s", domain.Body)
	}
}

func nodeIDs(nodes []graph.Node) []string {
	ids := make([]string, len(nodes))
	for i, n := range nodes {
		ids[i] = n.ID
	}
	return ids
}
