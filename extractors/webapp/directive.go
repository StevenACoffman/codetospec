package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"io/fs"
	"strings"
)

// permissionCheckDirective marks webapp's permission checkers. Its own
// linter (dev/linters/permissions_lint.go, ADR-211) requires every resolver
// to call a function so marked, which makes the directive the codebase's
// definition of an access check.
const permissionCheckDirective = "//ka:permission-check"

// checker is what the extractor knows of one marked checker.
type checker struct {
	// errDenies: the checker denies through its error, as it builds an
	// authorization error (errors.Unauthorized…) itself; _canEditAndIsAdmin
	// returns (isAdmin, Unauthorized). Otherwise its error is a failure, not
	// a denial: permissions.GetAllVisibleUDIs denies with an empty list,
	// UserIsKADSSSuperAdmin with false, and both return errors only when
	// data cannot be read.
	errDenies bool
	// filter: the checker returns first a collection and its error does not
	// deny — it keeps what the caller may see (GetAllVisibleUDIs).
	filter bool
}

// markedCheckers returns the checkers sources mark with the directive, by
// call key, the way the linter reads it: on a function's doc comment, an
// interface method's or a function variable's. Keys: "pkg.Func" and "Func"
// for a package function (called from another package or its own),
// ".Method" for a method (called on any receiver), "Var" for a variable.
func markedCheckers(sources []source) map[string]checker {
	marked := make(map[string]checker)
	for i := range sources {
		file := sources[i].file
		pkg := file.Name.Name
		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if !hasDirective(d.Doc) {
					continue
				}
				c := checker{errDenies: buildsAuthorizationError(d.Body)}
				c.filter = !c.errDenies && returnsCollectionFirst(d.Type)
				if d.Recv != nil {
					marked["."+d.Name.Name] = c
				} else {
					marked[d.Name.Name] = c
					marked[pkg+"."+d.Name.Name] = c
				}
			case *ast.GenDecl:
				markGenDecl(d, marked)
			}
		}
	}
	return marked
}

// markGenDecl marks the function variables and the interface methods that
// a declaration carries the directive on.
func markGenDecl(d *ast.GenDecl, marked map[string]checker) {
	for _, spec := range d.Specs {
		switch s := spec.(type) {
		case *ast.ValueSpec:
			if hasDirective(d.Doc) || hasDirective(s.Doc) {
				for _, name := range s.Names {
					marked[name.Name] = checker{}
				}
			}
		case *ast.TypeSpec:
			if iface, ok := s.Type.(*ast.InterfaceType); ok {
				markInterfaceMethods(iface, marked)
			}
		}
	}
}

// markInterfaceMethods marks the methods of iface that carry the
// directive: a call through the interface has no body to inspect, so its
// error is never a denial.
func markInterfaceMethods(iface *ast.InterfaceType, marked map[string]checker) {
	for _, method := range iface.Methods.List {
		if hasDirective(method.Doc) {
			for _, name := range method.Names {
				marked["."+name.Name] = checker{}
			}
		}
	}
}

// buildsAuthorizationError reports whether body calls an errors.X
// constructor that denies access (see isAuthorizationError), other than to
// log it.
func buildsAuthorizationError(body *ast.BlockStmt) bool {
	found := false
	if body == nil {
		return false
	}
	ast.Inspect(body, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if isLogCall(call) {
				return false
			}
			found = found || isAuthorizationCall(call)
		}
		return !found
	})
	return found
}

// hasDirective reports whether a doc comment holds the directive at the
// start of one of its lines.
func hasDirective(doc *ast.CommentGroup) bool {
	if doc == nil {
		return false
	}
	for _, c := range doc.List {
		if strings.HasPrefix(c.Text, permissionCheckDirective) {
			return true
		}
	}
	return false
}

// maxDerivation bounds the passes of deriveDenials: a call chain deeper than
// this through unmarked functions is not followed.
const maxDerivation = 4

// deriveDenials adds to marked the unmarked functions of sources that deny
// through their error by delegation: they return an error and call a
// checker whose error denies (updateUserRoleValidation calls the marked
// getValidatedAdminKaid). Passes repeat until nothing is added. A function
// that merely builds an authorization error somewhere is not one: across
// the services that reads data failures (signup.ProcessSignup fails) as
// access checks.
func deriveDenials(sources []source, marked map[string]checker) {
	known := checks{marked: marked}
	for range maxDerivation {
		if !derivePass(sources, marked, known) {
			return
		}
	}
}

// checkBackedDenials returns the functions of sources, not in known, that
// deny through their error because they check access themselves: a branch
// of their own returns an authorization error under a condition using a
// known check (ai-guide's _ensureDevAccess returns errors.Unauthorized when
// enrollment.HasAccessToAIGuideDev does not hold). Narrower than "builds
// an authorization error": the denial must rest on a check.
func checkBackedDenials(sources []source, known checks) map[string]checker {
	found := make(map[string]checker)
	for i := range sources {
		s := &sources[i]
		pkg := s.file.Name.Name
		sc := scanner{fset: s.fset, known: known}
		for _, fn := range funcDecls(s.file) {
			if fn.Recv != nil || !returnsErrorLast(fn.Type) {
				continue
			}
			if _, done := known.marked[pkg+"."+fn.Name.Name]; done ||
				!deniesOnCheck(sc.guards(fn.Body), known) {
				continue
			}
			found[fn.Name.Name] = checker{errDenies: true}
			found[pkg+"."+fn.Name.Name] = checker{errDenies: true}
		}
	}
	return found
}

// deniesOnCheck reports whether a guard returns an authorization error
// under a condition calling a known check.
func deniesOnCheck(guards []guard, known checks) bool {
	for i := range guards {
		g := &guards[i]
		if g.outcome == outcomeError && isAuthorizationError(g.errName) &&
			containsCall(g.cond, known.has) {
			return true
		}
	}
	return false
}

// derivePass marks the functions that deny by delegation to a checker
// already known; it reports whether it marked any.
func derivePass(sources []source, marked map[string]checker, known checks) bool {
	added := false
	for i := range sources {
		pkg := sources[i].file.Name.Name
		for _, decl := range sources[i].file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil || fn.Recv != nil || !returnsErrorLast(fn.Type) {
				continue
			}
			if _, done := marked[pkg+"."+fn.Name.Name]; done ||
				!containsDenyingCall(fn.Body, known) {
				continue
			}
			marked[fn.Name.Name] = checker{errDenies: true}
			marked[pkg+"."+fn.Name.Name] = checker{errDenies: true}
			added = true
		}
	}
	return added
}

// containsDenyingCall reports whether body calls a checker whose error
// denies.
func containsDenyingCall(body *ast.BlockStmt, known checks) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok && known.errDenies(call) {
			found = true
		}
		return !found
	})
	return found
}

// returnsCollectionFirst reports whether a function's first result is a
// slice or a map.
func returnsCollectionFirst(fn *ast.FuncType) bool {
	if fn.Results == nil || len(fn.Results.List) == 0 {
		return false
	}
	switch fn.Results.List[0].Type.(type) {
	case *ast.ArrayType, *ast.MapType:
		return true
	}
	return false
}

// returnsErrorLast reports whether a function's last result is an error.
func returnsErrorLast(fn *ast.FuncType) bool {
	if fn.Results == nil || len(fn.Results.List) == 0 {
		return false
	}
	ident, ok := fn.Results.List[len(fn.Results.List)-1].Type.(*ast.Ident)
	return ok && ident.Name == "error"
}

// sharedDir is where webapp keeps the packages services share.
const sharedDir = "pkg"

// sharedCheckers returns the checkers webapp's shared packages mark
// (pkg/khan/acl, pkg/web…). Only the files of pkg/ holding the directive
// are parsed: 6 of about 400. A nil webapp yields none; a file that does
// not parse is a warning.
func sharedCheckers(webapp fs.FS) (map[string]checker, []string) {
	if webapp == nil {
		return nil, nil
	}
	pkg, err := fs.Sub(webapp, sharedDir)
	if err != nil {
		return nil, []string{fmt.Sprintf("shared checkers: %v", err)}
	}
	names, err := goSources(pkg)
	if err != nil {
		return nil, []string{fmt.Sprintf("shared checkers: %v", err)}
	}
	var marking []string
	for _, name := range names {
		src, readErr := fs.ReadFile(pkg, name)
		if readErr == nil && bytes.Contains(src, []byte(permissionCheckDirective)) {
			marking = append(marking, name)
		}
	}
	sources, warnings := parseSources(pkg, marking)
	return markedCheckers(sources), warnings
}
