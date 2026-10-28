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
// of silently deleting authorization rules from real specs.
func TestExtractorRuleFactsPass(t *testing.T) {
	testdata := os.DirFS("../../extractors/webapp/testdata")
	goldens, err := fs.Glob(testdata, "golden*.json")
	if err != nil || len(goldens) == 0 {
		t.Fatalf("no webapp extractor golden (%v)", err)
	}
	for _, name := range goldens {
		t.Run(name, func(t *testing.T) {
			facts := goldenFacts(t, testdata, name)
			rules := 0
			for i := range facts {
				if facts[i].Kind == extract.KindRule {
					rules++
				}
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
