package extract_test

import (
	"testing"

	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/sitter"
)

// parsed returns the tree-sitter information of each Go file, as the
// pipeline's extract step builds it.
func parsed(t *testing.T, files map[string]string) map[string]*sitter.FileInfo {
	t.Helper()
	infos := make(map[string]*sitter.FileInfo, len(files))
	for path, src := range files {
		info, err := sitter.NewParser().Parse("go", []byte(src))
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		infos[path] = info
	}
	return infos
}

// goMain is the package name of every Go command.
const goMain = "main"

func TestAmbiguousNamespacesResolveByDirectory(t *testing.T) {
	infos := parsed(t, map[string]string{
		"cmd/seedroster/main.go": "package main\n\nfunc main() {}\n",
		"cmd/deduper/main.go":    "package main\n\nfunc main() {}\n",
		"cmd/deduper/flags.go":   "package main\n\nvar dryRun bool\n",
		"models/user.go":         "package models\n\ntype User struct{}\n",
		"models/district.go":     "package models\n\ntype District struct{}\n",
	})
	// The module facts of these files merge into one per name: they alone
	// could not show that "main" spans directories.
	var facts [][]extract.Fact
	for path, info := range infos {
		facts = append(facts, extract.UniversalFacts(path, info))
	}
	mains := 0
	for _, f := range extract.Merge(facts...) {
		if f.Kind == "module" && f.Attrs["name"] == goMain {
			mains++
		}
	}
	if mains != 1 {
		t.Fatalf("merged facts hold %d main modules; the test premise expects 1", mains)
	}

	r := extract.DomainResolver{Strategy: "auto", Ambiguous: extract.AmbiguousNamespaces(infos)}
	cases := map[string]struct {
		namespace, path, want string
	}{
		"command packages split by directory": {goMain, "cmd/seedroster/main.go", "cmd-seedroster"},
		"same command, other file":            {goMain, "cmd/deduper/flags.go", "cmd-deduper"},
		"one-directory namespace unchanged":   {modelsDomain, "models/user.go", modelsDomain},
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
