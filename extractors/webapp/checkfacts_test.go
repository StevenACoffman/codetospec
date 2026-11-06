package main

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"testing"
	"testing/fstest"
)

// The fixture files and checks these tests read.
const (
	guardsFile        = "resolvers/guards.go"
	federationFile    = "resolvers/federation.go"
	actorHasPerm      = "acl.ActorHasPermission"
	isCurrentUser     = "acl.IsCurrentUser"
	byIP              = "ratelimit.ByIP"
	userCanViewEmails = "permissions.UserCanViewEmails"
	kaidParam         = "kaid"
	userKaid          = "user.Kaid"
	argKaid           = "arg:kaid"
	parentKaid        = "parent:Kaid"
	userType          = "User"
	objKaid           = "*obj.Kaid"
	objParam          = "obj"
	userKaidLocal     = "userKaid"
)

// TestCheckArgs: a guard's rule names the checks it calls (checks) and, per
// check call in source order, the operands its condition drops (check_args):
// the variable it became, its meaning, the capability, the scope matcher, its
// meaning and its argument, the subject of a self check and where it comes
// from, a rate limit's key and numbers, an unknown check's arguments.
// Markers name themselves.
func TestCheckArgs(t *testing.T) {
	facts, _, err := Analyze(testInputs("en"))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name       string
		file       string
		line       int // a line of the guard
		wantChecks string
		wantArgs   []checkCall
	}{
		{
			name: "self or service admin", file: guardsFile, line: 14,
			wantChecks: isCurrentUser + ",method.RequestIsServiceAdminQuery",
			wantArgs: []checkCall{
				{
					Check: isCurrentUser, Var: isCurrentUser, Means: meansSelf,
					Subject: kaidParam, SubjectFrom: argKaid,
				},
				{
					Check: "method.RequestIsServiceAdminQuery", Var: "request.isServiceAdmin",
					Means: meansServiceAdmin,
				},
			},
		},
		{
			name: "capability with a user scope", file: guardsFile, line: 37,
			wantChecks: actorHasPerm + "," + isCurrentUser,
			wantArgs: []checkCall{
				// Printed as the requirement prints it: without the *.
				{
					Check: isCurrentUser, Var: isCurrentUser, Means: meansSelf,
					Subject: userKaid, SubjectFrom: parentKaid,
				},
				{
					Check: actorHasPerm, Var: "caller.hasPermission.CanViewUserIdentity",
					Means: meansCapability, Scope: scopeUser, Capability: "CanViewUserIdentity",
					Matcher: "acl.UserScope", MatcherArg: userKaid, SubjectFrom: parentKaid,
				},
			},
		},
		{
			name: "capability with the global scope", file: federationFile, line: 39,
			wantChecks: actorHasPerm,
			wantArgs: []checkCall{
				{
					Check: actorHasPerm, Var: "caller.hasPermission.CanManageBlockList",
					Means: meansCapability, Scope: scopeGlobal, Capability: "CanManageBlockList",
					Matcher: "acl.GlobalScope",
				},
			},
		},
		{
			name: "rate limit by IP", file: guardsFile, line: 158,
			wantChecks: byIP,
			wantArgs: []checkCall{
				{
					Check: byIP, Var: "caller.overRateLimit.Signup",
					Rate: &rateArgs{Key: "Signup", Count: "200", Window: "600 * time.Second"},
				},
			},
		},
		{
			name: "access predicate", file: guardsFile, line: 51,
			wantChecks: userCanViewEmails,
			wantArgs: []checkCall{{
				Check: userCanViewEmails, Var: userCanViewEmails,
				Args: []callArg{{Text: "ctx"}, {Text: userKaid, From: parentKaid}},
			}},
		},
		{name: "open access", file: federationFile, line: 21, wantChecks: checkOpen},
		{name: "todo marker", file: federationFile, line: 26, wantChecks: checkTodo},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rule, ok := ruleAt(facts, tc.file, tc.line)
			if !ok {
				t.Fatalf("no rule fact covers %s:%d", tc.file, tc.line)
			}
			if got := rule.Attrs[attrChecks]; got != tc.wantChecks {
				t.Errorf("checks = %q, want %q", got, tc.wantChecks)
			}
			if got := checkCallsOf(t, &rule); !reflect.DeepEqual(got, tc.wantArgs) {
				t.Errorf("check_args = %+v, want %+v", got, tc.wantArgs)
			}
		})
	}
}

// TestEveryNamedCheckHasAFact: each check a rule's check_args names has an
// access_check fact, so a reader can join a call to what it calls.
func TestEveryNamedCheckHasAFact(t *testing.T) {
	facts, _, err := Analyze(testInputs("en"))
	if err != nil {
		t.Fatal(err)
	}
	named := accessChecksByName(facts)
	for i := range facts {
		for _, c := range checkCallsOf(t, &facts[i]) {
			if _, ok := named[c.Check]; !ok {
				t.Errorf(
					"rule %s names check %s, which has no access_check fact",
					facts[i].ID,
					c.Check,
				)
			}
		}
	}
}

// TestAccessCheckForms: an access_check fact says how the check is known
// and cites its definition when the analyzed code holds it.
func TestAccessCheckForms(t *testing.T) {
	facts, _, err := Analyze(testInputs("en"))
	if err != nil {
		t.Fatal(err)
	}
	named := accessChecksByName(facts)
	cases := []struct {
		name, form, path string
	}{
		{userCanViewEmails, formPredicate, "permissions/permissions.go"},
		{"permissions.RequireEditor", formMarked, "permissions/permissions.go"},
		{"_isActor", formHelper, guardsFile},
		{isCurrentUser, formACL, ""}, // pkg/khan/acl isn't imported by the fixture
		{byIP, formRateLimit, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, ok := named[tc.name]
			if !ok {
				t.Fatalf("no access_check fact for %s", tc.name)
			}
			if f.Attrs[attrForm] != tc.form || f.Source.Path != tc.path {
				t.Errorf(
					"form %q at %q, want %q at %q",
					f.Attrs[attrForm],
					f.Source.Path,
					tc.form,
					tc.path,
				)
			}
		})
	}
}

// TestAccessCheckFactsSharedCitation: a check the shared code defines is
// cited under pkgRoot, relative to pkg/ like every shared citation.
func TestAccessCheckFactsSharedCitation(t *testing.T) {
	shared := []source{mustParse(t, "pkg/khan/acl/acl.go", `package acl

func IsLoggedIn(ctx any) bool { return true }
`)}
	rule := Fact{Kind: kindRule, Attrs: map[string]string{attrChecks: "acl.IsLoggedIn"}}
	got := accessCheckFacts([]Fact{rule}, checks{}, nil, shared)
	want := Ref{Path: "khan/acl/acl.go", Lines: "3-3", Root: pkgRoot}
	if len(got) != 1 || got[0].Source != want {
		t.Fatalf("access_check facts = %+v, want one cited at %+v", got, want)
	}
}

// ruleAt is the rule fact of file whose citation covers line.
func ruleAt(facts []Fact, file string, line int) (Fact, bool) {
	for _, f := range facts {
		if f.Kind != kindRule || f.Source.Path != file {
			continue
		}
		if start, end, ok := lineSpan(f.Source.Lines); ok && start <= line && line <= end {
			return f, true
		}
	}
	return Fact{}, false
}

// checkCallsOf decodes a fact's check_args; none for a fact without.
func checkCallsOf(t *testing.T, f *Fact) []checkCall {
	t.Helper()
	raw := f.Attrs[attrCheckArgs]
	if raw == "" {
		return nil
	}
	var calls []checkCall
	if err := json.Unmarshal([]byte(raw), &calls); err != nil {
		t.Fatalf("%s: check_args %q: %v", f.ID, raw, err)
	}
	return calls
}

// accessChecksByName indexes the access_check facts by the check's name.
func accessChecksByName(facts []Fact) map[string]Fact {
	named := map[string]Fact{}
	for _, f := range facts {
		if f.Kind == kindAccessCheck {
			named[f.Attrs[attrName]] = f
		}
	}
	return named
}

// mustParse parses one Go file named name, as the extractor parses sources.
func mustParse(t *testing.T, name, src string) source {
	t.Helper()
	s, err := parseGo(fstest.MapFS{name: {Data: []byte(src)}}, name)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// TestDelegationNamesItsOperation: a delegation rule names the method it is
// checked as and, since the schema has it, the operation that method serves.
func TestDelegationNamesItsOperation(t *testing.T) {
	facts, _, err := Analyze(testInputs("en"))
	if err != nil {
		t.Fatal(err)
	}
	rule, ok := ruleAt(facts, federationFile, 74)
	if !ok {
		t.Fatalf("no rule fact covers %s:74", federationFile)
	}
	if got := rule.Attrs[attrDelegatesTo]; got != "mutationResolver.AddAuthEmail" {
		t.Errorf("delegates_to = %q", got)
	}
	if got := rule.Attrs[attrDelegatesToOperation]; got != "Mutation.addAuthEmail" {
		t.Errorf("delegates_to_operation = %q, want Mutation.addAuthEmail", got)
	}
}

// TestBind: an operand from a resolver parameter is the request's
// argument, from a field resolver's first parameter its parent object, a
// local defined once from either the same; anything else (a loaded value,
// a call) has no origin the request states.
func TestBind(t *testing.T) {
	field := resolverMethod{typeName: userType, params: []string{objParam, "first"}}
	root := resolverMethod{typeName: gqlQuery, params: []string{kaidParam, "input"}}
	entity := resolverMethod{typeName: entityType, params: []string{kaidParam}}
	local := resolverMethod{
		typeName: userType,
		params:   []string{objParam},
		locals: map[string]string{
			userKaidLocal: objKaid, "loaded": "record.Kaid",
		},
	}
	for _, tc := range []struct {
		m          *resolverMethod
		text, want string
	}{
		{&field, objKaid, parentKaid},
		{&field, objParam, "parent"},
		{&field, "first", "arg:first"},
		{&root, kaidParam, argKaid},
		{&root, "input.Kaid", "arg:input.Kaid"},
		{&entity, kaidParam, argKaid},
		{&root, "actorKaid", ""},
		{&local, userKaidLocal, parentKaid},
		{&local, "userKaid.Suffix", "parent:Kaid.Suffix"},
		{&local, "loaded", ""},
		{&root, "ktx.RequestUser().Kaid", ""},
	} {
		if got := tc.m.bind(tc.text); got != tc.want {
			t.Errorf("%s bind(%q) = %q, want %q", tc.m.typeName, tc.text, got, tc.want)
		}
	}
}

// TestDefinedOnce: a local defined once at the body's top level is
// followed; one reassigned, defined in a nested block, or defined from
// another local is not.
func TestDefinedOnce(t *testing.T) {
	const src = `package r
func (r *userResolver) F(ctx context.Context, obj *User) {
	userKaid := *obj.Kaid
	other := obj.Other
	other = "x"
	chained := userKaid
	if true {
		nested := obj.Kaid
		_ = nested
	}
}`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "r.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	fn, ok := file.Decls[0].(*ast.FuncDecl)
	if !ok {
		t.Fatal("the source declares no function first")
	}
	// Printed as the requirement prints it: without the *.
	got := definedOnce(fset, fn)
	if want := map[string]string{"userKaid": "obj.Kaid"}; !reflect.DeepEqual(got, want) {
		t.Errorf("definedOnce = %v, want %v", got, want)
	}
}
