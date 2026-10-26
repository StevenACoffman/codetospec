package main

import (
	"go/ast"
	"go/printer"
	"go/token"
	"go/types"
	"net/http"
	"regexp"
	"strings"

	"golang.org/x/tools/go/packages"
)

// Route is one detected HTTP route.
type Route struct {
	Method  string
	Path    string
	Handler string
	// HandlerRef is the module code serving the route; the registration
	// itself when the handler is not the module's (playground.Handler(…)):
	// it is the closest code that serves the route.
	HandlerRef Ref
	Ref        Ref
}

// httpVerb is the HTTP verb a router method name declares directly, ""
// for any other name: gin/echo use upper case ("GET", and "ANY" for every
// verb), chi title case ("Get").
func httpVerb(name string) string {
	upper := strings.ToUpper(name)
	switch {
	case name == "ANY":
		return name
	case !isHTTPMethod(upper):
		return ""
	case name == upper, name == upper[:1]+strings.ToLower(upper[1:]):
		return upper
	default:
		return ""
	}
}

var methodPrefixRe = regexp.MustCompile(`^(GET|POST|PUT|PATCH|DELETE|OPTIONS|HEAD)\s+(/\S*)$`)

// ExtractRoutes walks loaded packages and returns detected routes with their
// group prefixes resolved and handlers named via type information.
func ExtractRoutes(pkgs []*packages.Package, fset *token.FileSet, root string) []Route {
	var routes []Route
	decls := funcDecls(pkgs)
	for _, pkg := range pkgs {
		handlers := handlerCode{info: pkg.TypesInfo, fset: fset, root: root, decls: decls}
		for _, file := range pkg.Syntax {
			// Resolve router-group prefixes within each function scope.
			ast.Inspect(file, func(n ast.Node) bool {
				var body *ast.BlockStmt
				switch fn := n.(type) {
				case *ast.FuncDecl:
					body = fn.Body
				case *ast.FuncLit:
					body = fn.Body
				}
				if body == nil {
					return true
				}
				prefixes := groupPrefixes(body)
				for _, stmt := range allCalls(body) {
					if r, ok := routeFrom(stmt, prefixes, &handlers); ok {
						routes = append(routes, r)
					}
				}
				return true
			})
		}
	}
	return dedupRoutes(routes)
}

// groupPrefixes maps a router-group variable name to its cumulative path
// prefix, resolving chained groups (api := r.Group("/api"); v1 := api.Group("/v1")).
func groupPrefixes(body *ast.BlockStmt) map[string]string {
	direct := map[string]struct {
		base   string
		prefix string
	}{}
	ast.Inspect(body, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
			return true
		}
		lhs, ok := assign.Lhs[0].(*ast.Ident)
		if !ok {
			return true
		}
		call, ok := assign.Rhs[0].(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Group" || len(call.Args) == 0 {
			return true
		}
		prefix, ok := stringLit(call.Args[0])
		if !ok {
			return true
		}
		base := ""
		if recv, isIdent := sel.X.(*ast.Ident); isIdent {
			base = recv.Name
		}
		direct[lhs.Name] = struct {
			base   string
			prefix string
		}{base, prefix}
		return true
	})

	resolved := make(map[string]string, len(direct))
	var resolve func(name string, seen map[string]bool) string
	resolve = func(name string, seen map[string]bool) string {
		if p, done := resolved[name]; done {
			return p
		}
		d, ok := direct[name]
		if !ok || seen[name] {
			return ""
		}
		seen[name] = true
		full := strings.TrimRight(resolve(d.base, seen), "/") + d.prefix
		resolved[name] = full
		return full
	}
	for name := range direct {
		resolve(name, map[string]bool{})
	}
	return resolved
}

// allCalls returns every call expression inside a function body.
func allCalls(body *ast.BlockStmt) []*ast.CallExpr {
	var calls []*ast.CallExpr
	ast.Inspect(body, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			calls = append(calls, call)
		}
		return true
	})
	return calls
}

// routeFrom interprets one call as a route registration, if it is one.
func routeFrom(
	call *ast.CallExpr,
	prefixes map[string]string,
	handlers *handlerCode,
) (Route, bool) {
	info, fset, root := handlers.info, handlers.fset, handlers.root
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || len(call.Args) == 0 {
		return Route{}, false
	}
	name := sel.Sel.Name
	recvPrefix := ""
	if recv, isIdent := sel.X.(*ast.Ident); isIdent {
		recvPrefix = prefixes[recv.Name]
	}

	method, path, isRoute := methodAndPath(name, call.Args)
	if !isRoute {
		return Route{}, false
	}

	full := strings.TrimRight(recvPrefix, "/") + path
	if !strings.HasPrefix(full, "/") {
		full = "/" + full
	}
	handler := call.Args[len(call.Args)-1]
	ref := refOf(call, fset, root)
	return Route{
		Method:     method,
		Path:       full,
		Handler:    handlerName(handler, info),
		HandlerRef: servedBy(handlers, handler, ref),
		Ref:        ref,
	}, true
}

// methodAndPathArgs is how many arguments Handle(method, path, …) takes
// before its handler.
const methodAndPathArgs = 2

// methodAndPath reads the method and path a registration call named name
// declares with args: r.GET("/x", h), Handle(method, path, h) (gin, chi)
// or Handle(path, h) (net/http), HandleFunc("GET /x", h) (Go 1.22).
func methodAndPath(name string, args []ast.Expr) (string, string, bool) {
	switch {
	case httpVerb(name) != "":
		p, ok := stringLit(args[0])
		return httpVerb(name), p, ok && strings.HasPrefix(p, "/")
	case name == "Handle" || name == "Method":
		if len(args) < methodAndPathArgs {
			return "", "", false
		}
		m, isMethod := stringLit(args[0])
		p, isPath := stringLit(args[1])
		if isMethod && isPath && isHTTPMethod(m) && strings.HasPrefix(p, "/") {
			return strings.ToUpper(m), p, true
		}
		p, isPath = stringLit(args[0])
		return "", p, isPath && strings.HasPrefix(p, "/") // net/http mux.Handle(path, handler)
	case name == "HandleFunc":
		p, ok := stringLit(args[0])
		if !ok {
			return "", "", false
		}
		if m := methodPrefixRe.FindStringSubmatch(p); m != nil { // Go 1.22 "GET /path"
			return m[1], m[2], true
		}
		return "", p, strings.HasPrefix(p, "/")
	}
	return "", "", false
}

// servedBy is the code serving a route: its handler's, or the
// registration (at) for a handler outside the module.
func servedBy(handlers *handlerCode, handler ast.Expr, at Ref) Ref {
	if ref := handlers.of(handler); ref.Path != "" {
		return ref
	}
	return at
}

// handlerName resolves a handler argument to a qualified symbol name so the
// route can later be linked to the file that defines it.
func handlerName(expr ast.Expr, info *types.Info) string {
	switch e := expr.(type) {
	case *ast.FuncLit:
		return "closure"
	case *ast.Ident:
		if info != nil {
			if obj := info.Uses[e]; obj != nil {
				return qualify(obj)
			}
		}
		return e.Name
	case *ast.SelectorExpr:
		return selectorName(e, info)
	case *ast.CallExpr:
		return handlerName(e.Fun, info) // constructor / wrapper
	default:
		return renderExpr(expr)
	}
}

// selectorName names a selector handler: a method value by its receiver
// type (Adapter.WebHookApple), a package function by its package.
func selectorName(e *ast.SelectorExpr, info *types.Info) string {
	if info != nil {
		if sel, found := info.Selections[e]; found {
			if recv := sel.Recv(); recv != nil {
				return typeName(recv) + "." + e.Sel.Name
			}
		}
		if obj := info.Uses[e.Sel]; obj != nil {
			return qualify(obj)
		}
	}
	if x, isIdent := e.X.(*ast.Ident); isIdent {
		return x.Name + "." + e.Sel.Name
	}
	return e.Sel.Name
}

func qualify(obj types.Object) string {
	if obj.Pkg() != nil {
		return obj.Pkg().Name() + "." + obj.Name()
	}
	return obj.Name()
}

func typeName(t types.Type) string {
	t = deref(t)
	if named, ok := t.(*types.Named); ok {
		obj := named.Obj()
		if obj.Pkg() != nil {
			return obj.Pkg().Name() + "." + obj.Name()
		}
		return obj.Name()
	}
	return t.String()
}

func deref(t types.Type) types.Type {
	if ptr, ok := t.(*types.Pointer); ok {
		return ptr.Elem()
	}
	return t
}

func stringLit(expr ast.Expr) (string, bool) {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	return strings.Trim(lit.Value, "`\""), true
}

func isHTTPMethod(s string) bool {
	switch strings.ToUpper(s) {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete,
		http.MethodOptions, http.MethodHead, http.MethodConnect, http.MethodTrace:
		return true
	}
	return false
}

func renderExpr(expr ast.Expr) string {
	var b strings.Builder
	_ = printer.Fprint(&b, token.NewFileSet(), expr)
	return b.String()
}

func refOf(node ast.Node, fset *token.FileSet, root string) Ref {
	start := fset.Position(node.Pos())
	end := fset.Position(node.End())
	path := start.Filename
	if rel, err := relTo(root, path); err == nil {
		path = rel
	}
	return Ref{Path: path, Lines: itoa(start.Line) + "-" + itoa(end.Line)}
}

func dedupRoutes(routes []Route) []Route {
	seen := make(map[string]bool, len(routes))
	var out []Route
	for _, r := range routes {
		key := r.Method + " " + r.Path
		if !seen[key] {
			seen[key] = true
			out = append(out, r)
		}
	}
	return out
}
