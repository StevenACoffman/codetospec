package position_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/graph"
	"github.com/StevenACoffman/codetospec/internal/position"
	"github.com/StevenACoffman/codetospec/internal/quality"
)

// Names the tests share: a technical layer's package pattern, an entity
// rules touch, and the root of shared platform code.
const (
	middleware     = "internal/middleware"
	invoicesEntity = "entity.invoices"
	platformRoot   = "pkg"
)

// cites is the sources of a rule citing paths under --src.
func cites(paths ...string) []extract.Ref {
	refs := make([]extract.Ref, 0, len(paths))
	for _, p := range paths {
		refs = append(refs, extract.Ref{Path: p, Lines: "1-2"})
	}
	return refs
}

// TestOf: the nature every cited file's layer agrees on; a package
// pattern holds its directory alone, a "..." one its tree; the most
// specific pattern wins; a file in no layer, disagreeing layers, or
// another root say nothing.
func TestOf(t *testing.T) {
	m, err := position.Parse(map[string][]string{
		position.Technical:    {middleware, "cmd/..."},
		position.Presentation: {"internal/render/..."},
		position.Business:     {"internal/render/rules"},
	})
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]struct {
		sources []extract.Ref
		want    string
	}{
		"exact package":        {cites("internal/middleware/auth.go"), position.Technical},
		"not its subdirectory": {cites("internal/middleware/x/y.go"), ""},
		"recursive tree":       {cites("cmd/codetospec/main.go"), position.Technical},
		"most specific wins":   {cites("internal/render/rules/rule.go"), position.Business},
		"in no layer":          {cites("internal/billing/pay.go"), ""},
		"all citations agree": {
			cites("cmd/a/a.go", "internal/middleware/m.go"),
			position.Technical,
		},
		"citations disagree":       {cites("cmd/a/a.go", "internal/render/r.go"), ""},
		"another root is unmapped": {[]extract.Ref{{Root: platformRoot, Path: "cmd/x.go"}}, ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := m.Of(tc.sources); got != tc.want {
				t.Errorf("Of = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestParseRejects: an unknown nature or an empty pattern is an error.
func TestParseRejects(t *testing.T) {
	cases := map[string]map[string][]string{
		"unknown nature": {"infrastructure": {"internal/infra"}},
		"empty pattern":  {position.Technical: {"  "}},
	}
	for name, byNature := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := position.Parse(byNature); err == nil {
				t.Error("Parse accepted it")
			}
		})
	}
}

// anarchModel is an anarch.json with two layers, one pattern outside the
// module.
const anarchModel = `{"version": 1, "layers": [
	{"name": "adapter", "packages": ["example.com/app/internal/http", "example.com/app/cmd/..."]},
	{"name": "domain", "packages": ["example.com/app", "github.com/other/lib"]}
]}`

// TestFromAnarch: a layer's import path patterns become paths under the
// module; an unmapped layer and a pattern outside the module are left out;
// a mapped layer the model lacks is an error.
func TestFromAnarch(t *testing.T) {
	m, err := position.FromAnarch([]byte(anarchModel), "example.com/app",
		map[string]string{"adapter": position.Technical, "domain": position.Business})
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]struct {
		file, want string
	}{
		"exact package":       {"internal/http/server.go", position.Technical},
		"recursive":           {"cmd/app/main.go", position.Technical},
		"module root package": {"app.go", position.Business},
		"elsewhere":           {"internal/billing/pay.go", ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := m.Of(cites(tc.file)); got != tc.want {
				t.Errorf("Of(%s) = %q, want %q", tc.file, got, tc.want)
			}
		})
	}

	_, err = position.FromAnarch([]byte(anarchModel), "example.com/app",
		map[string]string{"infra": position.Technical})
	if err == nil || !strings.Contains(err.Error(), `no layer "infra"`) {
		t.Errorf("mapping a missing layer: err = %v", err)
	}
}

// TestAnnotate: only a rule whose position contradicts its nature records
// the positional one, with its structural evidence; its own nature is
// kept, and the input untouched. Each layer counts its rules, their
// disagreements and the questions among them.
func TestAnnotate(t *testing.T) {
	m, err := position.Parse(map[string][]string{
		position.Technical: {middleware},
		position.Business:  {"billing/..."},
	})
	if err != nil {
		t.Fatal(err)
	}
	rule := func(id, nature, file string, edges ...graph.Edge) graph.Node {
		return graph.Node{
			ID: id, Type: graph.TypeRule, Sources: cites(file), Edges: edges,
			Extra: map[string]string{"nature": nature},
		}
	}
	touches := graph.Edge{Type: graph.EdgeTouches, To: invoicesEntity}
	backedByCondition := rule("rule.b.condition", position.Technical, "billing/pay.go")
	backedByCondition.Extra[graph.ExtraCondition] = `{"var":"invoices.total","op":"greater_than","value":0}`
	technicalQuality := rule("rule.a.quality", position.Business, "internal/middleware/auth.go")
	technicalQuality.Extra[graph.ExtraQualityAttribute] = "reliability"
	in := []graph.Node{
		{ID: invoicesEntity, Type: graph.TypeEntity, Title: "invoices"},
		rule("rule.a.disagrees", position.Business, "internal/middleware/auth.go", touches),
		rule("rule.a.agrees", position.Technical, "internal/middleware/auth.go"),
		rule("rule.a.unmapped", position.Business, "internal/other/pay.go"),
		technicalQuality,
		backedByCondition,
		rule("rule.b.none", position.Technical, "billing/x/refund.go"),
	}
	out, layers := position.Annotate(in, &m, nil)
	want := map[string]struct {
		positional, evidence string
		asks                 bool
	}{
		"rule.a.disagrees": {position.Technical, "business: touches entity.invoices", false},
		"rule.a.quality":   {position.Technical, "technical: quality_attribute reliability", true},
		"rule.b.condition": {position.Business, "business: condition invoices.total", true},
		"rule.b.none":      {position.Business, "", false},
	}
	for i := range out {
		w := want[out[i].ID]
		if got := out[i].Extra[graph.ExtraNatureByPosition]; got != w.positional {
			t.Errorf("%s: nature_by_position = %q, want %q", out[i].ID, got, w.positional)
		}
		if got := out[i].Extra[graph.ExtraNatureEvidence]; got != w.evidence {
			t.Errorf("%s: nature_evidence = %q, want %q", out[i].ID, got, w.evidence)
		}
		if got := position.Asks(&out[i]); got != w.asks {
			t.Errorf("%s: Asks = %v, want %v", out[i].ID, got, w.asks)
		}
		if out[i].Extra["nature"] != in[i].Extra["nature"] {
			t.Errorf("%s: nature changed", out[i].ID)
		}
	}
	if _, set := in[1].Extra[graph.ExtraNatureByPosition]; set {
		t.Error("the input node was modified")
	}
	wantLayers := []position.Layer{
		{
			Pattern: middleware, Nature: position.Technical,
			Placed: 3, Disagree: 2, Asked: 1,
		},
		{
			Pattern: "billing/...", Nature: position.Business,
			Placed: 2, Disagree: 2, Asked: 1,
		},
	}
	if !slices.Equal(layers, wantLayers) {
		t.Errorf("layers = %+v, want %+v", layers, wantLayers)
	}
}

// TestAnnotateWithoutLayers: an empty Map leaves the nodes and lists no
// layer.
func TestAnnotateWithoutLayers(t *testing.T) {
	in := []graph.Node{{ID: "rule.a.b", Type: graph.TypeRule, Sources: cites("a/b.go")}}
	out, layers := position.Annotate(in, &position.Map{}, nil)
	if len(layers) != 0 || out[0].Extra != nil {
		t.Errorf("layers = %v, extra = %v", layers, out[0].Extra)
	}
}

// TestAnnotateNatureByEvidence: the nature a rule's evidence alone gives,
// layers or not: business or technical when only one kind is present,
// open when both are, none without evidence; platform code (inherited, or
// cited only in another root) is technical evidence; access is the facet
// of a security rule that checks something.
func TestAnnotateNatureByEvidence(t *testing.T) {
	touches := graph.Edge{Type: graph.EdgeTouches, To: invoicesEntity}
	pkg := []extract.Ref{{Path: "lib/retry.go", Lines: "1-2", Root: platformRoot}}
	cases := map[string]struct {
		edges    []graph.Edge
		sources  []extract.Ref
		extra    map[string]string
		evidence string
		nature   string
		facets   string
	}{
		"none": {},
		"business": {
			edges:    []graph.Edge{touches},
			evidence: "business: touches entity.invoices",
			nature:   position.Business,
		},
		"technical quality": {
			extra:    map[string]string{graph.ExtraQualityAttribute: "reliability"},
			evidence: "technical: quality_attribute reliability",
			nature:   position.Technical,
		},
		"both": {
			edges:    []graph.Edge{touches},
			extra:    map[string]string{graph.ExtraQualityAttribute: "performance_efficiency"},
			evidence: "business: touches entity.invoices; technical: quality_attribute performance_efficiency",
			nature:   position.Open,
		},
		"inherited": {
			sources:  pkg,
			extra:    map[string]string{graph.ExtraInheritedFrom: platformRoot},
			evidence: "technical: inherited_from pkg",
			nature:   position.Technical,
		},
		"other root only": {
			sources:  pkg,
			evidence: "technical: root pkg",
			nature:   position.Technical,
		},
		"root and src": {
			sources: append(cites("billing/pay.go"), pkg...),
		},
		"access, mechanical": {
			extra: map[string]string{
				graph.ExtraQualityAttribute: quality.Security, graph.ExtraExtractedBy: "webapp",
			},
			facets: position.FacetAccess,
		},
		"access, condition": {
			extra: map[string]string{
				graph.ExtraQualityAttribute: quality.Security,
				graph.ExtraCondition:        `{"var":"user.kaid","op":"equal","value":""}`,
			},
			facets: position.FacetAccess,
		},
		"security, no check": {
			extra: map[string]string{graph.ExtraQualityAttribute: quality.Security},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			sources := tc.sources
			if sources == nil {
				sources = cites("billing/pay.go")
			}
			rule := graph.Node{
				ID: "rule.billing.x", Type: graph.TypeRule, Edges: tc.edges,
				Sources: sources, Extra: tc.extra,
			}
			out, _ := position.Annotate([]graph.Node{rule}, &position.Map{}, nil)
			got := out[0].Extra
			if got[graph.ExtraNatureEvidence] != tc.evidence {
				t.Errorf(
					"nature_evidence = %q, want %q",
					got[graph.ExtraNatureEvidence],
					tc.evidence,
				)
			}
			if got[graph.ExtraNatureByEvidence] != tc.nature {
				t.Errorf(
					"nature_by_evidence = %q, want %q",
					got[graph.ExtraNatureByEvidence],
					tc.nature,
				)
			}
			if got[graph.ExtraFacets] != tc.facets {
				t.Errorf("facets = %q, want %q", got[graph.ExtraFacets], tc.facets)
			}
		})
	}
}
