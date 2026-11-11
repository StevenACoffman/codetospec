package graph

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"

	"github.com/StevenACoffman/codetospec/internal/locale"
)

// Target is a domain rules move into (Move): its slug, its summary, the
// nodes it depends on (ids: other domains, services), and the Extra key
// whose value lists its rules under one heading each ("" for one list).
type Target struct {
	Slug, Summary string
	DependsOn     []string
	GroupBy       string
}

// Move moves each rule of moves (rule id → a target's slug) into its
// target domain, and rebuilds what a domain node carries so the move is
// complete: the rule's belongs_to edge; each domain's rule list (a
// domain keeps its reduce summary); the target's coverage, partial with
// every gap of the domains its rules came from when any was partial; its
// depends_on, the target's own plus the services its rules' domains
// depended on. A domain left with no rule and no coverage gap is
// dropped, and so is every depends_on edge to a domain not in the result. Rule ids don't change:
// a moved rule keeps its id, whose domain segment is its first domain's
// (MatchRules). Body labels use lang's text.
//
// A domain named like a target, its rules all moved, merges into it.
//
// Requires: every move names a rule of nodes and a target of targets;
// target slugs distinct, none the slug of a domain that keeps a rule.
// Ensures: nodes is not modified; sorted by id; deterministic.
func Move(nodes []Node, moves map[string]string, targets []Target, lang string) []Node {
	text := locale.For(lang)
	out := slices.Clone(nodes)
	from := moveRules(out, moves)

	rulesOf := map[string][]Node{}
	domains := map[string]*Node{}
	for i := range out {
		switch out[i].Type {
		case TypeRule:
			domain := RuleDomain(&out[i])
			rulesOf[domain] = append(rulesOf[domain], out[i])
		case TypeDomain:
			domains[strings.TrimPrefix(out[i].ID, "domain.")] = &out[i]
		}
	}
	dropped := rebuildSources(from, rulesOf, domains, text)
	for _, t := range targets {
		// A domain named like the target, its rules all gone, becomes the
		// target: its gaps go with it.
		if d := domains[t.Slug]; d != nil {
			if from[t.Slug] == nil {
				from[t.Slug] = map[string]bool{}
			}
			from[t.Slug][t.Slug] = true
			dropped[d.ID] = true
		}
	}

	// Target nodes read their sources through domains, pointers into
	// out: build them before deleting from out shifts its elements.
	added := make([]Node, 0, len(targets))
	for _, t := range targets {
		added = append(
			added,
			targetNode(&t, rulesOf[t.Slug], sortedKeys(from[t.Slug]), domains, text),
		)
	}
	kept := append(slices.DeleteFunc(out, func(n Node) bool { return dropped[n.ID] }), added...)
	pruneDomainEdges(kept)
	sort.SliceStable(kept, func(i, j int) bool { return kept[i].ID < kept[j].ID })
	return kept
}

// moveRules points each moved rule of nodes at its target, in place (on
// edges it copies), and returns, per target slug, the domains its rules
// came from.
func moveRules(nodes []Node, moves map[string]string) map[string]map[string]bool {
	from := map[string]map[string]bool{}
	for i := range nodes {
		slug, moved := moves[nodes[i].ID]
		if !moved || nodes[i].Type != TypeRule {
			continue
		}
		if from[slug] == nil {
			from[slug] = map[string]bool{}
		}
		from[slug][RuleDomain(&nodes[i])] = true
		nodes[i].Edges = slices.Clone(nodes[i].Edges)
		for j := range nodes[i].Edges {
			if nodes[i].Edges[j].Type == EdgeBelongsTo {
				nodes[i].Edges[j].To = "domain." + slug
			}
		}
	}
	return from
}

// rebuildSources rewrites the rule list of each domain rules left (from),
// in place, and returns the ids of those left with no rule and no gap.
func rebuildSources(
	from map[string]map[string]bool,
	rulesOf map[string][]Node,
	domains map[string]*Node,
	text *locale.Text,
) map[string]bool {
	changed := map[string]bool{}
	for _, sources := range from {
		maps.Copy(changed, sources)
	}
	dropped := map[string]bool{}
	for slug := range changed {
		d := domains[slug]
		switch {
		case d == nil:
		case len(rulesOf[slug]) == 0 && d.Extra[ExtraCoverage] != CoveragePartial:
			dropped[d.ID] = true
		default:
			d.Body = domainBody(domainSummary(d.Body, text), rulesOf[slug], "", text)
		}
	}
	return dropped
}

// pruneDomainEdges drops, in place, each depends_on edge to a domain
// nodes lacks.
func pruneDomainEdges(nodes []Node) {
	present := map[string]bool{}
	for i := range nodes {
		present[nodes[i].ID] = true
	}
	for i := range nodes {
		nodes[i].Edges = slices.DeleteFunc(slices.Clone(nodes[i].Edges), func(e Edge) bool {
			return e.Type == EdgeDependsOn && strings.HasPrefix(e.To, "domain.") && !present[e.To]
		})
	}
}

// targetNode is target t's domain node, holding rules, which came from
// the domains sources (nodes by slug in domains).
func targetNode(
	t *Target,
	rules []Node,
	sources []string,
	domains map[string]*Node,
	text *locale.Text,
) Node {
	n := Node{
		ID:     "domain." + t.Slug,
		Type:   TypeDomain,
		Status: StatusGenerated,
		Title:  fmt.Sprintf(text.DomainTitleFmt, t.Slug),
		Body:   domainBody(t.Summary, rules, t.GroupBy, text),
		Extra:  map[string]string{ExtraCoverage: CoverageComplete},
	}
	depends := map[string]bool{}
	for _, to := range t.DependsOn {
		depends[to] = true
	}
	var gaps []string
	for _, slug := range sources {
		d := domains[slug]
		if d == nil {
			continue
		}
		if d.Extra[ExtraCoverage] == CoveragePartial {
			n.Extra[ExtraCoverage] = CoveragePartial
			gaps = append(gaps, strings.Split(d.Extra[ExtraCoverageGaps], "; ")...)
		}
		for _, e := range d.Edges {
			if e.Type == EdgeDependsOn && !strings.HasPrefix(e.To, "domain.") {
				depends[e.To] = true
			}
		}
	}
	if len(gaps) > 0 {
		slices.Sort(gaps)
		n.Extra[ExtraCoverageGaps] = strings.Join(slices.Compact(gaps), "; ")
	}
	for _, to := range sortedKeys(depends) {
		n.Edges = append(n.Edges, Edge{Type: EdgeDependsOn, To: to})
	}
	sortEdges(n.Edges)
	return n
}

// domainBody is a domain node's body: its summary, then its rules by id,
// under one heading per value of their groupBy Extra key when given
// (sorted, rules without it first).
func domainBody(summary string, rules []Node, groupBy string, text *locale.Text) string {
	rules = slices.Clone(rules)
	slices.SortFunc(rules, func(a, b Node) int {
		if groupBy != "" {
			if c := cmp.Compare(a.Extra[groupBy], b.Extra[groupBy]); c != 0 {
				return c
			}
		}
		return cmp.Compare(a.ID, b.ID)
	})
	var list strings.Builder
	group := ""
	for i, r := range rules {
		if g := r.Extra[groupBy]; groupBy != "" && g != "" && (i == 0 || g != group) {
			fmt.Fprintf(&list, "\n### %s\n\n", g)
			group = g
		}
		fmt.Fprintf(&list, "- [%s](../rules/%s.md)\n", r.Title, strings.TrimPrefix(r.ID, "rule."))
	}
	body := summary + "\n\n" + text.RulesHeading + "\n\n" + strings.Trim(list.String(), "\n")
	return strings.TrimSpace(body)
}

// domainSummary is the summary a domain node's body opens with: what
// precedes its rules heading.
func domainSummary(body string, text *locale.Text) string {
	if i := strings.Index(body, text.RulesHeading); i >= 0 {
		return strings.TrimSpace(body[:i])
	}
	return body
}

func sortedKeys(m map[string]bool) []string {
	return slices.Sorted(maps.Keys(m))
}
