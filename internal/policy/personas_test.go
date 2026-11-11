package policy_test

import (
	"slices"
	"testing"

	"github.com/StevenACoffman/codetospec/internal/policy"
)

// TestPersonas: the fixed callers first; a phantom only when a check
// refuses one; a role only when it grants a checked capability, at every
// grant scope; an explicit holder per checked capability.
func TestPersonas(t *testing.T) {
	const checked = "can_view_user_identity"
	leaf := func(kind string) policy.Expr {
		return policy.Expr{Op: policy.OpCheck, Check: &policy.Leaf{
			Kind: kind, Var: kind, Capability: checked, Scope: policy.ScopeAny,
		}}
	}
	m := policy.Model{
		Operations: []policy.Operation{{Action: "User.emails", Denies: []policy.Deny{{
			When: policy.Expr{Op: policy.OpAll, Args: []policy.Expr{
				leaf(policy.LeafLoginStrict), leaf(policy.LeafCapability),
			}},
		}}}},
		Roles: []policy.Role{
			{Name: "Support", Capabilities: []string{checked, "can_manage_classrooms"}},
			{Name: "Unrelated", Capabilities: []string{"can_manage_classrooms"}},
		},
	}
	var ids []string
	for _, p := range policy.Personas(&m) {
		ids = append(ids, p.ID)
	}
	want := []string{"anonymous", "push", "service", "user", "phantom", "site-admin"}
	for _, scope := range policy.GrantScopes() {
		want = append(want, "role:Support@"+scope)
	}
	want = append(want, "holds:"+checked)
	if !slices.Equal(ids, want) {
		t.Errorf("personas = %v\nwant       %v", ids, want)
	}
}
