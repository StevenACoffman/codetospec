package policy

import (
	"encoding/json"
	"maps"
	"slices"
)

// MaxCompareLeaves bounds the truth table Equivalent walks: 2^16 rows.
const MaxCompareLeaves = 16

// Comparison is how a second reader's pin of a check compares with the
// first's: Equal, or a Counterexample (an assignment of the two pins'
// leaves, where AllowedBy says which pin allows: "first" or "second").
// Missing names the side with no pin ("first", "second"); Reason why the
// pins weren't compared (an unpinnable side, a different item, too many
// leaves).
type Comparison struct {
	Equal          bool        `json:"equal"`
	Missing        string      `json:"missing,omitempty"`
	Reason         string      `json:"reason,omitempty"`
	Counterexample []LeafTruth `json:"counterexample,omitempty"`
	AllowedBy      string      `json:"allowed_by,omitempty"`
	// Case names the first differing case of pins keyed on argument
	// values ("2=\"admin\"").
	Case string `json:"case,omitempty"`
}

// LeafTruth is one leaf of a counterexample (its canonical text, the
// leaf's JSON with the subject_arg it inherits) and its truth.
type LeafTruth struct {
	Leaf  string `json:"leaf"`
	Truth bool   `json:"truth"`
}

// Sides of a comparison (Comparison.Missing, AllowedBy).
const (
	SideFirst  = "first"
	SideSecond = "second"
)

// Equivalent compares pins a and b by truth table over their canonical
// leaves: two leaves are the same when every field the policy model reads
// (meaning, scope, capability or its argument, relation, attribute and
// holder, the subject argument, passed down from a compound as
// ParseCheckers passes it) is equal.
// Notes and digests aside, a different item or unpinnable reason makes
// the pins different without a table.
//
// Ensures: deterministic; the counterexample is the first differing row,
// leaves sorted by text, false before true.
func Equivalent(a, b *Checker) Comparison {
	switch {
	case a.Unpinnable != "" || b.Unpinnable != "":
		equal := a.Unpinnable == b.Unpinnable
		return Comparison{
			Equal:  equal,
			Reason: unlessEqual(equal, "unpinnable on one side or for another reason"),
		}
	case a.Item != b.Item:
		return Comparison{Reason: "the pins filter different items"}
	case (a.Call == nil) != (b.Call == nil):
		return Comparison{Reason: "only one pin states a call-level meaning", Case: "call"}
	case a.Call != nil:
		if cmp := Equivalent(a.Call, b.Call); !cmp.Equal {
			cmp.Case = "call"
			return cmp
		}
	case a.Cases != nil || b.Cases != nil:
		return equivalentCases(a, b)
	}
	na, nb := a.withSubjectArg(), b.withSubjectArg() // as ParseCheckers reads them
	a, b = &na, &nb
	leaves := map[string]bool{}
	collectLeaves(a, leaves)
	collectLeaves(b, leaves)
	names := slices.Sorted(maps.Keys(leaves))
	if len(names) > MaxCompareLeaves {
		return Comparison{Reason: "too many leaves to compare"}
	}
	truth := map[string]bool{}
	for row := range 1 << len(names) {
		for i, name := range names {
			truth[name] = row&(1<<(len(names)-1-i)) != 0
		}
		first, second := allows(a, truth), allows(b, truth)
		if first == second {
			continue
		}
		out := Comparison{AllowedBy: SideSecond}
		if first {
			out.AllowedBy = SideFirst
		}
		for _, name := range names {
			out.Counterexample = append(
				out.Counterexample,
				LeafTruth{Leaf: name, Truth: truth[name]},
			)
		}
		return out
	}
	return Comparison{Equal: true}
}

// equivalentCases compares two pins keyed on argument values case by
// case, in order: the same arguments, then equivalent meanings; the first
// differing case named in Case.
func equivalentCases(a, b *Checker) Comparison {
	if len(a.Cases) != len(b.Cases) {
		return Comparison{Reason: "the pins' cases differ"}
	}
	for i := range a.Cases {
		ka, kb := &a.Cases[i], &b.Cases[i]
		if ka.key() != kb.key() {
			return Comparison{Reason: "the pins' cases differ", Case: ka.key()}
		}
		if cmp := Equivalent(&ka.Checker, &kb.Checker); !cmp.Equal {
			cmp.Case = ka.key()
			return cmp
		}
	}
	return Comparison{Equal: true}
}

// unlessEqual is reason when the pins differ, "" when equal.
func unlessEqual(equal bool, reason string) string {
	if equal {
		return ""
	}
	return reason
}

// leafText is leaf c's canonical text: the fields the policy model reads.
func leafText(c *Checker) string {
	leaf := Checker{
		Means: c.Means, Scope: c.Scope, Capability: goName(c.Capability),
		CapabilityArg: c.CapabilityArg, Relation: c.Relation, Of: c.Of,
		Attribute: c.Attribute, SubjectArg: c.SubjectArg,
	}
	data, err := json.Marshal(&leaf)
	if err != nil {
		return c.Means
	}
	return string(data)
}

// collectLeaves adds the canonical text of each of c's leaves to out.
func collectLeaves(c *Checker, out map[string]bool) {
	if c.Means != "" {
		out[leafText(c)] = true
		return
	}
	for i := range c.Any {
		collectLeaves(&c.Any[i], out)
	}
	for i := range c.All {
		collectLeaves(&c.All[i], out)
	}
	if c.Not != nil {
		collectLeaves(c.Not, out)
	}
}

// allows evaluates c under truth, each leaf's truth by its canonical
// text.
func allows(c *Checker, truth map[string]bool) bool {
	switch {
	case c.Means != "":
		return truth[leafText(c)]
	case c.Not != nil:
		return !allows(c.Not, truth)
	case c.Any != nil:
		return slices.ContainsFunc(c.Any, func(p Checker) bool { return allows(&p, truth) })
	}
	for i := range c.All {
		if !allows(&c.All[i], truth) {
			return false
		}
	}
	return true
}

// Compare sets, on each check of inv either first or second pins, how the
// two pins compare (Definition.Comparison): the person's pins and a second
// reader's, written blind.
func (inv *Inventory) Compare(first, second Checkers) {
	for i := range inv.Checks {
		c := &inv.Checks[i]
		a, inFirst := first[c.Name]
		b, inSecond := second[c.Name]
		switch {
		case !inFirst && !inSecond:
			continue
		case !inFirst:
			c.Definition.Comparison = &Comparison{Missing: SideFirst}
		case !inSecond:
			c.Definition.Comparison = &Comparison{Missing: SideSecond}
		default:
			cmp := Equivalent(&a, &b)
			c.Definition.Comparison = &cmp
		}
	}
}
