package render

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/StevenACoffman/codetospec/internal/graph"
	"github.com/StevenACoffman/codetospec/internal/locale"
)

// llmsTxt is the output llms.txt: an index of every node for an agent,
// with the preamble telling it which rules it may rely on.
func llmsTxt(nodes []graph.Node, text *locale.Text) string {
	var b strings.Builder
	b.WriteString("# codetospec knowledge graph\n\n")
	b.WriteString(text.LLMsPreamble + "\n")
	for _, nodeType := range []string{graph.TypeDomain, graph.TypeEntity, "endpoint", graph.TypeService} {
		var lines []string
		for i := range nodes {
			if n := &nodes[i]; n.Type == nodeType {
				lines = append(lines, llmsLine(n))
			}
		}
		if len(lines) > 0 {
			fmt.Fprintf(&b, "\n## %s\n\n%s\n", nodeType, strings.Join(lines, "\n"))
		}
	}
	b.WriteString(llmsRules(nodes))
	return b.String()
}

// llmsLine is a node's index line: its file and title, plus a domain's
// coverage.
func llmsLine(n *graph.Node) string {
	line := fmt.Sprintf("- %s : %s", NodeFile(n), n.Title)
	if coverage := n.Extra[graph.ExtraCoverage]; coverage != "" {
		line += " · " + coverage
	}
	return line
}

// llmsRules is the rule index grouped by domain, stated once per group:
// each line carries what an agent needs to decide whether to open the
// rule (its citations, crosscheck verdict and grounding).
func llmsRules(nodes []graph.Node) string {
	byDomain := map[string][]string{}
	for i := range nodes {
		n := &nodes[i]
		if n.Type != graph.TypeRule {
			continue
		}
		cites := make([]string, len(n.Sources))
		for j, s := range n.Sources {
			cites[j] = "`" + s.File() + ":" + s.Lines + "`"
		}
		line := fmt.Sprintf("- %s : %s", NodeFile(n), n.Title)
		var lineSb681 strings.Builder
		for _, part := range []string{
			strings.Join(cites, " "),
			n.Extra["crosscheck"],
			n.Extra["grounding"],
		} {
			if part != "" {
				lineSb681.WriteString(" · " + part)
			}
		}
		line += lineSb681.String()
		domain := ruleDomain(n.ID)
		byDomain[domain] = append(byDomain[domain], line)
	}
	if len(byDomain) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n## " + graph.TypeRule + "\n")
	for _, domain := range slices.Sorted(maps.Keys(byDomain)) {
		fmt.Fprintf(&b, "\n### %s\n\n%s\n", domain, strings.Join(byDomain[domain], "\n"))
	}
	return b.String()
}
