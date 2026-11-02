package quire_test

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/graph"
	"github.com/StevenACoffman/codetospec/internal/quire"
)

// rule is a billing rule with body, verdict and extras.
func rule(slug, title, body, verdict string, extra map[string]string) graph.Node {
	extra = maps.Clone(extra)
	if extra == nil {
		extra = map[string]string{}
	}
	extra["crosscheck"] = verdict
	return graph.Node{
		ID: "rule.billing." + slug, Type: graph.TypeRule, Title: title, Body: body,
		Sources: []extract.Ref{{Path: "app/Billing.php", Lines: "10-20"}},
		Edges:   []graph.Edge{{Type: graph.EdgeBelongsTo, To: "domain.billing"}},
		Extra:   extra,
	}
}

// TestExport: each rule not refuted becomes an FR, numbered in id order,
// with its criteria (or its requirement when it states none) as the
// table, a pipe escaped, its quality attribute and tests carried; a
// technical rule serving a quality becomes an NFR, its threshold the one
// its code states or unknown with the question asking for it; an index
// per kind lists them.
func TestExport(t *testing.T) {
	nodes := []graph.Node{
		rule(
			"round",
			"Round\ninvoices",
			"**Requirement (EARS)**: The system shall round to the cent.\n\n"+
				"**Acceptance criteria**:\n1. 1.005 | rounds to 1.01.\n2. 1.004 rounds to 1.00.",
			"supported",
			map[string]string{
				graph.ExtraQualityAttribute: "reliability",
				graph.ExtraTestedBy:         "billing_test.go#TestRound, billing_test.go#TestTotal",
			},
		),
		rule(
			"refund",
			"Refund",
			"**Requirement (EARS)**: The system shall refund twice.",
			"unsupported",
			nil,
		),
		rule(
			"activate",
			"Activate",
			"**Requirement (EARS)**: WHEN a plan starts, the system shall bill it.",
			"unchecked",
			map[string]string{graph.ExtraQualityAttribute: "not-an-attribute"},
		),
		rule(
			"timeout",
			"Payment call timeout",
			"**Requirement (EARS)**: The system shall time out a payment call after 30 seconds.",
			"supported",
			map[string]string{
				"nature":                    "technical",
				graph.ExtraQualityAttribute: "performance_efficiency",
				graph.ExtraThreshold:        "30 seconds",
			},
		),
		rule(
			"signed",
			"Signed webhooks",
			"**Requirement (EARS)**: The system shall accept only signed webhooks.\n\n"+
				"**Acceptance criteria**:\n1. An unsigned webhook is rejected with 401.",
			"supported",
			map[string]string{
				"nature":                    "technical",
				graph.ExtraQualityAttribute: "security",
				graph.ExtraDigest:           "d1",
			},
		),
		{ID: "domain.billing", Type: graph.TypeDomain, Title: "Billing"},
	}

	spec, err := quire.Export(nodes)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}

	if !slices.Equal(spec.Refuted, []string{"rule.billing.refund"}) {
		t.Errorf("refuted = %v", spec.Refuted)
	}
	want := []string{
		"functional/FR-001-billing-activate.md",
		"functional/FR-002-billing-round.md",
		"functional/index.md",
		"non-functional/NFR-001-billing-signed.md",
		"non-functional/NFR-002-billing-timeout.md",
		"non-functional/index.md",
	}
	if spec.FRs != 2 || spec.NFRs != 2 {
		t.Errorf("FRs, NFRs = %d, %d, want 2, 2", spec.FRs, spec.NFRs)
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
		t.Fatalf(
			"missing golden %s (run UPDATE_GOLDEN=1 go test ./internal/quire): %v",
			golden,
			err,
		)
	}
	if got := string(data); got != string(want) {
		t.Errorf(
			"differs from golden:\n--- got ---\n%s\n--- want ---\n%s",
			got,
			strings.TrimSpace(string(want)),
		)
	}
}
