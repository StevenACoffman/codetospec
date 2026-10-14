package main

import (
	"fmt"
	"go/ast"
	"go/token"
	"strings"
)

// A policy (rulePermit) is a function that decides access as an allow-list:
// it returns only an error, accepts on some paths (return nil) and ends in
// a denial — graphql-gateway's _validatePermission and ValidateCSRF. Its
// rule lists every condition it accepts under: each is necessary for its
// path, so "accepts only when one of them holds" is exact about what the
// code allows, whatever else rejects on the way.

// policy is a policy function and its rule.
type policy struct {
	decl funcDecl
	rule guard
}

// policies finds the policy functions of sources, in source order.
func policies(sources []source, known checks) []policy {
	idx := indexFuncs(sources)
	var found []policy
	for i := range sources {
		s := &sources[i]
		sc := scanner{fset: s.fset, known: known}
		for _, decl := range s.file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			if g, isPolicy := sc.permit(fn, idx.denier(s.file.Name.Name)); isPolicy {
				found = append(found, policy{decl: funcDecl{file: s, fn: fn}, rule: g})
			}
		}
	}
	return found
}

// policyFacts returns one rule per policy, cited on the whole function, in
// the domain domainAt gives it.
func policyFacts(found []policy, w *wording, domainAt func(string, int) string) []Fact {
	var facts []Fact
	for _, p := range found {
		s, fn := p.decl.file, p.decl.fn
		m := resolverMethod{method: fn.Name.Name, guards: []guard{p.rule}}
		op := operation{name: fn.Name.Name, domain: domainAt(s.name, p.rule.start)}
		facts = append(facts, ruleFacts(s.fset, w, s.name, &m, &op)...)
	}
	return facts
}

// permit returns the policy rule of fn when fn is an allow-list (see
// rulePermit); denies says whether a call's error denies access.
func (sc scanner) permit(fn *ast.FuncDecl, denies func(*ast.CallExpr) bool) (guard, bool) {
	if !returnsOnlyError(fn.Type) || len(fn.Body.List) == 0 {
		return guard{}, false
	}
	last, ok := fn.Body.List[len(fn.Body.List)-1].(*ast.ReturnStmt)
	if !ok || len(last.Results) != 1 {
		return guard{}, false
	}
	errName, isDenial := sc.denialOf(last.Results[0], fn.Body.List, denies)
	if !isDenial {
		return guard{}, false
	}
	sc.rule = scanner.allowance
	allows := sc.walk(fn.Body.List, reach{}, make(map[string]binding), 0)
	if len(allows) == 0 {
		return guard{}, false
	}
	g := guard{
		kind:    rulePermit,
		errName: errName,
		start:   sc.fset.Position(fn.Pos()).Line,
		end:     sc.fset.Position(fn.End()).Line,
		marks:   newCallMarks(),
	}
	for _, a := range allows {
		g.alts = append(g.alts, a)
		g.marks = g.marks.with(a.marks)
	}
	if errName != "" {
		g.outcome = outcomeError
	}
	return g, true
}

// denialOf reports whether the returned result denies access, and names
// it when the function itself does: an authorization error built there
// (errors.Unauthorized(…)) or held in a variable (RateLimitExceeded), or a
// local bound at the top level to a call whose error denies
// (validateErr := _validateCSRF(…); …; return validateErr), named by no
// error of its own.
func (sc scanner) denialOf(
	result ast.Expr,
	stmts []ast.Stmt,
	denies func(*ast.CallExpr) bool,
) (string, bool) {
	name, isDenial := sc.known.errorName(&ast.BlockStmt{List: []ast.Stmt{&ast.ReturnStmt{
		Results: []ast.Expr{result},
	}}})
	if isDenial {
		return name, true
	}
	ident, ok := result.(*ast.Ident)
	if !ok {
		return "", false
	}
	var bound *ast.CallExpr
	for _, stmt := range stmts {
		if assign, isAssign := stmt.(*ast.AssignStmt); isAssign && len(assign.Lhs) == 1 {
			if lhs, isIdent := assign.Lhs[0].(*ast.Ident); isIdent && lhs.Name == ident.Name {
				bound, _ = assign.Rhs[0].(*ast.CallExpr)
			}
		}
	}
	return "", bound != nil && (denies(bound) || sc.known.errDenies(bound))
}

// allowance makes a guard of a branch that accepts: it ends in return nil.
func (sc scanner) allowance(b *branch) (guard, bool) {
	ret, ok := b.body[len(b.body)-1].(*ast.ReturnStmt)
	if !ok || len(ret.Results) != 1 {
		return guard{}, false
	}
	if null, isIdent := ret.Results[0].(*ast.Ident); !isIdent || null.Name != goNil {
		return guard{}, false
	}
	return guard{
		cond:  b.path.and(b.cond),
		marks: b.path.marks.with(b.res.marks),
		loop:  b.path.loop,
	}, true
}

// returnsOnlyError reports whether a function's sole result is an error.
func returnsOnlyError(fn *ast.FuncType) bool {
	return fn.Results != nil && len(fn.Results.List) == 1 &&
		len(fn.Results.List[0].Names) <= 1 && returnsErrorLast(fn)
}

// denier returns whether a call made in package pkg is to a function of
// sources that builds an authorization error itself (_validateCSRF).
func (idx funcIndex) denier(pkg string) func(*ast.CallExpr) bool {
	return func(call *ast.CallExpr) bool {
		key := ""
		switch f := call.Fun.(type) {
		case *ast.Ident:
			key = pkg + "." + f.Name
		case *ast.SelectorExpr:
			if q, ok := f.X.(*ast.Ident); ok {
				key = q.Name + "." + f.Sel.Name
			}
		}
		d, found := idx.one(key)
		return found && buildsAuthorizationError(d.fn.Body)
	}
}

// policy renders a policy rule: the conditions it accepts under, each
// grouped (or for some item of its loop), joined by OR.
func (r renderer) policy(g *guard, name string) string {
	alts := make([]string, len(g.alts))
	for i := range g.alts {
		a := &g.alts[i]
		alts[i] = r.operand(a.cond, token.LOR, false)
		if a.loop != nil {
			alts[i] = "(" + fmt.Sprintf(
				r.w.forSomeFmt, r.source(a.loop.item), r.source(a.loop.over), r.cond(a.cond, false),
			) + ")"
		}
	}
	errClause := ""
	if g.outcome == outcomeError {
		errClause = fmt.Sprintf(r.w.withErrFmt, g.errName)
	}
	return fmt.Sprintf(r.w.permitFmt, name, strings.Join(alts, " "+r.w.or+" "), errClause)
}

// addPolicyTexts sets the phrases of policy rules and of map lookups.
func addPolicyTexts(w *wording, tr translate) {
	w.policyTitleFmt = tr("Politique d'accès : %s", "Access policy: %s")
	w.permitFmt = tr(
		"Le systeme doit accepter une requête vérifiée par `%s` seulement si %s, et la rejeter sinon%s.",
		"The system shall accept a request checked by `%s` only when %s, and reject it otherwise%s.",
	)
	w.keyFmt = tr("`%s` est une clé de `%s`", "`%s` is a key of `%s`")
	w.notKeyFmt = tr("`%s` n'est pas une clé de `%s`", "`%s` is not a key of `%s`")
}
