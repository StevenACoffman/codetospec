// Package graph assembles the spec knowledge graph from facts and reduced
// domain outputs, entirely deterministically.
package graph

import (
	"fmt"
	"maps"
	"slices"
	"sort"
	"strconv"
	"strings"

	"codetospec/internal/extract"
	"codetospec/internal/locale"
	"codetospec/internal/reducer"
)

// Node is one markdown file in the spec graph.
type Node struct {
	ID      string // "rule.billing.prorata-activation"
	Type    string // "domain" | "entity" | "endpoint" | "rule"
	Status  string // always "generated" in v0.1
	Sources []extract.Ref
	Edges   []Edge
	Title   string
	Body    string            // markdown body without frontmatter
	Extra   map[string]string // ears, acceptance (rules only)
}

// Edge links two nodes.
type Edge struct {
	Type string `json:"type"` // "belongs_to" | "touches" | "exposed_by" | "depends_on"
	To   string `json:"to"`
}

// RuleRequirement returns the EARS requirement of a rule body written in any
// supported language, falling back to the body's first line when the body
// has no requirement line.
func RuleRequirement(body string) string {
	texts := locale.All()
	for line := range strings.SplitSeq(body, "\n") {
		for _, text := range texts {
			if rest, ok := strings.CutPrefix(line, text.Requirement); ok {
				return rest
			}
		}
	}
	first, _, _ := strings.Cut(body, "\n")
	return first
}

// RuleCriteria returns the numbered acceptance criteria lines ("1. ...") of
// a rule body written in any supported language.
func RuleCriteria(body string) []string {
	texts := locale.All()
	var criteria []string
	inList := false
	for line := range strings.SplitSeq(body, "\n") {
		if !inList {
			inList = slices.ContainsFunc(texts, func(text *locale.Text) bool {
				return strings.HasPrefix(line, text.Criteria)
			})
			continue
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || trimmed[0] < '0' || trimmed[0] > '9' {
			break
		}
		criteria = append(criteria, trimmed)
	}
	return criteria
}

// EdgeBelongsTo links a rule to its domain.
const EdgeBelongsTo = "belongs_to"

// TypeRule is the Node.Type of a business rule.
const TypeRule = "rule"

// StatusGenerated is the only node status in v0.1.
const StatusGenerated = "generated"

// Build assembles the node set: entities from table facts, endpoints from
// route facts, rules and domains from the reduce outputs and from rule facts
// (which must have passed ValidateRuleFacts), and domain
// depends_on edges computed from cross-domain import facts (never by the LLM).
// Node bodies use the labels of the --lang code lang.
func Build(
	facts []extract.Fact,
	reduced []reducer.Output,
	resolver extract.DomainResolver,
	lang string,
) []Node {
	text := locale.For(lang)
	var nodes []Node
	nodes = append(nodes, entityNodes(facts, text)...)
	nodes = append(nodes, endpointNodes(facts, text)...)

	ruleNodes, domainNodes := ruleAndDomainNodes(reduced, facts, text)
	domainDeps := domainDependencies(facts, resolver, domainNodes)
	for i := range domainNodes {
		slug := strings.TrimPrefix(domainNodes[i].ID, "domain.")
		domainNodes[i].Edges = append(domainNodes[i].Edges, domainDeps[slug]...)
		sortEdges(domainNodes[i].Edges)
	}
	nodes = append(nodes, ruleNodes...)
	nodes = append(nodes, domainNodes...)

	sort.SliceStable(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	return nodes
}

// EntityID derives the node id for a table fact.
func EntityID(f extract.Fact) string {
	name := f.Attrs["name"]
	if name == "" {
		name = strings.TrimPrefix(f.ID, "table.")
	}
	return "entity." + extract.Slugify(name)
}

// EndpointID derives the node id for a route fact.
func EndpointID(f extract.Fact) string {
	verb := strings.ToLower(routeVerb(f))
	return "endpoint." + extract.Slugify(verb+" "+f.Attrs["path"])
}

func routeVerb(f extract.Fact) string {
	if v := f.Attrs["method"]; v != "" {
		return v
	}
	return f.Attrs["verb"]
}

func routeHandler(f extract.Fact) string {
	handler := f.Attrs["handler"]
	if handler == "" {
		handler = f.Attrs["controller"]
		if action := f.Attrs["action"]; action != "" && handler != "" {
			handler += "@" + action
		}
	}
	return handler
}

func entityNodes(facts []extract.Fact, text *locale.Text) []Node {
	var nodes []Node
	for _, f := range facts {
		if f.Kind != "table" {
			continue
		}
		name := f.Attrs["name"]
		if name == "" {
			name = strings.TrimPrefix(f.ID, "table.")
		}
		var body strings.Builder
		body.WriteString(text.Table + "`" + name + "`\n\n" + text.Columns + "\n")
		for _, col := range splitColumns(f.Attrs["columns"]) {
			body.WriteString("- `" + col + "`\n")
		}
		nodes = append(nodes, Node{
			ID:      EntityID(f),
			Type:    "entity",
			Status:  StatusGenerated,
			Sources: []extract.Ref{f.Source},
			Title:   name,
			Body:    strings.TrimRight(body.String(), "\n"),
		})
	}
	return nodes
}

func splitColumns(columns string) []string {
	var cols []string
	for c := range strings.SplitSeq(columns, ",") {
		if c = strings.TrimSpace(c); c != "" {
			cols = append(cols, c)
		}
	}
	return cols
}

func endpointNodes(facts []extract.Fact, text *locale.Text) []Node {
	var nodes []Node
	for _, f := range facts {
		if f.Kind != "route" {
			continue
		}
		verb := strings.ToUpper(routeVerb(f))
		path := f.Attrs["path"]
		body := text.Method + verb + "\n\n" + text.Path + "`" + path + "`"
		if handler := routeHandler(f); handler != "" {
			body += "\n\n" + text.Handler + "`" + handler + "`"
		}
		nodes = append(nodes, Node{
			ID:      EndpointID(f),
			Type:    "endpoint",
			Status:  StatusGenerated,
			Sources: []extract.Ref{f.Source},
			Title:   verb + " " + path,
			Body:    body,
		})
	}
	return nodes
}

// domainDraft collects one domain's summary and rule nodes before its
// domain node is written.
type domainDraft struct {
	summary string
	rules   []Node
}

// ruleAndDomainNodes builds rule nodes from the reduce outputs and from the
// mechanical rule facts, and one domain node per domain that has rules.
func ruleAndDomainNodes(
	reduced []reducer.Output,
	facts []extract.Fact,
	text *locale.Text,
) ([]Node, []Node) {
	drafts := make(map[string]*domainDraft)
	draft := func(domain string) *domainDraft {
		if drafts[domain] == nil {
			drafts[domain] = &domainDraft{}
		}
		return drafts[domain]
	}
	taken := make(map[string]bool)

	for _, out := range reduced {
		if out.Failed || len(out.Rules) == 0 {
			continue
		}
		d := draft(out.Domain)
		d.summary = out.DomainSummary
		for i := range out.Rules {
			n := reducedRuleNode(out.Domain, &out.Rules[i], text)
			taken[n.ID] = true
			d.rules = append(d.rules, n)
		}
	}
	for i := range facts {
		f := &facts[i]
		if f.Kind != extract.KindRule {
			continue
		}
		domain := f.Attrs[extract.AttrDomain]
		id := uniqueID("rule."+domain+"."+f.Attrs[extract.AttrSlug], taken)
		taken[id] = true
		draft(domain).rules = append(draft(domain).rules, mechanicalRuleNode(f, id, text))
	}

	var rules, domains []Node
	for _, domain := range slices.Sorted(maps.Keys(drafts)) {
		d := drafts[domain]
		sort.SliceStable(d.rules, func(i, j int) bool { return d.rules[i].ID < d.rules[j].ID })
		var ruleList strings.Builder
		for _, r := range d.rules {
			fmt.Fprintf(
				&ruleList,
				"- [%s](../rules/%s.md)\n",
				r.Title,
				strings.TrimPrefix(r.ID, "rule."),
			)
		}
		rules = append(rules, d.rules...)
		body := d.summary + "\n\n" + text.RulesHeading + "\n\n" + strings.TrimRight(
			ruleList.String(),
			"\n",
		)
		domains = append(domains, Node{
			ID:     "domain." + domain,
			Type:   "domain",
			Status: StatusGenerated,
			Title:  fmt.Sprintf(text.DomainTitleFmt, domain),
			Body:   strings.TrimSpace(body),
		})
	}
	return rules, domains
}

// reducedRuleNode turns one reduced rule of domain into a rule node.
func reducedRuleNode(domain string, rule *reducer.Rule, text *locale.Text) Node {
	edges := []Edge{{Type: EdgeBelongsTo, To: "domain." + domain}}
	for _, e := range dedupSorted(rule.Entities) {
		edges = append(edges, Edge{Type: "touches", To: e})
	}
	for _, e := range dedupSorted(rule.Endpoints) {
		edges = append(edges, Edge{Type: "exposed_by", To: e})
	}
	sortEdges(edges)

	sources := append([]extract.Ref(nil), rule.Citations...)
	sort.SliceStable(sources, func(i, j int) bool {
		if sources[i].Path != sources[j].Path {
			return sources[i].Path < sources[j].Path
		}
		return sources[i].Lines < sources[j].Lines
	})

	var body strings.Builder
	body.WriteString(text.Requirement + rule.Requirement)
	if rule.Rationale != "" {
		body.WriteString("\n\n" + text.Rationale + rule.Rationale)
	}
	if len(rule.AcceptanceCriteria) > 0 {
		body.WriteString("\n\n" + text.Criteria)
		for i, criterion := range rule.AcceptanceCriteria {
			fmt.Fprintf(&body, "\n%d. %s", i+1, criterion)
		}
	}

	return Node{
		ID:      fmt.Sprintf("rule.%s.%s", domain, rule.Slug),
		Type:    TypeRule,
		Status:  StatusGenerated,
		Sources: sources,
		Edges:   edges,
		Title:   rule.Title,
		Body:    body.String(),
		Extra: map[string]string{
			"ears":       rule.EarsKind,
			"acceptance": strconv.Itoa(len(rule.AcceptanceCriteria)),
			"nature":     rule.Nature,
			"origin":     rule.Origin,
			"confidence": strconv.FormatFloat(rule.Confidence, 'f', 2, 64),
		},
	}
}

// uniqueID returns id, or id-2, id-3… if taken already holds it.
func uniqueID(id string, taken map[string]bool) string {
	if !taken[id] {
		return id
	}
	for n := 2; ; n++ {
		if candidate := id + "-" + strconv.Itoa(n); !taken[candidate] {
			return candidate
		}
	}
}

// domainDependencies computes depends_on edges between existing domains from
// import facts whose source and target resolve to different domains.
func domainDependencies(
	facts []extract.Fact,
	resolver extract.DomainResolver,
	domains []Node,
) map[string][]Edge {
	existing := make(map[string]bool, len(domains))
	for _, d := range domains {
		existing[strings.TrimPrefix(d.ID, "domain.")] = true
	}

	type module struct {
		segments []string
		domain   string
	}
	var modules []module
	for _, f := range facts {
		if f.Kind != "module" {
			continue
		}
		name := f.Attrs["name"]
		if name == "" {
			continue
		}
		modules = append(modules, module{
			segments: extract.SplitQualified(name),
			domain:   resolver.Resolve(name, f.Source.Path),
		})
	}

	deps := make(map[string]map[string]bool)
	for _, f := range facts {
		if f.Kind != "import" {
			continue
		}
		from := resolver.Resolve(f.Attrs["namespace"], f.Source.Path)
		targetSegments := extract.SplitQualified(f.Attrs["target"])
		to, bestLen := "", 0
		for _, m := range modules {
			if len(m.segments) > bestLen && segmentsPrefix(m.segments, targetSegments) {
				to, bestLen = m.domain, len(m.segments)
			}
		}
		if to == "" {
			// Suffix fallback for languages where imports carry a path while
			// module facts carry the bare package name (Go: import
			// "app/internal/billing" vs package "billing"). Only applied
			// when it resolves to exactly one domain.
			suffixDomains := map[string]bool{}
			for _, m := range modules {
				if len(m.segments) > 0 && segmentsSuffix(m.segments, targetSegments) {
					suffixDomains[m.domain] = true
				}
			}
			if len(suffixDomains) == 1 {
				for domain := range suffixDomains {
					to = domain
				}
			}
		}
		if to == "" || to == from || !existing[from] || !existing[to] {
			continue
		}
		if deps[from] == nil {
			deps[from] = make(map[string]bool)
		}
		deps[from][to] = true
	}

	edges := make(map[string][]Edge, len(deps))
	for from, targets := range deps {
		var list []Edge
		for to := range targets {
			list = append(list, Edge{Type: "depends_on", To: "domain." + to})
		}
		sortEdges(list)
		edges[from] = list
	}
	return edges
}

// segmentsPrefix reports whether prefix is a segment-wise prefix of full.
func segmentsPrefix(prefix, full []string) bool {
	if len(prefix) > len(full) {
		return false
	}
	for i, s := range prefix {
		if full[i] != s {
			return false
		}
	}
	return true
}

// segmentsSuffix reports whether suffix is a segment-wise suffix of full.
func segmentsSuffix(suffix, full []string) bool {
	if len(suffix) > len(full) {
		return false
	}
	offset := len(full) - len(suffix)
	for i, s := range suffix {
		if full[offset+i] != s {
			return false
		}
	}
	return true
}

func sortEdges(edges []Edge) {
	sort.SliceStable(edges, func(i, j int) bool {
		if edges[i].Type != edges[j].Type {
			return edgeTypeRank(edges[i].Type) < edgeTypeRank(edges[j].Type)
		}
		return edges[i].To < edges[j].To
	})
}

func edgeTypeRank(t string) int {
	switch t {
	case EdgeBelongsTo:
		return 0
	case "touches":
		return 1
	case "exposed_by":
		return 2
	default: // depends_on
		return 3
	}
}

func dedupSorted(values []string) []string {
	seen := make(map[string]bool, len(values))
	var out []string
	for _, v := range values {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out
}

// Coverage summarizes how much of the graph the rules reference.
type Coverage struct {
	EndpointsTotal      int
	EndpointsReferenced int
	EntitiesTotal       int
	EntitiesTouched     int
}

// ComputeCoverage counts endpoints and entities referenced by at least one
// rule edge.
func ComputeCoverage(nodes []Node) Coverage {
	var cov Coverage
	referenced := make(map[string]bool)
	for _, n := range nodes {
		if n.Type != TypeRule {
			continue
		}
		for _, e := range n.Edges {
			if e.Type == "touches" || e.Type == "exposed_by" {
				referenced[e.To] = true
			}
		}
	}
	for _, n := range nodes {
		switch n.Type {
		case "endpoint":
			cov.EndpointsTotal++
			if referenced[n.ID] {
				cov.EndpointsReferenced++
			}
		case "entity":
			cov.EntitiesTotal++
			if referenced[n.ID] {
				cov.EntitiesTouched++
			}
		}
	}
	return cov
}
