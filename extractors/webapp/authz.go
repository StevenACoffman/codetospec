package main

import (
	"go/ast"
	"go/token"
	"maps"
	"math"
	"path"
	"slices"
	"strings"
)

// resolverMethod is one gqlgen resolver method and the access guards found
// at the top level of its body.
type resolverMethod struct {
	recv       string // Go receiver type, e.g. "mutationResolver"
	typeName   string // GraphQL type, e.g. "Mutation" for mutationResolver
	method     string // Go method name, e.g. "AddAuthEmail"
	start, end int    // method span, 1-based inclusive
	guards     []guard
}

// outcome is what a guard does to a caller it stops.
type outcome int

const (
	outcomeUnknown outcome = iota // returns something else (e.g. a wrapped error)
	outcomeError                  // returns an errors.X(…) error
	outcomeEmpty                  // returns only zero values: no data, no error
)

// guard is one access check of a resolver method.
type guard struct {
	open    bool     // acl.OpenAccess(): anyone may call
	cond    ast.Expr // rejecting condition; nil when open
	outcome outcome
	errName string // returned error constructor, e.g. "Unauthorized"; "" if unknown
	// cited lines: the if statement (the outer one when nested), from the
	// assignment of the first local its condition reads
	start, end int
	// bindings maps the locals cond reads to the calls they hold; failures
	// maps the error locals cond compares to nil to the calls they come from.
	bindings, failures map[string]*ast.CallExpr
}

// resolverSuffix ends the Go type name of every gqlgen resolver
// (mutationResolver, queryResolver, userResolver …).
const resolverSuffix = "Resolver"

// resolverMethods returns the resolver methods declared in file, with the
// guards built on known checks.
func resolverMethods(fset *token.FileSet, file *ast.File, known checks) []resolverMethod {
	sc := scanner{fset: fset, known: known}
	var methods []resolverMethod
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		recv, typeName := resolverType(fn)
		if typeName == "" {
			continue
		}
		methods = append(methods, resolverMethod{
			recv:     recv,
			typeName: typeName,
			method:   fn.Name.Name,
			start:    fset.Position(fn.Pos()).Line,
			end:      fset.Position(fn.End()).Line,
			guards:   sc.guards(fn.Body),
		})
	}
	return methods
}

// resolverType returns a method's receiver type and the GraphQL type it
// resolves (mutationResolver → Mutation); both are "" for anything that is
// not a resolver.
func resolverType(fn *ast.FuncDecl) (string, string) {
	if fn.Recv == nil || len(fn.Recv.List) != 1 {
		return "", ""
	}
	expr := fn.Recv.List[0].Type
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	ident, ok := expr.(*ast.Ident)
	if !ok {
		return "", ""
	}
	name := strings.TrimSuffix(ident.Name, resolverSuffix)
	if name == ident.Name || name == "" {
		return "", ""
	}
	return ident.Name, strings.ToUpper(name[:1]) + name[1:]
}

// checks says which calls are access checks: acl predicates
// (isAccessCheck), the access packages' predicates and the resolver
// package's helper predicates.
type checks struct {
	predicates map[string]bool // "permissions.UserCanViewDistrict"
	helpers    map[string]bool // "_isActor"
}

// has reports whether call is a known access check.
func (c checks) has(call *ast.CallExpr) bool {
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		return c.helpers[fun.Name]
	case *ast.SelectorExpr:
		if pkg, ok := fun.X.(*ast.Ident); ok && c.predicates[pkg.Name+"."+fun.Sel.Name] {
			return true
		}
	}
	return isAccessCheck(call)
}

// accessPredicates names, as "pkg.Func", the exported functions of the
// packages in dirs named one of pkgs whose first result is a bool
// (permissions.UserCanViewDistrict): a guard's condition can only test
// a predicate; loaders and filters (permissions.GetAllVisibleUDIs) are
// not checks. Callers must import the package under its own name.
func accessPredicates(sources []source, pkgs []string) map[string]bool {
	predicates := make(map[string]bool)
	for i := range sources {
		file := sources[i].file
		if !slices.Contains(pkgs, path.Base(path.Dir(sources[i].name))) {
			continue
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if ok && fn.Recv == nil && fn.Name.IsExported() && returnsBoolFirst(fn.Type) {
				predicates[file.Name.Name+"."+fn.Name.Name] = true
			}
		}
	}
	return predicates
}

// returnsBoolFirst reports whether a function's first result is a bool.
func returnsBoolFirst(fn *ast.FuncType) bool {
	if fn.Results == nil || len(fn.Results.List) == 0 {
		return false
	}
	ident, ok := fn.Results.List[0].Type.(*ast.Ident)
	return ok && ident.Name == "bool"
}

// helperPredicates names the package functions of files whose body is a
// single return of an expression using a known check
// (func _isActor(ctx, kaid) bool { return acl.IsCurrentUser(ctx, kaid) }):
// resolvers calling them are guarded.
func helperPredicates(files []*ast.File, known checks) map[string]bool {
	helpers := make(map[string]bool)
	for _, file := range files {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || fn.Body == nil || len(fn.Body.List) != 1 {
				continue
			}
			ret, isReturn := fn.Body.List[0].(*ast.ReturnStmt)
			if isReturn && len(ret.Results) == 1 && containsCall(ret.Results[0], known.has) {
				helpers[fn.Name.Name] = true
			}
		}
	}
	return helpers
}

// containsCall reports whether e calls a function accepted by match.
func containsCall(e ast.Expr, match func(*ast.CallExpr) bool) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok && match(call) {
			found = true
		}
		return !found
	})
	return found
}

// scanner finds the guards of resolver methods in one file.
type scanner struct {
	fset  *token.FileSet
	known checks
}

// binding is a local bound to a call (allowed := acl.IsLoggedIn(…),
// underAgeGate, err := age.IsUnderAgeGate(…)).
type binding struct {
	call   *ast.CallExpr
	line   int
	access bool // the call is an access check
	isErr  bool // the local is the call's error result
}

// guards scans the top-level statements of body, in order. Only shapes it
// recognizes yield a guard; anything else is left to the LLM.
func (sc scanner) guards(body *ast.BlockStmt) []guard {
	locals := make(map[string]binding)
	var found []guard
	for _, stmt := range body.List {
		switch s := stmt.(type) {
		case *ast.AssignStmt:
			sc.bind(s, locals)
		case *ast.ExprStmt:
			if call, ok := s.X.(*ast.CallExpr); ok && isOpenAccess(call) {
				line := sc.fset.Position(s.Pos()).Line
				found = append(found, guard{open: true, start: line, end: line})
			}
		case *ast.IfStmt:
			if g, ok := sc.ifGuard(s, locals); ok {
				found = append(found, g)
			}
			found = append(found, sc.nestedGuards(s, locals)...)
		}
		if _, isAssign := stmt.(*ast.AssignStmt); !isAssign {
			forget(stmt, locals)
		}
	}
	return found
}

// forget drops the locals that stmt, a nested statement, may reassign
// ("err = g()" inside an if): they no longer hold their bound call. A
// nested ":=" declares a new variable and leaves the outer one alone.
func forget(stmt ast.Stmt, locals map[string]binding) {
	ast.Inspect(stmt, func(n ast.Node) bool {
		if s, ok := n.(*ast.AssignStmt); ok && s.Tok != token.DEFINE {
			for _, lhs := range s.Lhs {
				if ident, isIdent := lhs.(*ast.Ident); isIdent {
					delete(locals, ident.Name)
				}
			}
		}
		return true
	})
}

// bind records the locals of "x := f(…)", "x, err := f(…)" and
// "err := f(…)": the first local holds f's value, the last of several
// (Go's convention) or a lone err holds its error. Any other local s
// assigns is forgotten.
func (sc scanner) bind(s *ast.AssignStmt, locals map[string]binding) {
	for _, lhs := range s.Lhs {
		if ident, ok := lhs.(*ast.Ident); ok {
			delete(locals, ident.Name)
		}
	}
	call, ok := singleCall(s)
	if !ok {
		return
	}
	line := sc.fset.Position(s.Pos()).Line
	last := len(s.Lhs) - 1
	for i, lhs := range s.Lhs {
		ident, isIdent := lhs.(*ast.Ident)
		if !isIdent || ident.Name == "_" {
			continue
		}
		isErr := i == last && (last > 0 || ident.Name == "err")
		if i == 0 || isErr {
			locals[ident.Name] = binding{
				call:   call,
				line:   line,
				access: !isErr && sc.known.has(call),
				isErr:  isErr,
			}
		}
	}
}

// singleCall returns the call s assigns, when its right side is one call.
func singleCall(s *ast.AssignStmt) (*ast.CallExpr, bool) {
	if len(s.Rhs) != 1 {
		return nil, false
	}
	call, ok := s.Rhs[0].(*ast.CallExpr)
	return call, ok
}

// nilComparison returns the local e compares to nil ("err != nil",
// "err == nil").
func nilComparison(e *ast.BinaryExpr) (string, bool) {
	if e.Op != token.EQL && e.Op != token.NEQ {
		return "", false
	}
	ident, isIdent := e.X.(*ast.Ident)
	if null, isNil := e.Y.(*ast.Ident); !isIdent || !isNil || null.Name != "nil" {
		return "", false
	}
	return ident.Name, true
}

// ifGuard recognizes "if <condition> { … return … }" when the condition
// uses an access check, or when the block returns an authorization error.
func (sc scanner) ifGuard(s *ast.IfStmt, locals map[string]binding) (guard, bool) {
	if s.Init != nil || !exits(s.Body) {
		return guard{}, false
	}
	c := sc.scanCond(s.Cond, locals)
	errName := errorName(s.Body)
	if !c.mentions && !isAuthorizationError(errName) {
		return guard{}, false
	}
	return guard{
		cond:     s.Cond,
		outcome:  outcomeOf(s.Body),
		errName:  errName,
		start:    min(sc.fset.Position(s.Pos()).Line, c.start),
		end:      sc.fset.Position(s.End()).Line,
		bindings: c.bindings,
		failures: c.failures,
	}, true
}

// nestedGuards recognizes the guards one level inside
// "if C1 { …; if C2 { … return … } }": each stops the caller when C1 AND C2
// hold, cited from the outer if. An outer if with an init statement or an
// else branch is skipped: either would change what C1 means.
func (sc scanner) nestedGuards(outer *ast.IfStmt, locals map[string]binding) []guard {
	if outer.Init != nil || outer.Else != nil {
		return nil
	}
	c1 := sc.scanCond(outer.Cond, locals)
	inner := maps.Clone(locals)
	var found []guard
	for _, stmt := range outer.Body.List {
		switch s := stmt.(type) {
		case *ast.AssignStmt:
			sc.bind(s, inner)
		case *ast.IfStmt:
			if g, ok := sc.ifGuard(s, inner); ok && c1.agrees(&g) {
				g.cond = &ast.BinaryExpr{X: outer.Cond, Op: token.LAND, Y: g.cond}
				g.start = min(sc.fset.Position(outer.Pos()).Line, c1.start)
				maps.Copy(g.bindings, c1.bindings)
				maps.Copy(g.failures, c1.failures)
				found = append(found, g)
			}
		}
		if _, isAssign := stmt.(*ast.AssignStmt); !isAssign {
			forget(stmt, inner)
		}
	}
	return found
}

// condScan is what a condition reads: whether it uses an access check, the
// locals it reads with their calls, and the first line they are bound on.
type condScan struct {
	mentions           bool
	start              int // math.MaxInt when cond reads no bound local
	bindings, failures map[string]*ast.CallExpr
}

// scanCond scans the operands of cond against the bound locals.
func (sc scanner) scanCond(cond ast.Expr, locals map[string]binding) condScan {
	c := condScan{
		start:    math.MaxInt,
		bindings: make(map[string]*ast.CallExpr),
		failures: make(map[string]*ast.CallExpr),
	}
	for _, operand := range operands(cond) {
		switch e := operand.(type) {
		case *ast.CallExpr:
			c.mentions = c.mentions || containsCall(e, sc.known.has)
		case *ast.Ident:
			if b, ok := locals[e.Name]; ok && !b.isErr {
				c.mentions = c.mentions || b.access
				c.start = min(c.start, b.line)
				c.bindings[e.Name] = b.call
			}
		case *ast.BinaryExpr:
			// An error never makes a guard by itself: "if err != nil
			// { return nil, err }" is plumbing, not a denial.
			name, isNilTest := nilComparison(e)
			if b, ok := locals[name]; isNilTest && ok && b.isErr {
				c.start = min(c.start, b.line)
				c.failures[name] = b.call
			}
		}
	}
	return c
}

// agrees reports whether no local means one call in c and another in g
// (an outer err shadowed by an inner "x, err := …"): merging their
// conditions would misname one of them.
func (c *condScan) agrees(g *guard) bool {
	for _, pair := range [][2]map[string]*ast.CallExpr{
		{c.bindings, g.bindings}, {c.failures, g.failures}, {c.bindings, g.failures}, {c.failures, g.bindings},
	} {
		for name, call := range pair[0] {
			if other, ok := pair[1][name]; ok && other != call {
				return false
			}
		}
	}
	return true
}

// operands returns the leaves of a boolean condition: it descends through
// !, &&, || and parentheses only, so a local used as a call argument
// (acl.IsCurrentUser(ktx, …)) is not mistaken for a bound operand.
func operands(cond ast.Expr) []ast.Expr {
	switch e := cond.(type) {
	case *ast.ParenExpr:
		return operands(e.X)
	case *ast.UnaryExpr:
		if e.Op == token.NOT {
			return operands(e.X)
		}
	case *ast.BinaryExpr:
		if e.Op == token.LAND || e.Op == token.LOR {
			return append(operands(e.X), operands(e.Y)...)
		}
	}
	return []ast.Expr{cond}
}

// isAuthorizationError reports whether an errors.X constructor denies
// access: a block returning one is a guard whatever its condition.
func isAuthorizationError(name string) bool {
	switch name {
	case "Unauthorized", "NotAllowed", "Forbidden", "PermissionDenied":
		return true
	}
	return false
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

// exits reports whether block ends in a return: only then does a guard
// stop the caller. A block that merely contains one (if hasAny { …
// if err != nil { return … } … }) lets control fall through.
func exits(block *ast.BlockStmt) bool {
	if len(block.List) == 0 {
		return false
	}
	_, ok := block.List[len(block.List)-1].(*ast.ReturnStmt)
	return ok
}

// outcomeOf classifies what block returns to a stopped caller.
func outcomeOf(block *ast.BlockStmt) outcome {
	if errorName(block) != "" {
		return outcomeError
	}
	for _, stmt := range block.List {
		if ret, ok := stmt.(*ast.ReturnStmt); ok && len(ret.Results) > 0 && allZero(ret.Results) {
			return outcomeEmpty
		}
	}
	return outcomeUnknown
}

// allZero reports whether every expression is nil, false, "" or 0.
func allZero(exprs []ast.Expr) bool {
	for _, e := range exprs {
		switch x := e.(type) {
		case *ast.Ident:
			if x.Name != "nil" && x.Name != "false" {
				return false
			}
		case *ast.BasicLit:
			if x.Value != `""` && x.Value != "0" {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// errorName returns X for the first errors.X(…) call on block's own path,
// so a guard can say which error it returns; "" when there is none. Nested
// if, switch and select statements and function literals are skipped: they
// return on another path (if ok { …; if err != nil { return errors.X() };
// return result } succeeds). A loop is on the path: a guard may build one
// error per item.
func errorName(block *ast.BlockStmt) string {
	name := ""
	ast.Inspect(block, func(n ast.Node) bool {
		if name != "" {
			return false
		}
		switch n.(type) {
		case *ast.IfStmt, *ast.SwitchStmt, *ast.TypeSwitchStmt, *ast.SelectStmt, *ast.FuncLit:
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
