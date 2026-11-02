// Package position derives a rule's nature from where its cited code
// lives, without an LLM: a rule whose citations all fall in an adapter,
// middleware or infrastructure package is technical by position, whatever
// its text says. The layers come from codetospec.yaml (nature_by_path) or
// from the target's anarch-lint model (anarch.json layers, nature_by_layer).
//
// The positional nature never overrides the LLM's: a disagreement is
// recorded (graph.ExtraNatureByPosition) and asked as a question, until
// measurement says which one wins.
package position

import (
	"cmp"
	"encoding/json"
	"fmt"
	"maps"
	"path"
	"slices"
	"strings"

	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/graph"
)

// The natures a layer can have, as the map and reduce write them.
const (
	Business     = "business"
	Presentation = "presentation"
	Technical    = "technical"
)

// Map is the nature of each directory pattern. The zero Map knows no
// layer: every rule's positional nature is unknown.
type Map struct {
	patterns []pattern // sorted, so the first match is deterministic
}

// pattern is one Go-style package pattern, as a slash path relative to
// --src: "internal/text" holds the files of that directory alone,
// "cmd/..." those of the whole tree under cmd.
type pattern struct {
	dir       string
	recursive bool
	nature    string
}

// Parse reads codetospec.yaml's nature_by_path: per nature, the package
// patterns ("internal/middleware", "cmd/...") of its layers.
//
// Ensures: an error names an unknown nature or an empty pattern.
func Parse(byNature map[string][]string) (Map, error) {
	var m Map
	for _, nature := range slices.Sorted(maps.Keys(byNature)) {
		if !isNature(nature) {
			return Map{}, fmt.Errorf("nature_by_path: unknown nature %q "+
				"(business, presentation or technical)", nature)
		}
		for _, p := range byNature[nature] {
			parsed, ok := parsePattern(p, nature)
			if !ok {
				return Map{}, fmt.Errorf("nature_by_path: %s: empty pattern", nature)
			}
			m.patterns = append(m.patterns, parsed)
		}
	}
	m.sort()
	return m, nil
}

// FromAnarch reads the layers of an anarch-lint model (anarch.json) and
// gives each the nature byLayer maps it to; a layer byLayer does not name
// is left out. Package patterns are import paths: modulePath (the target's
// go.mod module) is cut from each, a pattern outside the module dropped.
//
// Ensures: an error names an unknown nature, a layer byLayer names that
// the model does not have, or a model that does not parse.
func FromAnarch(model []byte, modulePath string, byLayer map[string]string) (Map, error) {
	var parsed struct {
		Layers []struct {
			Name     string   `json:"name"`
			Packages []string `json:"packages"`
		} `json:"layers"`
	}
	if err := json.Unmarshal(model, &parsed); err != nil {
		return Map{}, fmt.Errorf("anarch.json: %w", err)
	}
	var m Map
	found := map[string]bool{}
	for _, layer := range parsed.Layers {
		nature, mapped := byLayer[layer.Name]
		if !mapped {
			continue
		}
		if !isNature(nature) {
			return Map{}, fmt.Errorf("nature_by_layer: %s: unknown nature %q "+
				"(business, presentation or technical)", layer.Name, nature)
		}
		found[layer.Name] = true
		for _, pkg := range layer.Packages {
			rel, inModule := relativeTo(pkg, modulePath)
			if !inModule {
				continue
			}
			if parsed, ok := parsePattern(rel, nature); ok {
				m.patterns = append(m.patterns, parsed)
			}
		}
	}
	for _, name := range slices.Sorted(maps.Keys(byLayer)) {
		if !found[name] {
			return Map{}, fmt.Errorf("nature_by_layer: anarch.json has no layer %q", name)
		}
	}
	m.sort()
	return m, nil
}

// relativeTo cuts modulePath from an import path pattern: the module root
// itself is ".", "<module>/..." is "...".
func relativeTo(pkg, modulePath string) (string, bool) {
	if pkg == modulePath {
		return ".", true
	}
	rel, ok := strings.CutPrefix(pkg, modulePath+"/")
	return rel, ok
}

// parsePattern reads one package pattern; ok is false for an empty one.
func parsePattern(p, nature string) (pattern, bool) {
	p = strings.Trim(strings.TrimSpace(p), "/")
	dir, recursive := strings.CutSuffix(p, "...")
	dir = strings.TrimSuffix(dir, "/")
	if dir == "" && !recursive {
		return pattern{}, false
	}
	if dir == "" {
		dir = "." // "..." alone: the whole module
	}
	return pattern{dir: dir, recursive: recursive, nature: nature}, true
}

// sort orders the patterns most specific first (longest directory, exact
// before recursive), so a sub-layer overrides the tree it sits in.
func (m *Map) sort() {
	slices.SortFunc(m.patterns, func(a, b pattern) int {
		return cmp.Or(
			cmp.Compare(len(b.dir), len(a.dir)),
			compareBool(a.recursive, b.recursive),
			cmp.Compare(a.dir, b.dir),
			cmp.Compare(a.nature, b.nature),
		)
	})
}

// compareBool orders false before true.
func compareBool(a, b bool) int {
	switch {
	case a == b:
		return 0
	case a:
		return 1
	}
	return -1
}

func isNature(s string) bool {
	return s == Business || s == Presentation || s == Technical
}

// natureOf is the nature of the layer holding file, "" when none does.
func (m *Map) natureOf(file string) string {
	dir := path.Dir(file)
	for _, p := range m.patterns {
		switch {
		case p.dir == ".":
			if p.recursive || dir == "." {
				return p.nature
			}
		case dir == p.dir, p.recursive && strings.HasPrefix(dir, p.dir+"/"):
			return p.nature
		}
	}
	return ""
}

// Of is the nature of the layers sources fall in: the one nature every
// source under --src has, "" when a source is in no layer, the layers
// disagree, or no source is under --src (another root is not mapped).
func (m *Map) Of(sources []extract.Ref) string {
	nature := ""
	for _, src := range sources {
		if src.Root != "" {
			continue
		}
		n := m.natureOf(src.Path)
		if n == "" || (nature != "" && n != nature) {
			return ""
		}
		nature = n
	}
	return nature
}

// Annotate records, on each rule whose positional nature differs from the
// nature it was given, the positional one (graph.ExtraNatureByPosition).
//
// Requires: m from Parse or FromAnarch.
// Ensures:  nodes is not modified; a rule whose position says nothing, or
// agrees, gets no field; no rule's nature changes.
func Annotate(nodes []graph.Node, m *Map) []graph.Node {
	if len(m.patterns) == 0 {
		return nodes
	}
	out := slices.Clone(nodes)
	for i := range out {
		if out[i].Type != graph.TypeRule {
			continue
		}
		positional := m.Of(out[i].Sources)
		if positional == "" || positional == out[i].Extra["nature"] {
			continue
		}
		out[i].Extra = maps.Clone(out[i].Extra)
		if out[i].Extra == nil {
			out[i].Extra = map[string]string{}
		}
		out[i].Extra[graph.ExtraNatureByPosition] = positional
	}
	return out
}
