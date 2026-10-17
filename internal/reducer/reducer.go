// Package reducer runs the REDUCE phase: one LLM call per domain (in a worker
// pool), consolidating candidate rules into a deduplicated specification.
package reducer

import (
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
	"sync"

	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/llm"
	"github.com/StevenACoffman/codetospec/internal/locale"
	"github.com/StevenACoffman/codetospec/internal/mapper"
	"github.com/StevenACoffman/codetospec/internal/state"
)

const systemPrompt = `You are a requirements engineer. You consolidate candidate business rules extracted from legacy code into a clean, deduplicated specification for one domain.

Hard rules:
- Output ONLY a valid JSON object matching the schema. No markdown fences, no prose.
- Merge duplicates and near-duplicates. Keep the union of their citations verbatim; never invent or alter citations.
- entities and endpoints MUST be subsets of the provided allowed lists.
- Keep requirements in <LANG>, one behavior per rule, each in one of these EARS patterns:
<EARS>
- slug: lowercase ascii, words separated by "-", max 5 words, unique within the domain.
- acceptance_criteria: 2 to 5 concrete, testable checks per rule, each restating a behavior the requirement and its cited code actually establish. Do NOT invent error cases, edge cases, thresholds, defaults or inputs the requirement does not state — a criterion that reaches beyond the cited behavior gets the whole rule rejected at review. Written in <LANG>.
- nature ("business"|"presentation"|"technical") and origin ("explicit"|"implicit"): carry over from the merged candidates; when they disagree, pick the dominant one.
- confidence: number in [0,1], your certainty in the consolidated rule (roughly the max confidence of the candidates you merged).`

const userPromptFormat = `DOMAIN: %s
ALLOWED_ENTITIES: %s
ALLOWED_ENDPOINTS: %s

OUTPUT JSON SCHEMA:
{"domain_summary": string, "rules": [{"slug": string, "title": string, "ears_kind": string, "requirement": string, "rationale": string, "citations": [{"path": string, "lines": "A-B"}], "entities": [string], "endpoints": [string], "acceptance_criteria": [string], "nature": "business|presentation|technical", "origin": "explicit|implicit", "confidence": number}]}

CANDIDATE_RULES:
%s`

// Rule is one consolidated business rule.
type Rule struct {
	Slug               string        `json:"slug"`
	Title              string        `json:"title"`
	EarsKind           string        `json:"ears_kind"`
	Requirement        string        `json:"requirement"`
	Rationale          string        `json:"rationale"`
	Citations          []extract.Ref `json:"citations"`
	Entities           []string      `json:"entities"`
	Endpoints          []string      `json:"endpoints"`
	AcceptanceCriteria []string      `json:"acceptance_criteria"`
	Nature             string        `json:"nature"`
	Origin             string        `json:"origin"`
	Confidence         float64       `json:"confidence"`
}

// Output is the persisted result of reducing one domain.
type Output struct {
	Domain         string `json:"domain"`
	DomainSummary  string `json:"domain_summary"`
	Rules          []Rule `json:"rules"`
	CandidatesHash string `json:"candidates_hash,omitempty"` // content of the candidates this was reduced from
	Failed         bool   `json:"failed,omitempty"`
	Error          string `json:"error,omitempty"`
}

// reducePayload mirrors the JSON schema requested from the LLM.
type reducePayload struct {
	DomainSummary string `json:"domain_summary"`
	Rules         []Rule `json:"rules"`
}

// Reducer drives the reduce phase.
type Reducer struct {
	LLM llm.Chatter
	// Log receives progress and degraded-mode warnings (required).
	Log       *slog.Logger
	Model     string // model id, folded into the cache key
	Lang      string
	OutDir    string   // <out>/.codetospec/reduce
	Entities  []string // allowed entity ids
	Endpoints []string // allowed endpoint ids
	BatchSize int      // candidates per reduce call; 0 uses DefaultBatchSize
	Workers   int      // parallel domains; 0 or 1 means sequential
	Mode      string   // ModeFull (default when empty) or ModeMerge
	// OnUnit is called after each finished domain with its token usage.
	OnUnit func(out Output, usage llm.Usage)
}

// Run reduces every domain, in a worker pool (domains are independent —
// cross-domain edges are computed later, in the build phase). Domains whose
// cached output still matches their candidates are reused. Results are
// returned sorted by domain for determinism.
func (r *Reducer) Run(ctx context.Context, candidates map[string][]mapper.Rule) ([]Output, error) {
	if err := os.MkdirAll(r.OutDir, 0o755); err != nil {
		return nil, fmt.Errorf("create reduce dir: %w", err)
	}
	domains := make([]string, 0, len(candidates))
	for domain := range candidates {
		domains = append(domains, domain)
	}
	sort.Strings(domains)

	workers := max(r.Workers, 1)
	jobs := make(chan string)
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		outputs []Output
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
		if r.OnUnit != nil {
			r.OnUnit(out, usage)
		}
	}

	for range workers {
		wg.Go(func() {
			for domain := range jobs {
				out, usage, err := r.reduceDomain(ctx, domain, candidates[domain])
				record(out, usage, err)
			}
		})
	}
feed:
	for _, domain := range domains {
		select {
		case <-ctx.Done():
			break feed
		case jobs <- domain:
		}
	}
	close(jobs)
	wg.Wait()

	if runErr == nil && ctx.Err() != nil {
		runErr = ctx.Err()
	}
	sort.SliceStable(outputs, func(i, j int) bool { return outputs[i].Domain < outputs[j].Domain })
	return outputs, runErr
}

// DefaultBatchSize caps how many candidate rules go into one reduce call.
// Sized so a batch's consolidated output fits comfortably under a typical
// max-tokens (a domain with more candidates is reduced in batches then
// merged, so a huge domain never truncates its JSON and gets lost).
const DefaultBatchSize = 30

// consolidate sends rules to the LLM, in batches when they exceed the batch
// size. A failure that is not a cancellation is recorded in the Output.
func (r *Reducer) consolidate(
	ctx context.Context,
	domain string,
	rules []mapper.Rule,
) (Output, llm.Usage, error) {
	batchSize := r.BatchSize
	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}
	if len(rules) > batchSize {
		return r.reduceInBatches(ctx, domain, rules, batchSize)
	}
	payload, usage, err := r.reduceBatch(ctx, domain, rules, 2)
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return Output{}, usage, err
	}
	if err != nil {
		r.Log.WarnContext(ctx, "domain failed", "domain", domain, "err", err)
	}
	return outcomeFrom(domain, payload, err), usage, nil
}

// reduceDomain reduces one domain, reusing a cached output when present.
// Domains larger than the batch size are reduced in batches and merged
// deterministically.
func (r *Reducer) reduceDomain(
	ctx context.Context,
	domain string,
	rules []mapper.Rule,
) (Output, llm.Usage, error) {
	outPath := filepath.Join(r.OutDir, domain+".json")
	hash := candidatesHash(r.promptKey(), rules)
	if data, err := os.ReadFile(outPath); err == nil {
		var cached Output
		switch unmarshalErr := json.Unmarshal(data, &cached); {
		case unmarshalErr != nil:
			r.Log.WarnContext(ctx, "corrupt reduce cache, re-reducing", "path", outPath)
		case cached.CandidatesHash == hash:
			return cached, llm.Usage{}, nil // candidates and prompt unchanged
		default:
			r.Log.InfoContext(
				ctx,
				"reduce cache stale (candidates, model or prompt changed), re-reducing",
				"domain",
				domain,
			)
		}
	}

	toLLM, asIsRules := rules, []mapper.Rule(nil)
	if r.Mode == ModeMerge {
		toLLM, asIsRules = splitClusters(rules)
	}
	out := Output{Domain: domain}
	var usage llm.Usage
	if len(toLLM) > 0 {
		var err error
		out, usage, err = r.consolidate(ctx, domain, toLLM)
		if err != nil {
			return Output{}, usage, err
		}
	}
	if !out.Failed && len(asIsRules) > 0 {
		for i := range asIsRules {
			out.Rules = append(out.Rules, asIs(&asIsRules[i]))
		}
		out.Rules = mergeBatchRules(out.Rules)
	}
	out.CandidatesHash = hash

	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return Output{}, usage, fmt.Errorf("encode reduce output: %w", err)
	}
	if err := state.WriteFileAtomic(outPath, append(data, '\n')); err != nil {
		return Output{}, usage, fmt.Errorf("write reduce output: %w", err)
	}
	return out, usage, nil
}

// reduceBatch runs one reduce LLM call over a candidate set, allowing up to
// maxRepairs correction rounds. In the adaptive path maxRepairs is 0: a
// truncated batch cannot be fixed by resending at the same size — halving is
// the recovery — so retries there would only waste calls.
func (r *Reducer) reduceBatch(
	ctx context.Context,
	domain string,
	rules []mapper.Rule,
	maxRepairs int,
) (reducePayload, llm.Usage, error) {
	candidateJSON, err := json.Marshal(rules)
	if err != nil {
		return reducePayload{}, llm.Usage{}, fmt.Errorf("encode candidates: %w", err)
	}
	userPrompt := fmt.Sprintf(userPromptFormat,
		domain, mustJSON(r.Entities), mustJSON(r.Endpoints), string(candidateJSON))
	msgs := []llm.Message{
		{
			Role:    "system",
			Content: locale.For(r.Lang).Prompt(systemPrompt),
		},
		{Role: "user", Content: userPrompt},
	}
	allowedCitations := citationSet(rules)
	var usage llm.Usage
	payload, err := llm.ChatJSON(ctx, r.LLM, msgs, maxRepairs, func(u llm.Usage) { usage.Add(u) },
		func(reply string) (reducePayload, error) {
			return validateReduceReply(reply, r.Entities, r.Endpoints, allowedCitations)
		})
	return payload, usage, err
}

// reduceFloor is the smallest batch we still try to reduce; a batch this
// small that still fails to produce valid JSON is genuinely unprocessable
// and gets dropped rather than split further.
const reduceFloor = 5

// reduceInBatches reduces a large domain in fixed-size batches, then merges
// the batch outputs deterministically (dedup by identical requirement,
// slugs renumbered on collision). A batch that truncates its JSON is split
// in half and retried down to reduceFloor, so growth in output size can
// never lose a whole batch (let alone a domain).
func (r *Reducer) reduceInBatches(
	ctx context.Context,
	domain string,
	rules []mapper.Rule,
	batchSize int,
) (Output, llm.Usage, error) {
	var usage llm.Usage
	var merged []Rule
	summary := ""
	dropped := 0
	for i := 0; i < len(rules); i += batchSize {
		end := min(i+batchSize, len(rules))
		batchRules, batchSummary, err := r.reduceBatchAdaptive(
			ctx,
			domain,
			rules[i:end],
			&usage,
			&dropped,
		)
		if err != nil {
			return Output{}, usage, err // cancellation only
		}
		if summary == "" {
			summary = batchSummary
		}
		merged = append(merged, batchRules...)
	}
	r.Log.InfoContext(ctx, "reduced large domain in batches", "domain", domain,
		"candidates", len(rules), "rules", len(merged), "dropped_candidates", dropped)

	out := Output{Domain: domain}
	if len(merged) == 0 {
		out.Failed = true
		out.Error = "every reduce batch failed"
		return out, usage, nil
	}
	out.DomainSummary = summary
	out.Rules = mergeBatchRules(merged)
	return out, usage, nil
}

// reduceBatchAdaptive reduces one batch, halving and retrying on failure
// until batches succeed or shrink to reduceFloor. Returns cancellation
// errors; persistent small-batch failures are counted in *dropped, not
// returned.
func (r *Reducer) reduceBatchAdaptive(
	ctx context.Context,
	domain string,
	rules []mapper.Rule,
	usage *llm.Usage,
	dropped *int,
) ([]Rule, string, error) {
	// Fail fast while we can still halve; allow real correction rounds only
	// at the floor, where halving is no longer an option.
	maxRepairs := 0
	if len(rules) <= reduceFloor {
		maxRepairs = 2
	}
	payload, batchUsage, err := r.reduceBatch(ctx, domain, rules, maxRepairs)
	usage.Add(batchUsage)
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return nil, "", err
	}
	if err == nil {
		return payload.Rules, payload.DomainSummary, nil
	}
	if len(rules) > reduceFloor {
		mid := len(rules) / 2
		left, leftSummary, err := r.reduceBatchAdaptive(ctx, domain, rules[:mid], usage, dropped)
		if err != nil {
			return nil, "", err
		}
		right, rightSummary, err := r.reduceBatchAdaptive(ctx, domain, rules[mid:], usage, dropped)
		if err != nil {
			return nil, "", err
		}
		summary := leftSummary
		if summary == "" {
			summary = rightSummary
		}
		return append(left, right...), summary, nil
	}
	*dropped += len(rules)
	r.Log.WarnContext(
		ctx,
		"dropping unprocessable reduce batch",
		"domain",
		domain,
		"candidates",
		len(rules),
		"err",
		err,
	)
	return nil, "", nil
}

// outcomeFrom turns a reduce payload/error into a domain Output.
func outcomeFrom(domain string, payload reducePayload, err error) Output {
	out := Output{Domain: domain}
	if err != nil {
		out.Failed = true
		out.Error = err.Error()
		return out
	}
	out.DomainSummary = payload.DomainSummary
	out.Rules = payload.Rules
	return out
}

// mergeBatchRules deduplicates rules merged from several batches: identical
// requirements collapse (unioning their citations), and colliding slugs are
// renumbered so they stay unique within the domain.
func mergeBatchRules(rules []Rule) []Rule {
	byRequirement := make(map[string]int) // requirement -> index in out
	slugs := make(map[string]bool)
	var out []Rule
	for _, rule := range rules {
		if idx, ok := byRequirement[rule.Requirement]; ok {
			out[idx].Citations = unionRefs(out[idx].Citations, rule.Citations)
			continue
		}
		rule.Slug = uniqueSlug(normalizeSlug(rule.Slug), slugs)
		slugs[rule.Slug] = true
		byRequirement[rule.Requirement] = len(out)
		out = append(out, rule)
	}
	return out
}

// unionRefs merges two citation slices, dropping duplicates, stably sorted.
func unionRefs(a, b []extract.Ref) []extract.Ref {
	seen := make(map[string]bool, len(a)+len(b))
	var out []extract.Ref
	for _, ref := range append(append([]extract.Ref(nil), a...), b...) {
		key := ref.Path + "|" + ref.Lines
		if !seen[key] {
			seen[key] = true
			out = append(out, ref)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].Lines < out[j].Lines
	})
	return out
}

// validateReduceReply enforces the deterministic contract on a reduce reply:
// map-level checks plus slug uniqueness and verbatim citations. Slugs are
// repaired mechanically (slugify, truncate to 5 words, numeric suffix on
// duplicates) instead of burning LLM correction rounds on formatting.
func validateReduceReply(
	reply string,
	allowedEntities, allowedEndpoints []string,
	allowedCitations map[string]bool,
) (reducePayload, error) {
	var payload reducePayload
	if err := json.Unmarshal([]byte(llm.StripFences(reply)), &payload); err != nil {
		return payload, fmt.Errorf("invalid JSON: %w", err)
	}
	slugs := make(map[string]bool)
	for i := range payload.Rules {
		rule := &payload.Rules[i]
		slug := normalizeSlug(rule.Slug)
		if slug == "" {
			slug = normalizeSlug(rule.Title)
		}
		if slug == "" {
			return payload, fmt.Errorf("rules[%d] has neither a usable slug nor a usable title", i)
		}
		slug = uniqueSlug(slug, slugs)
		slugs[slug] = true
		rule.Slug = slug
	}
	for i, rule := range payload.Rules {
		if !mapper.EARSKinds[rule.EarsKind] {
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
			if _, _, err := extract.ParseLines(c.Lines); err != nil {
				return payload, fmt.Errorf("rules[%d]: %w", i, err)
			}
			if !allowedCitations[citationKey(c)] {
				return payload, fmt.Errorf(
					"rules[%d] cites %s:%s which is not among the candidate citations",
					i,
					c.Path,
					c.Lines,
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
		if !mapper.Natures[rule.Nature] {
			return payload, fmt.Errorf(
				"rules[%d].nature %q is not one of business|presentation|technical",
				i,
				rule.Nature,
			)
		}
		if !mapper.Origins[rule.Origin] {
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

// normalizeSlug lowercases, slugifies and truncates a slug to 5 words.
func normalizeSlug(s string) string {
	slug := extract.Slugify(s)
	words := strings.Split(slug, "-")
	if len(words) > 5 {
		words = words[:5]
	}
	return strings.Join(words, "-")
}

// uniqueSlug appends a numeric suffix until slug is unused within the domain.
func uniqueSlug(slug string, taken map[string]bool) string {
	if !taken[slug] {
		return slug
	}
	for n := 2; ; n++ {
		candidate := slug + "-" + strconv.Itoa(n)
		if !taken[candidate] {
			return candidate
		}
	}
}

// promptKey covers everything besides the candidates that shapes a reduce
// output: model, language, prompt templates and a non-default mode.
func (r *Reducer) promptKey() string {
	parts := []string{locale.For(r.Lang).Prompt(systemPrompt), userPromptFormat}
	if r.Mode == ModeMerge {
		// Only a non-default mode joins the key, so ModeFull caches written
		// before modes existed stay valid.
		parts = append(parts, r.Mode)
	}
	return llm.PromptKey(r.Model, parts...)
}

// candidatesHash is a stable content hash of a domain's candidate rules and
// the prompt key (model + prompt templates), so the reduce cache is
// invalidated when the candidates change (e.g. after the source was edited
// and re-mapped), the model changes or the prompt is edited.
func candidatesHash(promptKey string, rules []mapper.Rule) string {
	sorted := append([]mapper.Rule(nil), rules...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Requirement != sorted[j].Requirement {
			return sorted[i].Requirement < sorted[j].Requirement
		}
		return sorted[i].Title < sorted[j].Title
	})
	data, err := json.Marshal(sorted)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(append([]byte(promptKey+"\n"), data...))
	return hex.EncodeToString(sum[:])[:16]
}

func citationSet(rules []mapper.Rule) map[string]bool {
	set := make(map[string]bool)
	for _, rule := range rules {
		for _, c := range rule.Citations {
			set[citationKey(c)] = true
		}
	}
	return set
}

func citationKey(c extract.Ref) string {
	return c.Path + "|" + c.Lines
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
	if err != nil || string(data) == "null" {
		return "[]"
	}
	return string(data)
}
