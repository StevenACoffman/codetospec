package main

import (
	"encoding/json"
	"go/ast"
	"go/token"
	"slices"
	"strconv"
	"strings"
)

// The access_check kind and the attributes naming what a guard checks
// (codetospec's extract.KindAccessCheck, extract.AttrChecks,
// extract.AttrCheckArgs, extract.AttrCheckScope, extract.AttrForm).
const (
	kindAccessCheck = "access_check"
	attrChecks      = "checks"
	attrCheckArgs   = "check_args"
	attrCheckScope  = "scope_of_check"
	attrForm        = "form"
	// Allowing paths of a service's own checker (see allowingPaths), as
	// extract.AttrPaths, AttrNotAnalyzable and AttrNotAnalyzableReason.
	attrPaths               = "paths"
	attrNotAnalyzable       = "not_analyzable"
	attrNotAnalyzableReason = "not_analyzable_reason"
	// attrDelegatesTo is the resolver method ("recv.Method") a delegation
	// rule is checked as: its checks are that method's.
	attrDelegatesTo = "delegates_to"
	// attrAnswers names the checks whose result the guarded resolver
	// returns as its value (see checks.answers), as extract.AttrAnswers.
	attrAnswers = "answers"
	// attrDelegatesToOperation is the operation that method serves, when
	// the service's schema has it: the delegation is checked as it.
	attrDelegatesToOperation = "delegates_to_operation"
)

// Values of attrChecks for a rule that calls no check, and of
// attrCheckScope for a guard that drops one item instead of the call.
const (
	checkOpen      = "open"
	checkTodo      = "todo"
	checkScopeItem = "item"
)

// Values of attrForm: how the extractor knows a call is a check.
const (
	formMarked    = "marked"    // marked //ka:permission-check (see markedCheckers)
	formPredicate = "predicate" // an access package's bool function (accessPredicates)
	formHelper    = "helper"    // a resolver helper returning a check (helperPredicates)
	formACL       = "acl"       // named like an acl predicate (isAccessCheck)
	formRateLimit = "ratelimit" // a rate limiter (isRateLimit)
	// formCall: a function a denying guard calls that is no known check
	// (scanner.unknownCalls): named so a person can pin it, with no meaning.
	formCall = "call"
)

// checkCall is one check a guard calls, with the operands the condition
// notation leaves out (it has no call operands): the policy inventory and
// the policy exports read them. Check is the check's name, as its
// access_check fact gives it; the other fields are set only when the call
// passes them.
type checkCall struct {
	Check string `json:"check"`
	// Form is set only for a call no known check makes (formCall): the
	// name alone can't tell it from a helper.
	Form string `json:"form,omitempty"`
	// Var is the condition variable the call became, so a reader links
	// each variable to its call exactly; Fails says the variable holds
	// when the call fails (a bound error). Unset outside the condition.
	Var   string `json:"var,omitempty"`
	Fails bool   `json:"fails,omitempty"`
	// Means is what the check means (meaningOf), Scope what its matcher
	// means (scopeOf); unset when unknown.
	Means string `json:"means,omitempty"`
	Scope string `json:"scope,omitempty"`
	// Capability is the Go name of a capabilities.X argument: its stored
	// value lives in generated code this extractor doesn't read.
	Capability string `json:"capability,omitempty"`
	// Matcher is the scope matcher argument as itself (acl.GlobalScope,
	// acl.UserScope), never a meaning: each matcher decides differently.
	Matcher string `json:"matcher,omitempty"`
	// MatcherArg is the matcher's argument, as written (acl.UserScope(kaid)).
	MatcherArg string `json:"matcher_arg,omitempty"`
	// Subject is the user a self or coach check is about, as written.
	Subject string `json:"subject,omitempty"`
	// SubjectFrom says where the user a self, coach or user-scope check
	// is about comes from (resolverMethod.bind); unset for a local.
	SubjectFrom string    `json:"subject_from,omitempty"`
	Rate        *rateArgs `json:"rate,omitempty"`
	// Args are the arguments of a check with no known meaning, so a
	// person pinning one can name its subject.
	Args []callArg `json:"args,omitempty"`
}

// callArg is one argument as written, and where it comes from.
type callArg struct {
	Text string `json:"text"`
	From string `json:"from,omitempty"`
}

// rateArgs is a rate limit's key and limit, as the code writes them.
type rateArgs struct {
	Key    string `json:"key"`
	Count  string `json:"count,omitempty"`
	Window string `json:"window,omitempty"`
}

// subjectChecks are the acl checks about one user, whose subject argument
// check_args records: the caller is that user, or coaches them.
func subjectChecks() []string {
	return []string{"acl.IsCurrentUser", "acl.IsCoachedByActor"}
}

// checkAttrs returns the attributes naming what g checks: attrChecks (the
// names of the checks it calls, sorted and joined by ",", or a marker's
// value), attrCheckArgs (its check calls in source order, as JSON, each
// operand's origin told by bind) and attrCheckScope for a guard that drops
// one item. Nothing for a guard that calls no known check.
func (c checks) checkAttrs(
	fset *token.FileSet,
	g *guard,
	bind func(string) string,
) map[string]string {
	attrs := map[string]string{}
	switch g.kind {
	case ruleOpen:
		attrs[attrChecks] = checkOpen
		return attrs
	case ruleUnchecked:
		attrs[attrChecks] = checkTodo
		return attrs
	case ruleVisibility:
		attrs[attrCheckScope] = checkScopeItem
	case ruleAccess, ruleRateLimit, ruleChecked, rulePermit:
		if g.perItem {
			attrs[attrCheckScope] = checkScopeItem
		}
	}
	calls := c.checkCalls(fset, g)
	if len(calls) == 0 {
		return attrs
	}
	for i := range calls {
		calls[i].bindOperands(bind)
	}
	names := make([]string, 0, len(calls))
	for _, call := range calls {
		if !slices.Contains(names, call.Check) {
			names = append(names, call.Check)
		}
	}
	slices.Sort(names)
	attrs[attrChecks] = strings.Join(names, ",")
	if data, err := json.Marshal(calls); err == nil {
		attrs[attrCheckArgs] = string(data)
	}
	return attrs
}

// checkCalls lists the known checks g's condition and subject call, in
// source order, with their operands.
func (c checks) checkCalls(fset *token.FileSet, g *guard) []checkCall {
	col := callCollector{
		known:     c,
		fset:      fset,
		g:         g,
		cond:      conditioner{r: renderer{fset: fset, marks: g.marks}},
		withVars:  conditionOf(fset, g) != "",
		unknownAt: make(map[*ast.CallExpr]bool, len(g.unknown)),
	}
	for _, call := range g.unknown {
		col.unknownAt[call] = true
	}
	if g.cond != nil {
		col.inCondition = true
		ast.Inspect(g.cond, col.visit)
		col.inCondition = false
	}
	if g.subject != nil {
		// A visibility or checked rule names its checker, called or not.
		subject := g.subject
		if _, isCall := subject.(*ast.CallExpr); !isCall {
			subject = &ast.CallExpr{Fun: subject}
		}
		ast.Inspect(subject, col.visit)
	}
	return col.calls
}

// callCollector gathers a guard's check calls (see checks.checkCalls).
type callCollector struct {
	known       checks
	fset        *token.FileSet
	g           *guard
	cond        conditioner
	withVars    bool // the guard has a condition: calls name their variables
	inCondition bool
	unknownAt   map[*ast.CallExpr]bool // g.unknown
	calls       []checkCall
}

// visit adds the calls of n: "call != nil" on a failed call is the call's
// failure (see conditioner.comparison), a bare call its decision.
func (col *callCollector) visit(n ast.Node) bool {
	switch x := n.(type) {
	case *ast.BinaryExpr:
		if call, isCall := x.X.(*ast.CallExpr); isCall && col.g.marks.failed[call] != 0 &&
			(x.Op == token.EQL || x.Op == token.NEQ) {
			col.add(call, true)
			for _, arg := range call.Args {
				ast.Inspect(arg, col.visit)
			}
			return false
		}
	case *ast.CallExpr:
		col.add(x, false)
	}
	return true
}

// add records call, a known check or one of the guard's unknown calls
// (formCall), once per variable.
func (col *callCollector) add(call *ast.CallExpr, fails bool) {
	name, known := col.known.nameOf(call)
	unknown := !known && col.unknownAt[call]
	if unknown {
		name = unknownName(call)
	}
	if !known && !unknown {
		return
	}
	out := callOperands(col.fset, call, name)
	if unknown {
		out.Form = formCall
	}
	if col.inCondition && col.withVars {
		out.Var = col.cond.decisionVariable(call)
		if fails {
			out.Var, out.Fails = col.cond.callVariable(call)
		}
	}
	if !slices.ContainsFunc(col.calls, func(o checkCall) bool {
		return o.Check == out.Check && o.Var == out.Var && o.Var != ""
	}) {
		col.calls = append(col.calls, out)
	}
}

// nameOf names the check call calls, with has's precedence: "pkg.Func"
// through a package, "Func" for one called in its own package,
// "method.Name" for a method (its receiver's type is unknown to a parse
// without types); false for no known check. formOf says how it is known.
func (c checks) nameOf(call *ast.CallExpr) (string, bool) {
	if isRateLimit(call) {
		return selectorName(call.Fun), true
	}
	if _, marked := c.checkerOf(call); marked {
		return c.markedName(call), true
	}
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		if c.helpers[fun.Name] {
			return fun.Name, true
		}
	case *ast.SelectorExpr:
		if pkg, ok := fun.X.(*ast.Ident); ok && c.predicates[pkg.Name+"."+fun.Sel.Name] {
			return pkg.Name + "." + fun.Sel.Name, true
		}
	}
	if isAccessCheck(call) {
		return selectorName(call.Fun), true
	}
	return "", false
}

// unknownName names a call no known check makes (see
// scanner.unknownCalls): "Func", or "pkg.Func".
func unknownName(call *ast.CallExpr) string {
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		return fun.Name
	case *ast.SelectorExpr:
		if pkg, isIdent := fun.X.(*ast.Ident); isIdent {
			return pkg.Name + "." + fun.Sel.Name
		}
	}
	return ""
}

// markedName names a marked checker call by the key checkerOf found it
// under.
func (c checks) markedName(call *ast.CallExpr) string {
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		return fun.Name
	case *ast.SelectorExpr:
		if pkg, isPkg := fun.X.(*ast.Ident); isPkg {
			if _, ok := c.marked[pkg.Name+"."+fun.Sel.Name]; ok {
				return pkg.Name + "." + fun.Sel.Name
			}
		}
		return "method." + fun.Sel.Name
	}
	return ""
}

// selectorName names a pattern-known check: "acl.IsLoggedIn" through the
// acl or ratelimit package, "method.RequestIsTask" for a request method.
func selectorName(fun ast.Expr) string {
	sel, ok := fun.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	if pkg, isIdent := sel.X.(*ast.Ident); isIdent &&
		(pkg.Name == aclPkg || pkg.Name == ratelimitPkg) {
		return pkg.Name + "." + sel.Sel.Name
	}
	return "method." + sel.Sel.Name
}

// callOperands reads the operands of the check call named name.
func callOperands(fset *token.FileSet, call *ast.CallExpr, name string) checkCall {
	r := renderer{fset: fset}
	out := checkCall{Check: name, Means: meaningOf(r, call, name)}
	if isRateLimit(call) {
		out.Rate = rateOperands(r, call)
		return out
	}
	if slices.Contains(subjectChecks(), name) && len(call.Args) > subjectArg {
		out.Subject = r.source(call.Args[subjectArg])
	}
	if out.Means == "" {
		for _, arg := range call.Args {
			out.Args = append(out.Args, callArg{Text: r.source(arg)})
		}
	}
	for _, arg := range call.Args {
		if sel, ok := arg.(*ast.SelectorExpr); ok && pkgIs(sel.X, "capabilities") {
			out.Capability = sel.Sel.Name
		}
		if matcher, matcherArg, ok := matcherOf(r, arg); ok {
			out.Matcher, out.MatcherArg = matcher, matcherArg
			out.Scope = scopeOf(matcher)
		}
	}
	return out
}

// bindOperands sets where the call's subject and arguments come from.
func (cc *checkCall) bindOperands(bind func(string) string) {
	switch {
	case cc.Subject != "":
		cc.SubjectFrom = bind(cc.Subject)
	case cc.Scope == scopeUser && cc.MatcherArg != "":
		cc.SubjectFrom = bind(cc.MatcherArg)
	}
	for i := range cc.Args {
		cc.Args[i].From = bind(cc.Args[i].Text)
	}
}

// rateOperands reads a rate limiter call's key and the limit its options
// set, as written.
func rateOperands(r renderer, call *ast.CallExpr) *rateArgs {
	rate := &rateArgs{Key: rateKey(r, call)}
	if count := option(call, "MaxRequestsPerWindow"); count != nil {
		rate.Count = r.source(count)
	}
	if window := option(call, "LimitWindow"); window != nil {
		rate.Window = r.source(window)
	}
	return rate
}

// matcherOf reads an acl scope matcher argument: a matcher value
// (acl.GlobalScope) or a matcher built from an argument
// (acl.UserScope(kaid)), with that argument as written.
func matcherOf(r renderer, arg ast.Expr) (string, string, bool) {
	var built *ast.CallExpr
	if call, isCall := arg.(*ast.CallExpr); isCall {
		built, arg = call, call.Fun
	}
	sel, isSel := arg.(*ast.SelectorExpr)
	if !isSel || !pkgIs(sel.X, aclPkg) || !strings.HasSuffix(sel.Sel.Name, "Scope") {
		return "", "", false
	}
	matcherArg := ""
	if built != nil && len(built.Args) > 0 {
		matcherArg = r.source(built.Args[0])
	}
	return aclPkg + "." + sel.Sel.Name, matcherArg, true
}

// rateKey is a limiter's name, its second argument: the string's value,
// or the expression's source.
func rateKey(r renderer, call *ast.CallExpr) string {
	if len(call.Args) <= subjectArg {
		return ""
	}
	if lit, ok := call.Args[subjectArg].(*ast.BasicLit); ok && lit.Kind == token.STRING {
		if value, err := strconv.Unquote(lit.Value); err == nil {
			return value
		}
	}
	return r.source(call.Args[subjectArg])
}

// pkgIs reports whether e is the package identifier name.
func pkgIs(e ast.Expr, name string) bool {
	ident, ok := e.(*ast.Ident)
	return ok && ident.Name == name
}

// accessCheckFacts returns one access_check fact per check the rule facts
// name, saying how it is known (attrForm) and cited at its definition when
// the service (own) or the shared code it imports (shared, cited under
// pkgRoot) defines it; a check known only by its name (an acl predicate
// whose package isn't read) has no citation.
//
// Ensures: one fact per name, sorted by name.
func accessCheckFacts(facts []Fact, known checks, own, shared []source) []Fact {
	var names []string
	calls := unknownCallNames(facts)
	for _, f := range facts {
		for name := range strings.SplitSeq(f.Attrs[attrChecks], ",") {
			if name != "" && name != checkOpen && name != checkTodo &&
				!slices.Contains(names, name) {
				names = append(names, name)
			}
		}
	}
	slices.Sort(names)
	ownDefs, sharedDefs := checkDefinitions(own), checkDefinitions(shared)
	out := make([]Fact, 0, len(names))
	for _, name := range names {
		def, ok := ownDefs[name]
		src := def.ref
		if !ok {
			if shared, found := sharedDefs[name]; found {
				src = underPkg(shared.ref)
			}
		}
		form := known.formOf(name)
		if calls[name] {
			form = formCall
		}
		attrs := map[string]string{attrName: name, attrForm: form}
		if ok {
			// The service's own checkers: where they allow, or why that
			// can't be read (see scanner.allowingPaths).
			paths, reason := fileScanner(def.src, known).allowingPaths(def.fn)
			switch {
			case reason != "":
				attrs[attrNotAnalyzable] = "true"
				attrs[attrNotAnalyzableReason] = reason
			case len(paths) > 0:
				attrs[attrPaths] = pathsJSON(paths)
			}
		}
		out = append(out, Fact{
			Kind:      kindAccessCheck,
			ID:        "access_check.webapp." + name,
			Attrs:     attrs,
			Source:    src,
			Origin:    origin,
			Certainty: certain,
		})
	}
	return out
}

// unknownCallNames returns the checks facts' check calls name with form
// formCall: no known check (see scanner.unknownCalls).
func unknownCallNames(facts []Fact) map[string]bool {
	names := make(map[string]bool)
	for _, f := range facts {
		var calls []checkCall
		if json.Unmarshal([]byte(f.Attrs[attrCheckArgs]), &calls) != nil {
			continue
		}
		for _, call := range calls {
			if call.Form == formCall {
				names[call.Check] = true
			}
		}
	}
	return names
}

// formOf says how a named check is known, as nameOf decided. Helper
// predicates are found per scan, so c may lack them: a helper is the one
// check named bare (no package, no "method."), unless marked.
func (c checks) formOf(name string) string {
	key := name
	if method, isMethod := strings.CutPrefix(name, "method."); isMethod {
		key = "." + method // markedCheckers' key for a method
	}
	switch {
	case strings.HasPrefix(name, ratelimitPkg+"."):
		return formRateLimit
	case c.isMarkedName(key):
		return formMarked
	case c.predicates[name]:
		return formPredicate
	case !strings.Contains(name, "."):
		return formHelper
	}
	return formACL
}

// isMarkedName reports whether name is a marked checker's key.
func (c checks) isMarkedName(name string) bool {
	_, ok := c.marked[name]
	return ok
}

// checkDefinitions indexes the functions and methods of sources by the
// names nameOf gives their calls: "pkg.Func" and "Func" for a function,
// "method.Name" for a method; the first definition of a name wins.
func checkDefinitions(sources []source) map[string]checkDef {
	defs := map[string]checkDef{}
	add := func(name string, d checkDef) {
		if _, seen := defs[name]; !seen {
			defs[name] = d
		}
	}
	for i := range sources {
		s := &sources[i]
		pkg := s.file.Name.Name
		for _, fn := range funcDecls(s.file) {
			d := checkDef{
				ref: Ref{
					Path:  s.name,
					Lines: lines(s.fset.Position(fn.Pos()).Line, s.fset.Position(fn.End()).Line),
				},
				src: s,
				fn:  fn,
			}
			if fn.Recv != nil {
				add("method."+fn.Name.Name, d)
				continue
			}
			add(pkg+"."+fn.Name.Name, d)
			add(fn.Name.Name, d) // called unqualified in its own package
		}
	}
	return defs
}

// checkDef is a named check's definition: its citation, file and
// declaration.
type checkDef struct {
	ref Ref
	src *source
	fn  *ast.FuncDecl
}

// delegatesTo is the resolver method ("recv.Method") a delegation guard is
// checked as, for a guard of a method whose receiver type is recv; "" for
// any other guard.
func (g *guard) delegatesTo(recv string) string {
	if g.callee == "" {
		return ""
	}
	if g.calleeRecv != "" {
		recv = g.calleeRecv
	}
	return recv + "." + g.callee
}
