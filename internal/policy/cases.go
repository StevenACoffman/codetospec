package policy

import (
	"context"
	"slices"

	"github.com/StevenACoffman/codetospec/internal/condition"
)

// MaxCaseLeaves caps the checks an operation's cases enumerate: 2^6 cases.
const MaxCaseLeaves = 6

// Case is one assignment of truth to an operation's checks, with the
// decision the rules' conditions give it.
type Case struct {
	Given  map[string]bool // whether each check holds, by its Leaf.Var
	Denied bool
}

// Leaves lists the distinct checks of an operation's guards, by condition
// variable, in first-seen order.
func (op *Operation) Leaves() []Leaf {
	var leaves []Leaf
	var walk func(e *Expr)
	walk = func(e *Expr) {
		if e.Check != nil &&
			!slices.ContainsFunc(leaves, func(l Leaf) bool { return l.Var == e.Check.Var }) {
			leaves = append(leaves, *e.Check)
		}
		for i := range e.Args {
			walk(&e.Args[i])
		}
	}
	for i := range op.Denies {
		walk(&op.Denies[i].When)
	}
	return leaves
}

// Cases enumerates every assignment of truth to the operation's checks,
// each decided by the rules' conditions as the extractor wrote them
// (package condition), a compound checker's variable decided from its
// pinned parts (Deny.Defined): the oracle a rendering must agree with, not
// the rendering itself. None for an operation with more than MaxCaseLeaves
// checks; an assignment a condition can't decide is left out. Whether a
// principal can realize an assignment is the renderer's to say.
//
// Ensures: in mask order; deterministic.
func (op *Operation) Cases() []Case {
	leaves := op.Leaves()
	if len(op.Denies) == 0 || len(leaves) > MaxCaseLeaves {
		return nil
	}
	conds := make([]condition.Condition, 0, len(op.Denies))
	for _, d := range op.Denies {
		c, err := condition.Parse([]byte(d.Condition))
		if err != nil {
			return nil
		}
		conds = append(conds, c)
	}
	var cases []Case
	for mask := range 1 << len(leaves) {
		given := map[string]bool{}
		vars := map[string]any{}
		for b, l := range leaves {
			given[l.Var] = mask&(1<<b) != 0
			vars[l.Var] = given[l.Var] != l.Fails // a failure variable holds when the check doesn't
		}
		for _, d := range op.Denies {
			for variable, e := range d.Defined {
				vars[variable] = e.holds(given) // a compound checker's, from its parts
			}
		}
		denied, decided := false, true
		for i := range conds {
			switch conds[i].Evaluate(context.Background(), vars) {
			case condition.Matches:
				denied = true
			case condition.Indeterminate:
				decided = false
			case condition.DoesNotMatch:
			}
		}
		if decided {
			cases = append(cases, Case{Given: given, Denied: denied})
		}
	}
	return cases
}
