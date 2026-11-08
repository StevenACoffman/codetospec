package main

import (
	"encoding/json"
	"go/ast"
	"go/token"
)

// allowPath is one path on which a checker allows: the condition reaching
// it (the condition notation; empty when the checker always allows there),
// the lines that allow, and the check calls in the condition.
type allowPath struct {
	Condition string      `json:"condition,omitempty"`
	Lines     string      `json:"lines"`
	Checks    []checkCall `json:"checks,omitempty"`
}

// resultMode is how a checker says it allows: by a bool result (at index
// boolAt) or, with none, by a nil last error.
type resultMode struct {
	boolAt  int // -1 when no result is a bool
	errOnly bool
	// nilDenies: an error-only checker whose first result is a pointer
	// denies by returning nil there with no error (a value withheld:
	// _getBooleanSetting's "return nil, nil").
	nilDenies bool
}

// modeOf reads fn's results: its first bool result, else a last error
// (any value beside it is ignored: the error decides). A filter, whose
// first result is a collection, decides per item, which no path reads;
// a bool beside its collection is a separate decision its callers may
// gate on (AdministeredSchoolsInDistrict's "administers the district"),
// not the filter's.
func modeOf(fn *ast.FuncType) (resultMode, bool) {
	types := resultTypes(fn)
	if len(types) > 0 && isCollection(types[0]) {
		return resultMode{}, false
	}
	for i, t := range types {
		if ident, ok := t.(*ast.Ident); ok && ident.Name == goBool {
			return resultMode{boolAt: i}, true
		}
	}
	if returnsErrorLast(fn) {
		_, pointer := types[0].(*ast.StarExpr)
		return resultMode{boolAt: -1, errOnly: true, nilDenies: pointer && len(types) > 1}, true
	}
	return resultMode{}, false
}

// errorDecides reports whether fn's last error, not its bool, carries the
// decision: fn denies by building an authorization error and never by its
// bool alone (no return of false beside a nil error, nor a bare return).
// The bool is then data its callers read: AuthorizeCSVExportRequest
// returns (isCurrentUser, err), _canEditAndIsAdmin (isAdmin, err).
func errorDecides(fn *ast.FuncDecl, mode resultMode) bool {
	if mode.boolAt < 0 || !returnsErrorLast(fn.Type) || !buildsAuthorizationError(fn.Body) {
		return false
	}
	alone := false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.ReturnStmt:
			alone = alone || len(node.Results) <= mode.boolAt ||
				isIdentNamed(node.Results[mode.boolAt], goFalse) &&
					isIdentNamed(node.Results[len(node.Results)-1], goNil)
		}
		return !alone
	})
	return !alone
}

// isIdentNamed reports whether e is the identifier name.
func isIdentNamed(e ast.Expr, name string) bool {
	ident, ok := e.(*ast.Ident)
	return ok && ident.Name == name
}

// isCollection reports whether t is a slice, array or map type.
func isCollection(t ast.Expr) bool {
	switch t.(type) {
	case *ast.ArrayType, *ast.MapType:
		return true
	}
	return false
}

// allowingPaths reads the paths on which checker fn allows, in source
// order, for the shapes it can read whole:
//   - local definitions, then one return whose decision is an expression:
//     each of its top-level || disjuncts is a path;
//   - top-level "if C { return <allow> }" statements, with denying ones
//     ("if D { return <deny> }") between them, ending in a return: path i
//     is C_i under the negation of every D before it, which is exact;
//
// else it returns why not (a loop or switch that decides, a branch that
// returns on only some of its paths, an else). A checker returning a bool
// allows with true or a true expression; one ending in an error
// allows with nil.
func (sc scanner) allowingPaths(fn *ast.FuncDecl) ([]allowPath, string) {
	mode, ok := modeOf(fn.Type)
	switch {
	case fn.Body == nil:
		return nil, "has no body"
	case !ok && (returnsErrorLast(fn.Type) || returnsCollectionFirst(fn.Type)):
		return nil, "a filter: decides per item"
	case !ok:
		return nil, "returns no bool and no error"
	case errorDecides(fn, mode):
		mode = resultMode{boolAt: -1, errOnly: true}
	}
	w := pathWalk{sc: sc, mode: mode, locals: map[string]binding{}, marks: newCallMarks()}
	for _, stmt := range fn.Body.List {
		if reason := w.step(stmt); reason != "" {
			return nil, reason
		}
		if w.done {
			return w.paths, ""
		}
	}
	return nil, "ends without a return"
}

// pathWalk follows a checker's top-level statements (see allowingPaths).
type pathWalk struct {
	sc     scanner
	mode   resultMode
	locals map[string]binding
	negs   []ast.Expr // the negated conditions of the denials passed
	marks  callMarks
	paths  []allowPath
	done   bool // the final return was read
}

// step reads one top-level statement; why the body can't be read, else
// "".
func (w *pathWalk) step(stmt ast.Stmt) string {
	switch s := stmt.(type) {
	case *ast.AssignStmt:
		w.sc.bind(s, w.locals)
	case *ast.DeclStmt, *ast.ExprStmt, *ast.EmptyStmt:
		// local declarations, logging: no decision
	case *ast.IfStmt:
		return w.branch(s)
	case *ast.ReturnStmt:
		w.final(s)
		w.done = true
	default:
		if containsReturn(stmt) {
			return "a loop, switch or select decides"
		}
		forget(stmt, w.locals)
	}
	return ""
}

// branch reads "if C { …; return R }": R allowing makes a path, R denying
// a negation every later path is under.
func (w *pathWalk) branch(s *ast.IfStmt) string {
	if s.Else != nil {
		return "an if with an else decides"
	}
	scope := w.locals
	if s.Init != nil {
		init, ok := s.Init.(*ast.AssignStmt)
		if !ok {
			return "an if's init statement is no assignment"
		}
		scope = cloneLocals(w.locals)
		w.sc.bind(init, scope)
	}
	ret, exits := lastReturn(s.Body.List)
	if !exits {
		if containsReturn(s) {
			return "a branch returns on only some of its paths"
		}
		forget(s, w.locals)
		return ""
	}
	for _, inner := range s.Body.List[:len(s.Body.List)-1] {
		if containsReturn(inner) {
			return "a branch returns on only some of its paths"
		}
	}
	res := newResolution()
	cond := w.sc.resolve(s.Cond, scope, &res)
	w.marks = w.marks.with(res.marks)
	if decision, allows := w.decision(ret, scope); allows {
		w.add(append(append([]ast.Expr(nil), w.negs...), conjoin(cond, decision)...), ret)
	}
	w.negs = append(w.negs, &ast.UnaryExpr{Op: token.NOT, X: &ast.ParenExpr{X: cond}})
	return ""
}

// final reads the last top-level return: each disjunct of a returned
// decision is a path under the negations passed.
func (w *pathWalk) final(ret *ast.ReturnStmt) {
	decision, allows := w.decision(ret, w.locals)
	if !allows {
		return
	}
	if decision == nil {
		w.add(w.negs, ret)
		return
	}
	for _, d := range disjuncts(decision) {
		w.add(append(append([]ast.Expr(nil), w.negs...), d), ret)
	}
}

// decision reads what return ret decides: allows false when it denies;
// a nil expression when it always allows (true, a nil error); else the
// expression it allows under, resolved through locals.
func (w *pathWalk) decision(ret *ast.ReturnStmt, locals map[string]binding) (ast.Expr, bool) {
	var result ast.Expr
	switch {
	case w.mode.boolAt >= 0 && w.mode.boolAt < len(ret.Results):
		result = ret.Results[w.mode.boolAt]
	case w.mode.errOnly && len(ret.Results) > 0:
		result = ret.Results[len(ret.Results)-1]
	default:
		return nil, false
	}
	if w.mode.nilDenies && isIdentNamed(ret.Results[0], goNil) {
		return nil, false // a nil value, withheld
	}
	if ident, ok := result.(*ast.Ident); ok {
		switch {
		case ident.Name == goTrue && !w.mode.errOnly, ident.Name == goNil && w.mode.errOnly:
			return nil, true
		case ident.Name == goFalse, w.mode.errOnly:
			return nil, false // false, or an error value
		}
	}
	if w.mode.errOnly {
		call, isCall := result.(*ast.CallExpr)
		if !isCall || buildsError(call) {
			return nil, false
		}
		// return check(…): allows when the check's error is nil.
		w.marks.failed[call] = failsWithSoleError
		return &ast.BinaryExpr{X: call, Op: token.EQL, Y: ast.NewIdent(goNil)}, true
	}
	res := newResolution()
	expr := w.sc.resolve(result, locals, &res)
	w.marks = w.marks.with(res.marks)
	return expr, true
}

// add records the path whose condition is the conjunction of conds,
// allowing at ret.
func (w *pathWalk) add(conds []ast.Expr, ret *ast.ReturnStmt) {
	line := w.sc.fset.Position(ret.Pos()).Line
	p := allowPath{Lines: lines(line, line)}
	if len(conds) > 0 {
		g := guard{kind: ruleAccess, cond: allOf(conds), marks: w.marks}
		g.unknown = w.sc.unknownCalls(g.cond, g.marks)
		p.Condition = conditionOf(w.sc.fset, &g)
		p.Checks = w.sc.known.checkCalls(w.sc.fset, &g)
	}
	w.paths = append(w.paths, p)
}

// pathsJSON encodes paths for an access_check fact; "" when there are
// none.
func pathsJSON(paths []allowPath) string {
	if len(paths) == 0 {
		return ""
	}
	data, err := json.Marshal(paths)
	if err != nil {
		return ""
	}
	return string(data)
}

// buildsError reports whether call builds an error (errors.X(…),
// fmt.Errorf): returned by an error-only checker, it denies.
func buildsError(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, isIdent := sel.X.(*ast.Ident)
	return isIdent && (pkg.Name == errorsPkg || pkg.Name == "fmt" && sel.Sel.Name == "Errorf")
}

// conjoin is cond and, when set, decision.
func conjoin(cond, decision ast.Expr) []ast.Expr {
	if decision == nil {
		return []ast.Expr{cond}
	}
	return []ast.Expr{cond, decision}
}

// disjuncts splits e on its top-level ||.
func disjuncts(e ast.Expr) []ast.Expr {
	if paren, ok := e.(*ast.ParenExpr); ok {
		return disjuncts(paren.X)
	}
	if bin, ok := e.(*ast.BinaryExpr); ok && bin.Op == token.LOR {
		return append(disjuncts(bin.X), disjuncts(bin.Y)...)
	}
	return []ast.Expr{e}
}

// lastReturn is the return ending stmts, if they end in one.
func lastReturn(stmts []ast.Stmt) (*ast.ReturnStmt, bool) {
	if len(stmts) == 0 {
		return nil, false
	}
	ret, ok := stmts[len(stmts)-1].(*ast.ReturnStmt)
	return ret, ok
}

// containsReturn reports whether n holds a return statement outside a
// function literal.
func containsReturn(n ast.Node) bool {
	found := false
	ast.Inspect(n, func(node ast.Node) bool {
		switch node.(type) {
		case *ast.FuncLit:
			return false
		case *ast.ReturnStmt:
			found = true
		}
		return !found
	})
	return found
}

// cloneLocals copies locals for a scope of its own.
func cloneLocals(locals map[string]binding) map[string]binding {
	out := make(map[string]binding, len(locals))
	for k, v := range locals {
		out[k] = v
	}
	return out
}
