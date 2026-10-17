package grounding_test

import (
	"fmt"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/graph"
	"github.com/StevenACoffman/codetospec/internal/grounding"
)

// plan.go: the rule cites lines 1-2; the values sit further down.
func planFile() string {
	lines := make([]string, 60)
	for i := range lines {
		lines[i] = fmt.Sprintf("x%d := f()", i+1)
	}
	lines[9] = `tier := "PRO"`                          // line 10, near
	lines[11] = `retries := 7`                          // line 12, near, a lone digit
	lines[19], lines[24] = `a := "GOLD"`, `b := "GOLD"` // twice
	lines[54] = `limit := "FAR"`                        // line 55, too far
	return strings.Join(lines, "\n")
}

func TestAnnotateWidensNarrowCitations(t *testing.T) {
	fsys := fstest.MapFS{"plan.go": {Data: []byte(planFile())}}
	cases := map[string]struct {
		value       string
		wantStatus  grounding.Status
		wantWidened string
	}{
		"one location nearby":   {`"PRO"`, grounding.StatusGrounded, "plan.go:10-10"},
		"two locations":         {`"GOLD"`, grounding.StatusUngrounded, ""},
		"too far from the code": {`"FAR"`, grounding.StatusUngrounded, ""},
		"a lone digit":          {"7", grounding.StatusUngrounded, ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			node := graph.Node{
				ID:      "rule.plan.tier",
				Type:    graph.TypeRule,
				Body:    "**Requirement (EARS)**: WHEN the plan is " + tc.value + ", the system shall upgrade it.",
				Sources: []extract.Ref{{Path: "plan.go", Lines: "1-2"}},
			}
			got := grounding.Annotate(extract.Roots{Src: fsys}, []graph.Node{node}, grounding.Options{DecimalSeparator: '.'})[0]
			if got.Extra["grounding"] != string(tc.wantStatus) ||
				got.Extra[graph.ExtraGroundingWidened] != tc.wantWidened {
				t.Fatalf("grounding = %q, widened = %q, sources %v; want %q, %q",
					got.Extra["grounding"], got.Extra[graph.ExtraGroundingWidened], got.Sources,
					tc.wantStatus, tc.wantWidened)
			}
			if tc.wantWidened == "" && len(got.Sources) != 1 {
				t.Fatalf("sources changed without widening: %v", got.Sources)
			}
		})
	}
}
