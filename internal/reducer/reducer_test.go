package reducer_test

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"testing"

	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/llm"
	"github.com/StevenACoffman/codetospec/internal/mapper"
	"github.com/StevenACoffman/codetospec/internal/reducer"
)

type chatFunc func(ctx context.Context, msgs []llm.Message) (string, llm.Usage, error)

func (f chatFunc) Chat(ctx context.Context, msgs []llm.Message) (string, llm.Usage, error) {
	return f(ctx, msgs)
}

// Values the candidate fixtures share.
const (
	billing    = "billing"
	earsEvent  = "event"
	xPath      = "app/X.php"
	invoices   = "entity.invoices"
	prorataRef = "prorata-activation"
)

func candidates() map[string][]mapper.Rule {
	return map[string][]mapper.Rule{
		billing: {{
			Title:       "Prorata",
			EarsKind:    earsEvent,
			Requirement: "QUAND x, le systeme doit y.",
			Citations:   []extract.Ref{{Path: xPath, Lines: "12-15"}},
			Entities:    []string{invoices},
			Confidence:  0.9,
		}},
	}
}

func newReducer(t *testing.T, chat llm.Chatter) *reducer.Reducer {
	t.Helper()
	return &reducer.Reducer{
		Log:       slog.New(slog.NewTextHandler(t.Output(), nil)),
		LLM:       chat,
		Lang:      "fr",
		OutDir:    t.TempDir(),
		Entities:  []string{invoices},
		Endpoints: []string{"endpoint.post-api-activate"},
	}
}

const validReduceReply = `{"domain_summary": "Facturation.", "rules": [{"slug": "prorata-activation",
"title": "Prorata", "ears_kind": "event", "requirement": "QUAND x, le systeme doit y.",
"rationale": "Vu dans le code.",
"citations": [{"path": "app/X.php", "lines": "12-15"}],
"entities": ["entity.invoices"], "endpoints": [],
"acceptance_criteria": ["a", "b"], "nature": "business", "origin": "explicit", "confidence": 0.9}]}`

func TestReduceNominal(t *testing.T) {
	calls := 0
	r := newReducer(
		t,
		chatFunc(func(_ context.Context, msgs []llm.Message) (string, llm.Usage, error) {
			calls++
			if !strings.Contains(msgs[1].Content, "DOMAIN: billing") {
				t.Errorf("user prompt missing domain:\n%s", msgs[1].Content)
			}
			if !strings.Contains(msgs[1].Content, "CANDIDATE_RULES:") {
				t.Error("user prompt missing candidates")
			}
			// Acceptance criteria must be constrained to the cited behavior.
			if !strings.Contains(msgs[0].Content, "Do NOT invent error cases") {
				t.Error("system prompt missing the acceptance-criteria provability constraint")
			}
			return validReduceReply, llm.Usage{PromptTokens: 20, CompletionTokens: 10}, nil
		}),
	)

	outputs, err := r.Run(context.Background(), candidates())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if calls != 1 || len(outputs) != 1 {
		t.Fatalf("calls=%d outputs=%d, want 1/1", calls, len(outputs))
	}
	if outputs[0].Failed || len(outputs[0].Rules) != 1 ||
		outputs[0].Rules[0].Slug != "prorata-activation" {
		t.Fatalf("output = %+v", outputs[0])
	}
}

func TestReduceRejectsInventedCitationThenCorrects(t *testing.T) {
	calls := 0
	r := newReducer(
		t,
		chatFunc(func(_ context.Context, msgs []llm.Message) (string, llm.Usage, error) {
			calls++
			if calls == 1 {
				// Citation not present verbatim among the candidates.
				return strings.ReplaceAll(validReduceReply, "12-15", "12-16"), llm.Usage{}, nil
			}
			last := msgs[len(msgs)-1]
			if !strings.Contains(last.Content, "output rejected:") {
				t.Errorf("correction message malformed: %q", last.Content)
			}
			return validReduceReply, llm.Usage{}, nil
		}),
	)

	outputs, err := r.Run(context.Background(), candidates())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if calls != 2 || outputs[0].Failed {
		t.Fatalf("calls=%d output=%+v", calls, outputs[0])
	}
}

func TestReduceDoubleFailureMarksDomainFailed(t *testing.T) {
	calls := 0
	r := newReducer(t, chatFunc(func(context.Context, []llm.Message) (string, llm.Usage, error) {
		calls++
		return `{"domain_summary": "x", "rules": [{"slug": "INVALID SLUG"}]}`, llm.Usage{}, nil
	}))

	outputs, err := r.Run(context.Background(), candidates())
	if err != nil {
		t.Fatalf("Run should continue on domain failure, got %v", err)
	}
	if calls != 3 || !outputs[0].Failed {
		t.Fatalf("calls=%d output=%+v, want 3 calls and failed domain", calls, outputs[0])
	}
}

// TestReduceValidationRules: slugs are repaired mechanically (slugified,
// truncated to five words, renumbered on collision) instead of sent back.
func TestReduceValidationRules(t *testing.T) {
	duplicated := strings.Replace(
		validReduceReply,
		`"rules": [{`,
		`"rules": [{"slug": "prorata-activation",
"title": "Dup", "ears_kind": "event", "requirement": "QUAND a, le systeme doit b.",
"rationale": "", "citations": [{"path": "app/X.php", "lines": "12-15"}],
"entities": [], "endpoints": [], "acceptance_criteria": ["a", "b"], "nature": "business", "origin": "explicit", "confidence": 0.8}, {`,
		1,
	)
	cases := map[string]struct {
		reply     string
		wantSlugs []string
	}{
		"valid reply": {validReduceReply, []string{prorataRef}},
		"duplicate slug renumbered": {
			duplicated, []string{prorataRef, prorataRef + "-2"},
		},
		"malformed slug repaired": {
			strings.ReplaceAll(
				validReduceReply,
				"prorata-activation",
				"Un Slug beaucoup TROP long vraiment",
			),
			[]string{"un-slug-beaucoup-trop-long"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			r := newReducer(
				t,
				chatFunc(func(context.Context, []llm.Message) (string, llm.Usage, error) {
					return tc.reply, llm.Usage{}, nil
				}),
			)
			outputs, err := r.Run(context.Background(), candidates())
			if err != nil || len(outputs) != 1 || outputs[0].Failed {
				t.Fatalf("Run: outputs=%+v err=%v", outputs, err)
			}
			var slugs []string
			for _, rule := range outputs[0].Rules {
				slugs = append(slugs, rule.Slug)
			}
			if !reflect.DeepEqual(slugs, tc.wantSlugs) {
				t.Errorf("slugs = %v, want %v", slugs, tc.wantSlugs)
			}
		})
	}
}

func TestReduceBatchesLargeDomain(t *testing.T) {
	// 130 candidates with BatchSize 50 -> 3 batches, each answered per-batch.
	big := make([]mapper.Rule, 130)
	for i := range big {
		big[i] = mapper.Rule{
			Title:       fmt.Sprintf("Règle %d", i),
			EarsKind:    earsEvent,
			Requirement: fmt.Sprintf("QUAND cas %d, le systeme doit agir.", i),
			Citations:   []extract.Ref{{Path: xPath, Lines: fmt.Sprintf("%d-%d", i+1, i+2)}},
		}
	}

	calls := 0
	var batchSizes []int
	r := newReducer(
		t,
		chatFunc(func(_ context.Context, msgs []llm.Message) (string, llm.Usage, error) {
			calls++
			// Count how many candidates this batch carried.
			_, candJSON, _ := strings.Cut(msgs[1].Content, "CANDIDATE_RULES:\n")
			var cands []mapper.Rule
			if err := json.Unmarshal([]byte(candJSON), &cands); err != nil {
				t.Fatalf("batch %d: bad candidates JSON: %v", calls, err)
			}
			batchSizes = append(batchSizes, len(cands))
			// Echo each candidate back as one consolidated rule.
			rules := make([]map[string]any, len(cands))
			for i, c := range cands {
				rules[i] = map[string]any{
					"slug": "regle", "title": c.Title, "ears_kind": "event",
					"requirement": c.Requirement, "rationale": "r",
					"citations": c.Citations, "entities": []string{}, "endpoints": []string{},
					"acceptance_criteria": []string{"a", "b"},
					"nature":              "business", "origin": "explicit", "confidence": 0.8,
				}
			}
			out, _ := json.Marshal(map[string]any{"domain_summary": "S", "rules": rules})
			return string(out), llm.Usage{}, nil
		}),
	)
	r.BatchSize = 50

	outputs, err := r.Run(context.Background(), map[string][]mapper.Rule{billing: big})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want 3 batches", calls)
	}
	if batchSizes[0] != 50 || batchSizes[1] != 50 || batchSizes[2] != 30 {
		t.Errorf("batch sizes = %v, want [50 50 30]", batchSizes)
	}
	out := outputs[0]
	if out.Failed {
		t.Fatalf("batched domain must not fail: %+v", out)
	}
	// Every merged rule must have a unique slug (renumbered on collision).
	slugs := map[string]bool{}
	for _, rule := range out.Rules {
		if slugs[rule.Slug] {
			t.Errorf("duplicate slug after merge: %q", rule.Slug)
		}
		slugs[rule.Slug] = true
	}
	if len(out.Rules) != 130 {
		t.Errorf("merged rules = %d, want 130 distinct requirements", len(out.Rules))
	}
}

func TestReduceBatchAdaptiveHalvingRecoversMostRules(t *testing.T) {
	// 80 candidates, batchSize 40. Any batch containing candidate 0 (its
	// requirement carries "QUAND cas 0,") always returns garbage, forcing the
	// adaptive halving to isolate it down to reducer.MinBatchSize. Everything else is
	// recovered; only the floor-sized fragment holding candidate 0 is dropped.
	big := make([]mapper.Rule, 80)
	for i := range big {
		big[i] = mapper.Rule{
			Title: fmt.Sprintf("R%d", i), EarsKind: earsEvent,
			Requirement: fmt.Sprintf("QUAND cas %d, le systeme doit agir.", i),
			Citations:   []extract.Ref{{Path: xPath, Lines: fmt.Sprintf("%d-%d", i+1, i+2)}},
		}
	}
	r := newReducer(
		t,
		chatFunc(func(_ context.Context, msgs []llm.Message) (string, llm.Usage, error) {
			_, candJSON, _ := strings.Cut(msgs[1].Content, "CANDIDATE_RULES:\n")
			if strings.Contains(candJSON, "QUAND cas 0,") {
				return "not json", llm.Usage{}, nil
			}
			var cands []mapper.Rule
			if err := json.Unmarshal([]byte(candJSON), &cands); err != nil {
				t.Fatalf("bad candidates JSON: %v", err)
			}
			rules := make([]map[string]any, len(cands))
			for i, c := range cands {
				rules[i] = map[string]any{
					"slug":        "r",
					"title":       c.Title,
					"ears_kind":   "event",
					"requirement": c.Requirement,
					"rationale":   "r",
					"citations":   c.Citations,
					"entities":    []string{},
					"endpoints":   []string{},
					"acceptance_criteria": []string{
						"a",
						"b",
					},
					"nature":     "business",
					"origin":     "explicit",
					"confidence": 0.8,
				}
			}
			out, _ := json.Marshal(map[string]any{"domain_summary": "S", "rules": rules})
			return string(out), llm.Usage{}, nil
		}),
	)
	r.BatchSize = 40

	outputs, err := r.Run(context.Background(), map[string][]mapper.Rule{billing: big})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if outputs[0].Failed {
		t.Fatalf("domain must survive: %+v", outputs[0])
	}
	// Only the ≤reducer.MinBatchSize fragment containing candidate 0 is lost; the vast
	// majority of rules is recovered by halving.
	if got := len(outputs[0].Rules); got < 80-reducer.MinBatchSize || got >= 80 {
		t.Errorf("recovered %d rules, want between %d and 79", got, 80-reducer.MinBatchSize)
	}
}

func TestReduceCacheInvalidatedByChangedCandidates(t *testing.T) {
	calls := 0
	r := newReducer(t, chatFunc(func(context.Context, []llm.Message) (string, llm.Usage, error) {
		calls++
		return validReduceReply, llm.Usage{}, nil
	}))
	if _, err := r.Run(context.Background(), candidates()); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	if calls != 1 {
		t.Fatalf("first run calls = %d, want 1", calls)
	}
	// Same candidates → cache hit, no new call.
	if _, err := r.Run(context.Background(), candidates()); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("unchanged candidates should hit cache, calls = %d", calls)
	}
	// Changed candidate → cache stale → re-reduced.
	changed := candidates()
	changed[billing][0].Requirement = "QUAND z, le systeme doit w."
	if _, err := r.Run(context.Background(), changed); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("changed candidates should re-reduce, calls = %d, want 2", calls)
	}
}

func TestReduceParallelDeterministic(t *testing.T) {
	// Several domains reduced by a worker pool must return sorted, stable.
	multi := map[string][]mapper.Rule{}
	for _, d := range []string{"zeta", "alpha", "mu", "beta", "kappa"} {
		multi[d] = []mapper.Rule{
			{
				Title:       "T",
				EarsKind:    earsEvent,
				Requirement: "QUAND x, le systeme doit y.",
				Citations: []extract.Ref{
					{Path: xPath, Lines: "12-15"},
				},
				Entities: []string{invoices},
			},
		}
	}
	run := func() []string {
		r := newReducer(
			t,
			chatFunc(func(context.Context, []llm.Message) (string, llm.Usage, error) {
				return validReduceReply, llm.Usage{}, nil
			}),
		)
		r.Workers = 4
		outputs, err := r.Run(context.Background(), multi)
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		got := make([]string, len(outputs))
		for i, o := range outputs {
			got[i] = o.Domain
		}
		return got
	}
	first := run()
	want := []string{"alpha", "beta", "kappa", "mu", "zeta"}
	for i, d := range want {
		if first[i] != d {
			t.Fatalf("parallel output not sorted: %v, want %v", first, want)
		}
	}
	if second := run(); strings.Join(first, ",") != strings.Join(second, ",") {
		t.Fatalf("parallel reduce not deterministic: %v vs %v", first, second)
	}
}

func TestReduceResumeSkipsCachedDomains(t *testing.T) {
	r := newReducer(t, chatFunc(func(context.Context, []llm.Message) (string, llm.Usage, error) {
		return validReduceReply, llm.Usage{}, nil
	}))
	if _, err := r.Run(context.Background(), candidates()); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	r.LLM = chatFunc(func(context.Context, []llm.Message) (string, llm.Usage, error) {
		t.Fatal("cached domain must not trigger an LLM call")
		return "", llm.Usage{}, nil
	})
	outputs, err := r.Run(context.Background(), candidates())
	if err != nil || len(outputs) != 1 || outputs[0].Failed {
		t.Fatalf("cached run: outputs=%+v err=%v", outputs, err)
	}
}

func TestReduceCacheKeyedByModelAndPrompt(t *testing.T) {
	cases := map[string]struct {
		change    func(r *reducer.Reducer)
		wantCalls int
	}{
		"unchanged":   {change: func(*reducer.Reducer) {}, wantCalls: 0},
		"other model": {change: func(r *reducer.Reducer) { r.Model = "model-b" }, wantCalls: 1},
		"other lang":  {change: func(r *reducer.Reducer) { r.Lang = "en" }, wantCalls: 1},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			r := newReducer(
				t,
				chatFunc(func(context.Context, []llm.Message) (string, llm.Usage, error) {
					return validReduceReply, llm.Usage{}, nil
				}),
			)
			r.Model = "model-a"
			if _, err := r.Run(context.Background(), candidates()); err != nil {
				t.Fatalf("first Run: %v", err)
			}

			calls := 0
			r.LLM = chatFunc(func(context.Context, []llm.Message) (string, llm.Usage, error) {
				calls++
				return validReduceReply, llm.Usage{}, nil
			})
			tc.change(r)
			if _, err := r.Run(context.Background(), candidates()); err != nil {
				t.Fatalf("second Run: %v", err)
			}
			if calls != tc.wantCalls {
				t.Fatalf("second Run made %d LLM calls, want %d", calls, tc.wantCalls)
			}
		})
	}
}

// Fixture values reused across the merge-mode tests.
const (
	sharedLines    = "1-10"
	natureBusiness = "business"
	originExplicit = "explicit"
	fileA          = "a.go"
	repliedLines   = "12-15" // the lines validReduceReply cites
)

// mergeCandidates: two near-duplicates (same requirement, overlapping
// citations) and one distinct rule.
func mergeCandidates() map[string][]mapper.Rule {
	rule := func(title, requirement, lines string) mapper.Rule {
		return mapper.Rule{
			Title: title, EarsKind: earsEvent, Requirement: requirement,
			Citations: []extract.Ref{{Path: xPath, Lines: lines}},
			Nature:    natureBusiness, Origin: originExplicit, Confidence: 0.8,
		}
	}
	return map[string][]mapper.Rule{billing: {
		rule("Prorata", "QUAND x, le systeme doit y.", repliedLines),
		rule("Prorata bis", "QUAND x, le systeme doit y.", "13-14"),
		rule(
			"Refund window",
			"QUAND un remboursement est demandé, le systeme doit vérifier la fenêtre.",
			"40-44",
		),
	}}
}

// TestSplitClusters: in merge mode only candidates in a cluster (similar
// requirement or overlapping citation) reach the LLM.
func TestSplitClusters(t *testing.T) {
	cases := map[string]struct {
		rules         []mapper.Rule
		wantClustered int
	}{
		"duplicates cluster, distinct stays single": {mergeCandidates()[billing], 2},
		"citation overlap alone clusters": {
			[]mapper.Rule{
				{
					Requirement: "alpha beta gamma",
					Citations:   []extract.Ref{{Path: fileA, Lines: sharedLines}},
				},
				{
					Requirement: "delta epsilon zeta",
					Citations:   []extract.Ref{{Path: fileA, Lines: "10-12"}},
				},
			},
			2,
		},
		"same lines in another file do not cluster": {
			[]mapper.Rule{
				{
					Requirement: "alpha beta gamma",
					Citations:   []extract.Ref{{Path: fileA, Lines: sharedLines}},
				},
				{
					Requirement: "delta epsilon zeta",
					Citations:   []extract.Ref{{Path: "b.go", Lines: sharedLines}},
				},
			},
			0,
		},
		"empty": {nil, 0},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			sent, calls := 0, 0
			r := newReducer(
				t,
				chatFunc(func(_ context.Context, msgs []llm.Message) (string, llm.Usage, error) {
					// Only the first call shows the selection: the canned reply
					// cites none of these candidates, so corrections follow.
					calls++
					if calls > 1 {
						return validReduceReply, llm.Usage{}, nil
					}
					_, list, _ := strings.Cut(msgs[1].Content, "CANDIDATE_RULES:\n")
					var rules []mapper.Rule
					if err := json.Unmarshal([]byte(list), &rules); err != nil {
						t.Errorf("candidates sent: %v", err)
					}
					sent += len(rules)
					return validReduceReply, llm.Usage{}, nil
				}),
			)
			r.Mode = reducer.ModeMerge
			if _, err := r.Run(
				context.Background(),
				map[string][]mapper.Rule{"d": tc.rules},
			); err != nil {
				t.Fatal(err)
			}
			if sent != tc.wantClustered {
				t.Fatalf("%d candidates sent to the LLM, want %d", sent, tc.wantClustered)
			}
		})
	}
}

func TestReduceMergeModeSendsOnlyClusters(t *testing.T) {
	var prompts []string
	r := newReducer(
		t,
		chatFunc(func(_ context.Context, msgs []llm.Message) (string, llm.Usage, error) {
			prompts = append(prompts, msgs[1].Content)
			return validReduceReply, llm.Usage{}, nil
		}),
	)
	r.Mode = reducer.ModeMerge
	outputs, err := r.Run(context.Background(), mergeCandidates())
	if err != nil || len(outputs) != 1 || outputs[0].Failed {
		t.Fatalf("Run: outputs=%+v err=%v", outputs, err)
	}
	if len(prompts) != 1 || strings.Contains(prompts[0], "Refund window") {
		t.Fatalf(
			"LLM saw %d prompts; the distinct candidate must not be sent:\n%s",
			len(prompts),
			strings.Join(prompts, "\n---\n"),
		)
	}
	var kept *reducer.Rule
	for i := range outputs[0].Rules {
		if outputs[0].Rules[i].Title == "Refund window" {
			kept = &outputs[0].Rules[i]
		}
	}
	if kept == nil {
		t.Fatalf("distinct candidate missing from %+v", outputs[0].Rules)
	}
	if kept.Slug != "refund-window" || len(kept.AcceptanceCriteria) != 0 ||
		kept.Nature != natureBusiness {
		t.Errorf(
			"kept rule = %+v, want slug refund-window, no criteria, nature carried over",
			*kept,
		)
	}
}

func TestReduceCacheKeyedByMode(t *testing.T) {
	r := newReducer(t, chatFunc(func(context.Context, []llm.Message) (string, llm.Usage, error) {
		return validReduceReply, llm.Usage{}, nil
	}))
	if _, err := r.Run(context.Background(), mergeCandidates()); err != nil {
		t.Fatal(err)
	}
	calls := 0
	r.LLM = chatFunc(func(context.Context, []llm.Message) (string, llm.Usage, error) {
		calls++
		return validReduceReply, llm.Usage{}, nil
	})
	r.Mode = reducer.ModeMerge
	if _, err := r.Run(context.Background(), mergeCandidates()); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("switching to merge mode made %d calls, want 1 (cache keyed by mode)", calls)
	}
}
