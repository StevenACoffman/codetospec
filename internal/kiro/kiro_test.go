package kiro_test

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/graph"
	"github.com/StevenACoffman/codetospec/internal/kiro"
)

// rule is a rule of domain with body, verdict and extras.
func rule(domain, slug, body, verdict string, extra map[string]string) graph.Node {
	extra = maps.Clone(extra)
	if extra == nil {
		extra = map[string]string{}
	}
	extra["crosscheck"] = verdict
	return graph.Node{
		ID: "rule." + domain + "." + slug, Type: graph.TypeRule, Title: "Rule " + slug,
		Body:    body,
		Sources: []extract.Ref{{Path: "app/" + domain + ".go", Lines: "10-20"}},
		Edges:   []graph.Edge{{Type: graph.EdgeBelongsTo, To: "domain." + domain}},
		Extra:   extra,
	}
}

// TestExport: a spec per domain; each rule not refuted a requirement
// whose one criterion is its sentence in Kiro's capitals (as written when
// it does not parse); its acceptance criteria the property validating it;
// the actors it names in the glossary, described or not.
func TestExport(t *testing.T) {
	nodes := []graph.Node{
		{
			ID: "domain.billing", Type: graph.TypeDomain, Title: "Domain billing",
			Body: "\n# Domain billing\n\nBills subscribers.\n\n## Rules\n\n" +
				"- [Rule x](../rules/billing.x.md)",
		},
		{
			ID: "domain.crm", Type: graph.TypeDomain, Title: "Domain crm",
			Body: "\n# Domain crm\n\n## Rules\n\n- [Rule retry](../rules/crm.retry.md)",
		},
		rule("billing", "prorata",
			"**Requirement (EARS)**: WHILE the plan is active, WHEN a subscriber joins "+
				"mid-month, the system shall bill the remaining days.\n\n"+
				"**Acceptance criteria**:\n1. A join on day 16 of 30 bills 15 days.\n"+
				"2. A join on day 1 bills the month.",
			"supported", map[string]string{graph.ExtraEARSActions: "bill"}),
		rule("billing", "nightly",
			"**Requirement (EARS)**: The scheduler shall close the invoices at midnight.",
			"unchecked", nil),
		rule("billing", "refund", "**Requirement (EARS)**: The system shall refund twice.",
			"unsupported", nil),
		rule("billing", "legacy",
			"**Requirement (EARS)**: Once paid, the system shall ship.", "partial", nil),
		rule("crm", "retry",
			"**Requirement (EARS)**: WHEN a sync fails, IF it failed three times, THEN "+
				"the notifier shall page the owner.", "supported", nil),
	}
	actors := map[string]string{"the scheduler": "Runs the nightly jobs."}

	spec := kiro.Export(nodes, actors)

	if !slices.Equal(spec.Refuted, []string{"rule.billing.refund"}) || spec.Requirements != 4 {
		t.Errorf("refuted = %v, requirements = %d", spec.Refuted, spec.Requirements)
	}
	want := []string{
		".kiro/specs/billing/design.md",
		".kiro/specs/billing/requirements.md",
		".kiro/specs/crm/requirements.md",
	}
	if got := slices.Sorted(maps.Keys(spec.Files)); !slices.Equal(got, want) {
		t.Fatalf("files = %v, want %v", got, want)
	}
	for _, rel := range want {
		t.Run(rel, func(t *testing.T) {
			checkGolden(t, filepath.Join("testdata", filepath.FromSlash(rel)), spec.Files[rel])
		})
	}
}

// checkGolden compares data with the golden file, or rewrites it when
// UPDATE_GOLDEN is set.
func checkGolden(t *testing.T, golden string, data []byte) {
	t.Helper()
	if os.Getenv("UPDATE_GOLDEN") != "" {
		if err := os.MkdirAll(filepath.Dir(golden), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(filepath.Clean(golden))
	if err != nil {
		t.Fatalf("missing golden %s (run UPDATE_GOLDEN=1 go test ./internal/kiro): %v",
			golden, err)
	}
	if got := string(data); got != string(want) {
		t.Errorf("differs from golden:\n--- got ---\n%s\n--- want ---\n%s",
			got, strings.TrimSpace(string(want)))
	}
}
