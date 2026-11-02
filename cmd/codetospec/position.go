package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/StevenACoffman/codetospec/internal/config"
	"github.com/StevenACoffman/codetospec/internal/domainmodel"
	"github.com/StevenACoffman/codetospec/internal/position"
)

// loadPositions builds the layer map of the positional nature from
// codetospec.yaml: nature_by_path as written, then the layers of
// <src>/anarch.json that nature_by_layer names, read against <src>/go.mod's
// module path. A mapping that does not resolve stops the run before any
// LLM call; with neither key, the zero Map (no positional nature).
func loadPositions(cfg *config.Config) (position.Map, error) {
	if len(cfg.NatureByLayer) == 0 {
		return position.Parse(cfg.NatureByPath)
	}
	if len(cfg.NatureByPath) > 0 {
		return position.Map{}, errors.New(
			"codetospec.yaml: nature_by_path and nature_by_layer exclude each other: " +
				"keep the layers in one place")
	}
	model, err := os.ReadFile(filepath.Join(cfg.Src, "anarch.json"))
	if err != nil {
		return position.Map{}, fmt.Errorf("nature_by_layer needs the target's anarch.json: %w", err)
	}
	goMod, err := os.ReadFile(filepath.Join(cfg.Src, "go.mod"))
	if err != nil {
		return position.Map{}, fmt.Errorf("nature_by_layer needs the target's go.mod: %w", err)
	}
	module := modulePath(goMod)
	if module == "" {
		return position.Map{}, errors.New("nature_by_layer: go.mod declares no module")
	}
	return position.FromAnarch(model, module, cfg.NatureByLayer)
}

// modulePath is the path a go.mod's module directive declares, "" when
// none does.
func modulePath(goMod []byte) string {
	lines := bufio.NewScanner(bytes.NewReader(goMod))
	for lines.Scan() {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(lines.Text()), "module"); ok {
			return strings.Trim(strings.TrimSpace(rest), `"`)
		}
	}
	return ""
}

// humanInputs is what people wrote about the code, beside it: domain
// models and the layers of the positional nature.
type humanInputs struct {
	models    *domainmodel.Set // nil without --domain-model
	positions position.Map
}

// loadHumanInputs reads and checks the human inputs. Each is checked first
// (a model linted, a layer mapping resolved): an invalid one fails the run
// before any LLM call.
func loadHumanInputs(cfg *config.Config) (humanInputs, error) {
	var in humanInputs
	if len(cfg.DomainModels) > 0 {
		models, err := domainmodel.Load(cfg.DomainModels)
		if err != nil {
			return humanInputs{}, err
		}
		in.models = models
	}
	positions, err := loadPositions(cfg)
	if err != nil {
		return humanInputs{}, err
	}
	in.positions = positions
	return in, nil
}
