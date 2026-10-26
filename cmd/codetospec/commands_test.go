package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/StevenACoffman/codetospec/internal/config"
	"github.com/StevenACoffman/codetospec/internal/drift"
	"github.com/StevenACoffman/codetospec/internal/graph"
)

// checkDriftJSON: on the unchanged tree, `drift --format json` lists every
// rule, by id, as ok.
func checkDriftJSON(t *testing.T, cfg *config.Config, outDir string, nodes []graph.Node) {
	t.Helper()
	var stdout bytes.Buffer
	c := cli{
		stdout: &stdout, stderr: t.Output(),
		logger: slog.New(slog.NewTextHandler(t.Output(), nil)),
	}
	if code := c.drift(&config.Config{Src: cfg.Src, Out: outDir, Format: "json"}); code != exitOK {
		t.Fatalf("drift command exit = %d, want %d", code, exitOK)
	}
	var got struct {
		OK    int `json:"ok"`
		Rules []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"rules"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("drift JSON: %v\n%s", err, stdout.String())
	}
	rules := 0
	for _, n := range nodes {
		if n.Type == graph.TypeRule {
			rules++
		}
	}
	if got.OK != rules || len(got.Rules) != rules {
		t.Fatalf(
			"drift JSON: ok %d, %d rules listed; want %d of each",
			got.OK,
			len(got.Rules),
			rules,
		)
	}
	for _, r := range got.Rules {
		if r.Status != string(drift.StatusOK) || !strings.HasPrefix(r.ID, "rule.") {
			t.Errorf("drift JSON rule %+v, want an ok rule id", r)
		}
	}
}

// checkDiffSelf: a graph diffed with itself has every rule unchanged.
func checkDiffSelf(t *testing.T, outDir string, nodes []graph.Node) {
	t.Helper()
	var stdout bytes.Buffer
	c := cli{
		stdout: &stdout, stderr: t.Output(),
		logger: slog.New(slog.NewTextHandler(t.Output(), nil)),
	}
	if code := c.diff(&config.Config{Old: outDir, Out: outDir}); code != exitOK {
		t.Fatalf("diff command exit = %d, want %d", code, exitOK)
	}
	rules := 0
	for _, n := range nodes {
		if n.Type == graph.TypeRule {
			rules++
		}
	}
	want := fmt.Sprintf("diff: 0 added, 0 removed, 0 renamed, 0 changed, %d unchanged\n", rules)
	if stdout.String() != want {
		t.Errorf("diff of a graph with itself = %q, want %q", stdout.String(), want)
	}
}

// checkQuality: with --quality, each technical rule carries the attribute
// the mock model gave it, no business rule carries one, and the README
// counts them.
func checkQuality(t *testing.T, outDir string, nodes []graph.Node) {
	t.Helper()
	readme, err := fs.ReadFile(os.DirFS(outDir), "README.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(readme), "qualité ISO 25010 (règles techniques) : `security`") {
		t.Errorf("README does not count the quality attributes:\n%s", readme)
	}
	technical := 0
	for _, n := range nodes {
		if n.Type != graph.TypeRule {
			continue
		}
		want := ""
		if n.Extra["nature"] == "technical" {
			want = "security"
			technical++
		}
		if got := n.Extra[graph.ExtraQualityAttribute]; got != want {
			t.Errorf(
				"rule %s (%s): quality_attribute = %q, want %q",
				n.ID,
				n.Extra["nature"],
				got,
				want,
			)
		}
	}
	if technical == 0 {
		t.Error("the fixture run produced no technical rule to classify")
	}
}

// checkExport: export writes one FR per rule and an index into a new
// directory, and refuses a directory that already holds files.
func checkExport(t *testing.T, outDir string, nodes []graph.Node) {
	t.Helper()
	to := t.TempDir()
	var stdout bytes.Buffer
	c := cli{
		stdout: &stdout,
		stderr: t.Output(),
		logger: slog.New(slog.NewTextHandler(t.Output(), nil)),
	}
	if code := c.export(&config.Config{Out: outDir, To: to}); code != exitOK {
		t.Fatalf("export exit = %d, want %d", code, exitOK)
	}
	written, err := fs.Glob(os.DirFS(to), "functional/FR-*.md")
	if err != nil {
		t.Fatal(err)
	}
	rules := 0
	for _, n := range nodes {
		if n.Type == graph.TypeRule {
			rules++
		}
	}
	if len(written) != rules {
		t.Errorf(
			"exported %d FRs, want one per rule (%d): %s",
			len(written),
			rules,
			stdout.String(),
		)
	}
	if code := c.export(&config.Config{Out: outDir, To: to}); code != exitFailure {
		t.Errorf("second export into %s: exit = %d, want %d", to, code, exitFailure)
	}
}
