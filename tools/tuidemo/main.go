// Command tuidemo renders the TUI view once with realistic data, for
// documentation screenshots. Not part of the shipped binary.
package main

import (
	"fmt"
	"io"
	"os"
	"slices"

	"github.com/StevenACoffman/codetospec/internal/llm"
	"github.com/StevenACoffman/codetospec/internal/ui"
	tea "github.com/charmbracelet/bubbletea"
)

// The demo run: a Go service mid-crosscheck, with counts close to a real
// scan.
const (
	demoWorkers      = 8
	screenWidth      = 100
	screenHeight     = 34
	factsSymbols     = 402
	factsImports     = 61
	factsRoutes      = 8
	factsTables      = 27
	factsModules     = 14
	goFacts          = 35
	filesWalked      = 118
	factsPerFile     = 3
	chunks           = 460
	rulesPerChunk    = 2
	mapPrompt        = 2500
	mapCompletion    = 700
	domains          = 21
	rulesPerDomain   = 12
	reducePrompt     = 9000
	reduceCompletion = 4200
	rulesTotal       = 312
	rulesChecked     = 210
	checkPrompt      = 1400
	checkCompletion  = 190
	supportedShare   = 6
	repairedShare    = 3
	partialShare     = 2
	unsupportedShare = 2
)

func main() {
	if err := run(os.Stdout); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// run feeds the demo events to the dashboard and writes its view to w.
func run(w io.Writer) error {
	var model tea.Model = ui.NewModel("old-legacy-app", "./spec-graph", "deepseek-chat", demoWorkers, nil)
	for _, msg := range demoEvents() {
		model, _ = model.Update(msg)
	}
	_, err := fmt.Fprintln(w, model.View())
	return err
}

// demoEvents is the event stream of the demo run, in order.
func demoEvents() []tea.Msg {
	msgs := []tea.Msg{
		tea.WindowSizeMsg{Width: screenWidth, Height: screenHeight},
		ui.FactsMerged{
			Total: factsSymbols + factsImports + factsRoutes + factsTables + factsModules,
			ByKind: map[string]int{
				"symbol": factsSymbols,
				"import": factsImports,
				"route":  factsRoutes,
				"table":  factsTables,
				"module": factsModules,
			},
		},
		ui.ExtractorFinished{Name: "go", Status: "ok", Facts: goFacts},
		ui.ExtractorFinished{Name: "scip", Status: "ok", Facts: factsSymbols},
		ui.Chunked{Chunks: chunks},
	}
	for range filesWalked {
		msgs = append(
			msgs,
			ui.FileExtracted{Path: "service/x.go", Language: "go", Facts: factsPerFile},
		)
	}
	for i := range chunks { // map complete
		msgs = append(msgs, ui.MapUnit{
			Path: "service/billing.go", Lines: "42-118", Domain: "billing",
			Rules: rulesPerChunk, Done: i + 1, Total: chunks,
			Usage: llm.Usage{PromptTokens: mapPrompt, CompletionTokens: mapCompletion},
		})
	}
	msgs = append(msgs, ui.PhaseChanged{Phase: "reduce"})
	for i := range domains { // reduce complete
		msgs = append(msgs, ui.ReduceUnit{
			Domain: "service", Rules: rulesPerDomain, Done: i + 1, Total: domains,
			Usage: llm.Usage{PromptTokens: reducePrompt, CompletionTokens: reduceCompletion},
		})
	}
	msgs = append(msgs,
		ui.PhaseChanged{Phase: "crosscheck"},
		ui.LogLine{
			Message: "repaired rule.billing.prorata-refund → cited span 88-140 overlaps func Refund",
		},
	)
	return append(msgs, demoVerdicts()...)
}

// demoVerdicts is the crosscheck in progress, verdicts in roughly the
// observed proportions.
func demoVerdicts() []tea.Msg {
	cycle := slices.Concat(
		slices.Repeat([]string{"supported"}, supportedShare),
		slices.Repeat([]string{"repaired"}, repairedShare),
		slices.Repeat([]string{"partial"}, partialShare),
		slices.Repeat([]string{"unsupported"}, unsupportedShare),
	)
	msgs := make([]tea.Msg, 0, rulesChecked)
	for done := range rulesChecked {
		msgs = append(msgs, ui.CrosscheckUnit{
			RuleID: "rule.x", Verdict: cycle[done%len(cycle)], Done: done + 1, Total: rulesTotal,
			Usage: llm.Usage{PromptTokens: checkPrompt, CompletionTokens: checkCompletion},
		})
	}
	return msgs
}
