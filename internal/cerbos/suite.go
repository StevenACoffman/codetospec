package cerbos

import (
	"fmt"
	"slices"
	"strings"

	"github.com/StevenACoffman/codetospec/internal/policy"
	"gopkg.in/yaml.v3"
)

// The userKind values of a grant on users the loader contract names.
const (
	userKindAll  = "all"
	userKindNwea = "nwea-map"
)

// actorID and otherID are the KAIDs the suite's principals and subjects use.
const (
	actorID = "kaid_actor"
	otherID = "kaid_other"
)

// TestSuite writes the Cerbos test suite pinning the policy's decisions:
// for each operation, every assignment of truth to its guards' checks
// (at most policy.MaxCaseLeaves of them), realized as a principal and a resource the
// loader contract allows, expecting EFFECT_DENY exactly when one guard's
// condition, as the extractor wrote it, holds (package condition decides:
// the oracle is the rule, not this package's rendering). An assignment no
// principal can realize (a global grant always satisfies an any-scope
// check) is left out. `cerbos compile` runs it.
//
// Ensures: deterministic; every permit case has, for each guard, a case
// where that guard denies when one is realizable (authz §9.6).
func TestSuite(m *policy.Model, service string) ([]byte, error) {
	s := suite{Name: service, Principals: map[string]principal{}, Resources: map[string]resource{}}
	for i := range m.Operations {
		s.add(service, fmt.Sprintf("op%d", i), "", &m.Operations[i])
	}
	for i := range m.Items {
		item := &m.Items[i]
		s.add(
			ItemResource(service, item.Kind),
			fmt.Sprintf("item%d", i),
			item.Kind+" ",
			&item.Operation,
		)
	}
	data, err := yaml.Marshal(&s)
	if err != nil {
		return nil, fmt.Errorf("cerbos: encode test suite: %w", err)
	}
	return data, nil
}

// add adds op's realizable cases on resource kind kind, keyed by prefix,
// each test named after op's action prefixed by label.
func (s *suite) add(kind, prefix, label string, op *policy.Operation) {
	leaves := op.Leaves()
	for n, c := range op.Cases() {
		p, r := realize(leaves, c.Given)
		if !feasible(leaves, c.Given, &p, &r) {
			continue
		}
		effect := "EFFECT_ALLOW"
		if c.Denied {
			effect = "EFFECT_DENY"
		}
		key := fmt.Sprintf("%s_case%d", prefix, n)
		r.Kind = kind
		s.Principals[key] = p
		s.Resources[key] = r
		expected := map[string]string{}
		for _, a := range op.Actions() {
			expected[a] = effect
		}
		s.Tests = append(s.Tests, test{
			Name: label + op.Action + " " + caseName(leaves, c.Given),
			Input: input{
				Principals: []string{key},
				Resources:  []string{key},
				Actions:    op.Actions(),
			},
			Expected: []expectation{{Principal: key, Resource: key, Actions: expected}},
		})
	}
}

// realize builds a principal and a resource meant to make each check hold
// or not as given.
func realize(leaves []policy.Leaf, given map[string]bool) (principal, resource) {
	p := principal{ID: actorID, Roles: []string{RoleUser}, Attr: principalAttr{Grants: []grant{}}}
	r := resource{
		ID: "subject",
		Attr: resourceAttr{
			Subject: otherID, Coaches: []string{}, Country: "US", Relations: []string{},
		},
	}
	for i := range leaves {
		apply(&p, &r, &leaves[i], given[leaves[i].Var])
	}
	return p, r
}

// apply makes check l hold or not on p and r.
func apply(p *principal, r *resource, l *policy.Leaf, holds bool) {
	switch l.Kind {
	case policy.LeafServiceAdmin:
		if holds {
			p.Roles = []string{RoleService}
		}
	case policy.LeafPush:
		if holds {
			p.Roles = []string{RolePush}
		}
	case policy.LeafRelation:
		if holds {
			r.Attr.Relations = append(r.Attr.Relations, l.Relation)
		}
	case policy.LeafLogin, policy.LeafLoginStrict:
		p.Attr.LoggedIn = p.Attr.LoggedIn || holds
		if l.Kind == policy.LeafLoginStrict && !holds {
			p.Attr.Phantom = true
		}
	case policy.LeafTask:
		r.Attr.IsTask = holds
	case policy.LeafOkta:
		r.Attr.IsOktaLifecycle = holds
	case policy.LeafSelf:
		if holds {
			r.Attr.Subject = actorID
		}
	case policy.LeafCoach:
		if holds {
			r.Attr.Coaches = []string{actorID}
		}
	case policy.LeafCapability:
		if holds {
			p.Attr.Grants = append(p.Attr.Grants, grantFor(l))
		}
	}
}

// grantFor is a grant satisfying a capability check's matcher, and no
// broader than it needs.
func grantFor(l *policy.Leaf) grant {
	g := grant{Capability: l.Capability, UserKind: "none", Country: "none"}
	switch l.Scope {
	case policy.ScopeGlobal:
		g.Global = true
	case policy.ScopeUser, policy.ScopeUserAll:
		g.UserKind = userKindAll
	case policy.ScopeCountry:
		g.Country = "US"
	case policy.ScopeUserNwea:
		g.UserKind = userKindNwea
	}
	return g
}

// feasible reports whether p and r make every check hold exactly as
// given, by the same templates the policy's CEL states.
func feasible(leaves []policy.Leaf, given map[string]bool, p *principal, r *resource) bool {
	for _, l := range leaves {
		if holds(&l, p, r) != given[l.Var] {
			return false
		}
	}
	return true
}

// holds evaluates one check on p and r, as leafExpr's CEL does.
func holds(l *policy.Leaf, p *principal, r *resource) bool {
	switch l.Kind {
	case policy.LeafLogin:
		return p.Attr.LoggedIn
	case policy.LeafLoginStrict:
		return p.Attr.LoggedIn && !p.Attr.Phantom
	case policy.LeafServiceAdmin:
		return slices.Contains(p.Roles, RoleService)
	case policy.LeafPush:
		return slices.Contains(p.Roles, RolePush)
	case policy.LeafRelation:
		return slices.Contains(r.Attr.Relations, l.Relation)
	case policy.LeafTask:
		return r.Attr.IsTask
	case policy.LeafOkta:
		return r.Attr.IsOktaLifecycle
	case policy.LeafSelf:
		return r.Attr.Subject == p.ID
	case policy.LeafCoach:
		return slices.Contains(r.Attr.Coaches, p.ID)
	case policy.LeafCapability:
		return slices.ContainsFunc(p.Attr.Grants, func(g grant) bool {
			return g.Capability == l.Capability && grantMatches(&g, l.Scope, r)
		})
	}
	return false
}

// grantMatches mirrors scopeTest.
func grantMatches(g *grant, matcher string, r *resource) bool {
	switch matcher {
	case policy.ScopeGlobal:
		return g.Global
	case policy.ScopeUser:
		return g.UserKind == userKindAll || (g.UserKind == userKindNwea && r.Attr.KmapDistrictOwned)
	case policy.ScopeCountry:
		return g.Country == r.Attr.Country
	case policy.ScopeUserAll:
		return g.UserKind == userKindAll
	case policy.ScopeUserNwea:
		return g.UserKind == userKindNwea
	}
	return true
}

// caseName names an assignment: each check's variable, negated when false.
func caseName(leaves []policy.Leaf, given map[string]bool) string {
	parts := make([]string, len(leaves))
	for i, l := range leaves {
		parts[i] = l.Var
		if !given[l.Var] {
			parts[i] = "!" + parts[i]
		}
	}
	return strings.Join(parts, " ")
}

// The Cerbos test suite document, as much of it as the export writes.
type (
	suite struct {
		Name       string               `yaml:"name"`
		Principals map[string]principal `yaml:"principals"`
		Resources  map[string]resource  `yaml:"resources"`
		Tests      []test               `yaml:"tests"`
	}
	principal struct {
		ID    string        `yaml:"id"`
		Roles []string      `yaml:"roles"`
		Attr  principalAttr `yaml:"attr"`
	}
	principalAttr struct {
		LoggedIn bool    `yaml:"loggedIn"`
		Phantom  bool    `yaml:"phantom"`
		Grants   []grant `yaml:"grants"`
	}
	grant struct {
		Capability string `yaml:"capability"`
		Global     bool   `yaml:"global"`
		UserKind   string `yaml:"userKind"`
		Country    string `yaml:"country"`
	}
	resource struct {
		Kind string       `yaml:"kind"`
		ID   string       `yaml:"id"`
		Attr resourceAttr `yaml:"attr"`
	}
	resourceAttr struct {
		Subject           string   `yaml:"subject"`
		Coaches           []string `yaml:"coaches"`
		KmapDistrictOwned bool     `yaml:"kmapDistrictOwned"`
		Country           string   `yaml:"country"`
		IsTask            bool     `yaml:"isTask"`
		IsOktaLifecycle   bool     `yaml:"isOktaLifecycle"`
		Relations         []string `yaml:"relations"`
	}
	test struct {
		Name     string        `yaml:"name"`
		Input    input         `yaml:"input"`
		Expected []expectation `yaml:"expected"`
	}
	input struct {
		Principals []string `yaml:"principals"`
		Resources  []string `yaml:"resources"`
		Actions    []string `yaml:"actions"`
	}
	expectation struct {
		Principal string            `yaml:"principal"`
		Resource  string            `yaml:"resource"`
		Actions   map[string]string `yaml:"actions"`
	}
)
