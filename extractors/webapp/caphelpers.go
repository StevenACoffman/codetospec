package main

import (
	"cmp"
	"go/ast"
	"go/token"
	"io/fs"
	"slices"
	"strconv"
	"unicode"

	gqlast "github.com/vektah/gqlparser/v2/ast"
	gqlparser "github.com/vektah/gqlparser/v2/parser"
)

// A capability helper is a function of the service whose decision is
// admin's hasPermission field for one capability, read through a genqlient
// query: content-editing's cross_service.ActorCanEditAnyContent returns
// resp.User.CanEditContent, the field
// hasPermission(name: "CAN_EDIT_CONTENT", scope: ANY_ON_CURRENT_LOCALE).
// A call of one is a capability check, as acl.ActorHasPermission is.

// capHelper is what a capability helper checks: the capability's Go name
// when the query names it, else the helper's argument passing it
// (capabilityArg, from 0 with the context; -1 for a literal), and the
// scope its query's enum means.
type capHelper struct {
	capability    string
	capabilityArg int
	scope         string
	results       checker // where its decision is (see resultChecker)
}

// withHelper gives cc, a call of capability helper h, its meaning: a
// capability check on h's scope, of h's capability or the one the call's
// argument names (capabilities.X, by Go name).
func (cc *checkCall) withHelper(h capHelper, call *ast.CallExpr) {
	cc.Means, cc.Scope, cc.Capability = meansCapability, h.scope, h.capability
	if h.capabilityArg >= 0 && h.capabilityArg < len(call.Args) {
		cc.Capability, _ = capabilityOperand(call.Args[h.capabilityArg])
	}
}

// Admin's capability fields: hasPermission on a Scope enum value,
// hasContentPermission on a locale and content kind (contentScope).
const (
	hasPermissionField        = "hasPermission"
	hasContentPermissionField = "hasContentPermission"
)

// capabilitiesFile is where webapp keeps every capability's Go name and
// stored name, generated from admin's registrations.
const capabilitiesFile = "services/admin/generated/capabilities/capabilities.go"

// permissionScopes maps admin's Scope enum to what each value means; a
// value it lacks has no meaning here, and its helper is none.
func permissionScopes() map[string]string {
	return map[string]string{
		"ANY":                   scopeAny,
		"GLOBAL":                scopeGlobal,
		"ANY_ON_CURRENT_LOCALE": scopeLocale,
		"USER_KIND_ALL":         scopeUserAll,
	}
}

// capabilityHelpers indexes the capability helpers of sources by
// "pkg.Func", read from the service's genqlient operations and webapp's
// capability names.
func capabilityHelpers(in *Inputs, sources []source) map[string]capHelper {
	queries := operationQueries(in.Service)
	if len(queries) == 0 {
		return nil
	}
	goNames := capabilityGoNames(in.Webapp)
	out := map[string]capHelper{}
	for i := range sources {
		s := &sources[i]
		for _, decl := range s.file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil || fn.Recv != nil {
				continue
			}
			if h, isHelper := helperOf(fn, queries, goNames); isHelper {
				h.results = resultChecker(fn.Type, false)
				out[s.file.Name.Name+"."+fn.Name.Name] = h
			}
		}
	}
	return out
}

// helperOf reads fn as a capability helper: one genqlient call, whose
// query has the hasPermission field fn decides by, in one of two shapes:
// returning the field (fn's results are a bool and an error), or returning
// nil values when it is false (no bool result).
func helperOf(
	fn *ast.FuncDecl,
	queries map[string]string,
	goNames map[string]string,
) (capHelper, bool) {
	call, op := soleGenqlientCall(fn.Body)
	query, ok := queries[op]
	if call == nil || !ok {
		return capHelper{}, false
	}
	doc, err := gqlparser.ParseQuery(&gqlast.Source{Input: query})
	if err != nil || len(doc.Operations) != 1 {
		return capHelper{}, false
	}
	field, decided := decidingField(fn, permissionFields(doc.Operations[0].SelectionSet))
	if !decided {
		return capHelper{}, false
	}
	scope := scopeAny
	if field.Name == hasContentPermissionField {
		scope = scopeContentArgs
	} else if arg := field.Arguments.ForName("scope"); arg != nil {
		if scope, ok = permissionScopes()[arg.Value.Raw]; !ok {
			return capHelper{}, false
		}
	}
	h := capHelper{capabilityArg: -1, scope: scope}
	name := field.Arguments.ForName("name")
	switch {
	case name == nil:
		return capHelper{}, false
	case name.Value.Kind == gqlast.StringValue:
		h.capability = cmp.Or(goNames[name.Value.Raw], name.Value.Raw)
	case name.Value.Kind == gqlast.Variable:
		if literal, isLiteral := variableCapability(
			call,
			doc.Operations[0],
			name.Value.Raw,
		); isLiteral {
			h.capability = literal
			break
		}
		if h.capabilityArg = variableArg(
			fn,
			call,
			doc.Operations[0],
			name.Value.Raw,
		); h.capabilityArg < 0 {
			return capHelper{}, false
		}
	default:
		return capHelper{}, false
	}
	return h, true
}

// decidingField is the one hasPermission field fn's decision is: the field
// fn returns as its bool, or the one whose false makes fn return only zero
// values.
func decidingField(fn *ast.FuncDecl, fields []*gqlast.Field) (*gqlast.Field, bool) {
	types := resultTypes(fn.Type)
	byKey := func(goName string) (*gqlast.Field, bool) {
		i := slices.IndexFunc(
			fields,
			func(f *gqlast.Field) bool { return upperFirst(f.Alias) == goName },
		)
		if i < 0 {
			return nil, false
		}
		return fields[i], true
	}
	if len(types) == 2 && isBoolType(types[0]) && returnsErrorLast(fn.Type) {
		ret, ok := lastReturn(fn.Body.List)
		if !ok || len(ret.Results) != 2 {
			return nil, false
		}
		sel, isSel := ret.Results[0].(*ast.SelectorExpr)
		if !isSel {
			return nil, false
		}
		return byKey(sel.Sel.Name)
	}
	if slices.ContainsFunc(types, isBoolType) || !returnsErrorLast(fn.Type) {
		return nil, false
	}
	for _, stmt := range fn.Body.List {
		s, isIf := stmt.(*ast.IfStmt)
		if !isIf || s.Else != nil || !returnsZero(s.Body.List) {
			continue
		}
		not, isNot := s.Cond.(*ast.UnaryExpr)
		if !isNot || not.Op != token.NOT {
			continue
		}
		if sel, isSel := not.X.(*ast.SelectorExpr); isSel {
			return byKey(sel.Sel.Name)
		}
	}
	return nil, false
}

// permissionFields lists the hasPermission and hasContentPermission
// fields a selection set holds, at any depth.
func permissionFields(set gqlast.SelectionSet) []*gqlast.Field {
	var out []*gqlast.Field
	for _, sel := range set {
		f, ok := sel.(*gqlast.Field)
		if !ok {
			continue
		}
		if f.Name == hasPermissionField || f.Name == hasContentPermissionField {
			out = append(out, f)
		}
		out = append(out, permissionFields(f.SelectionSet)...)
	}
	return out
}

// soleGenqlientCall is body's one genqlient.<Operation>(…) call and that
// operation's name; nil when there is none or several.
func soleGenqlientCall(body *ast.BlockStmt) (*ast.CallExpr, string) {
	var found *ast.CallExpr
	var op string
	count := 0
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if sel, isSel := call.Fun.(*ast.SelectorExpr); isSel {
			if pkg, isIdent := sel.X.(*ast.Ident); isIdent && pkg.Name == "genqlient" {
				found, op = call, sel.Sel.Name
				count++
			}
		}
		return true
	})
	if count != 1 {
		return nil, ""
	}
	return found, op
}

// variableArg is the position (from 0, with the context) of fn's parameter
// that call passes as the query variable name: the genqlient function takes
// the context, the client, then the variables in declaration order. -1 when
// the argument is no parameter of fn.
func variableArg(
	fn *ast.FuncDecl,
	call *ast.CallExpr,
	op *gqlast.OperationDefinition,
	name string,
) int {
	k := slices.IndexFunc(op.VariableDefinitions, func(v *gqlast.VariableDefinition) bool {
		return v.Variable == name
	})
	if k < 0 || clientArg+1+k >= len(call.Args) {
		return -1
	}
	ident, ok := call.Args[clientArg+1+k].(*ast.Ident)
	if !ok {
		return -1
	}
	return slices.Index(paramList(fn), ident.Name)
}

// variableCapability is the capability the genqlient call passes as the
// query variable name when it writes one (string(capabilities.X)).
func variableCapability(
	call *ast.CallExpr,
	op *gqlast.OperationDefinition,
	name string,
) (string, bool) {
	k := slices.IndexFunc(op.VariableDefinitions, func(v *gqlast.VariableDefinition) bool {
		return v.Variable == name
	})
	if k < 0 || clientArg+1+k >= len(call.Args) {
		return "", false
	}
	return capabilityOperand(call.Args[clientArg+1+k])
}

// capabilityOperand is the Go name of the capability e names,
// capabilities.X, through a conversion (string(capabilities.X)); false for
// anything else.
func capabilityOperand(e ast.Expr) (string, bool) {
	if call, isCall := e.(*ast.CallExpr); isCall && len(call.Args) == 1 {
		e = call.Args[0] // a conversion keeps the name
	}
	sel, ok := e.(*ast.SelectorExpr)
	if !ok || !pkgIs(sel.X, "capabilities") {
		return "", false
	}
	return sel.Sel.Name, true
}

// paramList lists fn's parameter names in order, the context included.
func paramList(fn *ast.FuncDecl) []string {
	var names []string
	for _, field := range fn.Type.Params.List {
		for _, n := range field.Names {
			names = append(names, n.Name)
		}
	}
	return names
}

// operationQueries maps each genqlient operation of the service to its
// query text; empty when the service has none.
func operationQueries(service fs.FS) map[string]string {
	out := map[string]string{}
	if service == nil {
		return out
	}
	s, err := parseGo(service, genqlientQueries)
	if err != nil {
		return out
	}
	for _, decl := range s.file.Decls {
		if gen, ok := decl.(*ast.GenDecl); ok && gen.Tok == token.CONST {
			for _, spec := range gen.Specs {
				if name, query, isOp := operationText(spec); isOp {
					out[name] = query
				}
			}
		}
	}
	return out
}

// capabilityGoNames maps each capability's stored name to its Go name, from
// webapp's generated capabilities file; empty when it can't be read.
func capabilityGoNames(webapp fs.FS) map[string]string {
	out := map[string]string{}
	if webapp == nil {
		return out
	}
	s, err := parseGo(webapp, capabilitiesFile)
	if err != nil {
		return out
	}
	for _, decl := range s.file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.VAR {
			continue
		}
		for _, spec := range gen.Specs {
			vs, isValue := spec.(*ast.ValueSpec)
			if !isValue || len(vs.Names) != 1 || len(vs.Values) != 1 {
				continue
			}
			if lit, isLit := vs.Values[0].(*ast.BasicLit); isLit && lit.Kind == token.STRING {
				if stored, unquoteErr := strconv.Unquote(lit.Value); unquoteErr == nil {
					out[stored] = vs.Names[0].Name
				}
			}
		}
	}
	return out
}

// isBoolType reports whether t is bool.
func isBoolType(t ast.Expr) bool {
	ident, ok := t.(*ast.Ident)
	return ok && ident.Name == goBool
}

// returnsZero reports whether stmts end in a return of zero values only.
func returnsZero(stmts []ast.Stmt) bool {
	ret, ok := lastReturn(stmts)
	return ok && len(ret.Results) > 0 && allZero(ret.Results)
}

// upperFirst is s with its first letter in upper case: a GraphQL response
// key as genqlient names its Go field (canEditContent → CanEditContent).
func upperFirst(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}
