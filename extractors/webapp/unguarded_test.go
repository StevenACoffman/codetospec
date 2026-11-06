package main

import "testing"

// TestUnguardedFacts: a public resolver method with a schema field and no
// recognized guard or marker gets an unguarded fact citing the method, so
// the inventory tells "no check" apart from "not a resolver"; a method with
// a guard, a marker or no schema field gets none.
func TestUnguardedFacts(t *testing.T) {
	facts, _, err := Analyze(testInputs("en"))
	if err != nil {
		t.Fatal(err)
	}
	unguarded := factsOfKind(facts, kindUnguarded)
	if len(unguarded) == 0 {
		t.Fatal("no unguarded fact: the fixture no longer has an unguarded resolver")
	}
	for _, u := range unguarded {
		t.Run(u.ID, func(t *testing.T) {
			if u.Attrs[attrOperation] == "" {
				t.Errorf("no operation: %+v", u.Attrs)
			}
			if inside := rulesWithin(facts, u.Source); len(inside) > 0 {
				t.Errorf("rules %v lie inside a method called unguarded", inside)
			}
		})
	}
}

// factsOfKind returns the facts of one kind, in order.
func factsOfKind(facts []Fact, kind string) []Fact {
	var out []Fact
	for _, f := range facts {
		if f.Kind == kind {
			out = append(out, f)
		}
	}
	return out
}

// rulesWithin returns the IDs of the rule facts cited inside span.
func rulesWithin(facts []Fact, span Ref) []string {
	start, end, _ := lineSpan(span.Lines)
	var ids []string
	for _, f := range factsOfKind(facts, kindRule) {
		if a, b, _ := lineSpan(
			f.Source.Lines,
		); f.Source.Path == span.Path && a >= start &&
			b <= end {
			ids = append(ids, f.ID)
		}
	}
	return ids
}

// TestCrossTypeDelegation: a resolver method that only calls another
// resolver type's method, on a receiver it builds in place, is checked as
// that method (as a same-type alias is), never reported unguarded; its rule
// names the method it delegates to.
func TestCrossTypeDelegation(t *testing.T) {
	facts, _, err := Analyze(testInputs("en"))
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range factsOfKind(facts, kindUnguarded) {
		if u.Attrs[attrOperation] == "User.aiSummary" {
			t.Fatalf("User.aiSummary reported unguarded: %+v", u)
		}
	}
	for _, f := range factsOfKind(facts, kindRule) {
		if f.Source.Path == "resolvers/kaid.go" &&
			f.Attrs[attrDelegatesTo] == "aiProfileResolver.Summary" {
			return
		}
	}
	t.Error("no rule of User.aiSummary delegating to aiProfileResolver.Summary")
}
