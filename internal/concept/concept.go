// Package concept assigns each business rule to a concept of a person's
// confirmed domain model, from evidence and without an LLM
// (DOMAIN_PLAN.md recommendations 3, 12, 13, 15).
//
// A rule's evidence is the code entities it touches and the condition
// variables it reads on an entity's table, each standing for the concept
// the model matched to that entity. Several concepts resolve to their
// nearest common owner along the model's owned relationships (a rule
// about a configuration and its assignment is the assignment's); concepts
// with no common owner are a conflict, recorded with each option and its
// evidence, never settled by a majority. Only rules whose structural
// evidence says business (position's nature_by_evidence) are assigned:
// a technical or open rule has no concept to find.
//
// Regroup then applies it: each placed rule moves into its model's
// context domain; the Report says how the rules regrouped.
package concept

import (
	"cmp"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/StevenACoffman/codetospec/internal/condition"
	"github.com/StevenACoffman/codetospec/internal/graph"
	"github.com/StevenACoffman/codetospec/internal/position"
)

// Concept is one entity of a confirmed model, as assignment reads it.
type Concept struct {
	Name string // qualified, "<scope>.<Entity>"
	// Entity is the code entity node the model matched to it
	// (domainmodel.Compare); "" when none did: no evidence can name it.
	Entity string
	// Table is that entity's table name, as condition variables prefix it.
	Table string
	// Owner is the concept that owns it (an owned relationship), "" when
	// none does.
	Owner string
}

// Count is how one concept's assignment regroups the rules.
type Count struct {
	Concept string
	Rules   int
	// Domains are the current domains its rules come from, sorted by
	// rule count then name, each "domain (n)".
	Domains []string
}

// Report is the outcome of Assign, sorted by concept name.
type Report struct {
	Concepts  []Count
	Conflicts int // business rules whose concepts share no owner
	Unplaced  int // business rules with no concept evidence
	// Regrouped is, per context domain, how many rules Regroup moved
	// into it.
	Regrouped map[string]int
}

// The frontmatter keys Assign writes.
const (
	ExtraConcept         = "concept"
	ExtraConceptEvidence = "concept_evidence"
	ExtraConceptConflict = "concept_conflict"
)

// Assign records each business rule's concept (ExtraConcept) and its
// evidence (ExtraConceptEvidence), or, when its concepts share no owner,
// the options (ExtraConceptConflict).
//
// Requires: rules annotated by position.Annotate (nature_by_evidence);
// concepts with distinct names, owners among them or "".
// Ensures:  nodes is not modified; a rule that is not business, or has no
// evidence, gets no field; the result depends on neither node nor concept
// order.
func Assign(nodes []graph.Node, concepts []Concept) ([]graph.Node, Report) {
	if len(concepts) == 0 {
		return nodes, Report{}
	}
	idx := newIndex(concepts)
	counts := map[string]map[string]int{} // concept → domain → rules
	var report Report
	out := slices.Clone(nodes)
	for i := range out {
		rule := &out[i]
		if rule.Type != graph.TypeRule ||
			rule.Extra[graph.ExtraNatureByEvidence] != position.Business {
			continue
		}
		found := idx.evidence(rule)
		if len(found) == 0 {
			report.Unplaced++
			continue
		}
		rule.Extra = maps.Clone(rule.Extra)
		if owner := idx.commonOwner(slices.Sorted(maps.Keys(found))); owner != "" {
			rule.Extra[ExtraConcept] = owner
			rule.Extra[ExtraConceptEvidence] = summary(found)
			if counts[owner] == nil {
				counts[owner] = map[string]int{}
			}
			counts[owner][graph.RuleDomain(rule)]++
			continue
		}
		rule.Extra[ExtraConceptConflict] = summary(found)
		report.Conflicts++
	}
	for _, name := range slices.Sorted(maps.Keys(counts)) {
		report.Concepts = append(report.Concepts, count(name, counts[name]))
	}
	return out, report
}

// index finds concepts by entity and table, and walks owners.
type index struct {
	byEntity, byTable map[string]string
	owner             map[string]string
}

func newIndex(concepts []Concept) *index {
	idx := &index{
		byEntity: map[string]string{},
		byTable:  map[string]string{},
		owner:    map[string]string{},
	}
	for _, c := range concepts {
		if c.Entity != "" {
			idx.byEntity[c.Entity] = c.Name
		}
		if c.Table != "" {
			idx.byTable[c.Table] = c.Name
		}
		idx.owner[c.Name] = c.Owner
	}
	return idx
}

// evidence is the concepts rule's code names, each with what names it:
// "touches entity.x", "condition x.status".
func (idx *index) evidence(rule *graph.Node) map[string][]string {
	found := map[string][]string{}
	for _, e := range rule.Edges {
		if name, ok := idx.byEntity[e.To]; ok && e.Type == graph.EdgeTouches {
			found[name] = append(found[name], e.Type+" "+e.To)
		}
	}
	if raw := rule.Extra[graph.ExtraCondition]; raw != "" {
		if c, err := condition.Parse([]byte(raw)); err == nil {
			for _, v := range c.Variables() {
				table, _, ok := cutLast(v, ".")
				if name, known := idx.byTable[table]; ok && known {
					found[name] = append(found[name], "condition "+v)
				}
			}
		}
	}
	return found
}

// cutLast cuts s around the last sep: "a.b.c" → "a.b", "c".
func cutLast(s, sep string) (string, string, bool) {
	i := strings.LastIndex(s, sep)
	if i <= 0 {
		return "", "", false
	}
	return s[:i], s[i+len(sep):], true
}

// commonOwner is the nearest concept that is, or owns, every one of
// names: names[0] and its owners in order, the first that every other
// name reaches through its own owners; "" when none does.
func (idx *index) commonOwner(names []string) string {
	for _, candidate := range idx.chain(names[0]) {
		if !slices.ContainsFunc(names[1:], func(n string) bool {
			return !slices.Contains(idx.chain(n), candidate)
		}) {
			return candidate
		}
	}
	return ""
}

// chain is name then its owners, nearest first; a cycle stops it.
func (idx *index) chain(name string) []string {
	var chain []string
	for name != "" && !slices.Contains(chain, name) {
		chain = append(chain, name)
		name = idx.owner[name]
	}
	return chain
}

// summary writes found as "A: touches entity.a, condition a.x; B: …",
// concepts and their evidence sorted.
func summary(found map[string][]string) string {
	var parts []string
	for _, name := range slices.Sorted(maps.Keys(found)) {
		ev := slices.Clone(found[name])
		slices.Sort(ev)
		parts = append(parts, name+": "+strings.Join(slices.Compact(ev), ", "))
	}
	return strings.Join(parts, "; ")
}

// count is one concept's Count.
func count(name string, byDomain map[string]int) Count {
	c := Count{Concept: name}
	domains := slices.Collect(maps.Keys(byDomain))
	slices.SortFunc(domains, func(a, b string) int {
		return cmp.Or(cmp.Compare(byDomain[b], byDomain[a]), cmp.Compare(a, b))
	})
	for _, d := range domains {
		c.Rules += byDomain[d]
		c.Domains = append(c.Domains, d+" ("+strconv.Itoa(byDomain[d])+")")
	}
	return c
}

// Subjects makes the actor owning each placed rule's concept its EARS
// subject (DOMAIN_PLAN.md recommendation 23): owners maps a concept
// ("<scope>.<Entity>") or a whole model ("<scope>") to its actor, the
// concept winning over its model. The sentence is written again from its
// components (graph.WithSubject), no LLM; a rule a person reviewed is
// theirs and keeps its sentence.
//
// Requires: nodes as Assign returns them.
// Ensures:  nodes is not modified; returns how many rules changed subject.
func Subjects(nodes []graph.Node, owners map[string]string) ([]graph.Node, int) {
	if len(owners) == 0 {
		return nodes, 0
	}
	out := slices.Clone(nodes)
	changed := 0
	for i := range out {
		name := out[i].Extra[ExtraConcept]
		if name == "" || out[i].Status == graph.StatusReviewed {
			continue
		}
		scope, _, _ := strings.Cut(name, ".")
		actor := cmp.Or(owners[name], owners[scope])
		if actor == "" {
			continue
		}
		if rewritten, ok := graph.WithSubject(&out[i], actor); ok {
			out[i] = rewritten
			changed++
		}
	}
	return out, changed
}
