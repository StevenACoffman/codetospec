package cerbos_test

import (
	"bytes"
	"maps"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/StevenACoffman/codetospec/internal/cerbos"
	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/graph"
	"github.com/StevenACoffman/codetospec/internal/policy"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"gopkg.in/yaml.v3"
)

const (
	identityGoName = "CanViewUserIdentity"
	service        = "users"
	policyFile     = "resource_policies/users.yaml"
	suiteFile      = "tests/users_test.yaml"
	goldenFile     = "testdata/users.golden.yaml"
)

// usersCheckers pins usersModel's checks the extractor doesn't know.
func usersCheckers() policy.Checkers {
	return policy.Checkers{
		"permissions.CanViewCDI": {Means: policy.MeansRelation, Relation: "teacher_of"},
		// districts' ActorCanViewUserDistrictData, as its code reads.
		"permissions.ActorCanViewUserDistrictData": {Any: []policy.Checker{
			{Means: policy.MeansServiceAdmin},
			{
				Means:      policy.MeansCapability,
				Capability: identityGoName,
				Scope:      policy.ScopeUserAll,
			},
			{
				Means:      policy.MeansCapability,
				Capability: identityGoName,
				Scope:      policy.ScopeUserNwea,
			},
		}},
		// districts' VisibleAssignmentDistrictInfos-like filter.
		"permissions.VisibleThings": {Item: "Thing", Any: []policy.Checker{
			{Means: policy.MeansServiceAdmin},
			{All: []policy.Checker{
				{
					Means:      policy.MeansCapability,
					Capability: identityGoName,
					Scope:      policy.ScopeUserNwea,
				},
				{Means: policy.MeansRelation, Relation: "in_kmap_district"},
			}},
		}},
	}
}

// usersModel is User.emails, denied unless the caller is the user or holds
// can_view_user_identity on a user scope; Query.time, open; Query.cdi,
// denied when the caller isn't a teacher of the resource (a pinned checker
// returning an error), and Query.cdiAlias delegating to it; Mutation.notify,
// denied unless a Pub/Sub push; Query.udi, denied unless a compound checker
// (service admin, or the capability on userKind "all" or "nwea-map") holds.
func usersModel() policy.Model {
	facts := []extract.Fact{
		{
			Kind: extract.KindCapability,
			ID:   "capability.1",
			Attrs: map[string]string{
				extract.AttrName:  identityGoName,
				extract.AttrValue: "can_view_user_identity",
			},
		},
	}
	node := func(id, op, checks, cond, args string) graph.Node {
		return graph.Node{
			ID:      id,
			Type:    graph.TypeRule,
			Sources: []extract.Ref{{Path: "resolvers/u.go", Lines: "1-3"}},
			Extra: map[string]string{
				graph.ExtraExtractedBy: "webapp",
				graph.ExtraOperation:   op,
				graph.ExtraChecks:      checks,
				graph.ExtraCondition:   cond,
				graph.ExtraCheckArgs:   args,
			},
		}
	}
	rules := []graph.Node{
		node(
			"rule.user.authz-emails",
			"User.emails",
			"acl.ActorHasPermission,acl.IsCurrentUser",
			`{"all":[{"not":{"var":"acl.IsCurrentUser","op":"equal","value":true}},`+
				`{"not":{"var":"caller.hasPermission.CanViewUserIdentity","op":"equal","value":true}}]}`,
			`[{"check":"acl.IsCurrentUser","var":"acl.IsCurrentUser","means":"self","subject":"user.Kaid","subject_from":"parent:Kaid"},`+
				`{"check":"acl.ActorHasPermission","var":"caller.hasPermission.CanViewUserIdentity","means":"capability","scope":"user","capability":"CanViewUserIdentity","matcher":"acl.UserScope","matcher_arg":"user.Kaid","subject_from":"parent:Kaid"}]`,
		),
		node("rule.misc.authz-time", "Query.time", "open", "", ""),
		node(
			"rule.class.authz-cdi",
			"Query.cdi",
			"permissions.CanViewCDI",
			`{"var":"permissions.CanViewCDI.fails","op":"equal","value":true}`,
			`[{"check":"permissions.CanViewCDI","var":"permissions.CanViewCDI.fails","fails":true}]`,
		),
		node(
			"rule.hook.authz-notify",
			"Mutation.notify",
			"pubsub.DecodeAndValidatePushMsg",
			`{"var":"pubsub.DecodeAndValidatePushMsg.fails","op":"equal","value":true}`,
			`[{"check":"pubsub.DecodeAndValidatePushMsg","var":"pubsub.DecodeAndValidatePushMsg.fails",`+
				`"fails":true,"means":"push"}]`,
		),
	}
	alias := node("rule.class.authz-cdi-alias", "Query.cdiAlias", "", "", "")
	alias.Extra[graph.ExtraDelegatesTo] = "classResolver.Cdi"
	alias.Extra[graph.ExtraDelegatesToOperation] = "Query.cdi"
	checkers := usersCheckers()
	things := node("rule.district.authz-things", "Query.things", "permissions.VisibleThings", "",
		`[{"check":"permissions.VisibleThings"}]`)
	things.Extra[graph.ExtraCheckScope] = "item"
	udi := node(
		"rule.district.authz-udi",
		"Query.udi",
		"permissions.ActorCanViewUserDistrictData",
		`{"not":{"var":"permissions.ActorCanViewUserDistrictData","op":"equal","value":true}}`,
		`[{"check":"permissions.ActorCanViewUserDistrictData","var":"permissions.ActorCanViewUserDistrictData"}]`,
	)
	return policy.BuildModel(facts, append(rules, alias, udi, things), checkers)
}

// TestExportValidates: the policies (the service's, an item kind's) and the
// test suite conform to Cerbos's published JSON schemas.
func TestExportValidates(t *testing.T) {
	m := usersModel()
	spec, err := cerbos.Export(&m, service)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct{ file, schema string }{{suiteFile, "testdata/TestSuite.schema.json"}}
	for _, file := range slices.Sorted(maps.Keys(spec.Files)) {
		if strings.HasPrefix(file, "resource_policies/") {
			cases = append(
				cases,
				struct{ file, schema string }{file, "testdata/Policy.schema.json"},
			)
		}
	}
	if len(cases) != 3 {
		t.Fatalf(
			"files = %v, want the service's policy, the item's and the suite",
			slices.Sorted(maps.Keys(spec.Files)),
		)
	}
	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			var doc any
			if decodeErr := yaml.Unmarshal(spec.Files[tc.file], &doc); decodeErr != nil {
				t.Fatal(decodeErr)
			}
			if invalid := compile(t, tc.schema).Validate(doc); invalid != nil {
				t.Errorf("%s does not conform: %v\n%s", tc.file, invalid, spec.Files[tc.file])
			}
		})
	}
}

// TestSuitePairsDecisions: the suite holds, for the guarded operation, a
// case its guard denies and one it allows (the self check holding), each
// expectation decided by the rule's condition.
func TestSuitePairsDecisions(t *testing.T) {
	m := usersModel()
	spec, err := cerbos.Export(&m, service)
	if err != nil {
		t.Fatal(err)
	}
	suite := string(spec.Files[suiteFile])
	for _, want := range []string{
		"User.emails !acl.IsCurrentUser !caller.hasPermission.CanViewUserIdentity",
		"User.emails: EFFECT_DENY",
		"User.emails acl.IsCurrentUser !caller.hasPermission.CanViewUserIdentity",
		"User.emails: EFFECT_ALLOW",
	} {
		if !strings.Contains(suite, want) {
			t.Errorf("suite lacks %q:\n%s", want, suite)
		}
	}
}

// TestExportGolden pins the policy; regenerate with UPDATE_GOLDEN=1 go test
// ./internal/cerbos and review.
func TestExportGolden(t *testing.T) {
	m := usersModel()
	spec, err := cerbos.Export(&m, service)
	if err != nil {
		t.Fatal(err)
	}
	got := spec.Files[policyFile]
	if os.Getenv("UPDATE_GOLDEN") != "" {
		if writeErr := os.WriteFile(goldenFile, got, 0o600); writeErr != nil {
			t.Fatal(writeErr)
		}
	}
	want, err := os.ReadFile(goldenFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("policy differs from %s:\n%s", goldenFile, got)
	}
}

// compile loads a vendored Cerbos JSON schema.
func compile(t *testing.T, path string) *jsonschema.Schema {
	t.Helper()
	f, err := os.Open(path) //nolint:gosec // a vendored schema of testdata
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	doc, err := jsonschema.UnmarshalJSON(f)
	if err != nil {
		t.Fatal(err)
	}
	c := jsonschema.NewCompiler()
	if err = c.AddResource(path, doc); err != nil {
		t.Fatal(err)
	}
	s, err := c.Compile(path)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
