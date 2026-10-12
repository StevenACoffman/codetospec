package main

import (
	"go/ast"
	"go/token"
	"strings"
)

// resolverMethod is one gqlgen resolver method and the access guards found
// at the top level of its body.
type resolverMethod struct {
	typeName   string // GraphQL type, e.g. "Mutation" for mutationResolver
	method     string // Go method name, e.g. "AddAuthEmail"
	start, end int    // method span, 1-based inclusive
	guards     []guard
}

// guard is one access check of a resolver method.
type guard struct {
	open       bool     // acl.OpenAccess(): anyone may call
	cond       ast.Expr // rejecting condition; nil when open
	errName    string   // returned error constructor, e.g. "Unauthorized"; "" if unknown
	start, end int      // cited lines: the if statement, from its predicate's assignment
	// bindings maps the locals cond reads to the access checks they hold.
	bindings map[string]*ast.CallExpr
}

// resolverSuffix ends the Go type name of every gqlgen resolver
// (mutationResolver, queryResolver, userResolver …).
const resolverSuffix = "Resolver"

// resolverMethods returns the resolver methods declared in file.
func resolverMethods(fset *token.FileSet, file *ast.File) []resolverMethod {
	var methods []resolverMethod
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		typeName := resolverType(fn)
		if typeName == "" {
			continue
		}
		methods = append(methods, resolverMethod{
			typeName: typeName,
			method:   fn.Name.Name,
			start:    fset.Position(fn.Pos()).Line,
			end:      fset.Position(fn.End()).Line,
			guards:   guards(fset, fn.Body),
		})
	}
	return methods
}

// resolverType maps a method's receiver to its GraphQL type name
// (mutationResolver → Mutation); "" for anything that is not a resolver.
func resolverType(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) != 1 {
		return ""
	}
	recv := fn.Recv.List[0].Type
	if star, ok := recv.(*ast.StarExpr); ok {
		recv = star.X
	}
	ident, ok := recv.(*ast.Ident)
	if !ok {
		return ""
	}
	name := strings.TrimSuffix(ident.Name, resolverSuffix)
	if name == ident.Name || name == "" {
		return ""
	}
	return strings.ToUpper(name[:1]) + name[1:]
}

// predicate is a local bound to an access check (allowed := acl.IsLoggedIn(…)).
type predicate struct {
	call *ast.CallExpr
	line int
}

// guards scans the top-level statements of body, in order. Only shapes it
// recognizes yield a guard; anything else is left to the LLM.
func guards(fset *token.FileSet, body *ast.BlockStmt) []guard {
	locals := make(map[string]predicate)
	var found []guard
	for _, stmt := range body.List {
		switch s := stmt.(type) {
		case *ast.AssignStmt:
			bindPredicate(fset, s, locals)
		case *ast.ExprStmt:
			if call, ok := s.X.(*ast.CallExpr); ok && isOpenAccess(call) {
				line := fset.Position(s.Pos()).Line
				found = append(found, guard{open: true, start: line, end: line})
			}
		case *ast.IfStmt:
			if g, ok := ifGuard(fset, s, locals); ok {
				found = append(found, g)
			}
		}
	}
	return found
}

// bindPredicate records "x := acl.Check(…)" and "x, err := acl.CheckErr(…)".
func bindPredicate(fset *token.FileSet, s *ast.AssignStmt, locals map[string]predicate) {
	if len(s.Rhs) != 1 || len(s.Lhs) == 0 {
		return
	}
	call, ok := s.Rhs[0].(*ast.CallExpr)
	if !ok || !isAccessCheck(call) {
		return
	}
	if ident, isIdent := s.Lhs[0].(*ast.Ident); isIdent && ident.Name != "_" {
		locals[ident.Name] = predicate{call: call, line: fset.Position(s.Pos()).Line}
	}
}

// ifGuard recognizes "if <access condition> { … return … }".
func ifGuard(fset *token.FileSet, s *ast.IfStmt, locals map[string]predicate) (guard, bool) {
	if s.Init != nil || !returns(s.Body) {
		return guard{}, false
	}
	start := fset.Position(s.Pos()).Line
	mentions := false
	bindings := make(map[string]*ast.CallExpr)
	ast.Inspect(s.Cond, func(n ast.Node) bool {
		switch e := n.(type) {
		case *ast.CallExpr:
			if isAccessCheck(e) {
				mentions = true
			}
		case *ast.Ident:
			if p, ok := locals[e.Name]; ok {
				mentions = true
				start = min(start, p.line)
				bindings[e.Name] = p.call
			}
		}
		return true
	})
	if !mentions {
		return guard{}, false
	}
	return guard{
		cond:     s.Cond,
		errName:  errorName(s.Body),
		start:    start,
		end:      fset.Position(s.End()).Line,
		bindings: bindings,
	}, true
}

// isAccessCheck reports whether call is an access predicate: acl.Is…,
// acl.Actor…, acl.Has…, acl.Can… or a ktx.RequestIs…() method.
func isAccessCheck(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	name := sel.Sel.Name
	if pkg, isIdent := sel.X.(*ast.Ident); isIdent && pkg.Name == "acl" {
		for _, prefix := range []string{"Is", "Actor", "Has", "Can"} {
			if strings.HasPrefix(name, prefix) {
				return true
			}
		}
		return false
	}
	return strings.HasPrefix(name, "RequestIs")
}

func isOpenAccess(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "acl" && sel.Sel.Name == "OpenAccess"
}

// returns reports whether block contains a return statement.
func returns(block *ast.BlockStmt) bool {
	found := false
	ast.Inspect(block, func(n ast.Node) bool {
		if _, ok := n.(*ast.ReturnStmt); ok {
			found = true
		}
		return !found
	})
	return found
}

// errorName returns X for the first errors.X(…) call in block, so a guard
// can say which error it returns; "" when there is none.
func errorName(block *ast.BlockStmt) string {
	name := ""
	ast.Inspect(block, func(n ast.Node) bool {
		if name != "" {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if sel, isSel := call.Fun.(*ast.SelectorExpr); isSel {
			if pkg, isIdent := sel.X.(*ast.Ident); isIdent && pkg.Name == "errors" {
				name = sel.Sel.Name
			}
		}
		return true
	})
	return name
}
