package main

import (
	"go/ast"
	"go/token"
	"go/types"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/packages"
)

// handlerCode resolves a route's handler expression to the module code
// that serves it, as a file span, so the route links to the rules citing
// that code. It sees through the ways handlers are registered:
//
//	h                                   a function or method value: its declaration
//	func(w, r) { handleX(w, r) }        a closure calling one function: that function
//	func(w, r) { …inline… }             any other closure: the closure itself
//	http.HandlerFunc(h)                 a conversion: h
//	withAuth(h), serve.Task(handlers.X) a wrapper: the handler it takes, if in the module
//	newHandler()                        a constructor: its declaration
//
// The zero Ref when the code is outside the module (a library handler) or
// unknown.
type handlerCode struct {
	info  *types.Info
	fset  *token.FileSet
	root  string
	decls map[token.Pos]*ast.FuncDecl // by the position of their name
}

// funcDecls indexes the function declarations of pkgs by name position, so
// a types.Func (which knows only that position) finds its whole span.
func funcDecls(pkgs []*packages.Package) map[token.Pos]*ast.FuncDecl {
	decls := make(map[token.Pos]*ast.FuncDecl)
	for _, pkg := range pkgs {
		for _, file := range pkg.Syntax {
			for _, decl := range file.Decls {
				if fn, ok := decl.(*ast.FuncDecl); ok {
					decls[fn.Name.Pos()] = fn
				}
			}
		}
	}
	return decls
}

func (h handlerCode) of(expr ast.Expr) Ref {
	switch e := ast.Unparen(expr).(type) {
	case *ast.FuncLit:
		if call := soleCall(e.Body); call != nil {
			if ref := h.of(call.Fun); ref.Path != "" {
				return ref
			}
		}
		return h.span(e)
	case *ast.Ident, *ast.SelectorExpr:
		if fn, ok := h.object(e).(*types.Func); ok {
			if decl := h.decls[fn.Pos()]; decl != nil {
				return h.span(decl)
			}
		}
	case *ast.CallExpr:
		return h.ofCall(e)
	}
	return Ref{}
}

// ofCall resolves a conversion or a wrapper to the handler it takes, and a
// constructor to its own declaration.
func (h handlerCode) ofCall(call *ast.CallExpr) Ref {
	if h.info == nil {
		return Ref{}
	}
	if tv, ok := h.info.Types[call.Fun]; ok && tv.IsType() && len(call.Args) == 1 {
		return h.of(call.Args[0])
	}
	for _, arg := range call.Args {
		if ref := h.of(arg); ref.Path != "" {
			return ref
		}
	}
	return h.of(call.Fun)
}

// object is the object an identifier or selector refers to; nil if unknown.
func (h handlerCode) object(e ast.Expr) types.Object {
	if h.info == nil {
		return nil
	}
	switch x := e.(type) {
	case *ast.Ident:
		return h.info.Uses[x]
	case *ast.SelectorExpr:
		if sel, ok := h.info.Selections[x]; ok {
			return sel.Obj()
		}
		return h.info.Uses[x.Sel]
	}
	return nil
}

// span is node's module-relative file span; the zero Ref outside the
// module.
func (h handlerCode) span(node ast.Node) Ref {
	ref := refOf(node, h.fset, h.root)
	if ref.Path == ".." || strings.HasPrefix(ref.Path, "../") || filepath.IsAbs(ref.Path) {
		return Ref{}
	}
	return ref
}

// soleCall returns the call a block consists of ("f(w, r)" or "return
// f(w, r)"); nil for any other block.
func soleCall(body *ast.BlockStmt) *ast.CallExpr {
	if body == nil || len(body.List) != 1 {
		return nil
	}
	switch s := body.List[0].(type) {
	case *ast.ExprStmt:
		call, _ := s.X.(*ast.CallExpr)
		return call
	case *ast.ReturnStmt:
		if len(s.Results) == 1 {
			call, _ := s.Results[0].(*ast.CallExpr)
			return call
		}
	}
	return nil
}
