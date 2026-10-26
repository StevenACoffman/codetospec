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
	"github.com/StevenACoffman/codetospec/internal/quire"
	"github.com/StevenACoffman/codetospec/internal/render"
)

// errExportTarget means the export directory already holds files.
var errExportTarget = errors.New("export target is not empty")

// export writes the rules of the graph at cfg.Out as a Quire spec under
// cfg.To. It is a one-time bootstrap: once a team edits the exported
// documents, a second export would overwrite their work, so the target
// must be new or empty.
func (c cli) export(cfg *config.Config) int {
	nodes, err := render.LoadDir(cfg.Out)
	if err != nil {
		c.logger.Error("cannot load graph", "err", err)
		return exitFailure
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
	for _, id := range spec.Refuted {
		c.logger.Debug("rule not exported: refuted by the crosscheck", "rule", id)
	}
	_, _ = fmt.Fprintf(c.stdout,
		"export: %d functional requirement(s) written to %s, %d refuted rule(s) left out\n",
		len(spec.Files)-1, filepath.Join(cfg.To, quire.Directory), len(spec.Refuted))
	return exitOK
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
