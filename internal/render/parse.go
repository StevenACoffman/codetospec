package render

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
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
	Ears       string   `yaml:"ears"`
	Acceptance *int     `yaml:"acceptance"`
	Nature     string   `yaml:"nature"`
	Origin     string   `yaml:"origin"`
	Confidence *float64 `yaml:"confidence"`
	Crosscheck string   `yaml:"crosscheck"`
	Extracted  string   `yaml:"extracted_by"`
	Inherited  string   `yaml:"inherited_from"`
	Grounding  string   `yaml:"grounding"`
	Missing    string   `yaml:"grounding_missing"`
	Widened    string   `yaml:"grounding_widened"`
	Digest     string   `yaml:"digest"`
	Callees    []struct {
		Path  string `yaml:"path"`
		Lines string `yaml:"lines"`
	} `yaml:"callees"`
	CalleesDigest string `yaml:"callees_digest"`
	Coverage      string `yaml:"coverage"`
	CoverageGaps  string `yaml:"coverage_gaps"`
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
	n.Extra = doc.extra()
	for line := range strings.SplitSeq(body, "\n") {
		if title, isTitle := strings.CutPrefix(line, "# "); isTitle {
			n.Title = title
			break
		}
	}
	n.Body = body
	return n, nil
}

// extra is the node's Extra map from its frontmatter; nil when empty.
func (doc *frontmatterDoc) extra() map[string]string {
	extra := map[string]string{}
	set := func(key, value string) {
		if value != "" {
			extra[key] = value
		}
	}
	set("ears", doc.Ears)
	if doc.Acceptance != nil {
		extra["acceptance"] = strconv.Itoa(*doc.Acceptance)
	}
	set("nature", doc.Nature)
	set("origin", doc.Origin)
	if doc.Confidence != nil {
		extra["confidence"] = strconv.FormatFloat(*doc.Confidence, 'f', 2, 64)
	}
	set("crosscheck", doc.Crosscheck)
	set(graph.ExtraExtractedBy, doc.Extracted)
	set(graph.ExtraInheritedFrom, doc.Inherited)
	set("grounding", doc.Grounding)
	set("grounding_missing", doc.Missing)
	set(graph.ExtraGroundingWidened, doc.Widened)
	set("digest", doc.Digest)
	set(graph.ExtraCalleesDigest, doc.CalleesDigest)
	set(graph.ExtraCoverage, doc.Coverage)
	set(graph.ExtraCoverageGaps, doc.CoverageGaps)
	if len(extra) == 0 {
		return nil
	}
	return extra
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
