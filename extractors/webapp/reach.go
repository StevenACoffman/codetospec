package main

import (
	"go/ast"
	"go/token"
	"path"
	"strconv"
	"strings"
)

// declKey names a package-level function or variable of the shared code:
// its package directory and name (pkg/web/serve, HandleGraphQL).
type declKey struct{ dir, name string }

// reached returns the shared declarations the service's code reaches:
// the functions, variables and types it names — called, passed as values,
// as webapp passes middlewares, or used as types — and, in turn, those
// they name, across packages. A reached type reaches its methods: x.M()
// names no package, but x's type was named where x was built or received.
// Interfaces are not followed to their implementations: what is reached
// is a lower bound, never a guess. This is what makes a shared rule the
// service's: the gateway imports serve but never reaches its GraphQL
// middleware.
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
		for _, m := range d.methods {
			if !found[m] {
				found[m] = true
				queue = append(queue, m)
			}
		}
	}
	return found
}

// sharedDecl is the code of a shared declaration and its file.
type sharedDecl struct {
	file    *source
	node    ast.Node  // a function's declaration, a variable's value, a type's spec
	methods []declKey // a type's methods, keyed "Type.Method"
}

// indexDecls indexes the package functions, variables, types and methods
// of sources; a type lists its methods.
func indexDecls(sources []source) map[declKey]sharedDecl {
	decls := make(map[declKey]sharedDecl)
	var methods []declKey
	for i := range sources {
		s := &sources[i]
		dir := path.Dir(s.name)
		for _, fn := range funcDecls(s.file) {
			name := fn.Name.Name
			if fn.Recv != nil {
				recv := receiverName(fn)
				if recv == "" {
					continue // a generic receiver: not followed
				}
				name = recv + "." + name
				methods = append(methods, declKey{dir, name})
			}
			decls[declKey{dir, name}] = sharedDecl{file: s, node: fn}
		}
		for _, v := range packageVars(s.file) {
			decls[declKey{dir, v.name}] = sharedDecl{file: s, node: v.value}
		}
		for _, t := range typeSpecs(s.file) {
			decls[declKey{dir, t.Name.Name}] = sharedDecl{file: s, node: t}
		}
	}
	for _, m := range methods {
		typeName, _, _ := strings.Cut(m.name, ".")
		if t, ok := decls[declKey{m.dir, typeName}]; ok {
			t.methods = append(t.methods, m)
			decls[declKey{m.dir, typeName}] = t
		}
	}
	return decls
}

// typeSpecs lists file's type declarations.
func typeSpecs(file *ast.File) []*ast.TypeSpec {
	var specs []*ast.TypeSpec
	for _, decl := range file.Decls {
		if gen, ok := decl.(*ast.GenDecl); ok && gen.Tok == token.TYPE {
			for _, spec := range gen.Specs {
				if t, isType := spec.(*ast.TypeSpec); isType {
					specs = append(specs, t)
				}
			}
		}
	}
	return specs
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

// reachFacts lists, as reach facts cited under pkgRoot, the shared
// declarations the service reaches: functions and methods, variables and
// types. codetospec inherits the pkg/ spec's rules citing them
// (codetospec run --inherit pkg=<spec> --ref-root pkg=<checkout>/pkg).
func reachFacts(shared sharedCode, reach map[declKey]bool) []Fact {
	var facts []Fact
	for i := range shared.sources {
		s := &shared.sources[i]
		dir := path.Dir(s.name)
		add := func(name string, n ast.Node) {
			if !reach[declKey{dir, name}] {
				return
			}
			ref := underPkg(Ref{
				Path:  s.name,
				Lines: lines(s.fset.Position(n.Pos()).Line, s.fset.Position(n.End()).Line),
			})
			facts = append(facts, Fact{
				Kind:      kindReach,
				ID:        "reach." + pkgRoot + "." + ref.Path + "#" + name,
				Attrs:     map[string]string{attrSymbol: s.file.Name.Name + "." + name},
				Source:    ref,
				Origin:    origin,
				Certainty: certain,
			})
		}
		for _, fn := range funcDecls(s.file) {
			if fn.Recv == nil {
				add(fn.Name.Name, fn)
			} else if recv := receiverName(fn); recv != "" {
				add(recv+"."+fn.Name.Name, fn)
			}
		}
		for _, v := range packageVars(s.file) {
			add(v.name, v.value)
		}
		for _, t := range typeSpecs(s.file) {
			add(t.Name.Name, t)
		}
	}
	return facts
}
