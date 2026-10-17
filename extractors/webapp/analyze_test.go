package main

import (
	"bytes"
	"encoding/json"
	"go/ast"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
)

// testGOROOT() stands in for a Go installation: two net/http status codes.
func testGOROOT() fstest.MapFS {
	return fstest.MapFS{httpStatusFile: {Data: []byte(`package http

const (
	StatusOK         = 200 // RFC 9110, 15.3.1
	StatusBadRequest = 400 // RFC 9110, 15.5.1
)
`)}}
}

// testInputs analyzes testdata/service against a small supergraph.
func testInputs(lang string) *Inputs {
	return &Inputs{
		Service:      os.DirFS(filepath.Join("testdata", "service")),
		GOROOT:       testGOROOT(),
		Supergraph:   os.DirFS(filepath.Join("testdata", "supergraph")),
		Webapp:       os.DirFS(filepath.Join("testdata", "webapp")),
		ServiceGraph: "USERS",
		Lang:         lang,
		AccessPkgs:   []string{"permissions"},
	}
}

// TestAnalyzeGolden pins the facts derived from testdata/service, in both
// languages. Regenerate with UPDATE_GOLDEN=1 go test ./... and review.
func TestAnalyzeGolden(t *testing.T) {
	for _, lang := range []string{"fr", "en"} {
		t.Run(lang, func(t *testing.T) {
			facts, _, err := Analyze(testInputs(lang))
			if err != nil {
				t.Fatalf("Analyze: %v", err)
			}
			got, err := json.MarshalIndent(facts, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			got = append(got, '\n')
			golden := filepath.Join("testdata", "golden."+lang+".json")
			if os.Getenv("UPDATE_GOLDEN") == "1" {
				if writeErr := os.WriteFile(golden, got, 0o600); writeErr != nil {
					t.Fatal(writeErr)
				}
				return
			}
			want, err := fs.ReadFile(os.DirFS("testdata"), "golden."+lang+".json")
			if err != nil {
				t.Fatalf("missing golden (run UPDATE_GOLDEN=1 go test ./...): %v", err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("facts differ from %s:\n%s", golden, got)
			}
		})
	}
}

// TestGatewayGolden pins the facts derived from testdata/gateway, a
// service with net/http handlers and no gqlgen schema (graphql-gateway).
// Regenerate with UPDATE_GOLDEN=1 go test ./... and review.
func TestGatewayGolden(t *testing.T) {
	in := &Inputs{
		Service: os.DirFS(filepath.Join("testdata", "gateway")),
		GOROOT:  testGOROOT(),
		Webapp:  os.DirFS(filepath.Join("testdata", "webapp")),
		Lang:    "en",
	}
	facts, _, err := Analyze(in)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	got, err := json.MarshalIndent(facts, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	golden := filepath.Join("testdata", "golden.gateway.en.json")
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if writeErr := os.WriteFile(golden, got, 0o600); writeErr != nil {
			t.Fatal(writeErr)
		}
		return
	}
	want, err := fs.ReadFile(os.DirFS("testdata"), "golden.gateway.en.json")
	if err != nil {
		t.Fatalf("missing golden (run UPDATE_GOLDEN=1 go test ./...): %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("facts differ from %s:\n%s", golden, got)
	}
}

func TestAnalyzeWarnings(t *testing.T) {
	_, warnings, err := Analyze(testInputs("en"))
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	joined := strings.Join(warnings, "\n")
	cases := map[string]string{
		"unparsable file is skipped":      "skip resolvers/broken.go",
		"field-less resolver is reported": "Query.Orphan matches no schema field",
		"kind without its struct":         "kind Orphan: struct Missing not found",
	}
	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			if !strings.Contains(joined, want) {
				t.Fatalf("warnings lack %q:\n%s", want, joined)
			}
		})
	}
}

func TestAnalyzeFacts(t *testing.T) {
	facts, _, err := Analyze(testInputs("en"))
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	byDomain := map[string][]string{} // domain → rule slugs
	domainFacts := 0
	for _, f := range facts {
		switch f.Kind {
		case kindRule:
			byDomain[f.Attrs[attrDomain]] = append(byDomain[f.Attrs[attrDomain]], f.Attrs[attrSlug])
		case kindDomain:
			domainFacts++
		}
		if f.Attrs[attrSlug] == "" && f.Kind == kindRule {
			t.Errorf("rule %s has no slug", f.ID)
		}
	}
	cases := map[string]struct {
		domain string
		want   int
	}{
		// addAuthEmail, addCoachRole, enableTotp, 2 rate limits of signup,
		// merge (per item), roster (3 deep), 2 of optOut (then and else),
		// cooldown (error code), rename (delegated denial), login (checked by)
		"auth_emails.graphql operations": {"auth-emails", 12},
		// badges (accumulated), bio (2, nested) and partner (nested), checkUsername,
		// deviceAuthRequestInfo, drafts and notes (marked checkers), emails,
		// grants (bound condition), recoveryCodes, 2 guards each of nickname
		// and pendingEmailVerifications
		// …plus avatar (else if), peek (loop), search (switch default), and
		// the visibility of notes (filtered by VisibleNotes), tools (a
		// self-check that never denies), coachees (a filter from pkg/), legacy
		// (acl.TODO: unchecked)
		// …and the entity finders of User (@key): one checks, its alias
		// delegates to it
		"user.graphql operations": {"user", 24},
		// Orphan, and _unused: a private method with no caller
		"field-less resolver": {"resolvers", 2},
		// AIProfile.summary (its resolver type is named by the factory), and
		// the finders of RowByTeacher (open) and AIProfile (acl.TODO), and
		// blockEmail's check, in its private helper _blockEmail, and
		// devadminAddAuthEmail, checked by the target it rebuilds a receiver for
		"federation.graphql operations": {"federation", 5},
		// serverTime, from schema.graphql: its resolver file's domain
		"default schema file": {"guards", 1},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := len(byDomain[tc.domain]); got != tc.want {
				t.Fatalf(
					"domain %s has %d rules %v, want %d",
					tc.domain,
					got,
					byDomain[tc.domain],
					tc.want,
				)
			}
		})
	}
	// 16 resolver methods with a schema field (not helpers, not Orphan), plus
	// the whole of kaid.go, whose methods share one domain.
	// …and 3 helpers whose callers share a domain (_canEditDrafts, _isActor,
	// formatKaid): not helper (no caller) nor _audit (two domains).
	// …and validateRename, called by rename only.
	// …and AIProfile.summary, 6 entity finders (2 without a rule: an alias
	// of an unchecked one), blockEmail and its helper _blockEmail,
	// devadminAddAuthEmail (federation.go spans two domains: no whole-file
	// fact).
	if domainFacts != 45 {
		t.Errorf("got %d domain facts, want 45", domainFacts)
	}
	wholeFile := map[string]bool{}
	for _, f := range facts {
		if f.Kind == kindDomain && strings.HasPrefix(f.Source.Lines, "1-") {
			wholeFile[f.Source.Path] = true
		}
	}
	if !wholeFile["resolvers/kaid.go"] || wholeFile["resolvers/guards.go"] {
		t.Errorf(
			"whole-file domain facts = %v, want kaid.go only (guards.go spans two domains)",
			wholeFile,
		)
	}
}

// TestFinderOf: an entity finder names the longest @key type that fits.
func TestFinderOf(t *testing.T) {
	const users = "user.graphql"
	idx := schemaIndex{
		entityType: {
			{name: "Row", file: "row.graphql"},
			{name: "RowByTeacher", file: "teacher.graphql"},
			{name: "User", file: users},
		},
		// keyed in a shared schema, extended here
		"Activity": {{name: "persona", file: "activity.graphql"}},
	}
	cases := map[string]string{
		"FindManyUserByKaids":                         users,
		"FindUserByKaid":                              users,
		"FindManyActivityByContentIDAndKaLocales":     "activity.graphql",
		"FindManyRowByIDs":                            "row.graphql",
		"FindManyRowByTeacherByTeacherIDAndKaLocales": "teacher.graphql",
		"FindManyMissingByIDs":                        "",
		"Users":                                       "",
	}
	for method, want := range cases {
		t.Run(method, func(t *testing.T) {
			if got := idx.lookup(entityType, method).file; got != want {
				t.Fatalf("lookup(%s) in %q, want %q", method, got, want)
			}
		})
	}
}

// TestCatchAllSchemas: a schema file names no domain only when it holds
// most methods and spreads over many resolver files.
func TestCatchAllSchemas(t *testing.T) {
	// layout builds one resolver file per method; schemaOf[i] declares
	// method i's field.
	layout := func(schemaOf ...string) (schemaIndex, []*source, [][]resolverMethod) {
		idx := schemaIndex{}
		files := make([]*source, len(schemaOf))
		methods := make([][]resolverMethod, len(schemaOf))
		for i, schemaFile := range schemaOf {
			name := "M" + strings.Repeat("x", i)
			idx["Query"] = append(idx["Query"], schemaField{name: name, file: schemaFile})
			files[i] = &source{name: "resolvers/" + name + ".go"}
			methods[i] = []resolverMethod{{typeName: "Query", method: name}}
		}
		return idx, files, methods
	}
	repeat := func(file string, n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = file
		}
		return out
	}
	cases := map[string]struct {
		schemaOf []string
		want     bool // misc.graphql is a catch-all
	}{
		"dominant and spread": {
			append(repeat("misc.graphql", 10), repeat("user.graphql", 2)...), true,
		},
		"dominant in few files": {append(repeat("misc.graphql", 5), "user.graphql"), false},
		"gqlgen's default file beside topical ones": {
			[]string{"schema.graphql", "user.graphql", "user.graphql"}, true,
		},
		"spread but not dominant": {
			append(repeat("misc.graphql", 10), repeat("user.graphql", 10)...), false,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			idx, files, methods := layout(tc.schemaOf...)
			catchAll := catchAllSchemas(idx, files, methods)
			if got := catchAll["misc.graphql"] || catchAll["schema.graphql"]; got != tc.want {
				t.Fatalf("catch-all = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestAccessPredicates: only the exported bool-first functions of an
// access package are checks.
func TestAccessPredicates(t *testing.T) {
	names, err := goSources(testInputs("en").Service)
	if err != nil {
		t.Fatal(err)
	}
	sources, _ := parseSources(testInputs("en").Service, names)
	got := accessPredicates(sources, []string{"permissions"})
	cases := map[string]bool{
		"permissions.UserCanViewEmails": true,
		"permissions.LoadVisibleEmails": false, // a loader
		"permissions.canSee":            false, // unexported
		"resolvers.helper":              false, // not an access package
	}
	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			if got[name] != want {
				t.Fatalf("predicate %s = %v, want %v", name, got[name], want)
			}
		})
	}
}

// TestHelperDomains: a helper takes the domain its callers agree on.
func TestHelperDomains(t *testing.T) {
	facts, _, err := Analyze(testInputs("en"))
	if err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile(filepath.Join("testdata", "service", "resolvers", "guards.go"))
	if err != nil {
		t.Fatal(err)
	}
	lineOf := func(prefix string) int {
		for i, line := range strings.Split(string(src), "\n") {
			if strings.HasPrefix(line, prefix) {
				return i + 1
			}
		}
		t.Fatalf("no line starts with %q", prefix)
		return 0
	}
	domainAt := func(line int) string {
		for _, f := range facts {
			if f.Kind == kindDomain && f.Source.Path == "resolvers/guards.go" &&
				strings.HasPrefix(f.Source.Lines, strconv.Itoa(line)+"-") {
				return f.Attrs[attrDomain]
			}
		}
		return ""
	}
	cases := map[string]struct{ decl, want string }{
		"callers in one domain":  {"func _canEditDrafts(", "user"},
		"callers in two domains": {"func _audit(", ""},
		"no caller":              {"func helper(", ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := domainAt(lineOf(tc.decl)); got != tc.want {
				t.Fatalf("domain of %s = %q, want %q", tc.decl, got, tc.want)
			}
		})
	}
}

// TestSharedCheckers: the checkers webapp's pkg/ marks are known only when
// the webapp checkout is given; the service's own markings win.
func TestSharedCheckers(t *testing.T) {
	cases := map[string]struct {
		webapp bool
		want   bool // a visibility rule for User.coachees
	}{
		"webapp given":  {true, true},
		"service alone": {false, false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			in := testInputs("en")
			if !tc.webapp {
				in.Webapp = nil
			}
			facts, _, err := Analyze(in)
			if err != nil {
				t.Fatal(err)
			}
			got := false
			for _, f := range facts {
				got = got || f.Kind == kindRule && f.Attrs[attrSlug] == "visibility-user-coachees"
			}
			if got != tc.want {
				t.Fatalf("visibility rule for coachees = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestResolverTypes: a generated result interface names the GraphQL type,
// not the factory's method name; a local one, the method's name; a method
// with parameters is no factory; a Go type serving several types resolves,
// per method, the one whose schema has the field.
func TestResolverTypes(t *testing.T) {
	src := `package resolvers

func (r *Resolver) Mutation() graphql.MutationResolver { return &mutationResolver{r} }
func (r *Resolver) InvalidateCache() graphql.MutationResolver { return &mutationResolver{r} }
func (r *Resolver) AIGuidePrompt() graphql.AIGuidePromptResolver { return &promptResolver{r} }
func (r *Resolver) Context(ctx context.Context) *serviceContext { return &serviceContext{} }
func (r *Resolver) Helper() *serviceContext { return &serviceContext{} }
func (r *Resolver) Entity() graphql.EntityResolver { return &entityResolver{r} }
func (r *Resolver) StudentData() studentDataResolver { return &entityResolver{r} }
`
	s, err := parseGo(fstest.MapFS{"r.go": {Data: []byte(src)}}, "r.go")
	if err != nil {
		t.Fatal(err)
	}
	const studentData = "StudentData" // a federated type entityResolver also serves
	schema := schemaIndex{
		studentData: {{name: "region", file: "s.graphql"}},
		entityType:  {{name: studentData, file: "s.graphql"}},
	}
	idx := resolverTypes([]*ast.File{s.file}, schema)
	cases := []struct{ recv, method, want string }{
		{"mutationResolver", "AddCoach", "Mutation"},
		{"promptResolver", "Versions", "AIGuidePrompt"},
		{"entityResolver", "Region", studentData},
		{"entityResolver", "FindManyStudentDataByIDs", entityType},
		{"queryResolver", "Users", "Query"}, // no factory: its name
		{"serviceContext", "Repository", ""},
	}
	for _, tc := range cases {
		got, _ := idx.typeOf(tc.recv, tc.method)
		if got != tc.want {
			t.Errorf("typeOf(%s, %s) = %q, want %q", tc.recv, tc.method, got, tc.want)
		}
	}
}

// TestAnalyzeAll: a checkout's Go services are analyzed as Analyze does
// one, consolidated relative to the checkout: each citation under
// services/<svc>/ or pkg/, ids unique, a service's own rules all there, and
// a fact two services share kept once with both names.
func TestAnalyzeAll(t *testing.T) {
	const gateway, users = "gateway", "users"
	webapp := os.DirFS(filepath.Join("testdata", "allwebapp"))
	services, err := goServices(webapp)
	if err != nil || !slices.Equal(services, []string{gateway, users}) {
		t.Fatalf("goServices = %v, %v; want the two Go services", services, err)
	}
	facts, _, err := AnalyzeAll(&AllInputs{Webapp: webapp, GOROOT: testGOROOT(), Lang: "en"})
	if err != nil {
		t.Fatal(err)
	}
	ids := make(map[string]bool)
	rules := map[string]int{} // service → own rules
	for _, f := range facts {
		if ids[f.ID] {
			t.Errorf("duplicate id %s", f.ID)
		}
		ids[f.ID] = true
		if !relativeToCheckout(f.Source) {
			t.Errorf("%s cites %+v, not relative to the checkout", f.ID, f.Source)
		}
		if f.Kind == kindRule && strings.HasPrefix(f.Source.Path, servicesDir+"/") {
			rules[f.Attrs[attrInServices]]++
		}
		if f.ID == "constant.webapp.http.StatusOK" && f.Attrs[attrInServices] != gateway+","+users {
			t.Errorf("a shared constant lists %q", f.Attrs[attrInServices])
		}
	}
	for svc, dir := range map[string]string{users: "service", gateway: "gateway"} {
		if want := ownRules(t, dir, svc); rules[svc] != want {
			t.Errorf("%s: %d consolidated rules, want its %d", svc, rules[svc], want)
		}
	}
}

// relativeToCheckout reports whether a citation is relative to the
// checkout root: none, under services/, or under pkg/, never rooted.
func relativeToCheckout(ref Ref) bool {
	return ref.Root == "" && (ref.Path == "" ||
		strings.HasPrefix(ref.Path, servicesDir+"/") || strings.HasPrefix(ref.Path, sharedDir+"/"))
}

// ownRules counts the rules Analyze derives from testdata/<dir>, as service
// svc, that cite the service itself.
func ownRules(t *testing.T, dir, svc string) int {
	t.Helper()
	single, _, err := Analyze(&Inputs{
		Service:      os.DirFS(filepath.Join("testdata", dir)),
		GOROOT:       testGOROOT(),
		Webapp:       os.DirFS(filepath.Join("testdata", "webapp")),
		Supergraph:   os.DirFS(filepath.Join("testdata", "supergraph")),
		ServiceGraph: strings.ToUpper(svc),
		Lang:         "en",
	})
	if err != nil {
		t.Fatal(err)
	}
	own := 0
	for _, f := range single {
		if f.Kind == kindRule && f.Source.Root == "" {
			own++
		}
	}
	return own
}

// TestInheritFacts: the pkg spec's LLM rules about code the service
// reaches — a function, a method through its type — are inherited, cited in
// pkg/ under webappRoot with the spec's verdict; unreached code, a rule
// citing two files and the spec's own extractor rules are not.
func TestInheritFacts(t *testing.T) {
	in := testInputs("en")
	in.Inherit = os.DirFS(filepath.Join("testdata", "pkgspec"))
	facts, warnings, err := Analyze(in)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Fact{}
	for _, f := range facts {
		if f.Attrs[attrInheritedFrom] != "" {
			got[f.ID] = f
		}
	}
	want := []string{
		"rule.inherited.pkg.web-gqlclient.send-needs-headers",
		"rule.inherited.pkg.web-serve.missing-secret",
	}
	if ids := slices.Sorted(maps.Keys(got)); !slices.Equal(ids, want) {
		t.Fatalf("inherited %v, want %v", ids, want)
	}
	f := got[want[1]]
	if f.Source != (Ref{Path: "pkg/web/serve/graphql.go", Lines: "15-18", Root: webappRoot}) ||
		f.Attrs[attrCrosscheck] != "supported" || f.Attrs[attrDomain] != "shared-web-serve" {
		t.Errorf("inherited rule = %+v", f)
	}
	if !slices.ContainsFunc(
		warnings,
		func(w string) bool { return strings.Contains(w, "1 rules citing several files") },
	) {
		t.Errorf("no warning for the rule citing two files: %v", warnings)
	}
}
