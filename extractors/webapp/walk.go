package main

import (
	"go/ast"
	"go/token"
	"maps"
)

// maxDepth bounds how deep guards are looked for in nested statements.
const maxDepth = 4

// reach is what holds on the way to a statement: the resolved conditions of
// the enclosing branches, the calls standing for a bound error in them, the
// first line to cite (the outermost statement, or a local it reads), and
// the innermost range loop.
type reach struct {
	conds []ast.Expr
	marks callMarks
	start int // 0 at the top level
	loop  *loop
}

// loop is a range loop a guard sits in: "for _, udiID := range udiIDs".
type loop struct {
	item, over ast.Expr
}

// under returns p extended by cond, entered at line, with res learned from
// resolving cond.
func (p reach) under(cond ast.Expr, line int, res *resolution) reach {
	start := min(line, res.start)
	if p.start != 0 {
		start = min(p.start, start)
	}
	return reach{
		conds: append(append([]ast.Expr(nil), p.conds...), cond),
		marks: p.marks.with(res.marks),
		start: start,
		loop:  p.loop,
	}
}

// branch is a block the walk reached that stops the caller (see exits):
// an if or case body, entered under cond from the statement at line.
type branch struct {
	body []ast.Stmt
	cond ast.Expr // its own condition, resolved
	res  *resolution
	path reach // what holds on the way to it
	line int
	end  token.Pos
}

// branchRule makes a guard of a branch, or reports there is none: denial
// finds the branches that deny, allowance (see permit) those that accept.
type branchRule func(sc scanner, b *branch) (guard, bool)

// guards scans body for the guards of a resolver method, in order. Only
// shapes it recognizes yield a guard; anything else is left to the LLM.
func (sc scanner) guards(body *ast.BlockStmt) []guard {
	sc.rule = scanner.denial
	return sc.walk(body.List, reach{}, make(map[string]binding), 0)
}

// walk scans stmts under p: it binds locals, and finds guards in if
// statements (with their else branches), tagless switches and range loops,
// recursing into their bodies up to maxDepth.
func (sc scanner) walk(stmts []ast.Stmt, p reach, locals map[string]binding, depth int) []guard {
	var found []guard
	for _, stmt := range stmts {
		switch s := stmt.(type) {
		case *ast.AssignStmt:
			sc.bind(s, locals)
			continue
		case *ast.ExprStmt:
			if g, ok := sc.marker(s, depth); ok {
				found = append(found, g)
			}
		case *ast.IfStmt:
			if sc.accumulate(s, locals) {
				continue
			}
			found = append(found, sc.ifChain(s, p, locals, depth)...)
		case *ast.SwitchStmt:
			found = append(found, sc.switchCases(s, p, locals, depth)...)
		case *ast.RangeStmt:
			if depth < maxDepth {
				// A guard in the loop cites it from the range statement, which
				// names the item.
				inner := p
				inner.loop = &loop{item: rangeItem(s), over: s.X}
				inner.start = sc.fset.Position(s.Pos()).Line
				if p.start != 0 {
					inner.start = min(p.start, inner.start)
				}
				found = append(found, sc.walk(s.Body.List, inner, maps.Clone(locals), depth+1)...)
			}
		}
		forget(stmt, locals)
	}
	return found
}

// marker makes a rule of a top-level acl.OpenAccess() or acl.TODO()
// statement (see aclMarker).
func (sc scanner) marker(s *ast.ExprStmt, depth int) (guard, bool) {
	call, ok := s.X.(*ast.CallExpr)
	if !ok || depth != 0 {
		return guard{}, false
	}
	kind, isMarker := aclMarker(call)
	if !isMarker {
		return guard{}, false
	}
	line := sc.fset.Position(s.Pos()).Line
	return guard{kind: kind, start: line, end: line}, true
}

// ifChain finds the guards of an if statement: its own (see denial), those
// nested in its body under its condition, and those of its else branch
// under the negation — unless the body exits, which makes the else branch
// read like the statements after the if.
func (sc scanner) ifChain(s *ast.IfStmt, p reach, locals map[string]binding, depth int) []guard {
	if depth >= maxDepth {
		return nil
	}
	scope := locals
	if s.Init != nil {
		// "if err := check(); err != nil": the assignment binds for the
		// condition and the branches only.
		init, ok := s.Init.(*ast.AssignStmt)
		if !ok {
			return nil
		}
		scope = maps.Clone(locals)
		sc.bind(init, scope)
	}
	res := newResolution()
	cond := sc.resolve(s.Cond, scope, &res)
	line := sc.fset.Position(s.Pos()).Line
	var found []guard
	b := branch{body: s.Body.List, cond: cond, res: &res, path: p, line: line, end: s.End()}
	if g, ok := sc.rule(sc, &b); ok {
		found = append(found, g)
	}
	found = append(
		found,
		sc.walk(s.Body.List, p.under(cond, line, &res), maps.Clone(scope), depth+1)...)
	elsePath := p
	if !exits(s.Body.List, p.loop != nil) {
		negRes := newResolution()
		elsePath = p.under(&ast.UnaryExpr{Op: token.NOT, X: &ast.ParenExpr{X: cond}}, line, &negRes)
		elsePath.marks = elsePath.marks.with(res.marks)
	}
	switch e := s.Else.(type) {
	case *ast.IfStmt:
		found = append(found, sc.ifChain(e, elsePath, scope, depth)...)
	case *ast.BlockStmt:
		found = append(found, sc.walk(e.List, elsePath, maps.Clone(scope), depth+1)...)
	}
	return found
}

// switchCases finds the guards of a tagless switch: case i holds when its
// own condition does and no earlier one did; default when none did.
func (sc scanner) switchCases(
	s *ast.SwitchStmt,
	p reach,
	locals map[string]binding,
	depth int,
) []guard {
	if s.Tag != nil || s.Init != nil || depth >= maxDepth {
		return nil
	}
	line := sc.fset.Position(s.Pos()).Line
	res := newResolution()
	var earlier []ast.Expr // resolved conditions of the cases before
	var found []guard
	for _, stmt := range s.Body.List {
		clause, ok := stmt.(*ast.CaseClause)
		if !ok {
			continue
		}
		reached, own := p, noneOf(earlier) // default: none of the cases held
		if clause.List != nil {
			for _, prev := range earlier {
				reached = reached.under(
					&ast.UnaryExpr{Op: token.NOT, X: &ast.ParenExpr{X: prev}},
					line,
					&res,
				)
			}
			own = sc.resolve(anyOf(clause.List), locals, &res)
			earlier = append(earlier, own)
		}
		if own == nil {
			continue
		}
		b := branch{
			body: clause.Body,
			cond: own,
			res:  &res,
			path: reached,
			line: line,
			end:  clause.End(),
		}
		if g, isGuard := sc.rule(sc, &b); isGuard {
			found = append(found, g)
		}
		found = append(
			found,
			sc.walk(clause.Body, reached.under(own, line, &res), maps.Clone(locals), depth+1)...)
	}
	return found
}

// denial makes a guard of a branch when it stops the caller — it ends in a
// return, or in a continue inside a loop (that item is rejected) — and its
// own condition uses an access check or it returns an authorization error.
func (sc scanner) denial(b *branch) (guard, bool) {
	p := b.path
	inLoop := p.loop != nil
	if !exits(b.body, inLoop) {
		return guard{}, false
	}
	block := &ast.BlockStmt{List: b.body}
	errName, denies := sc.known.errorName(block)
	if !sc.mentions(b.cond, b.res) && !denies {
		return guard{}, false
	}
	start := min(b.line, b.res.start)
	if p.start != 0 {
		start = min(p.start, start)
	}
	kind := ruleAccess
	if containsCall(b.cond, isRateLimit) {
		kind = ruleRateLimit
	}
	return guard{
		kind:    kind,
		cond:    p.and(b.cond),
		outcome: outcomeOf(block, errName),
		errName: errName,
		start:   start,
		end:     sc.fset.Position(b.end).Line,
		marks:   p.marks.with(b.res.marks),
		loop:    p.loop,
		perItem: inLoop && endsInContinue(b.body),
	}, true
}

// and is cond under the conditions of the branches leading to it.
func (p reach) and(cond ast.Expr) ast.Expr {
	full := cond
	for i := len(p.conds) - 1; i >= 0; i-- {
		full = &ast.BinaryExpr{X: p.conds[i], Op: token.LAND, Y: full}
	}
	return full
}

// anyOf is the condition of a case listing several: a || b || ….
func anyOf(list []ast.Expr) ast.Expr {
	cond := list[0]
	for _, e := range list[1:] {
		cond = &ast.BinaryExpr{X: cond, Op: token.LOR, Y: e}
	}
	return cond
}

// allOf is the condition of conds all holding: a && b && …; conds is not
// empty.
func allOf(conds []ast.Expr) ast.Expr {
	all := conds[0]
	for _, c := range conds[1:] {
		all = &ast.BinaryExpr{X: all, Op: token.LAND, Y: c}
	}
	return all
}

// noneOf is the condition of a switch's default: !a && !b && …; nil when
// the switch has no other case.
func noneOf(conds []ast.Expr) ast.Expr {
	var none ast.Expr
	for _, c := range conds {
		not := &ast.UnaryExpr{Op: token.NOT, X: &ast.ParenExpr{X: c}}
		if none == nil {
			none = not
		} else {
			none = &ast.BinaryExpr{X: none, Op: token.LAND, Y: not}
		}
	}
	return none
}

// rangeItem is the variable a range loop names each element by: its value,
// or its key when the value is absent.
func rangeItem(s *ast.RangeStmt) ast.Expr {
	if s.Value != nil {
		return s.Value
	}
	return s.Key
}

// exits reports whether a branch stops: it ends in a return or, inside a
// loop, in a continue (the item is rejected). A branch that merely
// contains a return (if hasAny { … if err != nil { return … } … }) lets
// control fall through.
func exits(body []ast.Stmt, inLoop bool) bool {
	if len(body) == 0 {
		return false
	}
	switch last := body[len(body)-1].(type) {
	case *ast.ReturnStmt:
		return true
	case *ast.BranchStmt:
		return inLoop && last.Tok == token.CONTINUE
	}
	return false
}

// endsInContinue reports whether a branch ends in a continue.
func endsInContinue(body []ast.Stmt) bool {
	last, ok := body[len(body)-1].(*ast.BranchStmt)
	return ok && last.Tok == token.CONTINUE
}
