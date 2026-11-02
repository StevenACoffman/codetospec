package reducer_test

import (
	"context"
	"strings"
	"testing"

	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/llm"
	"github.com/StevenACoffman/codetospec/internal/mapper"
	"github.com/StevenACoffman/codetospec/internal/reducer"
)

// Conditions the cases use.
const (
	sessionOpen = `{"exists": "session"}`
	// sessionCanonical is sessionOpen in canonical form.
	sessionCanonical = `{"exists":"session"}`
	retried          = `{"var": "attempts", "op": "less_than", "value": 3}`
	retriedCanon     = `{"var":"attempts","op":"less_than","value":3}`
	notValid         = `{"var": "attempts", "op": "below", "value": 3}`
)

// conditionCandidates are two candidates of billing, citing lines 12-15
// and 16-18, with the conditions given ("" for none).
func conditionCandidates(first, second string) map[string][]mapper.Rule {
	rule := func(lines, cond string) mapper.Rule {
		r := withEARS(&mapper.Rule{
			Title:       "Prorata",
			Requirement: whenXThenY,
			Citations:   []extract.Ref{{Path: xPath, Lines: lines}},
			Entities:    []string{invoices},
			Confidence:  0.9,
		})
		if cond != "" {
			r.Condition = []byte(cond)
		}
		return r
	}
	return map[string][]mapper.Rule{billing: {rule("12-15", first), rule("16-18", second)}}
}

// mergedReply is a reply merging both candidates into one rule with the
// condition cond ("" for none).
func mergedReply(cond string) string {
	reply := strings.Replace(
		validReduceReply,
		`"citations": [{"path": "app/X.php", "lines": "12-15"}]`,
		`"citations": [{"path": "app/X.php", "lines": "12-15"}, {"path": "app/X.php", "lines": "16-18"}]`,
		1,
	)
	if cond == "" {
		return reply
	}
	return strings.Replace(reply, `"rationale":`, `"condition": `+cond+`, "rationale":`, 1)
}

// TestReduceConditions: merged candidates sharing one condition keep it,
// whatever the model wrote; otherwise the model's is kept when it parses,
// and dropped (never the rule) when it does not.
func TestReduceConditions(t *testing.T) {
	cases := map[string]struct {
		first, second, written string
		want                   string
	}{
		"shared: the model's is ignored": {
			sessionOpen,
			" " + sessionOpen,
			retried,
			sessionCanonical,
		},
		"different: the model's":   {sessionOpen, retried, retried, retriedCanon},
		"one without: the model's": {sessionOpen, "", retried, retriedCanon},
		"model's invalid: dropped": {sessionOpen, retried, notValid, ""},
		"none anywhere":            {"", "", "", ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			r := newReducer(
				t,
				chatFunc(func(context.Context, []llm.Message) (string, llm.Usage, error) {
					return mergedReply(tc.written), llm.Usage{}, nil
				}),
			)
			outputs, err := r.Run(context.Background(), conditionCandidates(tc.first, tc.second))
			if err != nil || len(outputs) != 1 || outputs[0].Failed || len(outputs[0].Rules) != 1 {
				t.Fatalf("Run: outputs=%+v err=%v", outputs, err)
			}
			if got := string(outputs[0].Rules[0].Condition); got != tc.want {
				t.Errorf("condition = %s, want %s", got, tc.want)
			}
		})
	}
}

// TestReduceMergeModeKeepsCondition: a candidate merge mode keeps as is
// keeps its condition.
func TestReduceMergeModeKeepsCondition(t *testing.T) {
	r := newReducer(t, chatFunc(func(context.Context, []llm.Message) (string, llm.Usage, error) {
		return validReduceReply, llm.Usage{}, nil
	}))
	r.Mode = reducer.ModeMerge
	single := conditionCandidates(retriedCanon, "")[billing][:1]
	outputs, err := r.Run(context.Background(), map[string][]mapper.Rule{billing: single})
	if err != nil || len(outputs) != 1 || len(outputs[0].Rules) != 1 {
		t.Fatalf("Run: outputs=%+v err=%v", outputs, err)
	}
	if got := string(outputs[0].Rules[0].Condition); got != retriedCanon {
		t.Errorf("condition = %s, want %s", got, retriedCanon)
	}
}
