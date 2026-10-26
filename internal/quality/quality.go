// Package quality classifies technical rules by the ISO 25010 quality
// characteristic they serve (security, reliability, performance…), the
// field an ISO 29148 non-functional requirement carries. An optional LLM
// pass over the rules' own text: no code is read, and the map and reduce
// prompts are untouched, so no cached phase is invalidated. Business and
// presentation rules are functional by definition and are not asked about.
package quality

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/StevenACoffman/codetospec/internal/atomicfs"
	"github.com/StevenACoffman/codetospec/internal/graph"
	"github.com/StevenACoffman/codetospec/internal/llm"
	"github.com/StevenACoffman/codetospec/internal/workpool"
)

// The attributes, as spec-artifacts-iso 0.3.0 (Quire) enumerates them for
// an NFR's quality_attribute: the ISO 25010 characteristics, plus the
// availability, scalability and compliance it lists apart.
const (
	FunctionalSuitability = "functional_suitability"
	PerformanceEfficiency = "performance_efficiency"
	Compatibility         = "compatibility"
	Usability             = "usability"
	Reliability           = "reliability"
	Availability          = "availability"
	Security              = "security"
	Maintainability       = "maintainability"
	Portability           = "portability"
	Scalability           = "scalability"
	Compliance            = "compliance"
	Safety                = "safety"
)

// IsAttribute reports whether s is one of the attributes.
func IsAttribute(s string) bool {
	switch s {
	case FunctionalSuitability, PerformanceEfficiency, Compatibility, Usability,
		Reliability, Availability, Security, Maintainability, Portability,
		Scalability, Compliance, Safety:
		return true
	}
	return false
}

// batchSize is how many rules one call classifies: the answer is one word
// per rule, so the fixed cost of a call dominates a small batch.
const batchSize = 40

// corrections is how many times a malformed reply is sent back before its
// batch is left unclassified.
const corrections = 2

const systemPrompt = `You classify the technical rules of a software system by the ISO 25010 quality characteristic each one serves.

Hard rules:
- Output ONLY a valid JSON object matching the schema. No markdown fences, no prose.
- Answer for every label given, exactly once.
- quality_attribute is one of:
  functional_suitability (computes or stores the right result; plain functionality),
  performance_efficiency (time, throughput, resource use: timeouts, limits, caching, batching),
  compatibility (interoperates with other systems or formats),
  usability (helps a user operate the system: messages, defaults, accessibility),
  reliability (keeps working through faults: retries, fallbacks, idempotence, consistency),
  availability (stays reachable: health checks, failover),
  security (confidentiality, integrity, authorization, authentication, auditing),
  maintainability (eases change: configuration, logging for operators, versioning),
  portability (runs in other environments),
  scalability (grows with load: sharding, pagination, quotas),
  compliance (meets a law, regulation or policy: retention, privacy, consent),
  safety (prevents harm to people or property).
- Pick the one the rule mainly serves. Use "" when the rule is plain functionality with no quality concern.`

const userPromptFormat = `RULES:
%s

OUTPUT JSON SCHEMA:
{"rules": [{"label": "R1", "quality_attribute": string}]}`

// Classifier drives the classification pass.
type Classifier struct {
	LLM llm.Chatter
	// Log receives degraded-mode warnings (required).
	Log       *slog.Logger
	Model     string // model id, folded into the cache key
	Workers   int
	CachePath string // <out>/.codetospec/quality.json
	// OnBatch is called after each finished call, in the calling goroutine.
	OnBatch func(usage llm.Usage)
}

// cacheFile is the persisted classification of every rule the last run
// classified, by input hash: a rule renamed or moved keeps its answer.
type cacheFile struct {
	Attributes map[string]string `json:"attributes"` // input hash → attribute or ""
}

// Run classifies every technical rule of nodes and returns its attribute
// by rule id ("" when none fits). A rule whose text is unchanged since a
// previous run is not asked again. A batch the model cannot answer is
// logged and its rules left out (asked again next run); only a cancelled
// context fails the run.
func (c *Classifier) Run(ctx context.Context, nodes []graph.Node) (map[string]string, error) {
	key := llm.PromptKey(c.Model, systemPrompt, userPromptFormat)
	rules := technicalRules(nodes)
	cached := c.load(ctx)
	kept := cacheFile{Attributes: map[string]string{}}
	out := map[string]string{}
	var pending []graph.Node
	for i := range rules {
		hash := inputHash(key, &rules[i])
		if attribute, hit := cached.Attributes[hash]; hit {
			out[rules[i].ID] = attribute
			kept.Attributes[hash] = attribute
		} else {
			pending = append(pending, rules[i])
		}
	}

	batches := slices.Collect(slices.Chunk(pending, batchSize))
	var runErr error
	workpool.Run(ctx, c.Workers, batches,
		func(ctx context.Context, batch *[]graph.Node) batchOutcome {
			attributes, usage, err := c.classify(ctx, *batch)
			return batchOutcome{rules: *batch, attributes: attributes, usage: usage, err: err}
		},
		func(r batchOutcome) {
			if c.OnBatch != nil {
				c.OnBatch(r.usage)
			}
			switch {
			case errors.Is(r.err, context.Canceled) || errors.Is(r.err, context.DeadlineExceeded):
				runErr = cmp.Or(runErr, r.err)
				return
			case r.err != nil:
				c.Log.WarnContext(ctx, "quality batch failed", "rules", len(r.rules), "err", r.err)
				return
			}
			for i := range r.rules {
				out[r.rules[i].ID] = r.attributes[i]
				kept.Attributes[inputHash(key, &r.rules[i])] = r.attributes[i]
			}
			runErr = cmp.Or(runErr, c.save(&kept))
		})
	if runErr == nil {
		runErr = cmp.Or(ctx.Err(), c.save(&kept))
	}
	return out, runErr
}

// batchOutcome is one call's result: the attribute of each rule of the
// batch, in order.
type batchOutcome struct {
	rules      []graph.Node
	attributes []string
	usage      llm.Usage
	err        error
}

// classify asks for the attributes of one batch, labelled R1…Rn.
func (c *Classifier) classify(
	ctx context.Context,
	batch []graph.Node,
) ([]string, llm.Usage, error) {
	var listing strings.Builder
	for i := range batch {
		fmt.Fprintf(
			&listing,
			"R%d. %s: %s\n",
			i+1,
			batch[i].Title,
			graph.RuleRequirement(batch[i].Body),
		)
	}
	msgs := []llm.Message{
		{Role: "system", Content: systemPrompt},
		{
			Role:    "user",
			Content: fmt.Sprintf(userPromptFormat, strings.TrimRight(listing.String(), "\n")),
		},
	}
	var usage llm.Usage
	attributes, err := llm.ChatJSON(ctx, c.LLM, msgs, corrections,
		func(u llm.Usage) { usage.Add(u) },
		func(reply string) ([]string, error) { return parseReply(reply, len(batch)) })
	return attributes, usage, err
}

// payload mirrors the reply schema.
type payload struct {
	Rules []struct {
		Label     string `json:"label"`
		Attribute string `json:"quality_attribute"`
	} `json:"rules"`
}

// parseReply reads a reply for n rules labelled R1…Rn.
//
// Requires: n > 0.
// Ensures:  on success, n attributes in label order, each one of the
// attributes or ""; an error names the first problem, for correction.
func parseReply(reply string, n int) ([]string, error) {
	var p payload
	if err := json.Unmarshal([]byte(llm.StripFences(reply)), &p); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	attributes := make([]string, n)
	answered := make([]bool, n)
	for _, r := range p.Rules {
		i, err := strconv.Atoi(strings.TrimPrefix(r.Label, "R"))
		switch {
		case err != nil || !strings.HasPrefix(r.Label, "R") || i < 1 || i > n:
			return nil, fmt.Errorf("label %q is not one of R1…R%d", r.Label, n)
		case answered[i-1]:
			return nil, fmt.Errorf("label %s answered twice", r.Label)
		case r.Attribute != "" && !IsAttribute(r.Attribute):
			return nil, fmt.Errorf("%s: %q is not a listed quality_attribute", r.Label, r.Attribute)
		}
		answered[i-1], attributes[i-1] = true, r.Attribute
	}
	if missing := slices.Index(answered, false); missing >= 0 {
		return nil, fmt.Errorf("no answer for R%d", missing+1)
	}
	return attributes, nil
}

// technicalRules lists the technical rules of nodes, sorted by id.
func technicalRules(nodes []graph.Node) []graph.Node {
	var rules []graph.Node
	for i := range nodes {
		if nodes[i].Type == graph.TypeRule && nodes[i].Extra["nature"] == "technical" {
			rules = append(rules, nodes[i])
		}
	}
	slices.SortFunc(rules, func(a, b graph.Node) int { return cmp.Compare(a.ID, b.ID) })
	return rules
}

// inputHash identifies what an answer depends on: the prompts and model,
// and the rule's title and requirement.
func inputHash(promptKey string, rule *graph.Node) string {
	sum := sha256.Sum256(fmt.Appendf(nil, "%s\n%s\n%s",
		promptKey, rule.Title, graph.RuleRequirement(rule.Body)))
	return hex.EncodeToString(sum[:])[:16]
}

// load reads the cache; a missing or unreadable one is empty (unreadable
// is logged): the pass asks again.
func (c *Classifier) load(ctx context.Context) cacheFile {
	data, err := os.ReadFile(filepath.Clean(c.CachePath))
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			c.Log.WarnContext(ctx, "quality cache unreadable", "err", err)
		}
		return cacheFile{}
	}
	var cache cacheFile
	if err = json.Unmarshal(data, &cache); err != nil {
		c.Log.WarnContext(ctx, "quality cache unreadable", "err", err)
		return cacheFile{}
	}
	return cache
}

// save writes the cache: the answers of the current rules only, so a rule
// gone since does not linger.
func (c *Classifier) save(cache *cacheFile) error {
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return fmt.Errorf("encode quality cache: %w", err)
	}
	if err = atomicfs.WriteFile(c.CachePath, append(data, '\n')); err != nil {
		return fmt.Errorf("write quality cache: %w", err)
	}
	return nil
}

// Annotate sets each classified rule's quality attribute.
//
// Requires: attributes from Run.
// Ensures:  nodes is not modified; a rule classified "" gets no field.
func Annotate(nodes []graph.Node, attributes map[string]string) []graph.Node {
	out := slices.Clone(nodes)
	for i := range out {
		attribute := attributes[out[i].ID]
		if out[i].Type != graph.TypeRule || attribute == "" {
			continue
		}
		out[i].Extra = maps.Clone(out[i].Extra)
		if out[i].Extra == nil {
			out[i].Extra = map[string]string{}
		}
		out[i].Extra[graph.ExtraQualityAttribute] = attribute
	}
	return out
}
