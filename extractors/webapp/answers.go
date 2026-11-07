package main

import (
	"go/ast"
	"slices"
)

// answers names the known checks whose result resolver fn returns as its
// value, sorted: a check called in a return's value (not its trailing
// error, which gates), directly or through a local the body's top level
// binds to it ("ok := permissions.IsCoach(…); return ok, nil"). A check
// whose error can reach the caller (returned whole, or bound to a name)
// gates and is no answer. A check a resolver answers is the field it
// serves, not a gate it misses.
func (c checks) answers(fn *ast.FuncDecl) []string {
	if fn.Body == nil {
		return nil
	}
	bound := c.boundChecks(fn.Body)
	values := len(resultTypes(fn.Type))
	if returnsErrorLast(fn.Type) {
		values-- // the trailing error gates
	}
	var names []string
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.ReturnStmt:
			if len(n.Results) < len(resultTypes(fn.Type)) {
				return true // return check(…): the call's error passes through, a gate
			}
			for _, result := range n.Results[:min(values, len(n.Results))] {
				for _, call := range c.returnedChecks(result, bound) {
					if name, ok := c.nameOf(call); ok && !slices.Contains(names, name) {
						names = append(names, name)
					}
				}
			}
		}
		return true
	})
	slices.Sort(names)
	return names
}

// boundChecks maps each local body's top level binds to a known check's
// call (its first left-hand side) to that call, when the call's error
// can't gate: a single value, or an error assigned to _ (one bound to a
// name is branched on, a gate).
func (c checks) boundChecks(body *ast.BlockStmt) map[string]*ast.CallExpr {
	bound := map[string]*ast.CallExpr{}
	for _, stmt := range body.List {
		assign, ok := stmt.(*ast.AssignStmt)
		if !ok || len(assign.Rhs) != 1 {
			continue
		}
		call, isCall := assign.Rhs[0].(*ast.CallExpr)
		if !isCall || !c.has(call) {
			continue
		}
		if last, isIdent := assign.Lhs[len(assign.Lhs)-1].(*ast.Ident); len(assign.Lhs) > 1 &&
			(!isIdent || last.Name != "_") {
			continue
		}
		if ident, isIdent := assign.Lhs[0].(*ast.Ident); isIdent && ident.Name != "_" {
			bound[ident.Name] = call
		}
	}
	return bound
}

// returnedChecks are the known check calls a returned value holds, or
// holds through a local bound to one; a function literal's are left out
// (a value it returns, not its result).
func (c checks) returnedChecks(result ast.Expr, bound map[string]*ast.CallExpr) []*ast.CallExpr {
	var calls []*ast.CallExpr
	ast.Inspect(result, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.CallExpr:
			if c.has(n) {
				calls = append(calls, n)
			}
		case *ast.Ident:
			if call := bound[n.Name]; call != nil {
				calls = append(calls, call)
			}
		}
		return true
	})
	return calls
}
