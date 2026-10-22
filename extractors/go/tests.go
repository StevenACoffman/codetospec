package main

import (
	"go/ast"
	"go/token"
	"go/types"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/tools/go/packages"
)

// TestFacts lists which of the module's declarations each test function
// uses: one test fact per test and declaration, citing the test, with the
// declaration as callee. A use through a helper declared in a _test.go
// file counts as the test's own: helpers are how tests share setup and
// assertions. codetospec follows the module's call facts from there to
// tell which rules' code a test reaches.
//
// Requires: m was loaded by loadPackages (tests included).
// Ensures:  sorted by id; callees are declarations of non-test files only.
func TestFacts(m *loadedModule, root string) []Fact {
	decls := declsByPosition(m.pkgs, m.fset, root)
	var facts []Fact
	for _, p := range m.tests {
		if p.TypesInfo != nil {
			facts = append(facts, packageTestFacts(p, m.fset, root, decls)...)
		}
	}
	sort.Slice(facts, func(i, j int) bool { return facts[i].ID < facts[j].ID })
	return facts
}

// packageTestFacts lists the test facts of one test package's _test.go
// files. Each such file belongs to one test variant only, so no fact
// repeats across packages.
func packageTestFacts(
	p *packages.Package,
	fset *token.FileSet,
	root string,
	decls map[token.Position]declSpan,
) []Fact {
	helpers := testHelpers(p.Syntax, p.TypesInfo, fset)
	var facts []Fact
	for _, file := range p.Syntax {
		rel, err := relTo(root, fset.Position(file.Pos()).Filename)
		if err != nil || !strings.HasSuffix(rel, "_test.go") {
			continue
		}
		for _, decl := range file.Decls {
			fn, isFunc := decl.(*ast.FuncDecl)
			if !isFunc || fn.Body == nil || !isTestFunc(fn, p.TypesInfo) {
				continue
			}
			lines := itoa(fset.Position(fn.Pos()).Line) + "-" + itoa(fset.Position(fn.End()).Line)
			for _, callee := range usedDecls(fn, p.TypesInfo, helpers, decls, fset) {
				facts = append(facts, Fact{
					Kind: "test",
					ID: "test." + rel + "#" + testName(fn) + ":" + lines +
						"#" + callee.file + ":" + callee.lines,
					Attrs: map[string]string{
						attrName:       testName(fn),
						"callee_path":  callee.file,
						"callee_lines": callee.lines,
					},
					Source:    Ref{Path: rel, Lines: lines},
					Origin:    origin,
					Certainty: certain,
				})
			}
		}
	}
	return facts
}

// declSpan is a module declaration a test may reach, by its citation.
type declSpan struct {
	file, lines string
}

// declsByPosition indexes the module's functions, methods and named types
// by the position of their name. A test variant type-checks its package
// again, so its objects are not the plain package's; their positions are.
func declsByPosition(
	pkgs []*packages.Package,
	fset *token.FileSet,
	root string,
) map[token.Position]declSpan {
	byPos := map[token.Position]declSpan{}
	for _, d := range moduleDecls(pkgs, fset, root) {
		lines := itoa(
			fset.Position(d.node.Pos()).Line,
		) + "-" + itoa(
			fset.Position(d.node.End()).Line,
		)
		byPos[fset.Position(d.obj.Pos())] = declSpan{file: d.file, lines: lines}
	}
	return byPos
}

// testHelpers indexes the functions and methods declared in the _test.go
// files of one test package, by object.
func testHelpers(
	files []*ast.File,
	info *types.Info,
	fset *token.FileSet,
) map[types.Object]*ast.FuncDecl {
	helpers := map[types.Object]*ast.FuncDecl{}
	for _, file := range files {
		if !strings.HasSuffix(fset.Position(file.Pos()).Filename, "_test.go") {
			continue
		}
		for _, decl := range file.Decls {
			if fn, isFunc := decl.(*ast.FuncDecl); isFunc && fn.Body != nil {
				if obj := info.Defs[fn.Name]; obj != nil {
					helpers[obj] = fn
				}
			}
		}
	}
	return helpers
}

// usedDecls lists the module declarations test uses, itself or through the
// test helpers it calls, transitively; sorted by file then lines.
func usedDecls(
	test *ast.FuncDecl,
	info *types.Info,
	helpers map[types.Object]*ast.FuncDecl,
	decls map[token.Position]declSpan,
	fset *token.FileSet,
) []declSpan {
	used := map[declSpan]bool{}
	visited := map[*ast.FuncDecl]bool{test: true}
	queue := []*ast.FuncDecl{test}
	for len(queue) > 0 {
		fn := queue[0]
		queue = queue[1:]
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			id, isIdent := n.(*ast.Ident)
			if !isIdent || info.Uses[id] == nil {
				return true
			}
			obj := genericOrigin(info.Uses[id])
			if helper, isHelper := helpers[obj]; isHelper {
				if !visited[helper] {
					visited[helper] = true
					queue = append(queue, helper)
				}
				return true
			}
			if d, isDecl := decls[fset.Position(obj.Pos())]; isDecl {
				used[d] = true
			}
			return true
		})
	}
	spans := make([]declSpan, 0, len(used))
	for d := range used {
		spans = append(spans, d)
	}
	sort.Slice(spans, func(i, j int) bool {
		if spans[i].file != spans[j].file {
			return spans[i].file < spans[j].file
		}
		return spans[i].lines < spans[j].lines
	})
	return spans
}

// isTestFunc reports whether fn is a test: a function `go test` runs,
// TestXxx(*testing.T) or FuzzXxx(*testing.F), or a suite method TestXxx()
// that a testify-style runner calls by reflection (`suite.Run(t, &s{})`),
// which the runner function's own body never names. Benchmarks and
// examples assert nothing about behavior, so they are left out.
func isTestFunc(fn *ast.FuncDecl, info *types.Info) bool {
	obj, isFunc := info.Defs[fn.Name].(*types.Func)
	if !isFunc {
		return false
	}
	params, name := obj.Signature().Params(), fn.Name.Name
	switch {
	case fn.Recv != nil:
		return isTestName(name, "Test") && params.Len() == 0 && obj.Signature().Results().Len() == 0
	case isTestName(name, "Test"):
		return params.Len() == 1 && params.At(0).Type().String() == "*testing.T"
	case isTestName(name, "Fuzz"):
		return params.Len() == 1 && params.At(0).Type().String() == "*testing.F"
	}
	return false
}

// testName names a test in its facts: a suite method by its receiver type
// too, since two suites of one package may share method names.
func testName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return fn.Name.Name
	}
	recv := fn.Recv.List[0].Type
	if star, isStar := recv.(*ast.StarExpr); isStar {
		recv = star.X
	}
	return types.ExprString(recv) + "." + fn.Name.Name
}

// isTestName reports whether name is prefix followed by nothing or by a
// character other than a lower-case letter (go test's rule: Testify is
// not a test).
func isTestName(name, prefix string) bool {
	rest, found := strings.CutPrefix(name, prefix)
	if !found {
		return false
	}
	r, _ := utf8.DecodeRuneInString(rest)
	return rest == "" || !unicode.IsLower(r)
}
