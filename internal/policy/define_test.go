package policy_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/locale"
	"github.com/StevenACoffman/codetospec/internal/policy"
)

// Paths as the webapp extractor writes them (extract.AttrPaths).
const (
	adminOrSelfPaths = `[` +
		`{"condition":"{\"var\":\"request.isServiceAdmin\",\"op\":\"equal\",\"value\":true}","lines":"11-11",` +
		`"checks":[{"check":"ctx.RequestIsServiceAdminQuery","var":"request.isServiceAdmin","means":"service_admin"}]},` +
		`{"condition":"{\"all\":[{\"not\":{\"var\":\"request.isServiceAdmin\",\"op\":\"equal\",\"value\":true}},` +
		`{\"var\":\"acl.IsCurrentUser\",\"op\":\"equal\",\"value\":true}]}","lines":"14-14",` +
		`"checks":[{"check":"ctx.RequestIsServiceAdminQuery","var":"request.isServiceAdmin","means":"service_admin"},` +
		`{"check":"acl.IsCurrentUser","var":"acl.IsCurrentUser","means":"self"}]}]`
	teacherPath = `[{"condition":"{\"all\":[{\"var\":\"caller.loggedIn\",\"op\":\"equal\",\"value\":true},` +
		`{\"var\":\"classroom.isTeacher\",\"op\":\"equal\",\"value\":true}]}","lines":"12-12",` +
		`"checks":[{"check":"acl.IsLoggedIn","var":"caller.loggedIn","means":"login"}]}]`
	delegatingPath = `[{"condition":"{\"var\":\"permissions.AdminOrSelf.fails\",\"op\":\"equal\",\"value\":false}",` +
		`"lines":"13-13","checks":[{"check":"permissions.AdminOrSelf","var":"permissions.AdminOrSelf.fails","fails":true}]}]`
	managedPaths = `[` +
		`{"condition":"{\"var\":\"caller.hasPermission.CanView\",\"op\":\"equal\",\"value\":true}","lines":"71-71",` +
		`"checks":[{"check":"acl.ActorHasPermission","var":"caller.hasPermission.CanView","means":"capability","capability":"CanView"},` +
		`{"check":"acl.UserScope","var":"acl.UserScope"}]},` +
		`{"condition":"{\"all\":[{\"not\":{\"var\":\"acl.ActorHasPermissionErr.fails\",\"op\":\"equal\",\"value\":true}},` +
		`{\"not\":{\"var\":\"caller.hasPermission.CanView\",\"op\":\"equal\",\"value\":true}},` +
		`{\"var\":\"isManagedByActor\",\"op\":\"equal\",\"value\":true}]}","lines":"73-73",` +
		`"checks":[{"check":"acl.ActorHasPermissionErr","var":"acl.ActorHasPermissionErr.fails","fails":true,"means":"capability","capability":"CanView"},` +
		`{"check":"acl.ActorHasPermissionErr","var":"caller.hasPermission.CanView","means":"capability","capability":"CanView"},` +
		`{"check":"isManagedByActor","var":"isManagedByActor"}]}]`
	eitherPath = `[{"condition":"{\"any\":[{\"var\":\"caller.loggedIn\",\"op\":\"equal\",\"value\":true},` +
		`{\"var\":\"request.isServiceAdmin\",\"op\":\"equal\",\"value\":true}]}","lines":"12-12",` +
		`"checks":[{"check":"acl.IsLoggedIn","var":"caller.loggedIn","means":"login"},` +
		`{"check":"ctx.RequestIsServiceAdminQuery","var":"request.isServiceAdmin","means":"service_admin"}]}]`
)

// Check names of the definition tests.
const (
	adminOrSelf = "permissions.AdminOrSelf"
	teacher     = "permissions.Teacher"
	delegating  = "permissions.Delegating"
	either      = "permissions.Either"
	looping     = "permissions.Looping"
	cross       = "permissions.Cross"
	managed     = "permissions.Managed"
	// partData is a coverage part needing only a condition on data.
	partData = "data"
)

// definitionFacts: one access_check fact per checker of the tests, each
// with the paths the extractor read (or why it couldn't), and the field
// the teacher check reads.
func definitionFacts() []extract.Fact {
	check := func(name, lines string, attrs map[string]string) extract.Fact {
		attrs[extract.AttrName] = name
		attrs[extract.AttrForm] = formMarked
		return fact(
			extract.KindAccessCheck,
			"access_check.webapp."+name,
			at(checkFile, lines),
			attrs,
		)
	}
	return []extract.Fact{
		check(adminOrSelf, "10-15", map[string]string{extract.AttrPaths: adminOrSelfPaths}),
		check(teacher, "20-25", map[string]string{extract.AttrPaths: teacherPath}),
		check(delegating, "30-35", map[string]string{extract.AttrPaths: delegatingPath}),
		check(either, "40-45", map[string]string{extract.AttrPaths: eitherPath}),
		check(looping, "50-55", map[string]string{
			extract.AttrNotAnalyzable:       "true",
			extract.AttrNotAnalyzableReason: "a loop, switch or select decides",
		}),
		check(cross, "60-65", map[string]string{}),
		check(managed, "70-75", map[string]string{extract.AttrPaths: managedPaths}),
		fact(extract.KindReads, "reads.1", at(checkFile, "21-21"),
			map[string]string{extract.AttrField: studentsOf}),
	}
}

func definitionPins(t *testing.T) policy.Checkers {
	t.Helper()
	cs, err := policy.ParseCheckers([]byte(`{
		"permissions.AdminOrSelf": {"subject_arg": 1, "any": [{"means": "service_admin"}, {"means": "self"}],
			"digest": "old"},
		"permissions.Teacher": {"all": [{"means": "login"}, {"means": "relation", "relation": "teacher_of"},
			{"means": "capability", "capability": "capabilities.CanView", "scope": "global"}]},
		"permissions.Delegating": {"subject_arg": 1, "any": [{"means": "service_admin"}, {"means": "self"},
			{"means": "relation", "relation": "ghost"}]},
		"permissions.Either": {"any": [{"means": "login"}, {"means": "service_admin"}], "approx": true},
		"permissions.Looping": {"means": "login"},
		"permissions.Cross": {"unpinnable": "cross_service"},
		"permissions.Managed": {"subject_arg": 1, "any": [
			{"means": "capability", "capability": "CanView", "scope": "user"},
			{"means": "relation", "relation": "manages"}]}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	return cs
}

func definitionRegistry(t *testing.T) policy.Registry {
	t.Helper()
	reg, err := policy.ParseRegistry([]byte(`{"teacher_of": {"from": ["` + studentsOf + `"],
		"phrase": {"en": "teaches the student"}, "concept": "Classroom"}}`))
	if err != nil {
		t.Fatal(err)
	}
	return reg
}

// definedInventory builds the inventory of definitionFacts, with Digest
// set as the CLI sets it, and defines its checks.
func definedInventory(t *testing.T) policy.Inventory {
	t.Helper()
	reg := definitionRegistry(t)
	inv := policy.Build(definitionFacts(), nil, reg)
	for i := range inv.Checks {
		inv.Checks[i].Digest = "new"
	}
	inv.Define(definitionPins(t), reg, locale.For("en"))
	return inv
}

// definitionsOf is each check's definition in definedInventory, by name.
func definitionsOf(t *testing.T) map[string]policy.Definition {
	t.Helper()
	inv := definedInventory(t)
	out := map[string]policy.Definition{}
	for i := range inv.Checks {
		out[inv.Checks[i].Name] = inv.Checks[i].Definition
	}
	return out
}

// TestDefineCovered: a pin whose alternatives are the code's paths is
// covered, a negated earlier allowance aside; its leaves are grounded in
// the paths' calls; a digest other than the code's makes it stale.
func TestDefineCovered(t *testing.T) {
	d := definitionsOf(t)[adminOrSelf]
	if !d.Pinned || d.Pin == nil || !d.Stale || d.Approx || d.Unpinnable {
		t.Errorf("flags = %+v", d)
	}
	want := &policy.Coverage{Covered: true, Parts: []string{"service_admin", "self"}}
	if !reflect.DeepEqual(d.Coverage, want) {
		t.Errorf("Coverage = %+v, want %+v", d.Coverage, want)
	}
	wantLeaves := []policy.PinLeaf{
		{Means: policy.MeansServiceAdmin, Grounding: policy.GroundingGrounded},
		{Means: policy.MeansSelf, Grounding: policy.GroundingGrounded},
	}
	if !reflect.DeepEqual(d.Leaves, wantLeaves) {
		t.Errorf("Leaves = %+v, want %+v", d.Leaves, wantLeaves)
	}
}

// TestDefineRelationAndCapability: a relation leaf is grounded by the
// field its registry entry declares and gives its concept; a capability
// the code never names is ungrounded and its part matches no path.
func TestDefineRelationAndCapability(t *testing.T) {
	d := definitionsOf(t)[teacher]
	wantLeaves := []policy.PinLeaf{
		{Means: policy.MeansLogin, Grounding: policy.GroundingGrounded},
		{Means: policy.MeansRelation, Operand: teacherOf, Grounding: policy.GroundingGrounded},
		{Means: policy.MeansCapability, Operand: "CanView", Grounding: policy.GroundingUngrounded},
	}
	if !reflect.DeepEqual(d.Leaves, wantLeaves) {
		t.Errorf("Leaves = %+v, want %+v", d.Leaves, wantLeaves)
	}
	if len(d.Concepts) != 1 || d.Concepts[0] != classroom || d.GlossaryMissing != nil {
		t.Errorf("Concepts %v, GlossaryMissing %v", d.Concepts, d.GlossaryMissing)
	}
	want := &policy.Coverage{
		Parts:          []string{"login ∧ capability CanView ∧ data"},
		UncoveredPaths: []int{0},
		UnmatchedParts: []int{0},
	}
	if !reflect.DeepEqual(d.Coverage, want) {
		t.Errorf("Coverage = %+v, want %+v", d.Coverage, want)
	}
}

// TestDefineDelegating: a path that passes another pinned checker takes
// that checker's alternatives; the relation the registry lacks is listed
// and its part matches no path.
func TestDefineDelegating(t *testing.T) {
	d := definitionsOf(t)[delegating]
	want := &policy.Coverage{
		Parts:          []string{"service_admin", "self", partData},
		UnmatchedParts: []int{2},
	}
	if !reflect.DeepEqual(d.Coverage, want) {
		t.Errorf("Coverage = %+v, want %+v", d.Coverage, want)
	}
	if len(d.GlossaryMissing) != 1 || d.GlossaryMissing[0] != unknownTerm {
		t.Errorf("GlossaryMissing = %v", d.GlossaryMissing)
	}
}

// TestDefineUnreadable: a path whose checks sit under an any matches
// nothing (never a false match); a check whose code isn't analyzable gets
// no coverage; an unpinnable entry is flagged, with its reason.
func TestDefineUnreadable(t *testing.T) {
	ds := definitionsOf(t)
	if d := ds[either]; d.Coverage == nil || len(d.Coverage.UncoveredPaths) != 1 || !d.Approx {
		t.Errorf("either: %+v", d)
	}
	if d := ds[looping]; !d.NotAnalyzable || d.Coverage != nil || !d.Pinned {
		t.Errorf("looping: %+v", d)
	}
	if d := ds[cross]; !d.Unpinnable || d.UnpinnableReason != "cross_service" || d.Pinned {
		t.Errorf("cross: %+v", d)
	}
}

// TestDefinitionsMarkdownGolden pins how the markdown shows definitions
// and a second reader's comparison.
// Regenerate with UPDATE_GOLDEN=1 go test ./internal/policy and review.
func TestDefinitionsMarkdownGolden(t *testing.T) {
	inv := definedInventory(t)
	inv.Compare(definitionPins(t), secondPins(t))
	checkGolden(t, "testdata/definitions.md", inv.Markdown())
}

// TestInventorySchema: the JSON policies writes, defined or not, matches
// inventory.schema.json; a key the schema lacks fails.
func TestInventorySchema(t *testing.T) {
	for name, inv := range map[string]policy.Inventory{
		"district": policy.Build(districtFacts(), districtRules(), districtRegistry(t)),
		"defined":  definedInventory(t),
		"compared": func() policy.Inventory {
			inv := definedInventory(t)
			inv.Compare(definitionPins(t), secondPins(t))
			return inv
		}(),
	} {
		data, err := json.Marshal(&inv)
		if err != nil {
			t.Fatal(err)
		}
		if err = policy.ValidateAgainst(policy.SchemaInventory, data); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	if err := policy.ValidateAgainst(policy.SchemaInventory,
		[]byte(`{"endpoints": [], "checks": [], "depth": 3, "extra": 1}`)); err == nil {
		t.Error("a key the schema lacks passed")
	}
}

// TestRegistryMethods: a method a check calls is an input a registry
// entry declares by "Type.Method()", so a relation built from it grounds;
// an undeclared one is listed as such.
func TestRegistryMethods(t *testing.T) {
	const hasTeacher, other = "models.Classroom.HasTeacher", "models.Classroom.Other"
	reg, err := policy.ParseRegistry([]byte(`{"teaches": {"from": ["` + hasTeacher + `()"]}}`))
	if err != nil {
		t.Fatal(err)
	}
	facts := []extract.Fact{
		fact(extract.KindAccessCheck, "access_check.webapp."+teacher, at(checkFile, "20-25"),
			map[string]string{extract.AttrName: teacher, extract.AttrForm: formMarked}),
		fact(extract.KindCallsMethod, "m.1", at(checkFile, "21-21"),
			map[string]string{extract.AttrCalledMethod: hasTeacher}),
		fact(extract.KindCallsMethod, "m.2", at(checkFile, "22-22"),
			map[string]string{extract.AttrCalledMethod: other}),
	}
	pins, err := policy.ParseCheckers(
		[]byte(`{"` + teacher + `": {"means": "relation", "relation": "teaches"}}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	inv := policy.Build(facts, nil, reg)
	inv.Define(pins, reg, locale.For("en"))
	want := []policy.Input{
		{Kind: policy.KindMethod, Name: hasTeacher, DeclaredBy: "teaches"},
		{Kind: policy.KindMethod, Name: other},
	}
	c := inv.Checks[0]
	if !reflect.DeepEqual(c.Inputs, want) {
		t.Errorf("inputs = %+v, want %+v", c.Inputs, want)
	}
	if g := c.Definition.Leaves[0].Grounding; g != policy.GroundingGrounded {
		t.Errorf("relation grounding = %s, want grounded", g)
	}
	if !reflect.DeepEqual(inv.Undeclared, []string{other}) {
		t.Errorf("undeclared = %v, want %v", inv.Undeclared, []string{other})
	}
}

// TestDefineOperandsAndHelpers: a scope matcher among a path's calls is
// the capability call's operand, not a term; so is a call's failure whose
// decision has its own variable (a system error, not the decision); a
// helper nothing pins is a condition on data, which matches a relation
// part.
func TestDefineOperandsAndHelpers(t *testing.T) {
	d := definitionsOf(t)[managed]
	want := &policy.Coverage{Covered: true, Parts: []string{"capability CanView", partData}}
	if !reflect.DeepEqual(d.Coverage, want) {
		t.Errorf("Coverage = %+v, want %+v", d.Coverage, want)
	}
}

// TestDefineInlinesCalledChecks: a path's call of a check with no meaning
// and no pin, whose own paths the inventory has, reads as those paths, so
// a pin stated in the callee's leaves covers it.
func TestDefineInlinesCalledChecks(t *testing.T) {
	const outer, inner = "permissions.Outer", "_helper"
	selfPath := `[{"condition":"{\"var\":\"acl.IsCurrentUser\",\"op\":\"equal\",\"value\":true}","lines":"11-11",` +
		`"checks":[{"check":"acl.IsCurrentUser","var":"acl.IsCurrentUser","means":"self"}]}]`
	callPath := `[{"condition":"{\"var\":\"_helper\",\"op\":\"equal\",\"value\":true}","lines":"21-21",` +
		`"checks":[{"check":"_helper","var":"_helper"}]}]`
	facts := []extract.Fact{
		fact(
			extract.KindAccessCheck,
			"access_check.webapp."+inner,
			at(checkFile, "10-15"),
			map[string]string{
				extract.AttrName:  inner,
				extract.AttrForm:  "call",
				extract.AttrPaths: selfPath,
			},
		),
		fact(
			extract.KindAccessCheck,
			"access_check.webapp."+outer,
			at(checkFile, "20-25"),
			map[string]string{
				extract.AttrName:  outer,
				extract.AttrForm:  formMarked,
				extract.AttrPaths: callPath,
			},
		),
	}
	pins, err := policy.ParseCheckers(
		[]byte(`{"` + outer + `": {"means": "self", "subject_arg": 1}}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	inv := policy.Build(facts, nil, policy.Registry{})
	inv.Define(pins, policy.Registry{}, locale.For("en"))
	for _, c := range inv.Checks {
		if c.Name == outer && (c.Definition.Coverage == nil || !c.Definition.Coverage.Covered) {
			t.Errorf(
				"%s coverage = %+v, want covered through %s",
				outer,
				c.Definition.Coverage,
				inner,
			)
		}
	}
}
