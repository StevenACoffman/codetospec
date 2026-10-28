package main

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"

	"github.com/StevenACoffman/codetospec/internal/atomicfs"
	"github.com/StevenACoffman/codetospec/internal/config"
	"github.com/StevenACoffman/codetospec/internal/graph"
	"github.com/StevenACoffman/codetospec/internal/kiro"
	"github.com/StevenACoffman/codetospec/internal/quire"
	"github.com/StevenACoffman/codetospec/internal/render"
)

// errExportTarget means the export directory already holds files.
var errExportTarget = errors.New("export target is not empty")

// export writes the rules of the graph at cfg.Out as a spec under
// cfg.To, in cfg.Format: Quire FRs (the default) or Kiro specs. It is a
// one-time bootstrap: once a team edits the exported documents, a second
// export would overwrite their work, so the target must be new or empty.
func (c cli) export(cfg *config.Config) int {
	nodes, err := render.LoadDir(cfg.Out)
	if err != nil {
		c.logger.Error("cannot load graph", "err", err)
		return exitFailure
	}
	if cfg.Format == config.ExportKiro {
		return c.exportKiro(cfg, nodes)
	}
	spec, err := quire.Export(nodes)
	if err != nil {
		c.logger.Error("cannot export", "err", err)
		return exitFailure
	}
	if err = writeSpec(cfg.To, spec.Files); err != nil {
		c.logger.Error("cannot write the spec", "dir", cfg.To, "err", err)
		return exitFailure
	}
	c.logRefuted(spec.Refuted)
	_, _ = fmt.Fprintf(c.stdout,
		"export: %d functional requirement(s) written to %s, %d refuted rule(s) left out\n",
		len(spec.Files)-1, filepath.Join(cfg.To, quire.Directory), len(spec.Refuted))
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
		path := filepath.Join(dir, filepath.FromSlash(rel))
		if err = os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			return err
		}
		if err = atomicfs.WriteFile(path, files[rel]); err != nil {
			return err
		}
	}
	return nil
}
