// Package ui carries run progress events from the pipeline to a consumer:
// either the plain slog output or the full-screen Bubble Tea TUI.
package ui

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strings"

	"github.com/StevenACoffman/codetospec/internal/graph"
	"github.com/StevenACoffman/codetospec/internal/llm"
)

// Sink receives pipeline progress events. Implementations must be safe for
// concurrent use (map workers emit in parallel).
type Sink interface {
	Emit(event any)
}

// RunStarted opens a run.
type RunStarted struct {
	Src     string
	Out     string
	Model   string
	Workers int
}

// FileExtracted reports one walked file (layer A).
type FileExtracted struct {
	Path     string
	Language string // "" when no grammar matches
	Facts    int
}

// ExtractorFinished reports one external extractor (layer B).
type ExtractorFinished struct {
	Name   string
	Status string // "ok" | "failed"
	Facts  int
}

// FactsMerged reports the merged fact set.
type FactsMerged struct {
	Total  int
	ByKind map[string]int
}

// Chunked reports the chunking result.
type Chunked struct {
	Chunks int
}

// PhaseChanged reports a pipeline phase transition.
type PhaseChanged struct {
	Phase string // "extract" | "map" | "reduce" | "build" | "render"
}

// MapUnit reports one mapped chunk (cached or live).
type MapUnit struct {
	Path   string
	Lines  string
	Domain string
	Rules  int
	Failed bool
	Done   int
	Total  int
	Usage  llm.Usage
}

// ReduceUnit reports one reduced domain.
type ReduceUnit struct {
	Domain string
	Rules  int
	Failed bool
	Done   int
	Total  int
	Usage  llm.Usage
}

// CrosscheckUnit reports one adversarially reviewed rule.
type CrosscheckUnit struct {
	RuleID  string
	Verdict string // "supported" | "partial" | "unsupported" | "" when failed
	Done    int
	Total   int
	Usage   llm.Usage
}

// LogLine forwards a log record (warnings surface in the TUI journal).
type LogLine struct {
	Level   slog.Level
	Message string
}

// RunFinished closes a run, successfully or not.
type RunFinished struct {
	Err           error
	NodesByType   map[string]int
	Coverage      graph.Coverage
	Violations    []string
	ChunksFailed  int
	ChunksTotal   int
	DomainsFailed int
	DomainsTotal  int
	Extractors    map[string]string
	Tokens        []PhaseUsage // this run's, in phase order
}

// PhaseUsage is the tokens one LLM phase used.
type PhaseUsage struct {
	Phase string
	Usage llm.Usage
}

// PrintSummary writes the end-of-run summary to w (stdout in plain mode,
// and after the TUI exits the alternate screen).
func PrintSummary(w io.Writer, e *RunFinished) {
	_, _ = fmt.Fprintf(w,
		"nodes: %d domains, %d entities, %d endpoints, %d rules\n",
		e.NodesByType[graph.TypeDomain],
		e.NodesByType[graph.TypeEntity],
		e.NodesByType["endpoint"],
		e.NodesByType[graph.TypeRule],
	)
	_, _ = fmt.Fprintf(w, "coverage: endpoints %d/%d, entities %d/%d\n",
		e.Coverage.EndpointsReferenced, e.Coverage.EndpointsTotal,
		e.Coverage.EntitiesTouched, e.Coverage.EntitiesTotal)
	_, _ = fmt.Fprintf(w, "failures: %d/%d chunks, %d/%d domains\n",
		e.ChunksFailed, e.ChunksTotal, e.DomainsFailed, e.DomainsTotal)
	if len(e.Tokens) > 0 {
		parts := make([]string, len(e.Tokens))
		for i, t := range e.Tokens {
			parts[i] = fmt.Sprintf(
				"%s %d+%d",
				t.Phase,
				t.Usage.PromptTokens,
				t.Usage.CompletionTokens,
			)
		}
		_, _ = fmt.Fprintf(
			w,
			"tokens this run: %s (prompt+completion)\n",
			strings.Join(parts, ", "),
		)
	}
	if len(e.Extractors) > 0 {
		names := make([]string, 0, len(e.Extractors))
		for name := range e.Extractors {
			names = append(names, name)
		}
		sort.Strings(names)
		parts := make([]string, len(names))
		for i, name := range names {
			parts[i] = name + "=" + e.Extractors[name]
		}
		_, _ = fmt.Fprintf(w, "extractors: %s\n", strings.Join(parts, " "))
	}
}

// Discard drops every event; used by tests.
type Discard struct{}

// Emit implements Sink.
func (Discard) Emit(any) {}

// Plain logs events to Log and writes the run summary to Out: the non-TUI
// output.
type Plain struct {
	Log *slog.Logger
	Out io.Writer
}

// Emit implements Sink.
func (p Plain) Emit(event any) {
	switch e := event.(type) {
	case FactsMerged:
		p.Log.Info("extract done", "facts", e.Total)
	case Chunked:
		p.Log.Info("chunking done", "chunks", e.Chunks)
	case ExtractorFinished:
		p.Log.Info("extractor finished", "name", e.Name, "status", e.Status, "facts", e.Facts)
	case MapUnit:
		if e.Done%25 == 0 {
			p.Log.Info("map progress", "done", e.Done, "total", e.Total)
		}
	case ReduceUnit:
		p.Log.Info("reduce progress", "domain", e.Domain, "done", e.Done, "total", e.Total)
	case CrosscheckUnit:
		p.Log.Info(
			"crosscheck progress",
			"rule",
			e.RuleID,
			"verdict",
			e.Verdict,
			"done",
			e.Done,
			"total",
			e.Total,
		)
	case LogLine:
		p.Log.Log(context.Background(), e.Level, e.Message)
	case RunFinished:
		if e.Err == nil {
			PrintSummary(p.Out, &e)
		}
	}
}
