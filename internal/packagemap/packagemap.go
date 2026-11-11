// Package packagemap draws the code's package dependencies: what the
// import facts say, nothing inferred. It is the code's structure, not
// its business domains (those are the concept map's): a package is a
// unit of code, whatever it means.
//
// A package is a package fact's import path when an extractor emits them
// (Go: an import names that exact path), else a module fact's name (a
// namespace: an import names a class under it, so the longest package
// prefixing the target wins). Packages of a shared root are drawn as
// external, and only those the code imports. Labels drop the prefix all
// packages share ("github.com/Khan/districts-jobs/"), so the code's own
// and the shared packages read apart: roster/pkg, pkg/errors.
package packagemap

import (
	"maps"
	"slices"
	"strings"

	"github.com/StevenACoffman/codetospec/internal/d2"
	"github.com/StevenACoffman/codetospec/internal/extract"
)

// pkg is one package drawn.
type pkg struct {
	name   string
	shared bool
}

// Build is the package dependency diagram of facts: a node per package,
// an edge per package importing another (each pair once), the packages
// no import connects gathered in one grid labelled isolatedLabel.
//
// Ensures: deterministic; nodes sorted by name, edges by importer then
// target.
func Build(facts []extract.Fact, isolatedLabel string) *d2.Diagram {
	pkgs, importerOf := packages(facts)
	targets := slices.Sorted(maps.Keys(pkgs))

	edges := map[[2]string]bool{}
	for i := range facts {
		f := &facts[i]
		if f.Kind != extract.KindImport || f.Source.Root != "" {
			continue
		}
		from := importerOf(f)
		to := resolve(targets, f.Attrs[extract.AttrTarget])
		if from != "" && to != "" && from != to {
			edges[[2]string{from, to}] = true
		}
	}

	imported := map[string]bool{}
	d := &d2.Diagram{}
	for _, e := range slices.SortedFunc(maps.Keys(edges), compareEdges) {
		d.Edges = append(d.Edges, d2.Edge{From: e[0], To: e[1]})
		imported[e[1]] = true
	}
	prefix := commonPrefix(targets)
	for _, name := range targets {
		p := pkgs[name]
		if p.shared && !imported[name] {
			continue
		}
		class := d2.ClassStep
		if p.shared {
			class = d2.ClassExternal
		}
		d.Nodes = append(d.Nodes, d2.Node{ID: name, Label: label(name, prefix), Class: class})
	}
	return d2.GridIsolated(d, isolatedLabel)
}

// packages are facts' packages by name, and how to find the package an
// import fact's file belongs to: the package fact citing a file of its
// directory, or the module its namespace names.
func packages(facts []extract.Fact) (map[string]pkg, func(*extract.Fact) string) {
	pkgs := map[string]pkg{}
	byDir := map[string]string{}
	for i := range facts {
		f := &facts[i]
		if name := f.Attrs[extract.AttrName]; f.Kind == extract.KindPackage && name != "" {
			pkgs[name] = pkg{name: name, shared: f.Source.Root != ""}
			if f.Source.Root == "" {
				byDir[dir(f.Source.Path)] = name
			}
		}
	}
	if len(pkgs) > 0 {
		return pkgs, func(f *extract.Fact) string { return byDir[dir(f.Source.Path)] }
	}
	for i := range facts {
		f := &facts[i]
		if name := f.Attrs[extract.AttrName]; f.Kind == extract.KindModule && name != "" {
			pkgs[name] = pkg{name: name}
		}
	}
	return pkgs, func(f *extract.Fact) string {
		if _, ok := pkgs[f.Attrs[extract.AttrNamespace]]; ok {
			return f.Attrs[extract.AttrNamespace]
		}
		return ""
	}
}

// resolve is the package an import target names: the one of that exact
// name, else the longest whose name prefixes it segment-wise (a class
// under a namespace); "" when none.
func resolve(names []string, target string) string {
	best := ""
	for _, name := range names {
		if name == target {
			return name
		}
		if len(name) > len(best) && strings.HasPrefix(target, name) &&
			strings.ContainsRune(`/\.`, rune(target[len(name)])) {
			best = name
		}
	}
	return best
}

// commonPrefix is the longest prefix of every name ending at a separator
// ("/" or "\"): a lone name's own directory; "" for none.
func commonPrefix(names []string) string {
	if len(names) == 0 {
		return ""
	}
	prefix := names[0]
	for _, name := range names[1:] {
		for !strings.HasPrefix(name, prefix) {
			prefix = prefix[:len(prefix)-1]
		}
	}
	if i := strings.LastIndexAny(prefix, `/\`); i >= 0 {
		return prefix[:i+1]
	}
	return ""
}

// label is name without prefix; the whole name when that leaves nothing
// (the package that is the prefix itself).
func label(name, prefix string) string {
	if rest := strings.TrimPrefix(name, prefix); rest != "" {
		return rest
	}
	return name
}

// dir is a slash path's directory, "" for a file at the root.
func dir(path string) string {
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[:i]
	}
	return ""
}

func compareEdges(a, b [2]string) int {
	if c := strings.Compare(a[0], b[0]); c != 0 {
		return c
	}
	return strings.Compare(a[1], b[1])
}
