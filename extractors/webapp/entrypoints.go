package main

import (
	"fmt"
	"go/ast"
	"go/token"
	"path"
	"slices"
	"strconv"
	"strings"
)

// servePkg is webapp's HTTP server package (pkg/web/serve): its HandleFunc
// and Handle register a handler on a path.
const servePkg = "serve"

// entryPoint is a net/http handler the service registers with
// serve.HandleFunc(path, h) or serve.Handle(path, h): the graphql-gateway's
// routes, the services' pubsub and task handlers.
type entryPoint struct {
	paths    []string // the paths it serves, sorted
	file     *source
	fn       *ast.FuncDecl
	wrappers []wrapperUse // the wrapper calls it is registered through
}

// wrapperUse is a wrapper call in a registration:
// serve.CreateTaskHandler(handlers.OrchestrateUserDeletion).
type wrapperUse struct {
	file *source // the registration's file
	call *ast.CallExpr
}

// name is what an entry point's rules are about: its paths.
func (e *entryPoint) name() string {
	return strings.Join(e.paths, ", ")
}

// entryPoints finds the handlers the service registers, in source order of
// their declarations. A handler is resolved through what webapp writes:
// a method value (rts.HandleGraphQL), a function (handleX, pkg.HandleX), a
// closure that only calls one (func(w, r) { handleX(w, r) }), or a wrapper
// call taking one (serve.CreateTaskHandler(handlers.OrchestrateUserDeletion)).
// A handler that resolves to no declaration, or to several, is skipped.
func entryPoints(sources []source) []entryPoint {
	decls := indexFuncs(sources)
	byFn := make(map[*ast.FuncDecl]*entryPoint)
	var order []*ast.FuncDecl
	for i := range sources {
		for _, r := range routesIn(&sources[i], decls) {
			e, seen := byFn[r.handler.fn]
			if !seen {
				e = &entryPoint{file: r.handler.file, fn: r.handler.fn}
				byFn[r.handler.fn] = e
				order = append(order, r.handler.fn)
			}
			if !slices.Contains(e.paths, r.path) {
				e.paths = append(e.paths, r.path)
			}
			for _, w := range r.wrappers {
				e.wrappers = append(e.wrappers, wrapperUse{file: &sources[i], call: w})
			}
		}
	}
	found := make([]entryPoint, 0, len(order))
	for _, fn := range order {
		e := byFn[fn]
		slices.Sort(e.paths)
		found = append(found, *e)
	}
	slices.SortFunc(found, func(a, b entryPoint) int {
		if c := strings.Compare(a.file.name, b.file.name); c != 0 {
			return c
		}
		return int(a.fn.Pos() - b.fn.Pos())
	})
	return found
}

// route is one path registered on a resolved handler, through wrappers.
type route struct {
	path     string
	handler  funcDecl
	wrappers []*ast.CallExpr
}

// routesIn returns the registrations of file s whose handler resolves.
func routesIn(s *source, decls funcIndex) []route {
	var found []route
	ast.Inspect(s.file, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if p, h, isRoute := handlerRoute(call); isRoute {
				if d, resolved := decls.handler(h, s.file.Name.Name); resolved {
					found = append(found, route{path: p, handler: d, wrappers: wrappersOf(h)})
				}
			}
		}
		return true
	})
	return found
}

// handlerRoute returns the path and handler of serve.HandleFunc(path, h)
// or serve.Handle(path, h).
func handlerRoute(call *ast.CallExpr) (string, ast.Expr, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || len(call.Args) != 2 || sel.Sel.Name != "HandleFunc" && sel.Sel.Name != "Handle" {
		return "", nil, false
	}
	if pkg, isIdent := sel.X.(*ast.Ident); !isIdent || pkg.Name != servePkg {
		return "", nil, false
	}
	lit, isLit := call.Args[0].(*ast.BasicLit)
	if !isLit || lit.Kind != token.STRING {
		return "", nil, false
	}
	route, err := strconv.Unquote(lit.Value)
	if err != nil || !strings.HasPrefix(route, "/") {
		return "", nil, false
	}
	return route, call.Args[1], true
}

// funcDecl is a declaration and the file holding it.
type funcDecl struct {
	file *source
	fn   *ast.FuncDecl
}

// funcIndex finds declarations by name: package functions by
// "pkg.Func", methods by ".Method" (whatever the receiver).
type funcIndex map[string][]funcDecl

func indexFuncs(sources []source) funcIndex {
	idx := make(funcIndex)
	for i := range sources {
		s := &sources[i]
		for _, decl := range s.file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			key := s.file.Name.Name + "." + fn.Name.Name
			if fn.Recv != nil {
				key = "." + fn.Name.Name
			}
			idx[key] = append(idx[key], funcDecl{file: s, fn: fn})
		}
	}
	return idx
}

// handler resolves a handler expression written in package pkg to the one
// declaration it names (see entryPoints).
func (idx funcIndex) handler(e ast.Expr, pkg string) (funcDecl, bool) {
	switch x := e.(type) {
	case *ast.Ident:
		return idx.one(pkg + "." + x.Name)
	case *ast.SelectorExpr:
		if q, isIdent := x.X.(*ast.Ident); isIdent {
			if d, ok := idx.one(q.Name + "." + x.Sel.Name); ok {
				return d, true
			}
		}
		return idx.one("." + x.Sel.Name)
	case *ast.FuncLit:
		if len(x.Body.List) == 1 {
			if stmt, ok := x.Body.List[0].(*ast.ExprStmt); ok {
				if call, isCall := stmt.X.(*ast.CallExpr); isCall {
					return idx.handler(call.Fun, pkg)
				}
			}
		}
	case *ast.CallExpr:
		if inner, _, ok := unwrapped(x); ok {
			return idx.handler(inner, pkg)
		}
	}
	return funcDecl{}, false
}

// unwrapped returns the handler a wrapper call takes, and the call: a call
// of one argument, serve.CreateTaskHandler(f) → f.
func unwrapped(e ast.Expr) (ast.Expr, *ast.CallExpr, bool) {
	call, ok := e.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return nil, nil, false
	}
	return call.Args[0], call, true
}

// wrappersOf returns the wrapper calls around a handler, outermost first.
func wrappersOf(e ast.Expr) []*ast.CallExpr {
	var calls []*ast.CallExpr
	for inner, call, ok := unwrapped(e); ok; inner, call, ok = unwrapped(inner) {
		calls = append(calls, call)
	}
	return calls
}

// checkingWrappers returns the functions of sources that wrap a handler in
// a check: they return a closure (or http.HandlerFunc(closure)) with a
// guard — serve.CreateTaskHandler rejects requests that are not tasks.
func checkingWrappers(sources []source, known checks) []qualifiedName {
	var found []qualifiedName
	for i := range sources {
		s := &sources[i]
		sc := scanner{fset: s.fset, known: known}
		for _, fn := range funcDecls(s.file) {
			if lit, ok := returnedClosure(fn.Body); ok && len(sc.guards(lit.Body)) > 0 {
				found = append(found, qualifiedName{pkg: s.file.Name.Name, name: fn.Name.Name})
			}
		}
	}
	return found
}

// wrapperName is how a wrapper call names its function: "serve.X", or
// "X" for a function of the registration's package.
func wrapperName(call *ast.CallExpr) string {
	switch f := call.Fun.(type) {
	case *ast.Ident:
		return f.Name
	case *ast.SelectorExpr:
		if q, ok := f.X.(*ast.Ident); ok {
			return q.Name + "." + f.Sel.Name
		}
	}
	return ""
}

// one returns the declaration key names when exactly one does.
func (idx funcIndex) one(key string) (funcDecl, bool) {
	if decls := idx[key]; len(decls) == 1 {
		return decls[0], true
	}
	return funcDecl{}, false
}

// entryScan derives the rules of the service's net/http handlers outside
// resolvers/: the same guards as a resolver's, about the paths the handler
// serves, in its directory's domain. Their endpoints come from the Go
// extractor; codetospec links them to these rules by their citations.
func entryScan(sources, shared []source, known checks, w *wording) []Fact {
	var entries []entryPoint
	for _, e := range entryPoints(sources) {
		if path.Dir(e.file.name) != resolversDir {
			entries = append(entries, e)
		}
	}
	var trees []*ast.File
	for _, e := range entries {
		if !slices.Contains(trees, e.file.file) {
			trees = append(trees, e.file.file)
		}
	}
	known.helpers = helperPredicates(trees, known)
	methods := make([]resolverMethod, len(entries))
	for i, e := range entries {
		sc := scanner{fset: e.file.fset, known: known}
		methods[i] = resolverMethod{
			recv:   receiverName(e.fn),
			method: e.fn.Name.Name,
			start:  e.file.fset.Position(e.fn.Pos()).Line,
			end:    e.file.fset.Position(e.fn.End()).Line,
			guards: sc.rules(e.fn),
		}
	}
	methods = keepBackedDelegations([][]resolverMethod{methods})[0]
	wrapping := make(map[string]bool) // as wrapperName names them
	for _, f := range checkingWrappers(shared, known) {
		wrapping[f.pkg+"."+f.name] = true
	}
	for _, f := range checkingWrappers(sources, known) {
		wrapping[f.name] = true // called unqualified in its package
		wrapping[f.pkg+"."+f.name] = true
	}
	var facts []Fact
	for i := range entries {
		e := &entries[i]
		op := operation{name: e.name(), domain: dirDomain(e.file.name)}
		facts = append(facts, ruleFacts(e.file.fset, w, e.file.name, &methods[i], &op)...)
		facts = append(facts, wrapperFacts(e, wrapping, &op, w)...)
	}
	return facts
}

// wrapperFacts states, for each checking wrapper an entry point is
// registered through, that it checks access to it ("checked by
// serve.CreateTaskHandler"), cited on the wrapper call in the
// registration: the check itself lives in webapp's shared code.
func wrapperFacts(e *entryPoint, wrapping map[string]bool, op *operation, w *wording) []Fact {
	var facts []Fact
	seen := make(map[string]bool)
	for _, use := range e.wrappers {
		name := wrapperName(use.call)
		if !wrapping[name] || seen[name] {
			continue
		}
		seen[name] = true
		fset := use.file.fset
		m := resolverMethod{guards: []guard{{
			kind:    ruleChecked,
			subject: use.call.Fun,
			start:   fset.Position(use.call.Pos()).Line,
			end:     fset.Position(use.call.End()).Line,
		}}}
		facts = append(facts, ruleFacts(fset, w, use.file.name, &m, op)...)
	}
	return facts
}

// dirDomain is the domain codetospec gives a file by default: its
// directory's, or its own name's at the service root.
func dirDomain(file string) string {
	if dir := path.Dir(file); dir != "." {
		return domainOf(dir)
	}
	return domainOf(file)
}

// servedRoute is a registration the service makes only under conditions:
// if os.Getenv("KA_IS_DEV_SERVER") == "1" { …serve.HandleFunc("/", …) }.
type servedRoute struct {
	path       string
	conds      []ast.Expr // the enclosing branches' conditions, else negated
	start, end int        // from the outermost if to the registration
}

// conditionalRoutes finds the registrations of s made under conditions,
// whatever their handler (playground.Handler is not the service's).
func conditionalRoutes(s *source) []servedRoute {
	var found []servedRoute
	for _, fn := range funcDecls(s.file) {
		found = append(found, routesUnder(s, fn.Body.List, nil, 0)...)
	}
	return found
}

// routesUnder walks stmts under conds, the outermost if starting at line
// start (0 at the top), into if/else branches.
func routesUnder(s *source, stmts []ast.Stmt, conds []ast.Expr, start int) []servedRoute {
	var found []servedRoute
	for _, stmt := range stmts {
		ifs, isIf := stmt.(*ast.IfStmt)
		if !isIf {
			if len(conds) > 0 {
				found = append(found, registrationsIn(s, stmt, conds, start)...)
			}
			continue
		}
		at := start
		if at == 0 {
			at = s.fset.Position(ifs.Pos()).Line
		}
		then := append(slices.Clone(conds), ifs.Cond)
		found = append(found, routesUnder(s, ifs.Body.List, then, at)...)
		otherwise := append(
			slices.Clone(conds),
			&ast.UnaryExpr{Op: token.NOT, X: &ast.ParenExpr{X: ifs.Cond}},
		)
		switch e := ifs.Else.(type) {
		case *ast.BlockStmt:
			found = append(found, routesUnder(s, e.List, otherwise, at)...)
		case *ast.IfStmt:
			found = append(found, routesUnder(s, []ast.Stmt{e}, otherwise, at)...)
		}
	}
	return found
}

// registrationsIn returns the registrations a statement makes under conds.
func registrationsIn(s *source, stmt ast.Stmt, conds []ast.Expr, start int) []servedRoute {
	var found []servedRoute
	ast.Inspect(stmt, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if p, _, isRoute := handlerRoute(call); isRoute {
				found = append(found, servedRoute{
					path:  p,
					conds: conds,
					start: start,
					end:   s.fset.Position(call.End()).Line,
				})
			}
		}
		return true
	})
	return found
}

// servedFacts states the conditions of the service's conditional
// registrations: "The system shall serve / only when …", cited from the
// outermost if to the registration.
func servedFacts(sources []source, w *wording) []Fact {
	var facts []Fact
	for i := range sources {
		s := &sources[i]
		r := renderer{w: w, fset: s.fset, marks: newCallMarks()}
		for _, route := range conditionalRoutes(s) {
			text := fmt.Sprintf(w.servedFmt, route.path, r.cond(allOf(route.conds), false))
			title := fmt.Sprintf(w.servedTitleFmt, route.path)
			f := tableRule(s, route.start, route.end, "served-"+route.path, title, text)
			f.ID = fmt.Sprintf("rule.webapp.served.%s:%d-%d", s.name, route.start, route.end)
			facts = append(facts, f)
		}
	}
	return facts
}

// addServedTexts sets the phrases of conditional-registration rules.
func addServedTexts(w *wording, tr translate) {
	w.servedTitleFmt = tr("Enregistrement : %s", "Registration: %s")
	w.servedFmt = tr(
		"Le système doit servir %s seulement si %s.",
		"The system shall serve %s only when %s.",
	)
}
