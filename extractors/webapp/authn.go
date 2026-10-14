package main

import (
	"go/ast"
	"go/token"
	"path"
	"strings"
)

// authenticators returns the functions of sources that authenticate a
// request: they return an error, take the request (*http.Request or
// http.Header) and read a secret — ctx.Secrets().Get(…) or
// secrets.Validate(…) — themselves or through a function of their own
// package. Their error denies: pubsub.DecodeAndValidatePushMsg checks a
// push message's signature this way, though it fails with InvalidInput.
func authenticators(sources []source) []qualifiedName {
	readers := secretReaders(sources)
	var found []qualifiedName
	for i := range sources {
		s := &sources[i]
		reads := readers[packageOf(s)]
		for _, fn := range funcDecls(s.file) {
			if fn.Recv == nil && returnsErrorLast(fn.Type) && takesRequest(fn.Type) &&
				(reads[fn.Name.Name] || callsAny(fn.Body, reads)) {
				found = append(found, qualifiedName{pkg: s.file.Name.Name, name: fn.Name.Name})
			}
		}
	}
	return found
}

// qualifiedName is a package function: pkg.name.
type qualifiedName struct{ pkg, name string }

// pkgKey identifies a package of sources: its directory and name.
type pkgKey struct{ dir, name string }

func packageOf(s *source) pkgKey {
	return pkgKey{path.Dir(s.name), s.file.Name.Name}
}

// secretReaders returns, per package, the functions that read a secret
// (see readsSecret).
func secretReaders(sources []source) map[pkgKey]map[string]bool {
	readers := make(map[pkgKey]map[string]bool)
	for i := range sources {
		k := packageOf(&sources[i])
		for _, fn := range funcDecls(sources[i].file) {
			if !readsSecret(fn.Body) {
				continue
			}
			if readers[k] == nil {
				readers[k] = make(map[string]bool)
			}
			readers[k][fn.Name.Name] = true
		}
	}
	return readers
}

// funcDecls lists the function and method declarations of file with a
// body.
func funcDecls(file *ast.File) []*ast.FuncDecl {
	var fns []*ast.FuncDecl
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Body != nil {
			fns = append(fns, fn)
		}
	}
	return fns
}

// readsSecret reports whether body reads a secret: x.Secrets().Get(…) or
// secrets.Validate(…).
func readsSecret(body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return !found
		}
		sel, isSel := call.Fun.(*ast.SelectorExpr)
		if !isSel {
			return !found
		}
		switch x := sel.X.(type) {
		case *ast.CallExpr:
			found = found || sel.Sel.Name == getMethod && calledName(x) == "Secrets"
		case *ast.Ident:
			found = found || x.Name == "secrets" && sel.Sel.Name == "Validate"
		}
		return !found
	})
	return found
}

// callsAny reports whether body calls, unqualified, one of names.
func callsAny(body *ast.BlockStmt, names map[string]bool) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if ident, isIdent := call.Fun.(*ast.Ident); isIdent && names[ident.Name] {
				found = true
			}
		}
		return !found
	})
	return found
}

// takesRequest reports whether a function has an *http.Request or an
// http.Header parameter.
func takesRequest(fn *ast.FuncType) bool {
	for _, p := range fn.Params.List {
		t := p.Type
		if star, ok := t.(*ast.StarExpr); ok {
			t = star.X
		}
		if sel, ok := t.(*ast.SelectorExpr); ok {
			if pkg, isIdent := sel.X.(*ast.Ident); isIdent && pkg.Name == "http" &&
				(sel.Sel.Name == "Request" || sel.Sel.Name == "Header") {
				return true
			}
		}
	}
	return false
}

// credentials returns the package-level strings of sources whose name says
// they are a credential (token, secret), as "Name" and "pkg.Name": a
// request value compared to one is a shared-secret check (if
// r.URL.Query().Get("token") != _pubsubToken). A heuristic on names: a
// constant compared for another reason (a content type) is not one.
func credentials(sources []source) map[string]bool {
	found := make(map[string]bool)
	for i := range sources {
		pkg := sources[i].file.Name.Name
		for _, v := range packageVars(sources[i].file) {
			if _, isString := stringLit(v.value); isString && isCredentialName(v.name) {
				found[v.name] = true
				found[pkg+"."+v.name] = true
			}
		}
	}
	return found
}

// isCredentialName reports whether a name says it holds a credential.
func isCredentialName(name string) bool {
	lower := strings.ToLower(name)
	return strings.Contains(lower, "token") || strings.Contains(lower, "secret")
}

// getMethod reads a value by key: a header, a query parameter, a secret.
const getMethod = "Get"

// urlQuery is the method of a request URL returning its query parameters.
const urlQuery = "Query"

// isRequestValue reports whether e reads a value the request carries:
// r.URL.Query().Get(…), r.Header.Get(…), r.FormValue(…).
func isRequestValue(e ast.Expr) bool {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, isSel := call.Fun.(*ast.SelectorExpr)
	if !isSel {
		return false
	}
	switch sel.Sel.Name {
	case "FormValue", "PostFormValue":
		return true
	case getMethod:
		switch x := sel.X.(type) {
		case *ast.CallExpr:
			return calledName(x) == urlQuery
		case *ast.SelectorExpr:
			return x.Sel.Name == "Header"
		}
	}
	return false
}

// comparesCredential reports whether e compares a request value with a
// credential (see credentials), in either order.
func (c checks) comparesCredential(e *ast.BinaryExpr) bool {
	if e.Op != token.EQL && e.Op != token.NEQ {
		return false
	}
	for _, pair := range [][2]ast.Expr{{e.X, e.Y}, {e.Y, e.X}} {
		if isRequestValue(pair[0]) && c.isCredential(pair[1]) {
			return true
		}
	}
	return false
}

// isCredential reports whether e names a credential: Name or pkg.Name.
func (c checks) isCredential(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.Ident:
		return c.credentials[x.Name]
	case *ast.SelectorExpr:
		pkg, ok := x.X.(*ast.Ident)
		return ok && c.credentials[pkg.Name+"."+x.Sel.Name]
	}
	return false
}
