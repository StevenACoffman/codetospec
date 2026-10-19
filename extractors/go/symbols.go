package main

import (
	"go/ast"
	"go/token"
	"go/types"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
)

// signatureMaxLen caps a signature carried in a symbol fact.
const signatureMaxLen = 400

// moduleDecl is a function, method or named type of the analyzed module.
type moduleDecl struct {
	obj  types.Object
	node ast.Node
	file string // relative to the module root
	info *types.Info
}

// moduleDecls indexes the functions, methods and named types the module's
// own packages declare, by object.
func moduleDecls(
	pkgs []*packages.Package,
	fset *token.FileSet,
	root string,
) map[types.Object]moduleDecl {
	decls := map[types.Object]moduleDecl{}
	for _, p := range pkgs {
		if p.TypesInfo == nil {
			continue
		}
		for _, file := range p.Syntax {
			rel, err := relTo(root, fset.Position(file.Pos()).Filename)
			if err == nil && !strings.HasPrefix(rel, "..") {
				addFileDecls(decls, file, rel, p.TypesInfo)
			}
		}
	}
	return decls
}

// addFileDecls adds a file's functions, methods and named types to decls;
// variables and constants are skipped: a rule is about behavior.
func addFileDecls(decls map[types.Object]moduleDecl, file *ast.File, rel string, info *types.Info) {
	for _, decl := range file.Decls {
		for _, d := range declared(decl) {
			if _, isValue := d.node.(*ast.ValueSpec); isValue {
				continue
			}
			if obj := info.Defs[d.name]; obj != nil {
				decls[obj] = moduleDecl{obj: obj, node: d.node, file: rel, info: info}
			}
		}
	}
}

// SymbolFacts lists the module's functions, methods and named types as
// precise symbol facts: the exact span the map phase may cite, and the
// target a crosscheck repair re-cites. Their ids match those of the
// tree-sitter layer and of the SCIP converter, so they merge.
//
// Requires: pkgs are the module's packages, with syntax and type information.
// Ensures:  one fact per declaration, sorted by id.
func SymbolFacts(pkgs []*packages.Package, fset *token.FileSet, root string) []Fact {
	decls := moduleDecls(pkgs, fset, root)
	facts := make([]Fact, 0, len(decls))
	for _, d := range decls {
		start, end := fset.Position(d.node.Pos()).Line, fset.Position(d.node.End()).Line
		name := d.obj.Name()
		attrs := map[string]string{
			attrName:    name,
			"kind":      symbolKind(d.obj),
			"namespace": d.obj.Pkg().Name(),
			"language":  "go",
			"precise":   "true",
			"signature": truncate(
				types.ObjectString(d.obj, types.RelativeTo(d.obj.Pkg())),
				signatureMaxLen,
			),
		}
		facts = append(facts, Fact{
			Kind:      "symbol",
			ID:        "symbol." + d.file + "#" + name + ":" + itoa(start),
			Attrs:     attrs,
			Source:    Ref{Path: d.file, Lines: itoa(start) + "-" + itoa(end)},
			Origin:    origin,
			Certainty: certain,
		})
	}
	sort.Slice(facts, func(i, j int) bool { return facts[i].ID < facts[j].ID })
	return facts
}

// symbolKind names a declaration's kind as codetospec's symbol facts do.
func symbolKind(obj types.Object) string {
	if fn, isFunc := obj.(*types.Func); isFunc {
		if fn.Signature().Recv() != nil {
			return "method"
		}
		return "function"
	}
	if _, isInterface := obj.Type().Underlying().(*types.Interface); isInterface {
		return "interface"
	}
	return "class"
}

// CallFacts lists which of the module's functions and methods call or use
// which others of the module: one call fact per caller and callee, both
// cited by their declaration span. codetospec places a definition used by
// callers of one domain alone in that domain.
//
// Requires: as SymbolFacts.
// Ensures:  sorted by id; no fact for a definition using itself.
func CallFacts(pkgs []*packages.Package, fset *token.FileSet, root string) []Fact {
	decls := moduleDecls(pkgs, fset, root)
	span := func(d moduleDecl) string {
		return itoa(fset.Position(d.node.Pos()).Line) + "-" + itoa(fset.Position(d.node.End()).Line)
	}
	seen := map[string]bool{}
	var facts []Fact
	for _, caller := range decls {
		fn, isFunc := caller.node.(*ast.FuncDecl)
		if !isFunc || fn.Body == nil {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			id, isIdent := n.(*ast.Ident)
			if !isIdent {
				return true
			}
			callee, isDecl := decls[genericOrigin(caller.info.Uses[id])]
			if !isDecl || callee.obj == caller.obj {
				return true
			}
			factID := "call." + caller.file + ":" + span(
				caller,
			) + "#" + callee.file + ":" + span(
				callee,
			)
			if !seen[factID] {
				seen[factID] = true
				facts = append(facts, Fact{
					Kind: "call",
					ID:   factID,
					Attrs: map[string]string{
						"callee_path":  callee.file,
						"callee_lines": span(callee),
					},
					Source:    Ref{Path: caller.file, Lines: span(caller)},
					Origin:    origin,
					Certainty: certain,
				})
			}
			return true
		})
	}
	sort.Slice(facts, func(i, j int) bool { return facts[i].ID < facts[j].ID })
	return facts
}

// truncate shortens s to n bytes, marking the cut.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
