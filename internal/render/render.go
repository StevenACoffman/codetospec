// Package render writes the graph to disk: markdown nodes with YAML
// frontmatter, graph.json, README.md and llms.txt. Output is byte-for-byte
// deterministic for a given graph.
package render

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"codetospec/internal/consistency"
	"codetospec/internal/extract"
	"codetospec/internal/graph"
	"codetospec/internal/locale"
	"gopkg.in/yaml.v3"
)

// Meta is run-level information rendered into README.md.
type Meta struct {
	Coverage         graph.Coverage
	ChunksFailed     int
	ChunksTotal      int
	DomainsFailed    int
	DomainsTotal     int
	FilesNoGrammar   []string
	ExtractorsFailed []string
	// Adversarial cross-check tally; only rendered when Crosschecked is true.
	Crosschecked          bool
	CrosscheckSupported   int
	CrosscheckPartial     int
	CrosscheckUnsupported int
	CrosscheckRepaired    int
	CrosscheckUnchecked   int // rules the review never judged
	CrosscheckMechanical  int // extractor rules, not sent to the reviewer
	// Near-duplicate rule pairs (deterministic consistency check).
	DuplicatePairs []consistency.DuplicatePair
	// RuleFactsRejected counts extractor rule facts dropped as malformed.
	RuleFactsRejected int
	// Lang is the --lang code selecting the output text (see locale).
	Lang string
}

// Write renders the whole graph into outDir: nodes/, graph.json, README.md
// and llms.txt. nodes/ is emptied then rewritten; .codetospec/ is never
// touched.
func Write(outDir string, nodes []graph.Node, meta Meta) error {
	text := locale.For(meta.Lang)
	nodesDir := filepath.Join(outDir, "nodes")
	if err := os.RemoveAll(nodesDir); err != nil {
		return fmt.Errorf("clear nodes dir: %w", err)
	}
	for _, sub := range []string{"domains", "entities", "endpoints", "rules"} {
		if err := os.MkdirAll(filepath.Join(nodesDir, sub), 0o755); err != nil {
			return fmt.Errorf("create nodes dir: %w", err)
		}
	}
	for _, n := range nodes {
		path := filepath.Join(outDir, filepath.FromSlash(NodeFile(n)))
		if err := os.WriteFile(path, []byte(NodeMarkdown(n, text)), 0o644); err != nil {
			return fmt.Errorf("write node %s: %w", n.ID, err)
		}
	}
	if err := writeGraphJSON(outDir, nodes); err != nil {
		return err
	}
	if err := os.WriteFile(
		filepath.Join(outDir, "README.md"),
		[]byte(readme(nodes, meta, text)),
		0o644,
	); err != nil {
		return fmt.Errorf("write README.md: %w", err)
	}
	if err := os.WriteFile(
		filepath.Join(outDir, "llms.txt"),
		[]byte(llmsTxt(nodes, text)),
		0o644,
	); err != nil {
		return fmt.Errorf("write llms.txt: %w", err)
	}
	return nil
}

// NodeFile returns the path of a node file relative to the output dir.
func NodeFile(n graph.Node) string {
	switch n.Type {
	case "domain":
		return "nodes/domains/" + strings.TrimPrefix(n.ID, "domain.") + ".md"
	case "entity":
		return "nodes/entities/" + strings.TrimPrefix(n.ID, "entity.") + ".md"
	case "endpoint":
		return "nodes/endpoints/" + strings.TrimPrefix(n.ID, "endpoint.") + ".md"
	default:
		return "nodes/rules/" + strings.TrimPrefix(n.ID, "rule.") + ".md"
	}
}

// Frontmatter renders the exact YAML frontmatter of a node.
func Frontmatter(n graph.Node) string {
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "id: %s\n", n.ID)
	fmt.Fprintf(&b, "type: %s\n", n.Type)
	fmt.Fprintf(&b, "status: %s\n", n.Status)
	if len(n.Sources) == 0 {
		b.WriteString("sources: []\n")
	} else {
		b.WriteString("sources:\n")
		for _, s := range n.Sources {
			fmt.Fprintf(&b, "  - path: %s\n    lines: %q\n", s.Path, s.Lines)
		}
	}
	if len(n.Edges) == 0 {
		b.WriteString("edges: []\n")
	} else {
		b.WriteString("edges:\n")
		for _, e := range n.Edges {
			fmt.Fprintf(&b, "  - {type: %s, to: %s}\n", e.Type, e.To)
		}
	}
	if v := n.Extra["ears"]; v != "" {
		fmt.Fprintf(&b, "ears: %s\n", v)
	}
	if v := n.Extra["acceptance"]; v != "" {
		fmt.Fprintf(&b, "acceptance: %s\n", v)
	}
	if v := n.Extra["nature"]; v != "" {
		fmt.Fprintf(&b, "nature: %s\n", v)
	}
	if v := n.Extra["origin"]; v != "" {
		fmt.Fprintf(&b, "origin: %s\n", v)
	}
	if v := n.Extra["confidence"]; v != "" {
		fmt.Fprintf(&b, "confidence: %s\n", v)
	}
	if v := n.Extra["crosscheck"]; v != "" {
		fmt.Fprintf(&b, "crosscheck: %s\n", v)
	}
	if v := n.Extra[graph.ExtraExtractedBy]; v != "" {
		fmt.Fprintf(&b, "%s: %s\n", graph.ExtraExtractedBy, v)
	}
	if v := n.Extra["grounding"]; v != "" {
		fmt.Fprintf(&b, "grounding: %s\n", v)
	}
	if v := n.Extra["grounding_missing"]; v != "" {
		fmt.Fprintf(&b, "grounding_missing: %q\n", v)
	}
	if v := n.Extra["digest"]; v != "" {
		fmt.Fprintf(&b, "digest: %s\n", v)
	}
	b.WriteString("---\n")
	return b.String()
}

// NodeMarkdown renders one full node file: frontmatter, title, body,
// sources line and inline links for every edge, labelled in text's language.
func NodeMarkdown(n graph.Node, text *locale.Text) string {
	var b strings.Builder
	b.WriteString(Frontmatter(n))
	b.WriteString("\n# " + n.Title + "\n")
	if n.Body != "" {
		b.WriteString("\n" + n.Body + "\n")
	}
	if len(n.Sources) > 0 {
		parts := make([]string, len(n.Sources))
		for i, s := range n.Sources {
			parts[i] = fmt.Sprintf("`%s:%s`", s.Path, s.Lines)
		}
		b.WriteString("\n" + text.SourcesLabel + strings.Join(parts, " · ") + "\n")
	}
	if len(n.Edges) > 0 {
		links := make([]string, len(n.Edges))
		for i, e := range n.Edges {
			links[i] = edgeLink(e, text)
		}
		b.WriteString("\n" + text.LinksLead + strings.Join(links, " · ") + "\n")
	}
	return b.String()
}

// edgeLink renders one inline markdown link with a correct relative path
// (every node lives one level under nodes/).
func edgeLink(e graph.Edge, text *locale.Text) string {
	switch {
	case strings.HasPrefix(e.To, "domain."):
		slug := strings.TrimPrefix(e.To, "domain.")
		label := fmt.Sprintf(text.DomainTitleFmt, slug)
		if e.Type == "depends_on" {
			label = e.To
		}
		return fmt.Sprintf("[%s](../domains/%s.md)", label, slug)
	case strings.HasPrefix(e.To, "entity."):
		return fmt.Sprintf("[%s](../entities/%s.md)", e.To, strings.TrimPrefix(e.To, "entity."))
	case strings.HasPrefix(e.To, "endpoint."):
		return fmt.Sprintf("[%s](../endpoints/%s.md)", e.To, strings.TrimPrefix(e.To, "endpoint."))
	case strings.HasPrefix(e.To, "rule."):
		return fmt.Sprintf("[%s](../rules/%s.md)", e.To, strings.TrimPrefix(e.To, "rule."))
	default:
		return e.To
	}
}

// graphJSONNode is one entry of graph.json.
type graphJSONNode struct {
	Type  string       `json:"type"`
	File  string       `json:"file"`
	Edges []graph.Edge `json:"edges"`
}

func writeGraphJSON(outDir string, nodes []graph.Node) error {
	entries := make(map[string]graphJSONNode, len(nodes))
	for _, n := range nodes {
		edges := n.Edges
		if edges == nil {
			edges = []graph.Edge{}
		}
		entries[n.ID] = graphJSONNode{Type: n.Type, File: NodeFile(n), Edges: edges}
	}
	data, err := json.MarshalIndent(map[string]any{"nodes": entries}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode graph.json: %w", err)
	}
	if err := os.WriteFile(
		filepath.Join(outDir, "graph.json"),
		append(data, '\n'),
		0o644,
	); err != nil {
		return fmt.Errorf("write graph.json: %w", err)
	}
	return nil
}

func countByType(nodes []graph.Node) map[string]int {
	counts := make(map[string]int)
	for _, n := range nodes {
		counts[n.Type]++
	}
	return counts
}

// countByExtra tallies rule nodes by the value of one Extra key.
func countByExtra(nodes []graph.Node, key string) map[string]int {
	counts := make(map[string]int)
	for _, n := range nodes {
		if n.Type == graph.TypeRule && n.Extra[key] != "" {
			counts[n.Extra[key]]++
		}
	}
	return counts
}

func percent(part, total int) string {
	if total == 0 {
		return "n/a"
	}
	return strconv.Itoa(part*100/total) + "%"
}

func readme(nodes []graph.Node, meta Meta, text *locale.Text) string {
	counts := countByType(nodes)
	var b strings.Builder
	b.WriteString(text.Title + "\n\n" + text.Intro + "\n\n")
	fmt.Fprintf(&b, text.DomainsFmt+"\n", counts["domain"])
	fmt.Fprintf(&b, text.EntitiesFmt+"\n", counts["entity"])
	fmt.Fprintf(&b, text.EndpointsFmt+"\n", counts["endpoint"])
	fmt.Fprintf(&b, text.RulesFmt+"\n", counts[graph.TypeRule])

	byNature := countByExtra(nodes, "nature")
	if total := byNature["business"] + byNature["presentation"] + byNature["technical"]; total > 0 {
		fmt.Fprintf(&b, text.NatureFmt+"\n",
			byNature["business"], byNature["presentation"], byNature["technical"])
	}
	byOrigin := countByExtra(nodes, "origin")
	if byOrigin["explicit"]+byOrigin["implicit"] > 0 {
		fmt.Fprintf(
			&b,
			text.OriginFmt+"\n",
			byOrigin["explicit"],
			byOrigin["implicit"],
		)
	}

	b.WriteString("\n" + text.CoverageHeading + "\n\n")
	fmt.Fprintf(&b, text.EndpointsCoveredFmt+"\n",
		meta.Coverage.EndpointsReferenced, meta.Coverage.EndpointsTotal,
		percent(meta.Coverage.EndpointsReferenced, meta.Coverage.EndpointsTotal))
	fmt.Fprintf(&b, text.EntitiesCoveredFmt+"\n",
		meta.Coverage.EntitiesTouched, meta.Coverage.EntitiesTotal,
		percent(meta.Coverage.EntitiesTouched, meta.Coverage.EntitiesTotal))
	fmt.Fprintf(&b, text.ChunksFailedFmt+"\n", meta.ChunksFailed, meta.ChunksTotal)
	fmt.Fprintf(&b, text.DomainsFailedFmt+"\n", meta.DomainsFailed, meta.DomainsTotal)
	if len(meta.FilesNoGrammar) > 0 {
		files := append([]string(nil), meta.FilesNoGrammar...)
		sort.Strings(files)
		fmt.Fprintf(
			&b,
			text.NoGrammarFmt+"\n",
			strings.Join(files, ", "),
		)
	}
	if len(meta.ExtractorsFailed) > 0 {
		names := append([]string(nil), meta.ExtractorsFailed...)
		sort.Strings(names)
		fmt.Fprintf(&b, text.ExtractorsFailedFmt+"\n", strings.Join(names, ", "))
	}
	if meta.Crosschecked {
		fmt.Fprintf(
			&b,
			text.CrosscheckFmt,
			meta.CrosscheckSupported,
			meta.CrosscheckPartial,
			meta.CrosscheckUnsupported,
		)
		if meta.CrosscheckRepaired > 0 {
			fmt.Fprintf(&b, text.RepairedFmt, meta.CrosscheckRepaired)
		}
		if meta.CrosscheckUnchecked > 0 {
			fmt.Fprintf(&b, text.UncheckedFmt, meta.CrosscheckUnchecked)
		}
		if meta.CrosscheckMechanical > 0 {
			fmt.Fprintf(&b, text.MechanicalFmt, meta.CrosscheckMechanical)
		}
		b.WriteString("\n")
	}
	if meta.RuleFactsRejected > 0 {
		fmt.Fprintf(&b, text.RejectedRulesFmt+"\n", meta.RuleFactsRejected)
	}
	b.WriteString(groundingSummary(nodes, text))
	b.WriteString(ungroundedSection(nodes, text))

	if len(meta.DuplicatePairs) > 0 {
		intra, cross := 0, 0
		for _, p := range meta.DuplicatePairs {
			if ruleDomain(p.A) == ruleDomain(p.B) {
				intra++
			} else {
				cross++
			}
		}
		b.WriteString("\n" + text.DuplicatesHeading + "\n\n")
		fmt.Fprintf(
			&b,
			text.DuplicatesIntroFmt+"\n\n",
			len(meta.DuplicatePairs),
			intra,
			cross,
		)
		shown := meta.DuplicatePairs[:min(len(meta.DuplicatePairs), maxListed)]
		for _, p := range shown {
			fmt.Fprintf(&b, "- `%s` ↔ `%s` (%.0f%%)\n", p.A, p.B, p.Similarity*100)
		}
		if len(meta.DuplicatePairs) > len(shown) {
			fmt.Fprintf(&b, text.AndMoreFmt+"\n", len(meta.DuplicatePairs)-len(shown))
		}
	}

	b.WriteString("\n" + text.DomainsHeading + "\n\n```mermaid\ngraph TD\n")
	for _, n := range nodes {
		if n.Type == "domain" {
			slug := strings.TrimPrefix(n.ID, "domain.")
			fmt.Fprintf(&b, "  %s[\"%s\"]\n", mermaidID(slug), slug)
		}
	}
	for _, n := range nodes {
		if n.Type != "domain" {
			continue
		}
		from := mermaidID(strings.TrimPrefix(n.ID, "domain."))
		for _, e := range n.Edges {
			if e.Type == "depends_on" {
				fmt.Fprintf(
					&b,
					"  %s --> %s\n",
					from,
					mermaidID(strings.TrimPrefix(e.To, "domain.")),
				)
			}
		}
	}
	b.WriteString("```\n")
	return b.String()
}

// maxListed caps how many entries a README review list shows.
const maxListed = 20

// groundingSummary is the README coverage line tallying the mechanical
// grounding check (see package grounding); empty when no rule was checked.
func groundingSummary(nodes []graph.Node, text *locale.Text) string {
	byStatus := countByExtra(nodes, "grounding")
	if len(byStatus) == 0 {
		return ""
	}
	return fmt.Sprintf(
		text.GroundingFmt+"\n",
		byStatus["grounded"],
		byStatus["ungrounded"],
		byStatus["unchecked"],
	)
}

// ungroundedSection lists the rules whose stated values are missing from
// their cited code, for review; empty when there are none.
func ungroundedSection(nodes []graph.Node, text *locale.Text) string {
	var listed []graph.Node
	for _, n := range nodes {
		if n.Type == graph.TypeRule && n.Extra["grounding"] == "ungrounded" {
			listed = append(listed, n)
		}
	}
	if len(listed) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n" + text.UngroundedHeading + "\n\n")
	fmt.Fprintf(&b, text.UngroundedIntroFmt+"\n\n", len(listed))
	for _, n := range listed[:min(len(listed), maxListed)] {
		fmt.Fprintf(&b, "- `%s` : %s\n", n.ID, n.Extra["grounding_missing"])
	}
	if len(listed) > maxListed {
		fmt.Fprintf(&b, text.AndMoreFmt+"\n", len(listed)-maxListed)
	}
	return b.String()
}

// mermaidID makes a slug safe as a Mermaid node identifier.
func mermaidID(slug string) string {
	return strings.ReplaceAll(slug, "-", "_")
}

// ruleDomain extracts the domain segment of a rule id ("rule.<domain>.<slug>").
func ruleDomain(ruleID string) string {
	parts := strings.SplitN(ruleID, ".", 3)
	if len(parts) >= 2 {
		return parts[1]
	}
	return ""
}

func llmsTxt(nodes []graph.Node, text *locale.Text) string {
	var b strings.Builder
	b.WriteString("# codetospec knowledge graph\n\n")
	b.WriteString(text.LLMsPreamble + "\n")
	for _, nodeType := range []string{"domain", "entity", "endpoint", "rule"} {
		var lines []string
		for _, n := range nodes {
			if n.Type == nodeType {
				lines = append(lines, fmt.Sprintf("- %s : %s", NodeFile(n), n.Title))
			}
		}
		if len(lines) == 0 {
			continue
		}
		fmt.Fprintf(&b, "\n## %s\n\n", nodeType)
		b.WriteString(strings.Join(lines, "\n") + "\n")
	}
	return b.String()
}

// frontmatterDoc mirrors the YAML frontmatter for parsing.
type frontmatterDoc struct {
	ID      string `yaml:"id"`
	Type    string `yaml:"type"`
	Status  string `yaml:"status"`
	Sources []struct {
		Path  string `yaml:"path"`
		Lines string `yaml:"lines"`
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
	Grounding  string   `yaml:"grounding"`
	Missing    string   `yaml:"grounding_missing"`
	Digest     string   `yaml:"digest"`
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
		n.Sources = append(n.Sources, extract.Ref{Path: s.Path, Lines: s.Lines})
	}
	for _, e := range doc.Edges {
		n.Edges = append(n.Edges, graph.Edge{Type: e.Type, To: e.To})
	}
	extra := map[string]string{}
	if doc.Ears != "" {
		extra["ears"] = doc.Ears
	}
	if doc.Acceptance != nil {
		extra["acceptance"] = strconv.Itoa(*doc.Acceptance)
	}
	if doc.Nature != "" {
		extra["nature"] = doc.Nature
	}
	if doc.Origin != "" {
		extra["origin"] = doc.Origin
	}
	if doc.Confidence != nil {
		extra["confidence"] = strconv.FormatFloat(*doc.Confidence, 'f', 2, 64)
	}
	if doc.Crosscheck != "" {
		extra["crosscheck"] = doc.Crosscheck
	}
	if doc.Extracted != "" {
		extra[graph.ExtraExtractedBy] = doc.Extracted
	}
	if doc.Grounding != "" {
		extra["grounding"] = doc.Grounding
	}
	if doc.Missing != "" {
		extra["grounding_missing"] = doc.Missing
	}
	if doc.Digest != "" {
		extra["digest"] = doc.Digest
	}
	if len(extra) > 0 {
		n.Extra = extra
	}
	for line := range strings.SplitSeq(body, "\n") {
		if title, ok := strings.CutPrefix(line, "# "); ok {
			n.Title = title
			break
		}
	}
	n.Body = body
	return n, nil
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
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read node %s: %w", path, err)
		}
		n, err := ParseNode(string(data))
		if err != nil {
			return nil, fmt.Errorf("parse node %s: %w", path, err)
		}
		nodes = append(nodes, n)
	}
	return nodes, nil
}
