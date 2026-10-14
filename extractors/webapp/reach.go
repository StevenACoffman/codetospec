package main

import (
	"go/ast"
	"path"
	"strconv"
	"strings"
)

// declKey names a package-level function or variable of the shared code:
// its package directory and name (pkg/web/serve, HandleGraphQL).
type declKey struct{ dir, name string }

// reached returns the shared functions and variables the service's code
// reaches: those it names — called or passed as values, as webapp passes
// middlewares — and, in turn, those they name, across packages. Methods
// are not followed (x.M() names no package), and neither are interfaces:
// what is reached is a lower bound, never a guess. This is what makes a
// shared rule the service's: the gateway imports serve but never reaches
// its GraphQL middleware.
func reached(service []source, shared sharedCode) map[declKey]bool {
	decls := indexDecls(shared.sources)
	names := packageNames(shared.sources)
	found := make(map[declKey]bool)
	var queue []declKey
	visit := func(s *source, n ast.Node) {
		imports := importDirs(s.file, shared.module, names)
		for _, k := range references(n, path.Dir(s.name), imports, decls) {
			if !found[k] {
				found[k] = true
				queue = append(queue, k)
			}
		}
	}
	for i := range service {
		visit(&service[i], service[i].file)
	}
	for len(queue) > 0 {
		k := queue[0]
		queue = queue[1:]
		d := decls[k]
		visit(d.file, d.node)
	}
	return found
}

// sharedDecl is the code of a shared declaration and its file.
type sharedDecl struct {
	file *source
	node ast.Node // a function's declaration, a variable's value
}

// indexDecls indexes the package functions and variables of sources.
func indexDecls(sources []source) map[declKey]sharedDecl {
	decls := make(map[declKey]sharedDecl)
	for i := range sources {
		s := &sources[i]
		dir := path.Dir(s.name)
		for _, fn := range funcDecls(s.file) {
			if fn.Recv == nil {
				decls[declKey{dir, fn.Name.Name}] = sharedDecl{file: s, node: fn}
			}
		}
		for _, v := range packageVars(s.file) {
			decls[declKey{dir, v.name}] = sharedDecl{file: s, node: v.value}
		}
	}
	return decls
}

// packageNames maps each shared package directory to its package name.
func packageNames(sources []source) map[string]string {
	names := make(map[string]string)
	for i := range sources {
		names[path.Dir(sources[i].name)] = sources[i].file.Name.Name
	}
	return names
}

// importDirs maps the names file refers to shared packages by (its alias,
// else the package's name) to their directories.
func importDirs(file *ast.File, module string, names map[string]string) map[string]string {
	dirs := make(map[string]string)
	for _, spec := range file.Imports {
		imported, err := strconv.Unquote(spec.Path.Value)
		if err != nil || !strings.HasPrefix(imported, module+"/") {
			continue
		}
		dir := strings.TrimPrefix(imported, module+"/")
		name := names[dir]
		if spec.Name != nil {
			name = spec.Name.Name
		}
		if name != "" {
			dirs[name] = dir
		}
	}
	return dirs
}

// references lists the shared declarations n names: Name of its own
// package (dir), and pkg.Name through imports.
func references(
	n ast.Node,
	dir string,
	imports map[string]string,
	decls map[declKey]sharedDecl,
) []declKey {
	var found []declKey
	ast.Inspect(n, func(node ast.Node) bool {
		switch x := node.(type) {
		case *ast.SelectorExpr:
			if q, ok := x.X.(*ast.Ident); ok {
				if d, isImport := imports[q.Name]; isImport {
					if k := (declKey{d, x.Sel.Name}); decls[k].node != nil {
						found = append(found, k)
					}
					return false
				}
			}
		case *ast.Ident:
			if k := (declKey{dir, x.Name}); decls[k].node != nil {
				found = append(found, k)
			}
		}
		return true
	})
	return found
}
