package policy_test

import (
	"reflect"
	"testing"

	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/graph"
	"github.com/StevenACoffman/codetospec/internal/policy"
)

const (
	parentKaid      = "parent:Kaid"
	canViewIdentity = "can_view_user_identity"
	userEmails      = "User.emails"
)

// guardRule is an extractor rule guarding op with a condition and the
// operands of its check calls.
func guardRule(id, op, cond, checkArgs string) graph.Node {
	return graph.Node{
		ID: id, Type: graph.TypeRule, Title: "Access control: " + op,
		Sources: []extract.Ref{at("resolvers/r.go", "1-3")},
		Extra: map[string]string{
			graph.ExtraExtractedBy: extractor, graph.ExtraOperation: op,
			graph.ExtraChecks: "x", graph.ExtraCondition: cond, graph.ExtraCheckArgs: checkArgs,
		},
	}
}

// TestBuildModel: a guard whose every check is in the pinned vocabulary
// becomes a deny over those checks, a capability by its stored name and
// matcher; any other guard is skipped with its reason; roles name stored
// capability names.
func TestBuildModel(t *testing.T) {
	facts := []extract.Fact{
		fact(extract.KindCapability, "capability.1", at("caps.go", "1-1"), map[string]string{
			extract.AttrName: "CanViewUserIdentity", extract.AttrValue: canViewIdentity,
			extract.AttrScopeKeys: "UserKindScopeType",
		}),
		fact(extract.KindRule, "rule.webapp.role.1", at("roles.go", "1-5"), map[string]string{
			"role": "supporter", "capabilities": "CanViewUserIdentity",
		}),
	}
	rules := []graph.Node{
		guardRule(
			"rule.user.authz-emails",
			userEmails,
			`{"all":[{"not":{"var":"acl.IsCurrentUser","op":"equal","value":true}},`+
				`{"not":{"var":"caller.hasPermission.CanViewUserIdentity","op":"equal","value":true}}]}`,
			`[{"check":"acl.IsCurrentUser","var":"acl.IsCurrentUser","means":"self","subject":"user.Kaid","subject_from":"parent:Kaid"},`+
				`{"check":"acl.ActorHasPermission","var":"caller.hasPermission.CanViewUserIdentity","means":"capability","scope":"user","capability":"CanViewUserIdentity","matcher":"acl.UserScope","matcher_arg":"user.Kaid","subject_from":"parent:Kaid"}]`,
		),
		guardRule("rule.user.authz-wrapper", "Query.ipInfo",
			`{"not":{"var":"actorHasGlobalPermission","op":"equal","value":true}}`,
			`[{"check":"actorHasGlobalPermission","capability":"CanModerateUsers"}]`),
	}
	m := policy.BuildModel(facts, rules, nil)

	if len(m.Operations) != 1 || m.Operations[0].Action != userEmails {
		t.Fatalf("operations = %+v, want User.emails only", m.Operations)
	}
	got := m.Operations[0].Denies[0].When
	want := policy.Expr{Op: policy.OpAll, Args: []policy.Expr{
		{
			Op: policy.OpNot,
			Args: []policy.Expr{
				{
					Op: policy.OpCheck,
					Check: &policy.Leaf{
						Kind:        policy.LeafSelf,
						Var:         "acl.IsCurrentUser",
						Subject:     "user.Kaid",
						SubjectFrom: parentKaid,
					},
				},
			},
		},
		{Op: policy.OpNot, Args: []policy.Expr{{Op: policy.OpCheck, Check: &policy.Leaf{
			Kind:        policy.LeafCapability,
			Var:         "caller.hasPermission.CanViewUserIdentity",
			Capability:  canViewIdentity,
			Scope:       policy.ScopeUser,
			Subject:     "user.Kaid",
			SubjectFrom: parentKaid,
		}}}},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("deny =\n%+v\nwant\n%+v", got, want)
	}
	if len(m.Skipped) != 1 || m.Skipped[0].RuleID != "rule.user.authz-wrapper" {
		t.Errorf("skipped = %+v, want the wrapper guard", m.Skipped)
	}
	if want := []policy.Role{
		{Name: "supporter", Capabilities: []string{canViewIdentity}},
	}; !reflect.DeepEqual(
		m.Roles,
		want,
	) {
		t.Errorf("roles = %+v, want %+v", m.Roles, want)
	}
}

// TestModelOperationWholeOrNot: an operation with one untranslated guard is
// left out whole, its translated guards listed as skipped too: exporting
// them alone would allow what the untranslated guard denies.
func TestModelOperationWholeOrNot(t *testing.T) {
	rules := []graph.Node{
		guardRule(
			"rule.a.login",
			"Query.mixed",
			`{"not":{"var":"caller.loggedIn","op":"equal","value":true}}`,
			`[{"check":"acl.IsLoggedIn","var":"caller.loggedIn","means":"login"}]`,
		),
		guardRule("rule.a.opaque", "Query.mixed",
			`{"not":{"var":"customCheck","op":"equal","value":true}}`, `[{"check":"customCheck"}]`),
	}
	m := policy.BuildModel(nil, rules, nil)
	if len(m.Operations) != 0 {
		t.Errorf("operations = %+v, want none", m.Operations)
	}
	if len(m.Skipped) != 2 {
		t.Errorf("skipped = %+v, want both guards", m.Skipped)
	}
}

// loginGuard is a login guard of op cited at resolvers/r.go:lines, its call
// named check.
func loginGuard(id, op, lines, check string) graph.Node {
	n := guardRule(id, op, `{"not":{"var":"caller.loggedIn","op":"equal","value":true}}`,
		`[{"check":"`+check+`","var":"caller.loggedIn","means":"login"}]`)
	n.Sources = []extract.Ref{at("resolvers/r.go", lines)}
	return n
}

// TestModelChecksBelowGuard: a resolver whose body reaches a recognized
// check its guards don't call is left out (the check may deny what the
// blanket allow grants); a check reached only through a check the guard
// calls is the guard's own, and changes nothing.
func TestModelChecksBelowGuard(t *testing.T) {
	const deep = "permissions.Deep"
	facts := []extract.Fact{
		fact(extract.KindAccessCheck, "ac.1", at("checks.go", "10-20"),
			map[string]string{extract.AttrName: deep}),
		fact(extract.KindAccessCheck, "ac.2", at("auth.go", "1-5"),
			map[string]string{extract.AttrName: "auth.Login"}),
		// Query.deep's resolver calls a helper that calls the check.
		fact(extract.KindCall, "call.1", at("resolvers/r.go", "1-9"),
			map[string]string{extract.AttrCalleePath: "helper.go", extract.AttrCalleeLines: "5-8"}),
		fact(
			extract.KindCall,
			"call.2",
			at("helper.go", "5-8"),
			map[string]string{
				extract.AttrCalleePath:  "checks.go",
				extract.AttrCalleeLines: "10-20",
			},
		),
		// Query.own's resolver calls its guard's check, which calls the same
		// check.
		fact(extract.KindCall, "call.3", at("resolvers/r.go", "20-29"),
			map[string]string{extract.AttrCalleePath: "auth.go", extract.AttrCalleeLines: "1-5"}),
		fact(
			extract.KindCall,
			"call.4",
			at("auth.go", "1-5"),
			map[string]string{
				extract.AttrCalleePath:  "checks.go",
				extract.AttrCalleeLines: "10-20",
			},
		),
	}
	m := policy.BuildModel(facts, []graph.Node{
		loginGuard("rule.a.deep", "Query.deep", "2-3", "acl.IsLoggedIn"),
		loginGuard("rule.a.own", "Query.own", "21-22", "auth.Login"),
	}, nil)
	if len(m.Operations) != 1 || m.Operations[0].Action != "Query.own" {
		t.Errorf("operations = %+v, want Query.own only", m.Operations)
	}
	want := []policy.Skip{{RuleID: "rule.a.deep", Reason: "checks below its guard: " + deep}}
	if !reflect.DeepEqual(m.Skipped, want) {
		t.Errorf("skipped = %+v, want %+v", m.Skipped, want)
	}
}

// TestModelSilentOperations: an extractor rule of an operation that names
// no check (a delegation, a guard on a local the extractor can't trace)
// keeps its operation out, with the reason; no operation is left unsaid.
func TestModelSilentOperations(t *testing.T) {
	delegation := guardRule("rule.a.alias", "Query.alias", "", "")
	delegation.Extra[graph.ExtraChecks] = ""
	delegation.Extra[graph.ExtraDelegatesTo] = "queryResolver.Target"
	local := guardRule("rule.a.local", "Query.local", "", "")
	local.Extra[graph.ExtraChecks] = ""
	m := policy.BuildModel(nil, []graph.Node{
		delegation, local, loginGuard("rule.a.login", "Query.local", "1-3", "acl.IsLoggedIn"),
	}, nil)
	if len(m.Operations) != 0 {
		t.Errorf("operations = %+v, want none", m.Operations)
	}
	want := []policy.Skip{
		{RuleID: "rule.a.alias", Reason: "delegation to queryResolver.Target"},
		{RuleID: "rule.a.local", Reason: "guard names no check"},
		{RuleID: "rule.a.login", Reason: "another guard of Query.local is skipped"},
	}
	if !reflect.DeepEqual(m.Skipped, want) {
		t.Errorf("skipped = %+v, want %+v", m.Skipped, want)
	}
}

// TestModelItemGuard: a guard dropping one item (scope_of_check, carried
// to the graph) isn't a call's deny.
func TestModelItemGuard(t *testing.T) {
	n := loginGuard("rule.a.item", "Query.list", "1-3", "acl.IsLoggedIn")
	n.Extra[graph.ExtraCheckScope] = "item"
	m := policy.BuildModel(nil, []graph.Node{n}, nil)
	want := []policy.Skip{{RuleID: "rule.a.item", Reason: "drops one item, not the call"}}
	if len(m.Operations) != 0 || !reflect.DeepEqual(m.Skipped, want) {
		t.Errorf("model = %+v, want %+v skipped", m, want)
	}
}
