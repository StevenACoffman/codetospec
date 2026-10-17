package main

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/tools/go/packages"
)

// ConstantFacts binds every numeric constant the module's code refers to,
// named as the code writes it, to its value, so grounding finds a stated
// "400" behind `http.StatusBadRequest` or a stated "3" behind `maxRetries`.
// Type information resolves an aliased import (`h "net/http"` gives
// "h.StatusBadRequest") and constants of any package, the standard library
// included.
//
// Requires: pkgs were loaded with syntax and type information.
// Ensures:  one fact per written name, sorted by id; a name written for two
// different values (the same bare name in two packages) is dropped, since
// grounding could not tell which one a rule means. A constant declared
// outside root has an empty source.
func ConstantFacts(pkgs []*packages.Package, fset *token.FileSet, root string) []Fact {
	values := map[string]map[string]bool{} // written name → values
	sources := map[string]Ref{}
	note := func(name string, obj *types.Const) {
		value, ok := numericValue(obj.Val())
		if !ok {
			return
		}
		if values[name] == nil {
			values[name] = map[string]bool{}
		}
		values[name][value] = true
		if _, seen := sources[name]; !seen {
			sources[name] = declaredAt(obj, fset, root)
		}
	}
	for _, p := range pkgs {
		if p.TypesInfo == nil {
			continue
		}
		for _, file := range p.Syntax {
			ast.Inspect(file, func(n ast.Node) bool {
				return visitConstant(n, p.TypesInfo, note)
			})
		}
	}

	facts := make([]Fact, 0, len(values))
	for name, set := range values {
		if len(set) != 1 {
			continue
		}
		for value := range set {
			facts = append(facts, Fact{
				Kind:      "constant",
				ID:        "constant.go." + name,
				Attrs:     map[string]string{"name": name, "value": value},
				Source:    sources[name],
				Origin:    origin,
				Certainty: certain,
			})
		}
	}
	sort.Slice(facts, func(i, j int) bool { return facts[i].ID < facts[j].ID })
	return facts
}

// visitConstant reports a constant reference at n to note, under the name
// the code writes: "pkg.Name" through a package qualifier, "Name" bare. It
// returns whether ast.Inspect should descend into n.
func visitConstant(n ast.Node, info *types.Info, note func(string, *types.Const)) bool {
	switch e := n.(type) {
	case *ast.SelectorExpr:
		qualifier, ok := e.X.(*ast.Ident)
		if !ok {
			return true
		}
		if _, isPkg := info.Uses[qualifier].(*types.PkgName); !isPkg {
			return true
		}
		if c, isConst := info.Uses[e.Sel].(*types.Const); isConst {
			note(qualifier.Name+"."+e.Sel.Name, c)
		}
		return false // Sel is not a bare reference
	case *ast.Ident:
		// Universe constants (true, iota) have no package and are no
		// business value.
		if c, isConst := info.Uses[e].(*types.Const); isConst && c.Pkg() != nil {
			note(e.Name, c)
		}
	}
	return true
}

// numericValue renders an integer or floating-point constant the way
// grounding parses numbers; other kinds are not numbers a rule states.
func numericValue(v constant.Value) (string, bool) {
	if v.Kind() == constant.Int {
		return v.ExactString(), true
	}
	if v.Kind() == constant.Float {
		f, _ := constant.Float64Val(v)
		return strconv.FormatFloat(f, 'g', -1, 64), true
	}
	return "", false
}

// declaredAt is the declaration line of obj when it lies under root.
func declaredAt(obj types.Object, fset *token.FileSet, root string) Ref {
	pos := fset.Position(obj.Pos())
	rel, err := relTo(root, pos.Filename)
	if err != nil || pos.Filename == "" || strings.HasPrefix(rel, "..") {
		return Ref{}
	}
	line := itoa(pos.Line)
	return Ref{Path: rel, Lines: line + "-" + line}
}
