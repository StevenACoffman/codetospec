package crosscheck

import (
	"context"
	"strings"
	"testing"

	"codetospec/internal/extract"
	"codetospec/internal/graph"
	"codetospec/internal/llm"
	"codetospec/internal/mapper"
)

type chatFunc func(ctx context.Context, msgs []llm.Message) (string, llm.Usage, error)

func (f chatFunc) Chat(ctx context.Context, msgs []llm.Message) (string, llm.Usage, error) {
	return f(ctx, msgs)
}

func ruleNode() graph.Node {
	return graph.Node{
		ID:     "rule.services.prorata-activation",
		Type:   "rule",
		Status: graph.StatusGenerated,
		Title:  "Prorata à l'activation",
		Body: "**Exigence (EARS)** : QUAND un abonné active en cours de mois, le systeme doit facturer au prorata.\n\n" +
			"**Critères d'acceptation** :\n1. Cas nominal.\n2. Cas limite.",
		Sources: []extract.Ref{
			{Path: "app/Services/Billing/ProrataCalculator.php", Lines: "11-24"},
		},
		Extra: map[string]string{"ears": "event", "acceptance": "2"},
	}
}

// supportedReply is a minimal valid crosscheck reply.
const supportedReply = `{"verdict": "supported", "reason": "ok"}`

func newChecker(t *testing.T, chat llm.Chatter) *Checker {
	t.Helper()
	return &Checker{
		LLM:     chat,
		Lang:    "fr",
		Workers: 2,
		OutDir:  t.TempDir(),
		SrcRoot: "../../testdata/fixture",
	}
}

func TestCitedExcerptShowsContext(t *testing.T) {
	c := newChecker(t, nil)
	excerpt, err := c.citedExcerpt(ruleNode().Sources) // cites lines 11-24
	if err != nil {
		t.Fatalf("citedExcerpt: %v", err)
	}
	// Cited lines are marked ">", context lines "·".
	if !strings.Contains(excerpt, "11 > ") {
		t.Errorf("first cited line 11 should be marked '>':\n%s", excerpt)
	}
	// At least one line before the cited span (< 11) must appear as context.
	hasContext := false
	for ln := range strings.SplitSeq(excerpt, "\n") {
		if strings.Contains(ln, " · ") {
			hasContext = true
			break
		}
	}
	if !hasContext {
		t.Errorf("excerpt should include surrounding context lines marked '·':\n%s", excerpt)
	}
}

func TestCrosscheckNominal(t *testing.T) {
	calls := 0
	c := newChecker(
		t,
		chatFunc(func(_ context.Context, msgs []llm.Message) (string, llm.Usage, error) {
			calls++
			if !strings.Contains(msgs[0].Content, "adversarial code reviewer") {
				t.Error("system prompt missing adversarial role")
			}
			user := msgs[1].Content
			if !strings.Contains(user, "RULE: rule.services.prorata-activation") {
				t.Errorf("user prompt missing rule id:\n%s", user)
			}
			// The prompt must carry the real cited source lines, numbered.
			if !strings.Contains(user, "app/Services/Billing/ProrataCalculator.php:11-24") ||
				!strings.Contains(user, "public function calculate") {
				t.Errorf("user prompt missing cited source:\n%s", user)
			}
			return `{"verdict": "supported", "reason": "Le calcul au prorata est visible lignes 21-23."}`, llm.Usage{
				PromptTokens:     50,
				CompletionTokens: 10,
			}, nil
		}),
	)

	verdicts, err := c.Run(
		context.Background(),
		[]graph.Node{ruleNode(), {ID: "domain.x", Type: "domain"}},
	)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1 (domains are not checked)", calls)
	}
	v := verdicts["rule.services.prorata-activation"]
	if v.Verdict != VerdictSupported || v.Reason == "" {
		t.Fatalf("verdict = %+v", v)
	}
}

func TestCrosscheckRejectsBadVerdictThenCorrects(t *testing.T) {
	calls := 0
	c := newChecker(
		t,
		chatFunc(func(_ context.Context, msgs []llm.Message) (string, llm.Usage, error) {
			calls++
			if calls == 1 {
				return `{"verdict": "maybe", "reason": "?"}`, llm.Usage{}, nil
			}
			last := msgs[len(msgs)-1]
			if !strings.Contains(last.Content, "output rejected:") {
				t.Errorf("correction message malformed: %q", last.Content)
			}
			return `{"verdict": "partial", "reason": "Le seuil cité diffère de l'exigence."}`, llm.Usage{}, nil
		}),
	)

	verdicts, err := c.Run(context.Background(), []graph.Node{ruleNode()})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if calls != 2 || verdicts["rule.services.prorata-activation"].Verdict != VerdictPartial {
		t.Fatalf("calls=%d verdicts=%+v", calls, verdicts)
	}
}

func TestCrosscheckDoubleFailureIsTraced(t *testing.T) {
	c := newChecker(t, chatFunc(func(context.Context, []llm.Message) (string, llm.Usage, error) {
		return "not json", llm.Usage{}, nil
	}))
	verdicts, err := c.Run(context.Background(), []graph.Node{ruleNode()})
	if err != nil {
		t.Fatalf("Run should continue on failure, got %v", err)
	}
	v := verdicts["rule.services.prorata-activation"]
	if !v.Failed || v.Error == "" {
		t.Fatalf("verdict should be traced as failed: %+v", v)
	}
}

func TestCrosscheckCacheByInputHash(t *testing.T) {
	c := newChecker(t, chatFunc(func(context.Context, []llm.Message) (string, llm.Usage, error) {
		return supportedReply, llm.Usage{}, nil
	}))
	if _, err := c.Run(context.Background(), []graph.Node{ruleNode()}); err != nil {
		t.Fatal(err)
	}

	// Unchanged rule: cache hit, no LLM call.
	c.LLM = chatFunc(func(context.Context, []llm.Message) (string, llm.Usage, error) {
		t.Fatal("cached verdict must not trigger an LLM call")
		return "", llm.Usage{}, nil
	})
	if _, err := c.Run(context.Background(), []graph.Node{ruleNode()}); err != nil {
		t.Fatal(err)
	}

	// Changed requirement: hash differs, the rule is re-checked.
	recalled := false
	c.LLM = chatFunc(func(context.Context, []llm.Message) (string, llm.Usage, error) {
		recalled = true
		return `{"verdict": "unsupported", "reason": "plus rien ne correspond"}`, llm.Usage{}, nil
	})
	changed := ruleNode()
	changed.Body = strings.ReplaceAll(changed.Body, "au prorata", "en double")
	verdicts, err := c.Run(context.Background(), []graph.Node{changed})
	if err != nil {
		t.Fatal(err)
	}
	if !recalled || verdicts[changed.ID].Verdict != VerdictUnsupported {
		t.Fatalf("changed rule should be re-checked, got %+v", verdicts[changed.ID])
	}
}

func TestRepairRecitesToGroundedSymbol(t *testing.T) {
	call := 0
	c := newChecker(
		t,
		chatFunc(func(_ context.Context, msgs []llm.Message) (string, llm.Usage, error) {
			call++
			if call == 1 { // adversarial verify -> unsupported
				return `{"verdict": "unsupported", "reason": "Les lignes citées ne montrent aucune logique."}`, llm.Usage{}, nil
			}
			// repair call: must carry the symbol catalog; re-cite the symbol span.
			user := msgs[1].Content
			if !strings.Contains(user, "AVAILABLE_SYMBOLS") ||
				!strings.Contains(user, "calculate (method) lines 12-24") {
				t.Errorf("repair prompt missing symbol catalog:\n%s", user)
			}
			return `{"citations": [{"path": "app/Services/Billing/ProrataCalculator.php", "lines": "12-24"}], "reason": "corps de calculate"}`, llm.Usage{}, nil
		}),
	)
	c.Repair = true
	c.SymbolsFor = func(path string) []mapper.SymbolContext {
		if path != "app/Services/Billing/ProrataCalculator.php" {
			return nil
		}
		return []mapper.SymbolContext{
			{
				Name:      "calculate",
				StartLine: 12,
				EndLine:   24,
				Kind:      "method",
				Signature: "func calculate()",
			},
		}
	}

	verdicts, err := c.Run(context.Background(), []graph.Node{ruleNode()})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	v := verdicts["rule.services.prorata-activation"]
	if v.Verdict != VerdictRepaired {
		t.Fatalf("verdict = %q, want repaired", v.Verdict)
	}
	if len(v.NewCitations) != 1 || v.NewCitations[0].Lines != "12-24" {
		t.Fatalf("new citations = %+v, want the symbol span 12-24", v.NewCitations)
	}
}

func TestRepairRejectsUngroundedCitation(t *testing.T) {
	call := 0
	c := newChecker(
		t,
		chatFunc(func(_ context.Context, msgs []llm.Message) (string, llm.Usage, error) {
			call++
			if call == 1 {
				return `{"verdict": "unsupported", "reason": "rien"}`, llm.Usage{}, nil
			}
			// The model cites lines outside any symbol body -> must be rejected.
			return `{"citations": [{"path": "app/Services/Billing/ProrataCalculator.php", "lines": "1-3"}], "reason": "x"}`, llm.Usage{}, nil
		}),
	)
	c.Repair = true
	c.SymbolsFor = func(string) []mapper.SymbolContext {
		return []mapper.SymbolContext{{Name: "calculate", StartLine: 12, EndLine: 24}}
	}

	verdicts, err := c.Run(context.Background(), []graph.Node{ruleNode()})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	v := verdicts["rule.services.prorata-activation"]
	if v.Verdict != VerdictUnsupported {
		t.Fatalf("ungrounded citation must not repair; verdict = %q", v.Verdict)
	}
	if len(v.NewCitations) != 0 {
		t.Errorf("no citations should be adopted, got %+v", v.NewCitations)
	}
}

func TestRepairNoopWithoutSymbols(t *testing.T) {
	call := 0
	c := newChecker(t, chatFunc(func(context.Context, []llm.Message) (string, llm.Usage, error) {
		call++
		return `{"verdict": "unsupported", "reason": "rien"}`, llm.Usage{}, nil
	}))
	c.Repair = true // but SymbolsFor is nil -> repair is skipped
	verdicts, err := c.Run(context.Background(), []graph.Node{ruleNode()})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if call != 1 {
		t.Fatalf("without symbols, no repair call should happen; calls = %d", call)
	}
	if verdicts["rule.services.prorata-activation"].Verdict != VerdictUnsupported {
		t.Fatal("verdict should stay unsupported")
	}
}

func TestAnnotateAndCount(t *testing.T) {
	cases := map[string]struct {
		verdict   *Verdict // nil: the rule got no verdict
		wantExtra string
		wantTally Tally
	}{
		"supported": {
			verdict:   &Verdict{Verdict: VerdictSupported},
			wantExtra: VerdictSupported,
			wantTally: Tally{Supported: 1},
		},
		"failed check": {
			verdict:   &Verdict{Failed: true, Error: "boom"},
			wantExtra: VerdictUnchecked,
			wantTally: Tally{Unchecked: 1},
		},
		"no verdict": {
			verdict:   nil,
			wantExtra: VerdictUnchecked,
			wantTally: Tally{Unchecked: 1},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			rule := ruleNode()
			domain := graph.Node{ID: "domain.services", Type: "domain"}
			verdicts := map[string]Verdict{}
			if tc.verdict != nil {
				v := *tc.verdict
				v.RuleID = rule.ID
				verdicts[rule.ID] = v
			}
			nodes := Annotate([]graph.Node{rule, domain}, verdicts)
			if got := nodes[0].Extra["crosscheck"]; got != tc.wantExtra {
				t.Errorf("rule crosscheck = %q, want %q", got, tc.wantExtra)
			}
			if _, ok := nodes[1].Extra["crosscheck"]; ok {
				t.Errorf("non-rule node annotated: %v", nodes[1].Extra)
			}
			if got := Count(nodes); got != tc.wantTally {
				t.Errorf("tally = %+v, want %+v", got, tc.wantTally)
			}
		})
	}
}

func TestCrosscheckCacheKeyedByModelAndPrompt(t *testing.T) {
	cases := map[string]struct {
		change    func(c *Checker)
		wantCalls int
	}{
		"unchanged":   {change: func(*Checker) {}, wantCalls: 0},
		"other model": {change: func(c *Checker) { c.Model = "model-b" }, wantCalls: 1},
		"other lang":  {change: func(c *Checker) { c.Lang = "en" }, wantCalls: 1},
		"repair on":   {change: func(c *Checker) { c.Repair = true }, wantCalls: 1},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			c := newChecker(
				t,
				chatFunc(func(context.Context, []llm.Message) (string, llm.Usage, error) {
					return supportedReply, llm.Usage{}, nil
				}),
			)
			c.Model = "model-a"
			if _, err := c.Run(context.Background(), []graph.Node{ruleNode()}); err != nil {
				t.Fatalf("first Run: %v", err)
			}

			calls := 0
			c.LLM = chatFunc(func(context.Context, []llm.Message) (string, llm.Usage, error) {
				calls++
				return supportedReply, llm.Usage{}, nil
			})
			tc.change(c)
			if _, err := c.Run(context.Background(), []graph.Node{ruleNode()}); err != nil {
				t.Fatalf("second Run: %v", err)
			}
			if calls != tc.wantCalls {
				t.Fatalf("second Run made %d LLM calls, want %d", calls, tc.wantCalls)
			}
		})
	}
}

func TestMechanicalRulesSkipTheReviewer(t *testing.T) {
	mechanical := ruleNode()
	mechanical.ID = "rule.services.mechanical"
	mechanical.Extra = map[string]string{graph.ExtraExtractedBy: "webapp"}
	reviewed := []string{}
	c := newChecker(
		t,
		chatFunc(func(_ context.Context, msgs []llm.Message) (string, llm.Usage, error) {
			reviewed = append(reviewed, msgs[1].Content)
			return supportedReply, llm.Usage{}, nil
		}),
	)
	c.Workers = 1
	nodes := []graph.Node{ruleNode(), mechanical}
	verdicts, err := c.Run(context.Background(), nodes)
	if err != nil {
		t.Fatal(err)
	}
	if len(reviewed) != 1 || strings.Contains(reviewed[0], mechanical.ID) {
		t.Fatalf("reviewer saw %d prompts, want only the LLM rule", len(reviewed))
	}
	nodes = Annotate(nodes, verdicts)
	if got := nodes[1].Extra["crosscheck"]; got != VerdictMechanical {
		t.Errorf("mechanical rule crosscheck = %q, want %q", got, VerdictMechanical)
	}
	if got := Count(nodes); got != (Tally{Supported: 1, Mechanical: 1}) {
		t.Errorf("tally = %+v", got)
	}
}

// TestAnnotateKeepsInheritedVerdict: a rule inherited from another spec is
// not reviewed here; it keeps the verdict it came with.
func TestAnnotateKeepsInheritedVerdict(t *testing.T) {
	rule := ruleNode()
	rule.Extra = map[string]string{graph.ExtraInheritedFrom: "pkg", "crosscheck": VerdictSupported}
	if graph.Reviewable(&rule) {
		t.Fatal("an inherited rule is reviewable")
	}
	verdicts := map[string]Verdict{rule.ID: {RuleID: rule.ID, Verdict: VerdictUnsupported}}
	if got := Annotate([]graph.Node{rule}, verdicts)[0].Extra["crosscheck"]; got != VerdictSupported {
		t.Fatalf("crosscheck = %q, want the inherited %q", got, VerdictSupported)
	}
}
