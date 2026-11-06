package main

import (
	"testing"
	"testing/fstest"
)

// TestCapabilityFacts: each capability admin registers is a fact joining
// its Go name (what checks and roles name) to its stored name (what grants
// hold) and the scope keys it requires.
func TestCapabilityFacts(t *testing.T) {
	service := fstest.MapFS{
		"permissions/capabilities/moderation.go": {Data: []byte(`package capabilities

var CanModerateUsers = RegisterCapabilityName("can_moderate_users")

var CanBanUsers = RegisterCapabilityDefinition(Capability{
	Name:              "can_ban_users",
	RequiredScopeKeys: []models.ScopeType{models.UserKindScopeType, models.LocaleScopeType},
})

var notACapability = register("x")

var UserScopeKeys = []models.ScopeType{models.UserKindScopeType}

var CanViewUserIdentity = RegisterCapabilityDefinition(Capability{
	Name:              "can_view_user_identity",
	RequiredScopeKeys: UserScopeKeys,
})
`)},
	}
	facts, _, err := Analyze(&Inputs{Service: service, Lang: "en"})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string][2]string{} // Go name → stored name, scope keys
	for _, f := range factsOfKind(facts, kindCapability) {
		got[f.Attrs[attrName]] = [2]string{f.Attrs[attrValue], f.Attrs[attrScopeKeys]}
	}
	want := map[string][2]string{
		"CanModerateUsers": {"can_moderate_users", ""},
		"CanBanUsers":      {"can_ban_users", "UserKindScopeType,LocaleScopeType"},
		"CanViewUserIdentity": {
			"can_view_user_identity",
			"UserKindScopeType",
		}, // through a shared list
	}
	if len(got) != len(want) {
		t.Errorf("capability facts = %v, want %v", got, want)
	}
	for name, w := range want {
		if got[name] != w {
			t.Errorf("%s = %v, want %v", name, got[name], w)
		}
	}
}
