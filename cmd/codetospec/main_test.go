package main

import (
	"context"
	"encoding/json"
	"io/fs"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/StevenACoffman/codetospec/internal/config"
	"github.com/StevenACoffman/codetospec/internal/drift"
	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/graph"
	"github.com/StevenACoffman/codetospec/internal/grounding"
	"github.com/StevenACoffman/codetospec/internal/llm"
	"github.com/StevenACoffman/codetospec/internal/mapper"
	"github.com/StevenACoffman/codetospec/internal/render"
	"github.com/StevenACoffman/codetospec/internal/sitter"
	"github.com/StevenACoffman/codetospec/internal/ui"
	"github.com/StevenACoffman/codetospec/internal/verify"
)

// mockLLM is a deterministic OpenAI-compatible endpoint: it answers map
// prompts with one rule per chunk containing business code, and reduce
// prompts by consolidating the candidates verbatim.
func mockLLM(t *testing.T) *httptest.Server {
	t.Helper()
	fileHeader := regexp.MustCompile(`FILE: (\S+) \(lines (\d+)-(\d+)\)`)
	listLine := func(prompt, prefix string) []string {
		for line := range strings.SplitSeq(prompt, "\n") {
			if rest, ok := strings.CutPrefix(line, prefix); ok {
				var list []string
				if err := json.Unmarshal([]byte(rest), &list); err != nil {
					t.Errorf("mock: cannot parse %s list %q: %v", prefix, rest, err)
				}
				return list
			}
		}
		return nil
	}

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []llm.Message `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Messages) < 2 {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		system, user := req.Messages[0].Content, req.Messages[1].Content

		var content string
		switch {
		case strings.Contains(system, "software archaeologist"):
			m := fileHeader.FindStringSubmatch(user)
			if m == nil {
				http.Error(w, "no FILE header", http.StatusBadRequest)
				return
			}
			payload := map[string]any{"chunk_summary": "Chunk analysé.", "rules": []any{}}
			if strings.Contains(user, "public function") {
				payload["rules"] = []any{
					map[string]any{
						"title":       "Règle de " + m[1],
						"ears_kind":   "event",
						"requirement": mockRequirement(m[1]),
						"citations": []any{
							map[string]any{"path": m[1], "lines": m[2] + "-" + m[3]},
						},
						"entities":   listLine(user, "ALLOWED_ENTITIES: "),
						"endpoints":  listLine(user, "ALLOWED_ENDPOINTS: "),
						"nature":     "business",
						"origin":     "explicit",
						"confidence": 0.9,
					},
				}
			}
			data, err := json.Marshal(payload)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			content = string(data)
		case strings.Contains(system, "requirements engineer"):
			_, candidateJSON, ok := strings.Cut(user, "CANDIDATE_RULES:\n")
			if !ok {
				http.Error(w, "no candidates", http.StatusBadRequest)
				return
			}
			var candidates []mapper.Rule
			if err := json.Unmarshal([]byte(candidateJSON), &candidates); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			rules := make([]any, 0, len(candidates))
			for i, c := range candidates {
				rules = append(rules, map[string]any{
					"slug":        "regle-" + string(rune('a'+i)),
					"title":       c.Title,
					"ears_kind":   c.EarsKind,
					"requirement": c.Requirement,
					"rationale":   "Consolidée depuis le code.",
					"citations":   c.Citations,
					"entities":    c.Entities,
					"endpoints":   c.Endpoints,
					"acceptance_criteria": []string{
						"Le calcul nominal est correct.",
						"Le cas limite est couvert.",
					},
					"nature":     "business",
					"origin":     "explicit",
					"confidence": 0.9,
				})
			}
			data, err := json.Marshal(
				map[string]any{"domain_summary": "Domaine consolidé.", "rules": rules},
			)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			content = string(data)
		case strings.Contains(system, "adversarial code reviewer"):
			if !strings.Contains(user, "CITED SOURCE") {
				http.Error(w, "no cited source in crosscheck prompt", http.StatusBadRequest)
				return
			}
			content = `{"verdict": "supported", "reason": "Les lignes citées implémentent la règle."}`
		default:
			http.Error(w, "unknown prompt", http.StatusBadRequest)
			return
		}

		resp := map[string]any{
			"choices": []any{
				map[string]any{"message": map[string]any{"role": "assistant", "content": content}},
			},
			"usage": map[string]any{"prompt_tokens": 100, "completion_tokens": 50},
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Errorf("mock: encode response: %v", err)
		}
	}))
}

// mockRequirement is the requirement the mock map step states for a file.
// The ProrataCalculator rule carries a planted wrong threshold (15 is not in
// the code) that the mechanical grounding check must flag.
func mockRequirement(path string) string {
	if strings.HasSuffix(path, "ProrataCalculator.php") {
		return "SI le jour d'activation dépasse 15, ALORS le systeme doit facturer au prorata."
	}
	return "QUAND un abonné active en cours de mois, le systeme doit facturer au prorata."
}

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
		Exclude:        config.DefaultExclude,
		FactsFiles:     []string{filepath.Join(src, "fixture.facts.json")},
		LogLevel:       "warn",
		DomainStrategy: "auto",
		Crosscheck:     true,
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

	// The graph must be non-empty: >= 1 domain, entity, endpoint, EARS rule.
	nodes, err := render.LoadDir(outDir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	counts := map[string]int{}
	for _, n := range nodes {
		counts[n.Type]++
	}
	for _, nodeType := range []string{"domain", "entity", "endpoint", "rule"} {
		if counts[nodeType] < 1 {
			t.Errorf("no %s node produced (counts=%v)", nodeType, counts)
		}
	}

	// Rendered graph must re-verify cleanly against the sources.
	if violations := verify.Run(nodes, cfg.Roots()); len(violations) != 0 {
		t.Fatalf("verify violations: %v", violations)
	}

	// The endpoint wired through the facts file must exist and be linked.
	endpointPath := filepath.Join(outDir, "nodes", "endpoints", "post-api-activate.md")
	if _, err := os.Stat(endpointPath); err != nil {
		t.Errorf("endpoint node missing: %v", err)
	}

	// README must contain the Mermaid domain graph and coverage.
	readme, err := os.ReadFile(filepath.Join(outDir, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(readme), "```mermaid") ||
		!strings.Contains(string(readme), "## Couverture") {
		t.Error("README.md missing mermaid graph or coverage section")
	}
	if !strings.Contains(string(readme), "Contre-vérification adversariale") {
		t.Error("README.md missing adversarial crosscheck tally")
	}
	// The planted wrong threshold must be listed for review.
	if !strings.Contains(string(readme), "## Règles non ancrées") ||
		!strings.Contains(string(readme), ": 15\n") {
		t.Errorf("README.md does not list the planted ungrounded rule:\n%s", readme)
	}
	var ungrounded int
	for _, n := range nodes {
		if n.Type == graph.TypeRule && n.Extra["grounding"] == string(grounding.StatusUngrounded) {
			ungrounded++
		}
	}
	if ungrounded != 1 {
		t.Errorf("%d ungrounded rules, want exactly the planted one", ungrounded)
	}

	// Every rule node must carry its crosscheck verdict in the frontmatter.
	rulePaths, err := filepath.Glob(filepath.Join(outDir, "nodes", "rules", "*.md"))
	if err != nil || len(rulePaths) == 0 {
		t.Fatalf("no rule files: %v", err)
	}
	ruleData, err := os.ReadFile(rulePaths[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(ruleData), "crosscheck: supported") {
		t.Errorf("rule frontmatter missing crosscheck verdict:\n%s", ruleData)
	}

	// graph.json must parse and reference existing files.
	var graphDoc struct {
		Nodes map[string]struct {
			Type  string `json:"type"`
			File  string `json:"file"`
			Edges []any  `json:"edges"`
		} `json:"nodes"`
	}
	graphData, err := os.ReadFile(filepath.Join(outDir, "graph.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(graphData, &graphDoc); err != nil {
		t.Fatalf("graph.json invalid: %v", err)
	}
	if len(graphDoc.Nodes) != len(nodes) {
		t.Errorf("graph.json has %d nodes, want %d", len(graphDoc.Nodes), len(nodes))
	}
	for id, n := range graphDoc.Nodes {
		if _, err := os.Stat(filepath.Join(outDir, filepath.FromSlash(n.File))); err != nil {
			t.Errorf("graph.json node %s references missing file %s", id, n.File)
		}
	}

	// Facts merge must be persisted.
	factsData, err := os.ReadFile(filepath.Join(outDir, ".codetospec", "facts.json"))
	if err != nil {
		t.Fatal(err)
	}
	var envelope extract.FactsEnvelope
	if err := json.Unmarshal(
		factsData,
		&envelope,
	); err != nil ||
		envelope.Schema != extract.FactsSchema {
		t.Fatalf("facts.json invalid: %v", err)
	}

	// The verify and stats commands must succeed on the produced graph.
	if code := verifyCommand(
		&config.Config{Src: cfg.Src, Out: outDir},
		slog.New(slog.NewTextHandler(t.Output(), nil)),
	); code != 0 {
		t.Errorf("verify command exit = %d, want 0", code)
	}
	if code := statsCommand(
		&config.Config{Out: outDir},
		slog.New(slog.NewTextHandler(t.Output(), nil)),
	); code != 0 {
		t.Errorf("stats command exit = %d, want 0", code)
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
	files := make(map[string]string)
	err := filepath.WalkDir(outDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".codetospec" {
				return filepath.SkipDir
			}
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		rel, relErr := filepath.Rel(outDir, path)
		if relErr != nil {
			return relErr
		}
		files[rel] = string(data)
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
	_, byPath := allowedLists([]extract.Fact{
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
