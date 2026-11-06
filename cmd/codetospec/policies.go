package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/StevenACoffman/codetospec/internal/config"
	"github.com/StevenACoffman/codetospec/internal/drift"
	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/policy"
	"github.com/StevenACoffman/codetospec/internal/render"
)

// policies writes the policy inventory of the graph at cfg.Out to stdout:
// its rule nodes, its merged facts (.codetospec/facts.json, filtered by the
// run) and, with --registry, a person's registry. It never writes the
// graph directory.
func (c cli) policies(cfg *config.Config) int {
	nodes, err := render.LoadDir(cfg.Out)
	if err != nil {
		c.logger.Error("policies: cannot load graph", "err", err)
		return exitFailure
	}
	facts, err := extract.LoadFactsFile(filepath.Join(cfg.Out, ".codetospec", "facts.json"))
	if err != nil {
		c.logger.Error("policies: cannot load the run's facts", "err", err)
		return exitFailure
	}
	reg, err := readRegistry(cfg.Registry)
	if err != nil {
		c.logger.Error("policies: cannot load the registry", "err", err)
		return exitFailure
	}
	inv := policy.Build(facts, nodes, reg)
	if cfg.Src != "" {
		names := make([]string, len(inv.Checks))
		for i := range inv.Checks {
			names[i] = inv.Checks[i].Name
		}
		digests := c.checkDigests(cfg.Roots(), facts, names)
		for i := range inv.Checks {
			inv.Checks[i].Digest = digests[inv.Checks[i].Name]
		}
	}
	out := inv.Markdown()
	if cfg.Format == config.FormatJSON {
		if out, err = json.MarshalIndent(&inv, "", "  "); err != nil {
			c.logger.Error("policies: cannot encode", "err", err)
			return exitFailure
		}
		out = append(out, '\n')
	}
	if _, err = c.stdout.Write(out); err != nil {
		c.logger.Error("policies: cannot write", "err", err)
		return exitFailure
	}
	return exitOK
}

// checkDigests digests, through roots, the definition of each of names
// an access_check fact of facts cites (as drift hashes a span), by name: a
// check cited nowhere has none, and one whose code can't be read is
// reported and left out.
func (c cli) checkDigests(
	roots extract.Roots,
	facts []extract.Fact,
	names []string,
) map[string]string {
	digests := map[string]string{}
	for _, f := range facts {
		name := f.Attrs[extract.AttrName]
		if f.Kind != extract.KindAccessCheck || f.Source.Path == "" ||
			!slices.Contains(names, name) {
			continue
		}
		d, err := drift.Digest(roots, []extract.Ref{f.Source})
		if err != nil {
			c.logger.Warn("cannot digest a check's definition", "check", name, "err", err)
			continue
		}
		digests[name] = d
	}
	return digests
}

// readRegistry reads and parses the registry file at path; an empty
// registry when path is "".
func readRegistry(path string) (policy.Registry, error) {
	if path == "" {
		return policy.Registry{}, nil
	}
	data, err := os.ReadFile(path) //nolint:gosec // a file the person names
	if err != nil {
		return policy.Registry{}, fmt.Errorf("read registry: %w", err)
	}
	return policy.ParseRegistry(data)
}

// readCheckers reads, parses and merges the checkers files at paths (none
// for no path); a name two files define is an error.
func readCheckers(paths []string) (policy.Checkers, error) {
	files := make(map[string]policy.Checkers, len(paths))
	for _, path := range paths {
		data, err := os.ReadFile(path) //nolint:gosec // a file the person names
		if err != nil {
			return nil, fmt.Errorf("read checkers: %w", err)
		}
		cs, err := policy.ParseCheckers(data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		files[path] = cs
	}
	return policy.MergeCheckers(files)
}
