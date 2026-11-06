package main

import (
	"encoding/json"
	"go/ast"
	"go/token"
	"strconv"
	"strings"
)

// attrCondition is a rule fact's condition, in codetospec's compact
// notation (codetospec's extract.AttrCondition): the checks the guard
// rejects on, as a tree a machine can hash and evaluate.
const attrCondition = "condition"

// cnode is one element of the compact notation; exactly one shape is set.
// Value is any so that false and 0 are written (omitempty drops only a nil
// interface).
type cnode struct {
	All    []cnode `json:"all,omitempty"`
	Any    []cnode `json:"any,omitempty"`
	Not    *cnode  `json:"not,omitempty"`
	Exists string  `json:"exists,omitempty"`
	Var    string  `json:"var,omitempty"`
	Op     string  `json:"op,omitempty"`
	Value  any     `json:"value,omitempty"`
	Var2   string  `json:"var2,omitempty"`
}

// conditionOf is the rejecting condition of g in the compact notation, ""
// when g has none the notation can state: an open, visibility, checked or
// policy rule, or a guard inside a loop (rejecting for some item needs a
// quantifier the notation lacks).
func conditionOf(fset *token.FileSet, g *guard) string {
	if g.cond == nil || g.loop != nil || (g.kind != ruleAccess && g.kind != ruleRateLimit) {
		return ""
	}
	c := conditioner{r: renderer{fset: fset, marks: g.marks}}
	data, err := json.Marshal(c.node(g.cond))
	if err != nil {
		return ""
	}
	return string(data)
}

// conditioner turns a guard condition into the notation, on the same marks
// as renderer.cond, which words it; renderer.source prints expressions.
type conditioner struct {
	r renderer
}

// node states e: logic as all, any and not; known checks as named
// variables; a comparison with a literal as itself; anything else as an
// opaque variable named by its source, true when it holds (honest, and
// still comparable across rules).
func (c conditioner) node(e ast.Expr) cnode {
	switch x := e.(type) {
	case *ast.ParenExpr:
		return c.node(x.X)
	case *ast.UnaryExpr:
		if x.Op == token.NOT {
			inner := c.node(x.X)
			if inner.Not != nil { // !(kaid == nil): kaid is present
				return *inner.Not
			}
			return cnode{Not: &inner}
		}
	case *ast.BinaryExpr:
		if x.Op == token.LAND || x.Op == token.LOR {
			return c.logical(x)
		}
		if n, ok := c.comparison(x); ok {
			return n
		}
	case *ast.CallExpr:
		name, _ := c.callVariable(x)
		return holds(name)
	case *ast.IndexExpr:
		if c.r.marks.present[x] {
			return cnode{Exists: variableName(c.r.source(x.X) + "." + c.key(x.Index))}
		}
	}
	return holds(variableName(c.r.source(e)))
}

// logical states a && or || chain as one all or any, flattening nested
// operations of the same operator.
func (c conditioner) logical(x *ast.BinaryExpr) cnode {
	var children []cnode
	for _, side := range []ast.Expr{x.X, x.Y} {
		child := c.node(side)
		switch {
		case x.Op == token.LAND && child.All != nil:
			children = append(children, child.All...)
		case x.Op == token.LOR && child.Any != nil:
			children = append(children, child.Any...)
		default:
			children = append(children, child)
		}
	}
	if x.Op == token.LAND {
		return cnode{All: children}
	}
	return cnode{Any: children}
}

// opEqual is the notation's equality operator.
const opEqual = "equal"

// isLoggedIn names acl.IsLoggedIn, the login check.
const isLoggedIn = "IsLoggedIn"

// isServiceAdminQuery names the request method saying a call is a
// service-admin request.
const isServiceAdminQuery = "RequestIsServiceAdminQuery"

// comparisonOp maps a Go comparison operator to the notation's; false for
// any other token.
func comparisonOp(tok token.Token) (string, bool) {
	ops := [...]struct {
		tok token.Token
		op  string
	}{
		{token.EQL, opEqual},
		{token.NEQ, "not_equal"},
		{token.LSS, "less_than"},
		{token.LEQ, "less_or_equal"},
		{token.GTR, "greater_than"},
		{token.GEQ, "greater_or_equal"},
	}
	for _, o := range ops {
		if o.tok == tok {
			return o.op, true
		}
	}
	return "", false
}

// comparison states a bound call's failure ("err != nil" on it: the call
// fails), a comparison with a literal, or a comparison of two request
// values a local held; false for any other binary expression.
func (c conditioner) comparison(x *ast.BinaryExpr) (cnode, bool) {
	op, known := comparisonOp(x.Op)
	if !known {
		return cnode{}, false
	}
	if call, isCall := x.X.(*ast.CallExpr); isCall && c.r.marks.failed[call] &&
		(x.Op == token.EQL || x.Op == token.NEQ) {
		return cnode{
			Var:   variableName(c.r.source(call.Fun) + ".fails"),
			Op:    opEqual,
			Value: x.Op == token.NEQ,
		}, true
	}
	if isNil(x.Y) && (x.Op == token.EQL || x.Op == token.NEQ) {
		present := cnode{Exists: c.operand(x.X)}
		if x.Op == token.EQL {
			return cnode{Not: &present}, true
		}
		return present, true
	}
	if value, isLiteral := literalValue(x.Y); isLiteral {
		return cnode{Var: c.operand(x.X), Op: op, Value: value}, true
	}
	if c.r.marks.fromLocal[x] {
		return cnode{Var: c.operand(x.X), Op: op, Var2: c.operand(x.Y)}, true
	}
	return cnode{}, false
}

// operand names a compared value: a request header read
// (r.Header.Get("H")) as request.header.H, anything else by its source.
func (c conditioner) operand(e ast.Expr) string {
	if call, ok := e.(*ast.CallExpr); ok && len(call.Args) == 1 {
		if sel, isSel := call.Fun.(*ast.SelectorExpr); isSel && sel.Sel.Name == "Get" {
			if inner, isInner := sel.X.(*ast.SelectorExpr); isInner &&
				inner.Sel.Name == headerField {
				return variableName("request.header." + c.key(call.Args[0]))
			}
		}
	}
	return variableName(c.r.source(e))
}

// isNil reports whether e is the predeclared nil.
func isNil(e ast.Expr) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == "nil"
}

// callVariable is the variable a call in the condition becomes, and
// whether it holds when the call fails: a bound error's call
// ("X.fails"), a known check (checkVariable), or a predicate by its
// function (the notation has no call operands, the arguments stay in the
// requirement's text).
func (c conditioner) callVariable(call *ast.CallExpr) (string, bool) {
	switch {
	case c.r.marks.failed[call]:
		return variableName(c.r.source(call.Fun) + ".fails"), true
	case isRateLimit(call) || isAccessCheck(call):
		return c.checkVariable(call), false
	}
	return variableName(c.r.source(call.Fun)), false
}

// checkVariable names the variable a known check stands for: the caller's
// login, a permission, a rate limit, a service-admin request; another
// access check by its function (its arguments stay in the requirement's
// text: the notation has no call operands).
func (c conditioner) checkVariable(call *ast.CallExpr) string {
	sel, isSel := call.Fun.(*ast.SelectorExpr)
	if !isSel {
		return variableName(c.r.source(call.Fun))
	}
	switch {
	case isRateLimit(call):
		return variableName("caller.overRateLimit." + strings.Trim(c.r.key(call), "`"))
	case sel.Sel.Name == isLoggedIn && c.r.hasArg(call, "PhantomProhibited"):
		return "caller.loggedInNonPhantom"
	case sel.Sel.Name == isLoggedIn:
		return "caller.loggedIn"
	case sel.Sel.Name == "ActorHasPermission" || sel.Sel.Name == "ActorHasPermissionErr":
		return variableName("caller.hasPermission." + c.r.permission(call))
	case sel.Sel.Name == isServiceAdminQuery:
		return "request.isServiceAdmin"
	}
	return variableName(c.r.source(call.Fun))
}

// key is a map index's key: a string literal's value, else its source.
func (c conditioner) key(e ast.Expr) string {
	if lit, ok := e.(*ast.BasicLit); ok && lit.Kind == token.STRING {
		if value, err := strconv.Unquote(lit.Value); err == nil {
			return value
		}
	}
	return c.r.source(e)
}

// holds is the check that variable name is true.
func holds(name string) cnode {
	return cnode{Var: name, Op: opEqual, Value: true}
}

// literalValue reads a basic literal or a predeclared constant (true,
// false) as the notation's value; false for anything else.
func literalValue(e ast.Expr) (any, bool) {
	switch x := e.(type) {
	case *ast.BasicLit:
		return basicValue(x)
	case *ast.Ident:
		if x.Name == goTrue || x.Name == goFalse {
			return x.Name == goTrue, true
		}
	}
	return nil, false
}

// basicValue reads a string, int or float literal; false for a char or
// an imaginary number.
func basicValue(x *ast.BasicLit) (any, bool) {
	if x.Kind == token.STRING {
		value, err := strconv.Unquote(x.Value)
		return value, err == nil
	}
	if x.Kind == token.INT {
		value, err := strconv.ParseInt(x.Value, 0, 64)
		return value, err == nil
	}
	if x.Kind == token.FLOAT {
		value, err := strconv.ParseFloat(x.Value, 64)
		return value, err == nil
	}
	return nil, false
}

// variableName turns source text into a variable name the notation
// accepts: dot-separated parts, none empty, without / or \ (replaced by
// _), on one line.
func variableName(src string) string {
	src = strings.NewReplacer("/", "_", `\`, "_", "\n", " ", "\t", " ").Replace(src)
	parts := strings.Split(src, ".")
	kept := parts[:0]
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			kept = append(kept, p)
		}
	}
	if len(kept) == 0 {
		return "unknown"
	}
	return strings.Join(kept, ".")
}
