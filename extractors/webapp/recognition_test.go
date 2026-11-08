package main

import (
	"fmt"
	"go/ast"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
)

// isServiceAdminCond is the condition of a service-admin check.
const isServiceAdminCond = `{"var":"request.isServiceAdmin","op":"equal","value":true}`

// The files the recognition tests parse.
const (
	resolverFile = "r/r.go"
	checkersFile = "r/c.go"
)

// sourcesOf parses files (name → Go source) as the extractor parses a
// service's files.
func sourcesOf(t *testing.T, files map[string]string) []source {
	t.Helper()
	fsys := fstest.MapFS{}
	names := make([]string, 0, len(files))
	for name, src := range files {
		fsys[name] = &fstest.MapFile{Data: []byte(src)}
		names = append(names, name)
	}
	sources, warnings := parseSources(fsys, names)
	if len(warnings) > 0 {
		t.Fatalf("parse: %v", warnings)
	}
	return sources
}

// TestDirectiveSpellings: the directive is recognized however its comment
// is spaced, and in its plural form, as webapp's code writes it; another
// directive, or the directive's words inside prose, is not one.
func TestDirectiveSpellings(t *testing.T) {
	sources := sourcesOf(t, map[string]string{"p/acls.go": `package p

//ka:permission-check
func standard() bool { return true }

// ka:permission-check // resolvers can use this
func spaced() bool { return true }

//ka:permissions-check
func plural() bool { return true }

//ka:shareable
func other() bool { return true }

// See ka:permission-check for what this means.
func prose() bool { return true }
`})
	marked := markedCheckers(sources)
	for name, want := range map[string]bool{
		"standard": true, "spaced": true, "plural": true, "other": false, "prose": false,
	} {
		if _, got := marked[name]; got != want {
			t.Errorf("%s marked = %v, want %v", name, got, want)
		}
	}
}

// guarded is what one rule of a function says: its condition (the
// notation), the checks it names and, for one that drops an item, its
// scope.
type guarded struct {
	cond, checks, scope string
}

// guardsOf returns what each guard of function fn in files says, the
// checks known from files alone.
func guardsOf(t *testing.T, files map[string]string, fn string) []guarded {
	t.Helper()
	sources := sourcesOf(t, files)
	known, _ := knownChecks(sources, nil, &Inputs{})
	trees := make([]*ast.File, len(sources))
	for i := range sources {
		trees[i] = sources[i].file
	}
	known.helpers = helperPredicates(trees, known)
	var out []guarded
	for i := range sources {
		sc := fileScanner(&sources[i], known)
		for _, decl := range funcDecls(sources[i].file) {
			if decl.Name.Name != fn {
				continue
			}
			for _, g := range sc.rules(decl) {
				attrs := known.checkAttrs(sources[i].fset, &g, func(string) string { return "" })
				out = append(out, guarded{
					conditionOf(sources[i].fset, &g), attrs[attrChecks], attrs[attrCheckScope],
				})
			}
		}
	}
	return out
}

// TestMultiValueDecision: a marked checker's decision is its first bool
// result, wherever it stands; a checker with no bool result denies with a
// nil result, so testing that result for nil reads as the check failing.
func TestMultiValueDecision(t *testing.T) {
	const checkers = `package r

//ka:permission-check
func canSetUp(ctx context.Context, id string) (string, bool, bool, error) {
	return "", true, false, nil
}

//ka:permission-check
func keyIfCanEdit(ctx context.Context) (*string, *string, error) {
	return nil, nil, nil
}
`
	cases := []struct {
		name, body string
		want       guarded
	}{
		{
			"second result decides",
			`kaid, ok, acting, err := canSetUp(ctx, id)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.Unauthorized("no")
	}`,
			guarded{`{"not":{"var":"canSetUp","op":"equal","value":true}}`, "canSetUp", ""},
		},
		{
			"nil result denies",
			`key, kaid, err := keyIfCanEdit(ctx)
	if err != nil {
		return nil, err
	}
	if key == nil {
		return nil, errors.Unauthorized("no")
	}`,
			guarded{`{"var":"keyIfCanEdit.fails","op":"equal","value":true}`, "keyIfCanEdit", ""},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := "package r\nfunc Resolve(ctx context.Context, id string) (*T, error) {\n\t" +
				tc.body + "\n\treturn nil, nil\n}\n"
			got := guardsOf(
				t,
				map[string]string{checkersFile: checkers, resolverFile: src},
				"Resolve",
			)
			if len(got) != 1 || got[0] != tc.want {
				t.Errorf("guards = %+v, want [%+v]", got, tc.want)
			}
		})
	}
}

// TestWrappedChecker: a package variable wrapping a checker (a cache
// around it, or the function itself) is that checker; a variable wrapping
// anything else is not.
func TestWrappedChecker(t *testing.T) {
	const checkers = `package r

//ka:permission-check
func _canView(ctx context.Context, d string) bool { return true }

func plain(ctx context.Context, d string) bool { return true }

var CanView = cache.Cache(_canView, cache.In(lib.RequestCache))

var Alias = _canView

var NotACheck = cache.Cache(plain, cache.In(lib.RequestCache))
`
	for fn, want := range map[string][]guarded{
		"ViaCache": {{`{"not":{"var":"CanView","op":"equal","value":true}}`, "CanView", ""}},
		"ViaAlias": {{`{"not":{"var":"Alias","op":"equal","value":true}}`, "Alias", ""}},
		"ViaPlain": nil,
	} {
		t.Run(fn, func(t *testing.T) {
			call := map[string]string{"ViaCache": "CanView", "ViaAlias": "Alias", "ViaPlain": "NotACheck"}[fn]
			src := "package r\nfunc " + fn + "(ctx context.Context, d string) (*T, error) {\n" +
				"\tif !" + call + "(ctx, d) {\n\t\treturn nil, errors.Unauthorized(\"no\")\n\t}\n" +
				"\treturn nil, nil\n}\n"
			files := map[string]string{checkersFile: checkers, resolverFile: src}
			if want == nil {
				known, _ := knownChecks(sourcesOf(t, files), nil, &Inputs{})
				if _, marked := known.marked[call]; marked {
					t.Errorf("%s is a known checker; it wraps no checker", call)
				}
				return
			}
			got := guardsOf(t, files, fn)
			if len(got) != 1 || got[0] != want[0] {
				t.Errorf("guards = %+v, want %+v", got, want)
			}
		})
	}
}

// TestHelperWithLocals: a function made of local definitions and one
// return using a known check is a helper predicate; one with control flow
// is not (its decision is no single expression).
func TestHelperWithLocals(t *testing.T) {
	sources := sourcesOf(t, map[string]string{"r/h.go": `package r

func single(ctx context.Context, kaid string) bool {
	return acl.IsCurrentUser(ctx, kaid)
}

func withLocal(ctx context.Context, kaid string) bool {
	scope := acl.UserScope(kaid)
	var c = capabilities.CanView
	return acl.ActorHasPermission(ctx, c, scope)
}

func branching(ctx context.Context, kaid string) bool {
	if kaid == "" {
		return false
	}
	return acl.IsCurrentUser(ctx, kaid)
}

func noCheck(ctx context.Context, kaid string) bool {
	x := kaid
	return x != ""
}
`})
	known, _ := knownChecks(sources, nil, &Inputs{})
	got := helperPredicates([]*ast.File{sources[0].file}, known)
	for name, want := range map[string]bool{
		"single": true, "withLocal": true, "branching": false, "noCheck": false,
	} {
		if got[name] != want {
			t.Errorf("helper %s = %v, want %v", name, got[name], want)
		}
	}
}

// TestUnknownCallsInDenials: a guard that returns an authorization error
// names every function it calls in its condition, known or not, so a
// person can pin what it means; method calls on locals, builtins and
// guards that deny nothing name none.
func TestUnknownCallsInDenials(t *testing.T) {
	const helpers = `package r

import "example.com/auth"

func isTester(ctx context.Context) bool {
	if x := 1; x > 0 {
		return true
	}
	return false
}
`
	for name, tc := range map[string]struct{ body, checks string }{
		"local function": {
			`if !isTester(ctx) {
		return nil, errors.Unauthorized("no")
	}`, "isTester",
		},
		"package function": {
			`if !auth.TokenMatches(ctx, token) {
		return nil, errors.Unauthorized("no")
	}`, "auth.TokenMatches",
		},
		"method on a local, builtin": {
			`if len(ids) == 0 || user.IsPhantom() {
		return nil, errors.Unauthorized("no")
	}`, "",
		},
		"a loader's failure": {
			`user, err := auth.LoadUser(ctx, token)
	if err != nil {
		return nil, errors.Unauthorized("no")
	}
	_ = user`, "",
		},
		"no authorization error": {
			`if !isTester(ctx) {
		return nil, errors.InvalidInput("no")
	}`, "",
		},
	} {
		t.Run(name, func(t *testing.T) {
			src := "package r\n\nimport \"example.com/auth\"\n\n" +
				"func Resolve(ctx context.Context, token string, ids []string) (*T, error) {\n\t" +
				tc.body + "\n\treturn nil, nil\n}\n"
			got := guardsOf(t, map[string]string{"r/h.go": helpers, resolverFile: src}, "Resolve")
			var checks strings.Builder
			for _, g := range got {
				checks.WriteString(g.checks)
			}
			if checks.String() != tc.checks {
				t.Errorf("checks = %q, want %q (%+v)", checks.String(), tc.checks, got)
			}
		})
	}
}

// TestEmbeddedResolverDirs: resolver packages the root resolvers embed are
// scanned too, followed through embedding; an imported package no
// resolver embeds is not.
func TestEmbeddedResolverDirs(t *testing.T) {
	const module = "github.com/Khan/webapp/services/svc"
	sources := sourcesOf(t, map[string]string{
		"resolvers/resolver.go": `package resolvers

import (
	"reflect"
	"` + module + `/resolvers/reporting"
	"` + module + `/helpers"
	modResolvers "` + module + `/moderation/resolvers"
)

type moderationQueries = modResolvers.QueryResolver

type queryResolver struct {
	*Resolver
	*reporting.QueryResolver
	moderationQueries
}

func use() { helpers.Do() }
`,
		"resolvers/reporting/query.go": `package reporting

import "` + module + `/resolvers/reporting/deeper"

type QueryResolver struct {
	deeper.Resolvers
}
`,
		"resolvers/reporting/deeper/d.go": "package deeper\n\ntype Resolvers struct{}\n",
		"helpers/h.go":                    "package helpers\n\nfunc Do() {}\n",
		"moderation/resolvers/q.go":       "package resolvers\n\ntype QueryResolver struct{}\n",
	})
	var got []string
	for _, s := range resolverFiles(sources) {
		got = append(got, s.name)
	}
	want := []string{
		"moderation/resolvers/q.go",
		"resolvers/reporting/deeper/d.go",
		"resolvers/reporting/query.go",
		"resolvers/resolver.go",
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("resolver files = %v, want %v", got, want)
	}
}

// TestOnlySchemaFieldsAreOperations: a resolver method matching no schema
// field (a field since removed, or a private helper whose callers serve
// different operations) still yields its rules, but they name no
// operation of their own; a helper whose callers agree takes theirs.
func TestOnlySchemaFieldsAreOperations(t *testing.T) {
	const deny = "\tif !acl.IsLoggedIn(ctx, acl.PhantomProhibited) {\n" +
		"\t\treturn nil, errors.Unauthorized(\"no\")\n\t}\n"
	fsys := fstest.MapFS{
		"gqlgen.yml": {Data: []byte("schema:\n- schema.graphql\n")},
		"schema.graphql": {Data: []byte(
			"type Mutation {\n  assign: String\n  assignAll: String\n  rename: String\n}\n")},
		"resolvers/m.go": {Data: []byte("package resolvers\n\n" +
			"func (r *Resolver) Mutation() graphql.MutationResolver { return &mutationResolver{r} }\n\n" +
			"func (r *mutationResolver) Assign(ctx context.Context) (*string, error) {\n" +
			"\tif err := r.canAssign(ctx); err != nil {\n\t\treturn nil, err\n\t}\n\treturn nil, nil\n}\n\n" +
			"func (r *mutationResolver) AssignAll(ctx context.Context) (*string, error) {\n" +
			"\tif err := r.canAssign(ctx); err != nil {\n\t\treturn nil, err\n\t}\n\treturn nil, nil\n}\n\n" +
			"func (r *mutationResolver) canAssign(ctx context.Context) error {\n" + deny + "\treturn nil\n}\n\n" +
			"func (r *mutationResolver) Rename(ctx context.Context) (*string, error) {\n" +
			"\treturn r.check(ctx)\n}\n\n" +
			"func (r *mutationResolver) check(ctx context.Context) (*string, error) {\n" + deny +
			"\treturn nil, nil\n}\n\n" +
			"func (r *mutationResolver) Removed(ctx context.Context) (*string, error) {\n" + deny +
			"\treturn nil, nil\n}\n")},
	}
	facts, _, err := Analyze(&Inputs{Service: fsys, Lang: "en"})
	if err != nil {
		t.Fatal(err)
	}
	ops := map[string]bool{}
	for _, f := range facts {
		if f.Kind == kindRule {
			ops[f.Attrs[attrOperation]] = true
		}
	}
	for op, want := range map[string]bool{
		"Mutation.canAssign": false, "Mutation.removed": false, "Mutation.rename": true,
	} {
		if ops[op] != want {
			t.Errorf("operation %s named = %v, want %v (all: %v)", op, ops[op], want, ops)
		}
	}
}

// TestEmptyFilterDeniesTheCall: a filter checker whose result is only
// tested for emptiness, to deny the call, is a call-level check (it fails
// when nothing is visible), not a filter of items; a filter whose result
// is returned stays one.
func TestEmptyFilterDeniesTheCall(t *testing.T) {
	const checkers = `package r

//ka:permission-check
func visibleSchools(ctx context.Context, d string) ([]string, error) { return nil, nil }
`
	cases := map[string]struct {
		body string
		want []guarded
	}{
		"empty denies": {
			`ids, err := visibleSchools(ctx, d)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, errors.Unauthorized("no")
	}
	return nil, nil`,
			[]guarded{
				{`{"var":"visibleSchools.fails","op":"equal","value":true}`, "visibleSchools", ""},
			},
		},
		"result returned": {
			`ids, err := visibleSchools(ctx, d)
	if err != nil {
		return nil, err
	}
	return ids, nil`,
			[]guarded{{"", "visibleSchools", checkScopeItem}},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			src := "package r\nfunc Resolve(ctx context.Context, d string) ([]string, error) {\n\t" +
				tc.body + "\n}\n"
			got := guardsOf(
				t,
				map[string]string{checkersFile: checkers, resolverFile: src},
				"Resolve",
			)
			if len(got) != len(tc.want) || got[0] != tc.want[0] {
				t.Errorf("rules = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// TestDecisionAndFailureOfOneCall: a guard that tests both a check's
// decision and its error (if !ok || err != nil) names two variables, the
// decision and the failure, never the failure twice.
func TestDecisionAndFailureOfOneCall(t *testing.T) {
	const checkers = `package r

//ka:permission-check
func isTeacher(ctx context.Context, kaid string) (bool, error) { return true, nil }
`
	src := `package r
func Resolve(ctx context.Context, kaid string) (*T, error) {
	ok, err := isTeacher(ctx, kaid)
	if !ok || err != nil {
		return nil, errors.Unauthorized("no")
	}
	return nil, nil
}
`
	got := guardsOf(t, map[string]string{checkersFile: checkers, resolverFile: src}, "Resolve")
	want := guarded{
		`{"any":[{"not":{"var":"isTeacher","op":"equal","value":true}},` +
			`{"var":"isTeacher.fails","op":"equal","value":true}]}`,
		"isTeacher", "",
	}
	if len(got) != 1 || got[0] != want {
		t.Errorf("guards = %+v, want [%+v]", got, want)
	}
}

// TestCheckReturningACheck: a marked checker that returns a check, called
// at once (anyOf(a, b)(ctx, kaid)), names one variable in the condition
// and in its check call, so the two stay linked.
func TestCheckReturningACheck(t *testing.T) {
	const checkers = `package r

//ka:permission-check
func anyOf(checks ...func(ctx context.Context, kaid string) bool) func(ctx context.Context, kaid string) bool {
	return nil
}
`
	src := `package r
func Resolve(ctx context.Context, kaid string) (*T, error) {
	allowed := anyOf(isActor, isCoach)(ctx, kaid)
	if !allowed {
		return nil, nil
	}
	return nil, nil
}
`
	sources := sourcesOf(t, map[string]string{checkersFile: checkers, resolverFile: src})
	known, _ := knownChecks(sources, nil, &Inputs{})
	for i := range sources {
		if sources[i].name != resolverFile {
			continue
		}
		sc := fileScanner(&sources[i], known)
		for _, g := range sc.rules(funcDecls(sources[i].file)[0]) {
			cond := conditionOf(sources[i].fset, &g)
			calls := known.checkCalls(sources[i].fset, &g)
			if len(calls) == 0 || !strings.Contains(cond, `"var":"`+calls[0].Var+`"`) {
				t.Errorf("condition %s and check calls %+v name different variables", cond, calls)
			}
		}
	}
}

// TestAllowingPaths: a checker's allowing paths, read from its body: each
// disjunct of a final returned expression; each "if C { return true }"
// under the negation of the denials before it; an error-only checker's
// "return nil". A body the scan can't read whole is not analyzable, with
// the reason.
// allowingPathsSrc holds a checker of each shape TestAllowingPaths reads.
const allowingPathsSrc = `package r

func returned(ctx context.Context, kaid string) bool {
	scope := acl.UserScope(kaid)
	return acl.IsCurrentUser(ctx, kaid) || acl.ActorHasPermission(ctx, capabilities.CanView, scope)
}

func sequence(ctx context.Context, kaid string) bool {
	if ctx.RequestIsServiceAdminQuery() {
		return true
	}
	if kaid == "" {
		return false
	}
	if acl.IsCurrentUser(ctx, kaid) {
		return true
	}
	return false
}

func errOnly(ctx context.Context) error {
	if acl.IsLoggedIn(ctx, acl.PhantomProhibited) {
		return nil
	}
	return errors.Unauthorized("no")
}

func delegating(ctx context.Context) error {
	if ctx.RequestIsServiceAdminQuery() {
		return nil
	}
	return validate(ctx)
}

func invalid(ctx context.Context, name string) error {
	if name == "" {
		return errors.InvalidInput("empty")
	}
	return fmt.Errorf("never")
}

func valued(ctx context.Context) (string, error) {
	if !acl.IsLoggedIn(ctx) {
		return "", errors.Unauthorized()
	}
	return "ok", nil
}

func filter(ctx context.Context) ([]string, error) {
	return nil, nil
}

func filterWithBool(ctx context.Context) ([]string, bool, error) {
	if ctx.RequestIsServiceAdminQuery() {
		return nil, true, nil
	}
	return nil, false, nil
}

func withheld(ctx context.Context, kaid string) (*bool, error) {
	if !acl.IsCurrentUser(ctx, kaid) {
		return nil, nil
	}
	v := true
	return &v, nil
}

func isSelfOrAdmin(ctx context.Context, kaid string) (isSelf bool, err error) {
	isSelf = acl.IsCurrentUser(ctx, kaid)
	if !isSelf && !ctx.RequestIsServiceAdminQuery() {
		return false, errors.Unauthorized()
	}
	return isSelf, nil
}

func boolDenies(ctx context.Context, kaid string) (bool, error) {
	if ctx.RequestIsServiceAdminQuery() {
		return true, nil
	}
	if kaid == "" {
		return false, errors.Unauthorized()
	}
	return false, nil
}

func looping(ctx context.Context, kaids []string) bool {
	for _, k := range kaids {
		if acl.IsCurrentUser(ctx, k) {
			return true
		}
	}
	return false
}
`

func TestAllowingPaths(t *testing.T) {
	sources := sourcesOf(t, map[string]string{resolverFile: allowingPathsSrc})
	known, _ := knownChecks(sources, nil, &Inputs{})
	sc := fileScanner(&sources[0], known)
	got := map[string][]string{}
	reasons := map[string]string{}
	for _, fn := range funcDecls(sources[0].file) {
		paths, reason := sc.allowingPaths(fn)
		reasons[fn.Name.Name] = reason
		for _, p := range paths {
			got[fn.Name.Name] = append(got[fn.Name.Name], p.Condition)
		}
	}
	want := map[string][]string{
		"returned": {
			`{"var":"acl.IsCurrentUser","op":"equal","value":true}`,
			`{"var":"caller.hasPermission.CanView","op":"equal","value":true}`,
		},
		"sequence": {
			isServiceAdminCond,
			`{"all":[{"not":` + isServiceAdminCond + `},` +
				`{"not":{"var":"kaid","op":"equal","value":""}},` +
				`{"var":"acl.IsCurrentUser","op":"equal","value":true}]}`,
		},
		"errOnly": {`{"var":"caller.loggedInNonPhantom","op":"equal","value":true}`},
		"valued":  {`{"var":"caller.loggedIn","op":"equal","value":true}`},
		"delegating": {
			isServiceAdminCond,
			`{"all":[{"not":` + isServiceAdminCond + `},` +
				`{"var":"validate.fails","op":"equal","value":false}]}`,
		},
		// return nil, nil from a pointer result withholds the value: a deny.
		"withheld": {`{"var":"acl.IsCurrentUser","op":"equal","value":true}`},
		// The error decides; the bool is data its callers read.
		"isSelfOrAdmin": {
			`{"not":{"all":[{"not":{"var":"acl.IsCurrentUser","op":"equal","value":true}},` +
				`{"not":` + isServiceAdminCond + `}]}}`,
		},
		// false beside a nil error denies by the bool: the bool decides.
		"boolDenies": {isServiceAdminCond},
	}
	for name, paths := range want {
		if strings.Join(got[name], "\n") != strings.Join(paths, "\n") || reasons[name] != "" {
			t.Errorf("%s: paths\n%s\nreason %q; want\n%s", name,
				strings.Join(got[name], "\n"), reasons[name], strings.Join(paths, "\n"))
		}
	}
	if len(got["invalid"]) != 0 || reasons["invalid"] != "" {
		t.Errorf("invalid: paths %v, reason %q; want none: every return builds an error",
			got["invalid"], reasons["invalid"])
	}
	const isFilter = "filter" // the reason names a filter
	for name, reason := range map[string]string{
		"looping": "loop", "filter": isFilter, "filterWithBool": isFilter,
	} {
		if len(got[name]) != 0 || !strings.Contains(reasons[name], reason) {
			t.Errorf(
				"%s: paths %v, reason %q; want none, %q",
				name,
				got[name],
				reasons[name],
				reason,
			)
		}
	}
}

// answersSrc holds a resolver of each shape TestAnswers reads.
const answersSrc = `package r

func direct(ctx context.Context, kaid string) (bool, error) {
	return acl.IsCurrentUser(ctx, kaid), nil
}

func bound(ctx context.Context, kaid string) (*bool, error) {
	ok := acl.IsCurrentUser(ctx, kaid)
	return &ok, nil
}

func gate(ctx context.Context, kaid string) (string, error) {
	if !acl.IsCurrentUser(ctx, kaid) {
		return "", nil
	}
	return kaid, nil
}

func errorOnly(ctx context.Context) error {
	return acl.IsLoggedInErr(ctx)
}

//ka:permission-check
func login(ctx context.Context, token string) (string, error) {
	return token, nil
}

func whole(ctx context.Context, token string) (string, error) {
	return login(ctx, token)
}

func boundErr(ctx context.Context, token string) (*string, error) {
	kaid, err := login(ctx, token)
	if err != nil {
		return nil, err
	}
	return &kaid, nil
}

func ignoredErr(ctx context.Context, token string) (*string, error) {
	kaid, _ := login(ctx, token)
	return &kaid, nil
}

func literal(ctx context.Context, kaid string) (func() bool, error) {
	return func() bool { return acl.IsCurrentUser(ctx, kaid) }, nil
}
`

// TestAnswers: a check whose result a resolver returns as its value,
// directly or through a top-level local, is an answer; one in a
// condition, the trailing error or a function literal is not, nor one
// whose error reaches the caller (returned whole, or bound and branched
// on): that error gates.
func TestAnswers(t *testing.T) {
	sources := sourcesOf(t, map[string]string{resolverFile: answersSrc})
	known, _ := knownChecks(sources, nil, &Inputs{})
	got := map[string][]string{}
	for _, fn := range funcDecls(sources[0].file) {
		got[fn.Name.Name] = known.answers(fn)
	}
	want := map[string][]string{
		"direct": {"acl.IsCurrentUser"}, "bound": {"acl.IsCurrentUser"},
		"gate": nil, "errorOnly": nil, "literal": nil,
		"whole": nil, "boundErr": nil, "ignoredErr": {"login"}, "login": nil,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("answers = %v, want %v", got, want)
	}
}

// TestCallerLocals: a local bound once, at the top level, from the request
// user is the caller; one assigned again, or bound in a nested block, is
// not.
func TestCallerLocals(t *testing.T) {
	sources := sourcesOf(t, map[string]string{resolverFile: `package r

func top(ctx context.Context) error {
	actor, err := ctx.RequestUser()
	if err != nil {
		return err
	}
	return nil
}

func reassigned(ctx context.Context) error {
	actor, err := ctx.RequestUser()
	actor, err = other()
	return err
}

func nested(ctx context.Context) error {
	if true {
		actor, _ := ctx.RequestUser()
		_ = actor
	}
	return nil
}
`})
	got := map[string][]string{}
	for _, fn := range funcDecls(sources[0].file) {
		got[fn.Name.Name] = callerLocals(fn)
	}
	want := map[string][]string{"top": {"actor"}, "reassigned": nil, "nested": nil}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("callerLocals = %v, want %v", got, want)
	}
}

// allowingReturnSrc is a bool field resolver that fails open: it returns
// true on its filter's error and on an empty result, then decides per
// item.
const allowingReturnSrc = `package r

//ka:permission-check
func visibleUDIs(ctx context.Context, kaid string) ([]string, error) {
	return nil, nil
}

func canSetOwnBirthday(ctx context.Context, kaid string) (bool, error) {
	acl.OpenAccess()
	udis, err := visibleUDIs(ctx, kaid)
	if err != nil {
		return true, nil
	}
	if len(udis) == 0 {
		return true, nil
	}
	return false, nil
}

func hasTakenExam(ctx context.Context, kaid string) (bool, error) {
	udis, err := visibleUDIs(ctx, kaid)
	if err != nil {
		return false, nil
	}
	return len(udis) > 0, nil
}
`

// TestAllowingReturnIsNoGuard: a branch that returns true from a bool
// resolver allows, so it is no access rule, whatever it tests; the
// resolver's open marker remains its rule.
func TestAllowingReturnIsNoGuard(t *testing.T) {
	sources := sourcesOf(t, map[string]string{resolverFile: allowingReturnSrc})
	known, _ := knownChecks(sources, nil, &Inputs{})
	sc := fileScanner(&sources[0], known)
	for _, fn := range funcDecls(sources[0].file) {
		if fn.Name.Name != "canSetOwnBirthday" {
			continue
		}
		for _, g := range sc.rules(fn) {
			if g.kind == ruleAccess {
				t.Errorf("canSetOwnBirthday: access rule at line %d, want none", g.start)
			}
		}
	}
}

// TestRequestUserFailureRejectsServiceAdmin: returning RequestUser()'s
// error rejects every service-admin request (it has no request user), so
// that branch is a guard on the service-admin check; the guard after it
// stays.
func TestRequestUserFailureRejectsServiceAdmin(t *testing.T) {
	const tools = `package r

func tools(ctx context.Context) ([]string, error) {
	user, err := ctx.RequestUser()
	if %s {
		return nil, err
	}
	kaid := user.Kaid
	if !acl.IsCurrentUser(ctx, kaid) {
		return []string{}, nil
	}
	return nil, nil
}
`
	notSelf := `{"not":{"var":"acl.IsCurrentUser","op":"equal","value":true}}`
	tests := []struct {
		name, cond string
		want       []string
	}{
		{"error", "err != nil", []string{isServiceAdminCond, notSelf}},
		{
			"error or no user", "err != nil || user == nil",
			[]string{`{"any":[` + isServiceAdminCond + `,{"not":{"exists":"user"}}]}`, notSelf},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sources := sourcesOf(t, map[string]string{resolverFile: fmt.Sprintf(tools, tt.cond)})
			known, _ := knownChecks(sources, nil, &Inputs{})
			sc := fileScanner(&sources[0], known)
			var conds []string
			for _, fn := range funcDecls(sources[0].file) {
				for _, g := range sc.rules(fn) {
					conds = append(conds, conditionOf(sc.fset, &g))
				}
			}
			if !reflect.DeepEqual(conds, tt.want) {
				t.Errorf("conditions = %q, want %q", conds, tt.want)
			}
		})
	}
}
