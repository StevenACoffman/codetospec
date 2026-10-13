package extract_test

import (
	"testing"

	"codetospec/internal/extract"
)

func module(name, path string) extract.Fact {
	return extract.Fact{
		Kind:   "module",
		Attrs:  map[string]string{"name": name},
		Source: extract.Ref{Path: path, Lines: "1-1"},
	}
}

func TestAmbiguousNamespacesResolveByDirectory(t *testing.T) {
	facts := []extract.Fact{
		module("main", "cmd/seedroster/main.go"),
		module("main", "cmd/deduper/main.go"),
		module("main", "cmd/deduper/flags.go"),
		module("models", "models/user.go"),
		module("models", "models/district.go"),
	}
	r := extract.DomainResolver{Strategy: "auto", Ambiguous: extract.AmbiguousNamespaces(facts)}
	cases := map[string]struct {
		namespace, path, want string
	}{
		"command packages split by directory": {"main", "cmd/seedroster/main.go", "cmd-seedroster"},
		"same command, other file":            {"main", "cmd/deduper/flags.go", "cmd-deduper"},
		"one-directory namespace unchanged":   {"models", "models/user.go", "models"},
		"no namespace falls back to path":     {"", "tasks/nickname.go", "tasks"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := r.Resolve(tc.namespace, tc.path); got != tc.want {
				t.Fatalf("Resolve(%q, %q) = %q, want %q", tc.namespace, tc.path, got, tc.want)
			}
		})
	}
}
