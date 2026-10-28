package graph_test

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/StevenACoffman/codetospec/internal/ears"
	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/graph"
	"github.com/StevenACoffman/codetospec/internal/locale"
	"github.com/StevenACoffman/codetospec/internal/reducer"
)

func fixtureFacts() []extract.Fact {
	return []extract.Fact{
		{
			Kind: extract.KindTable,
			ID:   "table.invoices",
			Attrs: map[string]string{
				extract.AttrName:    "invoices",
				extract.AttrColumns: "id:id, amount:decimal",
			},
			Source: extract.Ref{Path: "database/migrations/m.php", Lines: "11-16"},
		},
		{
			Kind: extract.KindRoute,
			ID:   "route.post./api/activate",
			Attrs: map[string]string{
				extract.AttrMethod: "POST",
				extract.AttrPath:   "/api/activate",
				"controller":       `App\Http\Controllers\ActivationController`,
				"action":           "store",
			},
			Source: extract.Ref{Path: "routes/web.php", Lines: "5-5"},
		},
		{
			Kind:  extract.KindModule,
			ID:    "module.App\\Http\\Controllers",
			Attrs: map[string]string{extract.AttrName: `App\Http\Controllers`},
			Source: extract.Ref{
				Path:  "app/Http/Controllers/ActivationController.php",
				Lines: "3-3",
			},
		},
		{
			Kind: extract.KindModule, ID: "module.App\\Services\\Billing",
			Attrs:  map[string]string{extract.AttrName: `App\Services\Billing`},
			Source: extract.Ref{Path: "app/Services/Billing/ProrataCalculator.php", Lines: "3-3"},
		},
		{
			Kind: extract.KindImport,
			ID:   "import.app/Http/Controllers/ActivationController.php#App\\Services\\Billing\\ProrataCalculator",
			Attrs: map[string]string{
				extract.AttrTarget:    `App\Services\Billing\ProrataCalculator`,
				extract.AttrNamespace: `App\Http\Controllers`,
			},
			Source: extract.Ref{
				Path:  "app/Http/Controllers/ActivationController.php",
				Lines: "6-6",
			},
		},
	}
}

func fixtureReduced() []reducer.Output {
	rule := func(slug string) reducer.Rule {
		return reducer.Rule{
			Slug:        slug,
			Title:       "Titre " + slug,
			EarsKind:    earsEvent,
			Requirement: "QUAND x, le systeme doit y.",
			Rationale:   "Parce que.",
			Citations: []extract.Ref{
				{Path: "app/Services/Billing/ProrataCalculator.php", Lines: "11-24"},
			},
			Entities:           []string{invoicesEntity},
			Endpoints:          []string{activateRoute},
			AcceptanceCriteria: []string{"a", "b", "c"},
			Nature:             natureBusiness,
			Origin:             originExplicit,
			Confidence:         0.9,
		}
	}
	return []reducer.Output{
		{
			Domain:        "services",
			DomainSummary: "Calculus.",
			Rules:         []reducer.Rule{rule("prorata-activation")},
		},
		{
			Domain:        "http",
			DomainSummary: "Entrées HTTP.",
			Rules:         []reducer.Rule{rule("activation-endpoint")},
		},
		{Domain: "failed", Failed: true},
	}
}

func TestBuildProducesAllNodeTypes(t *testing.T) {
	nodes := graph.Build(fixtureFacts(), fixtureReduced(), extract.DomainResolver{}, "fr", nil)
	byID := make(map[string]graph.Node, len(nodes))
	for _, n := range nodes {
		byID[n.ID] = n
	}

	for _, id := range []string{
		invoicesEntity,
		activateRoute,
		servicesDomain,
		"domain.http",
		prorataRule,
		"rule.http.activation-endpoint",
	} {
		if _, ok := byID[id]; !ok {
			t.Errorf("node %s missing (have %d nodes)", id, len(nodes))
		}
	}
	if _, ok := byID["domain.failed"]; ok {
		t.Error("failed domain must not produce a node")
	}

	rule := byID[prorataRule]
	if rule.Extra["ears"] != earsEvent || rule.Extra["acceptance"] != "3" {
		t.Errorf("rule extra = %v", rule.Extra)
	}
	wantEdges := []graph.Edge{
		{Type: "belongs_to", To: servicesDomain},
		{Type: "touches", To: invoicesEntity},
		{Type: "exposed_by", To: activateRoute},
	}
	if len(rule.Edges) != len(wantEdges) {
		t.Fatalf("rule edges = %+v", rule.Edges)
	}
	for i, want := range wantEdges {
		if rule.Edges[i] != want {
			t.Errorf("rule edge[%d] = %+v, want %+v", i, rule.Edges[i], want)
		}
	}

	endpoint := byID[activateRoute]
	if !strings.Contains(endpoint.Body, "POST") ||
		!strings.Contains(endpoint.Body, "/api/activate") ||
		!strings.Contains(endpoint.Body, "ActivationController@store") {
		t.Errorf("endpoint body = %q", endpoint.Body)
	}
}

// graph.TestBuildDomainDependsOnGoStyleImports covers the suffix fallback: Go
// imports carry a path ("app/internal/billing") while module facts carry the
// bare package name ("billing").
// TestBuildEARSPatterns: a rule's sentence names the patterns it
// combines, whatever kind it declares and whoever its actor is.
func TestBuildEARSPatterns(t *testing.T) {
	cases := map[string]struct {
		kind, requirement string
		patterns, issue   string
	}{
		"complex": {
			ears.Complex, "TANT QUE a, QUAND b, le système doit c.", "state, event", "",
		},
		"declared as its opening": {
			ears.State, "TANT QUE a, QUAND b, le système doit c.", "state, event", "",
		},
		"one pattern": {ears.Event, "QUAND b, le système doit c.", "", ""},
		"not parseable": {
			ears.Event,
			"QUAND b, le système doit c ; le système doit d.",
			"",
			"more than one",
		},
		"two triggers, another actor": {
			ears.Complex, "QUAND a, SI b, ALORS le logiciel doit c.", "event, unwanted", "",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			reduced := fixtureReduced()[:1]
			reduced[0].Rules[0].EarsKind = tc.kind
			reduced[0].Rules[0].Requirement = tc.requirement
			var rule graph.Node
			for _, n := range graph.Build(nil, reduced, extract.DomainResolver{}, "fr", nil) {
				if n.Type == graph.TypeRule {
					rule = n
				}
			}
			if got := rule.Extra[graph.ExtraEARSPatterns]; got != tc.patterns {
				t.Errorf("ears_patterns = %q, want %q", got, tc.patterns)
			}
			if got := rule.Extra[graph.ExtraEARSIssue]; !strings.Contains(got, tc.issue) ||
				(tc.issue == "") != (got == "") {
				t.Errorf("ears_issue = %q, want one holding %q", got, tc.issue)
			}
		})
	}
}

// TestRuleActions: the actions the model wrote are kept beside the
// sentence and read back whole; once an edit moves one, it is unknown.
func TestRuleActions(t *testing.T) {
	reduced := fixtureReduced()[:1]
	rule := &reduced[0].Rules[0]
	rule.EARS = ears.Components{
		When:  "x",
		Shall: []ears.Response{{Text: "create an invoice", Action: "create"}},
	}
	rule.Requirement = ears.Render(&rule.EARS, "fr")
	var node graph.Node
	for _, n := range graph.Build(nil, reduced, extract.DomainResolver{}, "fr", nil) {
		if n.Type == graph.TypeRule {
			node = n
		}
	}
	if got := node.Extra[graph.ExtraEARSActions]; got != "create" {
		t.Fatalf("ears_actions = %q, want create", got)
	}
	if c, ok := graph.RuleComponents(&node); !ok || !reflect.DeepEqual(c, rule.EARS) {
		t.Errorf("RuleComponents = %+v, %v; want %+v", c, ok, rule.EARS)
	}
	node.Body = strings.Replace(node.Body, "create an invoice", "issue an invoice", 1)
	if c, ok := graph.RuleComponents(&node); !ok || c.Shall[0].Action != "" {
		t.Errorf("edited rule's action = %q, want unknown", c.Shall[0].Action)
	}
}

func TestBuildDomainDependsOnGoStyleImports(t *testing.T) {
	facts := []extract.Fact{
		{
			Kind: extract.KindModule, ID: "module.billing",
			Attrs:  map[string]string{extract.AttrName: "billing"},
			Source: extract.Ref{Path: "internal/billing/calc.go", Lines: firstLine},
		},
		{
			Kind: extract.KindModule, ID: "module.invoice",
			Attrs:  map[string]string{extract.AttrName: "invoice"},
			Source: extract.Ref{Path: "internal/invoice/invoice.go", Lines: firstLine},
		},
		{
			Kind: extract.KindImport,
			ID:   "import.internal/invoice/invoice.go#app/internal/billing",
			Attrs: map[string]string{
				extract.AttrTarget:    "app/internal/billing",
				extract.AttrNamespace: "invoice",
			},
			Source: extract.Ref{Path: "internal/invoice/invoice.go", Lines: "5-5"},
		},
	}
	rule := func(domain string) reducer.Output {
		return reducer.Output{Domain: domain, DomainSummary: "s", Rules: []reducer.Rule{{
			Slug: "r", Title: "T", EarsKind: earsEvent, Requirement: "QUAND x, le systeme doit y.",
			Citations:          []extract.Ref{{Path: "internal/" + domain + "/x.go", Lines: "1-2"}},
			AcceptanceCriteria: []string{"a", "b"},
			Nature:             natureBusiness, Origin: originExplicit, Confidence: 0.9,
		}}}
	}
	nodes := graph.Build(
		facts,
		[]reducer.Output{rule("billing"), rule("invoice")},
		extract.DomainResolver{},
		"fr",
		nil,
	)
	for _, n := range nodes {
		if n.ID != "domain.invoice" {
			continue
		}
		for _, e := range n.Edges {
			if e.Type == "depends_on" && e.To == "domain.billing" {
				return
			}
		}
		t.Fatalf(
			"domain.invoice should depend on domain.billing via suffix match, edges = %+v",
			n.Edges,
		)
	}
	t.Fatal("domain.invoice not found")
}

func TestBuildDomainDependsOnFromImports(t *testing.T) {
	nodes := graph.Build(fixtureFacts(), fixtureReduced(), extract.DomainResolver{}, "fr", nil)
	for _, n := range nodes {
		if n.ID != "domain.http" {
			continue
		}
		for _, e := range n.Edges {
			if e.Type == "depends_on" && e.To == servicesDomain {
				return
			}
		}
		t.Fatalf("domain.http should depend on domain.services, edges = %+v", n.Edges)
	}
	t.Fatal("domain.http not found")
}

func TestBuildIsDeterministic(t *testing.T) {
	a := graph.Build(fixtureFacts(), fixtureReduced(), extract.DomainResolver{}, "fr", nil)
	b := graph.Build(fixtureFacts(), fixtureReduced(), extract.DomainResolver{}, "fr", nil)
	if len(a) != len(b) {
		t.Fatalf("node counts differ: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i].ID != b[i].ID || a[i].Body != b[i].Body {
			t.Fatalf("node %d differs between runs", i)
		}
	}
}

func TestComputeCoverage(t *testing.T) {
	nodes := graph.Build(fixtureFacts(), fixtureReduced(), extract.DomainResolver{}, "fr", nil)
	cov := graph.ComputeCoverage(nodes)
	if cov.EndpointsTotal != 1 || cov.EndpointsReferenced != 1 {
		t.Errorf(
			"endpoints coverage = %d/%d, want 1/1",
			cov.EndpointsReferenced,
			cov.EndpointsTotal,
		)
	}
	if cov.EntitiesTotal != 1 || cov.EntitiesTouched != 1 {
		t.Errorf("entities coverage = %d/%d, want 1/1", cov.EntitiesTouched, cov.EntitiesTotal)
	}
}

func TestRuleRequirement(t *testing.T) {
	cases := map[string]struct {
		body string
		want string
	}{
		"requirement line": {
			body: locale.For(
				"fr",
			).Requirement + "QUAND a, le systeme doit b.\n\n**Justification** : z",
			want: "QUAND a, le systeme doit b.",
		},
		"requirement after other lines": {
			body: "intro\n" + locale.For("fr").Requirement + "Le systeme doit y.",
			want: "Le systeme doit y.",
		},
		"no requirement line": {body: "first line\nsecond", want: "first line"},
		"empty body":          {body: "", want: ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := graph.RuleRequirement(tc.body); got != tc.want {
				t.Fatalf("RuleRequirement = %q, want %q", got, tc.want)
			}
		})
	}
}

// graph.TestRuleBodyRoundTripsInEveryLanguage builds a rule with each language's
// labels and parses its requirement and criteria back.
func TestRuleBodyRoundTripsInEveryLanguage(t *testing.T) {
	for _, text := range locale.All() {
		t.Run(text.Lang, func(t *testing.T) {
			nodes := graph.Build(nil, fixtureReduced(), extract.DomainResolver{}, text.Lang, nil)
			var rule graph.Node
			for _, n := range nodes {
				if n.Type == graph.TypeRule {
					rule = n
				}
			}
			want := fixtureReduced()[0].Rules[0]
			if got := graph.RuleRequirement(rule.Body); got != want.Requirement {
				t.Errorf(
					"RuleRequirement = %q, want %q\nbody:\n%s",
					got,
					want.Requirement,
					rule.Body,
				)
			}
			if got := graph.RuleCriteria(rule.Body); len(got) != len(want.AcceptanceCriteria) {
				t.Errorf("RuleCriteria = %q, want %d criteria", got, len(want.AcceptanceCriteria))
			}
			if !strings.HasPrefix(rule.Body, text.Requirement) {
				t.Errorf("body does not use %s labels:\n%s", text.Lang, rule.Body)
			}
		})
	}
}

func TestBuildLinksRulesToTheirHandlersEndpoint(t *testing.T) {
	const handlerFile = "app/Services/Billing/ProrataCalculator.php" // fixtureReduced cites it
	route := func(path, handlerLines string) extract.Fact {
		return extract.Fact{
			Kind: extract.KindRoute, ID: "route.any." + path,
			Attrs: map[string]string{
				extract.AttrPath:         path,
				extract.AttrHandlerPath:  handlerFile,
				extract.AttrHandlerLines: handlerLines,
			},
			Source: extract.Ref{Path: "routes.go", Lines: "3-4"},
		}
	}
	cases := map[string]struct {
		handlerLines string
		linked       bool
	}{
		"citation inside the handler":  {"10-30", true},
		"citation overlapping its end": {"20-40", true},
		"handler elsewhere in file":    {"30-40", false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			facts := append(fixtureFacts(), route("/pubsub/x", tc.handlerLines))
			nodes := graph.Build(facts, fixtureReduced(), extract.DomainResolver{}, "fr", nil)
			for _, n := range nodes {
				if n.ID != prorataRule { // cites lines 11-24
					continue
				}
				edge := graph.Edge{Type: graph.EdgeExposedBy, To: "endpoint.pubsub-x"}
				if got := slices.Contains(n.Edges, edge); got != tc.linked {
					t.Fatalf("linked = %v, want %v (edges %v)", got, tc.linked, n.Edges)
				}
				return
			}
			t.Fatal("rule node missing")
		})
	}
}

// TestEndpointIDNamesRoot: the path "/" gets a visible id, with or
// without a verb; other paths keep theirs.
func TestEndpointIDNamesRoot(t *testing.T) {
	route := func(method, path string) extract.Fact {
		return extract.Fact{
			Kind:  extract.KindRoute,
			Attrs: map[string]string{extract.AttrMethod: method, extract.AttrPath: path},
		}
	}
	cases := map[string]extract.Fact{
		"endpoint.root":         route("", "/"),
		"endpoint.get-root":     route("GET", "/"),
		"endpoint.safelist-add": route("", "/safelist/add"),
	}
	for want, f := range cases {
		if got := graph.EndpointID(&f); got != want {
			t.Errorf("EndpointID(%v) = %q, want %q", f.Attrs, got, want)
		}
	}
}

// TestBuildMergesCollidingEndpoints: routes whose ids collide share one
// endpoint node that cites both routes and lists both paths.
func TestBuildMergesCollidingEndpoints(t *testing.T) {
	const server = "cmd/webserver/webserver.go"
	route := func(path, line string) extract.Fact {
		return extract.Fact{
			Kind: extract.KindRoute,
			ID:   "route.any." + path,
			Attrs: map[string]string{
				extract.AttrPath: path, "handler": "backend.AuthServer.LoginStaticHandler",
			},
			Source: extract.Ref{Path: server, Lines: line},
		}
	}
	facts := []extract.Fact{route("/login/", "412-412"), route("/login", "411-411")}
	var endpoints []graph.Node
	for _, n := range graph.Build(facts, nil, extract.DomainResolver{}, "en", nil) {
		if n.Type == "endpoint" {
			endpoints = append(endpoints, n)
		}
	}
	if len(endpoints) != 1 {
		t.Fatalf("got %d endpoint nodes, want 1: %+v", len(endpoints), endpoints)
	}
	got := endpoints[0]
	if got.ID != "endpoint.login" || got.Title != " /login, /login/" {
		t.Errorf("id %q title %q", got.ID, got.Title)
	}
	wantSources := []extract.Ref{
		{Path: server, Lines: "411-411"},
		{Path: server, Lines: "412-412"},
	}
	if !reflect.DeepEqual(got.Sources, wantSources) {
		t.Errorf("sources %v, want %v", got.Sources, wantSources)
	}
	if strings.Count(got.Body, "LoginStaticHandler") != 1 {
		t.Errorf("handler not listed once: %q", got.Body)
	}
}
