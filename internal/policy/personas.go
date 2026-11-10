package policy

import (
	"maps"
	"slices"
)

// Persona is one kind of caller an access matrix asks the exported
// policies about: a principal kind and, for a user, whether the account is
// a phantom and which capability grants it holds. What the request is
// about (the user concerned, its country, the locale) is not a persona's:
// a decision that turns on it is a dependency, not a column.
type Persona struct {
	// ID is stable and unique: "anonymous", "role:<name>@<grant scope>"…
	ID        string `json:"id"`
	Principal string `json:"principal"` // Principal*
	Phantom   bool   `json:"phantom,omitempty"`
	// Role is the role whose grant gives Grants ("" for none).
	Role   string  `json:"role,omitempty"`
	Grants []Grant `json:"grants,omitempty"`
}

// Grant is one capability grant a persona holds: the capability's stored
// name, the grant's scope (Grant*), and whether the user holds it itself
// rather than through a role (an explicit check accepts only those).
type Grant struct {
	Capability string `json:"capability"`
	Scope      string `json:"scope"`
	Explicit   bool   `json:"explicit,omitempty"`
}

// Scopes of a grant, as webapp's admin service records them: on every
// user and resource, on users of one kind ("all", or "nwea-map" users),
// on one country's users, for one locale, for one locale and content
// kind. A check's scope (Scope*) says which grants it accepts; the export
// templates, not this list, decide that.
const (
	GrantGlobal    = "global"
	GrantUsersAll  = "users-all"
	GrantUsersNwea = "users-nwea"
	GrantCountry   = "country"
	GrantLocale    = "locale"
	GrantContent   = "content"
)

// GrantScopes lists every grant scope, in the order personas take them.
func GrantScopes() []string {
	return []string{
		GrantGlobal, GrantUsersAll, GrantUsersNwea, GrantCountry, GrantLocale, GrantContent,
	}
}

// Personas are the callers m's access matrix asks about, in a fixed
// order: anonymous, a Pub/Sub push, a service-admin request, a signed-in
// user with no grant, a phantom one (when a check refuses phantoms), a
// site administrator (every capability, globally), then for each role
// granting a capability some check names, that role granted at each
// scope, and for each such capability, a user holding it explicitly
// (globally). Roles and grant scopes nothing could tell apart are left to
// the matrix, which merges personas its policies decide alike.
//
// Ensures: deterministic; IDs unique.
func Personas(m *Model) []Persona {
	checked := checkedCapabilities(m)
	out := []Persona{
		{ID: PrincipalAnonymous, Principal: PrincipalAnonymous},
		{ID: PrincipalPush, Principal: PrincipalPush},
		{ID: PrincipalService, Principal: PrincipalService},
		{ID: PrincipalUser, Principal: PrincipalUser},
	}
	if m.refusesPhantoms() {
		out = append(out, Persona{ID: "phantom", Principal: PrincipalUser, Phantom: true})
	}
	admin := Persona{ID: "site-admin", Principal: PrincipalUser}
	for _, c := range slices.Sorted(maps.Keys(checked)) {
		admin.Grants = append(admin.Grants, Grant{Capability: c, Scope: GrantGlobal})
	}
	out = append(out, admin)

	for _, r := range m.Roles {
		if !slices.ContainsFunc(r.Capabilities, func(c string) bool { return checked[c] }) {
			continue
		}
		for _, scope := range GrantScopes() {
			p := Persona{ID: "role:" + r.Name + "@" + scope, Principal: PrincipalUser, Role: r.Name}
			for _, c := range r.Capabilities {
				p.Grants = append(p.Grants, Grant{Capability: c, Scope: scope})
			}
			out = append(out, p)
		}
	}
	for _, c := range slices.Sorted(maps.Keys(checked)) {
		out = append(out, Persona{
			ID: "holds:" + c, Principal: PrincipalUser,
			Grants: []Grant{{Capability: c, Scope: GrantGlobal, Explicit: true}},
		})
	}
	return out
}

// checkedCapabilities are the capabilities, by stored name, that some
// check of m's operations or item policies names.
func checkedCapabilities(m *Model) map[string]bool {
	out := map[string]bool{}
	for _, op := range m.allOperations() {
		for _, l := range op.Leaves() {
			if l.Kind == LeafCapability && l.Capability != "" {
				out[l.Capability] = true
			}
		}
	}
	return out
}

// refusesPhantoms reports whether some check of m refuses phantom
// accounts.
func (m *Model) refusesPhantoms() bool {
	for _, op := range m.allOperations() {
		if slices.ContainsFunc(
			op.Leaves(),
			func(l Leaf) bool { return l.Kind == LeafLoginStrict },
		) {
			return true
		}
	}
	return false
}

// allOperations are m's operations, then its item policies' operations.
func (m *Model) allOperations() []*Operation {
	out := make([]*Operation, 0, len(m.Operations)+len(m.Items))
	for i := range m.Operations {
		out = append(out, &m.Operations[i])
	}
	for i := range m.Items {
		out = append(out, &m.Items[i].Operation)
	}
	return out
}
