package render_test

import (
	"bytes"
	"fmt"
	"io/fs"
	"maps"
	"os"
	pathpkg "path"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/StevenACoffman/codetospec/internal/alignment"
	"github.com/StevenACoffman/codetospec/internal/communities"
	"github.com/StevenACoffman/codetospec/internal/concept"
	"github.com/StevenACoffman/codetospec/internal/consistency"
	"github.com/StevenACoffman/codetospec/internal/d2"
	"github.com/StevenACoffman/codetospec/internal/domainmodel"
	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/graph"
	"github.com/StevenACoffman/codetospec/internal/locale"
	"github.com/StevenACoffman/codetospec/internal/position"
	"github.com/StevenACoffman/codetospec/internal/questions"
	"github.com/StevenACoffman/codetospec/internal/render"
)

// testNodes is the test graph in French.
func testNodes() []graph.Node { return testNodesIn("fr") }

// testNodesIn is the test graph whose text is written in lang, labels as
// the build writes them.
func testNodesIn(lang string) []graph.Node {
	l := locale.For(lang).Labels
	tr := func(fr, en string) string {
		if lang == "fr" {
			return fr
		}
		return en
	}
	return []graph.Node{
		{
			ID:     billingDomain,
			Type:   "domain",
			Status: graph.StatusGenerated,
			Title:  fmt.Sprintf(l.DomainTitleFmt, "billing"),
			Body: tr("Facturation des abonnés.", "Subscriber billing.") + "\n\n" + l.RulesHeading +
				"\n\n- [" + tr("Prorata à l'activation", "Prorata on activation") +
				"](../rules/billing.prorata-activation.md)",
			Edges: []graph.Edge{{Type: "depends_on", To: "domain.crm"}},
		},
		{
			ID:     "domain.crm",
			Type:   "domain",
			Status: graph.StatusGenerated,
			Title:  fmt.Sprintf(l.DomainTitleFmt, "crm"),
			Body:   tr("Gestion des abonnés.", "Subscriber management."),
		},
		{
			ID:     "enum.models-invoicestatus",
			Type:   graph.TypeEnum,
			Status: graph.StatusGenerated,
			Title:  "models.InvoiceStatus",
			Body: l.EnumValues + "\n\n- `Draft` = `draft`\n- `Paid` = `paid`\n\n" +
				l.EnumTransitions + "\n\n" +
				l.EnumTransitionHeader + "\n| `Draft` | `Paid` | `app/Models/Invoice.php:20-24` |",
			Sources: []extract.Ref{
				{Path: "app/Models/Invoice.php", Lines: "5-5"},
				{Path: "app/Models/Invoice.php", Lines: "20-24"},
			},
			Extra: map[string]string{graph.ExtraTransitions: "1"},
		},
		{
			ID:      "entity.invoices",
			Type:    "entity",
			Status:  graph.StatusGenerated,
			Title:   "invoices",
			Body:    l.Table + "`invoices`\n\n" + l.Columns + "\n- `id:id`\n- `amount:decimal`",
			Sources: []extract.Ref{{Path: "database/migrations/m.php", Lines: "11-16"}},
		},
		{
			ID:     "endpoint.post-api-activate",
			Type:   "endpoint",
			Status: graph.StatusGenerated,
			Title:  "POST /api/activate",
			Body: l.Method + "POST\n\n" + l.Path + "`/api/activate`\n\n" + l.Handler +
				"`App\\Http\\Controllers\\ActivationController@store`",
			Sources: []extract.Ref{{Path: "routes/web.php", Lines: "5-5"}},
		},
		{
			ID:     "rule.billing.prorata-activation",
			Type:   "rule",
			Status: graph.StatusGenerated,
			Title:  tr("Prorata à l'activation", "Prorata on activation"),
			Body: l.Requirement + tr(
				"QUAND un abonné active en cours de mois, le systeme doit facturer au prorata des jours restants.",
				"WHEN a subscriber activates mid-month, the system shall bill pro rata for the remaining days.",
			) + "\n\n" +
				l.Rationale + tr("Calcul observé dans ProrataCalculator.", "Computation seen in ProrataCalculator.") +
				"\n\n" + l.Criteria + tr(
				"\n1. Cas nominal couvert.\n2. Cas limite couvert.\n3. Erreur couverte.",
				"\n1. Nominal case covered.\n2. Edge case covered.\n3. Error covered.",
			),
			Sources: []extract.Ref{
				{Path: "app/Services/Billing/ProrataCalculator.php", Lines: "11-24"},
			},
			Edges: []graph.Edge{
				{Type: "belongs_to", To: billingDomain},
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
				// Planted: a form problem, free text that must stay quoted.
				graph.ExtraEARSIssue: `event requirement does not open with WHEN`,
				graph.ExtraTests:     "2",
				graph.ExtraTestDepth: "1",
				graph.ExtraTestedBy: "tests/ProrataTest.php#testActivation, " +
					"tests/ProrataTest.php#testRefund",
				graph.ExtraTestsViaInterface: "1",
				graph.ExtraExecuted:          "7/9",
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

// TestWriteGolden writes the same graph in each language and compares it
// with testdata/golden/<lang>.
func TestWriteGolden(t *testing.T) {
	for _, lang := range []string{"en", "fr"} {
		t.Run(lang, func(t *testing.T) { writeGolden(t, lang) })
	}
}

func writeGolden(t *testing.T, lang string) {
	t.Helper()
	outDir := t.TempDir()
	nodes := graph.AnnotateCoverage(testNodesIn(lang), testGaps())
	meta := testMeta()
	meta.Lang = lang
	if err := render.Write(outDir, nodes, &meta); err != nil {
		t.Fatalf("Write: %v", err)
	}

	goldenFiles := []string{
		"nodes/rules/billing.prorata-activation.md",
		"nodes/domains/billing.md",
		"nodes/entities/invoices.md",
		"nodes/endpoints/post-api-activate.md",
		"nodes/enums/models-invoicestatus.md",
		"graph.json",
		"README.md",
		llmsFile,
		render.QuestionsFile,
	}
	out := os.DirFS(outDir)
	for _, rel := range goldenFiles {
		got, err := fs.ReadFile(out, rel)
		if err != nil {
			t.Fatalf("missing output %s: %v", rel, err)
		}
		checkGolden(t, filepath.Join("testdata", "golden", lang, filepath.FromSlash(rel)), got)
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

	rule := testNodes()[0]
	for _, n := range testNodes() {
		if n.Type == graph.TypeRule {
			rule = n
		}
	}
	rule.Extra = maps.Clone(rule.Extra)
	rule.Extra[graph.ExtraEARSPatterns] = "state, event"
	parsed, err := render.ParseNode(render.NodeMarkdown(&rule, locale.For("fr")))
	if err != nil || parsed.Extra[graph.ExtraEARSPatterns] != "state, event" {
		t.Errorf(
			"ears_patterns = %q, %v; want it read back",
			parsed.Extra[graph.ExtraEARSPatterns],
			err,
		)
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
	meta.Layers = []position.Layer{
		{Pattern: "cmd/...", Nature: position.Technical, Placed: 8, Disagree: 6, Asked: 1},
		{Pattern: "internal/old", Nature: position.Business},
	}
	meta.DomainModel = &domainmodel.Report{
		Models: []string{"billing.modelith.yaml"},
		Entities: []domainmodel.Entity{
			{
				Name: "billing.Invoice", Code: domainmodel.CodeEntity{ID: "entity.invoice"},
				Actions: []domainmodel.Action{
					{Name: "issue", Rules: []string{ruleID}},
					{Name: "void"},
				},
				UnmodeledActions: []domainmodel.RuleAction{
					{Verb: "prorate", Rules: []string{ruleID}},
				},
			},
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
				", 1 unchecked", "## Ungrounded rules",
				"## Scattered domains", "- `main`: 6 files in 2 communities",
				"## Shared entities", "- `Invoice` (`entity.invoice`): `billing`, `tax`",
				"## Layers", "| layer | nature | rules | disagree | questions |",
				"| `cmd/...` | technical | 8 | 6 (75%) | 1 |",
				"| `internal/old` | business | 0 | 0 | 0 |",
				"## Domain model", "- Model entities found in the code: 1/2",
				"- `billing.Customer`",
				"- Model actions a rule performs (same verb): 1/2",
				"1 model actions are performed by no rule of their entity",
				"- `billing.Invoice` · `void`",
				"- `billing.Invoice` · prorate (`rule.billing.prorata-activation`)",
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

// TestReadmeDiagrams: each drawn diagram is shown as its SVG, the dark
// one for a dark theme, linked to its source, all under diagrams/; the
// concept map says when it is a proposal; an absent diagram has no
// section.
func TestReadmeDiagrams(t *testing.T) {
	picture := func(name string) *d2.Picture {
		return &d2.Picture{
			Name: name, Source: name + " -> b\n",
			SVG: []byte("<svg>" + name + "</svg>"), DarkSVG: []byte("<svg>dark</svg>"),
		}
	}
	outDir := t.TempDir()
	meta := render.Meta{
		Lang:       "en",
		PackageMap: picture(render.PackageMapName), ConceptMap: picture(render.ConceptMapName),
		ConceptMapProposed: true,
	}
	if err := render.Write(outDir, nil, &meta); err != nil {
		t.Fatal(err)
	}
	readme, err := fs.ReadFile(os.DirFS(outDir), "README.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"## Package dependencies",
		`<source media="(prefers-color-scheme: dark)" srcset="diagrams/packages-dark.svg">`,
		`<img alt="The code&#39;s packages and their imports" src="diagrams/packages.svg">`,
		"[D2 source](diagrams/packages.d2)",
		"## Concept map\n\n**Proposal, not confirmed**",
		`src="diagrams/concepts.svg"`,
	} {
		if !strings.Contains(string(readme), want) {
			t.Errorf("README lacks %q:\n%s", want, readme)
		}
	}
	for _, name := range []string{"packages.d2", "packages.svg", "concepts-dark.svg"} {
		if _, readErr := fs.ReadFile(os.DirFS(outDir), "diagrams/"+name); readErr != nil {
			t.Errorf("diagrams/%s: %v", name, readErr)
		}
	}

	bare := t.TempDir()
	if err = render.Write(bare, nil, &render.Meta{Lang: "en"}); err != nil {
		t.Fatal(err)
	}
	if readme, err = fs.ReadFile(os.DirFS(bare), "README.md"); err != nil ||
		strings.Contains(string(readme), "## Package dependencies") {
		t.Errorf("README without diagrams = %q, %v; want no diagram section", readme, err)
	}
}

// TestReadmePackageMapFit: a package map fitted to be drawable says so in
// its intro: too many imports, then the grouping, then the arrows left
// out; one only reduced says nothing of groups.
func TestReadmePackageMapFit(t *testing.T) {
	readme := func(groupDepth, implied int) string {
		t.Helper()
		out := t.TempDir()
		meta := render.Meta{
			Lang: "en", PackageMapGroupDepth: groupDepth, PackageMapImplied: implied,
			PackageMap: &d2.Picture{
				Name: render.PackageMapName, Source: "a -> b\n", SVG: []byte("<svg/>"),
			},
		}
		if err := render.Write(out, nil, &meta); err != nil {
			t.Fatal(err)
		}
		b, err := fs.ReadFile(os.DirFS(out), "README.md")
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	grouped := readme(2, 542)
	want := "Too many imports to lay out (over 300). Packages are grouped by directory, " +
		"2 levels below the shared prefix; a node \"…/ (n)\" stands for the n packages under it. " +
		"542 arrows a longer path already says are left out"
	if !strings.Contains(grouped, want) {
		t.Errorf("README lacks %q:\n%s", want, grouped)
	}
	reduced := readme(0, 226)
	if !strings.Contains(reduced, "(over 300). 226 arrows") ||
		strings.Contains(reduced, "grouped") {
		t.Errorf("reduced map's intro:\n%s", reduced)
	}
	if plain := readme(0, 0); strings.Contains(plain, "Too many imports") {
		t.Errorf("a map drawn as it is says it was fitted:\n%s", plain)
	}
}

// TestReadmeTestedLine: the README counts the rules a test reaches, then
// apart those only an interface call reaches.
func TestReadmeTestedLine(t *testing.T) {
	ruleWith := func(slug string, extra map[string]string) graph.Node {
		return graph.Node{ID: "rule.billing." + slug, Type: graph.TypeRule, Extra: extra}
	}
	nodes := []graph.Node{
		ruleWith("a", map[string]string{graph.ExtraTests: "2"}),
		ruleWith("b", map[string]string{graph.ExtraTests: "0", graph.ExtraTestsViaInterface: "1"}),
		ruleWith("c", map[string]string{graph.ExtraTests: "0"}),
	}
	outDir := t.TempDir()
	if err := render.Write(outDir, nodes, &render.Meta{Lang: "en"}); err != nil {
		t.Fatal(err)
	}
	readme, err := fs.ReadFile(os.DirFS(outDir), "README.md")
	if err != nil {
		t.Fatal(err)
	}
	want := "- Tests: 1 of 3 rules have their cited code reached by a test, within 3 calls " +
		"(`tests`, `tested_by`); 1 more only through an interface call " +
		"(`tests_via_interface`: possibly run)\n"
	if !strings.Contains(string(readme), want) {
		t.Errorf("README lacks %q:\n%s", want, readme)
	}
}

// TestReadmeDuplicatesByEdge: a duplicate pair is intra-domain by the
// domains its rules belong to, not by their ids: a moved rule keeps its
// old domain in its id.
func TestReadmeDuplicatesByEdge(t *testing.T) {
	ruleIn := func(id, domain string) graph.Node {
		return graph.Node{
			ID: id, Type: graph.TypeRule,
			Edges: []graph.Edge{{Type: graph.EdgeBelongsTo, To: "domain." + domain}},
		}
	}
	nodes := []graph.Node{
		ruleIn("rule.billing.a", "invoicing"), // moved from billing
		ruleIn("rule.invoicing.b", "invoicing"),
	}
	meta := render.Meta{Lang: "en", DuplicatePairs: []consistency.DuplicatePair{
		{A: "rule.billing.a", B: "rule.invoicing.b", Similarity: 0.9},
	}}
	outDir := t.TempDir()
	if err := render.Write(outDir, nodes, &meta); err != nil {
		t.Fatal(err)
	}
	readme, err := fs.ReadFile(os.DirFS(outDir), "README.md")
	if err != nil {
		t.Fatal(err)
	}
	want := "1 intra-domain, 0 cross-domain"
	if !strings.Contains(string(readme), want) {
		t.Errorf("README lacks %q:\n%s", want, readme)
	}
}

// TestReadmeEvidenceSection: each domain's rules by the nature their
// evidence gives, those disagreeing with their own nature counted.
func TestReadmeEvidenceSection(t *testing.T) {
	ruleIn := func(id, nature, byEvidence string) graph.Node {
		n := graph.Node{
			ID: id, Type: graph.TypeRule,
			Edges: []graph.Edge{{Type: graph.EdgeBelongsTo, To: billingDomain}},
			Extra: map[string]string{"nature": nature},
		}
		if byEvidence != "" {
			n.Extra[graph.ExtraNatureByEvidence] = byEvidence
		}
		return n
	}
	nodes := []graph.Node{
		ruleIn("rule.billing.a", "business", "business"),
		ruleIn("rule.billing.b", "business", "technical"),
		ruleIn("rule.billing.c", "technical", "open"),
		ruleIn("rule.billing.d", "technical", ""),
	}
	outDir := t.TempDir()
	if err := render.Write(outDir, nodes, &render.Meta{Lang: "en"}); err != nil {
		t.Fatal(err)
	}
	readme, err := fs.ReadFile(os.DirFS(outDir), "README.md")
	if err != nil {
		t.Fatal(err)
	}
	want := "| `billing` | 4 | 1 | 1 | 1 | 1 | 1 |\n"
	if !strings.Contains(string(readme), want) {
		t.Errorf("README lacks %q:\n%s", want, readme)
	}
}

// TestReadmeConceptsSection: each concept with its rules and their current
// domains; conflicts and unplaced rules counted; no section without models.
func TestReadmeConceptsSection(t *testing.T) {
	readme := func(report *concept.Report) string {
		t.Helper()
		outDir := t.TempDir()
		if err := render.Write(
			outDir,
			nil,
			&render.Meta{Lang: "en", Concepts: report},
		); err != nil {
			t.Fatal(err)
		}
		data, err := fs.ReadFile(os.DirFS(outDir), "README.md")
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	got := readme(&concept.Report{
		Concepts: []concept.Count{
			{
				Concept: conceptAssignment,
				Rules:   3,
				Domains: []string{"models (2)", "resolvers (1)"},
			},
		},
		Conflicts: 1,
		Unplaced:  2,
	})
	for _, want := range []string{
		"## Concepts",
		"1 rules name concepts with no common owner",
		"2 business rules name no concept",
		"| `a.Assignment` | 3 | models (2), resolvers (1) |\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("README lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(readme(nil), "## Concepts") {
		t.Error("a Concepts section without models")
	}
}

// TestReadmeAlignmentSection: the questions settling the conflicts, the
// pairs by outcome with the unresolved divergences and the absences, the
// recovery (an index rounding to zero written 0.00) and the spread
// tables; a resolved divergence leaves the list (and stays in
// questions.json); no section without models.
func TestReadmeAlignmentSection(t *testing.T) {
	const (
		assignment  = conceptAssignment
		student     = "a.Student"
		config      = "a.Config"
		pairSubject = assignment + " ~ " + config
	)
	readme := func(meta *render.Meta) (string, string) {
		t.Helper()
		outDir := t.TempDir()
		if err := render.Write(outDir, nil, meta); err != nil {
			t.Fatal(err)
		}
		data, err := fs.ReadFile(os.DirFS(outDir), "README.md")
		if err != nil {
			t.Fatal(err)
		}
		qs, err := fs.ReadFile(os.DirFS(outDir), render.QuestionsFile)
		if err != nil {
			t.Fatal(err)
		}
		return string(data), string(qs)
	}
	report := &alignment.Report{
		Pairs: []alignment.Pair{
			{
				A:        assignment,
				B:        student,
				Outcome:  alignment.Converges,
				Evidence: []string{"fk"},
			},
			{
				A: assignment, B: config, Outcome: alignment.Diverges,
				Evidence: []string{"a.Assignment → a.Config: call a.go:1-2 → c.go:3-4", "second"},
			},
			{A: config, B: student, Outcome: alignment.Absent},
		},
		Scattering: []alignment.Spread{
			{Name: assignment, Rules: 5, Places: 3, Top: "a", TopRules: 3},
		},
		Tangling: []alignment.Spread{
			{Name: "shared", Rules: 4, Places: 2, Top: config, TopRules: 3},
		},
		Recovery: alignment.Agreement{
			Files:       9,
			Concepts:    3,
			Communities: 4,
			ARI:         0.25,
			Defined:     true,
		},
	}
	meta := &render.Meta{
		Lang: "en",
		Concepts: &concept.Report{
			Conflicts: 3,
			Sets: []concept.ConflictSet{
				{Concepts: []string{config, "a.Event"}, Rules: 2, Settles: 2},
				{Concepts: []string{student, "a.Event"}, Rules: 1, Settles: 1},
			},
		},
		Alignment: report,
	}
	got, qs := readme(meta)
	for _, want := range []string{
		"These 2 questions, asked in order, settle the 3 conflicts",
		"1. `a.Config`, `a.Event`: conflicts settled: 2\n",
		"## Concepts and code",
		"Pairs: 1 converge (declared and shown), 1 diverge",
		"1 absent (declared, not shown), 0 unobservable",
		"- `" + pairSubject + "`: a.Assignment → a.Config: call a.go:1-2 → c.go:3-4 (+1)\n",
		"- `a.Config ~ a.Student`\n",
		"adjusted Rand index of 0.25 (1: the same partition, 0: chance) over 9 files (3 concepts, 4 communities",
		"| `a.Assignment` | 5 | 3 | `a` (3) |\n",
		"| `shared` | 4 | 2 | `a.Config` (3) |\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("README lacks %q:\n%s", want, got)
		}
	}
	if !strings.Contains(qs, questions.KindConceptDivergence) {
		t.Errorf("questions.json lacks the divergence:\n%s", qs)
	}

	meta.Resolutions = map[string]questions.Resolution{
		questions.ID(questions.KindConceptDivergence, pairSubject, pairSubject, ""): {
			Disposition: questions.Verified, Reason: "the model lacks it",
		},
	}
	if got, _ = readme(meta); strings.Contains(got, "- `"+pairSubject+"`") {
		t.Errorf("a resolved divergence is listed:\n%s", got)
	}
	report.Recovery.ARI = -0.001
	if got, _ = readme(meta); !strings.Contains(got, "adjusted Rand index of 0.00 ") {
		t.Errorf("an index rounding to zero is not 0.00:\n%s", got)
	}
	if got, _ = readme(&render.Meta{Lang: "en"}); strings.Contains(got, "## Concepts and code") {
		t.Error("a Concepts and code section without models")
	}
}

// TestReadmeUnreviewedModels: models still holding definitions
// codetospec wrote are flagged in the Concepts section, and only those.
func TestReadmeUnreviewedModels(t *testing.T) {
	const unreviewed = "**Unreviewed models**: 1 of the 2 entities"
	meta := &render.Meta{
		Lang:     "en",
		Concepts: &concept.Report{},
		DomainModel: &domainmodel.Report{Entities: []domainmodel.Entity{
			{Name: conceptAssignment, Unreviewed: true}, {Name: "a.Config"},
		}},
	}
	cases := map[string]struct {
		models *domainmodel.Report
		want   bool
	}{
		"unreviewed entity": {meta.DomainModel, true},
		"reviewed throughout": {
			&domainmodel.Report{Entities: []domainmodel.Entity{{Name: "a.Config"}}},
			false,
		},
		"no model report": {nil, false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			meta.DomainModel = tc.models
			outDir := t.TempDir()
			if err := render.Write(outDir, nil, meta); err != nil {
				t.Fatal(err)
			}
			data, err := fs.ReadFile(os.DirFS(outDir), "README.md")
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.Contains(string(data), "**Unreviewed models**"); got != tc.want {
				t.Errorf("flagged = %v, want %v:\n%s", got, tc.want, data)
			}
			if tc.want && !strings.Contains(string(data), unreviewed) {
				t.Errorf("README lacks %q:\n%s", unreviewed, data)
			}
		})
	}
}

// TestReadmeModelFiles: the model files written, the entities still
// undefined, and the column links no resolution closed.
func TestReadmeModelFiles(t *testing.T) {
	open := extract.ColumnLink{Table: "ChangeEvent", Column: "assignment_id", Target: "assignments"}
	closed := extract.ColumnLink{Table: "Note", Column: "assignment_id", Target: "assignments"}
	meta := &render.Meta{
		Lang:           "en",
		ModelFiles:     []string{"generated/concepts.modelith.yaml"},
		ModelUndefined: map[string][]string{"generated/concepts.modelith.yaml": {"Receipt"}},
		ColumnLinks:    []extract.ColumnLink{open, closed},
		Resolutions: map[string]questions.Resolution{
			questions.ID(questions.KindColumnLink, closed.String(), closed.String(), ""): {
				Disposition: questions.Verified, Reason: "no link",
			},
		},
	}
	outDir := t.TempDir()
	if err := render.Write(outDir, nil, meta); err != nil {
		t.Fatal(err)
	}
	data, err := fs.ReadFile(os.DirFS(outDir), "README.md")
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	for _, want := range []string{
		"- `generated/concepts.modelith.yaml`\n",
		"- `Receipt` (`generated/concepts.modelith.yaml`)\n",
		"1 columns are named like a foreign key elsewhere",
		"- `ChangeEvent.assignment_id → assignments`\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("README lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, closed.String()) {
		t.Errorf("a resolved link is listed:\n%s", got)
	}
}

// conceptAssignment is a concept of the tests' confirmed model.
const conceptAssignment = "a.Assignment"

// billingDomain is the id of the domain node the tests' rules belong to.
const billingDomain = "domain.billing"

// scheduler is an actor of the glossaries these tests write.
const scheduler = "the scheduler"

// TestReadmeUnusedActors: the README names the glossary's actors no rule
// names; none named when every one is.
func TestReadmeUnusedActors(t *testing.T) {
	rule := func(slug, sentence string) graph.Node {
		return graph.Node{
			ID: "rule.billing." + slug, Type: graph.TypeRule,
			Body: "**Requirement (EARS)**: " + sentence,
		}
	}
	nodes := []graph.Node{
		rule("nightly", "The scheduler shall close the invoices."),
		rule("bill", "The system shall bill."),
	}
	for name, tc := range map[string]struct {
		actors map[string]string
		want   string
	}{
		"unused": {
			map[string]string{scheduler: "Runs jobs.", "the pager": "", "the notifier": ""},
			"- Glossary actors no rule names: the notifier, the pager\n",
		},
		"all named": {map[string]string{scheduler: "Runs jobs."}, ""},
	} {
		t.Run(name, func(t *testing.T) {
			outDir := t.TempDir()
			if err := render.Write(
				outDir,
				nodes,
				&render.Meta{Lang: "en", Actors: tc.actors},
			); err != nil {
				t.Fatal(err)
			}
			readme, err := fs.ReadFile(os.DirFS(outDir), "README.md")
			if err != nil {
				t.Fatal(err)
			}
			got := string(readme)
			if tc.want != "" && !strings.Contains(got, tc.want) {
				t.Errorf("README lacks %q:\n%s", tc.want, got)
			}
			if tc.want == "" && strings.Contains(got, "Glossary actors") {
				t.Errorf("README lists unused actors:\n%s", got)
			}
		})
	}
}

// TestActorsRoundTrip: the glossary of actors is kept in graph.json for
// exports; a graph without one reads back none.
func TestActorsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	actors := map[string]string{scheduler: "Runs the nightly jobs."}
	if err := render.Write(dir, testNodes(), &render.Meta{Lang: "en", Actors: actors}); err != nil {
		t.Fatal(err)
	}
	got, err := render.LoadActors(dir)
	if err != nil || !maps.Equal(got, actors) {
		t.Errorf("LoadActors = %v, %v; want %v", got, err, actors)
	}
	if none, noneErr := render.LoadActors(t.TempDir()); noneErr != nil || len(none) != 0 {
		t.Errorf("no graph: %v, %v", none, noneErr)
	}
}
