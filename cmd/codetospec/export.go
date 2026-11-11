package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"

	"github.com/StevenACoffman/codetospec/internal/atomicfs"
	"github.com/StevenACoffman/codetospec/internal/cedarpolicy"
	"github.com/StevenACoffman/codetospec/internal/cerbos"
	"github.com/StevenACoffman/codetospec/internal/config"
	"github.com/StevenACoffman/codetospec/internal/d2"
	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/graph"
	"github.com/StevenACoffman/codetospec/internal/kiro"
	"github.com/StevenACoffman/codetospec/internal/locale"
	"github.com/StevenACoffman/codetospec/internal/nfrtarget"
	"github.com/StevenACoffman/codetospec/internal/policy"
	"github.com/StevenACoffman/codetospec/internal/policyview"
	"github.com/StevenACoffman/codetospec/internal/quire"
	"github.com/StevenACoffman/codetospec/internal/render"
)

// errExportTarget means the export directory already holds files.
var errExportTarget = errors.New("export target is not empty")

// export writes the rules of the graph at cfg.Out as a spec under
// cfg.To, in cfg.Format: Quire FRs and NFRs (the default) or Kiro specs. It is a
// one-time bootstrap: once a team edits the exported documents, a second
// export would overwrite their work, so the target must be new or empty.
func (c cli) export(ctx context.Context, cfg *config.Config) int {
	nodes, err := render.LoadDir(cfg.Out)
	if err != nil {
		c.logger.ErrorContext(ctx, "cannot load graph", "err", err)
		return exitFailure
	}
	switch cfg.Format {
	case config.ExportKiro:
		return c.exportKiro(cfg, nodes)
	case config.ExportCerbos, config.ExportCedarPolicy:
		return c.exportPolicies(cfg, nodes)
	case config.ExportViews:
		return c.exportViews(ctx, cfg, nodes)
	}
	nfrTargets, err := nfrtarget.Load(cfg.Out)
	if err != nil {
		c.logger.ErrorContext(ctx, "cannot load the NFR targets", "err", err)
		return exitFailure
	}
	spec, err := quire.Export(nodes, nfrTargets)
	if err != nil {
		c.logger.ErrorContext(ctx, "cannot export", "err", err)
		return exitFailure
	}
	if err = writeSpec(cfg.To, spec.Files); err != nil {
		c.logger.ErrorContext(ctx, "cannot write the spec", "dir", cfg.To, "err", err)
		return exitFailure
	}
	c.logRefuted(spec.Refuted)
	_, _ = fmt.Fprintf(c.stdout,
		"export: %d functional requirement(s) written to %s, %d non-functional to %s, "+
			"%d refuted rule(s) left out\n",
		spec.FRs, filepath.Join(cfg.To, quire.Directory),
		spec.NFRs, filepath.Join(cfg.To, quire.NFRDirectory), len(spec.Refuted))
	return exitOK
}

// exportKiro writes nodes as Kiro specs, one per domain, the actors
// described by the glossary the run kept in graph.json.
func (c cli) exportKiro(cfg *config.Config, nodes []graph.Node) int {
	actors, err := render.LoadActors(cfg.Out)
	if err != nil {
		c.logger.Error("cannot load the actors glossary", "err", err)
		return exitFailure
	}
	spec := kiro.Export(nodes, actors)
	if err = writeSpec(cfg.To, spec.Files); err != nil {
		c.logger.Error("cannot write the spec", "dir", cfg.To, "err", err)
		return exitFailure
	}
	c.logRefuted(spec.Refuted)
	_, _ = fmt.Fprintf(
		c.stdout,
		"export: %d requirement(s) in %d spec file(s) written to %s, %d refuted rule(s) left out\n",
		spec.Requirements,
		len(spec.Files),
		filepath.Join(cfg.To, kiro.Directory),
		len(spec.Refuted),
	)
	return exitOK
}

// policyModel builds the graph's policy model from the run's merged facts
// (capabilities and roles, with --capabilities-from's), the person's
// checkers files (--checkers: what checks the extractor doesn't know
// mean) joined to other services' (--join), and --registry; each relation
// a checker names that the registry doesn't declare is warned about, and
// so is each stale pin. False when an input can't be loaded (logged).
func (c cli) policyModel(
	cfg *config.Config,
	nodes []graph.Node,
) (policy.Model, policy.Registry, []string, bool) {
	facts, err := extract.LoadFactsFile(filepath.Join(cfg.Out, ".codetospec", "facts.json"))
	if err != nil {
		c.logger.Error("cannot load the run's facts", "err", err)
		return policy.Model{}, policy.Registry{}, nil, false
	}
	if cfg.CapabilitiesFrom != "" {
		joined, joinErr := capabilityFacts(cfg.CapabilitiesFrom)
		if joinErr != nil {
			c.logger.Error("cannot load the capabilities", "graph", cfg.CapabilitiesFrom,
				"err", joinErr)
			return policy.Model{}, policy.Registry{}, nil, false
		}
		// Merged by id, as the run merges facts: admin's graph joins its
		// own facts, and each role would otherwise count twice.
		facts = extract.Merge(facts, joined)
	}
	checkers, err := readCheckers(cfg.Checkers)
	if err != nil {
		c.logger.Error("cannot load the checkers", "err", err)
		return policy.Model{}, policy.Registry{}, nil, false
	}
	reg, err := readRegistry(cfg.Registry)
	if err != nil {
		c.logger.Error("cannot load the registry", "err", err)
		return policy.Model{}, policy.Registry{}, nil, false
	}
	if cfg.Registry != "" {
		for _, rel := range checkers.Undeclared(reg) {
			c.logger.Warn("a checker names a relation the registry doesn't declare",
				"relation", rel)
		}
	}
	if checkers, _, err = c.joinedCheckers(cfg.Joins, facts, checkers); err != nil {
		c.logger.Error("cannot join", "err", err)
		return policy.Model{}, policy.Registry{}, nil, false
	}
	stale := c.stalePins(cfg, facts, checkers)
	m := policy.BuildModel(facts, nodes, checkers)
	for _, s := range m.Skipped {
		c.logger.Debug("guard not exported", "rule", s.RuleID, "reason", s.Reason)
	}
	return m, reg, stale, true
}

// exportPolicies writes the graph's extractor guards as authorization
// policies, Cerbos or the Cedar policy language, for the service the graph
// directory is named after; each guard it can't translate is logged at
// debug level.
func (c cli) exportPolicies(cfg *config.Config, nodes []graph.Node) int {
	m, _, stale, ok := c.policyModel(cfg, nodes)
	if !ok {
		return exitFailure
	}
	service := filepath.Base(filepath.Clean(cfg.Out))
	var files map[string][]byte
	var operations, denies int
	var err error
	if cfg.Format == config.ExportCerbos {
		spec, exportErr := cerbos.Export(&m, service)
		files, operations, denies, err = spec.Files, spec.Operations, spec.Denies, exportErr
	} else {
		spec, exportErr := cedarpolicy.Export(&m, service)
		files, operations, denies, err = spec.Files, spec.Operations, spec.Denies, exportErr
	}
	if err != nil {
		c.logger.Error("cannot export", "err", err)
		return exitFailure
	}
	if err = writeSpec(cfg.To, files); err != nil {
		c.logger.Error("cannot write the policies", "dir", cfg.To, "err", err)
		return exitFailure
	}
	summary := fmt.Sprintf(
		"export: %d operation(s), %d item kind(s), %d deny rule(s) written to %s, "+
			"%d guard(s) not translated (see --log-level debug)",
		operations,
		len(m.Items),
		denies,
		cfg.To,
		len(m.Skipped),
	)
	if n := m.ObligationCount(); n > 0 {
		summary += fmt.Sprintf(", %d rate limit(s) left to the enforcement point (obligations)", n)
	}
	if len(stale) > 0 {
		summary += fmt.Sprintf(", %d pin(s) stale", len(stale))
	}
	_, _ = fmt.Fprintln(c.stdout, summary)
	return exitOK
}

// exportViews writes the graph's access views: the access matrix the
// exported policies in the Cedar policy language decide, and a decision
// diagram per guarded operation, drawn with --d2-renderer when found.
func (c cli) exportViews(ctx context.Context, cfg *config.Config, nodes []graph.Node) int {
	m, reg, _, ok := c.policyModel(cfg, nodes)
	if !ok {
		return exitFailure
	}
	service := filepath.Base(filepath.Clean(cfg.Out))
	spec, err := cedarpolicy.Export(&m, service)
	if err != nil {
		c.logger.ErrorContext(ctx, "cannot export", "err", err)
		return exitFailure
	}
	decider, err := cedarpolicy.NewDecider(&spec, service)
	if err != nil {
		c.logger.ErrorContext(ctx, "cannot load the exported policies", "err", err)
		return exitFailure
	}
	views := policyview.Build(&policyview.Input{
		Service: service, Model: &m, Decider: decider, Skipped: skippedOperations(&m, nodes),
		Registry: reg, Text: locale.For(cfg.Lang),
	})
	renderer := d2.FindRenderer(cfg.D2Renderer)
	if renderer.Cmd == "" {
		c.logger.InfoContext(ctx, "no D2 renderer: the diagrams are written as their sources",
			"renderer", cfg.D2Renderer)
	}
	pictures, err := renderer.Draw(ctx, views.Names, views.Diagrams)
	if err != nil {
		c.logger.WarnContext(ctx, "cannot draw every diagram", "err", err)
	}
	files, err := views.Files(pictures)
	if err != nil {
		c.logger.ErrorContext(ctx, "cannot build the views", "err", err)
		return exitFailure
	}
	if err = writeSpec(cfg.To, files); err != nil {
		c.logger.ErrorContext(ctx, "cannot write the views", "dir", cfg.To, "err", err)
		return exitFailure
	}
	_, _ = fmt.Fprintf(c.stdout,
		"export: access matrix of %d row(s) by %d column(s) (%d caller kinds), "+
			"%d decision diagram(s), %d operation(s) not exported, written to %s\n",
		len(views.Rows), len(views.Columns), views.Personas, len(views.Names),
		len(views.Skipped), cfg.To)
	return exitOK
}

// skippedOperations are the operations m leaves out, each with the
// distinct reasons its rules' guards don't translate, sorted.
func skippedOperations(m *policy.Model, nodes []graph.Node) map[string][]string {
	operation := map[string]string{}
	for i := range nodes {
		operation[nodes[i].ID] = nodes[i].Extra[graph.ExtraOperation]
	}
	out := map[string][]string{}
	for _, s := range m.Skipped {
		op := operation[s.RuleID]
		if op == "" {
			continue
		}
		if !slices.Contains(out[op], s.Reason) {
			out[op] = append(out[op], s.Reason)
		}
	}
	for op := range out {
		slices.Sort(out[op])
	}
	return out
}

// stalePins returns the checkers whose recorded digest no longer matches
// their checker's code, each warned about; without --src the digests
// can't be checked, which is said once when any pin records one. A stale
// pin is still applied: it is a question for the person, not a failure.
func (c cli) stalePins(
	cfg *config.Config,
	facts []extract.Fact,
	checkers policy.Checkers,
) []string {
	var digested []string
	for name, k := range checkers {
		if k.Digest != "" {
			digested = append(digested, name)
		}
	}
	if len(digested) == 0 {
		return nil
	}
	if cfg.Src == "" {
		c.logger.Info(
			"pins record digests: pass --src to tell the stale ones",
			"pins",
			len(digested),
		)
		return nil
	}
	stale := policy.StalePins(checkers, c.checkDigests(cfg.Roots(), facts, digested))
	for _, name := range stale {
		c.logger.Warn("a pin was written for code that has changed since", "check", name)
	}
	return stale
}

// capabilityFacts reads the capability and role facts of the graph at dir:
// what a service names but another (admin) registers.
func capabilityFacts(dir string) ([]extract.Fact, error) {
	facts, err := extract.LoadFactsFile(filepath.Join(dir, ".codetospec", "facts.json"))
	if err != nil {
		return nil, err
	}
	var kept []extract.Fact
	for _, f := range facts {
		if f.Kind == extract.KindCapability ||
			(f.Kind == extract.KindRule && f.Attrs["role"] != "") {
			kept = append(kept, f)
		}
	}
	return kept, nil
}

// logRefuted reports, at debug level, the rules an export left out.
func (c cli) logRefuted(refuted []string) {
	for _, id := range refuted {
		c.logger.Debug("rule not exported: refuted by the crosscheck", "rule", id)
	}
}

// writeSpec writes files under dir, which must be new or empty.
func writeSpec(dir string, files map[string][]byte) error {
	entries, err := os.ReadDir(dir)
	switch {
	case err != nil && !errors.Is(err, fs.ErrNotExist):
		return err
	case len(entries) > 0:
		return fmt.Errorf("%w: %s", errExportTarget, dir)
	}
	for _, rel := range slices.Sorted(maps.Keys(files)) {
		if err = atomicfs.WriteFile(
			filepath.Join(dir, filepath.FromSlash(rel)),
			files[rel],
		); err != nil {
			return err
		}
	}
	return nil
}
