package cedarpolicy

import (
	"fmt"
	"slices"

	"github.com/StevenACoffman/codetospec/internal/policy"
	"github.com/cedar-policy/cedar-go"
	"github.com/cedar-policy/cedar-go/types"
)

// Decider asks one service's exported policies what they decide for a
// persona (policy.Persona), loading its entities as the loader contract
// says: the exported text decides, so a matrix built on it shows exactly
// what the export allows.
type Decider struct {
	ns       string
	policies *cedar.PolicySet
}

// NewDecider parses service's exported policies from spec.
func NewDecider(spec *Spec, service string) (*Decider, error) {
	ps, err := cedar.NewPolicySetFromBytes(service+".cedar", spec.Files[service+".cedar"])
	if err != nil {
		return nil, fmt.Errorf("cedarpolicy: parse the exported policies: %w", err)
	}
	return &Decider{ns: spec.Namespace, policies: ps}, nil
}

// Outcomes of a Decision.
const (
	Allowed   = "allowed"   // whatever the request is about
	Denied    = "denied"    // whatever the request is about
	Depends   = "depends"   // on Decision.Depends
	Undecided = "undecided" // too many request facts to enumerate
)

// Decision is what an operation's policies decide for one persona.
type Decision struct {
	Outcome string
	// Depends are the request facts the outcome turns on, in the order
	// the operation's checks first name them (Outcome Depends).
	Depends []Fact
}

// Kinds of a Fact: a check about the request itself, or what a grant's
// scope is compared with.
const (
	FactCheck   = "check"   // Fact.Check holds
	FactKmap    = "kmap"    // the user concerned is in a district owned through NWEA MAP
	FactCountry = "country" // the user concerned is in the country of the persona's grant
	FactLocale  = "locale"  // the request's locale is the persona's grant's
)

// Fact is one fact about a request that no persona fixes: the user
// concerned is the caller, the caller coaches them, a relation or
// attribute holds, the request comes from a task; or the request matches
// a scoped grant.
type Fact struct {
	Kind  string       `json:"kind"`
	Check *policy.Leaf `json:"check,omitempty"`
}

// maxFacts caps the request facts one decision enumerates: 2^10 requests.
const maxFacts = 10

// Decide is what op's policies, as action on a resource of resourceType
// (Target, or an item kind's ItemType), decide for p: allowed or denied
// whatever the request is about, or depending on which facts.
//
// Ensures: deterministic.
func (d *Decider) Decide(
	op *policy.Operation,
	action, resourceType string,
	p *policy.Persona,
) Decision {
	facts := FactsOf(op)
	if len(facts) > maxFacts {
		return Decision{Outcome: Undecided}
	}
	allowed := make([]bool, 1<<len(facts))
	for mask := range allowed {
		entities, req := d.request(action, resourceType, p, facts, mask)
		decision, _ := cedar.Authorize(d.policies, entities, req)
		allowed[mask] = decision == cedar.Allow
	}
	switch {
	case !slices.Contains(allowed, false):
		return Decision{Outcome: Allowed}
	case !slices.Contains(allowed, true):
		return Decision{Outcome: Denied}
	}
	out := Decision{Outcome: Depends}
	for i, f := range facts {
		for mask := range allowed {
			if allowed[mask] != allowed[mask^(1<<i)] {
				out.Depends = append(out.Depends, f)
				break
			}
		}
	}
	return out
}

// FactsOf are the request facts op's checks read, in the order they first
// name them, each once.
func FactsOf(op *policy.Operation) []Fact {
	var out []Fact
	add := func(f Fact) {
		if !slices.ContainsFunc(out, f.same) {
			out = append(out, f)
		}
	}
	for _, l := range op.Leaves() {
		switch l.Kind {
		case policy.LeafSelf, policy.LeafCoach, policy.LeafRelation, policy.LeafAttribute,
			policy.LeafTask, policy.LeafOkta:
			add(Fact{Kind: FactCheck, Check: &l})
		case policy.LeafCapability:
			switch l.Scope {
			case policy.ScopeUser:
				add(Fact{Kind: FactKmap})
			case policy.ScopeCountry:
				add(Fact{Kind: FactCountry})
			case policy.ScopeLocale, policy.ScopeContent:
				add(Fact{Kind: FactLocale})
			}
		}
	}
	return out
}

// same reports whether f and g are one fact: a check's by what it reads,
// whatever condition variable names it.
func (f Fact) same(g Fact) bool {
	if f.Kind != g.Kind || f.Kind != FactCheck {
		return f.Kind == g.Kind
	}
	a, b := f.Check, g.Check
	return a.Kind == b.Kind && a.Relation == b.Relation && a.Holder == b.Holder &&
		a.Attribute == b.Attribute && (a.Subject != "") == (b.Subject != "") && a.Second == b.Second
}

// Attribute names the loader sets in more than one entity or record.
const (
	attrAttributes = "attributes"
	attrLocale     = "locale"
	attrCapability = "capability"
)

// The values a loaded request takes for the facts a mask sets or clears:
// the persona's grants are on grantCountry and grantLocale.
const (
	grantCountry, otherCountry = "US", "ZZ"
	grantLocale, otherLocale   = "en", "zz"
	callerID, otherUserID      = "caller", "other"
)

// request is the entities and request the loader contract builds for p
// asking action on a resource of resourceType, each of facts holding when
// its bit of mask is set.
func (d *Decider) request(
	action, resourceType string,
	p *policy.Persona,
	facts []Fact,
	mask int,
) (types.EntityMap, cedar.Request) {
	uid := func(typ, id string) types.EntityUID {
		return types.NewEntityUID(types.EntityType(d.ns+"::"+typ), types.String(id))
	}
	principal := uid(principalTypes()[p.Principal], callerID)
	w := loaded{
		principal: principal,
		subject:   uid("User", otherUserID), subject2: uid("User", otherUserID),
		country: otherCountry, locale: otherLocale,
	}
	for i, f := range facts {
		if mask&(1<<i) != 0 {
			w.set(f)
		}
	}

	entity := types.Entity{UID: principal}
	if p.Principal == policy.PrincipalUser {
		parents, attrs := userOf(p, w.callerAttrs, uid)
		entity.Parents, entity.Attributes = parents, attrs
	}
	target := uid(resourceType, "t")
	entities := types.EntityMap{
		principal: entity,
		target: {UID: target, Attributes: types.NewRecord(types.RecordMap{
			"subject":           w.subject,
			"coaches":           types.NewSet(w.coaches...),
			"kmapDistrictOwned": types.Boolean(w.kmap),
			"country":           types.String(w.country),
			"relations":         types.NewSet(w.relations...),
			"subjectRelations":  types.NewSet(w.subjectRelations...),
			attrAttributes:      types.NewSet(w.resourceAttrs...),
			"subject2":          w.subject2,
			"coaches2":          types.NewSet(w.coaches2...),
			"subjectRelations2": types.NewSet(w.subjectRelations2...),
		})},
	}
	return entities, cedar.Request{
		Principal: principal,
		Action:    uid("Action", action),
		Resource:  target,
		Context: types.NewRecord(types.RecordMap{
			"isTask":          types.Boolean(w.task),
			"isOktaLifecycle": types.Boolean(w.okta),
			attrLocale:        types.String(w.locale),
			attrAttributes:    types.NewSet(w.requestAttrs...),
		}),
	}
}

// loaded is what the request facts set on the target and the context.
type loaded struct {
	principal, subject, subject2 types.EntityUID
	coaches, coaches2            []types.Value
	kmap, task, okta             bool
	country, locale              string
	relations, subjectRelations  []types.Value
	subjectRelations2            []types.Value
	callerAttrs, resourceAttrs   []types.Value
	requestAttrs                 []types.Value
}

// set makes fact f hold.
func (w *loaded) set(f Fact) {
	switch f.Kind {
	case FactKmap:
		w.kmap = true
		return
	case FactCountry:
		w.country = grantCountry
		return
	case FactLocale:
		w.locale = grantLocale
		return
	}
	l := f.Check
	switch l.Kind {
	case policy.LeafSelf:
		if l.Second {
			w.subject2 = w.principal
		} else {
			w.subject = w.principal
		}
	case policy.LeafCoach:
		if l.Second {
			w.coaches2 = append(w.coaches2, w.principal)
		} else {
			w.coaches = append(w.coaches, w.principal)
		}
	case policy.LeafTask:
		w.task = true
	case policy.LeafOkta:
		w.okta = true
	case policy.LeafRelation:
		name := types.String(l.Relation)
		switch {
		case l.Subject != "" && l.Second:
			w.subjectRelations2 = append(w.subjectRelations2, name)
		case l.Subject != "":
			w.subjectRelations = append(w.subjectRelations, name)
		default:
			w.relations = append(w.relations, name)
		}
	case policy.LeafAttribute:
		name := types.String(l.Attribute)
		switch l.Holder {
		case policy.HolderCaller:
			w.callerAttrs = append(w.callerAttrs, name)
		case policy.HolderContext:
			w.requestAttrs = append(w.requestAttrs, name)
		default:
			w.resourceAttrs = append(w.resourceAttrs, name)
		}
	}
}

// userOf is a signed-in user persona's groups and attributes: a
// capability group per grant (CapAny for every grant, CapGlobal for a
// global one, CapUserAll and CapNwea for users of a kind, CapExplicit for
// one held itself), its country, locale and content grants on the
// request's grantCountry and grantLocale (a content grant on every
// content kind), and the caller attributes the facts set.
func userOf(
	p *policy.Persona,
	callerAttrs []types.Value,
	uid func(typ, id string) types.EntityUID,
) (types.EntityUIDSet, types.Record) {
	var countryGrants, localeGrants, contentGrants []types.Value
	var groups []types.EntityUID
	for _, g := range p.Grants {
		capability := types.String(g.Capability)
		groups = append(groups, uid("CapAny", g.Capability))
		switch g.Scope {
		case policy.GrantGlobal:
			groups = append(groups, uid("CapGlobal", g.Capability))
		case policy.GrantUsersAll:
			groups = append(groups, uid("CapUserAll", g.Capability))
		case policy.GrantUsersNwea:
			groups = append(groups, uid("CapNwea", g.Capability))
		case policy.GrantCountry:
			countryGrants = append(countryGrants, types.NewRecord(types.RecordMap{
				attrCapability: capability, "country": types.String(grantCountry),
			}))
		case policy.GrantLocale:
			localeGrants = append(localeGrants, types.NewRecord(types.RecordMap{
				attrCapability: capability, attrLocale: types.String(grantLocale),
			}))
		case policy.GrantContent:
			contentGrants = append(contentGrants, types.NewRecord(types.RecordMap{
				attrCapability: capability, attrLocale: types.String(grantLocale),
				"contentKind": types.String(policy.EveryValue),
			}))
		}
		if g.Explicit {
			groups = append(groups, uid("CapExplicit", g.Capability))
		}
	}
	return types.NewEntityUIDSet(groups...), types.NewRecord(types.RecordMap{
		"loggedIn":      types.True,
		"phantom":       types.Boolean(p.Phantom),
		"countryGrants": types.NewSet(countryGrants...),
		"localeGrants":  types.NewSet(localeGrants...),
		"contentGrants": types.NewSet(contentGrants...),
		attrAttributes:  types.NewSet(callerAttrs...),
	})
}
