package main

import (
	"cmp"
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
	// private: an unexported method, which gqlgen never calls: a helper of
	// the methods that call it (see privateOps).
	private bool
	calls   []string // the methods it calls on its receiver
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
	// callee is the receiver method a delegation guard names (see
	// delegate); "" for any other guard.
	callee string
	// alts are the branches a policy accepts in (rulePermit): their
	// conditions, and the loop each sits in.
	alts []guard
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
	sc := scanner{fset: fset, known: known}
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
			private:  !fn.Name.IsExported(),
			calls:    receiverCalls(fn),
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
	rule  branchRule // what the walk makes of a branch (see guards)
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
	access     bool // the call is an access check (an error: one that denies)
	isErr      bool // the local is the call's error result
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
	line := sc.fset.Position(s.Pos()).Line
	last := len(s.Lhs) - 1
	for i, lhs := range s.Lhs {
		ident, isIdent := lhs.(*ast.Ident)
		if !isIdent || ident.Name == "_" {
			continue
		}
		isErr := i == last && (last > 0 || isErrName(ident.Name))
		if i == 0 || isErr {
			locals[ident.Name] = binding{
				call:   call,
				value:  call,
				line:   line,
				access: !isErr && sc.known.has(call) || isErr && sc.known.errDenies(call),
				isErr:  isErr,
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
		return x.Name != goNil && x.Name != goTrue && x.Name != "false"
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
	goBool   = "bool"
	goString = "string"
)

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
	// failed stand for a bound error: the condition reads "err != nil" as
	// "call != nil", rendered "call fails".
	failed map[*ast.CallExpr]bool
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

func newCallMarks() callMarks {
	return callMarks{
		failed:    make(map[*ast.CallExpr]bool),
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
		if name, ok := nilComparison(e); ok {
			if b, bound := locals[name]; bound && b.isErr {
				res.start = min(res.start, b.line)
				res.marks.failed[b.call] = true
				return &ast.BinaryExpr{X: b.call, Op: e.Op, Y: e.Y}
			}
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
	if !ok || sel.Sel.Name != "Kaid" {
		return false
	}
	b := locals[user.Name]
	if b.call == nil || b.isErr {
		return false
	}
	fn, isSel := b.call.Fun.(*ast.SelectorExpr)
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
			if call, ok := e.X.(*ast.CallExpr); ok && res.marks.failed[call] &&
				sc.known.errDenies(call) {
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

// aclMarker returns the rule a marker statement states: acl.OpenAccess()
// (anyone may call) or acl.TODO() — by its documentation a no-op marking a
// resolver that lacks a permission check, "a potential security risk".
func aclMarker(call *ast.CallExpr) (ruleKind, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return 0, false
	}
	if pkg, isIdent := sel.X.(*ast.Ident); !isIdent || pkg.Name != "acl" {
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
				if pkg, isIdent := sel.X.(*ast.Ident); isIdent && pkg.Name == "errors" {
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
	return isIdent && pkg.Name == "errors" && isAuthorizationError(sel.Sel.Name)
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
	found := append(sc.guards(fn.Body), sc.visibility(fn.Body)...)
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

// delegate names the first exported method fn calls on its receiver: an
// alias resolver (FindManyStudentListByDescriptors calling
// r.FindManyClassroomByDescriptors) is checked as the one it calls.
func (sc scanner) delegate(fn *ast.FuncDecl) (guard, bool) {
	var (
		found  *ast.CallExpr
		callee string
	)
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok && found == nil {
			if sel, isSel := call.Fun.(*ast.SelectorExpr); isSel &&
				sel.Sel.IsExported() && isReceiver(fn, sel.X) {
				found, callee = call, sel.Sel.Name
			}
		}
		return found == nil
	})
	if found == nil {
		return guard{}, false
	}
	return guard{
		kind:    ruleChecked,
		subject: found.Fun,
		start:   sc.fset.Position(found.Pos()).Line,
		end:     sc.fset.Position(found.End()).Line,
		callee:  callee,
	}, true
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
					!ruled[m.recv+"."+m.guards[0].callee] {
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
