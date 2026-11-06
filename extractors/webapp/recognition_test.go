package main

import (
	"go/ast"
	"strings"
	"testing"
	"testing/fstest"
)

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
