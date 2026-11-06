package main

import (
	"strconv"
	"strings"
	"testing"
)

// TestRouteHandlerLines: a GraphQL endpoint's route fact gives its resolver
// method's span, so codetospec can follow calls from it (journeys,
// exposed_by): every guard rule of the endpoint lies inside that span.
func TestRouteHandlerLines(t *testing.T) {
	facts, _, err := Analyze(testInputs("en"))
	if err != nil {
		t.Fatal(err)
	}
	spans := map[string][2]int{} // route id → handler span
	for _, f := range facts {
		if f.Kind != kindRoute {
			continue
		}
		start, end, ok := lineSpan(f.Attrs[attrHandlerLines])
		if !ok || f.Attrs[attrHandlerPath] == "" {
			t.Errorf("route %s: handler_path %q, handler_lines %q",
				f.ID, f.Attrs[attrHandlerPath], f.Attrs[attrHandlerLines])
			continue
		}
		spans[f.ID] = [2]int{start, end}
	}
	if len(spans) == 0 {
		t.Fatal("no route fact: the fixture no longer exercises endpoints")
	}
	guarded := 0
	for _, f := range facts {
		span, isRoute := spans[f.Attrs[attrRoute]]
		if f.Kind != kindRule || !isRoute || strings.Contains(f.Attrs[attrTitle], "(via ") {
			continue // a helper's rule lies in the helper, reached by calls
		}
		start, end, _ := lineSpan(f.Source.Lines)
		if start < span[0] || end > span[1] {
			t.Errorf(
				"rule %s at %s lies outside its handler %d-%d",
				f.ID,
				f.Source.Lines,
				span[0],
				span[1],
			)
		}
		guarded++
	}
	if guarded == 0 {
		t.Fatal("no guard rule names a route: the fixture no longer exercises guards")
	}
}

// lineSpan parses "a-b".
func lineSpan(s string) (int, int, bool) {
	a, b, found := strings.Cut(s, "-")
	start, errA := strconv.Atoi(a)
	end, errB := strconv.Atoi(b)
	return start, end, found && errA == nil && errB == nil
}
