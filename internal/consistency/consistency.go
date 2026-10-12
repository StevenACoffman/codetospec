// Package consistency runs deterministic cross-rule checks: it flags
// near-duplicate rules (the same behavior surfaced in several places) so a
// reviewer can reconcile them. No LLM; pure text similarity.
package consistency

import (
	"regexp"
	"sort"

	"codetospec/internal/graph"
	"codetospec/internal/textsim"
)

// DuplicatePair is two rules whose requirements are near-identical.
type DuplicatePair struct {
	A, B       string  // rule ids, A < B
	Similarity float64 // Jaccard over requirement word sets, in [0,1]
}

// DefaultThreshold is the similarity above which two rules are reported as
// duplicate candidates.
const DefaultThreshold = 0.8

// FindDuplicates returns near-duplicate rule pairs, most similar first.
// Deterministic: pairs and ordering are stable.
func FindDuplicates(nodes []graph.Node, threshold float64) []DuplicatePair {
	if threshold <= 0 {
		threshold = DefaultThreshold
	}
	type rule struct {
		id        string
		words     map[string]bool
		negations int
	}
	var rules []rule
	for _, n := range nodes {
		if n.Type != graph.TypeRule {
			continue
		}
		requirement := graph.RuleRequirement(n.Body)
		words := textsim.Words(requirement)
		if len(words) == 0 {
			continue
		}
		rules = append(rules, rule{id: n.ID, words: words, negations: negations(requirement)})
	}
	sort.SliceStable(rules, func(i, j int) bool { return rules[i].id < rules[j].id })

	var pairs []DuplicatePair
	for i := range rules {
		for j := i + 1; j < len(rules); j++ {
			// Word overlap is blind to "is marked" vs "is not marked": rules
			// that negate a different number of times state opposite things.
			if rules[i].negations != rules[j].negations {
				continue
			}
			sim := textsim.Jaccard(rules[i].words, rules[j].words)
			if sim >= threshold {
				pairs = append(
					pairs,
					DuplicatePair{A: rules[i].id, B: rules[j].id, Similarity: sim},
				)
			}
		}
	}
	sort.SliceStable(pairs, func(i, j int) bool {
		if pairs[i].Similarity != pairs[j].Similarity {
			return pairs[i].Similarity > pairs[j].Similarity
		}
		if pairs[i].A != pairs[j].A {
			return pairs[i].A < pairs[j].A
		}
		return pairs[i].B < pairs[j].B
	})
	return pairs
}

// negationRe matches one negation event in English or French. French
// "ne … pas/jamais/plus/aucun" is counted once, on "ne"/"n'".
var negationRe = regexp.MustCompile(
	`(?i)\b(?:not|no|never|without|cannot|none|nor|ne|sans)\b|n['’]t\b|\bn['’]`,
)

// negations counts the negation events of a requirement.
func negations(s string) int {
	return len(negationRe.FindAllString(s, -1))
}
