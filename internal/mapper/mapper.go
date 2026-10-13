// Package mapper runs the MAP phase: one LLM call per chunk, in a worker
// pool, with deterministic validation and bounded self-correction.
package mapper

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"codetospec/internal/extract"
	"codetospec/internal/llm"
	"codetospec/internal/locale"
	"codetospec/internal/sitter"
	"codetospec/internal/state"
)

// EARSKinds enumerates the accepted EARS pattern kinds.
var EARSKinds = map[string]bool{
	"ubiquitous": true,
	"event":      true,
	"state":      true,
	"unwanted":   true,
	"optional":   true,
}

const systemPrompt = `You are a senior software archaeologist. You read legacy source code in any language and extract candidate business rules.

Hard rules:
- Output ONLY a valid JSON object matching the schema provided by the user. No markdown fences, no prose.
- The CODE is shown with an absolute line number prefixed to every line ("712\t<code>"). Cite using those exact numbers — copy the number shown next to the code, never count lines yourself. Lines under a CONTEXT block are for reference only; never cite them.
- Every rule MUST cite one or more exact line ranges inside the provided range. Never cite outside it.
- When PRECISE_SYMBOLS is present, cite the exact line span of the symbol whose logic a rule describes; do not cite blank lines, import blocks or type declarations that carry no behavior.
- entities MUST be a subset of ALLOWED_ENTITIES. endpoints MUST be a subset of ALLOWED_ENDPOINTS. When unsure, use [].
- Write the requirement in <LANG> using one EARS pattern:
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
%s
OUTPUT JSON SCHEMA:
{"chunk_summary": string, "rules": [{"title": string, "ears_kind": "ubiquitous|event|state|unwanted|optional", "requirement": string, "citations": [{"path": string, "lines": "A-B"}], "entities": [string], "endpoints": [string], "nature": "business|presentation|technical", "origin": "explicit|implicit", "confidence": number}]}

%s`

// numberedContent renders a chunk's code with the ABSOLUTE line number
// prefixed to every line, so the model cites the numbers it sees instead of
// counting from the chunk's start (which drifts on large chunks and produced
// systematically off-by-N citations). For a per-method chunk, the enclosing
// declaration is shown as non-citable CONTEXT with its own real line numbers,
// and only the CODE (body) lines are numbered from the chunk start — keeping
// every citable line number true to the source.
func numberedContent(chunk sitter.Chunk) string {
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
	Title       string        `json:"title"`
	EarsKind    string        `json:"ears_kind"`
	Requirement string        `json:"requirement"`
	Citations   []extract.Ref `json:"citations"`
	Entities    []string      `json:"entities"`
	Endpoints   []string      `json:"endpoints"`
	Nature      string        `json:"nature"` // "business" | "presentation" | "technical"
	Origin      string        `json:"origin"` // "explicit" | "implicit"
	Confidence  float64       `json:"confidence"`
}

// Natures classifies what a rule describes.
var Natures = map[string]bool{"business": true, "presentation": true, "technical": true}

// Origins classifies how directly a rule is stated in the code.
var Origins = map[string]bool{"explicit": true, "implicit": true}

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
	LLM          llm.Chatter
	Model        string // model id, folded into the cache key
	Lang         string // "fr" | "en"
	Workers      int
	OutDir       string   // <out>/.codetospec/map
	Entities     []string // allowed entity ids, sorted
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
}

// Run maps every chunk, reusing a chunk's cached output when it was produced
// by the same model and prompt templates. Results are returned sorted by (path, start line).
func (m *Mapper) Run(ctx context.Context, chunks []sitter.Chunk) ([]Output, error) {
	if err := os.MkdirAll(m.OutDir, 0o755); err != nil {
		return nil, fmt.Errorf("create map dir: %w", err)
	}
	workers := max(m.Workers, 1)

	jobs := make(chan sitter.Chunk)
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		outputs []Output
		done    int
		runErr  error
	)

	record := func(out Output, usage llm.Usage, err error) {
		mu.Lock()
		defer mu.Unlock()
		if err != nil {
			if runErr == nil {
				runErr = err
			}
			return
		}
		outputs = append(outputs, out)
		done++
		if m.OnUnit != nil {
			m.OnUnit(out, usage)
		}
		if done%25 == 0 {
			slog.InfoContext(ctx, "map progress", "done", done, "total", len(chunks))
		}
	}

	for range workers {
		wg.Go(func() {
			for chunk := range jobs {
				out, usage, err := m.mapChunk(ctx, chunk)
				record(out, usage, err)
			}
		})
	}

feed:
	for _, chunk := range chunks {
		select {
		case <-ctx.Done():
			break feed
		case jobs <- chunk:
		}
	}
	close(jobs)
	wg.Wait()

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

// mapChunk maps one chunk, reusing a cached output when present.
func (m *Mapper) mapChunk(ctx context.Context, chunk sitter.Chunk) (Output, llm.Usage, error) {
	// The file name keys the chunk content; PromptKey keys everything else
	// that shapes the reply, including the chunk's context sections.
	outPath := filepath.Join(m.OutDir, chunk.ID+".json")
	sections := m.symbolsSection(&chunk) + m.rulesSection(&chunk) + m.docsSection(&chunk)
	promptKey := m.promptKey(sections)
	if data, err := os.ReadFile(outPath); err == nil {
		var cached Output
		switch unmarshalErr := json.Unmarshal(data, &cached); {
		case unmarshalErr != nil:
			slog.WarnContext(ctx, "corrupt map cache, re-mapping", "path", outPath)
		case cached.PromptKey == promptKey:
			// The domain is assignment metadata, not part of the reply: take
			// the current one so a domain change regroups cached outputs
			// instead of silently keeping the old grouping.
			cached.Domain = chunk.Domain
			return cached, llm.Usage{}, nil
		}
	}

	allowedEndpoints := m.EndpointsFor(chunk.Path)
	namespace := chunk.Namespace
	if namespace == "" {
		namespace = "-"
	}
	userPrompt := fmt.Sprintf(userPromptFormat,
		chunk.Path, chunk.StartLine, chunk.EndLine,
		chunk.Language, namespace, chunk.Domain,
		mustJSON(m.Entities), mustJSON(allowedEndpoints),
		sections,
		numberedContent(chunk),
	)
	msgs := []llm.Message{
		{Role: "system", Content: locale.For(m.Lang).Prompt(systemPrompt)},
		{Role: "user", Content: userPrompt},
	}

	var usage llm.Usage
	validate := func(reply string) (mapPayload, error) {
		return validateMapReply(reply, chunk, m.Entities, allowedEndpoints)
	}
	payload, err := llm.ChatJSON(ctx, m.LLM, msgs, 2, func(u llm.Usage) { usage.Add(u) }, validate)
	if err == nil && m.Ungrounded != nil {
		var regroundUsage llm.Usage
		payload, regroundUsage = m.reground(ctx, msgs, payload, &chunk, validate)
		usage.Add(regroundUsage)
	}

	out := Output{
		ChunkID:   chunk.ID,
		Path:      chunk.Path,
		StartLine: chunk.StartLine,
		EndLine:   chunk.EndLine,
		Domain:    chunk.Domain,
		PromptKey: promptKey,
	}
	switch {
	case err == nil:
		out.ChunkSummary = payload.ChunkSummary
		out.Rules = payload.Rules
	case errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded):
		return Output{}, usage, err
	default:
		slog.WarnContext(ctx, "chunk failed", "chunk", chunk.ID, "path", chunk.Path, "err", err)
		out.Failed = true
		out.Error = err.Error()
	}

	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return Output{}, usage, fmt.Errorf("encode map output: %w", err)
	}
	if err := state.WriteFileAtomic(outPath, append(data, '\n')); err != nil {
		return Output{}, usage, fmt.Errorf("write map output: %w", err)
	}
	return out, usage, nil
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

// validateMapReply enforces the deterministic contract on one map reply.
func validateMapReply(
	reply string,
	chunk sitter.Chunk,
	allowedEntities, allowedEndpoints []string,
) (mapPayload, error) {
	var payload mapPayload
	if err := json.Unmarshal([]byte(llm.StripFences(reply)), &payload); err != nil {
		return payload, fmt.Errorf("invalid JSON: %w", err)
	}
	for i, rule := range payload.Rules {
		if !EARSKinds[rule.EarsKind] {
			return payload, fmt.Errorf(
				"rules[%d].ears_kind %q is not one of ubiquitous|event|state|unwanted|optional",
				i,
				rule.EarsKind,
			)
		}
		if len(rule.Citations) == 0 {
			return payload, fmt.Errorf("rules[%d] has no citation", i)
		}
		for _, c := range rule.Citations {
			if c.Path != chunk.Path {
				return payload, fmt.Errorf(
					"rules[%d] cites path %q, must be %q",
					i,
					c.Path,
					chunk.Path,
				)
			}
			a, b, err := extract.ParseLines(c.Lines)
			if err != nil {
				return payload, fmt.Errorf("rules[%d]: %w", i, err)
			}
			if a < chunk.StartLine || b > chunk.EndLine {
				return payload, fmt.Errorf(
					"rules[%d] cites lines %s outside %d-%d",
					i,
					c.Lines,
					chunk.StartLine,
					chunk.EndLine,
				)
			}
		}
		if bad := notSubset(rule.Entities, allowedEntities); bad != "" {
			return payload, fmt.Errorf(
				"rules[%d].entities contains %q which is not in ALLOWED_ENTITIES",
				i,
				bad,
			)
		}
		if bad := notSubset(rule.Endpoints, allowedEndpoints); bad != "" {
			return payload, fmt.Errorf(
				"rules[%d].endpoints contains %q which is not in ALLOWED_ENDPOINTS",
				i,
				bad,
			)
		}
		if !Natures[rule.Nature] {
			return payload, fmt.Errorf(
				"rules[%d].nature %q is not one of business|presentation|technical",
				i,
				rule.Nature,
			)
		}
		if !Origins[rule.Origin] {
			return payload, fmt.Errorf(
				"rules[%d].origin %q is not one of explicit|implicit",
				i,
				rule.Origin,
			)
		}
		if rule.Confidence < 0 || rule.Confidence > 1 {
			return payload, fmt.Errorf(
				"rules[%d].confidence %v is outside [0,1]",
				i,
				rule.Confidence,
			)
		}
	}
	return payload, nil
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
