package inherit_test

import (
	"reflect"
	"testing"

	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/graph"
	"github.com/StevenACoffman/codetospec/internal/inherit"
)

// rootName is the shared root the tests inherit from.
const rootName = "pkg"

// specRule is a rule of the shared spec citing path:lines.
func specRule(id, path, lines string) graph.Node {
	return graph.Node{
		ID:      id,
		Type:    graph.TypeRule,
		Title:   "Roster sync skips archived classes",
		Body:    "**Requirement (EARS)**: IF a class is archived, THEN the system shall skip it.",
		Sources: []extract.Ref{{Path: path, Lines: lines}},
		Extra: map[string]string{
			"ears": "unwanted", "nature": "business", "crosscheck": "supported",
			"origin": "explicit", "confidence": "0.90",
		},
	}
}

// reachOf is a reach fact of the shared root citing path:lines.
func reachOf(path, lines string) extract.Fact {
	return extract.Fact{
		Kind: extract.KindReach, ID: "reach." + path + lines,
		Source: extract.Ref{Path: path, Lines: lines, Root: rootName},
	}
}

func TestRulesInheritsReachedRules(t *testing.T) {
	spec := []graph.Node{specRule("rule.roster.skip-archived", "roster/sync.go", "10-20")}
	got := inherit.Rules(spec, rootName, []extract.Fact{reachOf("roster/sync.go", "5-30")})
	want := []extract.Fact{{
		Kind: extract.KindRule,
		ID:   "rule.inherited.pkg.roster.skip-archived",
		Attrs: map[string]string{
			extract.AttrSlug:          "skip-archived",
			extract.AttrTitle:         "Roster sync skips archived classes",
			extract.AttrRequirement:   "IF a class is archived, THEN the system shall skip it.",
			extract.AttrEARS:          "unwanted",
			extract.AttrNature:        "business",
			extract.AttrDomain:        "shared-roster",
			extract.AttrInheritedFrom: rootName,
			extract.AttrCrosscheck:    "supported",
			extract.AttrOrigin:        "explicit",
			extract.AttrConfidence:    "0.90",
		},
		Source:    extract.Ref{Path: "roster/sync.go", Lines: "10-20", Root: rootName},
		Origin:    "inherit",
		Certainty: "static",
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
}

// TestRulesSkips: what is not reached, or not the spec's own single-citation
// LLM rule, is not inherited.
func TestRulesSkips(t *testing.T) {
	reached := []extract.Fact{reachOf("roster/sync.go", "5-30")}
	mechanical := specRule("rule.roster.mech", "roster/sync.go", "10-20")
	mechanical.Extra[graph.ExtraExtractedBy] = "go"
	inherited := specRule("rule.roster.again", "roster/sync.go", "10-20")
	inherited.Extra[graph.ExtraInheritedFrom] = "other"
	twoCitations := specRule("rule.roster.two", "roster/sync.go", "10-20")
	twoCitations.Sources = append(
		twoCitations.Sources,
		extract.Ref{Path: "roster/x.go", Lines: "1-2"},
	)
	cases := map[string]struct {
		rule  graph.Node
		reach []extract.Fact
	}{
		"citation outside reached lines": {
			specRule("rule.roster.far", "roster/sync.go", "40-50"), reached,
		},
		"citation in an unreached file": {
			specRule("rule.roster.other", "roster/other.go", "10-20"), reached,
		},
		"reach of another root": {
			specRule("rule.roster.root", "roster/sync.go", "10-20"),
			[]extract.Fact{{
				Kind:   extract.KindReach,
				Source: extract.Ref{Path: "roster/sync.go", Lines: "1-99", Root: "webapp"},
			}},
		},
		"mechanical rule":           {mechanical, reached},
		"rule inherited elsewhere":  {inherited, reached},
		"rule citing several spans": {twoCitations, reached},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := inherit.Rules([]graph.Node{tc.rule}, rootName, tc.reach); len(got) != 0 {
				t.Fatalf("inherited %+v", got)
			}
		})
	}
}
