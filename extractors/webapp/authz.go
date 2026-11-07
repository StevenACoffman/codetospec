package main

import (
	"cmp"
	"go/ast"
	"go/token"
	"maps"
	"math"
	"path"
	"slices"
	"strconv"
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
	// answers are the checks whose result it returns as its value (see
	// checks.answers).
	answers []string
	// private: an unexported method, which gqlgen never calls: a helper of
	// the methods that call it (see privateOps).
	private bool
	calls   []string // the methods it calls on its receiver
	// params are its parameters' names after the context: a field
	// resolver's first is the parent object (hasParent).
	params []string
	// locals maps each local its body defines once, at its top level,
	// to the source of its value (userKaid := *user.Kaid).
	locals map[string]string
	// callers are the locals bound once, at the top level, to the request
	// user (actor, err := ktx.RequestUser()): their Kaid is the caller's.
	callers []string
}

// fromCaller is the origin bind gives the request user's kaid: the call's
// argument is the caller (policy.FromCaller).
const fromCaller = "caller"

// kaidField is the field of a user identity that holds its kaid.
const kaidField = "Kaid"

// hasParent reports whether m resolves a field of an object, its first
// parameter that object: not a root operation's, nor a federation
// entity's (whose parameters are the keys).
func (m *resolverMethod) hasParent() bool {
	return !isRootType(m.typeName) && m.typeName != entityType
}

// bind says where an operand written as text comes from, when the
// request supplies it: "arg:<param>[.field…]" for a parameter of m,
// "parent[:field…]" for the parent object of a field resolver, "caller"
// for the request user's kaid (see callers); "" for anything else (a
// local, a call's result).
func (m *resolverMethod) bind(text string) string {
	t := strings.TrimLeft(text, "*&")
	root, rest, _ := strings.Cut(t, ".")
	if rest == kaidField && slices.Contains(m.callers, root) {
		return fromCaller
	}
	if value, ok := m.locals[root]; ok && !slices.Contains(m.params, root) {
		if rest != "" {
			value += "." + rest
		}
		return m.bind(value) // a value is never itself a local of the body (definedOnce)
	}
	switch i := slices.Index(m.params, root); {
	case i < 0 || root == "":
		return ""
	case i == 0 && m.hasParent() && rest == "":
		return "parent"
	case i == 0 && m.hasParent():
		return "parent:" + rest
	}
	return "arg:" + t
}

// callerLocals lists the locals fn's body binds once, at its top level, as
// the first result of a RequestUser() call: the request user.
func callerLocals(fn *ast.FuncDecl) []string {
	assigned := map[string]int{}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if as, ok := n.(*ast.AssignStmt); ok && len(as.Lhs) > 0 {
			if id, isIdent := as.Lhs[0].(*ast.Ident); isIdent {
				assigned[id.Name]++
			}
		}
		return true
	})
	var out []string
	for _, stmt := range fn.Body.List {
		as, ok := stmt.(*ast.AssignStmt)
		if !ok || as.Tok != token.DEFINE || len(as.Rhs) != 1 {
			continue
		}
		call, isCall := as.Rhs[0].(*ast.CallExpr)
		id, isIdent := as.Lhs[0].(*ast.Ident)
		if isCall && isIdent && isRequestUserCall(call) && assigned[id.Name] == 1 {
			out = append(out, id.Name)
		}
	}
	return out
}

// definedOnce maps each local fn's body defines at its top level with a
// single := and never assigns again to the source of its value.
func definedOnce(fset *token.FileSet, fn *ast.FuncDecl) map[string]string {
	r := renderer{fset: fset}
	locals := map[string]string{}
	assigned := map[string]int{}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if as, ok := n.(*ast.AssignStmt); ok {
			for _, lhs := range as.Lhs {
				if id, isIdent := lhs.(*ast.Ident); isIdent {
					assigned[id.Name]++
				}
			}
		}
		return true
	})
	for _, stmt := range fn.Body.List {
		as, ok := stmt.(*ast.AssignStmt)
		if !ok || as.Tok != token.DEFINE || len(as.Lhs) != 1 || len(as.Rhs) != 1 {
			continue
		}
		if id, isIdent := as.Lhs[0].(*ast.Ident); isIdent && assigned[id.Name] == 1 {
			locals[id.Name] = r.source(as.Rhs[0])
		}
	}
	// A value naming another local would chain: keep only values rooted
	// elsewhere, so bind never loops.
	for name, value := range locals {
		root, _, _ := strings.Cut(strings.TrimLeft(value, "*&"), ".")
		if _, chained := locals[root]; chained {
			delete(locals, name)
		}
	}
	return locals
}

// paramNames lists fn's parameter names after the first (the context).
func paramNames(fn *ast.FuncDecl) []string {
	var names []string
	for _, field := range fn.Type.Params.List {
		for _, name := range field.Names {
			names = append(names, name.Name)
		}
	}
	if len(names) > 0 {
		names = names[1:]
	}
	return names
}

// ruleKind is what a guard says about access.
type ruleKind int

const (
	ruleAccess     ruleKind = iota // IF <condition> THEN reject / no data
	ruleRateLimit                  // the condition is a rate limit (see isRateLimit)
	ruleOpen                       // acl.OpenAccess(): anyone may call
	ruleVisibility                 // results pass through a filter checker
	ruleChecked                    // no shape recognized: names the check made
	ruleUnchecked                  // acl.TODO(): the resolver lacks a permission check
	rulePermit                     // a policy function: accepts only under alts (see permit.go)
)

// outcome is what a guard does to a caller it stops.
type outcome int

const (
	outcomeUnknown outcome = iota // returns something else (e.g. a wrapped error)
	outcomeError                  // returns an errors.X(…) error
	outcomeEmpty                  // returns only zero values: no data, no error
)

// guard is one access check of a resolver method.
type guard struct {
	kind ruleKind
	// subject is what a visibility or checked rule names: the filter call,
	// or the checker referenced.
	subject ast.Expr
	noun    string   // what a visibility rule's filter returns ("udis"); "" if unnamed
	cond    ast.Expr // rejecting condition; nil when open
	outcome outcome
	errName string // returned error constructor, e.g. "Unauthorized"; "" if unknown
	// cited lines: the if statement (the outer one when nested), from the
	// assignment of the first local its condition reads
	start, end int
	marks      callMarks // what cond's calls stand for (see callMarks)
	// loop is the range loop the guard sits in (nil at none); perItem
	// that it rejects that loop's item and carries on (continue).
	loop    *loop
	perItem bool
	// callee is the resolver method a delegation guard names (see
	// delegate); "" for any other guard. calleeRecv is its receiver type
	// when it is not the delegating method's own (a receiver built in
	// place, see rebuiltResolver).
	callee     string
	calleeRecv string
	// alts are the branches a policy accepts in (rulePermit): their
	// conditions, and the loop each sits in.
	alts []guard
	// unknown are the calls of a denying guard's own condition that are no
	// known check (see scanner.unknownCalls): named, with no meaning.
	unknown []*ast.CallExpr
}

// resolverSuffix ends the Go type name of every gqlgen resolver
// (mutationResolver, queryResolver, userResolver …).
const resolverSuffix = "Resolver"

// resolverMethods returns the resolver methods declared in file, with the
// guards built on known checks; types names their GraphQL types.
func resolverMethods(
	fset *token.FileSet,
	file *ast.File,
	known checks,
	types *resolverTypeIndex,
) []resolverMethod {
	sc := scanner{fset: fset, known: known, imports: importNames(file)}
	var methods []resolverMethod
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		recv, typeName := resolverType(fn, types)
		if typeName == "" {
			continue
		}
		methods = append(methods, resolverMethod{
			recv:     recv,
			typeName: typeName,
			method:   fn.Name.Name,
			start:    fset.Position(fn.Pos()).Line,
			end:      fset.Position(fn.End()).Line,
			guards:   sc.rules(fn),
			answers:  known.answers(fn),
			private:  !fn.Name.IsExported(),
			calls:    receiverCalls(fn),
			params:   paramNames(fn),
			locals:   definedOnce(fset, fn),
			callers:  callerLocals(fn),
		})
	}
	return methods
}

// resolverType returns a method's receiver type and the GraphQL type it
// resolves (see resolverTypeIndex.typeOf); both are "" for anything that is
// not a resolver.
func resolverType(fn *ast.FuncDecl, types *resolverTypeIndex) (string, string) {
	recv := receiverName(fn)
	typeName, ok := types.typeOf(recv, fn.Name.Name)
	if !ok {
		return "", ""
	}
	return recv, typeName
}

// receiverName is the Go type name of a method's receiver; "" for a
// function or an unnamed receiver type.
func receiverName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) != 1 {
		return ""
	}
	expr := fn.Recv.List[0].Type
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	if ident, ok := expr.(*ast.Ident); ok {
		return ident.Name
	}
	return ""
}

// receiverCalls lists the methods fn calls on its receiver: _helper in
// e._helper(…).
func receiverCalls(fn *ast.FuncDecl) []string {
	var calls []string
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if sel, isSel := call.Fun.(*ast.SelectorExpr); isSel && isReceiver(fn, sel.X) {
				calls = append(calls, sel.Sel.Name)
			}
		}
		return true
	})
	return calls
}

// isReceiver reports whether x is method fn's receiver: its variable, or a
// value of its type built in place — (&mutationResolver{r.Resolver}).M(…),
// as assignments' devadmin aliases call their target.
func isReceiver(fn *ast.FuncDecl, x ast.Expr) bool {
	if fn.Recv == nil {
		return false
	}
	for {
		paren, ok := x.(*ast.ParenExpr)
		if !ok {
			break
		}
		x = paren.X
	}
	if addr, ok := x.(*ast.UnaryExpr); ok && addr.Op == token.AND {
		x = addr.X
	}
	switch v := x.(type) {
	case *ast.Ident:
		names := fn.Recv.List[0].Names
		return len(names) == 1 && names[0].Name == v.Name
	case *ast.CompositeLit:
		typeName, ok := v.Type.(*ast.Ident)
		return ok && typeName.Name == receiverName(fn)
	}
	return false
}

// rootResolver is the Go type gqlgen's generated code calls into.
const rootResolver = "Resolver"

// resolverTypeIndex says which GraphQL type a resolver method resolves,
// as the root resolver's factory methods declare it:
//
//	func (r *Resolver) AIGuideThread() generated.AIGuideThreadResolver {
//		return &aiGuideThreadResolver{r}
//	}
//
// A Go type may resolve several GraphQL types (gap-finder's entityResolver
// serves Entity and two federated types): a method resolves the one whose
// schema declares its field. A type no factory builds is named after
// itself (mutationResolver → Mutation).
type resolverTypeIndex struct {
	byImpl map[string][]string // Go type → GraphQL types, sorted
	schema schemaIndex
}

// resolverTypes indexes the factories of trees against schema.
func resolverTypes(trees []*ast.File, schema schemaIndex) *resolverTypeIndex {
	idx := &resolverTypeIndex{byImpl: make(map[string][]string), schema: schema}
	for _, file := range trees {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			if impl, typeName, isFactory := factoryType(fn); isFactory &&
				!slices.Contains(idx.byImpl[impl], typeName) {
				idx.byImpl[impl] = append(idx.byImpl[impl], typeName)
				slices.Sort(idx.byImpl[impl])
			}
		}
	}
	return idx
}

// typeOf returns the GraphQL type method of Go type recv resolves: of the
// types its factories declare, the first whose schema has the field (the
// first at all when none has); else the type recv is named after; false
// for a type that is no resolver.
func (idx *resolverTypeIndex) typeOf(recv, method string) (string, bool) {
	if candidates := idx.byImpl[recv]; len(candidates) > 0 {
		for _, c := range candidates {
			if idx.schema.lookup(c, method).file != "" {
				return c, true
			}
		}
		return candidates[0], true
	}
	name := strings.TrimSuffix(recv, resolverSuffix)
	if name == recv || name == "" {
		return "", false
	}
	return strings.ToUpper(name[:1]) + name[1:], true
}

// factoryType returns the Go type a root-resolver factory builds and the
// GraphQL type it resolves: a method of Resolver with no parameters, whose
// one result is a …Resolver interface and whose body returns &impl{…}. The
// type is the one a generated interface names (graphql.MutationResolver →
// Mutation, whatever the method is called: progress's alias
// InvalidatePracticeTaskByID()), else, for an interface of the resolver
// package (gapFillerContentSequenceDataResolver), the method's name. Any
// other method — content-editing's GetServiceSpecificKAContext(ctx),
// returning a context struct — is no factory.
func factoryType(fn *ast.FuncDecl) (string, string, bool) {
	if fn.Body == nil || receiverName(fn) != rootResolver || len(fn.Body.List) != 1 ||
		fn.Type.Params.NumFields() != 0 || fn.Type.Results.NumFields() != 1 {
		return "", "", false
	}
	result := fn.Type.Results.List[0].Type
	typeName, ok := strings.CutSuffix(typeIdent(result), resolverSuffix)
	if _, local := result.(*ast.Ident); local {
		typeName = fn.Name.Name
	}
	impl := constructed(fn.Body.List[0])
	if !ok || typeName == "" || impl == "" {
		return "", "", false
	}
	return impl, typeName, true
}

// typeIdent is the name a type expression ends with: T, or pkg.T's T.
func typeIdent(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		return t.Sel.Name
	}
	return ""
}

// constructed is the type a "return &T{…}" or "return T{…}" statement
// builds; "" for any other statement.
func constructed(stmt ast.Stmt) string {
	ret, ok := stmt.(*ast.ReturnStmt)
	if !ok || len(ret.Results) != 1 {
		return ""
	}
	expr := ret.Results[0]
	if addr, isAddr := expr.(*ast.UnaryExpr); isAddr && addr.Op == token.AND {
		expr = addr.X
	}
	lit, isLit := expr.(*ast.CompositeLit)
	if !isLit {
		return ""
	}
	if ident, isIdent := lit.Type.(*ast.Ident); isIdent {
		return ident.Name
	}
	return ""
}

// checks says which calls are access checks: the checkers the service
// marks //ka:permission-check (see markedCheckers), acl predicates
// (isAccessCheck), the access packages' predicates and the resolver
// package's helper predicates.
type checks struct {
	marked     map[string]checker // see markedCheckers
	predicates map[string]bool    // "permissions.UserCanViewDistrict"
	helpers    map[string]bool    // "_isActor"
	// errVars are the package variables holding an authorization error
	// (var RateLimitExceeded = errors.Unauthorized(…)), as "Var" and
	// "pkg.Var".
	errVars map[string]bool
	// credentials are the package strings a request value is checked
	// against (see credentials).
	credentials map[string]bool
	// capHelpers are the service's capability helpers, by "pkg.Func" (see
	// capHelper); each is also a recognized checker in marked.
	capHelpers map[string]capHelper
}

// has reports whether call is a known access check.
func (c checks) has(call *ast.CallExpr) bool {
	if c.isMarked(call) {
		return true
	}
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		return c.helpers[fun.Name]
	case *ast.SelectorExpr:
		if pkg, ok := fun.X.(*ast.Ident); ok && c.predicates[pkg.Name+"."+fun.Sel.Name] {
			return true
		}
	}
	return isAccessCheck(call) || isRateLimit(call)
}

// isMarked reports whether call is a checker marked //ka:permission-check.
func (c checks) isMarked(call *ast.CallExpr) bool {
	_, ok := c.checkerOf(call)
	return ok
}

// errDenies reports whether call's error is a denial (see checker): only
// then does "if err != nil { return … }" guard (see mentions).
func (c checks) errDenies(call *ast.CallExpr) bool {
	k, ok := c.checkerOf(call)
	return ok && k.errDenies
}

// isFilter reports whether call is a filter checker (see checker.filter).
func (c checks) isFilter(call *ast.CallExpr) bool {
	k, ok := c.checkerOf(call)
	return ok && k.filter
}

// checkerOf returns the marked checker call calls.
func (c checks) checkerOf(call *ast.CallExpr) (checker, bool) {
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		k, ok := c.marked[fun.Name]
		return k, ok
	case *ast.SelectorExpr:
		if pkg, isPkg := fun.X.(*ast.Ident); isPkg {
			if k, ok := c.marked[pkg.Name+"."+fun.Sel.Name]; ok {
				return k, true
			}
		}
		k, ok := c.marked["."+fun.Sel.Name]
		return k, ok
	}
	return checker{}, false
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
	return ok && ident.Name == goBool
}

// helperPredicates names the package functions of files whose decision is
// one expression using a known check: a body of local definitions then a
// single return, the return or a local it reads calling the check
// (func _isActor(ctx, kaid) bool { return acl.IsCurrentUser(ctx, kaid) };
// scope := acl.UserScope(kaid); return acl.ActorHasPermission(ctx, c,
// scope)). A body with control flow decides in several places: not one.
// Resolvers calling them are guarded.
func helperPredicates(files []*ast.File, known checks) map[string]bool {
	helpers := make(map[string]bool)
	for _, file := range files {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if ok && fn.Recv == nil && fn.Body != nil && decidesOnCheck(fn.Body.List, known) {
				helpers[fn.Name.Name] = true
			}
		}
	}
	return helpers
}

// decidesOnCheck reports whether stmts are local definitions (":=", var)
// followed by one return of a single expression that calls a known check,
// directly or through a local it reads.
func decidesOnCheck(stmts []ast.Stmt, known checks) bool {
	if len(stmts) == 0 {
		return false
	}
	ret, isReturn := stmts[len(stmts)-1].(*ast.ReturnStmt)
	if !isReturn || len(ret.Results) != 1 {
		return false
	}
	values := make(map[string]ast.Expr) // local → the value it is defined with
	for _, stmt := range stmts[:len(stmts)-1] {
		if !definesLocals(stmt, values) {
			return false
		}
	}
	if containsCall(ret.Results[0], known.has) {
		return true
	}
	found := false
	ast.Inspect(ret.Results[0], func(n ast.Node) bool {
		if ident, isIdent := n.(*ast.Ident); isIdent && values[ident.Name] != nil {
			found = found || containsCall(values[ident.Name], known.has)
		}
		return !found
	})
	return found
}

// definesLocals records into values the locals stmt defines, one value
// each ("x := v", "var x = v"); false for any other statement.
func definesLocals(stmt ast.Stmt, values map[string]ast.Expr) bool {
	var names []*ast.Ident
	var exprs []ast.Expr
	switch s := stmt.(type) {
	case *ast.AssignStmt:
		if s.Tok != token.DEFINE {
			return false
		}
		for _, lhs := range s.Lhs {
			ident, ok := lhs.(*ast.Ident)
			if !ok {
				return false
			}
			names = append(names, ident)
		}
		exprs = s.Rhs
	case *ast.DeclStmt:
		gen, ok := s.Decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.VAR || len(gen.Specs) != 1 {
			return false
		}
		spec, isValue := gen.Specs[0].(*ast.ValueSpec)
		if !isValue {
			return false
		}
		names, exprs = spec.Names, spec.Values
	default:
		return false
	}
	if len(names) != len(exprs) {
		return len(exprs) == 1 // x, err := f(): defined, with no value of its own
	}
	for i, name := range names {
		values[name.Name] = exprs[i]
	}
	return true
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
	rule  branchRule // what the walk makes of a branch (see guards)
	// imports are the names the file calls its imported packages by: a
	// call through one is a package function, not a method of a local.
	imports map[string]bool
}

// fileScanner returns the scanner of s's guards against known.
func fileScanner(s *source, known checks) scanner {
	return scanner{fset: s.fset, known: known, imports: importNames(s.file)}
}

// importNames returns the names file refers to its imports by: the alias,
// else the import path's last element.
func importNames(file *ast.File) map[string]bool {
	names := make(map[string]bool)
	for _, spec := range file.Imports {
		imported, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		name := path.Base(imported)
		if spec.Name != nil {
			name = spec.Name.Name
		}
		names[name] = true
	}
	return names
}

// unknownCalls returns the function calls of cond, under marks, whose
// result decides and that are no known check: a function of the file's
// package (by name, builtins aside) or of an imported package. A call
// whose error alone is tested ("call != nil", a failed call) is not one:
// a loader's failure (user, err := LoadUser(…); err != nil) is no
// decision. Nor is a method call on a local (user.IsPhantom()): it reads
// data.
func (sc scanner) unknownCalls(cond ast.Expr, marks callMarks) []*ast.CallExpr {
	var calls []*ast.CallExpr
	ast.Inspect(cond, func(n ast.Node) bool {
		if x, isBinary := n.(*ast.BinaryExpr); isBinary {
			if call, isCall := x.X.(*ast.CallExpr); isCall && marks.failed[call] == failsWithError {
				return false // the call's failure: its arguments are data
			}
		}
		call, isCall := n.(*ast.CallExpr)
		if !isCall || sc.known.has(call) {
			return true
		}
		switch fun := call.Fun.(type) {
		case *ast.Ident:
			if !isBuiltin(fun.Name) {
				calls = append(calls, call)
			}
		case *ast.SelectorExpr:
			if pkg, isIdent := fun.X.(*ast.Ident); isIdent && sc.imports[pkg.Name] {
				calls = append(calls, call)
			}
		}
		return true
	})
	return calls
}

// isBuiltin reports whether name is a Go builtin function a condition may
// call.
func isBuiltin(name string) bool {
	switch name {
	case "len", "cap", "min", "max", "new", "make", "append", "complex", "real", "imag",
		// conversions, not calls: string(capabilities.X)
		"string", "bool", "int", "int64", "float64", "byte", "rune":
		return true
	}
	return false
}

// binding is a local bound to a call (allowed := acl.IsLoggedIn(…),
// underAgeGate, err := age.IsUnderAgeGate(…)) or to a condition
// (authorized := svcAdmin || acl.IsCurrentUser(…)).
type binding struct {
	call  *ast.CallExpr // the call the local holds or, for an error, returns
	value ast.Expr      // what the local holds, resolved: call, or a condition
	marks callMarks     // of value's calls
	line  int
	// callerKaid: the local is the caller's own kaid (kaid := user.Kaid,
	// user from ktx.RequestUser()), so acl.IsCurrentUser on it always holds.
	callerKaid bool
	isErr      bool // the local is the call's error result
	// soleResult: the call's only result is the local (err := Validate(…)).
	soleResult bool
	// denialResult: the local is a result of a checker that denies by
	// returning nil (see checker.nilDenies), or a filter's result: "x ==
	// nil" (or "len(x) == 0") reads as its failure.
	denialResult bool
}

// accumulate recognizes "if !x { x = B }" and "if !x { if C { x = true } }"
// on a bound x — the idiom that grants access through one check or
// another — and rebinds x to "x || B" (or "x || C") instead of forgetting
// it.
func (sc scanner) accumulate(s *ast.IfStmt, locals map[string]binding) bool {
	not, isNot := s.Cond.(*ast.UnaryExpr)
	if s.Init != nil || s.Else != nil || !isNot || not.Op != token.NOT || len(s.Body.List) != 1 {
		return false
	}
	x, isIdent := not.X.(*ast.Ident)
	if !isIdent {
		return false
	}
	b, bound := locals[x.Name]
	if !bound || b.isErr {
		return false
	}
	grant := grantOf(s.Body.List[0], x.Name)
	if grant == nil {
		return false
	}
	res := newResolution()
	more := sc.resolve(grant, locals, &res)
	b.value = &ast.BinaryExpr{
		X:  &ast.ParenExpr{X: b.value},
		Op: token.LOR,
		Y:  &ast.ParenExpr{X: more},
	}
	b.marks = b.marks.with(res.marks)
	locals[x.Name] = b
	return true
}

// grantOf returns what stmt grants x: B in "x = B", C in
// "if C { x = true }"; nil for any other statement.
func grantOf(stmt ast.Stmt, x string) ast.Expr {
	switch s := stmt.(type) {
	case *ast.AssignStmt:
		if ident, ok := soleAssignee(s); ok && ident == x && s.Tok == token.ASSIGN {
			return s.Rhs[0]
		}
	case *ast.IfStmt:
		if s.Init != nil || s.Else != nil || len(s.Body.List) != 1 {
			return nil
		}
		set, ok := s.Body.List[0].(*ast.AssignStmt)
		if !ok {
			return nil
		}
		if ident, one := soleAssignee(set); one && ident == x && set.Tok == token.ASSIGN {
			if lit, isIdent := set.Rhs[0].(*ast.Ident); isIdent && lit.Name == goTrue {
				return s.Cond
			}
		}
	}
	return nil
}

// soleAssignee returns the one local s assigns one value to.
func soleAssignee(s *ast.AssignStmt) (string, bool) {
	if len(s.Lhs) != 1 || len(s.Rhs) != 1 {
		return "", false
	}
	ident, ok := s.Lhs[0].(*ast.Ident)
	if !ok {
		return "", false
	}
	return ident.Name, true
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
	if lookup, name, ok := commaOk(s); ok {
		marks := newCallMarks()
		marks.present[lookup] = true
		locals[name] = binding{
			value: lookup,
			marks: marks,
			line:  sc.fset.Position(s.Pos()).Line,
		}
		return
	}
	call, ok := singleCall(s)
	if !ok {
		sc.bindCondition(s, locals)
		return
	}
	sc.bindResults(s, call, locals)
}

// bindResults binds the locals s assigns call's results to: the result
// that decides (the first, or a marked checker's decision), the error
// (the last of several, or a lone err), and every result whose nil or
// emptiness is a marked checker's denial (see binding.denialResult).
func (sc scanner) bindResults(s *ast.AssignStmt, call *ast.CallExpr, locals map[string]binding) {
	line := sc.fset.Position(s.Pos()).Line
	last := len(s.Lhs) - 1
	k, marked := sc.known.checkerOf(call)
	for i, lhs := range s.Lhs {
		ident, isIdent := lhs.(*ast.Ident)
		if !isIdent || ident.Name == "_" {
			continue
		}
		isErr := i == last && (last > 0 || isErrName(ident.Name))
		decides := i == 0 && k.decision == 0 || i+1 == k.decision
		// A nil result is a nil-denying checker's denial; an empty one, a
		// filter's that the caller treats as one (if len(ids) == 0).
		denialResult := marked && !isErr && (k.nilDenies || k.filter && i == 0)
		if decides || isErr || denialResult {
			locals[ident.Name] = binding{
				call:         call,
				value:        call,
				line:         line,
				isErr:        isErr,
				soleResult:   last == 0,
				denialResult: denialResult,
			}
		}
	}
}

// commaOk returns the map lookup of "v, ok := m[k]" and the name of ok.
func commaOk(s *ast.AssignStmt) (*ast.IndexExpr, string, bool) {
	if len(s.Lhs) != 2 || len(s.Rhs) != 1 {
		return nil, "", false
	}
	lookup, isIndex := s.Rhs[0].(*ast.IndexExpr)
	ok, isIdent := s.Lhs[1].(*ast.Ident)
	if !isIndex || !isIdent || ok.Name == "_" {
		return nil, "", false
	}
	return lookup, ok.Name, true
}

// isErrName reports whether a local is named like an error: err, err2,
// validateErr.
func isErrName(name string) bool {
	rest, isErr := strings.CutPrefix(name, "err")
	return isErr && strings.Trim(rest, "0123456789") == "" || strings.HasSuffix(name, "Err")
}

// bindCondition records "x := <condition>" (authorized := a || b, ok :=
// !x, isOwner := doc.KAID == kaid), resolved with the bindings in scope, so
// "if !authorized" reads as the condition it stands for.
func (sc scanner) bindCondition(s *ast.AssignStmt, locals map[string]binding) {
	name, ok := soleAssignee(s)
	if !ok || name == "_" {
		return
	}
	if sel, isSel := s.Rhs[0].(*ast.SelectorExpr); isSel && isCallerKaid(sel, locals) {
		locals[name] = binding{callerKaid: true, line: sc.fset.Position(s.Pos()).Line}
		return
	}
	if !isCondition(s.Rhs[0]) {
		return
	}
	res := newResolution()
	value := sc.resolve(s.Rhs[0], locals, &res)
	line := min(sc.fset.Position(s.Pos()).Line, res.start)
	locals[name] = binding{value: value, marks: res.marks, line: line}
}

// isCondition reports whether e reads as a condition: a logical or
// comparison operation, a negation, a parenthesized condition, or a local.
func isCondition(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.ParenExpr:
		return isCondition(x.X)
	case *ast.UnaryExpr:
		return x.Op == token.NOT
	case *ast.BinaryExpr:
		return slices.Contains(conditionOps(), x.Op)
	case *ast.Ident:
		return x.Name != goNil && x.Name != goTrue && x.Name != goFalse
	}
	return false
}

// conditionOps are the binary operators whose result is a condition.
func conditionOps() []token.Token {
	return []token.Token{
		token.LAND,
		token.LOR,
		token.EQL,
		token.NEQ,
		token.LSS,
		token.LEQ,
		token.GTR,
		token.GEQ,
	}
}

// Go's predeclared identifiers the scanner reads.
const (
	goNil    = "nil"
	goTrue   = "true"
	goFalse  = "false"
	goBool   = "bool"
	goString = "string"
)

// The package the services build their errors with.
const errorsPkg = "errors"

// singleCall returns the call s assigns, when its right side is one call.
func singleCall(s *ast.AssignStmt) (*ast.CallExpr, bool) {
	if len(s.Rhs) != 1 {
		return nil, false
	}
	call, ok := s.Rhs[0].(*ast.CallExpr)
	return call, ok
}

// emptyComparison returns the local whose length e compares to zero and
// the nil comparison it amounts to: "len(x) == 0" (and "< 1") is "x ==
// nil", "len(x) != 0" (and "> 0") "x != nil".
func emptyComparison(e *ast.BinaryExpr) (string, token.Token, bool) {
	call, isCall := e.X.(*ast.CallExpr)
	lit, isLit := e.Y.(*ast.BasicLit)
	if !isCall || !isLit || len(call.Args) != 1 {
		return "", 0, false
	}
	fn, isIdent := call.Fun.(*ast.Ident)
	x, isLocal := call.Args[0].(*ast.Ident)
	if !isIdent || fn.Name != "len" || !isLocal {
		return "", 0, false
	}
	switch {
	case lit.Value == "0" && e.Op == token.EQL, lit.Value == "1" && e.Op == token.LSS:
		return x.Name, token.EQL, true
	case lit.Value == "0" && (e.Op == token.NEQ || e.Op == token.GTR):
		return x.Name, token.NEQ, true
	}
	return "", 0, false
}

// nilComparison returns the local e compares to nil ("err != nil",
// "err == nil").
func nilComparison(e *ast.BinaryExpr) (string, bool) {
	if e.Op != token.EQL && e.Op != token.NEQ {
		return "", false
	}
	ident, isIdent := e.X.(*ast.Ident)
	if null, isNil := e.Y.(*ast.Ident); !isIdent || !isNil || null.Name != goNil {
		return "", false
	}
	return ident.Name, true
}

// resolution is what resolving a condition learned: the first line of the
// locals it read and the calls standing for a bound error.
type resolution struct {
	start int // math.MaxInt when the condition reads no bound local
	marks callMarks
}

func newResolution() resolution {
	return resolution{start: math.MaxInt, marks: newCallMarks()}
}

// callMarks are what some calls of a resolved condition stand for.
type callMarks struct {
	// failed stand for a bound error, or a nil result of a checker that
	// denies with one (see binding.denialResult): the condition reads "err
	// != nil" (or "key == nil") as "call != nil", rendered "call fails".
	failed map[*ast.CallExpr]failure
	// self are acl.IsCurrentUser calls on the caller's own kaid: they always
	// hold, so the check never denies (see binding.callerKaid).
	self map[*ast.CallExpr]bool
	// present stand for the ok of "v, ok := m[k]": the lookup reads "k is
	// a key of m".
	present map[*ast.IndexExpr]bool
	// fromLocal are comparisons whose request value a local held (actual
	// := r.Header.Get(H); actual == ""): their sides read separately, as
	// no line writes them together.
	fromLocal map[*ast.BinaryExpr]bool
}

// failure is how a failed call (see callMarks.failed) fails.
type failure uint8

const (
	failsWithError     failure = iota + 1 // its error is set, beside a value (a loader's)
	failsWithSoleError                    // its error, its only result, is set (a check's)
	failsWithNil                          // it returned nil: its denial (see checker.nilDenies)
)

func newCallMarks() callMarks {
	return callMarks{
		failed:    make(map[*ast.CallExpr]failure),
		self:      make(map[*ast.CallExpr]bool),
		present:   make(map[*ast.IndexExpr]bool),
		fromLocal: make(map[*ast.BinaryExpr]bool),
	}
}

// with returns the union of m and o, in new maps.
func (m callMarks) with(o callMarks) callMarks {
	u := newCallMarks()
	for _, src := range []callMarks{m, o} {
		maps.Copy(u.failed, src.failed)
		maps.Copy(u.self, src.self)
		maps.Copy(u.present, src.present)
		maps.Copy(u.fromLocal, src.fromLocal)
	}
	return u
}

// resolve returns cond with the bound locals it tests replaced by what they
// hold — "allowed" by acl.IsLoggedIn(…), "err != nil" by "call != nil" —
// through !, &&, || and parentheses only, so each condition reads the
// bindings in scope where it stands. cond itself is not modified.
func (sc scanner) resolve(cond ast.Expr, locals map[string]binding, res *resolution) ast.Expr {
	switch e := cond.(type) {
	case *ast.ParenExpr:
		return &ast.ParenExpr{X: sc.resolve(e.X, locals, res)}
	case *ast.UnaryExpr:
		if e.Op == token.NOT {
			return &ast.UnaryExpr{Op: token.NOT, X: sc.resolve(e.X, locals, res)}
		}
	case *ast.BinaryExpr:
		if e.Op == token.LAND || e.Op == token.LOR {
			return &ast.BinaryExpr{
				X:  sc.resolve(e.X, locals, res),
				Op: e.Op,
				Y:  sc.resolve(e.Y, locals, res),
			}
		}
		if failed, ok := failedCall(e, locals, res); ok {
			return failed
		}
		if compared, ok := requestComparison(e, locals, res); ok {
			return compared
		}
	case *ast.Ident:
		if b, ok := locals[e.Name]; ok && !b.isErr && b.value != nil {
			res.start = min(res.start, b.line)
			res.marks = res.marks.with(b.marks)
			return b.value
		}
	case *ast.CallExpr:
		if isSelfCheck(e, locals) {
			res.marks.self[e] = true
		}
	}
	return cond
}

// failedCall returns "call != nil" (call fails) or "call == nil" for a
// nil comparison of a bound error, or of a nil-denying checker's result,
// whose nil is the failure (key == nil reads "call != nil"); false for any
// other comparison.
func failedCall(e *ast.BinaryExpr, locals map[string]binding, res *resolution) (ast.Expr, bool) {
	name, ok := nilComparison(e)
	op := e.Op
	if !ok {
		name, op, ok = emptyComparison(e)
	}
	b, bound := locals[name]
	if !ok || !bound || !b.isErr && !b.denialResult {
		return nil, false
	}
	res.start = min(res.start, b.line)
	how := failsWithError
	if b.soleResult {
		how = failsWithSoleError
	}
	if b.denialResult {
		// x == nil: the call failed, which "call != nil" stands for.
		how = failsWithNil
		if op == token.EQL {
			op = token.NEQ
		} else {
			op = token.EQL
		}
	}
	res.marks.failed[b.call] = how
	return &ast.BinaryExpr{X: b.call, Op: op, Y: e.Y}, true
}

// requestComparison returns e with the request values its locals hold
// (actual := r.Header.Get("X-Ka-Gateway-Secret"); if actual == ""), so
// the comparison names what the request carries; false when e is no
// equality or holds none.
func requestComparison(
	e *ast.BinaryExpr,
	locals map[string]binding,
	res *resolution,
) (ast.Expr, bool) {
	if e.Op != token.EQL && e.Op != token.NEQ {
		return nil, false
	}
	x, y := requestValueOf(e.X, locals, res), requestValueOf(e.Y, locals, res)
	if x == e.X && y == e.Y {
		return nil, false
	}
	compared := &ast.BinaryExpr{X: x, Op: e.Op, Y: y}
	res.marks.fromLocal[compared] = true
	return compared, true
}

// requestValueOf returns the request value a local holds (see
// isRequestValue), else e itself.
func requestValueOf(e ast.Expr, locals map[string]binding, res *resolution) ast.Expr {
	ident, ok := e.(*ast.Ident)
	if !ok {
		return e
	}
	if b, bound := locals[ident.Name]; bound && b.call != nil && isRequestValue(b.call) {
		res.start = min(res.start, b.line)
		return b.call
	}
	return e
}

// isSelfCheck reports whether call is acl.IsCurrentUser on the caller's own
// kaid — a local bound from it, or <user>.Kaid of the request user.
func isSelfCheck(call *ast.CallExpr, locals map[string]binding) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "IsCurrentUser" || len(call.Args) < 2 {
		return false
	}
	switch arg := call.Args[1].(type) {
	case *ast.Ident:
		return locals[arg.Name].callerKaid
	case *ast.SelectorExpr:
		return isCallerKaid(arg, locals)
	}
	return false
}

// isCallerKaid reports whether sel is <user>.Kaid with user bound from
// ktx.RequestUser().
func isCallerKaid(sel *ast.SelectorExpr, locals map[string]binding) bool {
	user, ok := sel.X.(*ast.Ident)
	if !ok || sel.Sel.Name != kaidField {
		return false
	}
	b := locals[user.Name]
	return b.call != nil && !b.isErr && isRequestUserCall(b.call)
}

// isRequestUserCall reports whether call is ktx.RequestUser(), the request
// user's identity.
func isRequestUserCall(call *ast.CallExpr) bool {
	fn, isSel := call.Fun.(*ast.SelectorExpr)
	return isSel && fn.Sel.Name == "RequestUser"
}

// mentions reports whether a resolved condition uses an access check: a
// check call, or the error of one that denies through it (see checker):
// "if err != nil { return nil, err }" after any other call is plumbing.
func (sc scanner) mentions(cond ast.Expr, res *resolution) bool {
	for _, operand := range operands(cond) {
		switch e := operand.(type) {
		case *ast.CallExpr:
			if containsCall(e, sc.known.has) {
				return true
			}
		case *ast.BinaryExpr:
			if call, ok := e.X.(*ast.CallExpr); ok && (res.marks.failed[call] == failsWithNil ||
				res.marks.failed[call] != failsWithNil && res.marks.failed[call] != 0 &&
					sc.known.errDenies(call)) {
				return true
			}
			if sc.known.comparesCredential(e) {
				return true
			}
		}
	}
	return false
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
	case "Unauthorized", "NotAllowed", "Forbidden", "PermissionDenied",
		// a net/http handler's answer: w.WriteHeader(http.StatusUnauthorized)
		"StatusUnauthorized", "StatusForbidden":
		return true
	}
	return false
}

// aclPkg is webapp's access-control package (pkg/khan/acl), named in
// calls by its package name.
const aclPkg = "acl"

// isAccessCheck reports whether call is an access predicate: acl.Is…,
// acl.Actor…, acl.Has…, acl.Can… or a ktx.RequestIs…() method.
func isAccessCheck(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	name := sel.Sel.Name
	if pkg, isIdent := sel.X.(*ast.Ident); isIdent && pkg.Name == aclPkg {
		for _, prefix := range []string{"Is", "Actor", "Has", "Can"} {
			if strings.HasPrefix(name, prefix) {
				return true
			}
		}
		return false
	}
	return strings.HasPrefix(name, "RequestIs")
}

// aclMarker returns the rule a marker statement states: acl.OpenAccess()
// (anyone may call) or acl.TODO() — by its documentation a no-op marking a
// resolver that lacks a permission check, "a potential security risk".
func aclMarker(call *ast.CallExpr) (ruleKind, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return 0, false
	}
	if pkg, isIdent := sel.X.(*ast.Ident); !isIdent || pkg.Name != aclPkg {
		return 0, false
	}
	switch sel.Sel.Name {
	case "OpenAccess":
		return ruleOpen, true
	case "TODO":
		return ruleUnchecked, true
	}
	return 0, false
}

// outcomeOf classifies what block returns to a stopped caller, errName
// being the error it names (see errorName).
func outcomeOf(block *ast.BlockStmt, errName string) outcome {
	if errName != "" {
		return outcomeError
	}
	for _, stmt := range block.List {
		if ret, ok := stmt.(*ast.ReturnStmt); ok && len(ret.Results) > 0 && allZero(ret.Results) {
			return outcomeEmpty
		}
	}
	return outcomeUnknown
}

// allZero reports whether every expression is nil, false, "", 0 or an
// empty slice or map literal ([]T{}): a result that carries no data.
func allZero(exprs []ast.Expr) bool {
	for _, e := range exprs {
		if !isZero(e) {
			return false
		}
	}
	return true
}

// isZero reports whether e is a zero or empty value (see allZero).
func isZero(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.Ident:
		return x.Name == goNil || x.Name == "false"
	case *ast.BasicLit:
		return x.Value == `""` || x.Value == "0"
	case *ast.CompositeLit:
		switch x.Type.(type) {
		case *ast.ArrayType, *ast.MapType:
			return len(x.Elts) == 0
		}
	}
	return false
}

// errorName names the error a stopped branch returns, and reports whether
// it denies access: the first authorization error — an errors.X(…) call, a
// GraphQL error code (…ErrorCodeUnauthorized), an HTTP status
// (http.StatusUnauthorized) or a variable holding one (RateLimitExceeded)
// — else the first errors.X; "" when there is none. Nested if, switch and
// select statements and function literals are skipped: they return on
// another path (if ok { …; if err != nil { return errors.X() }; return
// result } succeeds). A loop is on the path: a guard may build one error
// per item. An error built to be logged is not returned.
func (c checks) errorName(block *ast.BlockStmt) (string, bool) {
	first, denial := "", ""
	ast.Inspect(block, func(n ast.Node) bool {
		if denial != "" {
			return false
		}
		switch x := n.(type) {
		case *ast.IfStmt, *ast.SwitchStmt, *ast.TypeSwitchStmt, *ast.SelectStmt, *ast.FuncLit:
			return false
		case *ast.CallExpr:
			if isLogCall(x) {
				return false
			}
			if sel, isSel := x.Fun.(*ast.SelectorExpr); isSel {
				if pkg, isIdent := sel.X.(*ast.Ident); isIdent && pkg.Name == errorsPkg {
					first = cmp.Or(first, sel.Sel.Name)
					if isAuthorizationError(sel.Sel.Name) {
						denial = sel.Sel.Name
					}
				}
			}
		case *ast.SelectorExpr:
			denial = cmp.Or(denial, errorCodeKind(x.Sel.Name), httpDenial(x), c.errVar(x))
		case *ast.Ident:
			denial = cmp.Or(denial, errorCodeKind(x.Name), c.errVar(x))
		}
		return true
	})
	return cmp.Or(denial, first), denial != ""
}

// errVar is the name of the authorization error variable e reads; "" when
// it reads none.
func (c checks) errVar(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.Ident:
		if c.errVars[x.Name] {
			return x.Name
		}
	case *ast.SelectorExpr:
		if pkg, ok := x.X.(*ast.Ident); ok && c.errVars[pkg.Name+"."+x.Sel.Name] {
			return x.Sel.Name
		}
	}
	return ""
}

// errorVars returns the package variables of sources initialized with an
// authorization error, as "Var" and "pkg.Var".
func errorVars(sources []source) map[string]bool {
	vars := make(map[string]bool)
	for i := range sources {
		pkg := sources[i].file.Name.Name
		for _, v := range packageVars(sources[i].file) {
			if isAuthorizationCall(v.value) {
				vars[v.name] = true
				vars[pkg+"."+v.name] = true
			}
		}
	}
	return vars
}

// packageVar is a package variable or constant declared with a value.
type packageVar struct {
	name  string
	value ast.Expr
}

// packageVars lists file's package variables and constants declared with a
// value, in order: var X = …, and each name of var ( A = …; B, C = …, … ),
// const likewise.
func packageVars(file *ast.File) []packageVar {
	var vars []packageVar
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.VAR && gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			vs, isValue := spec.(*ast.ValueSpec)
			if !isValue || len(vs.Names) != len(vs.Values) {
				continue
			}
			for j, value := range vs.Values {
				vars = append(vars, packageVar{name: vs.Names[j].Name, value: value})
			}
		}
	}
	return vars
}

// isAuthorizationCall reports whether e is errors.X(…) for an
// authorization error X.
func isAuthorizationCall(e ast.Expr) bool {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, isSel := call.Fun.(*ast.SelectorExpr)
	if !isSel {
		return false
	}
	pkg, isIdent := sel.X.(*ast.Ident)
	return isIdent && pkg.Name == errorsPkg && isAuthorizationError(sel.Sel.Name)
}

// httpDenial returns the status a net/http handler denies with
// (http.StatusUnauthorized, http.StatusForbidden); "" for anything else.
func httpDenial(sel *ast.SelectorExpr) string {
	if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "http" &&
		isAuthorizationError(sel.Sel.Name) {
		return sel.Sel.Name
	}
	return ""
}

// isLogCall reports whether call logs: ctx.Log().Warn(…), log.Error(…).
func isLogCall(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	switch x := sel.X.(type) {
	case *ast.Ident:
		return x.Name == "log"
	case *ast.CallExpr:
		inner, isSel := x.Fun.(*ast.SelectorExpr)
		return isSel && inner.Sel.Name == "Log"
	}
	return false
}

// errorCodeKind returns the authorization error a GraphQL error code names
// (graphql.TeacherDirectedCooldownErrorCodeUnauthorized → Unauthorized);
// "" for any other name.
func errorCodeKind(name string) string {
	_, kind, found := strings.Cut(name, "ErrorCode")
	if found && isAuthorizationError(kind) {
		return kind
	}
	return ""
}

// visibility returns a visibility rule per call in body to a filter
// checker — a marked checker returning a collection whose error does not
// deny (permissions.GetAllVisibleUDIs): the operation shows only what it
// keeps. noun is the local the results land in.
func (sc scanner) visibility(body *ast.BlockStmt) []guard {
	var found []guard
	named := make(map[*ast.CallExpr]string)
	ast.Inspect(body, func(n ast.Node) bool {
		if s, ok := n.(*ast.AssignStmt); ok {
			if call, single := singleCall(s); single {
				if ident, isIdent := s.Lhs[0].(*ast.Ident); isIdent && ident.Name != "_" {
					named[call] = ident.Name
				}
			}
		}
		call, ok := n.(*ast.CallExpr)
		if !ok || !sc.known.isFilter(call) {
			return true
		}
		found = append(found, guard{
			kind:    ruleVisibility,
			subject: call,
			noun:    named[call],
			start:   sc.fset.Position(call.Pos()).Line,
			end:     sc.fset.Position(call.End()).Line,
		})
		return true
	})
	return found
}

// rules returns the access rules of a resolver method: its guards and the
// visibility its filters give; when there are none, the check it makes
// (see checkedBy), else the resolver it delegates to (see delegate).
func (sc scanner) rules(fn *ast.FuncDecl) []guard {
	guards := sc.guards(fn.Body)
	found := slices.Clone(guards)
	for _, v := range sc.visibility(fn.Body) {
		// A filter whose emptiness a guard denies on decides the call,
		// not an item (see binding.denialResult).
		if !slices.ContainsFunc(guards, func(g guard) bool {
			call, ok := v.subject.(*ast.CallExpr)
			return ok && g.marks.failed[call] == failsWithNil
		}) {
			found = append(found, v)
		}
	}
	if len(found) > 0 {
		return found
	}
	if g, ok := sc.checkedBy(fn.Body); ok {
		return []guard{g}
	}
	if g, ok := sc.delegate(fn); ok {
		return []guard{g}
	}
	return nil
}

// delegate names the first exported method fn calls on its receiver, or on
// another resolver type's value built in place: an alias resolver
// (FindManyStudentListByDescriptors calling r.FindManyClassroomByDescriptors,
// districts' studentListResolver calling
// (&classroomResolver{r.Resolver}).IsKacPilotClassroom) is checked as the
// one it calls.
func (sc scanner) delegate(fn *ast.FuncDecl) (guard, bool) {
	var (
		found              *ast.CallExpr
		callee, calleeRecv string
	)
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || found != nil {
			return found == nil
		}
		sel, isSel := call.Fun.(*ast.SelectorExpr)
		if !isSel || !sel.Sel.IsExported() {
			return true
		}
		if isReceiver(fn, sel.X) {
			found, callee = call, sel.Sel.Name
		} else if recv, built := rebuiltResolver(sel.X); built {
			found, callee, calleeRecv = call, sel.Sel.Name, recv
		}
		return found == nil
	})
	if found == nil {
		return guard{}, false
	}
	return guard{
		kind:       ruleChecked,
		subject:    found.Fun,
		start:      sc.fset.Position(found.Pos()).Line,
		end:        sc.fset.Position(found.End()).Line,
		callee:     callee,
		calleeRecv: calleeRecv,
	}, true
}

// rebuiltResolver returns the resolver type of a value built in place,
// (&classroomResolver{r.Resolver}): gqlgen's resolver types all end in
// resolverSuffix. False for anything else.
func rebuiltResolver(x ast.Expr) (string, bool) {
	for {
		paren, ok := x.(*ast.ParenExpr)
		if !ok {
			break
		}
		x = paren.X
	}
	if addr, ok := x.(*ast.UnaryExpr); ok && addr.Op == token.AND {
		x = addr.X
	}
	lit, isLit := x.(*ast.CompositeLit)
	if !isLit {
		return "", false
	}
	name, isIdent := lit.Type.(*ast.Ident)
	if !isIdent || !strings.HasSuffix(name.Name, resolverSuffix) || name.Name == rootResolver {
		return "", false
	}
	return name.Name, true
}

// keepBackedDelegations returns methods without the delegation of a method
// whose callee has no rule of its own, until none is left: delegating
// states that the callee checks access, which only its rules show.
func keepBackedDelegations(methods [][]resolverMethod) [][]resolverMethod {
	kept := make([][]resolverMethod, len(methods))
	for i := range methods {
		kept[i] = slices.Clone(methods[i])
	}
	for changed := true; changed; {
		changed = false
		ruled := make(map[string]bool) // "recv.method" with a rule
		for _, fileMethods := range kept {
			for _, m := range fileMethods {
				ruled[m.recv+"."+m.method] = len(m.guards) > 0
			}
		}
		for _, fileMethods := range kept {
			for j := range fileMethods {
				m := &fileMethods[j]
				if len(m.guards) == 1 && m.guards[0].callee != "" &&
					!ruled[m.guards[0].delegatesTo(m.recv)] {
					m.guards = nil
					changed = true
				}
			}
		}
	}
	return kept
}

// checkedBy names the first check body makes — a check call, or a marked
// checker passed as a value — when no guard shape was recognized: webapp's
// linter (ADR-211) makes every resolver reference one, so the rule states
// where access is decided without claiming how.
func (sc scanner) checkedBy(body *ast.BlockStmt) (guard, bool) {
	var found ast.Expr
	var at ast.Node
	ast.Inspect(body, func(n ast.Node) bool {
		if found != nil {
			return false
		}
		switch x := n.(type) {
		case *ast.CallExpr:
			if sc.known.has(x) {
				found, at = x.Fun, x
			}
		case *ast.Ident:
			if _, marked := sc.known.marked[x.Name]; marked {
				found, at = x, x
			}
		}
		return found == nil
	})
	if found == nil {
		return guard{}, false
	}
	return guard{
		kind:    ruleChecked,
		subject: found,
		start:   sc.fset.Position(at.Pos()).Line,
		end:     sc.fset.Position(at.End()).Line,
	}, true
}
