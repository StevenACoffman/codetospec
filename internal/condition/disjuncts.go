package condition

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// The operators a boolean check is written with.
const (
	opEqual    = "equal"
	opNotEqual = "not_equal"
)

// maxDisjuncts bounds Disjuncts: past it a condition is too branchy for
// anyone to compare its alternatives by eye, and the expansion grows
// exponentially with nested any.
const maxDisjuncts = 64

// Disjuncts is c's disjunctive normal form over its boolean checks: one
// map per alternative way c can match, from each variable that
// alternative needs at a given boolean value ("var equal|not_equal
// true|false", negations pushed to the leaves) to that value. Any other
// comparison constrains no boolean, so it is left out of every
// alternative (its variable stays in Variables). An alternative needing a
// variable both true and false can never match and is dropped; equal
// alternatives are kept once, in the order c writes them. The zero
// Condition has one alternative needing nothing. false when c has more
// than maxDisjuncts alternatives.
func (c *Condition) Disjuncts() ([]map[string]bool, bool) {
	if c.root == nil {
		return []map[string]bool{{}}, true
	}
	out, ok := c.root.disjuncts(false)
	if !ok {
		return nil, false
	}
	return distinct(out), true
}

// disjuncts is n's alternatives, n negated when negated is set; false
// past maxDisjuncts.
func (n *node) disjuncts(negated bool) ([]map[string]bool, bool) {
	switch {
	case n.Not != nil:
		return n.Not.disjuncts(!negated)
	case n.All != nil && !negated, n.Any != nil && negated:
		// A conjunction: all of its parts, or none of an any's.
		return conjoined(n.All, n.Any, negated)
	case n.Any != nil, n.All != nil:
		// A disjunction: any of its parts, or not all of an all's.
		var out []map[string]bool
		for _, part := range slices.Concat(n.Any, n.All) {
			alts, ok := part.disjuncts(negated)
			if !ok || len(out)+len(alts) > maxDisjuncts {
				return nil, false
			}
			out = append(out, alts...)
		}
		return out, true
	}
	if v, ok := n.boolean(); ok {
		return []map[string]bool{{n.Var: v != negated}}, true
	}
	return []map[string]bool{{}}, true
}

// conjoined is the alternatives of the conjunction of parts (the all's,
// or under negation the any's, each negated): every combination of one
// alternative per part that needs no variable both ways.
func conjoined(all, anyOf []node, negated bool) ([]map[string]bool, bool) {
	out := []map[string]bool{{}}
	for _, part := range slices.Concat(all, anyOf) {
		alts, ok := part.disjuncts(negated)
		if !ok {
			return nil, false
		}
		var next []map[string]bool
		for _, a := range out {
			for _, b := range alts {
				if merged, consistent := merge(a, b); consistent {
					next = append(next, merged)
				}
			}
		}
		if len(next) > maxDisjuncts {
			return nil, false
		}
		out = next
	}
	return out, true
}

// merge is the alternative needing what a and b both need; false when
// they need a variable at different values.
func merge(a, b map[string]bool) (map[string]bool, bool) {
	out := maps.Clone(a)
	for k, v := range b {
		if prior, set := out[k]; set && prior != v {
			return nil, false
		}
		out[k] = v
	}
	return out, true
}

// boolean is the value a "var equal|not_equal <bool>" leaf needs its
// variable at; false for any other leaf.
func (n *node) boolean() (bool, bool) {
	if n.Var == "" || n.Var2 != "" || (n.Op != opEqual && n.Op != opNotEqual) {
		return false, false
	}
	var b bool
	if json.Unmarshal(n.Value, &b) != nil {
		return false, false
	}
	return b == (n.Op == opEqual), true
}

// distinct keeps the first of each equal alternative.
func distinct(alts []map[string]bool) []map[string]bool {
	seen := map[string]bool{}
	out := alts[:0:0]
	for _, a := range alts {
		var key strings.Builder
		for _, k := range slices.Sorted(maps.Keys(a)) {
			fmt.Fprintf(&key, "%q=%t;", k, a[k])
		}
		if !seen[key.String()] {
			seen[key.String()] = true
			out = append(out, a)
		}
	}
	return out
}
