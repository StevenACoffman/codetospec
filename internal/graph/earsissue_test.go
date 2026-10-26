package graph_test

import (
	"testing"

	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/graph"
)

// TestBuildFlagsEARSIssues: a rule whose requirement does not have its
// kind's form carries ears_issue, an LLM rule and an inherited one alike;
// a well-formed rule carries none.
func TestBuildFlagsEARSIssues(t *testing.T) {
	reduced := fixtureReduced()
	malformed := reduced[0].Rules[0]
	malformed.Slug = "malformed"
	malformed.Requirement = "Le systeme doit x et doit y." // event kind, two modals
	reduced[0].Rules = append(reduced[0].Rules, malformed)
	inherited := ruleFact("rule.inherited.pkg.x")
	inherited.Attrs[extract.AttrInheritedFrom] = sharedRoot
	inherited.Attrs[extract.AttrRequirement] = notEARS
	facts, rejected := graph.ValidateRuleFacts(append(fixtureFacts(), inherited))
	if len(rejected) != 0 {
		t.Fatalf("rejected = %v", rejected)
	}

	nodes := graph.Build(facts, reduced, extract.DomainResolver{}, "fr", nil)

	cases := map[string]struct {
		id        string
		wantIssue bool
	}{
		"well formed":   {prorataRule, false},
		"two modals":    {"rule.services.malformed", true},
		"inherited":     {mechRule, true},
		"other domains": {"rule.http.activation-endpoint", false},
	}
	byID := map[string]graph.Node{}
	for _, n := range nodes {
		byID[n.ID] = n
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			n, ok := byID[tc.id]
			if !ok {
				t.Fatalf("no node %s; got %v", tc.id, nodeIDs(nodes))
			}
			if got := n.Extra[graph.ExtraEARSIssue] != ""; got != tc.wantIssue {
				t.Errorf(
					"%s ears_issue = %q, want set=%v",
					tc.id,
					n.Extra[graph.ExtraEARSIssue],
					tc.wantIssue,
				)
			}
		})
	}
}
