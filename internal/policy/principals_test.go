package policy_test

import (
	"reflect"
	"testing"

	"github.com/StevenACoffman/codetospec/internal/graph"
	"github.com/StevenACoffman/codetospec/internal/policy"
)

// TestPrincipals: an operation allows only the principal kinds some
// request passing its guards can come from: a push check admits pushes
// alone, a login or service-admin check users and services; a guard on a
// relation (any kind can hold one) leaves every kind, so nothing is set.
func TestPrincipals(t *testing.T) {
	notVar := func(v string) string { return `{"not":{"var":"` + v + `","op":"equal","value":true}}` }
	m := policy.BuildModel(nil, []graph.Node{
		guardRule("rule.push", "Mutation.push", notVar("push"),
			`[{"check":"pubsub.Validate","var":"push","means":"push"}]`),
		guardRule("rule.login", "Query.login",
			`{"all":[`+notVar("caller.loggedIn")+`,`+notVar("admin")+`]}`,
			`[{"check":"acl.IsLoggedIn","var":"caller.loggedIn","means":"login"},`+
				`{"check":"ctx.RequestIsServiceAdminQuery","var":"admin","means":"service_admin"}]`),
		guardRule("rule.relation", "Query.relation", notVar("rel"),
			`[{"check":"permissions.Teaches","var":"rel"}]`),
	}, policy.Checkers{"permissions.Teaches": {Means: policy.MeansRelation, Relation: "teacher_of"}})
	got := map[string][]string{}
	for _, op := range m.Operations {
		got[op.Action] = op.Principals
	}
	want := map[string][]string{
		"Mutation.push":  {policy.PrincipalPush},
		"Query.login":    {policy.PrincipalService, policy.PrincipalUser},
		"Query.relation": nil,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("principals = %v, want %v (skipped %+v)", got, want, m.Skipped)
	}
}

// TestRealizable: an assignment is realizable when one principal kind can
// make every check it holds hold: a service-admin request is never also
// the user a self check is about, while a relation holds for any kind.
func TestRealizable(t *testing.T) {
	const varAdmin, varSelf, varRel = "admin", "self", "rel"
	leaves := []policy.Leaf{
		{Var: varAdmin, Kind: policy.LeafServiceAdmin},
		{Var: varSelf, Kind: policy.LeafSelf},
		{Var: varRel, Kind: policy.LeafRelation},
	}
	tests := []struct {
		name  string
		given map[string]bool
		want  bool
	}{
		{"nothing holds", map[string]bool{}, true},
		{"service admin with a relation", map[string]bool{varAdmin: true, varRel: true}, true},
		{"self with a relation", map[string]bool{varSelf: true, varRel: true}, true},
		{"service admin and self", map[string]bool{varAdmin: true, varSelf: true}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := policy.Realizable(leaves, tt.given); got != tt.want {
				t.Errorf("Realizable(%v) = %v, want %v", tt.given, got, tt.want)
			}
		})
	}
}
