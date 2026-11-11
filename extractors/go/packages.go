package main

import (
	"sort"

	"golang.org/x/tools/go/packages"
)

// kindPackage is the core's extract.KindPackage: one package, named by
// its import path.
const kindPackage = "package"

// PackageFacts lists a package fact per package of the analyzed module,
// and per package of a shared root that one of them imports: its import
// path (attrName), citing its first Go file at line 1, under the shared
// root for a shared one. An import fact names its target by that path,
// so the core resolves an import exactly, whatever the package is called
// (cmd/pull-topic holds package pull_topic) and wherever it lives.
//
// Ensures: one fact per import path, sorted by id; a package with no Go
// file has none.
func PackageFacts(pkgs []*packages.Package, root string, shared []SharedRoot) []Fact {
	byID := map[string]Fact{}
	add := func(p *packages.Package, dir, rootName string) {
		if len(p.GoFiles) == 0 {
			return
		}
		file, err := relTo(dir, p.GoFiles[0])
		if err != nil {
			return
		}
		id := kindPackage + "." + p.PkgPath
		byID[id] = Fact{
			Kind: kindPackage, ID: id, Attrs: map[string]string{attrName: p.PkgPath},
			Source: Ref{Path: file, Lines: "1-1", Root: rootName},
			Origin: origin, Certainty: certain,
		}
	}
	for _, p := range pkgs {
		add(p, root, "")
		for _, imported := range p.Imports {
			for i := range shared {
				if shared[i].holds(imported.PkgPath) {
					add(imported, shared[i].Dir, shared[i].Name)
				}
			}
		}
	}
	facts := make([]Fact, 0, len(byID))
	for _, f := range byID {
		facts = append(facts, f)
	}
	sort.Slice(facts, func(i, j int) bool { return facts[i].ID < facts[j].ID })
	return facts
}
