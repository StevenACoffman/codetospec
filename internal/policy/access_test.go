package policy_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/StevenACoffman/codetospec/internal/graph"
	"github.com/StevenACoffman/codetospec/internal/locale"
	"github.com/StevenACoffman/codetospec/internal/policy"
)

// accessRules are districtRules with Query.cdi's guard denying when the
// caller is not the user and fails CanViewCDI: the two checks are
// alternatives, not requirements.
func accessRules() []graph.Node {
	rules := districtRules()
	rules[0].Extra[graph.ExtraCheckArgs] = `[` +
		`{"check":"acl.IsCurrentUser","var":"acl.IsCurrentUser","means":"self","subject":"kaid"},` +
		`{"check":"` + canView + `","var":"` + canView + `.fails","fails":true}]`
	rules[0].Extra[graph.ExtraCondition] = `{"all":[` +
		`{"var":"acl.IsCurrentUser","op":"equal","value":false},` +
		`{"var":"` + canView + `.fails","op":"equal","value":true}]}`
	return rules
}

// TestAccess: each operation is listed under the concepts its checks'
// pins name, each guard stated from its condition (a deny on both
// failing allows on either) with each call's meaning or pin; a guard
// calling a check nothing states is undecided, naming it, and without a
// pin the operation falls under no concept.
func TestAccess(t *testing.T) {
	reg, err := policy.ParseRegistry([]byte(`{"teacher_of": {"from": ["x.Y"],
		"phrase": {"en": "teaches the class"}, "concept": "Classroom"}}`))
	if err != nil {
		t.Fatal(err)
	}
	pins, err := policy.ParseCheckers([]byte(
		`{"` + canView + `": {"means": "relation", "relation": "teacher_of"}}`))
	if err != nil {
		t.Fatal(err)
	}
	guard := func(statement, reason string) []policy.OperationAccess {
		return []policy.OperationAccess{{Operation: endpointCDI, Guards: []policy.GuardAccess{{
			Rule:      "rule.classroom.authz-query-cdi",
			Checks:    []string{isCurrentUser, canView},
			Statement: statement, Undecided: statement == "", Reason: reason,
		}}}}
	}
	for name, tc := range map[string]struct {
		pins policy.Checkers
		want []policy.ConceptAccess
	}{
		"pinned": {pins, []policy.ConceptAccess{{Concept: classroom, Operations: guard(
			"Allowed when the caller is the user concerned, or the caller teaches the class.", "")}}},
		"unpinned": {nil, []policy.ConceptAccess{{Operations: guard(
			"", policy.ReasonNotPinned+": "+canView)}}},
	} {
		inv := policy.Build(districtFacts(), accessRules(), reg)
		inv.Define(tc.pins, reg, locale.For("en"))
		if !reflect.DeepEqual(inv.Access, tc.want) {
			t.Errorf("%s: Access = %+v, want %+v", name, inv.Access, tc.want)
		}
		if name != "pinned" {
			continue
		}
		md := string(inv.Markdown())
		for _, line := range []string{
			"## Who can do what", "### Classroom", "- " + endpointCDI,
			"  - " + isCurrentUser + ", " + canView +
				": Allowed when the caller is the user concerned, or the caller teaches the class.",
		} {
			if !strings.Contains(md, line+"\n") {
				t.Errorf("markdown lacks %q:\n%s", line, md)
			}
		}
	}
}

// TestAccessRequirements: a guard denying when either check fails states
// both as requirements; one testing data beside its checks is undecided.
func TestAccessRequirements(t *testing.T) {
	for name, tc := range map[string]struct{ condition, want string }{
		"either fails": {
			`{"any":[{"var":"acl.IsCurrentUser","op":"equal","value":false},` +
				`{"var":"` + canView + `.fails","op":"equal","value":true}]}`,
			"Allowed when the caller is the user concerned and the caller has the relation \"teacher_of\".",
		},
		partData: {
			`{"all":[{"var":"acl.IsCurrentUser","op":"equal","value":false},` +
				`{"var":"local.count","op":"greater_than","value":3}]}`,
			"",
		},
	} {
		rules := accessRules()
		rules[0].Extra[graph.ExtraCondition] = tc.condition
		inv := policy.Build(districtFacts(), rules, policy.Registry{})
		pins, err := policy.ParseCheckers([]byte(
			`{"` + canView + `": {"means": "relation", "relation": "teacher_of"}}`))
		if err != nil {
			t.Fatal(err)
		}
		inv.Define(pins, policy.Registry{}, locale.For("en"))
		g := inv.Access[0].Operations[0].Guards[0]
		if g.Statement != tc.want || g.Undecided != (tc.want == "") {
			t.Errorf("%s: %+v, want statement %q", name, g, tc.want)
		}
		if tc.want == "" && g.Reason != policy.ReasonData {
			t.Errorf("%s: reason %q, want %q", name, g.Reason, policy.ReasonData)
		}
	}
}
