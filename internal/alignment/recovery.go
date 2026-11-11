package alignment

import (
	"maps"
	"slices"
)

// Agreement is how far the import graph's communities recover the
// concepts, over the files that have both a community and a concept (the
// one most of its placed rules have; a tie leaves the file out). ARI is
// the adjusted Rand index (Hubert and Arabie 1985): 1 for the same
// partition, about 0 for one no better than chance, negative for worse.
type Agreement struct {
	Files, Concepts, Communities int
	ARI                          float64
	// Defined is false when the index is not: fewer than two files, or
	// both partitions a single group (or all singletons).
	Defined bool
}

// recovery compares each file's concept with its community.
func recovery(placed []placedRule, community map[string]int) Agreement {
	votes := map[string]map[string]int{} // file → concept → rules
	for _, r := range placed {
		files := map[string]bool{}
		for _, src := range r.sources {
			if _, ok := community[src.Path]; ok && src.Root == "" {
				files[src.Path] = true
			}
		}
		for file := range files {
			tally(votes, file, r.concept)
		}
	}
	type cell struct {
		concept   string
		community int
	}
	cells := map[cell]int{}
	byConcept, byCommunity := map[string]int{}, map[int]int{}
	n := 0
	for file, held := range votes {
		name, ok := majority(held)
		if !ok {
			continue
		}
		cells[cell{name, community[file]}]++
		byConcept[name]++
		byCommunity[community[file]]++
		n++
	}
	a := Agreement{Files: n, Concepts: len(byConcept), Communities: len(byCommunity)}
	var index, rows, cols int
	for _, k := range cells {
		index += pairsOf(k)
	}
	for _, k := range byConcept {
		rows += pairsOf(k)
	}
	for _, k := range byCommunity {
		cols += pairsOf(k)
	}
	if n < pairMembers {
		return a
	}
	expected := float64(rows) * float64(cols) / float64(pairsOf(n))
	most := float64(rows+cols) / meanOfTwo
	if most == expected {
		return a
	}
	a.ARI, a.Defined = (float64(index)-expected)/(most-expected), true
	return a
}

// majority is the concept with strictly the most votes.
func majority(held map[string]int) (string, bool) {
	best, top, tied := "", 0, false
	for _, name := range slices.Sorted(maps.Keys(held)) {
		switch {
		case held[name] > top:
			best, top, tied = name, held[name], false
		case held[name] == top:
			tied = true
		}
	}
	return best, !tied
}

// The twos of the index.
const (
	pairMembers = 2 // n choose 2 counts sets of this many
	meanOfTwo   = 2 // the most the index can reach is the mean of two counts
)

// pairsOf is n choose 2.
func pairsOf(n int) int { return n * (n - 1) / pairMembers }
