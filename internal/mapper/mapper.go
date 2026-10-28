// Package mapper runs the MAP phase: one LLM call per chunk, in a worker
// pool, with deterministic validation and bounded self-correction.
package mapper

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/StevenACoffman/codetospec/internal/atomicfs"
	"github.com/StevenACoffman/codetospec/internal/ears"
	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/llm"
	"github.com/StevenACoffman/codetospec/internal/locale"
	"github.com/StevenACoffman/codetospec/internal/sitter"
	"github.com/StevenACoffman/codetospec/internal/workpool"
)

const systemPrompt = `You are a senior software archaeologist. You read legacy source code in any language and extract candidate business rules.

Hard rules:
- Output ONLY a valid JSON object matching the schema provided by the user. No markdown fences, no prose.
- The CODE is shown with an absolute line number prefixed to every line ("712\t<code>"). Cite using those exact numbers — copy the number shown next to the code, never count lines yourself. Lines under a CONTEXT block are for reference only; never cite them.
- Every rule MUST cite one or more exact line ranges inside the provided range. Never cite outside it.
- When PRECISE_SYMBOLS is present, cite the exact line span of the symbol whose logic a rule describes; do not cite blank lines, import blocks or type declarations that carry no behavior.
- entities MUST be a subset of ALLOWED_ENTITIES. endpoints MUST be a subset of ALLOWED_ENDPOINTS. When unsure, use [].
- Write each rule's requirement as EARS components in "ears", in <LANG>, each part a phrase without its keyword: "where" the features it applies to (0 or more), "while" the states and preconditions it holds under (0 or more, a precondition of the trigger included), "when" the event that triggers it (0 or 1), "if" the unwanted condition it handles (0 or 1; with a "when" it is a second trigger, "WHEN x, IF y, THEN"), "system" one of ALLOWED_ACTORS when that part of the system performs the response; omit it otherwise (the system as a whole), "shall" what the actor does (1 or more; one behavior per rule, one item per action: "log an error and return the cached value" is two, "log an error" and "return the cached value"; a list the action applies to stays one, "validate the role, the source and the invariants"), each {"text": the verb phrase that follows "the system shall", "action": its opening words naming what is done, negation included ("not store" in "not store card numbers")}. Leave out a component the code does not condition the rule on. The sentence is written from them, in this order:
<EARS>
- Focus on business behavior: validation, calculation, state transitions, side effects, authorization. Ignore framework plumbing.
- Classify each rule:
  - nature: "business" (domain behavior: validation, calculation, pricing, authorization), "presentation" (display, formatting, UI text, rendering), or "technical" (framework wiring, logging, serialization, infrastructure).
  - origin: "explicit" (the behavior is directly coded, e.g. an if/throw) or "implicit" (it emerges from data flow or structure and is inferred).
- confidence is your certainty in [0,1] that this is a real, correctly-cited rule.
- If the chunk contains no business rule, return {"chunk_summary": "...", "rules": []}.`

const userPromptFormat = `FILE: %s (lines %d-%d)
LANGUAGE: %s
NAMESPACE: %s
DOMAIN: %s
ALLOWED_ENTITIES: %s
ALLOWED_ENDPOINTS: %s
ALLOWED_ACTORS: %s
%s
OUTPUT JSON SCHEMA:
{"chunk_summary": string, "rules": [{"title": string, "ears": {"where": [string], "while": [string], "when": string, "if": string, "system": string, "shall": [{"text": string, "action": string}]}, "citations": [{"path": string, "lines": "A-B"}], "entities": [string], "endpoints": [string], "nature": "business|presentation|technical", "origin": "explicit|implicit", "confidence": number}]}

%s`

// numberedContent renders a chunk's code with the ABSOLUTE line number
// prefixed to every line, so the model cites the numbers it sees instead of
// counting from the chunk's start (which drifts on large chunks and produced
// systematically off-by-N citations). For a per-method chunk, the enclosing
// declaration is shown as non-citable CONTEXT with its own real line numbers,
// and only the CODE (body) lines are numbered from the chunk start — keeping
// every citable line number true to the source.
func numberedContent(chunk *sitter.Chunk) string {
	var b strings.Builder
	if chunk.Context != "" {
		b.WriteString(
			"CONTEXT (enclosing declaration, shown for reference — do NOT cite these lines):\n",
		)
		for i, line := range strings.Split(chunk.Context, "\n") {
			fmt.Fprintf(&b, "%d\t%s\n", chunk.ContextStart+i, line)
		}
	}
	b.WriteString("CODE (cite these exact line numbers):\n")
	for i, line := range strings.Split(chunk.Content, "\n") {
		fmt.Fprintf(&b, "%d\t%s\n", chunk.StartLine+i, line)
	}
	return strings.TrimRight(b.String(), "\n")
}

// Rule is one candidate business rule produced by the map phase.
type Rule struct {
	Title string          `json:"title"`
	EARS  ears.Components `json:"ears"`
	// EarsKind and Requirement are derived from EARS (ears.KindOf,
	// ears.Render) by WithText, never written by the model nor stored:
	// the JSON of a Rule is what the model reads and writes.
	EarsKind    string        `json:"-"`
	Requirement string        `json:"-"`
	Citations   []extract.Ref `json:"citations"`
	Entities    []string      `json:"entities"`
	Endpoints   []string      `json:"endpoints"`
	Nature      string        `json:"nature"` // "business" | "presentation" | "technical"
	Origin      string        `json:"origin"` // "explicit" | "implicit"
	Confidence  float64       `json:"confidence"`
}

// IsNature reports whether nature classifies what a rule describes:
// business, presentation or technical.
func IsNature(nature string) bool {
	switch nature {
	case "business", "presentation", "technical":
		return true
	}
	return false
}

// IsOrigin reports whether origin says how directly a rule is stated in the
// code: explicit or implicit.
func IsOrigin(origin string) bool {
	return origin == "explicit" || origin == "implicit"
}

// Output is the persisted result of mapping one chunk.
type Output struct {
	ChunkID      string `json:"chunk_id"`
	Path         string `json:"path"`
	StartLine    int    `json:"start_line"`
	EndLine      int    `json:"end_line"`
	Domain       string `json:"domain"`
	ChunkSummary string `json:"chunk_summary"`
	Rules        []Rule `json:"rules"`
	Failed       bool   `json:"failed,omitempty"`
	Error        string `json:"error,omitempty"`
	// PromptKey records the model and prompt templates that produced this
	// output; a cached output is reused only while it matches.
	PromptKey string `json:"prompt_key"`
	// Regrounded records that its ungrounded rules already had their one
	// correction exchange (see reground), so --reground does not pay twice.
	Regrounded bool `json:"regrounded,omitempty"`
}

// mapPayload mirrors the JSON schema requested from the LLM.
type mapPayload struct {
	ChunkSummary string `json:"chunk_summary"`
	Rules        []Rule `json:"rules"`
}

// SymbolContext is a precisely-located symbol definition (from a SCIP index)
// injected into a chunk's prompt so the model cites real spans, not guesses.
type SymbolContext struct {
	Name      string
	StartLine int
	EndLine   int
	Kind      string
	Signature string
}

// Note is text an extractor attached to a line span: a mechanical rule's
// requirement, or authored documentation.
type Note struct {
	StartLine int
	EndLine   int
	Text      string
}

// Mapper drives the map phase.
type Mapper struct {
	LLM llm.Chatter
	// Log receives progress and degraded-mode warnings (required).
	Log      *slog.Logger
	Model    string // model id, folded into the cache key
	Lang     string // "fr" | "en"
	Workers  int
	OutDir   string   // <out>/.codetospec/map
	Entities []string // allowed entity ids, sorted
	// Actors are the actors a rule may name (config actors); none leaves
	// every rule to "the system". Like Entities, outside the prompt key: a
	// cached rule naming an actor no longer listed falls back to "the
	// system" (ListedActors).
	Actors       Actors
	EndpointsFor func(path string) []string
	// SymbolsFor returns the precise symbol definitions known for a file
	// (empty when no SCIP index was provided). Optional.
	SymbolsFor func(path string) []SymbolContext
	// RulesFor returns the mechanical rules (extractor rule facts) of a
	// file — Text is the requirement — shown to the model so it does not
	// restate them. Optional.
	RulesFor func(path string) []Note
	// DocsFor returns authored documentation (extractor doc facts) located
	// in a file, shown for the chunks it overlaps. Optional.
	DocsFor func(path string) []Note
	// Ungrounded, when set, checks each mapped requirement against its cited
	// lines; a reply with ungrounded rules gets one correction exchange,
	// adopted only when it improves (see reground). Optional.
	Ungrounded Ungrounded
	// OnUnit is called after each finished unit (cached or live) with the
	// usage consumed by that unit; callers persist state there.
	OnUnit func(out Output, usage llm.Usage)
	// Reground gives cached outputs with ungrounded rules the correction
	// exchange a fresh reply gets (once per output; needs Ungrounded).
	Reground bool
}

// mapProgressEvery is how many mapped chunks separate two progress logs.
const mapProgressEvery = 25

// mapCorrections is how many times a malformed map reply is sent back for
// correction before the chunk is recorded as failed.
const mapCorrections = 2

// Run maps every chunk, reusing a chunk's cached output when it was produced
// by the same model and prompt templates. Results are returned sorted by
// (path, start line).
func (m *Mapper) Run(ctx context.Context, chunks []sitter.Chunk) ([]Output, error) {
	if err := os.MkdirAll(m.OutDir, 0o750); err != nil {
		return nil, fmt.Errorf("create map dir: %w", err)
	}
	var (
		outputs []Output
		runErr  error
	)
	workpool.Run(ctx, m.Workers, chunks,
		func(ctx context.Context, chunk *sitter.Chunk) mapped {
			out, usage, err := m.mapChunk(ctx, chunk)
			return mapped{out: out, usage: usage, err: err}
		},
		func(r mapped) {
			if r.err != nil {
				runErr = cmp.Or(runErr, r.err)
				return
			}
			outputs = append(outputs, r.out)
			if m.OnUnit != nil {
				m.OnUnit(r.out, r.usage)
			}
			if len(outputs)%mapProgressEvery == 0 {
				m.Log.InfoContext(ctx, "map progress", "done", len(outputs), "total", len(chunks))
			}
		})
	if runErr == nil && ctx.Err() != nil {
		runErr = ctx.Err()
	}
	sort.SliceStable(outputs, func(i, j int) bool {
		if outputs[i].Path != outputs[j].Path {
			return outputs[i].Path < outputs[j].Path
		}
		return outputs[i].StartLine < outputs[j].StartLine
	})
	return outputs, runErr
}

// mapped is one chunk's result.
type mapped struct {
	out   Output
	usage llm.Usage
	err   error
}

// mapChunk maps one chunk, reusing a cached output when present.
func (m *Mapper) mapChunk(ctx context.Context, chunk *sitter.Chunk) (Output, llm.Usage, error) {
	// The file name keys the chunk content; PromptKey keys everything else
	// that shapes the reply, including the chunk's context sections.
	outPath := filepath.Join(m.OutDir, chunk.ID+".json")
	sections := m.symbolsSection(chunk) + m.rulesSection(chunk) + m.docsSection(chunk)
	promptKey := m.promptKey(sections)
	if data, err := os.ReadFile(filepath.Clean(outPath)); err == nil {
		var cached Output
		switch unmarshalErr := json.Unmarshal(data, &cached); {
		case unmarshalErr != nil:
			m.Log.WarnContext(ctx, "corrupt map cache, re-mapping", "path", outPath)
		case cached.PromptKey == promptKey:
			// The domain is assignment metadata, not part of the reply: take
			// the current one so a domain change regroups cached outputs
			// instead of silently keeping the old grouping.
			cached.Domain = chunk.Domain
			rules, unlisted := ListedActors(cached.Rules, &m.Actors)
			if unlisted > 0 {
				m.Log.InfoContext(ctx, "cached rules name actors no longer listed; "+
					"they now state the system", "chunk", chunk.ID, "rules", unlisted)
			}
			cached.Rules = WithText(rules, m.Lang)
			return m.regroundCached(ctx, &cached, chunk, sections, outPath)
		}
	}

	allowedEndpoints := m.EndpointsFor(chunk.Path)
	msgs := m.prompt(chunk, sections, allowedEndpoints)
	var usage llm.Usage
	validate := func(reply string) (mapPayload, error) {
		return validateMapReply(
			reply,
			chunk,
			m.Lang,
			&Allowed{m.Entities, allowedEndpoints, m.Actors},
		)
	}
	payload, err := llm.ChatJSON(
		ctx,
		m.LLM,
		msgs,
		mapCorrections,
		func(u llm.Usage) { usage.Add(u) },
		validate,
	)
	regrounded := false
	if err == nil && m.Ungrounded != nil {
		var regroundUsage llm.Usage
		payload, regroundUsage, regrounded = m.reground(ctx, msgs, payload, chunk, validate)
		usage.Add(regroundUsage)
	}

	out := Output{
		ChunkID:    chunk.ID,
		Path:       chunk.Path,
		StartLine:  chunk.StartLine,
		EndLine:    chunk.EndLine,
		Domain:     chunk.Domain,
		PromptKey:  promptKey,
		Regrounded: regrounded,
	}
	switch {
	case err == nil:
		out.ChunkSummary = payload.ChunkSummary
		out.Rules = payload.Rules
	case errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded):
		return Output{}, usage, err
	default:
		m.Log.WarnContext(ctx, "chunk failed", "chunk", chunk.ID, "path", chunk.Path, "err", err)
		out.Failed = true
		out.Error = err.Error()
	}

	if err = writeOutput(outPath, &out); err != nil {
		return Output{}, usage, err
	}
	return out, usage, nil
}

// prompt is the conversation that maps chunk.
func (m *Mapper) prompt(
	chunk *sitter.Chunk,
	sections string,
	allowedEndpoints []string,
) []llm.Message {
	namespace := chunk.Namespace
	if namespace == "" {
		namespace = "-"
	}
	userPrompt := fmt.Sprintf(userPromptFormat,
		chunk.Path, chunk.StartLine, chunk.EndLine,
		chunk.Language, namespace, chunk.Domain,
		mustJSON(m.Entities), mustJSON(allowedEndpoints), mustJSON(m.Actors.Names),
		sections,
		numberedContent(chunk),
	)
	return []llm.Message{
		{Role: "system", Content: locale.For(m.Lang).Prompt(systemPrompt)},
		{Role: "user", Content: userPrompt},
	}
}

// regroundCached gives a cached output, under Reground, the correction
// exchange its ungrounded rules would get from a fresh reply; the outcome is
// saved, Regrounded set, so a rerun does not pay twice. Without Reground, or
// with nothing to correct, the output is returned as cached.
func (m *Mapper) regroundCached(
	ctx context.Context,
	cached *Output,
	chunk *sitter.Chunk,
	sections, outPath string,
) (Output, llm.Usage, error) {
	if !m.Reground || m.Ungrounded == nil || cached.Failed || cached.Regrounded {
		return *cached, llm.Usage{}, nil
	}
	payload := mapPayload{ChunkSummary: cached.ChunkSummary, Rules: cached.Rules}
	if len(m.misses(payload, chunk)) == 0 {
		return *cached, llm.Usage{}, nil
	}
	allowedEndpoints := m.EndpointsFor(chunk.Path)
	validate := func(reply string) (mapPayload, error) {
		return validateMapReply(
			reply,
			chunk,
			m.Lang,
			&Allowed{m.Entities, allowedEndpoints, m.Actors},
		)
	}
	fixed, usage, _ := m.reground(
		ctx,
		m.prompt(chunk, sections, allowedEndpoints),
		payload,
		chunk,
		validate,
	)
	cached.ChunkSummary, cached.Rules, cached.Regrounded = fixed.ChunkSummary, fixed.Rules, true
	if err := writeOutput(outPath, cached); err != nil {
		return Output{}, usage, err
	}
	return *cached, usage, nil
}

// writeOutput saves a map output atomically.
func writeOutput(outPath string, out *Output) error {
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Errorf("encode map output: %w", err)
	}
	if writeErr := atomicfs.WriteFile(outPath, append(data, '\n')); writeErr != nil {
		return fmt.Errorf("write map output: %w", writeErr)
	}
	return nil
}

// promptKey covers everything besides the chunk content that shapes a map
// reply: model, language, prompt templates and the chunk's context sections
// (precise symbols, mechanical rules). The allowed entity and endpoint lists
// are left out on purpose: they change with every new table or route, and
// re-mapping the whole tree for that would cost more than it improves.
func (m *Mapper) promptKey(sections string) string {
	return llm.PromptKey(
		m.Model,
		locale.For(m.Lang).Prompt(systemPrompt),
		userPromptFormat,
		sections,
	)
}

// docsSection shows the authored documentation of the code in a chunk, so
// the model uses its authors' vocabulary.
func (m *Mapper) docsSection(chunk *sitter.Chunk) string {
	if m.DocsFor == nil {
		return ""
	}
	return noteSection(chunk, m.DocsFor(chunk.Path),
		"SCHEMA_DOCS (documentation written by the code's authors; context only, never a citation)")
}

// rulesSection lists the mechanical rules cited inside a chunk so the model
// extracts only the remaining behavior.
func (m *Mapper) rulesSection(chunk *sitter.Chunk) string {
	if m.RulesFor == nil {
		return ""
	}
	return noteSection(
		chunk,
		m.RulesFor(chunk.Path),
		"MECHANICAL_RULES (already extracted deterministically from this code; do NOT restate them, extract only other behavior)",
	)
}

// noteSection renders the notes overlapping chunk under header, one line
// each; "" when none overlaps.
func noteSection(chunk *sitter.Chunk, notes []Note, header string) string {
	var b strings.Builder
	for _, n := range notes {
		if n.StartLine <= chunk.EndLine && n.EndLine >= chunk.StartLine {
			fmt.Fprintf(&b, "- lines %d-%d: %s\n", n.StartLine, n.EndLine, n.Text)
		}
	}
	if b.Len() == 0 {
		return ""
	}
	return "\n" + header + ":\n" + b.String()
}

// symbolsSection renders the precise symbol definitions overlapping a chunk,
// so the model cites their exact spans instead of guessing. Returns "" when
// no SCIP index is available.
func (m *Mapper) symbolsSection(chunk *sitter.Chunk) string {
	if m.SymbolsFor == nil {
		return ""
	}
	var relevant []SymbolContext
	for _, s := range m.SymbolsFor(chunk.Path) {
		if s.StartLine <= chunk.EndLine && s.EndLine >= chunk.StartLine {
			relevant = append(relevant, s)
		}
	}
	if len(relevant) == 0 {
		return ""
	}
	sort.SliceStable(
		relevant,
		func(i, j int) bool { return relevant[i].StartLine < relevant[j].StartLine },
	)
	var b strings.Builder
	b.WriteString(
		"\nPRECISE_SYMBOLS (exact definitions resolved by an indexer; cite these line spans, never guess):\n",
	)
	for _, s := range relevant {
		kind := s.Kind
		if kind == "" {
			kind = "symbol"
		}
		fmt.Fprintf(&b, "- %s (%s) lines %d-%d", s.Name, kind, s.StartLine, s.EndLine)
		if s.Signature != "" {
			fmt.Fprintf(&b, ": %s", s.Signature)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// validateMapReply enforces the deterministic contract on one map reply,
// and returns its rules with their text in lang (WithText).
func validateMapReply(
	reply string,
	chunk *sitter.Chunk,
	lang string,
	lists *Allowed,
) (mapPayload, error) {
	var payload mapPayload
	if err := json.Unmarshal([]byte(llm.StripFences(reply)), &payload); err != nil {
		return payload, fmt.Errorf("invalid JSON: %w", err)
	}
	for i := range payload.Rules {
		rule := &payload.Rules[i]
		if err := checkCitations(i, rule.Citations, chunk); err != nil {
			return payload, err
		}
		normalized := rule.EARS.Normalized()
		rule.EARS = lists.Actors.Named(&normalized)
		labels := Labels{
			EARS: &rule.EARS, Nature: rule.Nature, Origin: rule.Origin,
			Confidence: rule.Confidence, Entities: rule.Entities, Endpoints: rule.Endpoints,
		}
		if err := CheckLabels(&labels, lists); err != nil {
			return payload, fmt.Errorf("rules[%d].%w", i, err)
		}
	}
	payload.Rules = WithText(payload.Rules, lang)
	return payload, nil
}

// Allowed are the lists a map or reduce rule's labels keep to: entity
// and endpoint ids, and actors.
type Allowed struct {
	Entities, Endpoints []string
	Actors              Actors
}

// Actors are the actors a rule may name instead of the system (config
// actors): Names, sorted, shown to the model as ALLOWED_ACTORS; Aliases,
// alias → name, the other names a rule may give one, accepted without a
// correction and never shown.
type Actors struct {
	Names   []string
	Aliases map[string]string
}

// Lists reports whether a rule may name system: a listed name, or ""
// for the system.
func (a *Actors) Lists(system string) bool {
	return system == "" || slices.Contains(a.Names, system)
}

// Named returns c naming its actor by the listed name when c names it by
// an alias; c otherwise (an unlisted actor is CheckLabels's to refuse).
func (a *Actors) Named(c *ears.Components) ears.Components {
	out := *c
	if name, ok := a.Aliases[c.System]; ok {
		out.System = name
	}
	return out
}

// Listed returns c as a cached rule keeps it: its actor by the listed
// name (Named), or the system when the glossary lists it no more; and
// whether it fell back to the system.
func (a *Actors) Listed(c *ears.Components) (ears.Components, bool) {
	out := a.Named(c)
	if a.Lists(out.System) {
		return out, false
	}
	out.System = ""
	return out, true
}

// ListedActors returns rules with each actor as Actors.Listed keeps it,
// and how many fell back to the system.
func ListedActors(rules []Rule, actors *Actors) ([]Rule, int) {
	out := slices.Clone(rules)
	unlisted := 0
	for i := range out {
		var dropped bool
		if out[i].EARS, dropped = actors.Listed(&out[i].EARS); dropped {
			unlisted++
		}
	}
	return out, unlisted
}

// WithText returns rules with the kind and sentence their components make
// in lang.
//
// Requires: each rule's EARS passed ears.Validate.
func WithText(rules []Rule, lang string) []Rule {
	out := slices.Clone(rules)
	for i := range out {
		out[i].EarsKind = ears.KindOf(&out[i].EARS)
		out[i].Requirement = ears.Render(&out[i].EARS, lang)
	}
	return out
}

// checkCitations requires rule i to cite at least one span, each within
// the chunk.
func checkCitations(i int, citations []extract.Ref, chunk *sitter.Chunk) error {
	if len(citations) == 0 {
		return fmt.Errorf("rules[%d] has no citation", i)
	}
	for _, c := range citations {
		if c.Path != chunk.Path {
			return fmt.Errorf("rules[%d] cites path %q, must be %q", i, c.Path, chunk.Path)
		}
		a, b, err := extract.ParseLines(c.Lines)
		if err != nil {
			return fmt.Errorf("rules[%d]: %w", i, err)
		}
		if a < chunk.StartLine || b > chunk.EndLine {
			return fmt.Errorf("rules[%d] cites lines %s outside %d-%d",
				i, c.Lines, chunk.StartLine, chunk.EndLine)
		}
	}
	return nil
}

// Labels is what a map or reduce rule declares about itself besides its
// title and citations.
type Labels struct {
	EARS                *ears.Components
	Nature, Origin      string
	Confidence          float64
	Entities, Endpoints []string
}

// CheckLabels checks a rule's labels: EARS components that make one
// sentence (ears.Validate) with an allowed actor, a known nature and
// origin, a confidence in [0,1], and entity and endpoint ids among the
// allowed ones. An error starts with the offending field, for the caller to prefix
// with the rule's position ("rules[2]."); map and reduce replies share it.
func CheckLabels(l *Labels, a *Allowed) error {
	if err := ears.Validate(l.EARS); err != nil {
		return err
	}
	if s := l.EARS.System; !a.Actors.Lists(s) {
		return fmt.Errorf("ears.system %q is not in ALLOWED_ACTORS; omit it for the system", s)
	}
	switch {
	case !IsNature(l.Nature):
		return fmt.Errorf("nature %q is not one of business|presentation|technical", l.Nature)
	case !IsOrigin(l.Origin):
		return fmt.Errorf("origin %q is not one of explicit|implicit", l.Origin)
	case l.Confidence < 0 || l.Confidence > 1:
		return fmt.Errorf("confidence %v is outside [0,1]", l.Confidence)
	}
	if bad := notSubset(l.Entities, a.Entities); bad != "" {
		return fmt.Errorf("entities contains %q which is not in ALLOWED_ENTITIES", bad)
	}
	if bad := notSubset(l.Endpoints, a.Endpoints); bad != "" {
		return fmt.Errorf("endpoints contains %q which is not in ALLOWED_ENDPOINTS", bad)
	}
	return nil
}

// notSubset returns the first element of values missing from allowed.
func notSubset(values, allowed []string) string {
	set := make(map[string]bool, len(allowed))
	for _, a := range allowed {
		set[a] = true
	}
	for _, v := range values {
		if !set[v] {
			return v
		}
	}
	return ""
}

func mustJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return "[]"
	}
	if string(data) == "null" {
		return "[]"
	}
	return string(data)
}
