package main

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/packages"
)

// packageVar is one name of a package-level variable of the module's own
// non-test files, with the spec declaring it.
type packageVar struct {
	obj  types.Object
	spec *ast.ValueSpec
	file string // relative to the module root
	info *types.Info
}

// packageVarsOf lists the package-level variables of the module's own
// non-test files, one entry per non-blank name (`var a, b = f()` gives two,
// sharing the spec), in package, file and source order.
//
// Requires: pkgs carry syntax and type information.
func packageVarsOf(pkgs []*packages.Package, fset *token.FileSet, root string) []packageVar {
	var vars []packageVar
	for _, p := range pkgs {
		if p.TypesInfo == nil {
			continue
		}
		for _, file := range p.Syntax {
			rel, err := relTo(root, fset.Position(file.Pos()).Filename)
			if err != nil || strings.HasPrefix(rel, "..") || strings.HasSuffix(rel, "_test.go") {
				continue
			}
			vars = append(vars, fileVars(file, rel, p.TypesInfo)...)
		}
	}
	return vars
}

// fileVars lists the package-level variables of one file.
func fileVars(file *ast.File, rel string, info *types.Info) []packageVar {
	var vars []packageVar
	for _, decl := range file.Decls {
		gen, isGen := decl.(*ast.GenDecl)
		if !isGen || gen.Tok != token.VAR {
			continue
		}
		for _, spec := range gen.Specs {
			vs, isValue := spec.(*ast.ValueSpec)
			if !isValue {
				continue
			}
			for _, name := range vs.Names {
				if obj := info.Defs[name]; obj != nil && name.Name != "_" {
					vars = append(vars, packageVar{obj: obj, spec: vs, file: rel, info: info})
				}
			}
		}
	}
	return vars
}

// packageVars indexes the module's package-level variables by the position
// of their name, each citing the whole spec that declares it
// (`var X = register(…)` spans the call).
//
// They are not module declarations (see addFileDecls): a rule is about
// behavior, so symbol facts skip them. But a rule can cite a registry table
// held in one (admin's role definitions), and a test that reads the table
// tests it, so test reach needs them.
func packageVars(
	pkgs []*packages.Package,
	fset *token.FileSet,
	root string,
) map[token.Position]declSpan {
	vars := map[token.Position]declSpan{}
	for _, v := range packageVarsOf(pkgs, fset, root) {
		vars[fset.Position(v.obj.Pos())] = spanOf(&moduleDecl{node: v.spec, file: v.file}, fset)
	}
	return vars
}

// callableVars returns, as declarations call facts can name, the module's
// package-level variables holding a function: built by a wrapper (`var
// Scopes = cache.Cache(_uncachedScopes)`), by a factory (a dataloader) or
// written in place. A call through one is otherwise a dead end, since only
// functions, methods and types are declarations. A variable holding data
// stays out whatever its initializer: `var ErrEmpty = errors.New(…)` is
// built by a call, but using it calls nothing.
func callableVars(
	pkgs []*packages.Package,
	fset *token.FileSet,
	root string,
) map[types.Object]moduleDecl {
	decls := map[types.Object]moduleDecl{}
	for _, v := range packageVarsOf(pkgs, fset, root) {
		if _, isFunc := v.obj.Type().Underlying().(*types.Signature); isFunc &&
			len(v.spec.Values) > 0 {
			decls[v.obj] = moduleDecl{obj: v.obj, node: v.spec, file: v.file, info: v.info}
		}
	}
	return decls
}
