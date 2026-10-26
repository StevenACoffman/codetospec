package reducer

import (
	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/mapper"
	"github.com/StevenACoffman/codetospec/internal/textsim"
)

// Reduce modes (Reducer.Mode).
const (
	// ModeFull sends every candidate to the LLM, which merges duplicates and
	// writes rationale and acceptance criteria for every rule.
	ModeFull = "full"
	// ModeMerge sends only candidates that have a near-duplicate to the LLM;
	// the others become rules as mapped, without rationale or acceptance
	// criteria. Measured on a large Go service, ~65% of candidates had no
	// near-duplicate, so reduce costs a fraction of ModeFull.
	ModeMerge = "merge"
)

// clusterSimilarity is the requirement similarity at which two candidates
// are worth merging.
const clusterSimilarity = 0.6

// splitClusters separates the candidates that have a near-duplicate — a
// similar requirement (textsim.Jaccard ≥ clusterSimilarity) or an
// overlapping citation, transitively — from the singletons.
//
// Requires: nothing.
// Ensures:  clustered and singles partition rules, each keeping rules' order.
func splitClusters(rules []mapper.Rule) ([]mapper.Rule, []mapper.Rule) {
	words := make([]map[string]bool, len(rules))
	for i := range rules {
		words[i] = textsim.Words(rules[i].Requirement)
	}
	parent := make([]int, len(rules))
	for i := range parent {
		parent[i] = i
	}
	find := func(i int) int {
		for parent[i] != i {
			parent[i] = parent[parent[i]]
			i = parent[i]
		}
		return i
	}
	for i := range rules {
		for j := i + 1; j < len(rules); j++ {
			if textsim.Jaccard(words[i], words[j]) >= clusterSimilarity ||
				citationsOverlap(rules[i].Citations, rules[j].Citations) {
				parent[find(i)] = find(j)
			}
		}
	}
	size := make(map[int]int)
	for i := range rules {
		size[find(i)]++
	}
	var clustered, singles []mapper.Rule
	for i := range rules {
		if size[find(i)] > 1 {
			clustered = append(clustered, rules[i])
		} else {
			singles = append(singles, rules[i])
		}
	}
	return clustered, singles
}

// citationsOverlap reports whether any citation of a and b share a line.
func citationsOverlap(a, b []extract.Ref) bool {
	for _, x := range a {
		xs, xe, err := extract.ParseLines(x.Lines)
		if err != nil {
			continue
		}
		for _, y := range b {
			ys, ye, yErr := extract.ParseLines(y.Lines)
			if yErr == nil && x.Path == y.Path && xs <= ye && ys <= xe {
				return true
			}
		}
	}
	return false
}

// asIs turns a candidate that needs no merging into a rule, keeping what
// map produced; slugs are made unique by mergeBatchRules.
func asIs(c *mapper.Rule) Rule {
	return Rule{
		Slug:        normalizeSlug(c.Title),
		Title:       c.Title,
		EarsKind:    c.EarsKind,
		Requirement: c.Requirement,
		Citations:   c.Citations,
		Entities:    c.Entities,
		Endpoints:   c.Endpoints,
		Nature:      c.Nature,
		Origin:      c.Origin,
		Confidence:  c.Confidence,
	}
}
