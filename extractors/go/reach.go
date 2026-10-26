package main

import (
	"go/ast"
	"go/token"
	"go/types"
	"path"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
)

// SharedRoot is a tree of code other modules build on (a workspace's
// pkg/), named as codetospec's --ref-root names it.
type SharedRoot struct {
	Name   string // the root's name: reach facts cite their code "@name/path"
	Dir    string // absolute directory of the shared module
	Module string // its Go module path
}

// holds reports whether a package path belongs to the shared module.
func (s *SharedRoot) holds(pkgPath string) bool {
	return pkgPath == s.Module || strings.HasPrefix(pkgPath, s.Module+"/")
}

// ReachFacts lists the shared declarations the analyzed module reaches,
// as reach facts citing each declaration in the shared tree: what the
// module's code names (calls, values, types, constants) and, in turn, what
// those declarations name. A reached named type reaches its methods, since
// a method call names no package but the type was named where its value
// was built or received. Interfaces are not followed to their
// implementations: what is reached is a lower bound, never a guess.
//
// Requires: pkgs are the module's packages, loaded with dependencies, syntax
// and type information (the shared module among the dependencies).
// Ensures:  one fact per reached declaration, sorted by id; declarations of
// the module itself are not facts.
func ReachFacts(pkgs []*packages.Package, fset *token.FileSet, shared *SharedRoot) []Fact {
	decls := sharedDecls(pkgs, shared)
	reached := map[types.Object]bool{}
	var queue []types.Object
	mark := func(obj types.Object) {
		obj = genericOrigin(obj)
		if _, isDecl := decls[obj]; isDecl && !reached[obj] {
			reached[obj] = true
			queue = append(queue, obj)
		}
	}
	for _, p := range pkgs {
		if p.TypesInfo == nil || shared.holds(p.PkgPath) {
			continue
		}
		for _, obj := range p.TypesInfo.Uses {
			mark(obj)
		}
	}
	for len(queue) > 0 {
		obj := queue[0]
		queue = queue[1:]
		d := decls[obj]
		ast.Inspect(d.node, func(n ast.Node) bool {
			if id, isIdent := n.(*ast.Ident); isIdent {
				mark(d.info.Uses[id])
			}
			return true
		})
		for _, m := range methodsOf(obj) {
			mark(m)
		}
	}

	facts := make([]Fact, 0, len(reached))
	for obj := range reached {
		facts = append(facts, decls[obj].fact(obj, fset, shared))
	}
	sort.Slice(facts, func(i, j int) bool { return facts[i].ID < facts[j].ID })
	return facts
}

// sharedDecl is a package-level declaration of the shared module: its node
// (a function, a value spec, a type spec) and the type information that
// resolves the names inside it.
type sharedDecl struct {
	node ast.Node
	info *types.Info
}

// sharedDecls indexes the package-level declarations of every shared
// package among pkgs and their dependencies.
func sharedDecls(pkgs []*packages.Package, shared *SharedRoot) map[types.Object]sharedDecl {
	decls := map[types.Object]sharedDecl{}
	packages.Visit(pkgs, nil, func(p *packages.Package) {
		if p.TypesInfo == nil || !shared.holds(p.PkgPath) {
			return
		}
		for _, file := range p.Syntax {
			for _, decl := range file.Decls {
				for _, d := range declared(decl) {
					if obj := p.TypesInfo.Defs[d.name]; obj != nil {
						decls[obj] = sharedDecl{node: d.node, info: p.TypesInfo}
					}
				}
			}
		}
	})
	return decls
}

// declaredName is a node a top-level declaration declares, with the name
// defining it.
type declaredName struct {
	node ast.Node
	name *ast.Ident
}

// declared lists what a top-level declaration declares: a function, each
// type spec, each name of each value spec.
func declared(decl ast.Decl) []declaredName {
	if fn, isFunc := decl.(*ast.FuncDecl); isFunc {
		return []declaredName{{fn, fn.Name}}
	}
	gen, isGen := decl.(*ast.GenDecl)
	if !isGen {
		return nil
	}
	var names []declaredName
	for _, spec := range gen.Specs {
		switch s := spec.(type) {
		case *ast.TypeSpec:
			names = append(names, declaredName{s, s.Name})
		case *ast.ValueSpec:
			for _, name := range s.Names {
				names = append(names, declaredName{s, name})
			}
		}
	}
	return names
}

// genericOrigin is the generic declaration behind an instantiated function
// or type, so a use of F[int] reaches F.
func genericOrigin(obj types.Object) types.Object {
	switch o := obj.(type) {
	case *types.Func:
		return o.Origin()
	case *types.Var:
		return o.Origin()
	case *types.TypeName:
		if named, isNamed := o.Type().(*types.Named); isNamed {
			return named.Origin().Obj()
		}
	}
	return obj
}

// methodsOf lists the methods declared on a named type; none for any other
// object.
func methodsOf(obj types.Object) []types.Object {
	tn, isType := obj.(*types.TypeName)
	if !isType {
		return nil
	}
	named, isNamed := tn.Type().(*types.Named)
	if !isNamed {
		return nil
	}
	methods := make([]types.Object, 0, named.NumMethods())
	for i := range named.NumMethods() {
		methods = append(methods, named.Method(i))
	}
	return methods
}

// fact is the reach fact of obj, declared at d: its id names the root, the
// package (relative to the shared module) and the symbol; its source is the
// declaration's span under the root.
func (d sharedDecl) fact(obj types.Object, fset *token.FileSet, shared *SharedRoot) Fact {
	symbol := obj.Name()
	if fn, isFunc := obj.(*types.Func); isFunc {
		if recv := fn.Signature().Recv(); recv != nil {
			if named, isNamed := deref(recv.Type()).(*types.Named); isNamed {
				symbol = named.Obj().Name() + "." + symbol
			}
		}
	}
	pkgDir := strings.TrimPrefix(strings.TrimPrefix(obj.Pkg().Path(), shared.Module), "/")
	start, end := fset.Position(d.node.Pos()), fset.Position(d.node.End())
	file := start.Filename
	if rel, err := relTo(shared.Dir, file); err == nil {
		file = rel
	}
	qualified := obj.Pkg().Name() + "." + symbol
	return Fact{
		Kind:  "reach",
		ID:    "reach." + shared.Name + "." + path.Join(".", pkgDir) + "." + symbol,
		Attrs: map[string]string{"symbol": qualified},
		Source: Ref{
			Path:  file,
			Lines: itoa(start.Line) + "-" + itoa(end.Line),
			Root:  shared.Name,
		},
		Origin:    origin,
		Certainty: certain,
	}
}
