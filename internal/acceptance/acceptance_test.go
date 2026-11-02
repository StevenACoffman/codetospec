package acceptance_test

import (
	"context"
	"log/slog"
	"reflect"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/StevenACoffman/codetospec/internal/acceptance"
	"github.com/StevenACoffman/codetospec/internal/condition"
	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/graph"
	"github.com/StevenACoffman/codetospec/internal/grounding"
	"github.com/StevenACoffman/codetospec/internal/llm"
	"github.com/StevenACoffman/codetospec/internal/locale"
)

// The cited code: a retry while fewer than 3 attempts, answering 429.
const (
	retryCode = "if attempts < 3 {\n\tretry()\n} else {\n\treturn 429\n}\n"
	retryTest = "func TestGiveUp(t *testing.T) {\n\tif got := call(); got != 429 {\n\t\tt.Fail()\n\t}\n}\n"
	attempts  = `{"var":"attempts","op":"less_than","value":3}`
	ruleID    = "rule.billing.retry"
)

// reply is a valid reply with the examples given (JSON array).
func reply(source, check, examples string) string {
	return `{"observable": {"channel": "response", "check": "` + check + `", "source": "` + source + `"},
"non_claims": ["does not notify the user"], "examples": ` + examples + `}`
}

// boundary is two examples on either side of the condition's bound.
const boundary = `[{"given": {"attempts": 2}, "expect": "matches"}, {"given": {"attempts": 3}, "expect": "does_not_match"}]`

func mustCondition(t *testing.T, raw string) condition.Condition {
	t.Helper()
	c, err := condition.Parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// TestValidate: a reply is refused, naming the field, unless every fact
// is well formed and its examples test the rule's own condition.
func TestValidate(t *testing.T) {
	withCond := mustCondition(t, attempts)
	none := condition.Condition{}
	cases := map[string]struct {
		reply      string
		c          *condition.Condition
		testsShown bool
		want       string // "" when valid
	}{
		"valid":              {reply("code", "answers 429", boundary), &withCond, false, ""},
		"valid from a test":  {reply("test", "answers 429", "[]"), &withCond, true, ""},
		"no condition, none": {reply("code", "answers 429", "[]"), &none, false, ""},
		"unknown field": {
			`{"observable": {}, "extra": 1}`,
			&none,
			false,
			"unknown field",
		},
		"unknown channel": {
			strings.Replace(reply("code", "x", "[]"), "response", "screen", 1),
			&none,
			false,
			"channel",
		},
		"test without tests":     {reply("test", "answers 429", "[]"), &none, false, "no TESTS"},
		"unknown source":         {reply("doc", "answers 429", "[]"), &none, false, "source"},
		"empty check":            {reply("code", " ", "[]"), &none, false, "check is empty"},
		"examples, no condition": {reply("code", "x", boundary), &none, false, "must be []"},
		"bad expectation": {
			reply("code", "x", `[{"given": {"attempts": 2}, "expect": "yes"}]`),
			&withCond,
			false,
			"expect",
		},
		"variable not in it": {
			reply("code", "x", `[{"given": {"tries": 2}, "expect": "matches"}]`),
			&withCond,
			false,
			`"tries"`,
		},
		"too many non-claims": {
			strings.Replace(
				reply("code", "x", "[]"),
				`["does not notify the user"]`,
				`["a","b","c","d"]`,
				1,
			),
			&none,
			false,
			"at most 3",
		},
		"an empty non-claim": {
			strings.Replace(reply("code", "x", "[]"), `["does not notify the user"]`, `[" "]`, 1),
			&none,
			false,
			"non_claims[0]",
		},
		"fenced reply still read": {
			"```json\n" + reply("code", "answers 429", "[]") + "\n```",
			&none,
			false,
			"",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := acceptance.Validate(tc.reply, tc.c, tc.testsShown)
			switch {
			case tc.want == "" && err != nil:
				t.Errorf("Validate: %v", err)
			case tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)):
				t.Errorf("Validate error = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}

// citations loads text as one citation of its whole lines.
func citations(path, text string) []grounding.Citation {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	return []grounding.Citation{{Path: path, Start: 1, End: len(lines), File: lines}}
}

// TestAssess: the observable's values are looked for where it was read
// from; examples hold, are contradicted, or decide nothing.
func TestAssess(t *testing.T) {
	c := mustCondition(t, attempts)
	code, tests := citations("retry.go", retryCode), citations("retry_test.go", retryTest)
	cases := map[string]struct {
		reply        string
		wantStatus   grounding.Status
		held         int
		contradicted []int
	}{
		"from the code, grounded": {
			reply("code", "answers 429", boundary),
			grounding.StatusGrounded,
			2,
			nil,
		},
		"from the code, not there": {
			reply("code", "answers 503", "[]"),
			grounding.StatusUngrounded,
			0,
			nil,
		},
		"from a test": {
			reply("test", "answers 429", "[]"),
			grounding.StatusGrounded,
			0,
			nil,
		},
		"one contradicted": {
			reply(
				"code",
				"answers 429",
				`[{"given": {"attempts": 3}, "expect": "matches"}, {"given": {"attempts": 1}, "expect": "matches"}]`,
			),
			grounding.StatusGrounded,
			1,
			[]int{1},
		},
		"undecided": {
			reply("code", "answers 429", `[{"given": {}, "expect": "matches"}]`),
			grounding.StatusGrounded, 0, nil,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f, err := acceptance.Validate(tc.reply, &c, true)
			if err != nil {
				t.Fatal(err)
			}
			a := acceptance.Assess(
				t.Context(),
				&f,
				&c,
				code,
				tests,
				grounding.Options{DecimalSeparator: '.'},
			)
			if a.Grounding != tc.wantStatus || a.Held != tc.held ||
				!slices.Equal(a.Contradicted, tc.contradicted) {
				t.Errorf(
					"Assess = %+v, want %s, held %d, contradicted %v",
					a,
					tc.wantStatus,
					tc.held,
					tc.contradicted,
				)
			}
		})
	}
}

// TestAnnotateReadsBack: the facts go into the body and frontmatter, and
// the observable and non-claims read back in either language.
func TestAnnotateReadsBack(t *testing.T) {
	c := mustCondition(t, attempts)
	f, err := acceptance.Validate(reply("code", "answers 429", boundary), &c, false)
	if err != nil {
		t.Fatal(err)
	}
	results := map[string]acceptance.Result{ruleID: {
		Facts: f,
		Assessment: acceptance.Assessment{
			Grounding:    grounding.StatusGrounded,
			Held:         1,
			Contradicted: []int{2},
		},
	}}
	for _, lang := range []string{"en", "fr"} {
		t.Run(lang, func(t *testing.T) {
			in := []graph.Node{
				{ID: ruleID, Type: graph.TypeRule, Body: "**Requirement (EARS)**: x"},
			}
			got := acceptance.Annotate(in, results, locale.For(lang))[0]
			if graph.RuleObservable(got.Body) != "answers 429" {
				t.Errorf(
					"observable reads back %q from:\n%s",
					graph.RuleObservable(got.Body),
					got.Body,
				)
			}
			if claims := graph.RuleNonClaims(
				got.Body,
			); !slices.Equal(
				claims,
				[]string{"does not notify the user"},
			) {
				t.Errorf("non-claims read back %q", claims)
			}
			want := map[string]string{
				graph.ExtraObservable: "response", graph.ExtraObservableSource: "code",
				graph.ExtraObservableGrounding: "grounded", graph.ExtraExamples: "1/2",
				graph.ExtraExamplesContradicted: "2",
			}
			for key, value := range want {
				if got.Extra[key] != value {
					t.Errorf("%s = %q, want %q", key, got.Extra[key], value)
				}
			}
			if in[0].Extra != nil || strings.Contains(in[0].Body, "429") {
				t.Error("the input node was modified")
			}
		})
	}
}

// script replies with reply and counts its calls.
type script struct {
	reply string
	calls int
}

func (s *script) Chat(context.Context, []llm.Message) (string, llm.Usage, error) {
	s.calls++
	return s.reply, llm.Usage{PromptTokens: 1}, nil
}

// TestRun: LLM rules not refuted are asked, once: an unchanged rule reuses
// its cached facts, which give the same results as the live reply (a
// number written 2.0 stays 2.0); extractor and refuted rules are not
// asked.
func TestRun(t *testing.T) {
	rule := func(id string, extra map[string]string) graph.Node {
		return graph.Node{
			ID: id, Type: graph.TypeRule, Body: "**Requirement (EARS)**: x",
			Sources: []extract.Ref{{Path: "retry.go", Lines: "1-5"}},
			Extra:   extra,
		}
	}
	nodes := []graph.Node{
		rule(ruleID, map[string]string{graph.ExtraCondition: attempts}),
		rule("rule.billing.extracted", map[string]string{graph.ExtraExtractedBy: "webapp"}),
		rule("rule.billing.refuted", map[string]string{"crosscheck": "unsupported"}),
	}
	model := &script{reply: reply(
		"code",
		"answers 429",
		`[{"given": {"attempts": 2.0}, "expect": "matches"}, {"given": {"attempts": 3}, "expect": "does_not_match"}]`,
	)}
	w := &acceptance.Writer{
		LLM:       model,
		Log:       slog.New(slog.NewTextHandler(t.Output(), nil)),
		Lang:      "en",
		Workers:   2,
		OutDir:    t.TempDir(),
		Roots:     extract.Roots{Src: fstest.MapFS{"retry.go": {Data: []byte(retryCode)}}},
		Grounding: grounding.Options{DecimalSeparator: '.'},
	}
	var runs []map[string]acceptance.Result
	for run := range 2 {
		results, err := w.Run(t.Context(), nodes)
		if err != nil {
			t.Fatal(err)
		}
		if len(results) != 1 || results[ruleID].Assessment.Held != 2 {
			t.Fatalf("run %d: results %+v, want the one LLM rule, both examples held", run, results)
		}
		runs = append(runs, results)
	}
	if !reflect.DeepEqual(runs[0], runs[1]) {
		t.Errorf("the cached run differs from the live one:\n%+v\n%+v", runs[0], runs[1])
	}
	if model.calls != 1 {
		t.Errorf("%d calls over two runs, want 1 (the second from the cache)", model.calls)
	}
}
