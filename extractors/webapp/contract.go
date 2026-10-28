package main

import (
	"fmt"
	"go/ast"
	"go/token"
	"path"
	"slices"
	"strings"
)

// A header contract is how services trust each other's requests: one side
// sends a secret in a header (the gateway's X-Ka-Gateway-Secret), the other
// accepts the header only when it matches the secret, and the gateway
// forwards some client headers (X-Ka-Service-Admin-Query) on. Rules carry
// attrHeader, attrContract and attrSecret — the secret's store name
// (secrets.GSM("backend_to_gateway_secret", 1)), the same in every service
// — so a view joins the two sides without reading prose.

// Attributes of contract rules, and the roles attrContract takes.
const (
	attrHeader   = "header" // a forwarding rule's: its custom headers, comma-separated
	attrContract = "contract"
	attrSecret   = "secret" // the secret's store name
	roleSends    = "sends"
	roleRequires = "requires"
	roleForwards = "forwards"
)

// What contract rules read in code.
const (
	customHeader = "X-"      // the headers a contract is about
	secretsPkg   = "secrets" // webapp's secret store (pkg/gcloud/secrets)
	headerField  = "Header"  // r.Header
)

// contractCode is what contract rules resolve names against: the secrets
// (variable → store name) and string constants of the service and the
// shared code it builds on.
type contractCode struct {
	secrets map[string]string // secret variable → store name; "" when ambiguous
	consts  map[string]string // package string constant → value
}

// contractFacts returns the header-contract rules of the service's own
// functions and of the shared ones it reaches (cited in pkg/).
func contractFacts(sources []source, shared sharedCode, reach map[declKey]bool, w *wording) []Fact {
	all := slices.Concat(sources, shared.sources)
	cc := contractCode{secrets: secretStores(all), consts: stringConsts(all)}
	var facts []Fact
	for _, set := range []struct {
		files  []source
		rooted bool
	}{{sources, false}, {shared.sources, true}} {
		for i := range set.files {
			s := &set.files[i]
			for _, fn := range funcDecls(s.file) {
				if set.rooted &&
					(fn.Recv != nil || !reach[declKey{path.Dir(s.name), fn.Name.Name}]) {
					continue
				}
				found := cc.functionFacts(s, fn, set.files, w)
				if set.rooted {
					found = rooted(found)
				}
				facts = append(facts, found...)
			}
		}
		facts = append(facts, forwardFacts(set.files, w)...)
	}
	return facts
}

// secretStores maps each secret variable (var X = secrets.GSM("name", 1))
// to its store name, by "X" and "pkg.X"; a name two stores share maps to "".
func secretStores(sources []source) map[string]string {
	stores := make(map[string]string)
	set := func(key, store string) {
		if old, seen := stores[key]; seen && old != store {
			store = ""
		}
		stores[key] = store
	}
	for i := range sources {
		pkg := sources[i].file.Name.Name
		for _, v := range packageVars(sources[i].file) {
			call, ok := v.value.(*ast.CallExpr)
			if !ok || !isSecretsCall(call) || len(call.Args) == 0 {
				continue
			}
			if store, isString := stringLit(call.Args[0]); isString {
				set(v.name, store)
				set(pkg+"."+v.name, store)
			}
		}
	}
	return stores
}

// isSecretsCall reports whether call is secrets.X(…).
func isSecretsCall(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, isIdent := sel.X.(*ast.Ident)
	return isIdent && pkg.Name == secretsPkg
}

// stringConsts maps the package string constants and variables of sources
// to their values (serviceAdminQueryHeaderName → "X-Ka-Service-Admin-Query").
func stringConsts(sources []source) map[string]string {
	consts := make(map[string]string)
	for i := range sources {
		for _, v := range packageVars(sources[i].file) {
			if value, ok := stringLit(v.value); ok {
				consts[v.name] = value
			}
		}
	}
	return consts
}

// secretOf returns the secret an expression names: a secret variable,
// "X" or "pkg.X", and its store name.
func (cc *contractCode) secretOf(e ast.Expr) (string, string, bool) {
	key := ""
	switch x := e.(type) {
	case *ast.Ident:
		key = x.Name
	case *ast.SelectorExpr:
		if q, ok := x.X.(*ast.Ident); ok {
			if store, found := cc.secrets[q.Name+"."+x.Sel.Name]; found && store != "" {
				return q.Name + "." + x.Sel.Name, store, true
			}
			key = x.Sel.Name
		}
	}
	store, found := cc.secrets[key]
	return exprName(e), store, found && store != ""
}

// exprName is an identifier or selector as written.
func exprName(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.SelectorExpr:
		if q, ok := x.X.(*ast.Ident); ok {
			return q.Name + "." + x.Sel.Name
		}
	}
	return ""
}

// secretRead returns the secret a call reads: one of its arguments names
// a secret (ctx.Secrets().Get(ctx, S), secretsClient.Get(ctx, S),
// secrets.Validate(ctx, S, v)).
func (cc *contractCode) secretRead(call *ast.CallExpr) (string, string, bool) {
	for _, a := range call.Args {
		if name, store, ok := cc.secretOf(a); ok {
			return name, store, true
		}
	}
	return "", "", false
}

// header is a header name as a call names it: its value, and how the code
// writes it (the literal, or the constant holding it).
type header struct {
	name    string // "X-Ka-Gateway-Secret"
	written string // `"X-Ka-…"` as quoted text, or the constant's name
	literal bool
}

// headerOf resolves a header-name argument: a string literal, or a
// package string constant; only custom headers (X-…) count.
func (cc *contractCode) headerOf(e ast.Expr) (header, bool) {
	if value, ok := stringLit(e); ok && strings.HasPrefix(value, customHeader) {
		return header{name: value, written: value, literal: true}, true
	}
	if ident, ok := e.(*ast.Ident); ok {
		if value, isConst := cc.consts[ident.Name]; isConst &&
			strings.HasPrefix(value, customHeader) {
			return header{name: value, written: ident.Name}, true
		}
	}
	return header{}, false
}

// isHeaderGet returns the header a call reads from a request: r.Header.Get(H),
// header.Get(H) on an http.Header named so.
func (cc *contractCode) isHeaderGet(e ast.Expr) (header, bool) {
	call, ok := e.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return header{}, false
	}
	sel, isSel := call.Fun.(*ast.SelectorExpr)
	if !isSel || sel.Sel.Name != getMethod || !isHeaderValue(sel.X) {
		return header{}, false
	}
	return cc.headerOf(call.Args[0])
}

// isHeaderValue reports whether e is an http.Header as webapp names one:
// r.Header, or a local or parameter named …header / …Header(s).
func isHeaderValue(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.SelectorExpr:
		return x.Sel.Name == headerField
	case *ast.Ident:
		lower := strings.ToLower(x.Name)
		return strings.HasSuffix(lower, "header") || strings.HasSuffix(lower, "headers")
	}
	return false
}

// bound is what a local of a function was first assigned: a secret read,
// or a request header.
type bound struct {
	secret, store string // secret read
	header        header // header read
	line          int
}

// bindings returns the locals of body first assigned a secret read or a
// request header (gatewaySecret, err := ctx.Secrets().Get(ctx, S);
// actual := r.Header.Get(H)).
func (cc *contractCode) bindings(fset *token.FileSet, body *ast.BlockStmt) map[string]bound {
	locals := make(map[string]bound)
	ast.Inspect(body, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok || assign.Tok != token.DEFINE || len(assign.Rhs) != 1 {
			return true
		}
		ident, isIdent := assign.Lhs[0].(*ast.Ident)
		if !isIdent || ident.Name == "_" {
			return true
		}
		if _, done := locals[ident.Name]; done {
			return true
		}
		line := fset.Position(assign.Pos()).Line
		if h, isHeader := cc.isHeaderGet(assign.Rhs[0]); isHeader {
			locals[ident.Name] = bound{header: h, line: line}
		} else if call, isCall := assign.Rhs[0].(*ast.CallExpr); isCall {
			if name, store, reads := cc.secretRead(call); reads {
				locals[ident.Name] = bound{secret: name, store: store, line: line}
			}
		}
		return true
	})
	return locals
}

// functionFacts returns the contract rules of one function: the secret
// headers it sends, and the headers it accepts only against a secret.
func (cc *contractCode) functionFacts(
	s *source,
	fn *ast.FuncDecl,
	pkgFiles []source,
	w *wording,
) []Fact {
	locals := cc.bindings(s.fset, fn.Body)
	var facts []Fact
	seen := make(map[string]bool) // role|header
	add := func(f Fact) {
		if key := f.Attrs[attrContract] + "|" + f.Attrs[attrHeader]; !seen[key] {
			seen[key] = true
			facts = append(facts, f)
		}
	}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.CallExpr:
			if f, sends := cc.sendFact(s, fn, x, locals, pkgFiles, w); sends {
				add(f)
			}
			if f, requires := cc.validateFact(s, fn, x, locals, w); requires {
				add(f)
			}
		case *ast.BinaryExpr:
			if f, requires := cc.compareFact(s, fn, x, locals, w); requires {
				add(f)
			}
		}
		return true
	})
	return facts
}

// sendFact makes the rule of X.Header.Set(H, v) / headers.Add(H, v) when v
// holds a secret: read in the function, or passed as a parameter by a call
// of the function in its package (gqlclient's NewClient → newClient).
func (cc *contractCode) sendFact(
	s *source, fn *ast.FuncDecl, call *ast.CallExpr,
	locals map[string]bound, pkgFiles []source, w *wording,
) (Fact, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || len(call.Args) != 2 || sel.Sel.Name != "Set" && sel.Sel.Name != "Add" {
		return Fact{}, false
	}
	h, isHeader := cc.headerOf(call.Args[0])
	value, isIdent := call.Args[1].(*ast.Ident)
	if !isHeader || !isIdent {
		return Fact{}, false
	}
	end := s.fset.Position(call.End()).Line
	if b, isSecret := locals[value.Name]; isSecret && b.store != "" {
		text := fmt.Sprintf(w.sendsFmt, "`"+b.secret+"`", b.store, writtenHeader(h))
		return contractFact(s, b.line, end, roleSends, h, b.store, text, w), true
	}
	secret, store, passed := cc.passedSecret(fn, value.Name, pkgFiles)
	if !passed {
		return Fact{}, false
	}
	// The secret is read by the caller: named, not quoted (not cited here).
	text := fmt.Sprintf(w.sendsFmt, secret, store, writtenHeader(h))
	return contractFact(
		s,
		s.fset.Position(call.Pos()).Line,
		end,
		roleSends,
		h,
		store,
		text,
		w,
	), true
}

// passedSecret returns the secret the callers of fn, in its package, pass
// as its parameter param, when they agree on one.
func (cc *contractCode) passedSecret(
	fn *ast.FuncDecl,
	param string,
	pkgFiles []source,
) (string, string, bool) {
	at := paramIndex(fn, param)
	if at < 0 {
		return "", "", false
	}
	secret, store := "", ""
	for i := range pkgFiles {
		for _, caller := range funcDecls(pkgFiles[i].file) {
			locals := cc.bindings(pkgFiles[i].fset, caller.Body)
			ast.Inspect(caller.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok || calledName(call) != fn.Name.Name || len(call.Args) <= at {
					return true
				}
				if arg, isIdent := call.Args[at].(*ast.Ident); isIdent &&
					locals[arg.Name].store != "" {
					secret, store = locals[arg.Name].secret, locals[arg.Name].store
				}
				return true
			})
		}
	}
	return secret, store, store != ""
}

// paramIndex is the position of a named parameter of fn; -1 if none.
func paramIndex(fn *ast.FuncDecl, name string) int {
	i := 0
	for _, field := range fn.Type.Params.List {
		for _, n := range field.Names {
			if n.Name == name {
				return i
			}
			i++
		}
		if len(field.Names) == 0 {
			i++
		}
	}
	return -1
}

// validateFact makes the rule of secrets.Validate(ctx, S, v) when v is a
// request header, read in place or held by a local; the function is cited
// whole.
func (cc *contractCode) validateFact(
	s *source, fn *ast.FuncDecl, call *ast.CallExpr, locals map[string]bound, w *wording,
) (Fact, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Validate" || len(call.Args) != 3 {
		return Fact{}, false
	}
	if pkg, isIdent := sel.X.(*ast.Ident); !isIdent || pkg.Name != secretsPkg {
		return Fact{}, false
	}
	secret, store, isSecret := cc.secretOf(call.Args[1])
	h, isHeader := cc.isHeaderGet(call.Args[2])
	if v, isIdent := call.Args[2].(*ast.Ident); isIdent && locals[v.Name].header.name != "" {
		h, isHeader = locals[v.Name].header, true
	}
	if !isSecret || !isHeader {
		return Fact{}, false
	}
	return requiresFact(s, fn, h, secret, store, w), true
}

// compareFact makes the rule of a header local compared with a secret local
// (actual != expected1, serve's GraphQL middleware); the function is cited
// whole.
func (cc *contractCode) compareFact(
	s *source, fn *ast.FuncDecl, cmp *ast.BinaryExpr, locals map[string]bound, w *wording,
) (Fact, bool) {
	if cmp.Op != token.EQL && cmp.Op != token.NEQ {
		return Fact{}, false
	}
	for _, pair := range [][2]ast.Expr{{cmp.X, cmp.Y}, {cmp.Y, cmp.X}} {
		h, isHeader := pair[0].(*ast.Ident)
		v, isSecret := pair[1].(*ast.Ident)
		if !isHeader || !isSecret || locals[h.Name].header.name == "" ||
			locals[v.Name].store == "" {
			continue
		}
		b := locals[v.Name]
		return requiresFact(s, fn, locals[h.Name].header, b.secret, b.store, w), true
	}
	return Fact{}, false
}

// requiresFact states that fn accepts header h only against a secret.
func requiresFact(s *source, fn *ast.FuncDecl, h header, secret, store string, w *wording) Fact {
	start, end := s.fset.Position(fn.Pos()).Line, s.fset.Position(fn.End()).Line
	text := fmt.Sprintf(w.requiresFmt, writtenHeader(h), "`"+secret+"`", store)
	return contractFact(s, start, end, roleRequires, h, store, text, w)
}

// writtenHeader quotes a header as the cited code writes it: the literal,
// or the constant with its value.
func writtenHeader(h header) string {
	if h.literal {
		return "`" + h.name + "`"
	}
	return "`" + h.written + "` (" + h.name + ")"
}

// contractFact makes a contract rule cited on lines start-end of s.
func contractFact(
	s *source,
	start, end int,
	role string,
	h header,
	store, text string,
	w *wording,
) Fact {
	f := tableRule(
		s,
		start,
		end,
		"contract-"+role+"-"+h.name,
		fmt.Sprintf(w.contractTitleFmt, h.name),
		text,
	)
	f.ID = fmt.Sprintf("rule.webapp.contract.%s:%d-%d:%s:%s", s.name, start, end, role, h.name)
	f.Attrs[attrHeader] = h.name
	f.Attrs[attrContract] = role
	if store != "" {
		f.Attrs[attrSecret] = store
	}
	return f
}

// forwardFacts returns the allow-lists of headers files forward: a package
// variable a function ranges over to copy headers onto a request
// (for name := range _reqHeaderForwardingMap { … req.Header.Add(name, v) }),
// listing the header names it writes out.
func forwardFacts(files []source, w *wording) []Fact {
	ranged := headerCopyLoops(files)
	var facts []Fact
	for i := range files {
		for _, v := range packageVars(files[i].file) {
			if ranged[v.name] {
				if f, ok := forwardFact(&files[i], v, w); ok {
					facts = append(facts, f)
				}
			}
		}
	}
	return facts
}

// headerCopyLoops returns the variables files range over to copy headers.
func headerCopyLoops(files []source) map[string]bool {
	ranged := make(map[string]bool)
	for i := range files {
		ast.Inspect(files[i].file, func(n ast.Node) bool {
			loop, ok := n.(*ast.RangeStmt)
			if ok && copiesHeaders(loop.Body) {
				if v, isIdent := loop.X.(*ast.Ident); isIdent {
					ranged[v.Name] = true
				}
			}
			return true
		})
	}
	return ranged
}

// forwardFact makes the one rule of a forwarded-header list; attrHeader
// lists its custom headers. A list naming none is no contract.
func forwardFact(s *source, v packageVar, w *wording) (Fact, bool) {
	names, others := headerNames(v.value)
	var custom []string
	for _, name := range names {
		if strings.HasPrefix(name, customHeader) {
			custom = append(custom, name)
		}
	}
	if len(custom) == 0 {
		return Fact{}, false
	}
	list := quoteAll(names)
	if len(others) > 0 {
		list = fmt.Sprintf(w.withListsFmt, list, quoteAll(others))
	}
	start, end := s.fset.Position(v.value.Pos()).Line, s.fset.Position(v.value.End()).Line
	h := header{name: strings.Join(custom, ","), literal: true}
	f := contractFact(
		s,
		start,
		end,
		roleForwards,
		h,
		"",
		fmt.Sprintf(w.forwardsFmt, list, v.name),
		w,
	)
	f.Attrs[attrTitle] = fmt.Sprintf(w.contractTitleFmt, v.name)
	f.Attrs[attrSlug] = slugify("contract-forwards-" + v.name)
	return f, true
}

// copiesHeaders reports whether a loop body adds or sets a header.
func copiesHeaders(body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if sel, isSel := call.Fun.(*ast.SelectorExpr); isSel &&
				(sel.Sel.Name == "Add" || sel.Sel.Name == "Set") && isHeaderValue(sel.X) {
				found = true
			}
		}
		return !found
	})
	return found
}

// headerNames returns the string literals of a header list's value, and
// the other lists it includes (gqlclient.ForwardedUserRequestHeaderNames).
func headerNames(e ast.Expr) ([]string, []string) {
	var names, others []string
	ast.Inspect(e, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.BasicLit:
			if v, ok := stringLit(x); ok {
				names = append(names, v)
			}
		case *ast.SelectorExpr:
			others = append(others, exprName(x))
			return false
		}
		return true
	})
	return names, others
}

// addContractTexts sets the phrases of header-contract rules.
func addContractTexts(w *wording, tr translate) {
	w.contractTitleFmt = tr("Contrat d'en-tête : %s", "Header contract: %s")
	w.sendsFmt = tr(
		"Le système doit envoyer le secret %s (%s) dans l'en-tête %s des requêtes qu'il émet.",
		"The system shall send the secret %s (%s) in the %s header of the requests it makes.",
	)
	w.requiresFmt = tr(
		"Le système doit accepter l'en-tête %s seulement s'il correspond au secret %s (%s).",
		"The system shall accept the %s header only when it matches the secret %s (%s).",
	)
	w.forwardsFmt = tr(
		"Le système doit transmettre aux requêtes qu'il émet les en-têtes %s (%s).",
		"The system shall forward the request headers %s to the requests it makes (%s).",
	)
	w.withListsFmt = tr("%s et ceux de %s", "%s and those of %s")
}
