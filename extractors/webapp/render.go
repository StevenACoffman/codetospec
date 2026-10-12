package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/printer"
	"go/token"
	"strings"
)

// wording is one language's phrasing of access checks. Fields ending in
// "Fmt" are fmt formats; negated forms come first in each pair.
type wording struct {
	and, or                     string
	holdsFmt, notHoldsFmt       string // %s: Go source
	loggedIn, notLoggedIn       string
	nonPhantom                  string
	currentUserFmt, notCurrFmt  string // %s: user expression
	coachedFmt, notCoachedFmt   string // %s: user expression
	hasPermFmt, notHasPermFmt   string // %s permission, %s scope
	scopeUserFmt, scopeOtherFmt string // %s: user / scope expression
	scopeAny                    string
	serviceAdmin, notSvcAdmin   string
	rejectFmt                   string // %s condition, %s operation, %s error clause
	withErrFmt                  string // %s error name
	openFmt                     string // %s operation
	titleFmt                    string // %s operation
}

// wordingFor returns the wording of lang ("fr" or "en"), French and English
// side by side so neither can miss a phrase.
func wordingFor(lang string) wording {
	tr := func(french, english string) string {
		if lang == "en" {
			return english
		}
		return french
	}
	return wording{
		and:            tr("ET", "AND"),
		or:             tr("OU", "OR"),
		holdsFmt:       tr("`%s` est vrai", "`%s` holds"),
		notHoldsFmt:    tr("`%s` est faux", "`%s` does not hold"),
		loggedIn:       tr("l'appelant est connecté", "the caller is logged in"),
		notLoggedIn:    tr("l'appelant n'est pas connecté", "the caller is not logged in"),
		nonPhantom:     tr(" avec un compte non fantôme", " with a non-phantom account"),
		currentUserFmt: tr("l'appelant est l'utilisateur `%s`", "the caller is the user `%s`"),
		notCurrFmt: tr(
			"l'appelant n'est pas l'utilisateur `%s`",
			"the caller is not the user `%s`",
		),
		coachedFmt: tr(
			"l'appelant est coach de l'utilisateur `%s`",
			"the user `%s` is coached by the caller",
		),
		notCoachedFmt: tr(
			"l'appelant n'est pas coach de l'utilisateur `%s`",
			"the user `%s` is not coached by the caller",
		),
		hasPermFmt: tr(
			"l'appelant a la permission `%s` %s",
			"the caller has permission `%s` %s",
		),
		notHasPermFmt: tr(
			"l'appelant n'a pas la permission `%s` %s",
			"the caller lacks permission `%s` %s",
		),
		scopeUserFmt:  tr("pour l'utilisateur `%s`", "for the user `%s`"),
		scopeOtherFmt: tr("pour `%s`", "for `%s`"),
		scopeAny:      tr("pour tout utilisateur", "for any user"),
		serviceAdmin: tr(
			"la requête est une requête service-admin",
			"the request is a service-admin query",
		),
		notSvcAdmin: tr(
			"la requête n'est pas une requête service-admin",
			"the request is not a service-admin query",
		),
		rejectFmt: tr(
			"SI %s, ALORS le systeme doit rejeter %s%s.",
			"IF %s, THEN the system shall reject %s%s.",
		),
		withErrFmt: tr(" avec l'erreur `%s`", " with the `%s` error"),
		openFmt: tr(
			"Le systeme doit autoriser tout appelant à appeler %s.",
			"The system shall allow any caller to call %s.",
		),
		titleFmt: tr("Contrôle d'accès : %s", "Access control: %s"),
	}
}

// requirement renders g as an EARS requirement for operation (e.g.
// "Mutation.addAuthEmail"): ubiquitous for open access, unwanted otherwise.
func (w *wording) requirement(fset *token.FileSet, g *guard, operation string) string {
	if g.open {
		return fmt.Sprintf(w.openFmt, operation)
	}
	errClause := ""
	if g.errName != "" {
		errClause = fmt.Sprintf(w.withErrFmt, g.errName)
	}
	r := renderer{w: w, fset: fset, bindings: g.bindings}
	return fmt.Sprintf(w.rejectFmt, r.cond(g.cond, false), operation, errClause)
}

// renderer turns a guard condition into prose.
type renderer struct {
	w        *wording
	fset     *token.FileSet
	bindings map[string]*ast.CallExpr
}

// cond renders e, negated when negate is set (De Morgan for && and ||).
func (r renderer) cond(e ast.Expr, negate bool) string {
	switch x := e.(type) {
	case *ast.ParenExpr:
		return r.cond(x.X, negate)
	case *ast.UnaryExpr:
		if x.Op == token.NOT {
			return r.cond(x.X, !negate)
		}
	case *ast.BinaryExpr:
		if x.Op == token.LAND || x.Op == token.LOR {
			isAnd := (x.Op == token.LAND) != negate
			word := r.w.or
			if isAnd {
				word = r.w.and
			}
			return r.operand(x.X, x.Op, negate) + " " + word + " " + r.operand(x.Y, x.Op, negate)
		}
	case *ast.Ident:
		if call, ok := r.bindings[x.Name]; ok {
			return r.check(call, negate)
		}
	case *ast.CallExpr:
		if isAccessCheck(x) {
			return r.check(x, negate)
		}
	}
	return r.verbatim(e, negate)
}

// operand renders one side of a && or ||, parenthesized when it is the
// other operator, so mixed conditions keep their grouping.
func (r renderer) operand(e ast.Expr, parent token.Token, negate bool) string {
	inner := e
	if p, ok := inner.(*ast.ParenExpr); ok {
		inner = p.X
	}
	if b, ok := inner.(*ast.BinaryExpr); ok && (b.Op == token.LAND || b.Op == token.LOR) &&
		b.Op != parent {
		return "(" + r.cond(inner, negate) + ")"
	}
	return r.cond(e, negate)
}

// check renders a known access check, or its source when unknown.
func (r renderer) check(call *ast.CallExpr, negate bool) string {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return r.verbatim(call, negate)
	}
	pick := func(positive, negative string) string {
		if negate {
			return negative
		}
		return positive
	}
	switch sel.Sel.Name {
	case "IsLoggedIn":
		phrase := pick(r.w.loggedIn, r.w.notLoggedIn)
		if r.hasArg(call, "PhantomProhibited") {
			phrase += r.w.nonPhantom
		}
		return phrase
	case "IsCurrentUser":
		return fmt.Sprintf(pick(r.w.currentUserFmt, r.w.notCurrFmt), r.arg(call, 1))
	case "IsCoachedByActor":
		return fmt.Sprintf(pick(r.w.coachedFmt, r.w.notCoachedFmt), r.arg(call, 1))
	case "ActorHasPermission", "ActorHasPermissionErr":
		return fmt.Sprintf(
			pick(r.w.hasPermFmt, r.w.notHasPermFmt),
			r.permission(call),
			r.scope(call),
		)
	case "RequestIsServiceAdminQuery":
		return pick(r.w.serviceAdmin, r.w.notSvcAdmin)
	}
	return r.verbatim(call, negate)
}

// permission names the capability argument without its package
// (capabilities.CanChangeUserData → CanChangeUserData).
func (r renderer) permission(call *ast.CallExpr) string {
	src := r.arg(call, 1)
	if i := strings.LastIndex(src, "."); i >= 0 {
		return src[i+1:]
	}
	return src
}

// scopeArg is the index of the scope argument of acl.ActorHasPermission[Err]
// (ktx, permission, scope).
const scopeArg = 2

// scope renders the scope argument of a permission check.
func (r renderer) scope(call *ast.CallExpr) string {
	if len(call.Args) <= scopeArg {
		return r.w.scopeAny
	}
	switch s := call.Args[scopeArg].(type) {
	case *ast.SelectorExpr:
		if s.Sel.Name == "AnyScope" {
			return r.w.scopeAny
		}
	case *ast.CallExpr:
		if sel, ok := s.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "UserScope" &&
			len(s.Args) == 1 {
			return fmt.Sprintf(r.w.scopeUserFmt, r.source(s.Args[0]))
		}
	}
	return fmt.Sprintf(r.w.scopeOtherFmt, r.source(call.Args[scopeArg]))
}

// arg returns the source of argument i, or "?" when absent.
func (r renderer) arg(call *ast.CallExpr, i int) string {
	if i >= len(call.Args) {
		return "?"
	}
	return r.source(call.Args[i])
}

func (r renderer) hasArg(call *ast.CallExpr, name string) bool {
	for _, a := range call.Args {
		if sel, ok := a.(*ast.SelectorExpr); ok && sel.Sel.Name == name {
			return true
		}
	}
	return false
}

// verbatim renders an unrecognized condition as its Go source: accurate,
// never paraphrased.
func (r renderer) verbatim(e ast.Expr, negate bool) string {
	if negate {
		return fmt.Sprintf(r.w.notHoldsFmt, r.source(e))
	}
	return fmt.Sprintf(r.w.holdsFmt, r.source(e))
}

// source prints e, dropping a leading dereference or address-of so
// "*user.Kaid" reads "user.Kaid".
func (r renderer) source(e ast.Expr) string {
	if u, ok := e.(*ast.UnaryExpr); ok && u.Op == token.AND {
		e = u.X
	}
	if s, ok := e.(*ast.StarExpr); ok {
		e = s.X
	}
	var b bytes.Buffer
	if err := printer.Fprint(&b, r.fset, e); err != nil {
		return "?"
	}
	return b.String()
}
