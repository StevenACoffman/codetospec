package position_test

import (
	"strings"
	"testing"

	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/graph"
	"github.com/StevenACoffman/codetospec/internal/position"
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
		position.Technical:    {"internal/middleware", "cmd/..."},
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
		"another root is unmapped": {[]extract.Ref{{Root: "pkg", Path: "cmd/x.go"}}, ""},
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
// the positional one; its own nature is kept, and the input untouched.
func TestAnnotate(t *testing.T) {
	m, err := position.Parse(map[string][]string{position.Technical: {"internal/middleware"}})
	if err != nil {
		t.Fatal(err)
	}
	rule := func(id, nature, file string) graph.Node {
		return graph.Node{
			ID: id, Type: graph.TypeRule, Sources: cites(file),
			Extra: map[string]string{"nature": nature},
		}
	}
	in := []graph.Node{
		rule("rule.a.disagrees", position.Business, "internal/middleware/auth.go"),
		rule("rule.a.agrees", position.Technical, "internal/middleware/auth.go"),
		rule("rule.a.unmapped", position.Business, "internal/billing/pay.go"),
	}
	out := position.Annotate(in, &m)
	want := map[string]string{"rule.a.disagrees": position.Technical}
	for i := range out {
		got := out[i].Extra[graph.ExtraNatureByPosition]
		if got != want[out[i].ID] {
			t.Errorf("%s: nature_by_position = %q, want %q", out[i].ID, got, want[out[i].ID])
		}
		if out[i].Extra["nature"] != in[i].Extra["nature"] {
			t.Errorf("%s: nature changed", out[i].ID)
		}
	}
	if _, set := in[0].Extra[graph.ExtraNatureByPosition]; set {
		t.Error("the input node was modified")
	}
}
