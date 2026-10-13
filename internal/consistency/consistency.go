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
	rules := ruleTexts(nodes)
	var pairs []DuplicatePair
	for i := range rules {
		for j := i + 1; j < len(rules); j++ {
			if !mayDuplicate(&rules[i], &rules[j]) {
				continue
			}
			if sim := textsim.Jaccard(rules[i].words, rules[j].words); sim >= threshold {
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

// ruleText is what the duplicate check knows about one rule.
type ruleText struct {
	id         string
	words      map[string]bool
	negations  int
	mechanical bool // produced by an extractor (graph.ExtraExtractedBy)
}

// ruleTexts returns the rules with a non-empty requirement, sorted by id.
func ruleTexts(nodes []graph.Node) []ruleText {
	var rules []ruleText
	for _, n := range nodes {
		if n.Type != graph.TypeRule {
			continue
		}
		requirement := graph.RuleRequirement(n.Body)
		words := textsim.Words(requirement)
		if len(words) == 0 {
			continue
		}
		rules = append(rules, ruleText{
			id:         n.ID,
			words:      words,
			negations:  negations(requirement),
			mechanical: n.Extra[graph.ExtraExtractedBy] != "",
		})
	}
	sort.SliceStable(rules, func(i, j int) bool { return rules[i].id < rules[j].id })
	return rules
}

// mayDuplicate reports whether two rules can be duplicates at all, before
// comparing their words. Word overlap is blind to "is marked" vs "is not
// marked", so rules negating a different number of times never pair. Two
// extractor rules come from one template and differ in the operation they
// name: alike in words, distinct by construction.
func mayDuplicate(a, b *ruleText) bool {
	return a.negations == b.negations && (!a.mechanical || !b.mechanical)
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
