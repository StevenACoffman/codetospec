package quality_test

import (
	"context"
	"errors"
	"log/slog"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/graph"
	"github.com/StevenACoffman/codetospec/internal/grounding"
	"github.com/StevenACoffman/codetospec/internal/llm"
	"github.com/StevenACoffman/codetospec/internal/quality"
)

// script is a fake model answering each call with the next reply (the
// last one repeats); an empty reply is a failed call.
type script struct {
	replies []string
	prompts []string // the user prompt of each call
}

func (s *script) Chat(_ context.Context, msgs []llm.Message) (string, llm.Usage, error) {
	s.prompts = append(s.prompts, msgs[1].Content)
	reply := s.replies[min(len(s.prompts), len(s.replies))-1]
	if reply == "" {
		return "", llm.Usage{}, errors.New("model unavailable")
	}
	return reply, llm.Usage{}, nil
}

// rule is a rule node of the given nature.
func rule(id, nature, requirement string) graph.Node {
	return graph.Node{
		ID: id, Type: graph.TypeRule, Title: id,
		Body:  "**Requirement (EARS)**: " + requirement,
		Extra: map[string]string{"nature": nature},
	}
}

// Rules the cases classify, and the reply naming all three asked about:
// the technical ones and the business one, never the presentation one.
const (
	timeoutRule  = "rule.api.timeout"
	signedRule   = "rule.api.signed"
	ownerRule    = "rule.billing.owner"
	answerAll    = `{"rules": [{"label": "R1", "quality_attribute": ""}, {"label": "R2", "quality_attribute": "performance_efficiency"}, {"label": "R3", "quality_attribute": "security"}]}`
	wrongValue   = `{"rules": [{"label": "R1", "quality_attribute": "speed"}, {"label": "R2", "quality_attribute": "security"}, {"label": "R3", "quality_attribute": "security"}]}`
	missingLabel = `{"rules": [{"label": "R2", "quality_attribute": "security"}, {"label": "R3", "quality_attribute": "security"}]}`
)

func nodes() []graph.Node {
	return []graph.Node{
		rule(timeoutRule, "technical", "The system shall time out a request after 30 seconds."),
		rule(signedRule, "technical", "The system shall accept only signed webhooks."),
		rule(
			ownerRule,
			"business",
			"The system shall let only the account owner refund an invoice.",
		),
		rule(
			"rule.billing.label",
			"presentation",
			"The system shall show the invoice total in bold.",
		),
	}
}

// TestRun: the technical and business rules are asked about in one
// labelled batch, the presentation ones never; a
// reply naming no listed attribute, or skipping a label, is corrected; a
// failed batch leaves its rules out without failing the run. Each run
// reports its one batch done, answered or failed.
func TestRun(t *testing.T) {
	cases := map[string]struct {
		replies   []string
		want      map[string]string
		wantCalls int
	}{
		"answered": {
			replies: []string{answerAll},
			want: map[string]string{
				signedRule:  "",
				timeoutRule: quality.PerformanceEfficiency,
				ownerRule:   quality.Security,
			},
			wantCalls: 1,
		},
		"unknown value corrected": {
			replies: []string{wrongValue, answerAll},
			want: map[string]string{
				signedRule:  "",
				timeoutRule: quality.PerformanceEfficiency,
				ownerRule:   quality.Security,
			},
			wantCalls: 2,
		},
		"missing label corrected": {
			replies: []string{missingLabel, answerAll},
			want: map[string]string{
				signedRule:  "",
				timeoutRule: quality.PerformanceEfficiency,
				ownerRule:   quality.Security,
			},
			wantCalls: 2,
		},
		"batch failed": {
			replies:   []string{""},
			want:      map[string]string{},
			wantCalls: 1,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			model := &script{replies: tc.replies}
			c := classifier(t, model, t.TempDir())
			var progress [][2]int
			c.OnBatch = func(done, total int, _ llm.Usage) {
				progress = append(progress, [2]int{done, total})
			}
			got, err := c.Run(t.Context(), nodes())
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if !maps.Equal(got, tc.want) {
				t.Errorf("attributes = %v, want %v", got, tc.want)
			}
			if len(model.prompts) != tc.wantCalls {
				t.Fatalf("calls = %d, want %d", len(model.prompts), tc.wantCalls)
			}
			if want := [][2]int{{1, 1}}; !slices.Equal(progress, want) {
				t.Errorf("progress = %v, want %v", progress, want)
			}
			if prompt := model.prompts[0]; strings.Contains(prompt, "bold") ||
				!strings.Contains(prompt, "R1. "+signedRule) ||
				!strings.Contains(prompt, "R3. "+ownerRule) {
				t.Errorf(
					"prompt does not list the non-presentation rules alone, by id:\n%s",
					prompt,
				)
			}
		})
	}
}

// TestRunReusesAnswers: a second run over unchanged rules asks nothing;
// a reworded rule is asked again, alone.
func TestRunReusesAnswers(t *testing.T) {
	dir := t.TempDir()
	if _, err := classifier(t, &script{replies: []string{answerAll}}, dir).
		Run(t.Context(), nodes()); err != nil {
		t.Fatal(err)
	}

	again := &script{replies: []string{""}}
	got, err := classifier(t, again, dir).Run(t.Context(), nodes())
	if err != nil || len(again.prompts) != 0 || got[timeoutRule] != quality.PerformanceEfficiency {
		t.Fatalf("second run: %v, %d calls, %v", got, len(again.prompts), err)
	}

	reworded := nodes()
	reworded[0].Body += " Or 60."
	retry := &script{
		replies: []string{`{"rules": [{"label": "R1", "quality_attribute": "reliability"}]}`},
	}
	got, err = classifier(t, retry, dir).Run(t.Context(), reworded)
	if err != nil || len(retry.prompts) != 1 || got[timeoutRule] != quality.Reliability {
		t.Fatalf("reworded run: %v, prompts %q, %v", got, retry.prompts, err)
	}
}

// TestAnnotate: a classified rule gets its attribute; one classified ""
// gets none; the input is not modified.
func TestAnnotate(t *testing.T) {
	in := nodes()
	out := quality.Annotate(in, map[string]string{timeoutRule: quality.Reliability, signedRule: ""})
	if out[0].Extra[graph.ExtraQualityAttribute] != quality.Reliability {
		t.Errorf("timeout rule: %v", out[0].Extra)
	}
	if _, set := out[1].Extra[graph.ExtraQualityAttribute]; set {
		t.Errorf("signed rule: %v", out[1].Extra)
	}
	if _, set := in[0].Extra[graph.ExtraQualityAttribute]; set {
		t.Error("input modified")
	}
}

// classifier is a classifier on model caching under dir.
func classifier(t *testing.T, model llm.Chatter, dir string) *quality.Classifier {
	t.Helper()
	return &quality.Classifier{
		LLM:       model,
		Log:       slog.New(slog.NewTextHandler(t.Output(), nil)),
		Model:     "test-model",
		Workers:   2,
		CachePath: filepath.Join(dir, "quality.json"),
	}
}

// technical is the nature of the rules a non-functional requirement can be.
const technical = "technical"

// grounded is the grounding status of a rule whose stated values all
// occur in its cited code.
const grounded = string(grounding.StatusGrounded)

// TestAnnotateThresholds: a grounded non-functional rule records the
// quantities its requirement states; any other rule, or one whose values
// grounding did not find in the code, records none.
func TestAnnotateThresholds(t *testing.T) {
	const cited = "retry.go"
	roots := extract.Roots{Src: fstest.MapFS{
		cited: {Data: []byte("package retry\nfunc Do() { for i := 0; i < MaxAttempts; i++ {} }\n")},
	}}
	opts := grounding.Options{DecimalSeparator: '.'}
	quantities := map[string]float64{"MaxAttempts": 3}
	cases := map[string]struct {
		nature, attribute, grounding, requirement string
		sources                                   string // lines of retry.go cited, "" for none
		threshold, quantity                       string
	}{
		"grounded NFR stating a number": {
			technical,
			quality.PerformanceEfficiency,
			grounded,
			"The system shall time out a request after 30 seconds.",
			"",
			"30 seconds",
			graph.QuantityStated,
		},
		"several numbers": {
			technical,
			quality.Reliability,
			grounded,
			"The system shall retry 3 times, 2 seconds apart.",
			"",
			"3 times; 2 seconds",
			graph.QuantityStated,
		},
		"ungrounded": {
			technical, quality.PerformanceEfficiency, "ungrounded",
			"The system shall time out a request after 30 seconds.", "", "", graph.QuantityStated,
		},
		"no number, a constant read": {
			technical, quality.Reliability, "unchecked",
			"The system shall retry a failed call.", "2-2", "MaxAttempts = 3", "",
		},
		"no number, no constant": {
			technical, quality.Security, "unchecked",
			"The system shall accept only signed webhooks.", "", "", "",
		},
		"business rule": {
			"business", quality.Security, grounded,
			"The system shall lock an account after 5 failed logins.", "", "", "",
		},
		"plain functionality": {
			technical, quality.FunctionalSuitability, grounded,
			"The system shall round to 2 decimals.", "", "", "",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			n := rule("rule.api.x", tc.nature, tc.requirement)
			n.Extra[graph.ExtraQualityAttribute] = tc.attribute
			n.Extra["grounding"] = tc.grounding
			if tc.sources != "" {
				n.Sources = []extract.Ref{{Path: cited, Lines: tc.sources}}
			}
			in := []graph.Node{n}
			got := quality.AnnotateThresholds(roots, in, opts, quantities)
			if v := got[0].Extra[graph.ExtraThreshold]; v != tc.threshold {
				t.Errorf("threshold = %q, want %q", v, tc.threshold)
			}
			if v := got[0].Extra[graph.ExtraQuantity]; v != tc.quantity {
				t.Errorf("quantity = %q, want %q", v, tc.quantity)
			}
			if _, set := in[0].Extra[graph.ExtraThreshold]; set {
				t.Error("the input node was modified")
			}
		})
	}
}
