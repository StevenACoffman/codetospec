package policy_test

import (
	"strings"
	"testing"

	"github.com/StevenACoffman/codetospec/internal/policy"
)

// TestEquivalent: pins equal by truth table are equivalent however they
// are written (order, distribution, a compound's subject_arg on its
// parts); different ones give the first differing row and the side that
// allows there.
func TestEquivalent(t *testing.T) {
	cs, err := policy.ParseCheckers([]byte(`{
		"a": {"any": [{"means": "service_admin"}, {"all": [{"means": "login"}, {"means": "self"}]}], "subject_arg": 1},
		"a2": {"all": [{"any": [{"means": "service_admin"}, {"means": "login"}]},
			{"any": [{"means": "service_admin"}, {"means": "self", "subject_arg": 1}]}], "note": "distributed"},
		"b": {"any": [{"means": "service_admin"}, {"means": "self", "subject_arg": 1}]},
		"c": {"any": [{"means": "service_admin"}, {"all": [{"means": "login"}, {"means": "self"}]}], "subject_arg": 2},
		"cap": {"means": "capability", "capability": "capabilities.CanView", "scope": "global"},
		"cap2": {"means": "capability", "capability": "CanView", "scope": "global"},
		"filter": {"item": "Note", "means": "self"},
		"n": {"subject_arg": 1, "not": {"means": "self"}},
		"n2": {"not": {"means": "self", "subject_arg": 1}},
		"x": {"unpinnable": "cross_service"},
		"y": {"unpinnable": "data_predicate"}}`))
	if err != nil {
		t.Fatal(err)
	}
	cmp := func(a, b string) policy.Comparison {
		pa, pb := cs[a], cs[b]
		return policy.Equivalent(&pa, &pb)
	}
	for _, pair := range [][2]string{{"a", "a2"}, {"cap", "cap2"}, {"n", "n2"}, {"x", "x"}} {
		if got := cmp(pair[0], pair[1]); !got.Equal {
			t.Errorf("%s ~ %s: %+v, want equal", pair[0], pair[1], got)
		}
	}
	got := cmp("a", "b")
	if got.Equal || got.AllowedBy != policy.SideSecond || len(got.Counterexample) != 3 {
		t.Fatalf("a ~ b: %+v, want b allowing on a row", got)
	}
	for _, l := range got.Counterexample {
		if strings.Contains(l.Leaf, `"self"`) != l.Truth {
			t.Errorf("counterexample %+v: want only self true", got.Counterexample)
		}
	}
	if got = cmp("a", "c"); got.Equal {
		t.Error("a ~ c: equal, though they are about different arguments")
	}
	for _, pair := range [][2]string{{"x", "y"}, {"b", "filter"}} {
		if got = cmp(pair[0], pair[1]); got.Equal || got.Reason == "" {
			t.Errorf("%s ~ %s: %+v, want different with a reason", pair[0], pair[1], got)
		}
	}
}

// TestCompare: each inventory check either file pins gets a comparison;
// a pin one file lacks names that side.
func TestCompare(t *testing.T) {
	inv := definedInventory(t)
	inv.Compare(definitionPins(t), secondPins(t))
	got := map[string]*policy.Comparison{}
	for i := range inv.Checks {
		got[inv.Checks[i].Name] = inv.Checks[i].Definition.Comparison
	}
	if c := got[adminOrSelf]; c == nil || !c.Equal {
		t.Errorf("AdminOrSelf: %+v, want equal", c)
	}
	if c := got[teacher]; c == nil || c.Equal || c.AllowedBy != policy.SideSecond {
		t.Errorf("Teacher: %+v, want the second allowing", c)
	}
	if c := got[looping]; c == nil || c.Missing != policy.SideSecond {
		t.Errorf("Looping: %+v, want missing second", c)
	}
}

// secondPins are a second reader's pins of the definition tests' checks:
// one equal, one different, one of a check the inventory lacks.
func secondPins(t *testing.T) policy.Checkers {
	t.Helper()
	cs, err := policy.ParseCheckers([]byte(`{
		"permissions.AdminOrSelf": {"any": [{"means": "self", "subject_arg": 1}, {"means": "service_admin"}]},
		"permissions.Teacher": {"means": "login"},
		"permissions.Other": {"means": "login"}}`))
	if err != nil {
		t.Fatal(err)
	}
	return cs
}
