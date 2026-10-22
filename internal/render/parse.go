package render

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/graph"
	"gopkg.in/yaml.v3"
)

// frontmatterDoc mirrors the YAML frontmatter for parsing.
type frontmatterDoc struct {
	ID      string `yaml:"id"`
	Type    string `yaml:"type"`
	Status  string `yaml:"status"`
	Sources []struct {
		Path  string `yaml:"path"`
		Lines string `yaml:"lines"`
		Root  string `yaml:"root"`
	} `yaml:"sources"`
	Edges []struct {
		Type string `yaml:"type"`
		To   string `yaml:"to"`
	} `yaml:"edges"`
	Callees []struct {
		Path  string `yaml:"path"`
		Lines string `yaml:"lines"`
	} `yaml:"callees"`
}

// ParseNode parses one rendered node file back into a Node (frontmatter
// fields and title; the body is kept verbatim). Used by the round-trip
// check and the verify command.
func ParseNode(content string) (graph.Node, error) {
	var n graph.Node
	rest, ok := strings.CutPrefix(content, "---\n")
	if !ok {
		return n, errors.New("missing frontmatter opening")
	}
	front, body, ok := strings.Cut(rest, "\n---\n")
	if !ok {
		return n, errors.New("missing frontmatter closing")
	}
	var doc frontmatterDoc
	if err := yaml.Unmarshal([]byte(front), &doc); err != nil {
		return n, fmt.Errorf("parse frontmatter: %w", err)
	}
	var scalars map[string]yaml.Node
	if err := yaml.Unmarshal([]byte(front), &scalars); err != nil {
		return n, fmt.Errorf("parse frontmatter: %w", err)
	}
	n.ID = doc.ID
	n.Type = doc.Type
	n.Status = doc.Status
	for _, s := range doc.Sources {
		n.Sources = append(n.Sources, extract.Ref{Path: s.Path, Lines: s.Lines, Root: s.Root})
	}
	for _, c := range doc.Callees {
		n.Callees = append(n.Callees, extract.Ref{Path: c.Path, Lines: c.Lines})
	}
	for _, e := range doc.Edges {
		n.Edges = append(n.Edges, graph.Edge{Type: e.Type, To: e.To})
	}
	n.Extra = extraOf(scalars)
	for line := range strings.SplitSeq(body, "\n") {
		if title, isTitle := strings.CutPrefix(line, "# "); isTitle {
			n.Title = title
			break
		}
	}
	n.Body = body
	return n, nil
}

// extraOf is the node's Extra map from its frontmatter's scalar values,
// read as written (a confidence of 0.90 stays "0.90"); nil when empty.
// The keys are those the writer emits, so a new one is declared once.
func extraOf(scalars map[string]yaml.Node) map[string]string {
	extra := map[string]string{}
	for _, key := range FrontmatterKeys() {
		if v, ok := scalars[key]; ok && v.Kind == yaml.ScalarNode && v.Value != "" {
			extra[key] = v.Value
		}
	}
	if len(extra) == 0 {
		return nil
	}
	return extra
}

// FrontmatterKeys lists the Extra keys a node's frontmatter carries: what
// a node keeps when written and read back.
func FrontmatterKeys() []string {
	keys := []string{graph.ExtraCalleesDigest}
	for _, field := range frontmatterExtras() {
		keys = append(keys, field.key)
	}
	return keys
}

// LoadDir reads every node file under outDir/nodes for re-verification.
func LoadDir(outDir string) ([]graph.Node, error) {
	pattern := filepath.Join(outDir, "nodes", "*", "*.md")
	paths, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("glob %s: %w", pattern, err)
	}
	sort.Strings(paths)
	var nodes []graph.Node
	for _, path := range paths {
		data, readErr := os.ReadFile(filepath.Clean(path))
		if readErr != nil {
			return nil, fmt.Errorf("read node %s: %w", path, readErr)
		}
		n, parseErr := ParseNode(string(data))
		if parseErr != nil {
			return nil, fmt.Errorf("parse node %s: %w", path, parseErr)
		}
		nodes = append(nodes, n)
	}
	return nodes, nil
}
