package main

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/packages"
)

// guard is code that runs only while an enum-typed expression holds one
// of some values: the body of `if x == A`, or of `case A:` in `switch x`.
type guard struct {
	subject string // the expression, as written (types.ExprString)
	enum    *enumType
	from    []string // the constants it holds there
	node    ast.Node // what is cited
	body    []ast.Stmt
}

// transitionFacts lists the transitions the code guards: an assignment
// `x = B` of an enum constant inside a guard on the same expression x
// holding A is a transition from A to B, cited by the guard. An unguarded
// assignment is not one: it says where a state is entered, not from where.
//
// Requires: as EnumFacts; enums from moduleEnums.
// Ensures:  one fact per guard, from and to; never from a value to itself.
func transitionFacts(
	pkgs []*packages.Package,
	fset *token.FileSet,
	root string,
	enums map[*types.TypeName]*enumType,
) []Fact {
	var facts []Fact
	for _, f := range moduleFiles(pkgs, fset, root) {
		ast.Inspect(f.syntax, func(n ast.Node) bool {
			for _, g := range guards(n, f.info, enums) {
				facts = append(facts, g.transitions(lineSpan(fset, g.node, f.rel), f.info)...)
			}
			return true
		})
	}
	return facts
}

// transitions lists g's transitions, cited at at, each once.
func (g *guard) transitions(at Ref, info *types.Info) []Fact {
	seen := map[string]bool{}
	var facts []Fact
	for _, to := range assigned(g, info) {
		for _, from := range g.from {
			id := "transition." + at.Path + ":" + at.Lines + "#" + from + ">" + to
			if from == to || seen[id] {
				continue
			}
			seen[id] = true
			facts = append(facts, Fact{
				Kind:      "transition",
				ID:        id,
				Attrs:     map[string]string{attrEnum: g.enum.id, "from": from, "to": to},
				Source:    at,
				Origin:    origin,
				Certainty: certain,
			})
		}
	}
	return facts
}

// guards lists the guards node opens: an if statement, or each case of a
// switch, tagged or not.
func guards(n ast.Node, info *types.Info, enums map[*types.TypeName]*enumType) []guard {
	switch s := n.(type) {
	case *ast.IfStmt:
		if g, ok := conditionGuard(s.Cond, info, enums); ok {
			g.node, g.body = s, s.Body.List
			return []guard{g}
		}
	case *ast.SwitchStmt:
		var out []guard
		for _, stmt := range s.Body.List {
			clause, isClause := stmt.(*ast.CaseClause)
			if !isClause || clause.List == nil {
				continue // the default case holds no named value
			}
			g, ok := caseGuard(s.Tag, clause.List, info, enums)
			if ok {
				g.node, g.body = clause, clause.Body
				out = append(out, g)
			}
		}
		return out
	}
	return nil
}

// caseGuard is the guard of one case: `case A, B:` of `switch x`, or
// `case x == A:` of a tagless switch.
func caseGuard(
	tag ast.Expr,
	list []ast.Expr,
	info *types.Info,
	enums map[*types.TypeName]*enumType,
) (guard, bool) {
	if tag == nil {
		if len(list) != 1 {
			return guard{}, false
		}
		return conditionGuard(list[0], info, enums)
	}
	e := enumOf(info.TypeOf(tag), enums)
	if e == nil {
		return guard{}, false
	}
	g := guard{subject: types.ExprString(tag), enum: e}
	for _, expr := range list {
		name, ok := enumConst(expr, info, e)
		if !ok {
			return guard{}, false
		}
		g.from = append(g.from, name)
	}
	return g, true
}

// conditionGuard reads `x == A`, or `A == x`, alone or as one operand of
// a chain of &&: the body runs only while x holds A.
func conditionGuard(
	cond ast.Expr,
	info *types.Info,
	enums map[*types.TypeName]*enumType,
) (guard, bool) {
	bin, isBinary := ast.Unparen(cond).(*ast.BinaryExpr)
	if !isBinary {
		return guard{}, false
	}
	if bin.Op == token.LAND {
		if g, ok := conditionGuard(bin.X, info, enums); ok {
			return g, true
		}
		return conditionGuard(bin.Y, info, enums)
	}
	if bin.Op != token.EQL {
		return guard{}, false
	}
	for _, pair := range [][2]ast.Expr{{bin.X, bin.Y}, {bin.Y, bin.X}} {
		e := enumOf(info.TypeOf(pair[0]), enums)
		if e == nil {
			continue
		}
		name, isConst := enumConst(pair[1], info, e)
		if _, subjectIsConst := enumConst(pair[0], info, e); isConst && !subjectIsConst {
			return guard{subject: types.ExprString(pair[0]), enum: e, from: []string{name}}, true
		}
	}
	return guard{}, false
}

// assigned lists the constants of g's enum assigned to g's subject in g's
// body, at any depth.
func assigned(g *guard, info *types.Info) []string {
	var to []string
	for _, stmt := range g.body {
		ast.Inspect(stmt, func(n ast.Node) bool {
			assign, isAssign := n.(*ast.AssignStmt)
			if !isAssign || assign.Tok != token.ASSIGN || len(assign.Lhs) != len(assign.Rhs) {
				return true
			}
			for i, lhs := range assign.Lhs {
				if types.ExprString(lhs) != g.subject {
					continue
				}
				if name, ok := enumConst(assign.Rhs[i], info, g.enum); ok {
					to = append(to, name)
				}
			}
			return true
		})
	}
	return to
}

// enumOf is the enumeration t is, or nil.
func enumOf(t types.Type, enums map[*types.TypeName]*enumType) *enumType {
	if named, isNamed := t.(*types.Named); isNamed {
		return enums[named.Obj()]
	}
	return nil
}

// enumConst is the name of the constant of e that expr refers to.
func enumConst(expr ast.Expr, info *types.Info, e *enumType) (string, bool) {
	var id *ast.Ident
	switch x := ast.Unparen(expr).(type) {
	case *ast.Ident:
		id = x
	case *ast.SelectorExpr:
		id = x.Sel
	default:
		return "", false
	}
	c, isConst := info.Uses[id].(*types.Const)
	if !isConst {
		return "", false
	}
	if named, isNamed := c.Type().(*types.Named); !isNamed || named.Obj() != e.obj {
		return "", false
	}
	return c.Name(), true
}
