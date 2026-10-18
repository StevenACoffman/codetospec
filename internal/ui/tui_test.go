package ui_test

import (
	"strings"
	"testing"

	"github.com/StevenACoffman/codetospec/internal/llm"
	"github.com/StevenACoffman/codetospec/internal/ui"
	tea "github.com/charmbracelet/bubbletea"
)

// billing is the domain the progress events name.
const billing = "billing"

func drive(t *testing.T, m *ui.Model, msgs ...tea.Msg) *ui.Model {
	t.Helper()
	var model tea.Model = m
	for _, msg := range msgs {
		model, _ = model.Update(msg)
	}
	result, ok := model.(*ui.Model)
	if !ok {
		t.Fatalf("model type changed: %T", model)
	}
	return result
}

func TestModelViewReflectsProgress(t *testing.T) {
	m := ui.NewModel("old-legacy-app", "spec-legacy", "deepseek-chat", 4, nil)
	m = drive(t, m,
		tea.WindowSizeMsg{Width: 100, Height: 30},
		ui.FileExtracted{Path: "index.php", Language: "php", Facts: 3},
		ui.FileExtracted{Path: "app/billing.php", Language: "php", Facts: 5},
		ui.FactsMerged{Total: 12, ByKind: map[string]int{"symbol": 8, "module": 4}},
		ui.Chunked{Chunks: 38},
		ui.PhaseChanged{Phase: "map"},
		ui.MapUnit{
			Path:   "app/billing.php",
			Lines:  "10-80",
			Domain: billing,
			Rules:  2,
			Done:   1,
			Total:  38,
			Usage:  llm.Usage{PromptTokens: 4000, CompletionTokens: 500},
		},
		ui.MapUnit{
			Path: "index.php", Lines: "1-90", Domain: "core", Rules: 1, Done: 2, Total: 38,
			Usage: llm.Usage{PromptTokens: 3000, CompletionTokens: 400},
		},
		ui.LogLine{Message: "extractor failed name=php"},
	)

	// Header, hero (candidate rules), stage strip, per-domain bars, tokens.
	view := m.View()
	for _, want := range []string{
		"codetospec", "old-legacy-app", "deepseek-chat",
		"CANDIDATE RULES",                   // hero caption
		"2/38",                              // chunks mapped, in the hero subline
		"extract", "map", "reduce", "check", // stage strip
		"CANDIDATES PER DOMAIN", "billing", // domain bars
		"7k+900",   // map tokens: prompt 7000, completion 900
		"[q] quit", // footer hint
	} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q:\n%s", want, view)
		}
	}

	// Advance into crosscheck: the verdict stream and proven tally appear.
	m = drive(t, m,
		ui.PhaseChanged{Phase: "reduce"},
		ui.ReduceUnit{Domain: billing, Rules: 2, Done: 1, Total: 2},
		ui.PhaseChanged{Phase: "crosscheck"},
		ui.CrosscheckUnit{RuleID: "rule.billing.prorata", Verdict: "supported", Done: 1, Total: 2},
		ui.CrosscheckUnit{RuleID: "rule.billing.refund", Verdict: "unsupported", Done: 2, Total: 2},
	)
	view = m.View()
	for _, want := range []string{
		"RULES TO REVIEW",      // hero flips to the review countdown
		"LATEST VERDICTS",      // verdict panel
		"rule.billing.prorata", // a flipped rule id
		"proven",               // tally
		"1 to review",          // the unsupported one
	} {
		if !strings.Contains(view, want) {
			t.Errorf("crosscheck view missing %q:\n%s", want, view)
		}
	}
}

func TestModelQuitPathways(t *testing.T) {
	cancelled := false
	m := ui.NewModel("src", "out", "m", 1, func() { cancelled = true })
	m = drive(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})

	// First q cancels the pipeline but keeps the screen up.
	m = drive(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if !cancelled {
		t.Fatal("q should cancel the pipeline context")
	}
	if _, finished := m.Result(); finished {
		t.Fatal("model must not be finished before ui.RunFinished")
	}

	// ui.RunFinished quits and exposes the result.
	m = drive(t, m, ui.RunFinished{NodesByType: map[string]int{"rule": 3}})
	result, finished := m.Result()
	if !finished || result.NodesByType["rule"] != 3 {
		t.Fatalf("result = %+v finished=%v", result, finished)
	}
}

// TestFormatTokens: token counts read as 950, 1k, 166k or 2.4M in the
// footer.
func TestFormatTokens(t *testing.T) {
	cases := map[int]string{950: "950+0", 1500: "1k+0", 166446: "166k+0", 2_400_000: "2.4M+0"}
	for in, want := range cases {
		t.Run(want, func(t *testing.T) {
			m := drive(t, ui.NewModel("src", "out", "m", 1, nil),
				tea.WindowSizeMsg{Width: 120, Height: 30},
				ui.MapUnit{Done: 1, Total: 1, Usage: llm.Usage{PromptTokens: in}})
			if view := m.View(); !strings.Contains(view, "map "+want) {
				t.Errorf("footer lacks %q:\n%s", "map "+want, view)
			}
		})
	}
}
