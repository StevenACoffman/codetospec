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
// definition of an access check. Sources also write it with a space after
// "//" and in the plural (see isDirective).
const permissionCheckDirective = "ka:permission-check"

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
	// decision is the position, from 1, of the bool result that carries
	// the decision when it is not the first (actorKaid, canSetUp, …, err :=
	// ActorCanSetUpAndEditAdminUDI(…)); 0 when the first result decides or
	// the results are unknown.
	decision int
	// nilDenies: the checker has no bool result and denies by returning nil
	// (key, kaid, err := userKeyIfActorCanEditContentKind(…); key == nil),
	// so a nil test of any of its non-error results reads as its failure.
	nilDenies bool
}

// resultChecker returns what a checker's declared results say about where
// its decision is (see checker.decision, checker.nilDenies), with errDenies
// and filter as given.
func resultChecker(fn *ast.FuncType, errDenies bool) checker {
	c := checker{errDenies: errDenies}
	c.filter = !errDenies && returnsCollectionFirst(fn)
	types := resultTypes(fn)
	for i, t := range types {
		if ident, ok := t.(*ast.Ident); ok && ident.Name == goBool {
			if i > 0 {
				c.decision = i + 1
			}
			return c
		}
	}
	c.nilDenies = !c.filter && len(types) > 1 && returnsErrorLast(fn)
	return c
}

// resultTypes lists a function's result types, one per result: (a, b
// bool) counts twice.
func resultTypes(fn *ast.FuncType) []ast.Expr {
	if fn.Results == nil {
		return nil
	}
	var types []ast.Expr
	for _, field := range fn.Results.List {
		for range max(1, len(field.Names)) {
			types = append(types, field.Type)
		}
	}
	return types
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
				c := resultChecker(d.Type, buildsAuthorizationError(d.Body))
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

// wrappedCheckers returns the package variables of sources that hold a
// known checker, by "Var" and "pkg.Var": the checker itself (var Alias =
// _canView) or a wrapper call taking it first (var CanView =
// cache.Cache(_canView, cache.In(…))), whatever the wrapper. A call
// through the variable is the checker's call.
func wrappedCheckers(sources []source, marked map[string]checker) map[string]checker {
	wrapped := make(map[string]checker)
	for i := range sources {
		pkg := sources[i].file.Name.Name
		for _, v := range packageVars(sources[i].file) {
			value := v.value
			if call, isCall := value.(*ast.CallExpr); isCall && len(call.Args) > 0 {
				value = call.Args[0]
			}
			c, ok := checkerNamed(value, pkg, marked)
			if !ok {
				continue
			}
			for _, key := range []string{v.name, pkg + "." + v.name} {
				if _, own := marked[key]; !own {
					wrapped[key] = c
				}
			}
		}
	}
	return wrapped
}

// checkerNamed returns the checker e names as a function value: f (a
// function of package pkg) or p.f.
func checkerNamed(e ast.Expr, pkg string, marked map[string]checker) (checker, bool) {
	switch x := e.(type) {
	case *ast.Ident:
		if c, ok := marked[pkg+"."+x.Name]; ok {
			return c, true
		}
	case *ast.SelectorExpr:
		if p, isIdent := x.X.(*ast.Ident); isIdent {
			c, ok := marked[p.Name+"."+x.Sel.Name]
			return c, ok
		}
	}
	return checker{}, false
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
// start of one of its lines (see isDirective).
func hasDirective(doc *ast.CommentGroup) bool {
	if doc == nil {
		return false
	}
	for _, c := range doc.List {
		if isDirective(c.Text) {
			return true
		}
	}
	return false
}

// isDirective reports whether a "//" comment line opens with the
// directive, spaced or not, singular or plural: "//ka:permission-check",
// "// ka:permission-check", "//ka:permissions-check" all occur in webapp.
func isDirective(comment string) bool {
	text, ok := strings.CutPrefix(comment, "//")
	if !ok {
		return false
	}
	text = strings.TrimLeft(text, " \t")
	text = strings.Replace(text, "ka:permissions-check", permissionCheckDirective, 1)
	rest, found := strings.CutPrefix(text, permissionCheckDirective)
	return found && (rest == "" || strings.HasPrefix(rest, " ") || strings.HasPrefix(rest, "\t"))
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
		sc := fileScanner(s, known)
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
	names, err := goSources(pkg, nil)
	if err != nil {
		return nil, []string{fmt.Sprintf("shared checkers: %v", err)}
	}
	var marking []string
	for _, name := range names {
		src, readErr := fs.ReadFile(pkg, name)
		if readErr == nil && bytes.Contains(src, []byte("ka:permission")) {
			marking = append(marking, name)
		}
	}
	sources, warnings := parseSources(pkg, marking)
	return markedCheckers(sources), warnings
}
