package main

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/StevenACoffman/codetospec/internal/drift"
	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/graph"
	"github.com/StevenACoffman/codetospec/internal/mapper"
	"github.com/StevenACoffman/codetospec/internal/reducer"
	"github.com/StevenACoffman/codetospec/internal/render"
	"github.com/StevenACoffman/codetospec/internal/ui"
)

// TestCoverageGaps: only failed map and reduce units become gaps, next to
// the generated files, each in the domain of its unit.
func TestCoverageGaps(t *testing.T) {
	const (
		domain   = "billing"
		chunk    = "billing/a.go"
		userFile = "users/models_gen.go"
		genGap   = "generated " + userFile
	)
	generated := []graph.CoverageGap{
		{Domain: "users", Reason: graph.GapGenerated, Path: userFile},
	}
	cases := []struct {
		name    string
		mapped  []mapper.Output
		reduced []reducer.Output
		want    []string
	}{
		{"nothing failed", []mapper.Output{
			{Path: chunk, StartLine: 1, EndLine: 9, Domain: domain},
		}, []reducer.Output{{Domain: domain}}, []string{genGap}},
		{"a failed chunk", []mapper.Output{
			{Path: chunk, StartLine: 3, EndLine: 9, Domain: domain, Failed: true},
		}, nil, []string{genGap, "map-failed billing/a.go:3-9"}},
		{"a failed domain", nil, []reducer.Output{
			{Domain: domain, Failed: true},
		}, []string{genGap, "reduce-failed"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gaps := coverageGaps(generated, tc.mapped, tc.reduced)

			got := make([]string, len(gaps))
			for i, g := range gaps {
				got[i] = g.String()
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("gaps = %q, want %q", got, tc.want)
			}
			if len(generated) != 1 {
				t.Error("the generated gaps were modified")
			}
		})
	}
}

// TestEndToEndCallees: a rule citing a caller records its callee and the
// callee's digest, which survive the frontmatter round trip and keep a
// fresh graph drift-free.
func TestEndToEndCallees(t *testing.T) {
	server := mockLLM(t)
	defer server.Close()
	calls := `{"schema": "codetospec/facts/v1", "facts": [{"kind": "call",
"id": "call.calculate#invoice", "source": {"path": "app/Services/Billing/ProrataCalculator.php", "lines": "11-24"},
"attrs": {"callee_path": "app/Models/Invoice.php", "callee_lines": "1-19"},
"origin": "go", "certainty": "static"}]}`
	factsFile := filepath.Join(t.TempDir(), "calls.facts.json")
	if err := os.WriteFile(factsFile, []byte(calls), 0o600); err != nil {
		t.Fatal(err)
	}
	outDir := t.TempDir()
	cfg := e2eConfig(t, server.URL, outDir)
	cfg.FactsFiles = append(cfg.FactsFiles, factsFile)
	if err := runPipeline(context.Background(), cfg, ui.Discard{},
		slog.New(slog.NewTextHandler(t.Output(), nil))); err != nil {
		t.Fatalf("runPipeline: %v", err)
	}
	nodes, err := render.LoadDir(outDir)
	if err != nil {
		t.Fatal(err)
	}

	withCallees := 0
	for i := range nodes {
		n := &nodes[i]
		if len(n.Callees) == 0 {
			continue
		}
		withCallees++
		if n.Callees[0] != (extract.Ref{Path: "app/Models/Invoice.php", Lines: "1-19"}) ||
			n.Extra[graph.ExtraCalleesDigest] == "" {
			t.Errorf("rule %s: callees %v, digest %q", n.ID, n.Callees,
				n.Extra[graph.ExtraCalleesDigest])
		}
	}
	if withCallees == 0 {
		t.Fatal("no rule records the calculator's callee")
	}
	if report := drift.Check(nodes, cfg.Roots()); report.Affected != 0 || report.Drifted() {
		t.Errorf("fresh graph drifted: %+v", report)
	}
}

// TestEndToEndKeepsReviewedRule: a rule file a person edited survives the
// next run as edited, marked reviewed; once its citation no longer
// resolves, the run stops before any LLM call.
func TestEndToEndKeepsReviewedRule(t *testing.T) {
	server := mockLLM(t)
	defer server.Close()
	outDir := t.TempDir()
	cfg := e2eConfig(t, server.URL, outDir)
	logger := slog.New(slog.NewTextHandler(t.Output(), nil))
	if err := runPipeline(context.Background(), cfg, ui.Discard{}, logger); err != nil {
		t.Fatalf("first run: %v", err)
	}
	paths, err := filepath.Glob(filepath.Join(outDir, "nodes", "rules", "*.md"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no rule file: %v", err)
	}
	out, err := os.OpenRoot(outDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = out.Close() })
	rulePath := "nodes/rules/" + filepath.Base(paths[0])
	data, err := out.ReadFile(rulePath)
	if err != nil {
		t.Fatal(err)
	}
	const note = "\nA reviewer's note.\n"
	if err = out.WriteFile(rulePath, append(data, note...), 0o600); err != nil {
		t.Fatal(err)
	}

	if err = runPipeline(context.Background(), cfg, ui.Discard{}, logger); err != nil {
		t.Fatalf("second run: %v", err)
	}
	kept, err := out.ReadFile(rulePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(string(kept), note) ||
		!strings.Contains(string(kept), "\nstatus: reviewed\n") {
		t.Fatalf("the edited rule was not kept:\n%s", kept)
	}

	broken := strings.Replace(string(kept), "lines: \"", "lines: \"9000-", 1)
	if err = out.WriteFile(rulePath, []byte(broken), 0o600); err != nil {
		t.Fatal(err)
	}
	err = runPipeline(context.Background(), cfg, ui.Discard{}, logger)
	if err == nil || !strings.Contains(err.Error(), "no longer cites existing code") {
		t.Fatalf("third run: err = %v, want the broken reviewed rule named", err)
	}
}
