package policy_test

import (
	"bytes"
	"os"
	"reflect"
	"testing"

	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/graph"
	"github.com/StevenACoffman/codetospec/internal/policy"
)

const (
	checkFile  = "permissions/permissions.go"
	modelFile  = "models/udi.go"
	canView    = "permissions.CanViewCDI"
	studentsOf = "models.UserDistrictInfo.StudentOfCDIKeys"
	kaid       = "kaid"
	extractor  = "webapp"
	teacherOf  = "teacher_of"
	goldenFile = "testdata/inventory.md"
	formMarked = "marked"
	// isCurrentUser, endpointCDI and classroom are names the tests share.
	isCurrentUser = "acl.IsCurrentUser"
	endpointCDI   = "endpoint.query-cdi"
	classroom     = "Classroom"
)

func at(path, lines string) extract.Ref { return extract.Ref{Path: path, Lines: lines} }

func fact(kind, id string, src extract.Ref, attrs map[string]string) extract.Fact {
	return extract.Fact{Kind: kind, ID: id, Source: src, Attrs: attrs}
}

// districtFacts is what the extractors say of one access check: its
// definition reads a field itself and another through the method it calls,
// names a capability, a flag and a role constant (the value_kind facts say
// which types are which), and queries the UDIs by kaid; one resolver is
// unguarded.
func districtFacts() []extract.Fact {
	def := at(checkFile, "10-20")
	return []extract.Fact{
		fact(extract.KindAccessCheck, "access_check.webapp."+canView, def,
			map[string]string{extract.AttrName: canView, extract.AttrForm: formMarked}),
		fact(extract.KindAccessCheck, "access_check.webapp.acl.IsCurrentUser", extract.Ref{},
			map[string]string{extract.AttrName: isCurrentUser, extract.AttrForm: formMarked}),
		fact(extract.KindCall, "call.1", def,
			map[string]string{extract.AttrCalleePath: modelFile, extract.AttrCalleeLines: "5-7"}),
		fact(extract.KindReads, "reads.1", def,
			map[string]string{extract.AttrField: "models.ClassroomDistrictInfo.AllTeacherUDIsIDs"}),
		fact(extract.KindReads, "reads.2", at(modelFile, "5-7"),
			map[string]string{extract.AttrField: studentsOf}),
		fact(extract.KindUsesValue, "uses.1", def, map[string]string{
			extract.AttrName:  "capabilities.CanActLikeADistrictAdmin",
			extract.AttrType:  "permissions.CapabilityName",
			extract.AttrValue: "CAN_ACT_LIKE_A_DISTRICT_ADMIN",
		}),
		fact(extract.KindUsesValue, "uses.2", def, map[string]string{
			extract.AttrName: "feature_flags.MoaEnforcementFlag",
			extract.AttrType: "featureflags.FeatureFlagName", extract.AttrValue: "moa-enforcement",
		}),
		fact(extract.KindUsesValue, "uses.3", def, map[string]string{
			extract.AttrName:  "models.Student",
			extract.AttrType:  "models.Role",
			extract.AttrValue: "STUDENT",
		}),
		fact(extract.KindLoads, "loads.1", def, map[string]string{
			extract.AttrDatastoreKind: "UserDistrictInfo", extract.AttrProperties: kaid,
		}),
		fact(extract.KindUnguarded, "unguarded.1", at("resolvers/x.go", "3-9"),
			map[string]string{extract.AttrOperation: "StudentList.legacy"}),
		fact(extract.KindValueKind, "value_kind.1", extract.Ref{}, map[string]string{
			extract.AttrType: "permissions.CapabilityName", extract.AttrValueKind: policy.KindStaff,
		}),
		fact(extract.KindValueKind, "value_kind.2", extract.Ref{}, map[string]string{
			extract.AttrType:      "featureflags.FeatureFlagName",
			extract.AttrValueKind: policy.KindFlag,
		}),
	}
}

// districtRules: the extractor's guard of Query.cdi, an open resolver, and
// an LLM rule describing the check.
func districtRules() []graph.Node {
	return []graph.Node{
		{
			ID:      "rule.classroom.authz-query-cdi",
			Type:    graph.TypeRule,
			Title:   "Access control: Query.cdi",
			Sources: []extract.Ref{at("resolvers/cdi.go", "12-14")},
			Edges:   []graph.Edge{{Type: graph.EdgeExposedBy, To: endpointCDI}},
			Extra: map[string]string{
				graph.ExtraExtractedBy: extractor,
				graph.ExtraChecks:      "acl.IsCurrentUser," + canView,
				graph.ExtraCheckArgs:   `[{"check":"acl.IsCurrentUser","var":"acl.IsCurrentUser","means":"self","subject":"kaid"},{"check":"` + canView + `"}]`,
			},
		},
		{
			ID:      "rule.misc.authz-query-time",
			Type:    graph.TypeRule,
			Title:   "Access control: Query.serverTime",
			Sources: []extract.Ref{at("resolvers/time.go", "3-3")},
			Edges:   []graph.Edge{{Type: graph.EdgeExposedBy, To: "endpoint.query-servertime"}},
			Extra: map[string]string{
				graph.ExtraExtractedBy: extractor,
				graph.ExtraChecks:      "open",
			},
		},
		{
			ID:      "rule.permissions.view-cdi",
			Type:    graph.TypeRule,
			Title:   "Teachers and students view their class",
			Sources: []extract.Ref{at(checkFile, "12-14")},
			Extra:   map[string]string{"crosscheck": "supported"},
		},
	}
}

func districtRegistry(t *testing.T) policy.Registry {
	t.Helper()
	reg, err := policy.ParseRegistry([]byte(`
teacher_of:
  from: [models.ClassroomDistrictInfo.AllTeacherUDIsIDs]
  condition: minus grantees
hasUDI:
  from: ["load UserDistrictInfo"]
isChild:
  from: [models.UserDistrictInfo.IsChild]
`))
	if err != nil {
		t.Fatal(err)
	}
	return reg
}

// TestBuildCheckInputs: a check's inputs are what its definition and the
// code it calls read, sorted into kinds by structure, each declared or not
// by the registry.
func TestBuildCheckInputs(t *testing.T) {
	inv := policy.Build(districtFacts(), districtRules(), districtRegistry(t))
	var check *policy.Check
	for i := range inv.Checks {
		if inv.Checks[i].Name == canView {
			check = &inv.Checks[i]
		}
	}
	if check == nil {
		t.Fatalf("no check %s in %+v", canView, inv.Checks)
	}
	want := []policy.Input{
		{
			Kind:       policy.KindField,
			Name:       "models.ClassroomDistrictInfo.AllTeacherUDIsIDs",
			DeclaredBy: teacherOf,
		},
		{Kind: policy.KindField, Name: studentsOf},
		{
			Kind:   policy.KindFlag,
			Name:   "feature_flags.MoaEnforcementFlag",
			Detail: "moa-enforcement",
		},
		{Kind: policy.KindLoad, Name: "UserDistrictInfo", Detail: kaid, DeclaredBy: "hasUDI"},
		{
			Kind:   policy.KindStaff,
			Name:   "capabilities.CanActLikeADistrictAdmin",
			Detail: "CAN_ACT_LIKE_A_DISTRICT_ADMIN",
		},
		{Kind: policy.KindValue, Name: "models.Student", Detail: "STUDENT"},
	}
	if !reflect.DeepEqual(check.Inputs, want) {
		t.Errorf("inputs =\n%+v\nwant\n%+v", check.Inputs, want)
	}
	if len(check.Descriptions) != 1 || check.Descriptions[0].RuleID != "rule.permissions.view-cdi" {
		t.Errorf("descriptions = %+v, want the LLM rule citing the check", check.Descriptions)
	}
}

// TestBuildEnforcementPoints: each endpoint lists its guards and their
// checks; open, todo and unguarded points are listed apart; undeclared
// field inputs are summed and registry entries no check reads are listed.
func TestBuildEnforcementPoints(t *testing.T) {
	inv := policy.Build(districtFacts(), districtRules(), districtRegistry(t))
	if len(inv.Endpoints) != 1 || inv.Endpoints[0].Endpoint != endpointCDI {
		t.Fatalf(
			"endpoints = %+v, want only endpoint.query-cdi (the open one is listed apart)",
			inv.Endpoints,
		)
	}
	guard := inv.Endpoints[0].Guards[0]
	if guard.Kinds[0] != policy.KindSelf || guard.Calls[0].Subject != kaid {
		t.Errorf("guard = %+v, want a self check on kaid first", guard)
	}
	if got := inv.Open; len(got) != 1 || got[0].RuleID != "rule.misc.authz-query-time" {
		t.Errorf("open = %+v", got)
	}
	if got := inv.Unguarded; len(got) != 1 || got[0].Operation != "StudentList.legacy" {
		t.Errorf("unguarded = %+v", got)
	}
	if want := []string{
		studentsOf,
	}; !reflect.DeepEqual(
		inv.Undeclared,
		want,
	) {
		t.Errorf("undeclared = %v, want %v", inv.Undeclared, want)
	}
	if want := []string{"isChild"}; !reflect.DeepEqual(inv.Unread, want) {
		t.Errorf("unread registry entries = %v, want %v", inv.Unread, want)
	}
}

// TestMarkdownGolden pins the inventory's Markdown on the district facts;
// rendering twice gives the same bytes. Regenerate with UPDATE_GOLDEN=1 go
// test ./internal/policy and review.
func TestMarkdownGolden(t *testing.T) {
	inv := policy.Build(districtFacts(), districtRules(), districtRegistry(t))
	got := inv.Markdown()
	if again := inv.Markdown(); !bytes.Equal(got, again) {
		t.Fatal("two renders differ")
	}
	checkGolden(t, goldenFile, got)
}

// TestFieldResolverIsAnEnforcementPoint: a guard with no endpoint (a field
// resolver's) is listed under the operation it serves, never dropped.
func TestFieldResolverIsAnEnforcementPoint(t *testing.T) {
	rule := graph.Node{
		ID:      "rule.user.authz-user-emails",
		Type:    graph.TypeRule,
		Title:   "Access control: User.emails",
		Sources: []extract.Ref{at("resolvers/user.go", "40-42")},
		Extra: map[string]string{
			graph.ExtraExtractedBy: extractor,
			graph.ExtraOperation:   "User.emails",
			graph.ExtraChecks:      "acl.IsLoggedIn",
			graph.ExtraCheckArgs:   `[{"check":"acl.IsLoggedIn","var":"caller.loggedIn","means":"login"}]`,
		},
	}
	inv := policy.Build(nil, []graph.Node{rule}, policy.Registry{})
	if len(inv.Endpoints) != 1 || inv.Endpoints[0].Endpoint != userEmails {
		t.Fatalf("endpoints = %+v, want "+userEmails, inv.Endpoints)
	}
}

// checkGolden compares got with the golden file at path, rewriting it
// first under UPDATE_GOLDEN.
func checkGolden(t *testing.T, path string, got []byte) {
	t.Helper()
	if os.Getenv("UPDATE_GOLDEN") != "" {
		if err := os.WriteFile(path, got, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path) //nolint:gosec // a golden file the test names
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("differs from %s:\n%s", path, got)
	}
}
