package policy_test

import (
	"strings"
	"testing"

	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/graph"
	"github.com/StevenACoffman/codetospec/internal/policy"
)

// unknownTerm is a name no registry declares.
const unknownTerm = "ghost"

// argKaid binds a user to the request's kaid argument.
const argKaid = "arg:kaid"

// checkScopeItem is the scope_of_check of a guard that drops one item.
const checkScopeItem = "item"

// settingsCap is a capability fact the vocabulary tests join on.
func settingsCap() []extract.Fact {
	return []extract.Fact{
		fact(
			extract.KindCapability,
			"capability.1",
			at("caps.go", "1-1"),
			map[string]string{
				extract.AttrName:  "CanViewUserSettingsData",
				extract.AttrValue: "view_settings",
			},
		),
	}
}

// bioCall is a call of canSeeBioFields(ctx, kaid, capabilities.X), its
// user bound to the kaid argument.
const bioCall = `[{"check":"canSeeBioFields","var":"canSeeBioFields","args":[{"text":"ktx"},` +
	`{"text":"kaid","from":"arg:kaid"},{"text":"capabilities.CanViewUserSettingsData"}]}]`

// bioGuard guards Query.bio on canSeeBioFields.
func bioGuard() graph.Node {
	return guardRule("rule.u.bio", "Query.bio",
		`{"not":{"var":"canSeeBioFields","op":"equal","value":true}}`, bioCall)
}

// TestCompoundSubjectArg: a subject_arg on a compound checker passes to
// every part about a user that names none; a part about a user left
// without one is named in the error.
func TestCompoundSubjectArg(t *testing.T) {
	cs, err := policy.ParseCheckers([]byte(`
canSeeBioFields:
  subject_arg: 1
  any:
    - {means: service_admin}
    - {means: self}
    - {means: coach}
`))
	if err != nil {
		t.Fatal(err)
	}
	m := policy.BuildModel(nil, []graph.Node{bioGuard()}, cs)
	if len(m.Operations) != 1 || m.Operations[0].Subject != argKaid {
		t.Fatalf("model = %+v, want Query.bio bound to arg:kaid", m)
	}
	_, err = policy.ParseCheckers([]byte("x: {any: [{means: service_admin}, {means: self}]}\n"))
	if err == nil || !strings.Contains(err.Error(), "part 2") {
		t.Errorf("err = %v, want one naming part 2", err)
	}
}

// TestCapabilityFromArgument: a part may take its capability from the
// call's argument (capability_arg), as a leaf takes the call's own.
func TestCapabilityFromArgument(t *testing.T) {
	cs, err := policy.ParseCheckers([]byte(`
canSeeBioFields:
  subject_arg: 1
  any:
    - {means: self}
    - {means: capability, capability_arg: 2, scope: user}
`))
	if err != nil {
		t.Fatal(err)
	}
	m := policy.BuildModel(settingsCap(), []graph.Node{bioGuard()}, cs)
	if len(m.Operations) != 1 {
		t.Fatalf("model = %+v, want Query.bio", m)
	}
	leaves := m.Operations[0].Leaves()
	if len(leaves) != 2 || leaves[1].Capability != "view_settings" {
		t.Errorf("leaves = %+v, want the argument's capability by its stored name", leaves)
	}
	if _, parseErr := policy.ParseCheckers(
		[]byte("x: {means: capability, capability: C, capability_arg: 2, scope: any}\n"),
	); parseErr == nil {
		t.Error("a capability named twice parsed")
	}
}

// TestItemFilterAboutItsUser: in an item filter, self and coach are about
// the item's own user: they need no subject_arg, and bind no operation.
func TestItemFilterAboutItsUser(t *testing.T) {
	cs, err := policy.ParseCheckers([]byte(`
permissions.GetAllVisibleUDIs:
  item: UserDistrictInfo
  any:
    - {means: service_admin}
    - {means: self}
    - {means: coach}
`))
	if err != nil {
		t.Fatal(err)
	}
	n := guardRule("rule.d.udis", "User.udis", "", `[{"check":"permissions.GetAllVisibleUDIs"}]`)
	n.Extra[graph.ExtraCheckScope] = checkScopeItem
	m := policy.BuildModel(nil, []graph.Node{n}, cs)
	if len(m.Items) != 1 {
		t.Fatalf("model = %+v, want one item policy", m)
	}
	for _, l := range m.Items[0].Leaves() {
		if (l.Kind == policy.LeafSelf || l.Kind == policy.LeafCoach) &&
			l.SubjectFrom != policy.SubjectItem {
			t.Errorf("leaf %+v: want its user from the item", l)
		}
	}
	if len(m.Operations) != 1 || m.Operations[0].Subject != "" {
		t.Errorf("operations = %+v, want User.udis bound to no user", m.Operations)
	}
}

// TestUnpinnableEntries: a checkers entry may state why a check can't be
// pinned; its guards are skipped with that reason, not as a check outside
// the vocabulary. Unpinnable excludes any meaning.
func TestUnpinnableEntries(t *testing.T) {
	cs, err := policy.ParseCheckers([]byte(
		"canSeeBioFields: {unpinnable: cross_service, note: decided by another service}\n"))
	if err != nil {
		t.Fatal(err)
	}
	m := policy.BuildModel(nil, []graph.Node{bioGuard()}, cs)
	if len(m.Skipped) != 1 ||
		m.Skipped[0].Reason != "not pinnable (cross_service): canSeeBioFields" {
		t.Errorf("skipped = %+v, want the stated reason", m.Skipped)
	}
	if _, parseErr := policy.ParseCheckers(
		[]byte("x: {unpinnable: flag, means: login}\n"),
	); parseErr == nil {
		t.Error("an unpinnable entry with a meaning parsed")
	}
}

// TestMergeCheckers: checkers files merge into one; a name two files
// define is an error naming both files.
func TestMergeCheckers(t *testing.T) {
	shared := policy.Checkers{"acl.Shared": {Means: policy.MeansLogin}}
	own := policy.Checkers{"local": {Means: policy.MeansServiceAdmin}}
	merged, err := policy.MergeCheckers(
		map[string]policy.Checkers{"shared.yaml": shared, "own.yaml": own},
	)
	if err != nil || len(merged) != 2 {
		t.Fatalf("merged = %v, %v; want both entries", merged, err)
	}
	_, err = policy.MergeCheckers(map[string]policy.Checkers{"a.yaml": own, "b.yaml": own})
	if err == nil || !strings.Contains(err.Error(), "a.yaml") ||
		!strings.Contains(err.Error(), "b.yaml") {
		t.Errorf("err = %v, want one naming a.yaml and b.yaml", err)
	}
}

// TestStalePins: a pin whose recorded digest differs from its checker's
// current one is stale; a pin with no digest, or whose checker's digest
// is unknown, never is.
func TestStalePins(t *testing.T) {
	const changed = "changed"
	cs := policy.Checkers{
		"same":    {Means: policy.MeansLogin, Digest: "d1"},
		changed:   {Means: policy.MeansLogin, Digest: "d1"},
		"none":    {Means: policy.MeansLogin},
		"unknown": {Means: policy.MeansLogin, Digest: "d1"},
	}
	got := policy.StalePins(cs, map[string]string{"same": "d1", changed: "d2", "none": "d3"})
	if strings.Join(got, ",") != changed {
		t.Errorf("stale = %v, want [changed]", got)
	}
}

// TestAttributesNegationScopes: a checker may test a boolean attribute of
// the caller, the resource or the request, negate any checker, pin the
// locale and explicit scopes, and name a relation to an argument's user.
func TestAttributesNegationScopes(t *testing.T) {
	cs, err := policy.ParseCheckers([]byte(`
canSeeBioFields:
  all:
    - {means: attribute, of: caller, attribute: internal_user}
    - not: {means: attribute, of: resource, attribute: deleted}
    - {means: capability, capability: CanViewUserSettingsData, scope: locale}
    - {means: capability, capability: CanViewUserSettingsData, scope: explicit}
    - {means: relation, relation: managing_coach_of, subject_arg: 1}
`))
	if err != nil {
		t.Fatal(err)
	}
	m := policy.BuildModel(settingsCap(), []graph.Node{bioGuard()}, cs)
	if len(m.Operations) != 1 {
		t.Fatalf("model = %+v, want Query.bio", m)
	}
	op := &m.Operations[0]
	if op.Subject != argKaid {
		t.Errorf("subject = %q, want the relation's user, arg:kaid", op.Subject)
	}
	kinds := map[string]bool{}
	for _, l := range op.Leaves() {
		kinds[l.Kind+":"+l.Holder+l.Scope] = true
	}
	for _, want := range []string{
		"attribute:caller", "attribute:resource", "capability:locale",
		"capability:explicit", "relation:",
	} {
		if !kinds[want] {
			t.Errorf("leaves %v lack %s", kinds, want)
		}
	}
	// The negated part is the negation: a case where deleted holds never
	// lets canSeeBioFields hold.
	for _, c := range op.Cases() {
		if c.Given["canSeeBioFields#2#1"] && !c.Denied {
			t.Errorf("case %+v allowed with the resource deleted", c.Given)
		}
	}
	for _, bad := range []string{
		"x: {means: attribute, of: planet, attribute: a}",
		"x: {means: attribute, of: caller}",
		"x: {not: {means: login}, means: login}",
	} {
		if _, parseErr := policy.ParseCheckers([]byte(bad)); parseErr == nil {
			t.Errorf("ParseCheckers(%q) parsed", bad)
		}
	}
}

// TestRegistryPhrases: a registry entry may give its plain phrase per
// language and its concept; JSON and YAML read alike; an unknown key
// stops with its path; a phrase in another language falls back to
// English, and a term without one to its name.
func TestRegistryPhrases(t *testing.T) {
	reg, err := policy.ParseRegistry([]byte(`{
  "teacher_of": {"from": ["models.Classroom.AllTeacherKaids"], "concept": "Classroom",
    "phrase": {"en": "teaches the class", "fr": "enseigne dans la classe"}},
  "internal_user": {"from": ["models.User.Email"]}
}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ term, lang, want string }{
		{teacherOf, "en", "teaches the class"},
		{teacherOf, "fr", "enseigne dans la classe"},
		{teacherOf, "de", "teaches the class"},
		{"internal_user", "en", "internal_user"},
		{unknownTerm, "en", unknownTerm},
	} {
		if got := reg.Phrase(tc.term, tc.lang); got != tc.want {
			t.Errorf("Phrase(%s, %s) = %q, want %q", tc.term, tc.lang, got, tc.want)
		}
	}
	if got := reg.Concept(teacherOf); got != classroom {
		t.Errorf("concept = %q, want Classroom", got)
	}
	if _, err = policy.ParseRegistry(
		[]byte(`{"x": {"from": ["a.B"], "phrases": {}}}`),
	); err == nil ||
		!strings.Contains(err.Error(), "phrases") {
		t.Errorf("err = %v, want one naming phrases", err)
	}
	cs := policy.Checkers{"c": {All: []policy.Checker{
		{Means: policy.MeansRelation, Relation: teacherOf},
		{Means: policy.MeansAttribute, Of: policy.HolderCaller, Attribute: "deleted_user"},
	}}}
	if got := cs.Undeclared(reg); strings.Join(got, ",") != "deleted_user" {
		t.Errorf("undeclared = %v, want [deleted_user]", got)
	}
}
