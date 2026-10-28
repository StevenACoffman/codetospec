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
	and, or                      string
	holdsFmt, notHoldsFmt        string // %s: Go source
	failsFmt, succeedsFmt        string // %s: Go source of a call
	loggedIn, notLoggedIn        string
	nonPhantom                   string
	currentUserFmt, notCurrFmt   string // %s: user expression
	selfFmt, notSelfFmt          string // %s: the caller's own kaid
	coachedFmt, notCoachedFmt    string // %s: user expression
	hasPermFmt, notHasPermFmt    string // %s permission, %s scope
	scopeUserFmt, scopeOtherFmt  string // %s: user / scope expression
	scopeAny                     string
	serviceAdmin, notSvcAdmin    string
	rejectFmt                    string // %s condition, %s operation, %s error clause
	emptyFmt                     string // %s condition, %s operation
	rejectItemFmt                string // %s condition, %s item, %s operation, %s error clause
	forSomeFmt                   string // %s item, %s collection, %s condition
	withErrFmt                   string // %s error name
	openFmt                      string // %s operation
	titleFmt                     string // %s operation
	visTitleFmt                  string // %s operation
	checkTitleFmt                string // %s operation
	uncheckedTitleFmt            string // %s operation
	uncheckedFmt                 string // %s operation
	roleTitleFmt                 string // %s role
	roleFmt                      string // %s role, %s (description), %s capabilities
	roleNoneFmt                  string // %s role, %s (description)
	checkedFmt                   string // %s operation, %s checker
	visibilityFmt                string // %s operation, %s what, %s filter call
	resultsNoun                  string
	rateTitleFmt                 string // %s operation
	overLimitFmt                 string // %s key, %s scope, %s limit
	withinLimitFmt               string // same arguments
	byIP, byAccount, byRecipient string
	limitFmt                     string // %s count, %s window
	seconds, minutes, hours      string
	defaultLimit                 string
	policyTitleFmt               string // %s function
	permitFmt                    string // %s function, %s conditions, %s error clause
	keyFmt, notKeyFmt            string // %s key, %s map
	tableTitleFmt                string // %s table
	tableSetFmt                  string // %s keys, %s table, %s reader
	tableMapFmt                  string // %s pairs, %s table, %s reader
	pairFmt                      string // %s key, %s value
	tableListFmt                 string // %s values, %s key, %s table, %s reader
	rateTableFmt                 string // %s operation, %s limits
	tableLimitFmt                string // %s limit, %s scope
	second, minute, hour         string
	alsoWord                     string // joins a table's limits
	throughFmt                   string // %s wrapper: what its guards reject
	isFmt, isNotFmt              string // %s request value, %s compared value
	servedTitleFmt               string // %s path
	servedFmt                    string // %s path, %s condition
	contractTitleFmt             string // %s header
	sendsFmt                     string // %s secret, %s store name, %s header
	requiresFmt                  string // %s header, %s secret, %s store name
	forwardsFmt                  string // %s headers, %s list variable
	withListsFmt                 string // %s headers, %s other lists
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
	w := wording{
		and:            tr("ET", "AND"),
		or:             tr("OU", "OR"),
		holdsFmt:       tr("`%s` est vrai", "`%s` holds"),
		notHoldsFmt:    tr("`%s` est faux", "`%s` does not hold"),
		failsFmt:       tr("`%s` échoue", "`%s` fails"),
		isFmt:          tr("`%s` vaut `%s`", "`%s` is `%s`"),
		isNotFmt:       tr("`%s` ne vaut pas `%s`", "`%s` is not `%s`"),
		succeedsFmt:    tr("`%s` réussit", "`%s` succeeds"),
		loggedIn:       tr("l'appelant est connecté", "the caller is logged in"),
		notLoggedIn:    tr("l'appelant n'est pas connecté", "the caller is not logged in"),
		nonPhantom:     tr(" avec un compte non fantôme", " with a non-phantom account"),
		currentUserFmt: tr("l'appelant est l'utilisateur `%s`", "the caller is the user `%s`"),
		notCurrFmt: tr(
			"l'appelant n'est pas l'utilisateur `%s`",
			"the caller is not the user `%s`",
		),
		selfFmt: tr(
			"l'appelant est l'utilisateur `%s`, lui-même (toujours vrai)",
			"the caller is the user `%s`, the caller themselves (always true)",
		),
		notSelfFmt: tr(
			"l'appelant n'est pas l'utilisateur `%s`, lui-même (jamais vrai)",
			"the caller is not the user `%s`, the caller themselves (never true)",
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
	}
	addRuleTexts(&w, tr)
	addRateLimitTexts(&w, tr)
	addPolicyTexts(&w, tr)
	addTableTexts(&w, tr)
	addSharedTexts(&w, tr)
	addContractTexts(&w, tr)
	addServedTexts(&w, tr)
	return w
}

// translate picks the French or the English of a phrase.
type translate func(french, english string) string

// addRuleTexts sets the phrases of whole rules: outcomes and titles.
func addRuleTexts(w *wording, tr translate) {
	w.rejectFmt = tr(
		"SI %s, ALORS le système doit rejeter %s%s.",
		"IF %s, THEN the system shall reject %s%s.",
	)
	w.emptyFmt = tr(
		"SI %s, ALORS le système doit ne renvoyer aucune donnée pour %s.",
		"IF %s, THEN the system shall return no data for %s.",
	)
	w.withErrFmt = tr(" avec l'erreur `%s`", " with the `%s` error")
	w.openFmt = tr(
		"Le système doit autoriser tout appelant à appeler %s.",
		"The system shall allow any caller to call %s.",
	)
	w.rejectItemFmt = tr(
		"SI %s, ALORS le système doit rejeter cet élément `%s` de %s%s.",
		"IF %s, THEN the system shall reject that `%s` of %s%s.",
	)
	w.forSomeFmt = tr(
		"pour un `%s` de `%s`, %s",
		"for some `%s` in `%s`, %s",
	)
	w.titleFmt = tr("Contrôle d'accès : %s", "Access control: %s")
	w.visTitleFmt = tr("Visibilité : %s", "Visibility: %s")
	w.checkTitleFmt = tr("Vérification d'accès : %s", "Access check: %s")
	w.uncheckedTitleFmt = tr("Accès non vérifié : %s", "Unchecked access: %s")
	w.roleTitleFmt = tr("Rôle : %s", "Role: %s")
	w.roleFmt = tr(
		"Le système doit accorder au rôle `%s`%s les capacités %s.",
		"The system shall grant the `%s` role%s the capabilities %s.",
	)
	w.roleNoneFmt = tr(
		"Le système ne doit accorder aucune capacité au rôle `%s`%s.",
		"The system shall grant the `%s` role%s no capability.",
	)
	w.uncheckedFmt = tr(
		"Le système doit laisser tout appelant appeler %s sans contrôle de permission (`acl.TODO()` : le contrôle manque encore).",
		"The system shall let any caller call %s without a permission check (`acl.TODO()`: the check is still missing).",
	)
	w.checkedFmt = tr(
		"Le système doit vérifier l'accès à %s avec `%s`.",
		"The system shall check access to %s with `%s`.",
	)
	w.visibilityFmt = tr(
		"Le système doit limiter %s aux %s que `%s` laisse voir à l'appelant.",
		"The system shall limit %s to the %s that `%s` lets the caller see.",
	)
	w.resultsNoun = tr("résultats", "results")
}

// addRateLimitTexts sets the phrases of rate-limit conditions.
func addRateLimitTexts(w *wording, tr translate) {
	w.rateTitleFmt = tr("Limite de débit : %s", "Rate limit: %s")
	w.overLimitFmt = tr(
		"l'appelant dépasse la limite de débit %s %s (%s)",
		"the caller exceeds the %s rate limit %s (%s)",
	)
	w.withinLimitFmt = tr(
		"l'appelant ne dépasse pas la limite de débit %s %s (%s)",
		"the caller is within the %s rate limit %s (%s)",
	)
	w.byIP = tr("par adresse IP", "by IP")
	w.byAccount = tr("par compte", "by account")
	w.byRecipient = tr("par destinataire d'e-mail", "by email recipient")
	w.limitFmt = tr("%s requêtes par %s", "%s requests per %s")
	w.seconds = tr("secondes", "seconds")
	w.minutes = tr("minutes", "minutes")
	w.hours = tr("heures", "hours")
	w.defaultLimit = tr("sa limite par défaut", "its default limit")
}

// requirement renders g as an EARS requirement for operation (e.g.
// "Mutation.addAuthEmail"): ubiquitous for open access, unwanted otherwise.
func (w *wording) requirement(fset *token.FileSet, g *guard, operation string) string {
	switch g.kind {
	case ruleOpen:
		return fmt.Sprintf(w.openFmt, operation)
	case ruleUnchecked:
		return fmt.Sprintf(w.uncheckedFmt, operation)
	case ruleVisibility:
		r := renderer{w: w, fset: fset}
		noun := w.resultsNoun
		if g.noun != "" {
			noun = "`" + g.noun + "`"
		}
		return fmt.Sprintf(w.visibilityFmt, operation, noun, r.source(g.subject))
	case ruleChecked:
		r := renderer{w: w, fset: fset}
		return fmt.Sprintf(w.checkedFmt, operation, r.source(g.subject))
	case rulePermit:
		r := renderer{w: w, fset: fset, marks: g.marks}
		return r.policy(g, operation)
	case ruleAccess, ruleRateLimit:
	}
	r := renderer{w: w, fset: fset, marks: g.marks}
	cond := r.cond(g.cond, false)
	errClause := ""
	if g.outcome == outcomeError {
		errClause = fmt.Sprintf(w.withErrFmt, g.errName)
	}
	switch {
	case g.loop != nil && g.perItem:
		return fmt.Sprintf(w.rejectItemFmt, cond, r.source(g.loop.item), operation, errClause)
	case g.loop != nil:
		cond = fmt.Sprintf(w.forSomeFmt, r.source(g.loop.item), r.source(g.loop.over), cond)
	}
	switch g.outcome {
	case outcomeEmpty:
		return fmt.Sprintf(w.emptyFmt, cond, operation)
	case outcomeError:
		return fmt.Sprintf(w.rejectFmt, cond, operation, fmt.Sprintf(w.withErrFmt, g.errName))
	case outcomeUnknown:
	}
	return fmt.Sprintf(w.rejectFmt, cond, operation, "")
}

// renderer turns a guard condition into prose.
type renderer struct {
	w     *wording
	fset  *token.FileSet
	marks callMarks // see guard.marks
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
		if text, ok := r.comparison(x, negate); ok {
			return text
		}
	case *ast.CallExpr:
		if isRateLimit(x) || isAccessCheck(x) {
			return r.call(x, negate)
		}
	case *ast.IndexExpr:
		if r.marks.present[x] {
			format := r.w.keyFmt
			if negate {
				format = r.w.notKeyFmt
			}
			return fmt.Sprintf(format, r.source(x.Index), r.source(x.X))
		}
	}
	return r.verbatim(e, negate)
}

// comparison renders the comparisons the renderer words: a bound error's
// (see failure), and a request value a local held, side by side ("`h` is
// not `expected`") since no line of code writes them together.
func (r renderer) comparison(x *ast.BinaryExpr, negate bool) (string, bool) {
	if text, ok := r.failure(x, negate); ok {
		return text, true
	}
	if !r.marks.fromLocal[x] {
		return "", false
	}
	format := r.w.isFmt
	if (x.Op == token.NEQ) != negate {
		format = r.w.isNotFmt
	}
	return fmt.Sprintf(format, r.source(x.X), r.source(x.Y)), true
}

// failure renders "err != nil" (or "== nil") on the error of a bound call
// as "`call` fails" (or succeeds).
func (r renderer) failure(x *ast.BinaryExpr, negate bool) (string, bool) {
	call, ok := x.X.(*ast.CallExpr)
	if !ok || !r.marks.failed[call] || (x.Op != token.EQL && x.Op != token.NEQ) {
		return "", false
	}
	format := r.w.succeedsFmt
	if (x.Op == token.NEQ) != negate {
		format = r.w.failsFmt
	}
	return fmt.Sprintf(format, r.source(call)), true
}

// call renders a check call: a rate limit, a known access check, or its
// source.
func (r renderer) call(call *ast.CallExpr, negate bool) string {
	if isRateLimit(call) {
		return r.rateLimit(call, negate)
	}
	return r.check(call, negate)
}

// operand renders one side of a && or ||, parenthesized when it reads as
// the other operator — after De Morgan too: !(a && b) under && reads
// "(not a OR not b)" — so mixed conditions keep their grouping.
func (r renderer) operand(e ast.Expr, parent token.Token, negate bool) string {
	if op, ok := logicalOp(e); ok && op != parent {
		return "(" + r.cond(e, negate) + ")"
	}
	return r.cond(e, negate)
}

// logicalOp is the && or || that e reads as, through parentheses and
// negations (each ! swaps them); false when e is no logical operation.
func logicalOp(e ast.Expr) (token.Token, bool) {
	flipped := false
	for {
		switch x := e.(type) {
		case *ast.ParenExpr:
			e = x.X
			continue
		case *ast.UnaryExpr:
			if x.Op == token.NOT {
				e, flipped = x.X, !flipped
				continue
			}
		case *ast.BinaryExpr:
			switch {
			case x.Op != token.LAND && x.Op != token.LOR:
			case flipped && x.Op == token.LAND:
				return token.LOR, true
			case flipped:
				return token.LAND, true
			default:
				return x.Op, true
			}
		}
		return token.ILLEGAL, false
	}
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
		if r.marks.self[call] {
			return fmt.Sprintf(pick(r.w.selfFmt, r.w.notSelfFmt), r.subject(call))
		}
		return fmt.Sprintf(pick(r.w.currentUserFmt, r.w.notCurrFmt), r.subject(call))
	case "IsCoachedByActor":
		return fmt.Sprintf(pick(r.w.coachedFmt, r.w.notCoachedFmt), r.subject(call))
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
	src := r.subject(call)
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

// subject returns the source of the argument after the context (the user,
// the key), or "?" when absent.
func (r renderer) subject(call *ast.CallExpr) string {
	if len(call.Args) < subjectArg+1 {
		return "?"
	}
	return r.source(call.Args[subjectArg])
}

// subjectArg is the index of the argument after the context.
const subjectArg = 1

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
	return sourceLine(b.String())
}

// sourceLine joins a multi-line call's source into one line:
// "f(\n\tktx,\n\tx,\n)" → "f(ktx, x)".
func sourceLine(src string) string {
	return strings.NewReplacer("( ", "(", ", )", ")", " )", ")").Replace(oneLine(src))
}
