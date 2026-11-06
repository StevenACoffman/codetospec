package policy

import (
	"cmp"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/graph"
)

// Model is the authorization model the policy exports render: one place
// decides which guards translate, so the Cedar policy language and Cerbos
// outputs never disagree on what they cover. Each translated guard is a
// deny: the handler runs unless a guard's condition holds, so the export is
// a blanket allow per operation plus one deny per guard, each traced to its
// rule. A decision never depends on guard order: every guard must pass.
type Model struct {
	Operations   []Operation  `json:"operations"`
	Items        []ItemPolicy `json:"items,omitempty"`
	Capabilities []Capability `json:"capabilities,omitempty"`
	Roles        []Role       `json:"roles,omitempty"`
	Skipped      []Skip       `json:"skipped,omitempty"`
}

// Operation is one enforcement point and its denies.
type Operation struct {
	Action string `json:"action"` // the operation, "User.emails"
	Point  string `json:"point"`  // its endpoint id, or the operation
	Denies []Deny `json:"denies,omitempty"`
	// Aliases are the operations whose handler only delegates to this
	// one's: checked as it, sorted.
	Aliases []string `json:"aliases,omitempty"`
	// Subject is where the user its checks are about comes from in the
	// request (Leaf.SubjectFrom): what the PEP sets resource.subject from.
	// Unset when no check is about a user.
	Subject string `json:"subject,omitempty"`
	// Open says the handler is marked open, Unchecked marked todo:
	// both are the blanket allow alone.
	Open      bool `json:"open,omitempty"`
	Unchecked bool `json:"unchecked,omitempty"`
}

// ViewAction is an item policy's one action: seeing one item.
const ViewAction = "view"

// ItemPolicy is the decision on one item of Kind that the pinned
// visibility filters of exported operations make: ViewAction, denied by
// each filter (one Deny, traced to its first rule) when the item isn't
// visible; Point is the kind. Each Deny's Condition is "the filter's
// check fails", its check decided from the pinned parts (Deny.Defined).
type ItemPolicy struct {
	Kind    string   `json:"kind"`
	Filters []string `json:"filters"` // the operations whose results it filters, sorted
	Operation
}

// Actions are the actions an operation's policies apply to: its own,
// then its aliases'.
func (op *Operation) Actions() []string {
	return append([]string{op.Action}, op.Aliases...)
}

// Deny is one guard: the request is denied when When holds. Condition is
// the rule's condition as the extractor wrote it, When's source: tests
// evaluate it (package condition) as the oracle a rendering must agree with.
type Deny struct {
	RuleID    string      `json:"rule"`
	Source    extract.Ref `json:"source"`
	When      Expr        `json:"when"`
	Condition string      `json:"condition"`
	// Defined maps each condition variable a compound checker pinned to
	// its expression in When: the oracle decides the variable from it.
	Defined map[string]Expr `json:"defined,omitempty"`
}

// Operators of an Expr.
const (
	OpAll   = "all"
	OpAny   = "any"
	OpNot   = "not"
	OpCheck = "check"
)

// holds evaluates e with each check true as given, by Leaf.Var.
func (e *Expr) holds(given map[string]bool) bool {
	switch e.Op {
	case OpAll, OpAny:
		for i := range e.Args {
			if e.Args[i].holds(given) == (e.Op == OpAny) {
				return e.Op == OpAny
			}
		}
		return e.Op == OpAll
	case OpNot:
		return !e.Args[0].holds(given)
	case OpCheck:
		return given[e.Check.Var]
	}
	return false
}

// Expr is a boolean expression over checks: all, any, not, or one check.
type Expr struct {
	Op    string `json:"op"`
	Args  []Expr `json:"args,omitempty"`
	Check *Leaf  `json:"check,omitempty"`
}

// Kinds of Leaf: the checks the exports translate (the pinned vocabulary).
const (
	LeafLogin        = "login"         // the caller is logged in
	LeafLoginStrict  = "login_strict"  // logged in, phantoms refused
	LeafCapability   = "capability"    // a capability on a pinned matcher
	LeafSelf         = "self"          // the caller is Subject
	LeafCoach        = "coach"         // the caller coaches Subject
	LeafServiceAdmin = "service_admin" // a service-admin request
	LeafTask         = "task"          // a task request
	LeafOkta         = "okta"          // an Okta lifecycle request
	LeafPush         = "push"          // a Pub/Sub push, its token validated
	LeafRelation     = "relation"      // the caller holds Relation to the resource (or to Subject)
	LeafAttribute    = "attribute"     // Holder has the boolean Attribute
)

// Holders of an attribute check (Leaf.Holder): the caller, the request's
// resource (or item), the request itself.
const (
	HolderCaller   = "caller"
	HolderResource = "resource"
	HolderContext  = "context"
)

// Leaf is one check of the pinned vocabulary, with its operands.
type Leaf struct {
	Kind       string `json:"kind"`
	Var        string `json:"var"`                  // the condition variable it translates
	Capability string `json:"capability,omitempty"` // its stored name
	Scope      string `json:"scope,omitempty"`      // a capability's: ScopeAny, ScopeGlobal, ScopeUser, ScopeCountry
	Subject    string `json:"subject,omitempty"`    // the user a self, coach or user-scope check is about
	// SubjectFrom is where Subject comes from in the request
	// (CheckCall.SubjectFrom); unset for a local.
	SubjectFrom string `json:"subject_from,omitempty"`
	Relation    string `json:"relation,omitempty"` // a relation's name, a registry entry
	// Holder and Attribute are an attribute check's holder (Holder*) and
	// the name of the boolean attribute the PIP serves.
	Holder    string `json:"holder,omitempty"`
	Attribute string `json:"attribute,omitempty"`
	// Fails says Var holds when the check fails (a bound error): the
	// condition reads the check's negation.
	Fails bool `json:"fails,omitempty"`
}

// SubjectItem is the SubjectFrom of a check in an item policy about a
// user: the item's own (its resource's subject attribute).
const SubjectItem = "item"

// Meanings of a check call (CheckCall.Means): the extractor's statement
// of what a check is, in this vocabulary. Each but MeansRequest is a
// Leaf kind of the same name.
const (
	MeansLogin        = LeafLogin
	MeansLoginStrict  = LeafLoginStrict
	MeansCapability   = LeafCapability
	MeansSelf         = LeafSelf
	MeansCoach        = LeafCoach
	MeansServiceAdmin = LeafServiceAdmin
	MeansTask         = LeafTask
	MeansOkta         = LeafOkta
	MeansPush         = LeafPush
	MeansRelation     = LeafRelation  // pinned only (Checkers)
	MeansAttribute    = LeafAttribute // pinned only (Checkers)
	MeansRequest      = "request"     // another predicate on the request: not translated
)

// The scopes of a capability check (CheckCall.Scope, Leaf.Scope) whose
// meaning the exports' templates state: any grant of the capability, a
// global one, one on users, one on the subject's country. A check on
// another scope isn't translated.
const (
	ScopeAny     = "any"
	ScopeGlobal  = "global"
	ScopeUser    = "user"
	ScopeCountry = "country"
	// ScopeUserAll and ScopeUserNwea are grants on users of one kind,
	// "all" or "nwea-map", whatever the subject: what a service's own
	// checker may test (districts' _checkActorCapabilityScopes).
	ScopeUserAll  = "user_all"
	ScopeUserNwea = "user_nwea"
	// ScopeLocale is a grant for the request's locale; ScopeExplicit any
	// grant but those a role (admin's) holds implicitly.
	ScopeLocale   = "locale"
	ScopeExplicit = "explicit"
)

func pinnedScopes() []string {
	return []string{
		ScopeAny, ScopeGlobal, ScopeUser, ScopeCountry,
		ScopeUserAll, ScopeUserNwea, ScopeLocale, ScopeExplicit,
	}
}

// Capability is a registered capability.
type Capability struct {
	Name      string   `json:"name"` // stored name, "can_view_user_identity"
	GoName    string   `json:"go_name"`
	ScopeKeys []string `json:"scope_keys,omitempty"`
}

// Role is a role definition: granting it grants these capabilities (at the
// grant's scope), by stored name.
type Role struct {
	Name         string   `json:"name"`
	Capabilities []string `json:"capabilities"`
}

// Skip is a guard the model doesn't translate, and why.
type Skip struct {
	RuleID string `json:"rule"`
	Reason string `json:"reason"`
}

// BuildModel translates the extractor guards of rules (rule nodes carrying
// checks) into denies over the pinned vocabulary, joining capabilities to
// their stored names through facts' capability facts; roles come from the
// role rule facts. A guard outside the vocabulary is skipped with a reason,
// never guessed, and so is every other guard of its operation: an
// operation is in the model whole or not at all, since its blanket allow
// would otherwise grant what the skipped guard denies. For the same reason
// an operation is left out when an extractor rule of it names no check (a
// delegation, a guard the extractor can't trace), or when its resolver
// reaches, within MaxDepth direct calls (facts' call facts), a recognized
// check its guards don't call. An operation whose every extractor rule
// delegates to one exported operation is that operation's alias. A check
// the extractor gave no meaning takes checkers' (nil for none). LLM rules
// are never translated.
//
// Ensures: operations sorted by action, denies by rule, skips by rule;
// every extractor rule with an operation is in an operation or a skip.
func BuildModel(facts []extract.Fact, rules []graph.Node, checkers Checkers) Model {
	caps := capabilitiesOf(facts)
	stored := map[string]string{} // Go name → stored name
	for _, c := range caps {
		stored[c.GoName] = c.Name
	}
	m := Model{Capabilities: caps, Roles: rolesOf(facts, stored)}
	ops := map[string]*opGuards{}
	for i := range rules {
		n := &rules[i]
		action := n.Extra[graph.ExtraOperation]
		g, ok := guardOf(n)
		switch {
		case !ok && action != "" && n.Extra[graph.ExtraExtractedBy] != "":
			opOf(ops, action).silent(n)
			continue
		case !ok:
			continue
		case action == "":
			m.Skipped = append(m.Skipped, Skip{RuleID: n.ID, Reason: "no operation"})
			continue
		}
		opOf(ops, action).add(n, &g, translator{stored: stored, checkers: checkers})
	}
	idx := indexFacts(facts)
	exported := map[string]*Operation{}
	var aliases []string
	for _, action := range slices.Sorted(maps.Keys(ops)) {
		if ops[action].isAlias() {
			aliases = append(aliases, action)
			continue
		}
		op, skips := ops[action].settle(idx)
		m.Skipped = append(m.Skipped, skips...)
		if op != nil {
			exported[action] = op
		}
	}
	m.Skipped = append(m.Skipped, resolveAliases(ops, exported, aliases)...)
	for _, action := range slices.Sorted(maps.Keys(exported)) {
		m.Operations = append(m.Operations, *exported[action])
	}
	var items []itemGuard
	for action := range exported {
		items = append(items, ops[action].items...)
	}
	m.Items = itemPolicies(items)
	slices.SortFunc(m.Skipped, func(a, b Skip) int { return cmp.Compare(a.RuleID, b.RuleID) })
	return m
}

// subjectOf is where the one user op's checks are about comes from in the
// request ("" when none is about a user), or why it can't be bound: a
// check about a local, checks about different users (one
// resource.subject can't stand for two).
func subjectOf(op *Operation) (string, string) {
	var froms []string
	unbound := ""
	var walk func(e *Expr)
	walk = func(e *Expr) {
		if l := e.Check; l != nil && l.aboutUser() {
			if l.SubjectFrom == "" && unbound == "" {
				unbound = cmp.Or(l.Subject, "an unknown user")
			}
			if l.SubjectFrom != "" && !slices.Contains(froms, l.SubjectFrom) {
				froms = append(froms, l.SubjectFrom)
			}
		}
		for i := range e.Args {
			walk(&e.Args[i])
		}
	}
	for i := range op.Denies {
		walk(&op.Denies[i].When)
	}
	slices.Sort(froms)
	switch {
	case unbound != "":
		return "", "a check is about " + unbound + ", not a request value"
	case len(froms) > 1:
		return "", "checks about different users: " + strings.Join(froms, ", ")
	}
	return strings.Join(froms, ""), ""
}

// aboutUser reports whether l's template reads the resource's user: a
// self or coach check, a capability on a user scope (the user's data).
func (l *Leaf) aboutUser() bool {
	return l.Kind == LeafSelf || l.Kind == LeafCoach ||
		(l.Kind == LeafCapability && l.Scope == ScopeUser) ||
		(l.Kind == LeafRelation && l.Subject != "")
}

// resolveAliases makes each of aliases (actions of ops, sorted) an alias
// of its target when the target is exported and about no user, and
// returns a skip per delegation of each other.
func resolveAliases(
	ops map[string]*opGuards,
	exported map[string]*Operation,
	aliases []string,
) []Skip {
	var skips []Skip
	for _, action := range aliases {
		o := ops[action]
		target := exported[o.target]
		reason := "delegation to " + o.target + ", which is not exported"
		switch {
		case target != nil && target.Subject == "":
			target.Aliases = append(target.Aliases, action)
			continue
		case target != nil:
			// The alias's own arguments are unbound: its target's binding
			// names the target's.
			reason = "delegation to " + o.target + ", whose checks are about a user of its request"
		}
		for _, id := range o.delegations {
			skips = append(skips, Skip{RuleID: id, Reason: reason})
		}
	}
	return skips
}

// itemPolicies gathers item guards into one policy per kind, one deny per
// filter check.
//
// Ensures: sorted by kind, denies by check, filters by operation; the same
// whatever the order of guards.
func itemPolicies(guards []itemGuard) []ItemPolicy {
	byKind := map[string]map[string][]itemGuard{} // kind → check → guards
	for _, ig := range guards {
		if byKind[ig.kind] == nil {
			byKind[ig.kind] = map[string][]itemGuard{}
		}
		byKind[ig.kind][ig.check] = append(byKind[ig.kind][ig.check], ig)
	}
	var out []ItemPolicy
	for _, kind := range slices.Sorted(maps.Keys(byKind)) {
		p := ItemPolicy{Kind: kind, Operation: Operation{Action: ViewAction, Point: kind}}
		for _, check := range slices.Sorted(maps.Keys(byKind[kind])) {
			filtering := byKind[kind][check]
			slices.SortFunc(
				filtering,
				func(a, b itemGuard) int { return cmp.Compare(a.rule, b.rule) },
			)
			first := &filtering[0]
			p.Denies = append(p.Denies, Deny{
				RuleID:    first.rule,
				Source:    first.source,
				When:      Expr{Op: OpNot, Args: []Expr{first.visible}},
				Condition: fmt.Sprintf(`{"not":{"var":%q,"op":"equal","value":true}}`, check),
				Defined:   map[string]Expr{check: first.visible},
			})
			for _, ig := range filtering {
				if !slices.Contains(p.Filters, ig.op) {
					p.Filters = append(p.Filters, ig.op)
				}
			}
		}
		slices.Sort(p.Filters)
		out = append(out, p)
	}
	return out
}

// silentReason is why an extractor rule naming no check can't be
// exported: what it delegates to, or that its guard names none.
func silentReason(n *graph.Node) string {
	if to := n.Extra[graph.ExtraDelegatesTo]; to != "" {
		return "delegation to " + to
	}
	return "guard names no check"
}

// opGuards gathers one operation's guards while the model is built.
type opGuards struct {
	op      Operation
	skips   []Skip        // guards that don't translate
	guards  []string      // every guard's rule id
	spans   []extract.Ref // every guard's citation
	checked []string      // the checks the guards call
	// delegations are the rules delegating to target, an operation of
	// the service.
	delegations []string
	target      string
	items       []itemGuard // guards whose pinned filter drops items
}

// itemGuard is one guard dropping the items its pinned filter check
// doesn't make visible, of kind.
type itemGuard struct {
	op      string // the operation whose results it filters
	rule    string
	source  extract.Ref
	check   string
	kind    string
	visible Expr
}

// opOf returns the operation action, created when new; its point is the
// first guard's endpoint (add).
func opOf(ops map[string]*opGuards, action string) *opGuards {
	o := ops[action]
	if o == nil {
		o = &opGuards{op: Operation{Action: action}}
		ops[action] = o
	}
	return o
}

// skip records that rule doesn't translate, and why.
func (o *opGuards) skip(rule, reason string) {
	o.skips = append(o.skips, Skip{RuleID: rule, Reason: reason})
}

// silent records extractor rule n, which names no check: a delegation to
// an operation of the service (one target per operation; another makes
// it a skip), or a skip with silentReason.
func (o *opGuards) silent(n *graph.Node) {
	target := n.Extra[graph.ExtraDelegatesToOperation]
	if target == "" || (o.target != "" && o.target != target) {
		o.skip(n.ID, silentReason(n))
		return
	}
	o.target = target
	o.delegations = append(o.delegations, n.ID)
}

// isAlias reports whether every rule of the operation delegates to its
// target.
func (o *opGuards) isAlias() bool {
	return len(o.delegations) > 0 && len(o.guards) == 0 && len(o.skips) == 0
}

// add records guard g of rule n: a marker, a deny, or a skip.
func (o *opGuards) add(n *graph.Node, g *Guard, t translator) {
	if o.op.Point == "" {
		o.op.Point = firstOr(g.endpoints, o.op.Action)
	}
	o.guards = append(o.guards, n.ID)
	o.spans = append(o.spans, g.Source)
	for _, c := range g.Calls {
		o.checked = append(o.checked, c.Check)
	}
	switch g.marker {
	case markerOpen:
		o.op.Open = true
		return
	case markerTodo:
		o.op.Unchecked = true
		return
	}
	if g.Scope != "" {
		if ig, ok := t.item(n, g); ok {
			o.items = append(o.items, ig)
			return
		}
	}
	when, reason := t.translate(n, g)
	if reason != "" {
		o.skip(n.ID, reason)
		return
	}
	o.op.Denies = append(o.op.Denies, Deny{
		RuleID: n.ID, Source: g.Source, When: when, Condition: n.Extra[graph.ExtraCondition],
		Defined: t.defined,
	})
}

// settle returns the operation when every guard translated and its
// resolvers reach no other check, else nil and a skip per guard.
func (o *opGuards) settle(idx *factIndex) (*Operation, []Skip) {
	for _, id := range o.delegations { // beside other guards: not an alias
		o.skip(id, "delegation to "+o.target+" beside other guards")
	}
	if len(o.skips) > 0 {
		// Exporting the other guards alone would allow what the skipped
		// one denies: the whole operation stays out, its guards listed.
		skips := o.skips
		other := "another guard of " + o.op.Action + " is skipped"
		for _, d := range o.op.Denies {
			skips = append(skips, Skip{RuleID: d.RuleID, Reason: other})
		}
		for _, ig := range o.items {
			skips = append(skips, Skip{RuleID: ig.rule, Reason: other})
		}
		return nil, skips
	}
	var reason string
	if below := idx.checksBelow(o.spans, o.checked); len(below) > 0 {
		reason = "checks below its guard: " + strings.Join(below, ", ")
	} else {
		o.op.Subject, reason = subjectOf(&o.op)
	}
	if reason != "" {
		skips := make([]Skip, 0, len(o.guards))
		for _, id := range o.guards {
			skips = append(skips, Skip{RuleID: id, Reason: reason})
		}
		return nil, skips
	}
	slices.SortFunc(o.op.Denies, func(a, b Deny) int { return cmp.Compare(a.RuleID, b.RuleID) })
	return &o.op, nil
}

// translate turns one guard's condition into an Expr; the reason it can't
// otherwise.
func (t *translator) translate(n *graph.Node, g *Guard) (Expr, string) {
	switch {
	case g.Scope != "":
		return Expr{}, "drops one item, not the call"
	case g.DelegatesTo != "":
		return Expr{}, "delegation to " + g.DelegatesTo
	case g.Source.Root != "":
		return Expr{}, "shared middleware"
	}
	raw := n.Extra[graph.ExtraCondition]
	if raw == "" {
		return Expr{}, "no condition"
	}
	var c cond
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		return Expr{}, "malformed condition"
	}
	t.calls = g.Calls
	e := t.expr(&c)
	return e, t.reason
}

// cond is the compact condition notation (see package condition), read
// for translation.
type cond struct {
	All    []cond `json:"all"`
	Any    []cond `json:"any"`
	Not    *cond  `json:"not"`
	Exists string `json:"exists"`
	Var    string `json:"var"`
	Op     string `json:"op"`
	Value  any    `json:"value"`
}

// translator maps a condition's variables to leaves, recording the first
// reason one doesn't map.
type translator struct {
	calls    []CheckCall
	stored   map[string]string
	checkers Checkers
	defined  map[string]Expr // compound checkers' expressions, by variable
	reason   string
	inItem   bool // translating an item filter: users are the item's (SubjectItem)
}

// expr translates one node of the condition.
func (t *translator) expr(c *cond) Expr {
	switch {
	case len(c.All) > 0:
		return Expr{Op: OpAll, Args: t.each(c.All)}
	case len(c.Any) > 0:
		return Expr{Op: OpAny, Args: t.each(c.Any)}
	case c.Not != nil:
		return Expr{Op: OpNot, Args: []Expr{t.expr(c.Not)}}
	case c.Var != "" && c.Op == "equal" && c.Value == true:
		if e, ok := t.check(c.Var); ok {
			return e
		}
		t.fail(fmt.Sprintf("check outside the vocabulary: %s", c.Var))
	default:
		t.fail("comparison outside the vocabulary")
	}
	return Expr{}
}

func (t *translator) each(cs []cond) []Expr {
	out := make([]Expr, len(cs))
	for i := range cs {
		out[i] = t.expr(&cs[i])
	}
	return out
}

func (t *translator) fail(reason string) {
	if t.reason == "" {
		t.reason = reason
	}
}

// item reads guard g of n as an item guard: the first of its calls whose
// checker is a pinned visibility filter, its meaning the item's
// visibility; false when none is or its meaning doesn't translate.
func (t *translator) item(n *graph.Node, g *Guard) (itemGuard, bool) {
	for i := range g.Calls {
		call := &g.Calls[i]
		c, pinned := t.checkers[call.Check]
		if !pinned || c.Item == "" {
			continue
		}
		t.inItem = true
		visible, ok := t.part(&c, call, call.Check)
		t.inItem = false
		return itemGuard{
			op: n.Extra[graph.ExtraOperation], rule: n.ID, source: g.Source,
			check: call.Check, kind: c.Item, visible: visible,
		}, ok
	}
	return itemGuard{}, false
}

// check maps a condition variable to an expression over pinned checks,
// through the one call that became it (two calls of one variable, with
// different operands, can't be told apart), by the call's meaning (else
// its checker's) and with its operands. A variable that holds on its
// call's failure is the negation. A compound checker's expression is
// recorded in t.defined, by variable.
func (t *translator) check(variable string) (Expr, bool) {
	call, ok := t.only(func(c *CheckCall) bool { return c.Var == variable })
	if !ok {
		return Expr{}, false
	}
	pinned := Checker{Means: call.Means, Scope: call.Scope}
	if call.Means == "" {
		pinned = t.checkers[call.Check]
	}
	if pinned.Unpinnable != "" {
		t.fail(fmt.Sprintf("not pinnable (%s): %s", pinned.Unpinnable, call.Check))
		return Expr{}, false
	}
	if pinned.Any == nil && pinned.All == nil && pinned.Not == nil {
		leaf, isLeaf := t.leafOf(&pinned, &call)
		if !isLeaf {
			return Expr{}, false
		}
		leaf.Var, leaf.Fails = variable, call.Fails
		e := Expr{Op: OpCheck, Check: &leaf}
		if call.Fails {
			e = Expr{Op: OpNot, Args: []Expr{e}}
		}
		return e, true
	}
	e, ok := t.part(&pinned, &call, call.Check)
	if !ok {
		return Expr{}, false
	}
	if call.Fails {
		e = Expr{Op: OpNot, Args: []Expr{e}}
	}
	if t.defined == nil {
		t.defined = map[string]Expr{}
	}
	t.defined[variable] = e
	return e, true
}

// part translates c, a compound checker or one of its parts, for call;
// name is the Leaf.Var a check-shaped part takes, its parts numbered
// after it ("permissions.X#2").
func (t *translator) part(c *Checker, call *CheckCall, name string) (Expr, bool) {
	if c.Not != nil {
		e, ok := t.part(c.Not, call, name+"#1")
		return Expr{Op: OpNot, Args: []Expr{e}}, ok
	}
	parts, op := c.Any, OpAny
	if c.All != nil {
		parts, op = c.All, OpAll
	}
	if parts == nil {
		leaf, ok := t.leafOf(c, call)
		leaf.Var = name
		return Expr{Op: OpCheck, Check: &leaf}, ok
	}
	args := make([]Expr, len(parts))
	for i := range parts {
		e, ok := t.part(&parts[i], call, name+"#"+strconv.Itoa(i+1))
		if !ok {
			return Expr{}, false
		}
		args[i] = e
	}
	return Expr{Op: op, Args: args}, true
}

// leafOf is the check c, check-shaped, means for call: a capability's
// stored name from c's capability, else the call's argument.
func (t *translator) leafOf(c *Checker, call *CheckCall) (Leaf, bool) {
	leaf := Leaf{Kind: c.Means}
	switch c.Means {
	case MeansLogin, MeansLoginStrict, MeansServiceAdmin, MeansTask, MeansOkta, MeansPush:
		return leaf, true
	case MeansRelation:
		leaf.Relation = c.Relation
		if c.SubjectArg != nil {
			// A relation to the user an argument names, not to the resource.
			leaf.Subject, leaf.SubjectFrom = t.subject(c, call, "")
			return leaf, leaf.Subject != ""
		}
		return leaf, true
	case MeansAttribute:
		leaf.Holder, leaf.Attribute = c.Of, c.Attribute
		return leaf, true
	case MeansSelf, MeansCoach:
		leaf.Subject, leaf.SubjectFrom = t.subject(c, call, call.Subject)
		return leaf, leaf.Subject != ""
	case MeansCapability:
		leaf.Capability = t.stored[cmp.Or(c.Capability, argCapability(c, call), call.Capability)]
		leaf.Scope = c.Scope
		if leaf.Scope == ScopeUser {
			leaf.Subject, leaf.SubjectFrom = t.subject(c, call, call.MatcherArg)
		}
		return leaf, leaf.Capability != "" && slices.Contains(pinnedScopes(), leaf.Scope)
	}
	return Leaf{}, false
}

// subject is the user a check about one is about, and where it comes
// from: in an item filter the item's own, else the call's (subjectIn).
func (t *translator) subject(c *Checker, call *CheckCall, operand string) (string, string) {
	if t.inItem {
		return "the item's user", SubjectItem
	}
	return c.subjectIn(call, operand)
}

// argCapability is the Go name of the capability c takes from a call
// argument (CapabilityArg): the argument's last selector element
// (capabilities.CanViewUserSettingsData); "" when c names none.
func argCapability(c *Checker, call *CheckCall) string {
	if c.CapabilityArg == nil {
		return ""
	}
	i := *c.CapabilityArg
	if i < 0 || i >= len(call.Args) {
		return ""
	}
	text := call.Args[i].Text
	return text[strings.LastIndex(text, ".")+1:]
}

// only returns the one call of the guard matching, false for none or
// several.
func (t *translator) only(matching func(*CheckCall) bool) (CheckCall, bool) {
	var found []CheckCall
	for i := range t.calls {
		if matching(&t.calls[i]) {
			found = append(found, t.calls[i])
		}
	}
	if len(found) != 1 {
		return CheckCall{}, false
	}
	return found[0], true
}

// capabilitiesOf lists the capability facts, sorted by stored name.
func capabilitiesOf(facts []extract.Fact) []Capability {
	var caps []Capability
	for _, f := range facts {
		if f.Kind != extract.KindCapability {
			continue
		}
		c := Capability{Name: f.Attrs[extract.AttrValue], GoName: f.Attrs[extract.AttrName]}
		if keys := f.Attrs[extract.AttrScopeKeys]; keys != "" {
			c.ScopeKeys = strings.Split(keys, ",")
		}
		caps = append(caps, c)
	}
	slices.SortFunc(caps, func(a, b Capability) int { return cmp.Compare(a.Name, b.Name) })
	return caps
}

// rolesOf lists the role rule facts' roles with their capabilities' stored
// names (a capability with no capability fact keeps its Go name), sorted.
func rolesOf(facts []extract.Fact, stored map[string]string) []Role {
	var roles []Role
	for _, f := range facts {
		name := f.Attrs["role"]
		if f.Kind != extract.KindRule || name == "" {
			continue
		}
		r := Role{Name: name}
		for goName := range strings.SplitSeq(f.Attrs["capabilities"], ",") {
			if goName == "" {
				continue
			}
			r.Capabilities = append(r.Capabilities, cmp.Or(stored[goName], goName))
		}
		roles = append(roles, r)
	}
	slices.SortFunc(roles, func(a, b Role) int { return cmp.Compare(a.Name, b.Name) })
	return roles
}

func firstOr(xs []string, fallback string) string {
	if len(xs) > 0 {
		return xs[0]
	}
	return fallback
}
