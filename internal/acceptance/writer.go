package acceptance

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/StevenACoffman/codetospec/internal/atomicfs"
	"github.com/StevenACoffman/codetospec/internal/condition"
	"github.com/StevenACoffman/codetospec/internal/crosscheck"
	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/graph"
	"github.com/StevenACoffman/codetospec/internal/grounding"
	"github.com/StevenACoffman/codetospec/internal/llm"
	"github.com/StevenACoffman/codetospec/internal/locale"
	"github.com/StevenACoffman/codetospec/internal/workpool"
)

const systemPrompt = `You write the acceptance facts of one business rule, read from its cited code and, when given, the nearest tests exercising it.

Hard rules:
- Output ONLY a valid JSON object matching the schema. No markdown fences, no prose.
- observable: how an observer tells that the rule held from a plausible imitation. "channel": where to look (response: what the caller gets back; state: what is stored; event: what is emitted or sent; log: what is logged). "check": the concrete thing to verify, in <LANG>, with the exact values the code or test states. "source": "test" when one of the TESTS asserts it, else "code". Never state a value neither the CITED CODE nor the TESTS hold.
- non_claims: 0 to 3 things a reader might assume the rule promises but the code shows it does not (e.g. "does not retry", "does not notify the user"), in <LANG>. [] when none stands out.
- examples: only when CONDITION is not "-": 0 to 4 rows {"given": {variable: value}, "expect": "matches" | "does_not_match"} testing CONDITION at its boundaries, each giving a value to every variable CONDITION compares, and naming no other variable; values are JSON booleans, numbers or strings. An "exists" check holds when its variable is given at all, whatever the value: leave the variable out of "given" for the case where it is absent. [] when CONDITION is "-".`

const userPromptFormat = `RULE: %s
TITLE: %s
REQUIREMENT: %s
CONDITION: %s

CITED CODE:
%s

TESTS (the nearest tests exercising the cited code; - = none known):
%s

OUTPUT JSON SCHEMA:
{"observable": {"channel": "response|state|event|log", "check": string, "source": "test|code"}, "non_claims": [string], "examples": [{"given": object, "expect": "matches|does_not_match"}]}`

// corrections is how many times a malformed reply is sent back before the
// rule is left without facts.
const corrections = 2

// Writer drives the acceptance pass.
type Writer struct {
	LLM llm.Chatter
	// Log receives degraded-mode warnings (required).
	Log     *slog.Logger
	Model   string // model id, folded into the cache key
	Lang    string
	Workers int
	OutDir  string // <out>/.codetospec/acceptance
	// Roots reads the cited code and the tests.
	Roots extract.Roots
	// Grounding reads the observable's values (decimal separator,
	// constants).
	Grounding grounding.Options
	// TestsFor returns the nearest tests exercising a rule's cited code
	// (testreach.NearestTests); nil when no test is known. Optional.
	TestsFor func(rule *graph.Node) []extract.Ref
	// Review sends each observable to a blind adversarial review (with
	// --crosscheck): one more call per rule, cached beside its facts.
	Review bool
	// OnUnit is called after each rule (cached or live) with its usage.
	OnUnit func(usage llm.Usage)
}

// cached is one rule's cache file: its facts, or the error that left it
// without, and the review of its observable. A review is kept while the
// pass runs without Review, so turning it back on asks nothing again.
type cached struct {
	InputHash string        `json:"input_hash"`
	Facts     *Facts        `json:"facts,omitempty"`
	Error     string        `json:"error,omitempty"`
	Review    *cachedReview `json:"review,omitempty"`
}

// Asked reports whether the pass writes facts for a rule: one an LLM
// wrote (not an extractor's nor an inherited one), not refuted by the
// crosscheck.
func Asked(n *graph.Node) bool {
	return n.Type == graph.TypeRule && graph.Reviewable(n) &&
		n.Extra["crosscheck"] != crosscheck.VerdictUnsupported
}

// Run writes the acceptance facts of every asked rule and returns them
// with their assessment, keyed by rule id. A rule whose call fails, or
// whose code cannot be read, has none.
func (w *Writer) Run(ctx context.Context, nodes []graph.Node) (map[string]Result, error) {
	var rules []graph.Node
	for i := range nodes {
		if Asked(&nodes[i]) {
			rules = append(rules, nodes[i])
		}
	}
	slices.SortFunc(rules, func(a, b graph.Node) int { return cmp.Compare(a.ID, b.ID) })
	results := make(map[string]Result, len(rules))
	var runErr error
	workpool.Run(ctx, w.Workers, rules,
		func(ctx context.Context, rule *graph.Node) unit {
			r, ok, usage, err := w.rule(ctx, rule)
			return unit{id: rule.ID, result: r, ok: ok, usage: usage, err: err}
		},
		func(u unit) {
			if u.err != nil {
				runErr = cmp.Or(runErr, u.err)
				return
			}
			if u.ok {
				results[u.id] = u.result
			}
			if w.OnUnit != nil {
				w.OnUnit(u.usage)
			}
		})
	if runErr == nil && ctx.Err() != nil {
		runErr = ctx.Err()
	}
	return results, runErr
}

// unit is one worker's outcome.
type unit struct {
	id     string
	result Result
	ok     bool
	usage  llm.Usage
	err    error
}

// rule writes one rule's facts, and with Review the review of its
// observable, each reusing the cached one while its conversation is
// unchanged (the rule, its condition, its code, its tests and, for the
// review, the observable). ok is false when the rule gets no facts this
// run; err only on cancellation or a cache that cannot be written.
func (w *Writer) rule(ctx context.Context, rule *graph.Node) (Result, bool, llm.Usage, error) {
	code, err := grounding.Load(w.Roots, rule.Sources)
	if err != nil {
		w.Log.WarnContext(ctx, "acceptance: cited code unreadable", "rule", rule.ID, "err", err)
		return Result{}, false, llm.Usage{}, nil
	}
	tests := w.tests(ctx, rule)
	c := graph.RuleCondition(rule)
	outPath := filepath.Join(w.OutDir, rule.ID+".json")
	prior, priorData := readCached(outPath)

	entry, usage, err := w.facts(ctx, rule, &c, code, tests, &prior)
	if err != nil {
		return Result{}, false, usage, err
	}
	// The review of the observable, when asked; else the one cached stays.
	entry.Review = prior.Review
	if w.Review && entry.Facts != nil {
		var reviewUsage llm.Usage
		entry.Review, reviewUsage, err = w.review(ctx, rule, entry.Facts, code, tests, prior.Review)
		usage.Add(reviewUsage)
		if err != nil {
			return Result{}, false, usage, err
		}
	}
	if err = writeCached(outPath, &entry, priorData); err != nil {
		return Result{}, false, usage, err
	}
	if entry.Facts == nil {
		return Result{}, false, usage, nil
	}
	result := Result{
		Facts:      *entry.Facts,
		Assessment: Assess(ctx, entry.Facts, &c, code, tests, w.Grounding),
	}
	if w.Review && entry.Review != nil {
		result.Review = &entry.Review.Review
	}
	return result, true, usage, nil
}

// tests reads the nearest tests of rule; none when TestsFor is unset or
// they cannot be read.
func (w *Writer) tests(ctx context.Context, rule *graph.Node) []grounding.Citation {
	if w.TestsFor == nil {
		return nil
	}
	tests, err := grounding.Load(w.Roots, w.TestsFor(rule))
	if err != nil {
		w.Log.WarnContext(ctx, "acceptance: tests unreadable, written without them",
			"rule", rule.ID, "err", err)
		return nil
	}
	return tests
}

// facts returns the cache entry of rule's facts: prior's while the
// conversation asking for them is unchanged, else the model's reply, or
// the error that left the rule without facts. err only on cancellation.
func (w *Writer) facts(
	ctx context.Context,
	rule *graph.Node,
	c *condition.Condition,
	code, tests []grounding.Citation,
	prior *cached,
) (cached, llm.Usage, error) {
	msgs := w.prompt(rule, c, code, tests)
	entry := cached{InputHash: inputHash(msgs, w.Model)}
	if prior.InputHash == entry.InputHash && prior.Facts != nil {
		entry.Facts = prior.Facts
		return entry, llm.Usage{}, nil
	}
	var usage llm.Usage
	facts, err := llm.ChatJSON(ctx, w.LLM, msgs, corrections, func(u llm.Usage) { usage.Add(u) },
		func(reply string) (Facts, error) { return Validate(reply, c, len(tests) > 0) })
	switch {
	case err == nil:
		entry.Facts = &facts
	case errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded):
		return cached{}, usage, err
	default:
		w.Log.WarnContext(ctx, "acceptance failed", "rule", rule.ID, "err", err)
		entry.Error = err.Error()
	}
	return entry, usage, nil
}

// readCached reads a rule's cache file and returns it with its bytes; the
// zero entry when there is none or it does not decode. Numbers are kept as
// written, as Validate reads a live reply: a cached run renders the same
// examples as the run that cached them.
func readCached(path string) (cached, []byte) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return cached{}, nil
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var entry cached
	if dec.Decode(&entry) != nil {
		return cached{}, data
	}
	return entry, data
}

// writeCached writes entry to path unless prior, the file's bytes, already
// hold it: a warm run writes nothing.
func writeCached(path string, entry *cached, prior []byte) error {
	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return fmt.Errorf("encode acceptance: %w", err)
	}
	data = append(data, '\n')
	if bytes.Equal(data, prior) {
		return nil
	}
	if err = atomicfs.WriteFile(path, data); err != nil {
		return fmt.Errorf("write acceptance: %w", err)
	}
	return nil
}

// prompt is the conversation that asks for one rule's facts.
func (w *Writer) prompt(
	rule *graph.Node,
	c *condition.Condition,
	code, tests []grounding.Citation,
) []llm.Message {
	cond := cmp.Or(c.Canonical(), "-")
	testText := "-"
	if len(tests) > 0 {
		testText = excerpt(tests)
	}
	user := fmt.Sprintf(userPromptFormat, rule.ID, rule.Title, graph.RuleRequirement(rule.Body),
		cond, excerpt(code), testText)
	return []llm.Message{
		{Role: "system", Content: locale.For(w.Lang).Prompt(systemPrompt)},
		{Role: "user", Content: user},
	}
}

// inputHash keys a cached reply: the model and the whole conversation it
// was given (prompt templates, rule, condition, code and tests).
func inputHash(msgs []llm.Message, model string) string {
	h := sha256.New()
	h.Write([]byte(model))
	for _, m := range msgs {
		h.Write([]byte("\x00" + m.Role + "\x00" + m.Content))
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// excerpt renders citations' lines, numbered, under one header each.
func excerpt(citations []grounding.Citation) string {
	var b strings.Builder
	for _, c := range citations {
		fmt.Fprintf(&b, "--- %s:%d-%d ---\n", c.Path, c.Start, c.End)
		for i := c.Start; i <= c.End && i <= len(c.File); i++ {
			fmt.Fprintf(&b, "%5d %s\n", i, c.File[i-1])
		}
	}
	return strings.TrimRight(b.String(), "\n")
}
