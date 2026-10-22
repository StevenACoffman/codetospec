package quality_test

import (
	"context"
	"errors"
	"log/slog"
	"maps"
	"path/filepath"
	"strings"
	"testing"

	"github.com/StevenACoffman/codetospec/internal/graph"
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

// Rules the cases classify, and the reply naming both technical ones.
const (
	timeoutRule  = "rule.api.timeout"
	signedRule   = "rule.api.signed"
	answerBoth   = `{"rules": [{"label": "R1", "quality_attribute": ""}, {"label": "R2", "quality_attribute": "performance_efficiency"}]}`
	wrongValue   = `{"rules": [{"label": "R1", "quality_attribute": "speed"}, {"label": "R2", "quality_attribute": "security"}]}`
	missingLabel = `{"rules": [{"label": "R2", "quality_attribute": "security"}]}`
)

func nodes() []graph.Node {
	return []graph.Node{
		rule(timeoutRule, "technical", "The system shall time out a request after 30 seconds."),
		rule(signedRule, "technical", "The system shall accept only signed webhooks."),
		rule("rule.billing.prorata", "business", "The system shall prorate the first invoice."),
	}
}

// TestRun: the technical rules are asked about in one labelled batch; a
// reply naming no listed attribute, or skipping a label, is corrected; a
// failed batch leaves its rules out without failing the run.
func TestRun(t *testing.T) {
	cases := map[string]struct {
		replies   []string
		want      map[string]string
		wantCalls int
	}{
		"answered": {
			replies: []string{answerBoth},
			want: map[string]string{
				signedRule:  "",
				timeoutRule: quality.PerformanceEfficiency,
			},
			wantCalls: 1,
		},
		"unknown value corrected": {
			replies: []string{wrongValue, answerBoth},
			want: map[string]string{
				signedRule:  "",
				timeoutRule: quality.PerformanceEfficiency,
			},
			wantCalls: 2,
		},
		"missing label corrected": {
			replies: []string{missingLabel, answerBoth},
			want: map[string]string{
				signedRule:  "",
				timeoutRule: quality.PerformanceEfficiency,
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
			got, err := classifier(t, model, t.TempDir()).Run(t.Context(), nodes())
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if !maps.Equal(got, tc.want) {
				t.Errorf("attributes = %v, want %v", got, tc.want)
			}
			if len(model.prompts) != tc.wantCalls {
				t.Fatalf("calls = %d, want %d", len(model.prompts), tc.wantCalls)
			}
			if prompt := model.prompts[0]; strings.Contains(prompt, "prorate") ||
				!strings.Contains(prompt, "R1. "+signedRule) {
				t.Errorf("prompt does not list the technical rules alone, by id:\n%s", prompt)
			}
		})
	}
}

// TestRunReusesAnswers: a second run over unchanged rules asks nothing;
// a reworded rule is asked again, alone.
func TestRunReusesAnswers(t *testing.T) {
	dir := t.TempDir()
	if _, err := classifier(t, &script{replies: []string{answerBoth}}, dir).
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
