// Package render writes the graph to disk: markdown nodes with YAML
// frontmatter, graph.json, README.md and llms.txt. Output is byte-for-byte
// deterministic for a given graph.
package render

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/StevenACoffman/codetospec/internal/atomicfs"
	"github.com/StevenACoffman/codetospec/internal/communities"
	"github.com/StevenACoffman/codetospec/internal/consistency"
	"github.com/StevenACoffman/codetospec/internal/domainmodel"
	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/graph"
	"github.com/StevenACoffman/codetospec/internal/locale"
)

// Meta is run-level information rendered into README.md.
type Meta struct {
	Coverage       graph.Coverage
	ChunksFailed   int
	ChunksTotal    int
	DomainsFailed  int
	DomainsTotal   int
	FilesNoGrammar []string
	FilesGenerated int // generated files, not mapped
	// CoverageGaps is the code of each domain that produced no rule, sorted
	// (see graph.AnnotateCoverage).
	CoverageGaps     []graph.CoverageGap
	ExtractorsFailed []string
	// Adversarial cross-check tally; only rendered when Crosschecked is true.
	Crosschecked          bool
	CrosscheckSupported   int
	CrosscheckPartial     int
	CrosscheckUnsupported int
	CrosscheckRepaired    int
	CrosscheckUnchecked   int // rules the review never judged
	CrosscheckMechanical  int // extractor rules, not sent to the reviewer
	// DomainSplits are the domains whose files fall into several import-graph
	// communities (see communities.Splits).
	DomainSplits []communities.Split
	// DomainGroups maps each domain split out of an ambiguous namespace to
	// that namespace (cmd-seedroster → main): the README draws them together.
	DomainGroups map[string]string
	// Near-duplicate rule pairs (deterministic consistency check).
	DuplicatePairs []consistency.DuplicatePair
	// RuleFactsRejected counts extractor rule facts dropped as malformed.
	RuleFactsRejected int
	// SharedEntities are the entities touched by rules of several domains
	// (see graph.SharedEntities).
	SharedEntities []graph.SharedEntity
	// ModelFiles are the modelith model files written under models/
	// (--write-domain-model), and ModelUndefined, per file, the entities
	// whose definition is still a placeholder.
	ModelFiles     []string
	ModelUndefined map[string][]string
	// DomainModel compares the graph with human modelith models; nil when
	// none was given (--domain-model).
	DomainModel *domainmodel.Report
	// Lang is the --lang code selecting the output text (see locale).
	Lang string
}

// Write renders the whole graph into outDir: nodes/, graph.json, README.md
// and llms.txt. nodes/ is built aside and swapped in, and each top-level
// file is written by rename, so an interrupted render leaves the previous
// output readable, never half of it; .codetospec/ is never touched.
func Write(outDir string, nodes []graph.Node, meta *Meta) error {
	text := locale.For(meta.Lang)
	err := atomicfs.ReplaceDir(filepath.Join(outDir, "nodes"), func(staging string) error {
		return writeNodes(staging, nodes, text)
	})
	if err != nil {
		return err
	}
	if err = writeGraphJSON(outDir, nodes); err != nil {
		return err
	}
	if err = atomicfs.WriteFile(
		filepath.Join(outDir, "README.md"),
		[]byte(readme(nodes, meta, text)),
	); err != nil {
		return fmt.Errorf("write README.md: %w", err)
	}
	if err = atomicfs.WriteFile(
		filepath.Join(outDir, "llms.txt"),
		[]byte(llmsTxt(nodes, text)),
	); err != nil {
		return fmt.Errorf("write llms.txt: %w", err)
	}
	return nil
}

// writeNodes writes one markdown file per node under nodesDir, the
// directory NodeFile's "nodes/" prefix names.
func writeNodes(nodesDir string, nodes []graph.Node, text *locale.Text) error {
	for _, sub := range []string{"domains", "entities", "endpoints", "rules", "services"} {
		if err := os.MkdirAll(filepath.Join(nodesDir, sub), 0o750); err != nil {
			return fmt.Errorf("create nodes dir: %w", err)
		}
	}
	for i := range nodes {
		n := &nodes[i]
		rel := strings.TrimPrefix(NodeFile(n), "nodes/")
		path := filepath.Join(nodesDir, filepath.FromSlash(rel))
		if err := os.WriteFile(path, []byte(NodeMarkdown(n, text)), 0o600); err != nil {
			return fmt.Errorf("write node %s: %w", n.ID, err)
		}
	}
	return nil
}

// NodeFile returns the path of a node file relative to the output dir.
func NodeFile(n *graph.Node) string {
	switch n.Type {
	case "domain":
		return "nodes/domains/" + strings.TrimPrefix(n.ID, "domain.") + ".md"
	case graph.TypeEntity:
		return "nodes/entities/" + strings.TrimPrefix(n.ID, "entity.") + ".md"
	case "endpoint":
		return "nodes/endpoints/" + strings.TrimPrefix(n.ID, "endpoint.") + ".md"
	case graph.TypeService:
		return "nodes/services/" + strings.TrimPrefix(n.ID, graph.TypeService+".") + ".md"
	default:
		return "nodes/rules/" + strings.TrimPrefix(n.ID, "rule.") + ".md"
	}
}

// extraField is one optional frontmatter key read from Node.Extra; quoted
// values are free text that may hold YAML-significant characters.
type extraField struct {
	key    string
	quoted bool
}

// frontmatterExtras lists the optional frontmatter keys in output order.
// A key is written only when its value is set, so adding one here changes
// no existing node file.
func frontmatterExtras() []extraField {
	return []extraField{
		{key: "ears"},
		{key: "acceptance"},
		{key: "nature"},
		{key: "origin"},
		{key: "confidence"},
		{key: "crosscheck"},
		{key: graph.ExtraExtractedBy},
		{key: graph.ExtraInheritedFrom},
		{key: "grounding"},
		{key: "grounding_missing", quoted: true},
		{key: graph.ExtraGroundingWidened, quoted: true},
		{key: "digest"},
		{key: graph.ExtraCoverage},
		{key: graph.ExtraCoverageGaps, quoted: true},
	}
}

// writeCallees writes a rule's callees and their digest: the code drift
// re-hashes to flag the rule as affected.
func writeCallees(b *strings.Builder, n *graph.Node) {
	if len(n.Callees) == 0 {
		return
	}
	b.WriteString("callees:\n")
	for _, c := range n.Callees {
		fmt.Fprintf(b, "  - path: %s\n    lines: %q\n", c.Path, c.Lines)
	}
	fmt.Fprintf(b, "%s: %s\n", graph.ExtraCalleesDigest, n.Extra[graph.ExtraCalleesDigest])
}

// Frontmatter renders the exact YAML frontmatter of a node.
func Frontmatter(n *graph.Node) string {
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
			if s.Root != "" {
				fmt.Fprintf(&b, "    root: %s\n", s.Root)
			}
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
	writeCallees(&b, n)
	for _, field := range frontmatterExtras() {
		v := n.Extra[field.key]
		switch {
		case v == "":
		case field.quoted:
			fmt.Fprintf(&b, "%s: %q\n", field.key, v)
		default:
			fmt.Fprintf(&b, "%s: %s\n", field.key, v)
		}
	}
	b.WriteString("---\n")
	return b.String()
}

// NodeMarkdown renders one full node file: frontmatter, title, body,
// sources line and inline links for every edge, labelled in text's language.
func NodeMarkdown(n *graph.Node, text *locale.Text) string {
	var b strings.Builder
	b.WriteString(Frontmatter(n))
	b.WriteString("\n# " + n.Title + "\n")
	if n.Body != "" {
		b.WriteString("\n" + n.Body + "\n")
	}
	if len(n.Sources) > 0 {
		parts := make([]string, len(n.Sources))
		for i, s := range n.Sources {
			parts[i] = fmt.Sprintf("`%s:%s`", s.File(), s.Lines)
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
	case strings.HasPrefix(e.To, graph.TypeService+"."):
		slug := strings.TrimPrefix(e.To, graph.TypeService+".")
		return fmt.Sprintf("[%s](../services/%s.md)", fmt.Sprintf(text.ServiceTitleFmt, slug), slug)
	default:
		return e.To
	}
}

// graphJSONNode is one entry of graph.json.
type graphJSONNode struct {
	Type     string       `json:"type"`
	File     string       `json:"file"`
	Coverage string       `json:"coverage,omitempty"` // domains only
	Edges    []graph.Edge `json:"edges"`
}

func writeGraphJSON(outDir string, nodes []graph.Node) error {
	entries := make(map[string]graphJSONNode, len(nodes))
	for _, n := range nodes {
		edges := n.Edges
		if edges == nil {
			edges = []graph.Edge{}
		}
		entries[n.ID] = graphJSONNode{
			Type:     n.Type,
			File:     NodeFile(&n),
			Coverage: n.Extra[graph.ExtraCoverage],
			Edges:    edges,
		}
	}
	data, err := json.MarshalIndent(map[string]any{"nodes": entries}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode graph.json: %w", err)
	}
	if err = atomicfs.WriteFile(
		filepath.Join(outDir, "graph.json"),
		append(data, '\n'),
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

// percentScale turns a ratio in [0,1] into a percentage.
const percentScale = 100

func percent(part, total int) string {
	if total == 0 {
		return "n/a"
	}
	return strconv.Itoa(part*percentScale/total) + "%"
}

// readmeCounts opens the README: title, node counts, natures and origins.
func readmeCounts(nodes []graph.Node, text *locale.Text) string {
	counts := countByType(nodes)
	var b strings.Builder
	b.WriteString(text.Title + "\n\n" + text.Intro + "\n\n")
	fmt.Fprintf(&b, text.DomainsFmt+"\n", counts[graph.TypeDomain])
	fmt.Fprintf(&b, text.EntitiesFmt+"\n", counts[graph.TypeEntity])
	fmt.Fprintf(&b, text.EndpointsFmt+"\n", counts["endpoint"])
	fmt.Fprintf(&b, text.RulesFmt+"\n", counts[graph.TypeRule])
	if n := counts[graph.TypeService]; n > 0 {
		fmt.Fprintf(&b, text.ServicesFmt+"\n", n)
	}
	byNature := countByExtra(nodes, "nature")
	if total := byNature["business"] + byNature["presentation"] + byNature["technical"]; total > 0 {
		fmt.Fprintf(&b, text.NatureFmt+"\n",
			byNature["business"], byNature["presentation"], byNature["technical"])
	}
	byOrigin := countByExtra(nodes, "origin")
	if byOrigin["explicit"]+byOrigin["implicit"] > 0 {
		fmt.Fprintf(&b, text.OriginFmt+"\n", byOrigin["explicit"], byOrigin["implicit"])
	}
	return b.String()
}

// readmeCoverage is the README coverage section: what the graph covers,
// what failed or was left out, and the crosscheck tally.
func readmeCoverage(meta *Meta, text *locale.Text) string {
	var b strings.Builder
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
	if meta.FilesGenerated > 0 {
		fmt.Fprintf(&b, text.GeneratedFmt+"\n", meta.FilesGenerated)
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
	return b.String()
}

// readme renders the output README: counts, coverage, the checks for
// review, the domain-model sections and the domain map.
func readme(nodes []graph.Node, meta *Meta, text *locale.Text) string {
	var b strings.Builder
	b.WriteString(readmeCounts(nodes, text))
	b.WriteString(readmeCoverage(meta, text))
	b.WriteString(groundingSummary(nodes, text))
	b.WriteString(ungroundedSection(nodes, text))
	b.WriteString(gapsSection(meta.CoverageGaps, text))
	b.WriteString(splitSection(meta.DomainSplits, text))
	b.WriteString(sharedSection(meta.SharedEntities, text))

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
			fmt.Fprintf(&b, "- `%s` ↔ `%s` (%.0f%%)\n", p.A, p.B, p.Similarity*percentScale)
		}
		if len(meta.DuplicatePairs) > len(shown) {
			fmt.Fprintf(&b, text.AndMoreFmt+"\n", len(meta.DuplicatePairs)-len(shown))
		}
	}

	b.WriteString(modelSection(meta.DomainModel, text))
	b.WriteString(modelFilesSection(meta.ModelFiles, meta.ModelUndefined, text))

	b.WriteString("\n" + text.DomainsHeading + "\n\n")
	b.WriteString(domainDiagram(nodes, meta.DomainGroups))
	return b.String()
}

// domainDiagram draws the domain map as mermaid. Domains split out of one
// namespace (groups: domain → namespace, e.g. every cmd-<tool> out of Go's
// "main") share a subgraph, and their dependencies are drawn once per
// group, so a tree of commands does not bury the map in arrows.
func domainDiagram(nodes []graph.Node, groups map[string]string) string {
	dm := newDomainMap(nodes, groups)
	var b strings.Builder
	b.WriteString("```mermaid\ngraph TD\n")
	dm.writeNodes(&b, nodes)
	dm.writeArrows(&b, nodes)
	b.WriteString("```\n")
	return b.String()
}

// domainMap knows which domains are drawn together.
type domainMap struct {
	groups  map[string]string   // domain → namespace
	members map[string][]string // namespace → its domains, in node order
}

func newDomainMap(nodes []graph.Node, groups map[string]string) domainMap {
	members := make(map[string][]string)
	for _, n := range nodes {
		slug := strings.TrimPrefix(n.ID, "domain.")
		if ns := groups[slug]; n.Type == graph.TypeDomain && ns != "" {
			members[ns] = append(members[ns], slug)
		}
	}
	return domainMap{groups: groups, members: members}
}

// groupOf is the namespace whose subgraph holds a domain; "" when the
// domain is drawn alone (no group, or a group of one).
func (dm domainMap) groupOf(slug string) string {
	if ns := dm.groups[slug]; len(dm.members[ns]) > 1 {
		return ns
	}
	return ""
}

// writeNodes writes each domain, a group's members inside its subgraph.
func (dm domainMap) writeNodes(b *strings.Builder, nodes []graph.Node) {
	drawn := make(map[string]bool) // subgraphs written
	for _, n := range nodes {
		if n.Type != graph.TypeDomain {
			continue
		}
		slug := strings.TrimPrefix(n.ID, "domain.")
		switch ns := dm.groupOf(slug); {
		case ns == "":
			fmt.Fprintf(b, "  %s[\"%s\"]\n", mermaidID(slug), slug)
		case !drawn[ns]:
			drawn[ns] = true
			fmt.Fprintf(b, "  subgraph %s[\"%s\"]\n", groupID(ns), ns)
			for _, m := range dm.members[ns] {
				fmt.Fprintf(b, "    %s[\"%s\"]\n", mermaidID(m), m)
			}
			b.WriteString("  end\n")
		}
	}
}

// writeArrows writes the depends_on edges between domains (external
// services are listed as nodes of their own), a group's once from its
// subgraph, none within a group.
func (dm domainMap) writeArrows(b *strings.Builder, nodes []graph.Node) {
	seen := make(map[string]bool)
	for _, n := range nodes {
		if n.Type != graph.TypeDomain {
			continue
		}
		slug := strings.TrimPrefix(n.ID, "domain.")
		ns := dm.groupOf(slug)
		from := mermaidID(slug)
		if ns != "" {
			from = groupID(ns)
		}
		for _, e := range n.Edges {
			to := strings.TrimPrefix(e.To, "domain.")
			if e.Type != graph.EdgeDependsOn || to == e.To || ns != "" && dm.groupOf(to) == ns {
				continue
			}
			if arrow := fmt.Sprintf("  %s --> %s\n", from, mermaidID(to)); !seen[arrow] {
				seen[arrow] = true
				b.WriteString(arrow)
			}
		}
	}
}

// groupID is the mermaid id of a namespace's subgraph, apart from domain ids.
func groupID(namespace string) string {
	return "group_" + mermaidID(extract.Slugify(namespace))
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

// gapsSection lists the code that produced no rule, by domain: where a
// missing rule proves nothing.
func gapsSection(gaps []graph.CoverageGap, text *locale.Text) string {
	if len(gaps) == 0 {
		return ""
	}
	domains := map[string]bool{}
	for _, g := range gaps {
		domains[g.Domain] = true
	}
	var b strings.Builder
	b.WriteString("\n" + text.CoverageGapsHeading + "\n\n")
	fmt.Fprintf(&b, text.GapsIntroFmt+"\n\n", len(gaps), len(domains))
	for _, g := range gaps[:min(len(gaps), maxListed)] {
		fmt.Fprintf(&b, "- `%s` : %s\n", g.Domain, g.String())
	}
	if len(gaps) > maxListed {
		fmt.Fprintf(&b, text.AndMoreFmt+"\n", len(gaps)-maxListed)
	}
	return b.String()
}

// mermaidID makes a slug safe as a Mermaid node identifier.
func mermaidID(slug string) string {
	return strings.ReplaceAll(slug, "-", "_")
}

// ruleDomain extracts the domain segment of a rule id ("rule.<domain>.<slug>").
func ruleDomain(ruleID string) string {
	_, rest, found := strings.Cut(ruleID, ".")
	if !found {
		return ""
	}
	domain, _, _ := strings.Cut(rest, ".")
	return domain
}

// splitSection lists the domains whose files fall into several code
// communities; "" when there are none.

// modelListing is what the README says of a domain-model comparison: the
// counts, and each kind of gap as Markdown list items.
type modelListing struct {
	found, invariants, stated                     int
	missing, ambiguous, stale                     []string
	unmatchedInvariants, unstatedRules, unmodeled []string
}

// listModel turns a comparison into README list items.
func listModel(r *domainmodel.Report, text *locale.Text) modelListing {
	var l modelListing
	for _, e := range r.Entities {
		switch {
		case e.Code.ID != "":
			l.found++
		case len(e.Ambiguous) > 0:
			ids := make([]string, 0, len(e.Ambiguous))
			for _, c := range e.Ambiguous {
				ids = append(ids, "`"+c.ID+"`")
			}
			l.ambiguous = append(
				l.ambiguous,
				fmt.Sprintf("`%s`: %s", e.Name, strings.Join(ids, ", ")),
			)
		default:
			l.missing = append(l.missing, "`"+e.Name+"`")
		}
		l.addInvariants(e.Invariants, text)
		for _, id := range e.Unstated {
			l.unstatedRules = append(l.unstatedRules, fmt.Sprintf("`%s` (`%s`)", id, e.Name))
		}
	}
	l.addInvariants(r.Invariants, text)
	for _, c := range r.Unmodeled {
		l.unmodeled = append(l.unmodeled, fmt.Sprintf("`%s` (`%s`)", c.Title, c.ID))
	}
	for _, m := range r.StaleMappings {
		l.stale = append(l.stale, "`"+m+"`")
	}
	return l
}

// addInvariants counts invariants and lists those no rule states, with the
// closest rule as a lead.
func (l *modelListing) addInvariants(list []domainmodel.Invariant, text *locale.Text) {
	for _, inv := range list {
		l.invariants++
		if inv.Rule != "" {
			l.stated++
			continue
		}
		item := fmt.Sprintf("`%s` · `%s`", inv.Owner, inv.ID)
		if inv.Closest != "" {
			item += " (" + fmt.Sprintf(text.ModelClosestFmt, inv.Closest,
				int(math.Round(inv.Similarity*percentScale))) + ")"
		}
		l.unmatchedInvariants = append(l.unmatchedInvariants, item)
	}
}

// modelSection reports the comparison with human domain models: summary
// counts, then each kind of gap as a question, capped at maxListed.
func modelSection(r *domainmodel.Report, text *locale.Text) string {
	if r == nil {
		return ""
	}
	l := listModel(r, text)
	var b strings.Builder
	b.WriteString("\n" + text.ModelHeading + "\n\n")
	fmt.Fprintf(&b, text.ModelIntroFmt+"\n\n", len(r.Models), r.Warnings,
		int(domainmodel.DefaultThreshold*percentScale))
	fmt.Fprintf(&b, text.ModelEntitiesFmt+"\n", l.found, len(r.Entities))
	fmt.Fprintf(&b, text.ModelInvariantsFmt+"\n", l.stated, l.invariants)
	for _, list := range []struct {
		intro string
		items []string
	}{
		{text.ModelMissingFmt, l.missing},
		{text.ModelAmbiguousFmt, l.ambiguous},
		{text.ModelStaleMappingsFmt, l.stale},
		{text.ModelUnmatchedFmt, l.unmatchedInvariants},
		{text.ModelUnstatedRulesFmt, l.unstatedRules},
		{text.ModelUnmodeledFmt, l.unmodeled},
	} {
		b.WriteString(itemList(list.intro, list.items, text))
	}
	return b.String()
}

// modelFilesSection lists the model files written and the entities still
// awaiting a definition.
func modelFilesSection(files []string, undefined map[string][]string, text *locale.Text) string {
	if len(files) == 0 {
		return ""
	}
	listed := make([]string, len(files))
	var pending []string
	for i, file := range files {
		listed[i] = "`" + file + "`"
		for _, entity := range undefined[file] {
			pending = append(pending, fmt.Sprintf("`%s` (`%s`)", entity, file))
		}
	}
	return "\n" + text.ModelFilesHeading + "\n" +
		itemList(text.ModelFilesIntroFmt, listed, text) +
		itemList(text.ModelUndefinedFmt, pending, text)
}

// itemList writes a counted intro and its items, capped at maxListed; ""
// when there are none.
func itemList(introFmt string, items []string, text *locale.Text) string {
	if len(items) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n")
	fmt.Fprintf(&b, introFmt+"\n\n", len(items))
	shown := items[:min(len(items), maxListed)]
	for _, item := range shown {
		b.WriteString("- " + item + "\n")
	}
	if len(items) > len(shown) {
		fmt.Fprintf(&b, text.AndMoreFmt+"\n", len(items)-len(shown))
	}
	return b.String()
}

// sharedSection lists the entities several domains touch, with those
// domains.
func sharedSection(shared []graph.SharedEntity, text *locale.Text) string {
	if len(shared) == 0 {
		return ""
	}
	items := make([]string, 0, len(shared))
	for _, e := range shared {
		items = append(items, fmt.Sprintf("`%s` (`%s`): `%s`",
			e.Title, e.ID, strings.Join(e.Domains, "`, `")))
	}
	return "\n" + text.SharedHeading + "\n" + itemList(text.SharedIntroFmt, items, text)
}

func splitSection(splits []communities.Split, text *locale.Text) string {
	if len(splits) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n" + text.SplitsHeading + "\n\n")
	fmt.Fprintf(&b, text.SplitsIntroFmt+"\n\n", len(splits))
	shown := splits[:min(len(splits), maxListed)]
	for _, s := range shown {
		fmt.Fprintf(&b, "- `%s`: "+text.SplitFmt+"\n", s.Domain, s.Files, s.Communities)
	}
	if len(splits) > len(shown) {
		fmt.Fprintf(&b, text.AndMoreFmt+"\n", len(splits)-len(shown))
	}
	return b.String()
}
