package graph

import (
	"cmp"
	"maps"
	"slices"
	"strings"

	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/textsim"
)

// sameRuleSimilarity is the requirement similarity (textsim.Jaccard) above
// which two rules of one domain citing overlapping code are one rule
// reworded: the reduce's near-duplicate threshold.
const sameRuleSimilarity = 0.6

// MatchRules pairs the LLM rules of current with the rules of prior they
// restate, so that a re-run keeps a rule's id when the model rewords it or
// renames its slug. Two rules match when they belong to one domain and
// either state the same requirement, or cite overlapping code and read
// alike (Jaccard ≥ 0.6) or share an id. Mechanical and inherited rules are
// never matched: their ids derive from their facts alone.
//
// Requires: rule nodes as Build makes them (a belongs_to edge to their
// domain), or as render.LoadDir reads them back.
// Ensures:  current id → prior id, one-to-one: the most similar pairs are
// taken first, ties broken by equal ids then by ids, so the result is
// deterministic.
func MatchRules(prior, current []Node) map[string]string {
	type pair struct {
		cur, old   *Node
		similarity float64
	}
	priorWords := make([]map[string]bool, len(prior))
	for j := range prior {
		priorWords[j] = textsim.Words(RuleRequirement(prior[j].Body))
	}
	var pairs []pair
	for i := range current {
		cur := &current[i]
		if !Reviewable(cur) {
			continue
		}
		curWords := textsim.Words(RuleRequirement(cur.Body))
		for j := range prior {
			old := &prior[j]
			if !Reviewable(old) || ruleDomain(old) != ruleDomain(cur) {
				continue
			}
			sim := textsim.Jaccard(curWords, priorWords[j])
			if RuleRequirement(cur.Body) == RuleRequirement(old.Body) {
				sim = 1
			} else if !citationsOverlap(cur.Sources, old.Sources) ||
				(sim < sameRuleSimilarity && cur.ID != old.ID) {
				continue
			}
			pairs = append(pairs, pair{cur, old, sim})
		}
	}
	slices.SortFunc(pairs, func(a, b pair) int {
		return cmp.Or(
			cmp.Compare(b.similarity, a.similarity),
			compareBool(a.cur.ID == a.old.ID, b.cur.ID == b.old.ID),
			cmp.Compare(a.cur.ID, b.cur.ID),
			cmp.Compare(a.old.ID, b.old.ID),
		)
	})
	matched := map[string]string{}
	used := map[string]bool{}
	for _, p := range pairs {
		if _, done := matched[p.cur.ID]; done || used[p.old.ID] {
			continue
		}
		matched[p.cur.ID] = p.old.ID
		used[p.old.ID] = true
	}
	return matched
}

// compareBool orders true before false.
func compareBool(a, b bool) int {
	switch {
	case a == b:
		return 0
	case a:
		return -1
	default:
		return 1
	}
}

// ruleDomain is the domain slug a rule belongs to.
func ruleDomain(n *Node) string {
	for _, e := range n.Edges {
		if e.Type == EdgeBelongsTo {
			return strings.TrimPrefix(e.To, "domain.")
		}
	}
	return ""
}

// citationsOverlap reports whether a and b cite a common line of a file.
func citationsOverlap(a, b []extract.Ref) bool {
	for _, x := range a {
		xStart, xEnd, err := extract.ParseLines(x.Lines)
		if err != nil {
			continue
		}
		for _, y := range b {
			yStart, yEnd, yErr := extract.ParseLines(y.Lines)
			if yErr == nil && x.File() == y.File() && xStart <= yEnd && yStart <= xEnd {
				return true
			}
		}
	}
	return false
}

// assignRuleIDs gives the rules their final ids: mechanical rules (from
// facts) first, so their ids depend on their extractor alone; then LLM
// rules matched to a prior rule take its id; the others keep their own id
// unless a rule or a prior rule holds it, then take the next free suffix —
// an id a prior rule held never names another rule in the next run.
//
// Requires: llm holds the reduce's rule nodes, mechanical the rule facts'
// nodes with their base ids (rule.<domain>.<slug>).
// Ensures:  ids unique across both; inputs are not modified.
func assignRuleIDs(llm, mechanical, prior []Node) ([]Node, []Node) {
	taken := map[string]bool{}
	mech := slices.Clone(mechanical)
	for i := range mech {
		mech[i].ID = uniqueID(mech[i].ID, taken)
		taken[mech[i].ID] = true
	}

	rules := slices.Clone(llm)
	matches := MatchRules(prior, rules)
	final := make([]string, len(rules))
	for i := range rules {
		if old, ok := matches[rules[i].ID]; ok && !taken[old] {
			final[i] = old
			taken[old] = true
		}
	}
	reserved := maps.Clone(taken)
	for i := range prior {
		if prior[i].Type == TypeRule {
			reserved[prior[i].ID] = true
		}
	}
	for i := range rules {
		if final[i] == "" {
			final[i] = uniqueID(rules[i].ID, reserved)
			reserved[final[i]] = true
		}
	}
	for i := range rules {
		rules[i].ID = final[i]
	}
	return rules, mech
}
