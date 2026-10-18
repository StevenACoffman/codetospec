// Command codetospec reads a source repository, extracts business rules and
// produces a versionable markdown knowledge graph.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/StevenACoffman/codetospec/internal/config"
	"github.com/StevenACoffman/codetospec/internal/drift"
	"github.com/StevenACoffman/codetospec/internal/render"
	"github.com/StevenACoffman/codetospec/internal/state"
	"github.com/StevenACoffman/codetospec/internal/ui"
	"github.com/StevenACoffman/codetospec/internal/verify"
	tea "github.com/charmbracelet/bubbletea"
)

// Exit codes of the command.
const (
	exitOK          = 0
	exitFailure     = 1
	exitUsage       = 2
	exitInterrupted = 130 // 128 + SIGINT, the shell's convention
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

const usageText = `usage:
  codetospec run --src <repo-dir> --out <graph-dir> [flags]
  codetospec verify --src <repo-dir> --out <graph-dir>
  codetospec drift --src <repo-dir> --out <graph-dir>
  codetospec stats --out <graph-dir>`

// run parses the command line and dispatches the command; it returns the
// exit code.
func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) < 1 {
		_, _ = fmt.Fprintln(stderr, usageText)
		return exitUsage
	}
	parse := map[string]func([]string) (*config.Config, error){
		"run":    config.ParseRun,
		"verify": config.ParseVerify,
		"drift":  config.ParseDrift,
		"stats":  config.ParseStats,
	}[args[0]]
	if parse == nil {
		_, _ = fmt.Fprintln(stderr, usageText)
		return exitUsage
	}
	cfg, err := parse(args[1:])
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return exitUsage
	}
	c := cli{stdout: stdout, stderr: stderr, logger: newLogger(stderr, cfg.LogLevel)}
	switch args[0] {
	case "run":
		if useTUI(cfg, stdout) {
			return c.tui(ctx, cfg)
		}
		return c.pipeline(ctx, cfg)
	case "verify":
		return c.verify(cfg)
	case "drift":
		return c.drift(cfg)
	default: // stats
		return c.stats(cfg)
	}
}

// cli is what every command writes to: results on stdout, diagnostics on
// stderr through logger.
type cli struct {
	stdout, stderr io.Writer
	logger         *slog.Logger
}

// newLogger returns the logger a command passes down to every phase: text
// lines on w at level (--log-level; info when unparsable).
func newLogger(w io.Writer, level string) *slog.Logger {
	var l slog.Level
	if err := l.UnmarshalText([]byte(level)); err != nil {
		l = slog.LevelInfo
	}
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: l}))
}

// useTUI enables the dashboard when stdout is an interactive terminal,
// unless --no-tui.
func useTUI(cfg *config.Config, stdout io.Writer) bool {
	f, isFile := stdout.(*os.File)
	if cfg.NoTUI || !isFile {
		return false
	}
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// pipeline runs the pipeline with plain log output.
func (c cli) pipeline(ctx context.Context, cfg *config.Config) int {
	err := runPipeline(ctx, cfg, ui.Plain{Log: c.logger, Out: c.stdout}, c.logger)
	switch {
	case err == nil:
		return exitOK
	case errors.Is(err, context.Canceled):
		c.logger.WarnContext(ctx, "interrupted, state saved; rerun to resume")
		return exitInterrupted
	default:
		c.logger.ErrorContext(ctx, "run failed", "err", err)
		return exitFailure
	}
}

// tui runs the pipeline behind the full-screen Bubble Tea dashboard.
func (c cli) tui(ctx context.Context, cfg *config.Config) int {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	program := tea.NewProgram(
		ui.NewModel(cfg.Src, cfg.Out, cfg.Model, cfg.Workers, cancel),
		tea.WithAltScreen(),
	)
	sink := ui.TUISink{Program: program}
	// While the dashboard owns the terminal, warnings go to its log pane.
	dashboardLog := slog.New(ui.LogHandler{Sink: sink, Min: slog.LevelWarn})

	go func() {
		// runPipeline emits RunFinished itself, which quits the program.
		_ = runPipeline(runCtx, cfg, sink, dashboardLog)
	}()

	finalModel, err := program.Run()
	if err != nil {
		c.logger.ErrorContext(ctx, "tui failed", "err", err)
		return exitFailure
	}
	model, ok := finalModel.(*ui.Model)
	if !ok {
		return exitFailure
	}
	result, finished := model.Result()
	switch {
	case !finished, errors.Is(result.Err, context.Canceled):
		// The user quit, or interrupted the run; state is saved.
		_, _ = fmt.Fprintln(c.stdout, "interrupted, state saved; rerun to resume")
		return exitInterrupted
	case result.Err != nil:
		_, _ = fmt.Fprintln(c.stderr, "run failed:", result.Err)
		for _, v := range result.Violations {
			_, _ = fmt.Fprintln(c.stderr, " -", v)
		}
		return exitFailure
	default:
		ui.PrintSummary(c.stdout, &result)
		return exitOK
	}
}

// verify checks a rendered graph against the source tree.
func (c cli) verify(cfg *config.Config) int {
	nodes, err := render.LoadDir(cfg.Out)
	if err != nil {
		c.logger.Error("cannot load graph", "err", err)
		return exitFailure
	}
	if len(nodes) == 0 {
		c.logger.Error("no nodes found", "out", cfg.Out)
		return exitFailure
	}
	if violations := verify.Run(nodes, cfg.Roots()); len(violations) > 0 {
		for _, v := range violations {
			_, _ = fmt.Fprintln(c.stderr, v.String())
		}
		return exitFailure
	}
	_, _ = fmt.Fprintf(c.stdout, "ok: %d nodes verified\n", len(nodes))
	return exitOK
}

// drift reports the rules whose cited code changed since they were
// rendered; it fails when any did.
func (c cli) drift(cfg *config.Config) int {
	nodes, err := render.LoadDir(cfg.Out)
	if err != nil {
		c.logger.Error("cannot load graph", "err", err)
		return exitFailure
	}
	report := drift.Check(nodes, cfg.Roots())

	if cfg.Format == "json" {
		payload := map[string]int{
			"ok": report.OK, "stale": report.Stale,
			"broken": report.Broken, "unknown": report.Unknown, "total": len(report.Rules),
		}
		data, encodeErr := json.MarshalIndent(payload, "", "  ")
		if encodeErr != nil {
			c.logger.Error("encode drift report", "err", encodeErr)
			return exitFailure
		}
		_, _ = fmt.Fprintln(c.stdout, string(data))
	} else {
		for _, rs := range report.Rules {
			if rs.Status == drift.StatusStale || rs.Status == drift.StatusBroken {
				_, _ = fmt.Fprintf(c.stdout, "%s: %s — %s\n", rs.Status, rs.RuleID, rs.Detail)
			}
		}
		_, _ = fmt.Fprintf(
			c.stdout,
			"drift: %d ok, %d stale, %d broken, %d unknown (of %d rules)\n",
			report.OK,
			report.Stale,
			report.Broken,
			report.Unknown,
			len(report.Rules),
		)
	}
	if report.Drifted() {
		return exitFailure
	}
	return exitOK
}

// stats prints the progress and token use recorded in the state file.
func (c cli) stats(cfg *config.Config) int {
	s, err := state.Load(filepath.Join(cfg.Out, ".codetospec", "state.json"))
	if err != nil {
		c.logger.Error("cannot load state", "err", err)
		return exitFailure
	}
	w := c.stdout
	_, _ = fmt.Fprintf(w, "phase: %s (started %s)\n", s.Phase, s.StartedAt)
	_, _ = fmt.Fprintf(w, "chunks: %d done, %d failed, %d total\n",
		s.ChunksDone, s.ChunksFailed, s.ChunksTotal)
	_, _ = fmt.Fprintf(w, "domains: %d done, %d failed, %d total\n",
		s.DomainsDone, s.DomainsFailed, s.DomainsTotal)
	for _, phase := range state.Phases() {
		if t := s.Tokens[phase]; t != nil {
			_, _ = fmt.Fprintf(w,
				"tokens %s: last run prompt %d, completion %d; all runs prompt %d, completion %d\n",
				phase, t.LastPrompt, t.LastCompletion, t.Prompt, t.Completion,
			)
		}
	}
	for _, name := range sortedKeys(s.Extractors) {
		_, _ = fmt.Fprintf(w, "extractor %s: %s\n", name, s.Extractors[name])
	}
	return exitOK
}
