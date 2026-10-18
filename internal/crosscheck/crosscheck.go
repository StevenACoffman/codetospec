// Package crosscheck runs an optional adversarial review pass: for every
// rule node, a fresh-context LLM call receives ONLY the rule and the exact
// source lines it cites, and tries to refute it. Verdicts are recorded,
// never silently dropped — disputed rules stay in the graph, flagged.
//
// The design follows the separation used by large-scale AI porting efforts:
// the model that wrote a claim never judges it; the reviewer sees the diff
// (here: rule + cited code), nothing else.
package crosscheck

import (
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
	"sort"
	"strconv"
	"strings"

	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/graph"
	"github.com/StevenACoffman/codetospec/internal/llm"
	"github.com/StevenACoffman/codetospec/internal/locale"
	"github.com/StevenACoffman/codetospec/internal/mapper"
	"github.com/StevenACoffman/codetospec/internal/state"
	"github.com/StevenACoffman/codetospec/internal/workpool"
)

// Verdict values, from strongest to weakest support.
const (
	VerdictSupported   = "supported"
	VerdictPartial     = "partial"
	VerdictUnsupported = "unsupported"
	VerdictRepaired    = "repaired" // citation was corrected to a grounded symbol span
	// VerdictUnchecked marks a rule the review never judged (its call failed
	// or it got no verdict). Recorded so an unexamined rule never reads like
	// one that passed.
	VerdictUnchecked = "unchecked"
	// VerdictMechanical marks a rule an extractor derived deterministically
	// from the code (graph.ExtraExtractedBy): it is not sent to the reviewer,
	// and is neither supported (nobody reviewed it) nor unchecked (nothing is
	// missing — it is grounded by construction).
	VerdictMechanical = "mechanical"
)

// knownVerdict reports whether v is a verdict the reviewer may give.
func knownVerdict(v string) bool {
	switch v {
	case VerdictSupported, VerdictPartial, VerdictUnsupported:
		return true
	}
	return false
}

const systemPrompt = `You are an adversarial code reviewer. You receive one candidate business rule and the exact source lines it cites. The rule was written by another model; your job is to try to REFUTE it.

Hard rules:
- Output ONLY a valid JSON object matching the schema provided by the user. No markdown fences, no prose.
- verdict "supported": the cited lines clearly implement the described behavior, including its specific values and thresholds.
- verdict "partial": the behavior is plausible but the cited lines only show part of it, or values/thresholds differ from the requirement.
- verdict "unsupported": the cited lines do not show this behavior at all.
- The cited lines are marked ">"; up to a few surrounding lines marked "·" are shown as context. Base the verdict on the cited lines, using the context only to read an enclosing condition or scope. Do NOT credit behavior that appears only in context lines, and never assume code you cannot see.
- Write the reason in <LANG>: one or two concrete sentences naming what the lines do or do not show.`

const userPromptFormat = `RULE: %s
TITLE: %s
REQUIREMENT (EARS): %s
ACCEPTANCE_CRITERIA:
%s

CITED SOURCE (1-based line numbers; > = cited, · = surrounding context):
%s

OUTPUT JSON SCHEMA:
{"verdict": "supported|partial|unsupported", "reason": string}`

// maxExcerptLines caps how much cited source is sent per rule.
const maxExcerptLines = 400

// contextLines is how many lines of surrounding source the reviewer sees on
// each side of a cited span. Enough to reveal an enclosing condition or scope
// — the common cause of false "unsupported" verdicts on citations too narrow
// to include the guard that makes them true — without letting the reviewer
// credit behavior that lives only in the context.
const contextLines = 10

// Verdict is the persisted result of cross-checking one rule.
type Verdict struct {
	RuleID       string        `json:"rule_id"`
	Verdict      string        `json:"verdict"`
	Reason       string        `json:"reason"`
	InputHash    string        `json:"input_hash"`
	NewCitations []extract.Ref `json:"new_citations,omitempty"` // set when Verdict == "repaired"
	// RepairReview is the fresh review's verdict on a repaired citation
	// (unchecked when that review failed), and RepairReason its reason; both
	// empty when no grounded citation was proposed. A repair is adopted only
	// on "supported".
	RepairReview string `json:"repair_review,omitempty"`
	RepairReason string `json:"repair_reason,omitempty"`
	Failed       bool   `json:"failed,omitempty"`
	Error        string `json:"error,omitempty"`
}

// verdictPayload mirrors the JSON schema requested from the LLM.
type verdictPayload struct {
	Verdict string `json:"verdict"`
	Reason  string `json:"reason"`
}

const repairSystemPrompt = `You are fixing a business rule whose citation does not prove it. You receive the rule and the precise symbol definitions (with exact line spans) available in the files it currently cites. Another reviewer rejected the current citation.

Hard rules:
- Output ONLY a valid JSON object matching the schema. No markdown fences, no prose.
- Pick the symbol whose body actually implements the rule and cite its EXACT line span (copy the lines shown). You may cite more than one symbol.
- Only cite spans listed in AVAILABLE_SYMBOLS. Never invent a file or a range.
- If no available symbol implements the rule, return {"citations": [], "reason": "..."}.
- Write the reason in <LANG>: one concrete sentence.`

const repairUserPromptFormat = `RULE: %s
TITLE: %s
REQUIREMENT (EARS): %s
WHY THE CURRENT CITATION WAS REJECTED: %s

AVAILABLE_SYMBOLS (cite only these exact spans):
%s

OUTPUT JSON SCHEMA:
{"citations": [{"path": string, "lines": "A-B"}], "reason": string}`

// repairPayload mirrors the repair JSON schema.
type repairPayload struct {
	Citations []extract.Ref `json:"citations"`
	Reason    string        `json:"reason"`
}

// Checker drives the adversarial cross-check phase.
type Checker struct {
	LLM llm.Chatter
	// Log receives progress and degraded-mode warnings (required).
	Log     *slog.Logger
	Model   string // model id, folded into the cache key
	Lang    string
	Workers int
	OutDir  string // <out>/.codetospec/crosscheck
	SrcRoot string
	// Repair, when true, gives every unsupported/partial rule one chance to
	// re-cite the exact span of a precise symbol that implements it.
	Repair bool
	// SymbolsFor returns precise symbol definitions for a file (from a SCIP
	// index). Repair needs these as grounded citation targets.
	SymbolsFor func(path string) []mapper.SymbolContext
	// OnUnit is called after each finished rule (cached or live).
	OnUnit func(v Verdict, usage llm.Usage)
}

// Run cross-checks every rule node and returns verdicts keyed by rule id.
// Cached verdicts are reused as long as the rule and its cited code are
// unchanged (input hash). Rules are reviewed in parallel; verdicts are
// recorded, and OnUnit called, in the calling goroutine.
func (c *Checker) Run(ctx context.Context, nodes []graph.Node) (map[string]Verdict, error) {
	if err := os.MkdirAll(c.OutDir, 0o750); err != nil {
		return nil, fmt.Errorf("create crosscheck dir: %w", err)
	}
	var rules []graph.Node
	for i := range nodes {
		if graph.Reviewable(&nodes[i]) {
			rules = append(rules, nodes[i])
		}
	}
	sort.SliceStable(rules, func(i, j int) bool { return rules[i].ID < rules[j].ID })

	out := make(map[string]Verdict, len(rules))
	var runErr error
	workpool.Run(ctx, c.Workers, rules,
		func(ctx context.Context, rule *graph.Node) outcome {
			v, usage, err := c.checkRule(ctx, rule)
			return outcome{verdict: v, usage: usage, err: err}
		},
		func(r outcome) {
			if r.err != nil {
				runErr = cmp.Or(runErr, r.err)
				return
			}
			out[r.verdict.RuleID] = r.verdict
			if c.OnUnit != nil {
				c.OnUnit(r.verdict, r.usage)
			}
		})
	if runErr == nil && ctx.Err() != nil {
		runErr = ctx.Err()
	}
	return out, runErr
}

// outcome is one worker's result for one rule.
type outcome struct {
	verdict Verdict
	usage   llm.Usage
	err     error
}

// checkRule reviews one rule, reusing the cached verdict when the rule and
// its cited code are unchanged.
func (c *Checker) checkRule(ctx context.Context, rule *graph.Node) (Verdict, llm.Usage, error) {
	excerpt, err := c.citedExcerpt(rule.Sources)
	if err != nil {
		return Verdict{}, llm.Usage{}, fmt.Errorf("rule %s: %w", rule.ID, err)
	}
	hash := inputHash(c.promptKey(), rule, excerpt)

	outPath := filepath.Join(c.OutDir, rule.ID+".json")
	if data, readErr := os.ReadFile(filepath.Clean(outPath)); readErr == nil {
		var cached Verdict
		if json.Unmarshal(data, &cached) == nil && cached.InputHash == hash {
			return cached, llm.Usage{}, nil
		}
	}

	payload, usage, err := c.review(ctx, rule, excerpt)
	v := Verdict{RuleID: rule.ID, InputHash: hash}
	switch {
	case err == nil:
		v.Verdict = payload.Verdict
		v.Reason = payload.Reason
	case errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded):
		return Verdict{}, usage, err
	default:
		c.Log.WarnContext(ctx, "crosscheck failed", "rule", rule.ID, "err", err)
		v.Failed = true
		v.Error = err.Error()
	}

	if c.Repair && (v.Verdict == VerdictUnsupported || v.Verdict == VerdictPartial) {
		r, repairUsage, repairErr := c.tryRepair(ctx, rule, v.Reason)
		usage.Add(repairUsage)
		if repairErr != nil {
			return Verdict{}, usage, repairErr
		}
		v.RepairReview, v.RepairReason = r.review, r.reason
		if r.review == VerdictSupported {
			v.Verdict, v.NewCitations = VerdictRepaired, r.citations
		}
	}

	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return Verdict{}, usage, fmt.Errorf("encode verdict: %w", err)
	}
	if writeErr := state.WriteFileAtomic(outPath, append(data, '\n')); writeErr != nil {
		return Verdict{}, usage, fmt.Errorf("write verdict: %w", writeErr)
	}
	return v, usage, nil
}

// reviewCorrections is how many times a malformed review reply is sent back
// for correction before the rule is left unchecked.
const reviewCorrections = 2

// review asks the reviewer to refute rule against the cited excerpt. It
// takes the rule and its cited code only: no earlier verdict, reason or
// repair can reach the reviewer, so a re-review is as blind as the first.
func (c *Checker) review(
	ctx context.Context,
	rule *graph.Node,
	excerpt string,
) (verdictPayload, llm.Usage, error) {
	criteria := "-"
	if lines := graph.RuleCriteria(rule.Body); len(lines) > 0 {
		criteria = strings.Join(lines, "\n")
	}
	userPrompt := fmt.Sprintf(userPromptFormat,
		rule.ID, rule.Title, graph.RuleRequirement(rule.Body), criteria, excerpt)
	msgs := []llm.Message{
		{Role: "system", Content: locale.For(c.Lang).Prompt(systemPrompt)},
		{Role: "user", Content: userPrompt},
	}
	var usage llm.Usage
	payload, err := llm.ChatJSON(
		ctx,
		c.LLM,
		msgs,
		reviewCorrections,
		func(u llm.Usage) { usage.Add(u) },
		func(reply string) (verdictPayload, error) {
			var p verdictPayload
			if unmarshalErr := json.Unmarshal(
				[]byte(llm.StripFences(reply)),
				&p,
			); unmarshalErr != nil {
				return p, fmt.Errorf("invalid JSON: %w", unmarshalErr)
			}
			if !knownVerdict(p.Verdict) {
				return p, fmt.Errorf(
					"verdict %q is not one of supported|partial|unsupported",
					p.Verdict,
				)
			}
			if strings.TrimSpace(p.Reason) == "" {
				return p, errors.New("reason must not be empty")
			}
			return p, nil
		},
	)
	if err != nil {
		return verdictPayload{}, usage, fmt.Errorf("review %s: %w", rule.ID, err)
	}
	return payload, usage, nil
}

// repairOutcome is what a repair attempt found: the grounded citations it
// proposed, and the fresh review's verdict and reason on them. All empty
// when no grounded citation was proposed.
type repairOutcome struct {
	citations      []extract.Ref
	review, reason string
}

// tryRepair gives a flagged rule one chance to re-cite the exact span of a
// precise symbol, then has the new citation reviewed afresh. Only a
// "supported" review may adopt it: repair saw why the first review rejected
// the rule, so its answer is a new claim, not a verdict.
//
// Requires: reason is the first review's reason for flagging rule.
// Ensures:  review is VerdictUnchecked when the re-review could not run; the
// error is non-nil only on cancellation.
func (c *Checker) tryRepair(
	ctx context.Context,
	rule *graph.Node,
	reason string,
) (repairOutcome, llm.Usage, error) {
	citations, usage, err := c.repairRule(ctx, rule, reason)
	if err != nil || len(citations) == 0 {
		return repairOutcome{}, usage, err
	}
	out := repairOutcome{citations: citations}
	excerpt, err := c.citedExcerpt(citations)
	var payload verdictPayload
	if err == nil {
		repaired := *rule
		repaired.Sources = citations
		var reviewUsage llm.Usage
		payload, reviewUsage, err = c.review(ctx, &repaired, excerpt)
		usage.Add(reviewUsage)
	}
	switch {
	case errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded):
		return repairOutcome{}, usage, err
	case err != nil:
		out.review, out.reason = VerdictUnchecked, err.Error()
	default:
		out.review, out.reason = payload.Verdict, payload.Reason
	}
	return out, usage, nil
}

// repairRule asks the model to re-cite the exact span of a precise symbol
// that implements the rule, then validates the proposed citations
// deterministically: each must sit in a file the rule already cited and
// overlap a real symbol body. Without precise symbols it does nothing.
func (c *Checker) repairRule(
	ctx context.Context,
	rule *graph.Node,
	reason string,
) ([]extract.Ref, llm.Usage, error) {
	if c.SymbolsFor == nil {
		return nil, llm.Usage{}, nil
	}
	// Symbols available in the files this rule already cites.
	symbolsByPath := make(map[string][]mapper.SymbolContext)
	for _, src := range rule.Sources {
		if _, seen := symbolsByPath[src.Path]; seen {
			continue
		}
		symbolsByPath[src.Path] = c.SymbolsFor(src.Path)
	}
	catalog := formatSymbolCatalog(symbolsByPath)
	if catalog == "" {
		return nil, llm.Usage{}, nil // nothing to ground against
	}

	userPrompt := fmt.Sprintf(repairUserPromptFormat,
		rule.ID, rule.Title, graph.RuleRequirement(rule.Body), reason, catalog)
	msgs := []llm.Message{
		{
			Role:    "system",
			Content: locale.For(c.Lang).Prompt(repairSystemPrompt),
		},
		{Role: "user", Content: userPrompt},
	}

	var usage llm.Usage
	payload, err := llm.ChatJSON(ctx, c.LLM, msgs, 1, func(u llm.Usage) { usage.Add(u) },
		func(reply string) (repairPayload, error) {
			var p repairPayload
			if unmarshalErr := json.Unmarshal(
				[]byte(llm.StripFences(reply)),
				&p,
			); unmarshalErr != nil {
				return p, fmt.Errorf("invalid JSON: %w", unmarshalErr)
			}
			return p, nil
		})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, usage, err
		}
		return nil, usage, nil // a failed repair just leaves the rule flagged
	}

	valid := validateRepairCitations(payload.Citations, symbolsByPath)
	if len(valid) == 0 {
		return nil, usage, nil
	}
	return valid, usage, nil
}

// validateRepairCitations keeps only citations that land in an allowed file
// and overlap a real symbol body — the mechanical grounding guarantee.
func validateRepairCitations(
	citations []extract.Ref,
	symbolsByPath map[string][]mapper.SymbolContext,
) []extract.Ref {
	var valid []extract.Ref
	seen := make(map[string]bool)
	for _, c := range citations {
		symbols, ok := symbolsByPath[c.Path]
		if !ok {
			continue // a file the rule did not originally cite
		}
		a, b, err := extract.ParseLines(c.Lines)
		if err != nil {
			continue
		}
		grounded := false
		for _, s := range symbols {
			if a <= s.EndLine && b >= s.StartLine {
				grounded = true
				break
			}
		}
		if !grounded {
			continue
		}
		key := c.Path + "|" + c.Lines
		if !seen[key] {
			seen[key] = true
			valid = append(valid, c)
		}
	}
	return valid
}

// formatSymbolCatalog renders the symbols available per file as citation
// targets for the repair prompt.
func formatSymbolCatalog(symbolsByPath map[string][]mapper.SymbolContext) string {
	paths := make([]string, 0, len(symbolsByPath))
	for path, syms := range symbolsByPath {
		if len(syms) > 0 {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	var b strings.Builder
	for _, path := range paths {
		syms := append([]mapper.SymbolContext(nil), symbolsByPath[path]...)
		sort.SliceStable(syms, func(i, j int) bool { return syms[i].StartLine < syms[j].StartLine })
		fmt.Fprintf(&b, "%s:\n", path)
		for _, s := range syms {
			kind := s.Kind
			if kind == "" {
				kind = "symbol"
			}
			fmt.Fprintf(&b, "  - %s (%s) lines %d-%d", s.Name, kind, s.StartLine, s.EndLine)
			if s.Signature != "" {
				fmt.Fprintf(&b, ": %s", s.Signature)
			}
			b.WriteByte('\n')
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// citedExcerpt reads the cited lines from the source tree, with 1-based line
// numbers and up to contextLines of surrounding source on each side. Cited
// lines are marked ">", context lines "·", so the reviewer can see enclosing
// conditions while still judging the citation itself. Capped at
// maxExcerptLines.
func (c *Checker) citedExcerpt(sources []extract.Ref) (string, error) {
	var b strings.Builder
	budget := maxExcerptLines
	for _, src := range sources {
		start, end, err := extract.ParseLines(src.Lines)
		if err != nil {
			return "", err
		}
		data, err := os.ReadFile(filepath.Join(c.SrcRoot, filepath.FromSlash(src.Path)))
		if err != nil {
			return "", fmt.Errorf("read cited file: %w", err)
		}
		lines := strings.Split(string(data), "\n")
		if end > len(lines) {
			end = len(lines)
		}
		from := max(start-contextLines, 1)
		to := min(end+contextLines, len(lines))
		fmt.Fprintf(&b, "--- %s:%s ---\n", src.Path, src.Lines)
		for i := from; i <= to && budget > 0; i++ {
			marker := "·"
			if i >= start && i <= end {
				marker = ">"
			}
			fmt.Fprintf(&b, "%5d %s %s\n", i, marker, lines[i-1])
			budget--
		}
		if budget == 0 {
			b.WriteString("... (excerpt truncated)\n")
			break
		}
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

// Annotate writes verdicts into the rule nodes (Extra["crosscheck"]) and,
// for repaired rules, replaces their citations with the grounded ones. A rule
// whose check failed, or that has no verdict at all, is annotated
// VerdictUnchecked; a mechanical rule is annotated VerdictMechanical, and an
// inherited one keeps the verdict of the spec it comes from.
func Annotate(nodes []graph.Node, verdictsByRule map[string]Verdict) []graph.Node {
	for i := range nodes {
		if nodes[i].Type != graph.TypeRule {
			continue
		}
		if nodes[i].Extra == nil {
			nodes[i].Extra = map[string]string{}
		}
		if nodes[i].Extra[graph.ExtraExtractedBy] != "" {
			nodes[i].Extra["crosscheck"] = VerdictMechanical
			continue
		}
		if nodes[i].Extra[graph.ExtraInheritedFrom] != "" {
			continue // its spec's verdict, set when it was built
		}
		v, ok := verdictsByRule[nodes[i].ID]
		if !ok || v.Failed {
			nodes[i].Extra["crosscheck"] = VerdictUnchecked
			continue
		}
		nodes[i].Extra["crosscheck"] = v.Verdict
		if v.Verdict == VerdictRepaired && len(v.NewCitations) > 0 {
			nodes[i].Sources = v.NewCitations
		}
	}
	return nodes
}

// Tally counts verdicts for reporting.
type Tally struct {
	Supported   int
	Partial     int
	Unsupported int
	Repaired    int
	Unchecked   int
	Mechanical  int
}

// Count tallies the crosscheck annotations of annotated rule nodes (see
// Annotate), so rules that never got a verdict are counted as unchecked.
func Count(nodes []graph.Node) Tally {
	var t Tally
	for _, n := range nodes {
		if n.Type != graph.TypeRule {
			continue
		}
		switch n.Extra["crosscheck"] {
		case VerdictSupported:
			t.Supported++
		case VerdictPartial:
			t.Partial++
		case VerdictUnsupported:
			t.Unsupported++
		case VerdictRepaired:
			t.Repaired++
		case VerdictUnchecked:
			t.Unchecked++
		case VerdictMechanical:
			t.Mechanical++
		}
	}
	return t
}

// promptKey covers everything besides the rule and its cited code that
// shapes a verdict: model, language, both prompts and whether repair runs.
func (c *Checker) promptKey() string {
	text := locale.For(c.Lang)
	parts := []string{
		text.Prompt(systemPrompt), userPromptFormat,
		text.Prompt(repairSystemPrompt), repairUserPromptFormat,
		strconv.FormatBool(c.Repair),
	}
	if c.Repair {
		// How a repair is adopted is part of what a cached verdict means:
		// verdicts cached before repairs were re-reviewed are judged again.
		parts = append(parts, repairPolicy)
	}
	return llm.PromptKey(c.Model, parts...)
}

// repairPolicy states how a repaired citation is adopted (see tryRepair).
const repairPolicy = "a repaired citation is adopted only when a fresh blind review supports it"

// inputHash keys a cached verdict: the prompt key plus the rule and the
// exact cited code it was judged against.
func inputHash(promptKey string, rule *graph.Node, excerpt string) string {
	sum := sha256.Sum256(
		fmt.Appendf(nil, "%s\n%s\n%s\n%s\n%s", promptKey, rule.ID, rule.Title, rule.Body, excerpt),
	)
	return hex.EncodeToString(sum[:])[:16]
}
