package graph_test

import (
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/graph"
	"github.com/StevenACoffman/codetospec/internal/reducer"
)

// stableRule is an LLM rule node of domain billing citing lines of calc.php.
func stableRule(id, requirement, lines string) graph.Node {
	return graph.Node{
		ID:      id,
		Type:    graph.TypeRule,
		Body:    "**Requirement (EARS)**: " + requirement,
		Sources: []extract.Ref{{Path: "calc.php", Lines: lines}},
		Edges:   []graph.Edge{{Type: graph.EdgeBelongsTo, To: "domain." + billingDomain}},
	}
}

// Requirements the stable-id tests reword.
const (
	roundReq   = "The system shall round every invoice amount to the cent."
	reworded   = "The system shall round each invoice amount to the cent."
	refundReq  = "The system shall refund the unused days of a cancelled plan."
	unrelated  = "The system shall email the customer a receipt after payment."
	roundLines = "10-20"
)

// TestMatchRules: a reworded rule on the same code keeps its old id; a
// same-id rule on the same code is the same rule whatever its wording; an
// identical requirement matches wherever its code moved; another domain,
// other code with other words, or a mechanical rule never matches.
func TestMatchRules(t *testing.T) {
	mechanical := stableRule("rule.billing.mech", roundReq, roundLines)
	mechanical.Extra = map[string]string{graph.ExtraExtractedBy: webappOrigin}
	otherDomain := stableRule("rule.ops.round", roundReq, roundLines)
	otherDomain.Edges = []graph.Edge{{Type: graph.EdgeBelongsTo, To: "domain.ops"}}

	cases := map[string]struct {
		prior, current []graph.Node
		want           map[string]string
	}{
		"reworded, renamed": {
			prior:   []graph.Node{stableRule(roundRule, roundReq, roundLines)},
			current: []graph.Node{stableRule(centsRule, reworded, "12-20")},
			want:    map[string]string{centsRule: roundRule},
		},
		"same id, rewritten": {
			prior:   []graph.Node{stableRule(roundRule, roundReq, roundLines)},
			current: []graph.Node{stableRule(roundRule, unrelated, roundLines)},
			want:    map[string]string{roundRule: roundRule},
		},
		"same requirement, code moved": {
			prior:   []graph.Node{stableRule(roundRule, roundReq, roundLines)},
			current: []graph.Node{stableRule(centsRule, roundReq, "40-50")},
			want:    map[string]string{centsRule: roundRule},
		},
		"other code, other words": {
			prior:   []graph.Node{stableRule(roundRule, roundReq, roundLines)},
			current: []graph.Node{stableRule("rule.billing.refund", refundReq, "40-50")},
			want:    map[string]string{},
		},
		"other domain": {
			prior:   []graph.Node{otherDomain},
			current: []graph.Node{stableRule(centsRule, roundReq, roundLines)},
			want:    map[string]string{},
		},
		"mechanical": {
			prior:   []graph.Node{mechanical},
			current: []graph.Node{stableRule(centsRule, roundReq, roundLines)},
			want:    map[string]string{},
		},
		"one to one, closest first": {
			prior: []graph.Node{stableRule(roundRule, roundReq, roundLines)},
			current: []graph.Node{
				stableRule("rule.billing.a", reworded, roundLines),
				stableRule("rule.billing.b", roundReq, roundLines),
			},
			want: map[string]string{"rule.billing.b": roundRule},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := graph.MatchRules(tc.prior, tc.current); !maps.Equal(got, tc.want) {
				t.Errorf("MatchRules = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestBuildKeepsPriorIDs: Build gives a reworded rule its prior id, links
// it under that id from its domain, gives a new rule whose slug a retired
// rule held the next suffix, and lets a mechanical rule take its id before
// an LLM rule of the same slug.
func TestBuildKeepsPriorIDs(t *testing.T) {
	rule := func(slug, requirement, lines string) reducer.Rule {
		return reducer.Rule{
			Slug: slug, Title: slug, EarsKind: "ubiquitous", Requirement: requirement,
			Citations: []extract.Ref{{Path: "calc.php", Lines: lines}},
			Nature:    natureBusiness, Origin: originExplicit, Confidence: 0.9,
		}
	}
	reduced := []reducer.Output{{
		Domain: "role",
		Rules: []reducer.Rule{
			rule("cents", reworded, roundLines),                   // was "round"
			rule("refund", refundReq, "40-50"),                    // "refund" retired
			rule("authz-mutation-addcoachrole", unrelated, "1-5"), // a mechanical slug
		},
	}}
	prior := []graph.Node{
		stableRule("rule.role.round", roundReq, roundLines),
		stableRule("rule.role.refund", "The system shall refund a purchase within a day.", "60-70"),
	}
	for i := range prior {
		prior[i].Edges = []graph.Edge{{Type: graph.EdgeBelongsTo, To: "domain.role"}}
	}

	nodes := graph.Build(
		[]extract.Fact{ruleFact("rule.x")},
		reduced,
		extract.DomainResolver{},
		"en",
		prior,
	)

	ids := nodeIDs(nodes)
	for _, want := range []string{
		"rule.role.round",
		"rule.role.refund-2",
		mechRule, // the mechanical rule
		"rule.role.authz-mutation-addcoachrole-2", // the LLM rule
	} {
		if !slices.Contains(ids, want) {
			t.Errorf("no node %s; got %v", want, ids)
		}
	}
	for _, n := range nodes {
		if n.ID == mechRule &&
			n.Extra[graph.ExtraExtractedBy] == "" {
			t.Error("the LLM rule took the mechanical rule's id")
		}
		if n.ID == "domain.role" && !strings.Contains(n.Body, "(../rules/role.round.md)") {
			t.Errorf("domain body does not link the kept id:\n%s", n.Body)
		}
	}
}
