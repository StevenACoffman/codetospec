package policy

import "slices"

// Principal kinds: who sends a request, as the exports' loaders tell them
// apart (a Cerbos role, a Cedar policy language principal type).
const (
	PrincipalUser      = "user"
	PrincipalService   = "service" // a service-admin request
	PrincipalPush      = "push"    // a Pub/Sub push whose token validated
	PrincipalAnonymous = "anonymous"
)

// Principals lists every principal kind, sorted.
func Principals() []string {
	return []string{PrincipalAnonymous, PrincipalPush, PrincipalService, PrincipalUser}
}

// MaxPrincipalLeaves bounds the assignments principalsOf walks: 2^16.
const MaxPrincipalLeaves = 16

// principalsOf lists, sorted, the principal kinds some request op allows
// can come from: a kind is in when an assignment of op's checks that kind
// can realize passes every deny. A check holds only for the kinds its
// templates allow (kindsOf). Nil when every kind is in, the operation has
// no deny, or it has too many checks to walk: the blanket allow stands.
func (op *Operation) principalsOf() []string {
	leaves := op.Leaves()
	if len(op.Denies) == 0 || len(leaves) > MaxPrincipalLeaves {
		return nil
	}
	var in []string
	for _, kind := range Principals() {
		if op.allowsSome(leaves, kind) {
			in = append(in, kind)
		}
	}
	if len(in) == len(Principals()) {
		return nil
	}
	return in
}

// allowsSome reports whether an assignment of leaves that kind can
// realize passes every deny of op.
func (op *Operation) allowsSome(leaves []Leaf, kind string) bool {
	given := map[string]bool{}
	for mask := range 1 << len(leaves) {
		realizable := true
		for b := range leaves {
			holds := mask&(1<<b) != 0
			if holds && !slices.Contains(kindsOf(&leaves[b]), kind) {
				realizable = false
				break
			}
			given[leaves[b].Var] = holds
		}
		if realizable &&
			!slices.ContainsFunc(op.Denies, func(d Deny) bool { return d.When.holds(given) }) {
			return true
		}
	}
	return false
}

// kindsOf lists the principal kinds check l can hold for, as the exports'
// templates test them: a service-admin check for a service, a push for a
// push, a check on the caller's account, grants or attributes for a user
// (the loader gives grants only to users); any other (a relation, a
// request kind, a resource's or request's attribute) for every kind.
func kindsOf(l *Leaf) []string {
	switch {
	case l.Kind == LeafServiceAdmin:
		return []string{PrincipalService}
	case l.Kind == LeafPush:
		return []string{PrincipalPush}
	case l.Kind == LeafLogin, l.Kind == LeafLoginStrict, l.Kind == LeafSelf, l.Kind == LeafCoach,
		l.Kind == LeafCapability, l.Kind == LeafAttribute && l.Holder == HolderCaller:
		return []string{PrincipalUser}
	}
	return Principals()
}
