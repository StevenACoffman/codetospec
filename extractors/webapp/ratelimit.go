package main

import (
	"fmt"
	"go/ast"
	"go/token"
	"strconv"
	"strings"
)

// ratelimitPkg is webapp's rate limiter (pkg/web/ratelimit): its By…
// functions return true when a request is over its limit.
const ratelimitPkg = "ratelimit"

// rateLimitScopes words what each limiter counts by.
func rateLimitScopes(w *wording) map[string]string {
	return map[string]string{
		"ByIP":             w.byIP,
		"ByAccount":        w.byAccount,
		"ByEmailRecipient": w.byRecipient,
	}
}

// isRateLimit reports whether call is ratelimit.By…(…), a check that
// rejects a caller over its limit.
func isRateLimit(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, isIdent := sel.X.(*ast.Ident)
	return isIdent && pkg.Name == ratelimitPkg && strings.HasPrefix(sel.Sel.Name, "By")
}

// rateLimit renders a ratelimit.By…(ctx, key, …, options) call: the key
// and the limit its options set (MaxRequestsPerWindow, LimitWindow), with
// numbers as the code writes them so grounding finds them; "its default
// limit" when the call sets none.
func (r renderer) rateLimit(call *ast.CallExpr, negate bool) string {
	sel, isSel := call.Fun.(*ast.SelectorExpr)
	if !isSel {
		return r.verbatim(call, negate)
	}
	scope, known := rateLimitScopes(r.w)[sel.Sel.Name]
	if !known {
		return r.verbatim(call, negate)
	}
	count, window := option(call, "MaxRequestsPerWindow"), option(call, "LimitWindow")
	limit := r.w.defaultLimit
	if count != nil && window != nil {
		limit = fmt.Sprintf(r.w.limitFmt, r.quantity(count), r.duration(window))
	}
	format := r.w.overLimitFmt
	if negate {
		format = r.w.withinLimitFmt
	}
	return fmt.Sprintf(format, r.key(call), scope, limit)
}

// key is the limiter's name, its second argument: the string's value, or
// the expression's source.
func (r renderer) key(call *ast.CallExpr) string {
	if len(call.Args) > subjectArg {
		if lit, ok := call.Args[subjectArg].(*ast.BasicLit); ok && lit.Kind == token.STRING {
			if value, err := strconv.Unquote(lit.Value); err == nil {
				return "`" + value + "`"
			}
		}
	}
	return "`" + r.subject(call) + "`"
}

// quantity renders a count: a literal as the number, anything else as its
// quoted source.
func (r renderer) quantity(e ast.Expr) string {
	if lit, ok := e.(*ast.BasicLit); ok && lit.Kind == token.INT {
		return lit.Value
	}
	return "`" + r.source(e) + "`"
}

// duration renders a window: "N * time.Unit" (either order) as "N units",
// anything else as its quoted source.
func (r renderer) duration(e ast.Expr) string {
	if b, ok := e.(*ast.BinaryExpr); ok && b.Op == token.MUL {
		for _, pair := range [][2]ast.Expr{{b.X, b.Y}, {b.Y, b.X}} {
			if words, isUnits := r.units(pair[0], pair[1]); isUnits {
				return words
			}
		}
	}
	return "`" + r.source(e) + "`"
}

// units renders count * unit when count is an integer literal and unit a
// time package unit.
func (r renderer) units(count, unit ast.Expr) (string, bool) {
	lit, isLit := count.(*ast.BasicLit)
	sel, isSel := unit.(*ast.SelectorExpr)
	if !isLit || lit.Kind != token.INT || !isSel {
		return "", false
	}
	pkg, isPkg := sel.X.(*ast.Ident)
	word, known := timeUnits(r.w)[sel.Sel.Name]
	if !isPkg || pkg.Name != "time" || !known {
		return "", false
	}
	return lit.Value + " " + word, true
}

// timeUnits words the time package's units, plural.
func timeUnits(w *wording) map[string]string {
	return map[string]string{"Second": w.seconds, "Minute": w.minutes, "Hour": w.hours}
}

// option returns the argument of the ratelimit.<name>(…) option among
// call's arguments; nil when absent.
func option(call *ast.CallExpr, name string) ast.Expr {
	for _, a := range call.Args {
		opt, ok := a.(*ast.CallExpr)
		if !ok || len(opt.Args) != 1 {
			continue
		}
		if sel, isSel := opt.Fun.(*ast.SelectorExpr); isSel && sel.Sel.Name == name {
			return opt.Args[0]
		}
	}
	return nil
}
