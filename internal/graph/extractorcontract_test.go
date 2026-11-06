package graph_test

import (
	"encoding/json"
	"io/fs"
	"os"
	"testing"

	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/graph"
)

// TestExtractorRuleFactsPass: every rule fact of the webapp extractor's
// goldens passes ValidateRuleFacts. The extractor is another module and
// shares no code with the core, only the rule-fact contract, and the core
// drops a rule fact that breaks it (ears.Check included) without failing
// the run. A template change shows up in the goldens (UPDATE_GOLDEN=1 in
// extractors/webapp); a stricter check here then fails this test instead
// of silently deleting authorization rules from real specs. The goldens
// of the service carry conditions in the compact notation, which only
// package condition parses on this side: a notation change on either side
// fails here too.
func TestExtractorRuleFactsPass(t *testing.T) {
	testdata := os.DirFS("../../extractors/webapp/testdata")
	goldens, err := fs.Glob(testdata, "golden*.json")
	if err != nil || len(goldens) == 0 {
		t.Fatalf("no webapp extractor golden (%v)", err)
	}
	for _, name := range goldens {
		t.Run(name, func(t *testing.T) {
			facts := goldenFacts(t, testdata, name)
			rules, conditions := countRules(facts)
			if conditions == 0 {
				t.Error("no rule fact has a condition: the golden no longer exercises the notation")
			}
			if rules == 0 {
				t.Fatal("no rule fact: the golden no longer exercises the contract")
			}
			if _, rejected := graph.ValidateRuleFacts(facts); len(rejected) > 0 {
				t.Errorf("%d of %d rule facts rejected:\n%v", len(rejected), rules, rejected)
			}
		})
	}
}

// countRules returns how many rule facts facts holds, and how many of
// them carry a condition.
func countRules(facts []extract.Fact) (int, int) {
	rules, conditions := 0, 0
	for i := range facts {
		if facts[i].Kind != extract.KindRule {
			continue
		}
		rules++
		if facts[i].Attrs[extract.AttrCondition] != "" {
			conditions++
		}
	}
	return rules, conditions
}

// goldenFacts reads a golden: a bare JSON array of facts.
func goldenFacts(t *testing.T, testdata fs.FS, name string) []extract.Fact {
	t.Helper()
	data, err := fs.ReadFile(testdata, name)
	if err != nil {
		t.Fatal(err)
	}
	var facts []extract.Fact
	if err = json.Unmarshal(data, &facts); err != nil {
		t.Fatal(err)
	}
	return facts
}

// TestExtractorEndpointJourneys: the webapp extractor's route facts give
// each GraphQL endpoint its resolver's span (handler_lines), so the built
// endpoint's journey lists the guard rules the extractor found in that
// resolver. Without the span, GraphQL endpoints had no journey at all.
func TestExtractorEndpointJourneys(t *testing.T) {
	testdata := os.DirFS("../../extractors/webapp/testdata")
	facts, rejected := graph.ValidateRuleFacts(goldenFacts(t, testdata, "golden.en.json"))
	if len(rejected) > 0 {
		t.Fatalf("golden rule facts rejected: %v", rejected)
	}
	nodes := graph.Build(facts, nil, extract.DomainResolver{}, "en", nil)

	withJourney, endpoints := 0, 0
	for i := range nodes {
		n := &nodes[i]
		if n.Type != graph.TypeEndpoint {
			continue
		}
		endpoints++
		if n.Extra[graph.ExtraJourney] != "" {
			withJourney++
		}
	}
	if endpoints == 0 {
		t.Fatal("no endpoint node: the golden no longer exercises routes")
	}
	// Every guarded resolver's endpoint lists its guards; an open or
	// unguarded one may have none, but most of the fixture's are guarded.
	if withJourney*2 < endpoints {
		t.Errorf("%d of %d endpoints have a journey, want most", withJourney, endpoints)
	}
}
