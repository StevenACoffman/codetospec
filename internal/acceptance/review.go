package acceptance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/StevenACoffman/codetospec/internal/crosscheck"
	"github.com/StevenACoffman/codetospec/internal/graph"
	"github.com/StevenACoffman/codetospec/internal/grounding"
	"github.com/StevenACoffman/codetospec/internal/llm"
	"github.com/StevenACoffman/codetospec/internal/locale"
)

const reviewSystemPrompt = `You are an adversarial reviewer. Another model wrote the OBSERVABLE of one business rule: how an observer tells that the rule held from a plausible imitation. You receive the rule, that observable, the exact source lines the rule cites and, when given, the nearest tests exercising them. Your job is to try to REFUTE the observable.

Hard rules:
- Output ONLY a valid JSON object matching the schema provided by the user. No markdown fences, no prose.
- verdict "supported": the cited lines produce what the check names, with its values, where the channel says (response: returned to the caller; state: stored; event: emitted or sent; log: logged), and code that did not hold the rule would fail the check.
- verdict "partial": the check is plausible, but names a value or a channel the lines do not show, or code that did not hold the rule would pass it too.
- verdict "unsupported": the cited lines do not produce what the check names.
- TESTS, when given, show what the cited lines do and with which values. A test is not the system: do NOT credit what only a test does (a mock, a call the test makes itself).
- Write the reason in <LANG>: one or two concrete sentences naming what the lines do or do not show.`

const reviewUserPromptFormat = `RULE: %s
TITLE: %s
REQUIREMENT: %s
OBSERVABLE: (%s, read from the %s) %s

CITED CODE:
%s

TESTS (the nearest tests exercising the cited code; - = none known):
%s

OUTPUT JSON SCHEMA:
{"verdict": "supported|partial|unsupported", "reason": string}`

// Review is the adversarial review of a rule's observable: a verdict of
// the crosscheck's (crosscheck.VerdictSupported, Partial or Unsupported)
// and its reason.
type Review struct {
	Verdict string `json:"verdict"`
	Reason  string `json:"reason"`
}

// Refuted reports whether the review found the observable unsupported: a
// refuted observable stays in the rule for a person to read, but no
// export takes it as a criterion.
func (r *Review) Refuted() bool {
	return r != nil && r.Verdict == crosscheck.VerdictUnsupported
}

// ExportedObservable is the check of rule's observable an export takes as
// a criterion: "" when the rule has none, or when its review refuted it
// (graph.ExtraObservableCrosscheck).
func ExportedObservable(rule *graph.Node) string {
	if rule.Extra[graph.ExtraObservableCrosscheck] == crosscheck.VerdictUnsupported {
		return ""
	}
	return graph.RuleObservable(rule.Body)
}

// validateReview reads a review reply.
func validateReview(reply string) (Review, error) {
	var r Review
	if err := json.Unmarshal([]byte(llm.StripFences(reply)), &r); err != nil {
		return r, fmt.Errorf("invalid JSON: %w", err)
	}
	switch r.Verdict {
	case crosscheck.VerdictSupported, crosscheck.VerdictPartial, crosscheck.VerdictUnsupported:
	default:
		return r, fmt.Errorf("verdict %q is not one of supported|partial|unsupported", r.Verdict)
	}
	if strings.TrimSpace(r.Reason) == "" {
		return r, errors.New("reason must not be empty")
	}
	return r, nil
}

// reviewPrompt is the conversation that asks for the review of facts'
// observable. It holds the rule, the observable, the code and the tests
// only: nothing of the conversation that wrote the observable.
func (w *Writer) reviewPrompt(
	rule *graph.Node,
	facts *Facts,
	code, tests []grounding.Citation,
) []llm.Message {
	testText := "-"
	if len(tests) > 0 {
		testText = excerpt(tests)
	}
	o := facts.Observable
	user := fmt.Sprintf(reviewUserPromptFormat, rule.ID, rule.Title,
		graph.RuleRequirement(rule.Body), o.Channel, o.Source, o.Check, excerpt(code), testText)
	return []llm.Message{
		{Role: "system", Content: locale.For(w.Lang).Prompt(reviewSystemPrompt)},
		{Role: "user", Content: user},
	}
}

// review returns the review of facts' observable, reusing prior's while
// its conversation is unchanged; nil when the call fails (asked again on
// the next run). err only on cancellation.
func (w *Writer) review(
	ctx context.Context,
	rule *graph.Node,
	facts *Facts,
	code, tests []grounding.Citation,
	prior *cachedReview,
) (*cachedReview, llm.Usage, error) {
	msgs := w.reviewPrompt(rule, facts, code, tests)
	hash := inputHash(msgs, w.Model)
	if prior != nil && prior.InputHash == hash {
		return prior, llm.Usage{}, nil
	}
	var usage llm.Usage
	r, err := llm.ChatJSON(ctx, w.LLM, msgs, corrections, func(u llm.Usage) { usage.Add(u) },
		validateReview)
	switch {
	case err == nil:
		return &cachedReview{InputHash: hash, Review: r}, usage, nil
	case errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded):
		return nil, usage, err
	default:
		w.Log.WarnContext(ctx, "acceptance: observable review failed", "rule", rule.ID, "err", err)
		return nil, usage, nil
	}
}

// cachedReview is a review in a rule's cache file, keyed by the hash of
// its own conversation.
type cachedReview struct {
	InputHash string `json:"input_hash"`
	Review
}
