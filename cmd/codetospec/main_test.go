package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/StevenACoffman/codetospec/internal/config"
	"github.com/StevenACoffman/codetospec/internal/drift"
	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/graph"
	"github.com/StevenACoffman/codetospec/internal/grounding"
	"github.com/StevenACoffman/codetospec/internal/render"
	"github.com/StevenACoffman/codetospec/internal/sitter"
	"github.com/StevenACoffman/codetospec/internal/state"
	"github.com/StevenACoffman/codetospec/internal/ui"
	"github.com/StevenACoffman/codetospec/internal/verify"
)

func e2eConfig(t *testing.T, serverURL, outDir string) *config.Config {
	t.Helper()
	src, err := filepath.Abs(filepath.Join("..", "..", "testdata", "fixture"))
	if err != nil {
		t.Fatal(err)
	}
	return &config.Config{
		Src:            src,
		Out:            outDir,
		BaseURL:        serverURL,
		Model:          "mock",
		Lang:           "fr",
		Workers:        2,
		MaxTokens:      4096,
		Exclude:        config.DefaultExclude(),
		FactsFiles:     []string{filepath.Join(src, "fixture.facts.json")},
		LogLevel:       "warn",
		DomainStrategy: "auto",
		Crosscheck:     true,
		Quality:        true,
	}
}

func TestEndToEndRunOnFixture(t *testing.T) {
	server := mockLLM(t)
	defer server.Close()
	outDir := t.TempDir()
	cfg := e2eConfig(t, server.URL, outDir)

	if err := runPipeline(
		context.Background(),
		cfg,
		ui.Discard{},
		slog.New(slog.NewTextHandler(t.Output(), nil)),
	); err != nil {
		t.Fatalf("runPipeline: %v", err)
	}
	nodes, err := render.LoadDir(outDir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	out := os.DirFS(outDir)

	checkNodes(t, out, cfg, nodes)
	checkReadme(t, out)
	checkGraphJSON(t, out, len(nodes))
	checkFactsFile(t, out)

	// The verify and stats commands must succeed on the produced graph.
	c := cli{
		stdout: t.Output(), stderr: t.Output(),
		logger: slog.New(slog.NewTextHandler(t.Output(), nil)),
	}
	if code := c.verify(&config.Config{Src: cfg.Src, Out: outDir}); code != exitOK {
		t.Errorf("verify command exit = %d, want %d", code, exitOK)
	}
	if code := c.stats(&config.Config{Out: outDir}); code != exitOK {
		t.Errorf("stats command exit = %d, want %d", code, exitOK)
	}
	checkDriftJSON(t, cfg, outDir, nodes)
	checkDiffSelf(t, outDir, nodes)
	checkQuality(t, outDir, nodes)
	checkExport(t, outDir, nodes)
	checkExportKiro(t, outDir, nodes)
}

// checkNodes: at least one node of each type, a graph that re-verifies
// against the sources, the endpoint wired through the facts file, exactly
// the planted ungrounded rule, and crosscheck verdicts in rule files.
func checkNodes(t *testing.T, out fs.FS, cfg *config.Config, nodes []graph.Node) {
	t.Helper()
	counts := map[string]int{}
	ungrounded := 0
	for i := range nodes {
		counts[nodes[i].Type]++
		if nodes[i].Type == graph.TypeRule &&
			nodes[i].Extra["grounding"] == string(grounding.StatusUngrounded) {
			ungrounded++
		}
	}
	for _, nodeType := range []string{graph.TypeDomain, graph.TypeEntity, "endpoint", graph.TypeRule} {
		if counts[nodeType] < 1 {
			t.Errorf("no %s node produced (counts=%v)", nodeType, counts)
		}
	}
	if ungrounded != 1 {
		t.Errorf("%d ungrounded rules, want exactly the planted one", ungrounded)
	}
	if violations := verify.Run(nodes, cfg.Roots()); len(violations) != 0 {
		t.Fatalf("verify violations: %v", violations)
	}
	if _, err := fs.Stat(out, "nodes/endpoints/post-api-activate.md"); err != nil {
		t.Errorf("endpoint node missing: %v", err)
	}
	rulePaths, err := fs.Glob(out, "nodes/rules/*.md")
	if err != nil || len(rulePaths) == 0 {
		t.Fatalf("no rule files: %v", err)
	}
	ruleData, err := fs.ReadFile(out, rulePaths[0])
	if err != nil || !strings.Contains(string(ruleData), "crosscheck: supported") {
		t.Errorf("rule frontmatter missing crosscheck verdict (%v):\n%s", err, ruleData)
	}
}

// checkReadme: the README has the Mermaid domain graph, the coverage and
// crosscheck sections, and lists the planted wrong threshold for review.
func checkReadme(t *testing.T, out fs.FS) {
	t.Helper()
	readme, err := fs.ReadFile(out, "README.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"```mermaid", "## Couverture", "Contre-vérification adversariale",
		"## Règles non ancrées", ": 15\n",
	} {
		if !strings.Contains(string(readme), want) {
			t.Errorf("README.md lacks %q:\n%s", want, readme)
		}
	}
}

// checkGraphJSON: graph.json parses, has every node, and references
// existing files.
func checkGraphJSON(t *testing.T, out fs.FS, nodeCount int) {
	t.Helper()
	var graphDoc struct {
		Nodes map[string]struct {
			Type     string `json:"type"`
			File     string `json:"file"`
			Coverage string `json:"coverage"`
			Edges    []any  `json:"edges"`
		} `json:"nodes"`
	}
	graphData, err := fs.ReadFile(out, "graph.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(graphData, &graphDoc); err != nil {
		t.Fatalf("graph.json invalid: %v", err)
	}
	if len(graphDoc.Nodes) != nodeCount {
		t.Errorf("graph.json has %d nodes, want %d", len(graphDoc.Nodes), nodeCount)
	}
	for id, n := range graphDoc.Nodes {
		if _, statErr := fs.Stat(out, n.File); statErr != nil {
			t.Errorf("graph.json node %s references missing file %s", id, n.File)
		}
		// Every chunk of the fixture maps, so no domain has a gap.
		if n.Type == graph.TypeDomain && n.Coverage != graph.CoverageComplete {
			t.Errorf("domain %s coverage = %q, want %q", id, n.Coverage, graph.CoverageComplete)
		}
	}
}

// checkFactsFile: the merged facts are persisted as a facts envelope.
func checkFactsFile(t *testing.T, out fs.FS) {
	t.Helper()
	factsData, err := fs.ReadFile(out, ".codetospec/facts.json")
	if err != nil {
		t.Fatal(err)
	}
	var envelope extract.FactsEnvelope
	if err = json.Unmarshal(factsData, &envelope); err != nil ||
		envelope.Schema != extract.FactsSchema {
		t.Fatalf("facts.json invalid: %v", err)
	}
}

// TestEndToEndDeterministicRerun checks that a second run on a warm cache
// produces byte-for-byte identical output files.
func TestEndToEndDeterministicRerun(t *testing.T) {
	server := mockLLM(t)
	defer server.Close()
	outDir := t.TempDir()
	cfg := e2eConfig(t, server.URL, outDir)

	if err := runPipeline(
		context.Background(),
		cfg,
		ui.Discard{},
		slog.New(slog.NewTextHandler(t.Output(), nil)),
	); err != nil {
		t.Fatalf("first run: %v", err)
	}
	first := snapshotOutput(t, outDir)
	if err := runPipeline(
		context.Background(),
		cfg,
		ui.Discard{},
		slog.New(slog.NewTextHandler(t.Output(), nil)),
	); err != nil {
		t.Fatalf("second run: %v", err)
	}
	second := snapshotOutput(t, outDir)

	if len(first) != len(second) {
		t.Fatalf("file sets differ: %d vs %d", len(first), len(second))
	}
	for path, content := range first {
		if second[path] != content {
			t.Errorf("%s differs between warm-cache runs", path)
		}
	}
}

// snapshotOutput captures every rendered output file (everything except the
// .codetospec cache).
func snapshotOutput(t *testing.T, outDir string) map[string]string {
	t.Helper()
	out := os.DirFS(outDir)
	files := make(map[string]string)
	err := fs.WalkDir(out, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".codetospec" {
				return fs.SkipDir
			}
			return nil
		}
		data, readErr := fs.ReadFile(out, path)
		if readErr != nil {
			return readErr
		}
		files[path] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// TestEndToEndEnglish checks that --lang en reaches every writer: the
// README and rule bodies come out with English labels.
func TestEndToEndEnglish(t *testing.T) {
	server := mockLLM(t)
	defer server.Close()
	outDir := t.TempDir()
	cfg := e2eConfig(t, server.URL, outDir)
	cfg.Lang = "en"

	if err := runPipeline(
		context.Background(),
		cfg,
		ui.Discard{},
		slog.New(slog.NewTextHandler(t.Output(), nil)),
	); err != nil {
		t.Fatalf("runPipeline: %v", err)
	}
	readme, err := fs.ReadFile(os.DirFS(outDir), "README.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(readme), "## Coverage") ||
		strings.Contains(string(readme), "Couverture") {
		t.Errorf("README.md is not in English:\n%s", readme)
	}
	nodes, err := render.LoadDir(outDir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	for _, n := range nodes {
		if n.Type == graph.TypeRule && !strings.Contains(n.Body, "**Requirement (EARS)**: ") {
			t.Errorf("rule %s body lacks the English requirement label:\n%s", n.ID, n.Body)
		}
	}
	if violations := verify.Run(nodes, cfg.Roots()); len(violations) != 0 {
		t.Fatalf("verify violations: %v", violations)
	}
}

// TestEndToEndDomainModel: a human modelith model is linted before any LLM
// call and compared with the built graph in the README; an invalid one
// fails the run.
func TestEndToEndDomainModel(t *testing.T) {
	server := mockLLM(t)
	defer server.Close()
	modelDir := t.TempDir()
	write := func(name, content string) string {
		path := filepath.Join(modelDir, name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	valid := write("billing.modelith.yaml", `kind: DomainModel
version: v1
entities:
  Invoice:
    definition: A bill sent to a subscriber.
    invariants:
      - id: prorated-first-month
        statement: A subscriber activating mid-month is billed prorata.
  Customer:
    definition: Someone who pays invoices.
`)
	outDir := t.TempDir()
	cfg := e2eConfig(t, server.URL, outDir)
	cfg.Lang = "en"
	cfg.DomainModels = []string{valid}
	if err := runPipeline(
		context.Background(),
		cfg,
		ui.Discard{},
		slog.New(slog.NewTextHandler(t.Output(), nil)),
	); err != nil {
		t.Fatalf("runPipeline: %v", err)
	}
	readme, err := fs.ReadFile(os.DirFS(outDir), "README.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"## Domain model",
		"- Model entities found in the code: 1/2", // Invoice is the invoices table
		"- `billing.Customer`",
	} {
		if !strings.Contains(string(readme), want) {
			t.Errorf("README lacks %q:\n%s", want, readme)
		}
	}

	invalid := write("broken.modelith.yaml", `kind: DomainModel
version: v1
entities:
  Invoice:
    definition: A bill.
    relationships:
      - entity: Nowhere
        cardinality: "1:n"
`)
	cfg = e2eConfig(t, server.URL, t.TempDir())
	cfg.DomainModels = []string{invalid}
	runErr := runPipeline(
		context.Background(),
		cfg,
		ui.Discard{},
		slog.New(slog.NewTextHandler(t.Output(), nil)),
	)
	if runErr == nil || !strings.Contains(runErr.Error(), "lint error") {
		t.Fatalf("runPipeline with an invalid model: err = %v, want a lint error", runErr)
	}
}

// TestEndToEndMechanicalRule: an extractor rule fact becomes a rule node
// marked extracted_by and crosscheck mechanical and passes verify; a
// malformed one is dropped and counted; a domain fact regroups a chunk.
func TestEndToEndMechanicalRule(t *testing.T) {
	server := mockLLM(t)
	defer server.Close()
	outDir := t.TempDir()
	cfg := e2eConfig(t, server.URL, outDir)
	cfg.Lang = "en"

	factsPath := filepath.Join(t.TempDir(), "mechanical.facts.json")
	facts := `{"schema":"codetospec/facts/v1","facts":[
{"kind":"rule","id":"rule.authz.prorata","origin":"webapp","certainty":"static",
 "source":{"path":"app/Services/Billing/ProrataCalculator.php","lines":"13-15"},
 "attrs":{"slug":"reject-negative-amount","title":"Reject negative amounts",
  "requirement":"IF the monthly amount is negative, THEN the system shall throw InvalidArgumentException.",
  "ears":"unwanted","nature":"business","domain":"billing"}},
{"kind":"dependency","id":"dependency.prorata","origin":"webapp","certainty":"static",
 "source":{"path":"app/Services/Billing/ProrataCalculator.php","lines":"14-14"},
 "attrs":{"service":"assignments","operation":"Mutation.acceptInvitation"}},
{"kind":"domain","id":"domain.prorata","origin":"webapp","certainty":"static",
 "source":{"path":"app/Services/Billing/ProrataCalculator.php","lines":"7-25"},
 "attrs":{"domain":"pricing"}},
{"kind":"rule","id":"rule.authz.broken","origin":"webapp","certainty":"static",
 "source":{"path":"app/Services/Billing/ProrataCalculator.php","lines":"13-15"},
 "attrs":{"slug":"broken","title":"Broken","requirement":"",
  "ears":"unwanted","nature":"business","domain":"billing"}}]}`
	if err := os.WriteFile(factsPath, []byte(facts), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg.FactsFiles = append(cfg.FactsFiles, factsPath)

	if err := runPipeline(
		context.Background(),
		cfg,
		ui.Discard{},
		slog.New(slog.NewTextHandler(t.Output(), nil)),
	); err != nil {
		t.Fatalf("runPipeline: %v", err)
	}
	outFS := os.DirFS(outDir)
	rule, err := fs.ReadFile(outFS, "nodes/rules/billing.reject-negative-amount.md")
	if err != nil {
		t.Fatalf("mechanical rule not rendered: %v", err)
	}
	// The domain fact moved the ProrataCalculator chunk into "pricing".
	if _, statErr := fs.Stat(outFS, "nodes/domains/pricing.md"); statErr != nil {
		t.Errorf("domain fact did not create the pricing domain: %v", statErr)
	}
	for _, want := range []string{"extracted_by: webapp", "crosscheck: mechanical"} {
		if !strings.Contains(string(rule), want) {
			t.Errorf("rule frontmatter lacks %q:\n%s", want, rule)
		}
	}
	readme, err := fs.ReadFile(outFS, "README.md")
	if err != nil {
		t.Fatal(err)
	}
	if _, statErr := fs.Stat(outFS, "nodes/services/assignments.md"); statErr != nil {
		t.Errorf("dependency fact did not create the service node: %v", statErr)
	}
	for _, want := range []string{
		"1 mechanical",
		"Extractor rules rejected (malformed, see the logs): 1",
		"External services called: 1",
	} {
		if !strings.Contains(string(readme), want) {
			t.Errorf("README.md lacks %q:\n%s", want, readme)
		}
	}
	nodes, err := render.LoadDir(outDir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if violations := verify.Run(nodes, cfg.Roots()); len(violations) != 0 {
		t.Fatalf("verify violations: %v", violations)
	}
}

// TestEndToEndRootedRule: an extractor rule cited in another source tree
// (--ref-root) is rendered with its root, grounded and digested from that
// tree, and verified against it; without the root, verify reports it.
func TestEndToEndRootedRule(t *testing.T) {
	server := mockLLM(t)
	defer server.Close()
	outDir := t.TempDir()
	cfg := e2eConfig(t, server.URL, outDir)
	cfg.Lang = "en"
	shared := t.TempDir()
	if err := os.MkdirAll(filepath.Join(shared, "pkg", "web"), 0o750); err != nil {
		t.Fatal(err)
	}
	code := strings.Join([]string{
		"package web",
		"",
		"func check(h string) error {",
		"\tif h != \"X-Ka-Secret\" {",
		"\t\treturn errDenied",
		"\t}",
		"\treturn nil",
		"}",
		"",
	}, "\n")
	checkFile := filepath.Join(shared, "pkg", "web", "check.go")
	if err := os.WriteFile(checkFile, []byte(code), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg.RefRoots = map[string]string{"webapp": shared}
	factsPath := filepath.Join(t.TempDir(), "rooted.facts.json")
	facts := `{"schema":"codetospec/facts/v1","facts":[
{"kind":"rule","id":"rule.webapp.shared","origin":"webapp","certainty":"static",
 "source":{"path":"pkg/web/check.go","lines":"3-8","root":"webapp"},
 "attrs":{"slug":"shared-check","title":"Shared check",
  "requirement":"IF the header is not ` + "`X-Ka-Secret`" + `, THEN the system shall reject the request.",
  "ears":"unwanted","nature":"business","domain":"shared-web"}},
{"kind":"rule","id":"rule.inherited.pkg.web.check","origin":"webapp","certainty":"static",
 "source":{"path":"pkg/web/check.go","lines":"3-8","root":"webapp"},
 "attrs":{"slug":"inherited-check","title":"Inherited check",
  "requirement":"IF the header is not ` + "`X-Ka-Secret`" + `, THEN the system shall refuse the call.",
  "ears":"unwanted","nature":"business","domain":"shared-web",
  "inherited_from":"pkg","crosscheck":"supported","origin":"implicit","confidence":"0.80"}}]}`
	if err := os.WriteFile(factsPath, []byte(facts), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg.FactsFiles = append(cfg.FactsFiles, factsPath)
	if err := runPipeline(
		context.Background(),
		cfg,
		ui.Discard{},
		slog.New(slog.NewTextHandler(t.Output(), nil)),
	); err != nil {
		t.Fatalf("runPipeline: %v", err)
	}
	rule, err := fs.ReadFile(os.DirFS(outDir), "nodes/rules/shared-web.shared-check.md")
	if err != nil {
		t.Fatalf("rooted rule not rendered: %v", err)
	}
	for _, want := range []string{"    root: webapp", "grounding: grounded", "digest: ", "`@webapp/pkg/web/check.go:3-8`"} {
		if !strings.Contains(string(rule), want) {
			t.Errorf("rooted rule lacks %q:\n%s", want, rule)
		}
	}
	inherited, err := fs.ReadFile(os.DirFS(outDir), "nodes/rules/shared-web.inherited-check.md")
	if err != nil {
		t.Fatalf("inherited rule not rendered: %v", err)
	}
	for _, want := range []string{"inherited_from: pkg", "crosscheck: supported", "origin: implicit", "grounding: grounded"} {
		if !strings.Contains(string(inherited), want) {
			t.Errorf("inherited rule lacks %q:\n%s", want, inherited)
		}
	}
	if strings.Contains(string(inherited), "extracted_by:") {
		t.Errorf("an inherited rule reads as mechanical:\n%s", inherited)
	}
	nodes, err := render.LoadDir(outDir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if violations := verify.Run(nodes, cfg.Roots()); len(violations) != 0 {
		t.Fatalf("verify violations: %v", violations)
	}
	if report := drift.Check(nodes, cfg.Roots()); report.Drifted() {
		t.Fatalf("drift on an unchanged tree: %+v", report)
	}
	cfg.RefRoots = nil
	if violations := verify.Run(nodes, cfg.Roots()); len(violations) != 2 {
		t.Fatalf("without --ref-root, violations = %v; want the two rooted citations", violations)
	}
}

// TestAllowedListsHandlerPath: a route naming its handler's file is allowed
// in that file's chunks without any symbol matching.
func TestAllowedListsHandlerPath(t *testing.T) {
	byPath := endpointsByPath([]extract.Fact{
		{
			Kind: "route",
			ID:   "route.graphql.mutation.addAuthEmail",
			Attrs: map[string]string{
				"method":                "MUTATION",
				"path":                  "addAuthEmail",
				extract.AttrHandlerPath: "resolvers/add_auth_email.go",
			},
		},
	})
	if got := byPath["resolvers/add_auth_email.go"]; len(got) != 1 ||
		got[0] != "endpoint.mutation-addauthemail" {
		t.Fatalf("endpoints for the handler file = %v", got)
	}
}

func TestDomainGroups(t *testing.T) {
	chunk := func(path, namespace, domain string) sitter.Chunk {
		return sitter.Chunk{Path: path, StartLine: 1, Namespace: namespace, Domain: domain}
	}
	hints := extract.NewDomainHints([]extract.Fact{{
		Kind: extract.KindDomain, Attrs: map[string]string{extract.AttrDomain: "auth"},
		Source: extract.Ref{Path: "cmd/login/main.go", Lines: "1-9"},
	}})
	chunks := []sitter.Chunk{
		chunk("cmd/seed/main.go", "main", "cmd-seed"),
		chunk("cmd/login/main.go", "main", "auth"), // an extractor hint decided
		chunk("models/user.go", "models", "models"),
		chunk("x/a.go", "main", "x"), // reached from two ambiguous namespaces
		chunk("x/b.go", "util", "x"),
	}
	got := domainGroups(chunks, map[string]bool{"main": true, "util": true}, &hints)
	want := map[string]string{"cmd-seed": "main"}
	if !maps.Equal(got, want) {
		t.Fatalf("domainGroups = %v, want %v", got, want)
	}
}

// TestRunDispatch covers the command line itself: what is a usage error
// and what a command failure, with nothing written to the process streams.
func TestRunDispatch(t *testing.T) {
	cases := map[string]struct {
		args     []string
		wantCode int
		wantErr  string // substring of stderr
	}{
		"no command":      {args: nil, wantCode: exitUsage, wantErr: "usage:"},
		"unknown command": {args: []string{"frobnicate"}, wantCode: exitUsage, wantErr: "usage:"},
		"bad flags":       {args: []string{"verify"}, wantCode: exitUsage, wantErr: "--src"},
		"diff without --old": {
			args: []string{"diff", "--new", t.TempDir()}, wantCode: exitUsage, wantErr: "--old",
		},
		"export without --to": {
			args: []string{"export", "--out", t.TempDir()}, wantCode: exitUsage, wantErr: "--to",
		},
		"stats without a state file": {
			args: []string{"stats", "--out", t.TempDir()}, wantCode: exitFailure,
			wantErr: "cannot load state",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(context.Background(), tc.args, &stdout, &stderr)
			if code != tc.wantCode || !strings.Contains(stderr.String(), tc.wantErr) {
				t.Fatalf("exit %d, stderr %q; want %d and %q",
					code, stderr.String(), tc.wantCode, tc.wantErr)
			}
		})
	}
}

// TestEndToEndWritesDomainModel: --write-domain-model writes the models,
// their provenance and a README section, identically on a warm rerun.
func TestEndToEndWritesDomainModel(t *testing.T) {
	server := mockLLM(t)
	defer server.Close()
	human := filepath.Join(t.TempDir(), "billing.modelith.yaml")
	if err := os.WriteFile(human, []byte(`kind: DomainModel
version: v1
entities:
  Invoice:
    definition: A bill sent to a subscriber.
`), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := map[string]struct {
		models    []string
		wantFiles []string
	}{
		"human model mirrored": {
			models:    []string{human},
			wantFiles: []string{"models/billing.modelith.yaml"},
		},
		"no human model: generated per domain": {
			wantFiles: []string{"models/generated/database.modelith.yaml"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			outDir := t.TempDir()
			cfg := e2eConfig(t, server.URL, outDir)
			cfg.Lang = "en"
			cfg.DomainModels = tc.models
			cfg.WriteDomainModel = true
			logger := slog.New(slog.NewTextHandler(t.Output(), nil))
			if err := runPipeline(context.Background(), cfg, ui.Discard{}, logger); err != nil {
				t.Fatalf("runPipeline: %v", err)
			}
			first := snapshotOutput(t, outDir)
			checkModelFiles(t, first, tc.wantFiles)
			if err := runPipeline(context.Background(), cfg, ui.Discard{}, logger); err != nil {
				t.Fatalf("second run: %v", err)
			}
			if second := snapshotOutput(t, outDir); !reflect.DeepEqual(second, first) {
				t.Error("a warm rerun changed the output")
			}
		})
	}
}

// checkModelFiles: the expected models, their provenance and the README
// section listing them are in the output.
func checkModelFiles(t *testing.T, output map[string]string, wantFiles []string) {
	t.Helper()
	for _, want := range append(wantFiles, "models/provenance.json") {
		if _, ok := output[want]; !ok {
			t.Errorf("%s not written", want)
		}
	}
	if !strings.Contains(output["README.md"], "## Domain model files") {
		t.Errorf("README lacks the model files section:\n%s", output["README.md"])
	}
}

// TestEndToEndInheritsSharedRules: rules of a shared spec whose citation
// lies in reached code (reach facts) come into the analyzed code's spec,
// cited under the shared root and labeled inherited_from.
func TestEndToEndInheritsSharedRules(t *testing.T) {
	const root = "pkg" // the shared root
	server := mockLLM(t)
	defer server.Close()
	logger := slog.New(slog.NewTextHandler(t.Output(), nil))

	// The shared spec: the fixture, rendered.
	sharedOut := t.TempDir()
	shared := e2eConfig(t, server.URL, sharedOut)
	if err := runPipeline(context.Background(), shared, ui.Discard{}, logger); err != nil {
		t.Fatalf("shared run: %v", err)
	}

	// The analyzed code reaches the prorata calculator of the shared code.
	appSrc := t.TempDir()
	reach := `{"schema": "codetospec/facts/v1", "facts": [{"kind": "reach",
"id": "reach.pkg.prorata", "attrs": {},
"source": {"path": "app/Services/Billing/ProrataCalculator.php", "lines": "1-200", "root": "pkg"},
"origin": "go", "certainty": "static"}]}`
	factsFile := filepath.Join(t.TempDir(), "reach.facts.json")
	if err := os.WriteFile(factsFile, []byte(reach), 0o600); err != nil {
		t.Fatal(err)
	}
	appOut := t.TempDir()
	app := e2eConfig(t, server.URL, appOut)
	app.Src, app.FactsFiles = appSrc, []string{factsFile}
	app.RefRoots = map[string]string{root: shared.Src}
	app.Inherit = map[string]string{root: sharedOut}
	if err := runPipeline(context.Background(), app, ui.Discard{}, logger); err != nil {
		t.Fatalf("app run: %v", err)
	}

	nodes, err := render.LoadDir(appOut)
	if err != nil {
		t.Fatal(err)
	}
	inherited := 0
	for i := range nodes {
		n := &nodes[i]
		if n.Extra[graph.ExtraInheritedFrom] != root {
			continue
		}
		inherited++
		if n.Sources[0].Root != root ||
			n.Sources[0].Path != "app/Services/Billing/ProrataCalculator.php" {
			t.Errorf("inherited rule %s cites %+v", n.ID, n.Sources)
		}
	}
	if inherited == 0 {
		t.Fatalf("no rule inherited; nodes: %d", len(nodes))
	}
}

// TestEndToEndCallerDomain: call facts reach the pipeline, and a caller in
// another directory leaves a package's domain alone — the prorata
// calculator (services), used only by the activation controller (http),
// keeps its rule in services. Same-directory callers: internal/callers.
func TestEndToEndCallerDomain(t *testing.T) {
	server := mockLLM(t)
	defer server.Close()
	calls := `{"schema": "codetospec/facts/v1", "facts": [{"kind": "call",
"id": "call.store#calculate", "source": {"path": "app/Http/Controllers/ActivationController.php", "lines": "10-30"},
"attrs": {"callee_path": "app/Services/Billing/ProrataCalculator.php", "callee_lines": "11-24"},
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
	for i := range nodes {
		n := &nodes[i]
		if n.Type != graph.TypeRule ||
			n.Sources[0].Path != "app/Services/Billing/ProrataCalculator.php" {
			continue
		}
		if !strings.HasPrefix(n.ID, "rule.services.") {
			t.Errorf("the calculator's rule %s left its domain services", n.ID)
		}
		return
	}
	t.Fatal("no rule cites the prorata calculator")
}

// TestEndToEndRefusesAnotherSourcesOutput: a run into an output holding
// another source's spec fails before writing anything there.
func TestEndToEndRefusesAnotherSourcesOutput(t *testing.T) {
	server := mockLLM(t)
	defer server.Close()
	outDir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(t.Output(), nil))
	if err := runPipeline(
		context.Background(),
		e2eConfig(t, server.URL, outDir),
		ui.Discard{},
		logger,
	); err != nil {
		t.Fatalf("first run: %v", err)
	}
	factsPath := filepath.Clean(filepath.Join(outDir, ".codetospec", "facts.json"))
	before, err := os.ReadFile(factsPath)
	if err != nil {
		t.Fatal(err)
	}
	other := e2eConfig(t, server.URL, outDir)
	other.Src = t.TempDir()
	err = runPipeline(context.Background(), other, ui.Discard{}, logger)
	if !errors.Is(err, state.ErrOtherSource) {
		t.Fatalf("second source: err = %v, want ErrOtherSource", err)
	}
	after, err := os.ReadFile(factsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) {
		t.Fatal("the refused run rewrote facts.json")
	}
}
