package render_test

import (
	"bytes"
	"io/fs"
	"os"
	pathpkg "path"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/StevenACoffman/codetospec/internal/communities"
	"github.com/StevenACoffman/codetospec/internal/domainmodel"
	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/graph"
	"github.com/StevenACoffman/codetospec/internal/locale"
	"github.com/StevenACoffman/codetospec/internal/render"
)

func testNodes() []graph.Node {
	return []graph.Node{
		{
			ID:     "domain.billing",
			Type:   "domain",
			Status: graph.StatusGenerated,
			Title:  "Domaine billing",
			Body:   "Facturation des abonnés.\n\n## Règles\n\n- [Prorata à l'activation](../rules/billing.prorata-activation.md)",
			Edges:  []graph.Edge{{Type: "depends_on", To: "domain.crm"}},
		},
		{
			ID:     "domain.crm",
			Type:   "domain",
			Status: graph.StatusGenerated,
			Title:  "Domaine crm",
			Body:   "Gestion des abonnés.",
		},
		{
			ID:      "entity.invoices",
			Type:    "entity",
			Status:  graph.StatusGenerated,
			Title:   "invoices",
			Body:    "**Table** : `invoices`\n\n**Colonnes** :\n- `id:id`\n- `amount:decimal`",
			Sources: []extract.Ref{{Path: "database/migrations/m.php", Lines: "11-16"}},
		},
		{
			ID:      "endpoint.post-api-activate",
			Type:    "endpoint",
			Status:  graph.StatusGenerated,
			Title:   "POST /api/activate",
			Body:    "**Méthode** : POST\n\n**Path** : `/api/activate`\n\n**Handler** : `App\\Http\\Controllers\\ActivationController@store`",
			Sources: []extract.Ref{{Path: "routes/web.php", Lines: "5-5"}},
		},
		{
			ID:     "rule.billing.prorata-activation",
			Type:   "rule",
			Status: graph.StatusGenerated,
			Title:  "Prorata à l'activation",
			Body: "**Exigence (EARS)** : QUAND un abonné active en cours de mois, le systeme doit facturer au prorata des jours restants.\n\n" +
				"**Justification** : Calcul observé dans ProrataCalculator.\n\n" +
				"**Critères d'acceptation** :\n1. Cas nominal couvert.\n2. Cas limite couvert.\n3. Erreur couverte.",
			Sources: []extract.Ref{
				{Path: "app/Services/Billing/ProrataCalculator.php", Lines: "11-24"},
			},
			Edges: []graph.Edge{
				{Type: "belongs_to", To: "domain.billing"},
				{Type: "touches", To: "entity.invoices"},
				{Type: "exposed_by", To: "endpoint.post-api-activate"},
			},
			Extra: map[string]string{
				"ears":       "event",
				"acceptance": "3",
				"nature":     "business",
				"origin":     "explicit",
				"confidence": "0.90",
				"crosscheck": "supported",
				// Planted: the stated value sits outside the cited lines.
				"grounding":         "ungrounded",
				"grounding_missing": `30 @app/Services/Billing/ProrataCalculator.php:9`,
			},
		},
	}
}

// testGaps is the code the golden run left without rules: the failed chunk
// testMeta counts, in billing.
func testGaps() []graph.CoverageGap {
	return []graph.CoverageGap{{
		Domain: "billing",
		Reason: graph.GapMapFailed,
		Path:   "app/Services/Billing/Refunds.php",
		Lines:  "1-40",
	}}
}

func testMeta() render.Meta {
	return render.Meta{
		CoverageGaps: testGaps(),
		Coverage: graph.Coverage{
			EndpointsTotal: 1, EndpointsReferenced: 1,
			EntitiesTotal: 1, EntitiesTouched: 1,
		},
		ChunksTotal:    5,
		ChunksFailed:   1,
		DomainsTotal:   1,
		FilesNoGrammar: []string{"routes/web.php"},
		// Crosscheck ran: one supported rule, one the review never judged.
		Crosschecked:        true,
		CrosscheckSupported: 1,
		CrosscheckUnchecked: 1,
	}
}

func TestWriteGolden(t *testing.T) {
	outDir := t.TempDir()
	nodes := graph.AnnotateCoverage(testNodes(), testGaps())
	if err := render.Write(outDir, nodes, new(testMeta())); err != nil {
		t.Fatalf("Write: %v", err)
	}

	goldenFiles := []string{
		"nodes/rules/billing.prorata-activation.md",
		"nodes/domains/billing.md",
		"nodes/entities/invoices.md",
		"nodes/endpoints/post-api-activate.md",
		"graph.json",
		"README.md",
		llmsFile,
	}
	out := os.DirFS(outDir)
	for _, rel := range goldenFiles {
		got, err := fs.ReadFile(out, rel)
		if err != nil {
			t.Fatalf("missing output %s: %v", rel, err)
		}
		checkGolden(t, filepath.Join("testdata", "golden", filepath.FromSlash(rel)), got)
	}
}

// checkGolden compares data with the golden file, or rewrites it when
// UPDATE_GOLDEN=1.
func checkGolden(t *testing.T, golden string, data []byte) {
	t.Helper()
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(golden), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, data, 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(filepath.Clean(golden))
	if err != nil {
		t.Fatalf(
			"missing golden %s (run UPDATE_GOLDEN=1 go test ./internal/render): %v",
			golden,
			err,
		)
	}
	if !bytes.Equal(data, want) {
		t.Errorf("%s differs from golden:\n--- got ---\n%s\n--- want ---\n%s", golden, data, want)
	}
}

var mdLink = regexp.MustCompile(`\]\(([^)]+\.md)\)`)

func TestWriteRelativeLinksResolve(t *testing.T) {
	outDir := t.TempDir()
	if err := render.Write(outDir, testNodes(), new(testMeta())); err != nil {
		t.Fatalf("Write: %v", err)
	}
	out := os.DirFS(outDir)
	paths, err := fs.Glob(out, "nodes/*/*.md")
	if err != nil || len(paths) == 0 {
		t.Fatalf("no node files written: %v", err)
	}
	for _, path := range paths {
		data, readErr := fs.ReadFile(out, path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		for _, m := range mdLink.FindAllStringSubmatch(string(data), -1) {
			target := pathpkg.Join(pathpkg.Dir(path), m[1])
			if _, statErr := fs.Stat(out, target); statErr != nil {
				t.Errorf("%s: broken relative link %q", path, m[1])
			}
		}
	}
}

func TestParseNodeRoundTrip(t *testing.T) {
	for _, n := range testNodes() {
		parsed, err := render.ParseNode(render.NodeMarkdown(&n, locale.For("fr")))
		if err != nil {
			t.Fatalf("ParseNode(%s): %v", n.ID, err)
		}
		if parsed.ID != n.ID || parsed.Type != n.Type || parsed.Status != n.Status {
			t.Errorf("%s: id/type/status lost in round-trip", n.ID)
		}
		if parsed.Title != n.Title {
			t.Errorf("%s: title = %q, want %q", n.ID, parsed.Title, n.Title)
		}
		if len(parsed.Sources) != len(n.Sources) || len(parsed.Edges) != len(n.Edges) {
			t.Errorf("%s: sources/edges count lost", n.ID)
		}
		if n.Extra["ears"] != "" && parsed.Extra["ears"] != n.Extra["ears"] {
			t.Errorf("%s: ears = %q, want %q", n.ID, parsed.Extra["ears"], n.Extra["ears"])
		}
	}
}

func TestLoadDir(t *testing.T) {
	outDir := t.TempDir()
	if err := render.Write(outDir, testNodes(), new(testMeta())); err != nil {
		t.Fatalf("Write: %v", err)
	}
	nodes, err := render.LoadDir(outDir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if len(nodes) != len(testNodes()) {
		t.Fatalf("loaded %d nodes, want %d", len(nodes), len(testNodes()))
	}
}

func TestFrontmatterExactShape(t *testing.T) {
	var rule graph.Node
	for _, n := range testNodes() {
		if n.Type == "rule" {
			rule = n
		}
	}
	front := render.Frontmatter(&rule)
	for _, want := range []string{
		"id: rule.billing.prorata-activation\n",
		"type: rule\n",
		"status: generated\n",
		"  - path: app/Services/Billing/ProrataCalculator.php\n    lines: \"11-24\"\n",
		"  - {type: belongs_to, to: domain.billing}\n",
		"  - {type: touches, to: entity.invoices}\n",
		"  - {type: exposed_by, to: endpoint.post-api-activate}\n",
		"ears: event\n",
		"acceptance: 3\n",
	} {
		if !strings.Contains(front, want) {
			t.Errorf("frontmatter missing %q:\n%s", want, front)
		}
	}
}

// llmsFile is the agent-facing index render.Write produces.
const llmsFile = "llms.txt"

// TestWriteEnglish checks that --lang en output carries no French
// scaffolding: README, llms.txt and node footers are all in English.
func TestWriteEnglish(t *testing.T) {
	outDir := t.TempDir()
	meta := testMeta()
	meta.Lang = "en"
	meta.DomainSplits = []communities.Split{{Domain: "main", Communities: 2, Files: 6}}
	meta.FilesGenerated = 4
	meta.SharedEntities = []graph.SharedEntity{
		{ID: "entity.invoice", Title: "Invoice", Domains: []string{"billing", "tax"}},
	}
	meta.DomainModel = &domainmodel.Report{
		Models: []string{"billing.modelith.yaml"},
		Entities: []domainmodel.Entity{
			{Name: "billing.Invoice", Code: domainmodel.CodeEntity{ID: "entity.invoice"}},
			{Name: "billing.Customer"},
		},
	}
	if err := render.Write(outDir, testNodes(), &meta); err != nil {
		t.Fatalf("Write: %v", err)
	}
	cases := map[string]struct {
		file    string
		want    []string
		notWant []string
	}{
		"readme": {
			file: "README.md",
			want: []string{
				"# Generated specification", "## Coverage", "- Adversarial crosscheck:",
				", 1 unchecked", "## Ungrounded rules", "## Domains",
				"## Scattered domains", "- `main`: 6 files in 2 communities",
				"## Shared entities", "- `Invoice` (`entity.invoice`): `billing`, `tax`",
				"## Domain model", "- Model entities found in the code: 1/2",
				"- `billing.Customer`",
				"- Generated files (not mapped, \"Code generated … DO NOT EDIT.\" header): 4",
			},
			notWant: []string{"Spécification", "Couverture", "Domaines", "non vérifiées"},
		},
		"llms": {
			file:    llmsFile,
			want:    []string{"Navigation: start at nodes/domains/"},
			notWant: []string{"Navigation :"},
		},
		"rule node footer": {
			file:    filepath.Join("nodes", "rules", "billing.prorata-activation.md"),
			want:    []string{"\n**Sources**: ", "\nLinks: [Domain billing]"},
			notWant: []string{"Liens", "Domaine"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			data, err := fs.ReadFile(os.DirFS(outDir), filepath.ToSlash(tc.file))
			if err != nil {
				t.Fatal(err)
			}
			for _, w := range tc.want {
				if !strings.Contains(string(data), w) {
					t.Errorf("missing %q", w)
				}
			}
			for _, w := range tc.notWant {
				if strings.Contains(string(data), w) {
					t.Errorf("unexpected French %q", w)
				}
			}
		})
	}
}

func TestParseNodeKeepsWidenedCitations(t *testing.T) {
	n := testNodes()[0]
	n.Extra = map[string]string{
		"grounding":                 "grounded",
		graph.ExtraGroundingWidened: "a.go:10-10, a.go:12-12",
	}
	md := render.NodeMarkdown(&n, locale.For("en"))
	if !strings.Contains(md, `grounding_widened: "a.go:10-10, a.go:12-12"`) {
		t.Fatalf("frontmatter lacks grounding_widened:\n%s", md)
	}
	parsed, err := render.ParseNode(md)
	if err != nil {
		t.Fatal(err)
	}
	if got := parsed.Extra[graph.ExtraGroundingWidened]; got != n.Extra[graph.ExtraGroundingWidened] {
		t.Fatalf("grounding_widened = %q after round trip", got)
	}
}

// TestParseNodeKeepsCitationRoot: a citation outside --src keeps its root
// through the frontmatter, and the body names it @root/path.
func TestParseNodeKeepsCitationRoot(t *testing.T) {
	n := testNodes()[0]
	shared := extract.Ref{Path: "pkg/web/serve/x.go", Lines: "3-9", Root: "webapp"}
	n.Sources = append(n.Sources, shared)
	md := render.NodeMarkdown(&n, locale.For("en"))
	if !strings.Contains(md, "`@webapp/pkg/web/serve/x.go:3-9`") {
		t.Fatalf("body does not name the rooted citation:\n%s", md)
	}
	parsed, err := render.ParseNode(md)
	if err != nil {
		t.Fatal(err)
	}
	rooted := 0
	for _, src := range parsed.Sources {
		if src.Root != "" {
			rooted++
			if src != shared {
				t.Fatalf("citation after round trip = %+v, want %+v", src, shared)
			}
		}
	}
	if rooted != 1 || len(parsed.Sources) != len(n.Sources) {
		t.Fatalf("sources after round trip = %+v", parsed.Sources)
	}
}

// TestParseNodeKeepsInheritedFrom: an inherited rule's label survives the
// frontmatter round trip.
func TestParseNodeKeepsInheritedFrom(t *testing.T) {
	n := testNodes()[0]
	n.Extra = map[string]string{graph.ExtraInheritedFrom: "pkg", "crosscheck": "supported"}
	parsed, err := render.ParseNode(render.NodeMarkdown(&n, locale.For("en")))
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Extra[graph.ExtraInheritedFrom] != "pkg" {
		t.Fatalf("inherited_from after round trip = %q", parsed.Extra[graph.ExtraInheritedFrom])
	}
}

func TestDomainDiagramGroupsSplitNamespaces(t *testing.T) {
	const goMain = "main" // Go's command package, split by directory
	domain := func(slug string, dependsOn ...string) graph.Node {
		n := graph.Node{ID: "domain." + slug, Type: graph.TypeDomain}
		for _, d := range dependsOn {
			n.Edges = append(n.Edges, graph.Edge{Type: graph.EdgeDependsOn, To: "domain." + d})
		}
		return n
	}
	nodes := []graph.Node{
		domain("cmd-backfill", "models", "cmd-seed"),
		domain("cmd-seed", "models"),
		domain("models", "cmd-seed"),
		domain("tasks-main", "models"), // a group of one: drawn plain
	}
	groups := map[string]string{"cmd-backfill": goMain, "cmd-seed": goMain, "tasks-main": "tasks"}
	outDir := t.TempDir()
	if err := render.Write(
		outDir,
		nodes,
		&render.Meta{DomainGroups: groups, Lang: "en"},
	); err != nil {
		t.Fatal(err)
	}
	readme, err := fs.ReadFile(os.DirFS(outDir), "README.md")
	if err != nil {
		t.Fatal(err)
	}
	got := string(readme)
	for _, want := range []string{
		"  subgraph group_main[\"main\"]\n    cmd_backfill[\"cmd-backfill\"]\n    cmd_seed[\"cmd-seed\"]\n  end\n",
		"  group_main --> models\n",
		"  models --> cmd_seed\n",
		"  tasks_main[\"tasks-main\"]\n",
		"  tasks_main --> models\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("diagram lacks %q:\n%s", want, got)
		}
	}
	if n := strings.Count(got, "group_main --> models"); n != 1 {
		t.Errorf("group arrow drawn %d times, want once:\n%s", n, got)
	}
	if strings.Contains(got, "group_main --> cmd_seed") {
		t.Errorf("arrow within the group drawn:\n%s", got)
	}
}
