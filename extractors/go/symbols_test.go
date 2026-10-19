package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// kindFunction is the symbol kind of a plain function.
const kindFunction = "function"

// TestSymbolAndCallFacts analyzes the shared module of the test workspace
// as a module of its own.
func TestSymbolAndCallFacts(t *testing.T) {
	root := filepath.Join(writeWorkspace(t), "shared")
	pkgs, fset, ok := loadPackages(root, func(format string, a ...any) { t.Logf(format, a...) })
	if !ok {
		t.Fatal("module did not load")
	}

	kinds := map[string]string{}
	for _, f := range SymbolFacts(pkgs, fset, root) {
		start, _, _ := strings.Cut(f.Source.Lines, "-")
		if f.Attrs["precise"] != "true" ||
			f.ID != "symbol."+f.Source.Path+"#"+f.Attrs["name"]+":"+start {
			t.Errorf("symbol fact %+v: id or precision wrong", f)
		}
		kinds[f.Attrs["name"]] = f.Attrs["kind"]
	}
	symbols := map[string]string{
		"Account": "class", "Close": "method", "check": kindFunction,
		"Open": kindFunction, "Unused": kindFunction,
		"Limit": "", "errEmpty": "", // values are not symbols
	}
	for name, want := range symbols {
		t.Run("symbol "+name, func(t *testing.T) {
			if kinds[name] != want {
				t.Errorf("kind = %q, want %q", kinds[name], want)
			}
		})
	}

	calls := map[string]bool{}
	for _, f := range CallFacts(pkgs, fset, root) {
		calls[f.Source.Lines+">"+f.Attrs["callee_lines"]] = true
	}
	cases := map[string]struct {
		pair string // caller lines > callee lines
		want bool
	}{
		"method calls a function":   {"12-12>14-19", true},
		"function builds a type":    {"24-24>9-9", true},
		"signature use is not call": {"12-12>9-9", false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if calls[tc.pair] != tc.want {
				t.Errorf(
					"call %s = %v, want %v (calls: %v)",
					tc.pair,
					calls[tc.pair],
					tc.want,
					calls,
				)
			}
		})
	}
}
