package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/StevenACoffman/codetospec/internal/config"
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

// readCheckers reads and parses the checkers file at path; none when path
// is "".
func readCheckers(path string) (policy.Checkers, error) {
	if path == "" {
		return policy.Checkers{}, nil
	}
	data, err := os.ReadFile(path) //nolint:gosec // a file the person names
	if err != nil {
		return nil, fmt.Errorf("read checkers: %w", err)
	}
	return policy.ParseCheckers(data)
}
