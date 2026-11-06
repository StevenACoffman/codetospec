package policy_test

import (
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/graph"
	"github.com/StevenACoffman/codetospec/internal/policy"
)

const canModerate = "can_moderate_users"

const checkersYAML = `
actorHasGlobalPermission: {means: capability, scope: global}
permissions.CanViewCDI: {means: relation, relation: teacher_of}
`

// TestParseCheckers: a checkers file pins what a check means; an entry
// with an unknown meaning, a capability without a pinned scope or a
// relation without its name stops with the entry named.
func TestParseCheckers(t *testing.T) {
	got, err := policy.ParseCheckers([]byte(checkersYAML))
	if err != nil {
		t.Fatal(err)
	}
	want := policy.Checkers{
		"actorHasGlobalPermission": {Means: policy.MeansCapability, Scope: policy.ScopeGlobal},
		"permissions.CanViewCDI":   {Means: policy.MeansRelation, Relation: teacherOf},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("checkers = %+v, want %+v", got, want)
	}
	for _, bad := range []string{
		"x: {means: magic}",
		"x: {means: capability}",
		"x: {means: capability, scope: galaxy}",
		"x: {means: relation}",
		"x: {means: request}",
	} {
		if _, parseErr := policy.ParseCheckers([]byte(bad)); parseErr == nil ||
			!strings.Contains(parseErr.Error(), `"x"`) {
			t.Errorf("ParseCheckers(%q) = %v, want an error naming x", bad, parseErr)
		}
	}
}

// TestCheckersUndeclared: a relation no registry entry declares is
// reported; the loader couldn't serve it.
func TestCheckersUndeclared(t *testing.T) {
	cs, err := policy.ParseCheckers(
		[]byte(checkersYAML + "y: {means: relation, relation: ghost}\n"),
	)
	if err != nil {
		t.Fatal(err)
	}
	reg, err := policy.ParseRegistry([]byte("teacher_of: {from: [models.C.Teachers]}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := cs.Undeclared(reg); !reflect.DeepEqual(got, []string{"ghost"}) {
		t.Errorf("undeclared = %v, want [ghost]", got)
	}
}

// TestModelPinnedCheckers: a check the extractor gave no meaning takes the
// checkers file's; a check whose variable holds on its failure (a bound
// error) is the negation of the check; its cases give the check's truth.
func TestModelPinnedCheckers(t *testing.T) {
	cs, err := policy.ParseCheckers([]byte(checkersYAML))
	if err != nil {
		t.Fatal(err)
	}
	facts := []extract.Fact{
		fact(extract.KindCapability, "capability.1", at("caps.go", "1-1"), map[string]string{
			extract.AttrName: "CanModerateUsers", extract.AttrValue: canModerate,
		}),
	}
	rules := []graph.Node{
		guardRule(
			"rule.a.wrapper",
			"Query.ipInfo",
			`{"not":{"var":"actorHasGlobalPermission","op":"equal","value":true}}`,
			`[{"check":"actorHasGlobalPermission","var":"actorHasGlobalPermission","capability":"CanModerateUsers"}]`,
		),
		guardRule(
			"rule.a.cdi",
			"Query.cdi",
			`{"var":"permissions.CanViewCDI.fails","op":"equal","value":true}`,
			`[{"check":"permissions.CanViewCDI","var":"permissions.CanViewCDI.fails","fails":true}]`,
		),
	}
	m := policy.BuildModel(facts, rules, cs)
	if len(m.Skipped) != 0 || len(m.Operations) != 2 {
		t.Fatalf("model = %+v, want both operations", m)
	}
	cdi, wrapper := m.Operations[0], m.Operations[1]
	wantCDI := policy.Expr{
		Op: policy.OpNot,
		Args: []policy.Expr{{Op: policy.OpCheck, Check: &policy.Leaf{
			Kind:     policy.LeafRelation,
			Var:      "permissions.CanViewCDI.fails",
			Relation: teacherOf,
			Fails:    true,
		}}},
	}
	if got := cdi.Denies[0].When; !reflect.DeepEqual(got, wantCDI) {
		t.Errorf("Query.cdi deny = %+v, want %+v", got, wantCDI)
	}
	wantWrapper := &policy.Leaf{
		Kind: policy.LeafCapability, Var: "actorHasGlobalPermission",
		Capability: canModerate, Scope: policy.ScopeGlobal,
	}
	if got := wrapper.Denies[0].When.Args[0].Check; !reflect.DeepEqual(got, wantWrapper) {
		t.Errorf("Query.ipInfo leaf = %+v, want %+v", got, wantWrapper)
	}
	// Given is the check's truth: the teacher passes, the others are denied.
	for _, c := range cdi.Cases() {
		if teacher := c.Given["permissions.CanViewCDI.fails"]; c.Denied == teacher {
			t.Errorf("case %+v: denied = %v for teacher = %v", c.Given, c.Denied, teacher)
		}
	}
}

// TestModelPush: a Pub/Sub push validation is the push principal's check.
func TestModelPush(t *testing.T) {
	m := policy.BuildModel(nil, []graph.Node{
		guardRule(
			"rule.h.push",
			"POST /pubsub/x",
			`{"var":"pubsub.DecodeAndValidatePushMsg.fails","op":"equal","value":true}`,
			`[{"check":"pubsub.DecodeAndValidatePushMsg","var":"pubsub.DecodeAndValidatePushMsg.fails",`+
				`"fails":true,"means":"push"}]`,
		),
	}, nil)
	if len(m.Operations) != 1 ||
		m.Operations[0].Denies[0].When.Args[0].Check.Kind != policy.LeafPush {
		t.Errorf("model = %+v, want one push deny", m)
	}
}

// TestModelAliases: an operation whose every extractor rule delegates to
// one exported operation is that operation's alias, checked as it; one
// delegating to an operation left out is left out too.
func TestModelAliases(t *testing.T) {
	alias := func(id, op, target string) graph.Node {
		n := guardRule(id, op, "", "")
		n.Extra[graph.ExtraChecks] = ""
		n.Extra[graph.ExtraDelegatesTo] = "r." + target
		n.Extra[graph.ExtraDelegatesToOperation] = target
		return n
	}
	m := policy.BuildModel(nil, []graph.Node{
		loginGuard("rule.a.target", "Query.target", "1-3", "acl.IsLoggedIn"),
		alias("rule.a.alias", "Query.alias", "Query.target"),
		guardRule(
			"rule.a.opaque",
			"Query.opaque",
			`{"not":{"var":"customCheck","op":"equal","value":true}}`,
			`[{"check":"customCheck","var":"customCheck"}]`,
		),
		alias("rule.a.lost", "Query.lost", "Query.opaque"),
	}, nil)
	if len(m.Operations) != 1 ||
		!reflect.DeepEqual(m.Operations[0].Aliases, []string{"Query.alias"}) {
		t.Errorf("operations = %+v, want Query.target aliased by Query.alias", m.Operations)
	}
	want := []policy.Skip{
		{RuleID: "rule.a.lost", Reason: "delegation to Query.opaque, which is not exported"},
		{RuleID: "rule.a.opaque", Reason: "check outside the vocabulary: customCheck"},
	}
	if !reflect.DeepEqual(m.Skipped, want) {
		t.Errorf("skipped = %+v, want %+v", m.Skipped, want)
	}
}

// compoundYAML pins districts' ActorCanViewUserDistrictData as its code
// reads: a service-admin request, a grant of CanViewUserDistrictData on
// userKind "all", or CanActLikeADistrictAdmin on any scope.
const compoundYAML = `
permissions.ActorCanViewUserDistrictData:
  any:
    - {means: service_admin}
    - {means: capability, capability: CanViewUserDistrictData, scope: user_all}
    - {means: capability, capability: CanActLikeADistrictAdmin, scope: any}
`

// TestParseCompoundCheckers: a checker may be any or all of checkers; a
// capability inside one names its capability, and a checker states one
// shape only.
func TestParseCompoundCheckers(t *testing.T) {
	cs, err := policy.ParseCheckers([]byte(compoundYAML))
	if err != nil {
		t.Fatal(err)
	}
	if got := len(cs["permissions.ActorCanViewUserDistrictData"].Any); got != 3 {
		t.Errorf("any has %d checkers, want 3", got)
	}
	for _, bad := range []string{
		"x: {any: []}",
		"x: {any: [{means: capability, scope: any}]}",
		"x: {means: login, any: [{means: login}]}",
		"x: {all: [{means: magic}]}",
	} {
		if _, parseErr := policy.ParseCheckers([]byte(bad)); parseErr == nil ||
			!strings.Contains(parseErr.Error(), `"x"`) {
			t.Errorf("ParseCheckers(%q) = %v, want an error naming x", bad, parseErr)
		}
	}
}

// TestModelCompoundChecker: a compound checker becomes its expression,
// each part a check of its own; the cases decide the checker's variable
// from its parts, so the oracle still judges the translation.
func TestModelCompoundChecker(t *testing.T) {
	cs, err := policy.ParseCheckers([]byte(compoundYAML))
	if err != nil {
		t.Fatal(err)
	}
	var facts []extract.Fact
	for i, c := range [][2]string{
		{"CanViewUserDistrictData", "view_udd"}, {"CanActLikeADistrictAdmin", "act_like_admin"},
	} {
		facts = append(facts, fact(
			extract.KindCapability,
			"capability."+string(rune('1'+i)),
			at(
				"caps.go",
				"1-1",
			),
			map[string]string{extract.AttrName: c[0], extract.AttrValue: c[1]},
		))
	}
	const check = "permissions.ActorCanViewUserDistrictData"
	m := policy.BuildModel(facts, []graph.Node{guardRule("rule.u.authz-udi", "Query.udi",
		`{"not":{"var":"`+check+`","op":"equal","value":true}}`,
		`[{"check":"`+check+`","var":"`+check+`"}]`)}, cs)
	if len(m.Operations) != 1 {
		t.Fatalf("model = %+v, want Query.udi", m)
	}
	op := &m.Operations[0]
	leaves := op.Leaves()
	if len(leaves) != 3 || leaves[1].Scope != policy.ScopeUserAll ||
		leaves[2].Capability != "act_like_admin" {
		t.Fatalf("leaves = %+v, want the three parts", leaves)
	}
	cases := op.Cases()
	if len(cases) != 8 {
		t.Fatalf("%d cases, want 8", len(cases))
	}
	for _, c := range cases {
		anyHolds := false
		for _, l := range leaves {
			anyHolds = anyHolds || c.Given[l.Var]
		}
		if c.Denied == anyHolds {
			t.Errorf("case %+v: denied = %v", c.Given, c.Denied)
		}
	}
}

// subjectGuard guards op with a self check on subject, bound to from.
func subjectGuard(id, op, subject, from string) graph.Node {
	return guardRule(id, op, `{"not":{"var":"acl.IsCurrentUser","op":"equal","value":true}}`,
		`[{"check":"acl.IsCurrentUser","var":"acl.IsCurrentUser","means":"self",`+
			`"subject":"`+subject+`","subject_from":"`+from+`"}]`)
}

// TestModelSubjectBinding: an operation's checks about one user carry where
// that user comes from (Operation.Subject, the PEP's binding); one whose
// user is a local, or whose checks are about two users, is left out: the
// one resource.subject can't stand for it.
func TestModelSubjectBinding(t *testing.T) {
	m := policy.BuildModel(nil, []graph.Node{
		subjectGuard("rule.a.bound", "User.emails", "obj.Kaid", "parent:Kaid"),
		subjectGuard("rule.a.local", "Query.local", "actorKaid", ""),
		subjectGuard("rule.a.two1", "Query.two", "kaid", "arg:kaid"),
		subjectGuard("rule.a.two2", "Query.two", "other", "arg:other"),
	}, nil)
	if len(m.Operations) != 1 || m.Operations[0].Subject != "parent:Kaid" {
		t.Errorf("operations = %+v, want User.emails bound to parent:Kaid", m.Operations)
	}
	want := []policy.Skip{
		{RuleID: "rule.a.local", Reason: "a check is about actorKaid, not a request value"},
		{RuleID: "rule.a.two1", Reason: "checks about different users: arg:kaid, arg:other"},
		{RuleID: "rule.a.two2", Reason: "checks about different users: arg:kaid, arg:other"},
	}
	if !reflect.DeepEqual(m.Skipped, want) {
		t.Errorf("skipped = %+v, want %+v", m.Skipped, want)
	}
}

// TestModelPinnedSubject: a pinned checker names which of the call's
// arguments its user is (subject_arg), and so binds it.
func TestModelPinnedSubject(t *testing.T) {
	cs, err := policy.ParseCheckers([]byte(
		"actorHasUserScopedPermission: {means: capability, scope: user, subject_arg: 3}\n"))
	if err != nil {
		t.Fatal(err)
	}
	facts := []extract.Fact{
		fact(
			extract.KindCapability,
			"capability.1",
			at("caps.go", "1-1"),
			map[string]string{
				extract.AttrName:  "CanModerateUsers",
				extract.AttrValue: canModerate,
			},
		),
	}
	m := policy.BuildModel(facts, []graph.Node{guardRule("rule.a.scoped", "Query.notes",
		`{"not":{"var":"actorHasUserScopedPermission","op":"equal","value":true}}`,
		`[{"check":"actorHasUserScopedPermission","var":"actorHasUserScopedPermission",`+
			`"capability":"CanModerateUsers","args":[{"text":"ctx"},{"text":"iam"},`+
			`{"text":"capabilities.CanModerateUsers"},{"text":"kaid","from":"arg:kaid"}]}]`)}, cs)
	if len(m.Operations) != 1 || m.Operations[0].Subject != "arg:kaid" {
		t.Errorf("model = %+v, want Query.notes bound to arg:kaid", m)
	}
	if _, parseErr := policy.ParseCheckers(
		[]byte("x: {means: login, subject_arg: 1}\n"),
	); parseErr == nil {
		t.Error("a subject_arg on a check about no user parsed")
	}
}

// itemYAML pins districts' VisibleAssignmentDistrictInfos as a filter over
// AssignmentDistrictInfo items, as its code reads: all of them for a
// service-admin request or a global grant, a KMAP district's for an NWEA
// grant.
const itemYAML = `
permissions.VisibleAssignmentDistrictInfos:
  item: AssignmentDistrictInfo
  any:
    - {means: service_admin}
    - {means: capability, capability: CanViewUserAssignments, scope: global}
    - all:
        - {means: capability, capability: CanViewUserAssignments, scope: user_nwea}
        - {means: relation, relation: in_kmap_district}
`

// TestModelItemPolicy: an item guard whose filter is pinned leaves its
// operation's call decision whole and becomes the view policy of the
// filtered kind, denied when the item isn't visible; the oracle decides
// visibility from the pinned parts. An unpinned filter still keeps its
// operation out.
func TestModelItemPolicy(t *testing.T) {
	cs, err := policy.ParseCheckers([]byte(itemYAML))
	if err != nil {
		t.Fatal(err)
	}
	facts := []extract.Fact{
		fact(
			extract.KindCapability,
			"capability.1",
			at("caps.go", "1-1"),
			map[string]string{
				extract.AttrName:  "CanViewUserAssignments",
				extract.AttrValue: "view_assignments",
			},
		),
	}
	item := func(id, op, check string) graph.Node {
		n := guardRule(id, op, "", `[{"check":"`+check+`"}]`)
		n.Extra[graph.ExtraCheckScope] = "item"
		return n
	}
	m := policy.BuildModel(facts, []graph.Node{
		item("rule.a.list", "Query.adis", "permissions.VisibleAssignmentDistrictInfos"),
		loginGuard("rule.a.login", "Query.adis", "1-3", "acl.IsLoggedIn"),
		item("rule.a.other", "Query.other", "permissions.Unpinned"),
	}, cs)
	if len(m.Operations) != 1 || m.Operations[0].Action != "Query.adis" ||
		len(m.Operations[0].Denies) != 1 {
		t.Errorf("operations = %+v, want Query.adis with its login deny", m.Operations)
	}
	if len(m.Items) != 1 {
		t.Fatalf("items = %+v, want one", m.Items)
	}
	view := m.Items[0]
	if view.Kind != "AssignmentDistrictInfo" || view.Action != policy.ViewAction ||
		!reflect.DeepEqual(view.Filters, []string{"Query.adis"}) {
		t.Errorf("item policy = %+v", view)
	}
	cases := view.Cases()
	if len(cases) != 16 {
		t.Fatalf("%d cases, want 16", len(cases))
	}
	for _, c := range cases {
		g := func(i int) bool { return c.Given["permissions.VisibleAssignmentDistrictInfos#"+strconv.Itoa(i)] }
		visible := g(1) || g(2) || (c.Given["permissions.VisibleAssignmentDistrictInfos#3#1"] &&
			c.Given["permissions.VisibleAssignmentDistrictInfos#3#2"])
		if c.Denied == visible {
			t.Errorf("case %+v: denied = %v", c.Given, c.Denied)
		}
	}
	want := []policy.Skip{{RuleID: "rule.a.other", Reason: "drops one item, not the call"}}
	if !reflect.DeepEqual(m.Skipped, want) {
		t.Errorf("skipped = %+v, want %+v", m.Skipped, want)
	}
	if _, parseErr := policy.ParseCheckers(
		[]byte("x: {item: K, means: self, subject_arg: 1}\n"),
	); parseErr == nil {
		t.Error("an item checker about a user parsed")
	}
}
