// Package verify checks graph integrity against the source tree: unique ids,
// resolvable edges and citations, cited rules, and frontmatter round-trip.
package verify

import (
	"errors"
	"fmt"
	"io/fs"
	"reflect"

	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/graph"
	"github.com/StevenACoffman/codetospec/internal/render"
	"github.com/StevenACoffman/codetospec/internal/sitter"
)

// Violation is one failed integrity check.
type Violation struct {
	NodeID string
	Check  string
	Detail string
}

func (v Violation) String() string {
	return fmt.Sprintf("%s: %s: %s", v.NodeID, v.Check, v.Detail)
}

// Run performs all integrity checks on the graph. An empty result means the
// graph may be rendered.
func Run(nodes []graph.Node, roots extract.Roots) []Violation {
	var violations []Violation

	ids := make(map[string]bool, len(nodes))
	for _, n := range nodes {
		if ids[n.ID] {
			violations = append(
				violations,
				Violation{n.ID, "duplicate-id", "node id appears more than once"},
			)
		}
		ids[n.ID] = true
	}

	lineCounts := make(map[string]int)
	for _, n := range nodes {
		for _, e := range n.Edges {
			if !ids[e.To] {
				violations = append(violations, Violation{
					n.ID, "dangling-edge",
					fmt.Sprintf("edge %s points to missing node %s", e.Type, e.To),
				})
			}
		}
		if n.Type == graph.TypeRule && len(n.Sources) == 0 {
			violations = append(
				violations,
				Violation{n.ID, "rule-without-citation", "rule node has no source citation"},
			)
		}
		for _, s := range n.Sources {
			if detail := checkCitation(roots, s, lineCounts); detail != "" {
				violations = append(violations, Violation{n.ID, "unresolvable-citation", detail})
			}
		}
		if detail := checkRoundTrip(&n); detail != "" {
			violations = append(violations, Violation{n.ID, "frontmatter-round-trip", detail})
		}
	}
	return violations
}

// checkCitation verifies that ref's file exists under its root and that
// the line range fits inside it; lineCounts caches files by ref.File().
func checkCitation(roots extract.Roots, ref extract.Ref, lineCounts map[string]int) string {
	if !fs.ValidPath(ref.Path) {
		return fmt.Sprintf("path %q escapes the source root", ref.Path)
	}
	_, end, err := extract.ParseLines(ref.Lines)
	if err != nil {
		return err.Error()
	}
	count, ok := lineCounts[ref.File()]
	if !ok {
		data, readErr := roots.ReadFile(ref)
		switch {
		case errors.Is(readErr, extract.ErrUnknownRoot):
			return fmt.Sprintf("root %q of %s was not given (--ref-root)", ref.Root, ref.File())
		case readErr != nil:
			lineCounts[ref.File()] = -1
			return fmt.Sprintf("path %q not found under source root", ref.File())
		}
		count = sitter.CountLines(data)
		lineCounts[ref.File()] = count
	}
	if count < 0 {
		return fmt.Sprintf("path %q not found under source root", ref.File())
	}
	if end > count {
		return fmt.Sprintf("lines %s exceed file %s (%d lines)", ref.Lines, ref.File(), count)
	}
	return ""
}

// checkRoundTrip re-renders the node and re-parses its frontmatter,
// verifying that nothing is lost or altered.
func checkRoundTrip(n *graph.Node) string {
	parsed, err := render.ParseNode(render.Frontmatter(n))
	if err != nil {
		return fmt.Sprintf("regenerated frontmatter is not parsable: %v", err)
	}
	if parsed.ID != n.ID || parsed.Type != n.Type || parsed.Status != n.Status {
		return "id/type/status did not survive the round-trip"
	}
	if !reflect.DeepEqual(parsed.Sources, n.Sources) {
		return "sources did not survive the round-trip"
	}
	if !reflect.DeepEqual(parsed.Edges, n.Edges) {
		return "edges did not survive the round-trip"
	}
	for _, key := range []string{"ears", "acceptance", "nature", "origin", "confidence", "crosscheck", graph.ExtraExtractedBy, graph.ExtraInheritedFrom, "grounding", "grounding_missing", "digest"} {
		if parsed.Extra[key] != n.Extra[key] {
			return "extra field " + key + " did not survive the round-trip"
		}
	}
	return ""
}
