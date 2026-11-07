package policy

import (
	"cmp"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/StevenACoffman/codetospec/internal/locale"
)

// ConceptAccess is who can do what on one concept: the operations whose
// guards or their checks' pins name it ("" for those naming none),
// sorted.
type ConceptAccess struct {
	Concept    string            `json:"concept"`
	Operations []OperationAccess `json:"operations"`
}

// OperationAccess is one enforcement point and what each of its guards
// allows: every guard must allow, so each statement is a requirement.
type OperationAccess struct {
	Operation string        `json:"operation"`
	Guards    []GuardAccess `json:"guards"`
}

// GuardAccess is one guard (its rule, the checks it calls) and its
// statement: when the guard lets the request through, read from the
// guard's condition with each call's meaning or pin. Undecided when it
// can't be stated, Reason saying why (or, with a statement, what to know:
// a stale pin).
type GuardAccess struct {
	Rule      string   `json:"rule"`
	Checks    []string `json:"checks,omitempty"`
	Statement string   `json:"statement,omitempty"`
	Undecided bool     `json:"undecided"`
	Reason    string   `json:"reason,omitempty"`
}

// Reasons a guard has no statement, or a caveat on it.
const (
	ReasonNotPinned = "not pinned"
	ReasonRequest   = "a request predicate the policy model doesn't state"
	ReasonData      = "the guard also tests data the policy model doesn't state"
	ReasonNoCheck   = "the guard names no check"
	ReasonNoCond    = "the guard has no readable condition"
	ReasonItem      = "drops one item, by no pinned filter"
)

// access groups inv's endpoints by concept, each guard stated in t's
// language from cs's pins and reg's phrases (Define has set the checks'
// definitions).
func (inv *Inventory) access(cs Checkers, reg Registry, t *locale.Text) []ConceptAccess {
	defs := map[string]*Definition{}
	for i := range inv.Checks {
		defs[inv.Checks[i].Name] = &inv.Checks[i].Definition
	}
	st := guardStater{cs: cs, defs: defs, reg: reg, t: t}
	byConcept := map[string][]OperationAccess{}
	for i := range inv.Endpoints {
		op, concepts := st.operation(&inv.Endpoints[i])
		for _, c := range concepts {
			byConcept[c] = append(byConcept[c], op)
		}
	}
	out := make([]ConceptAccess, 0, len(byConcept))
	for _, c := range slices.Sorted(maps.Keys(byConcept)) {
		ops := byConcept[c]
		slices.SortFunc(
			ops,
			func(a, b OperationAccess) int { return cmp.Compare(a.Operation, b.Operation) },
		)
		out = append(out, ConceptAccess{Concept: c, Operations: ops})
	}
	return out
}

// guardStater states guards.
type guardStater struct {
	cs   Checkers
	defs map[string]*Definition
	reg  Registry
	t    *locale.Text
}

// operation states each guard of endpoint e and returns the concepts its
// guards and their checks' pins name, [""] when none.
func (st guardStater) operation(e *Endpoint) (OperationAccess, []string) {
	op := OperationAccess{Operation: e.Endpoint}
	concepts := map[string]bool{"": true}
	for i := range e.Guards {
		g := &e.Guards[i]
		op.Guards = append(op.Guards, st.guard(g))
		concepts[g.Concept] = true
		for _, call := range g.Calls {
			if d := st.defs[call.Check]; d != nil {
				for _, c := range d.Concepts {
					concepts[c] = true
				}
			}
		}
	}
	if len(concepts) > 1 {
		delete(concepts, "")
	}
	return op, slices.Sorted(maps.Keys(concepts))
}

// guard states guard g: when it lets the request through, the negation
// of its deny condition, each variable read through the call that became
// it.
func (st guardStater) guard(g *Guard) GuardAccess {
	a := GuardAccess{Rule: g.RuleID}
	for _, call := range g.Calls {
		if !slices.Contains(a.Checks, call.Check) {
			a.Checks = append(a.Checks, call.Check)
		}
	}
	var allow Checker
	var reasons []string
	switch {
	case g.DelegatesTo != "":
		a.Reason = "checked as " + g.DelegatesTo
		return a
	case len(g.Calls) == 0:
		a.Undecided, a.Reason = true, ReasonNoCheck
		return a
	case g.Scope != "":
		allow, reasons = st.itemFilter(g)
	default:
		var deny cond
		if g.condition == "" || json.Unmarshal([]byte(g.condition), &deny) != nil {
			a.Undecided, a.Reason = true, ReasonNoCond
			return a
		}
		var denies Checker
		denies, reasons = st.checker(&deny, g.Calls)
		allow = negated(&denies)
	}
	if len(reasons) > 0 {
		slices.Sort(reasons)
		a.Undecided, a.Reason = true, strings.Join(slices.Compact(reasons), "; ")
		return a
	}
	a.Statement = Statement(&allow, st.reg, st.t)
	var stale []string
	for _, c := range a.Checks {
		if d := st.defs[c]; d != nil && d.Stale {
			stale = append(stale, c)
		}
	}
	if len(stale) > 0 {
		a.Reason = "stale pin: " + strings.Join(stale, ", ")
	}
	return a
}

// itemFilter is the pin of the first call of item guard g that is a
// pinned visibility filter.
func (st guardStater) itemFilter(g *Guard) (Checker, []string) {
	for _, call := range g.Calls {
		if pin, ok := st.cs[call.Check]; ok && pin.Item != "" {
			if filter, matched := pin.For(&call); matched {
				return filter, nil
			}
			return Checker{}, []string{"no case matches its arguments: " + call.Check}
		}
	}
	return Checker{}, []string{ReasonItem}
}

// checker reads condition c as a pin tree over calls' meanings and pins;
// the reasons parts of it can't be read.
func (st guardStater) checker(c *cond, calls []CheckCall) (Checker, []string) {
	var reasons []string
	each := func(parts []cond) []Checker {
		out := make([]Checker, len(parts))
		for i := range parts {
			var why []string
			out[i], why = st.checker(&parts[i], calls)
			reasons = append(reasons, why...)
		}
		return out
	}
	switch {
	case len(c.All) > 0:
		return Checker{All: each(c.All)}, reasons
	case len(c.Any) > 0:
		return Checker{Any: each(c.Any)}, reasons
	case c.Not != nil:
		inner, why := st.checker(c.Not, calls)
		return negated(&inner), why
	case c.Var != "" && c.Op == "equal" && (c.Value == true || c.Value == false):
		holds, reason := st.variable(c.Var, calls)
		if reason != "" {
			return Checker{}, []string{reason}
		}
		if c.Value == false {
			holds = negated(&holds)
		}
		return holds, nil
	}
	return Checker{}, []string{ReasonData}
}

// variable is when condition variable v holds, through the first call
// that became it: its meaning, else its pin; negated for a variable that
// holds when the call fails. The reason it can't be read otherwise.
func (st guardStater) variable(v string, calls []CheckCall) (Checker, string) {
	i := slices.IndexFunc(calls, func(c CheckCall) bool { return c.Var == v })
	if i < 0 {
		return Checker{}, ReasonData
	}
	call := &calls[i]
	var passes Checker
	switch pin, pinned := st.cs[call.Check]; {
	case call.Means == MeansRequest:
		return Checker{}, ReasonRequest + ": " + call.Check
	case call.Means != "":
		passes = Checker{Means: call.Means, Capability: call.Capability, Scope: call.Scope}
	case pinned && pin.Unpinnable != "":
		return Checker{}, fmt.Sprintf("unpinnable (%s): %s", pin.Unpinnable, call.Check)
	case pinned:
		var matched bool
		if passes, matched = pin.For(call); !matched {
			return Checker{}, "no case matches its arguments: " + call.Check
		}
		passes.Approx, passes.Item = false, ""
	default:
		return Checker{}, ReasonNotPinned + ": " + call.Check
	}
	if call.Fails {
		return negated(&passes), ""
	}
	return passes, ""
}

// negated is c's negation with the negations pushed to the leaves (De
// Morgan), a double negation dropped; c is not modified.
func negated(c *Checker) Checker {
	negateAll := func(parts []Checker) []Checker {
		out := make([]Checker, len(parts))
		for i := range parts {
			out[i] = negated(&parts[i])
		}
		return out
	}
	switch {
	case c.Not != nil:
		return *c.Not
	case c.Any != nil:
		return Checker{All: negateAll(c.Any), SubjectArg: c.SubjectArg}
	case c.All != nil:
		return Checker{Any: negateAll(c.All), SubjectArg: c.SubjectArg}
	}
	leaf := *c
	return Checker{Not: &leaf}
}
