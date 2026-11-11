package concept

import (
	"cmp"
	"maps"
	"slices"
	"strings"

	"github.com/StevenACoffman/codetospec/internal/graph"
)

// ConflictConcepts are the concepts a rule's ExtraConceptConflict names,
// without their evidence, in its (sorted) order.
func ConflictConcepts(options string) []string {
	var names []string
	for part := range strings.SplitSeq(options, "; ") {
		if name, _, _ := strings.Cut(part, ": "); name != "" {
			names = append(names, name)
		}
	}
	return names
}

// ConflictSet is a question that settles conflicts: do these concepts
// share an owner? Answering it settles every conflict whose concepts it
// contains (an owner common to all of them is common to any of them).
type ConflictSet struct {
	Concepts []string // sorted
	Rules    int      // conflicts naming exactly these concepts
	Settles  int      // conflicts it settles that earlier sets left open
}

// ConflictSets orders the distinct concept sets of the conflicting rules
// so that answering them in turn settles the most conflicts soonest
// (greedy: most newly settled first, then fewer concepts, then by name).
// A set an earlier one contains is left out: answering that one settled
// it.
//
// Requires: nodes as Assign returns them.
// Ensures:  the Settles add up to the conflicts; deterministic.
func ConflictSets(nodes []graph.Node) []ConflictSet {
	sets := distinctSets(nodes)
	var out []ConflictSet
	for len(sets.open) > 0 {
		best := ConflictSet{}
		for _, key := range slices.Sorted(maps.Keys(sets.open)) {
			candidate := ConflictSet{
				Concepts: sets.members[key],
				Rules:    sets.open[key],
				Settles:  sets.settled(sets.members[key]),
			}
			if better(&candidate, &best) {
				best = candidate
			}
		}
		sets.settle(best.Concepts)
		out = append(out, best)
	}
	return out
}

// conflictSets are the concept sets of the conflicts still open, keyed by
// their sorted concepts joined.
type conflictSets struct {
	open    map[string]int      // key → conflicts naming exactly that set
	members map[string][]string // key → its sorted concepts
}

func distinctSets(nodes []graph.Node) conflictSets {
	sets := conflictSets{open: map[string]int{}, members: map[string][]string{}}
	for i := range nodes {
		names := ConflictConcepts(nodes[i].Extra[ExtraConceptConflict])
		if len(names) == 0 {
			continue
		}
		slices.Sort(names)
		key := strings.Join(names, "\x00")
		sets.open[key]++
		sets.members[key] = names
	}
	return sets
}

// settled is how many open conflicts answering concepts would settle:
// those whose set it contains.
func (s *conflictSets) settled(concepts []string) int {
	n := 0
	for key, count := range s.open {
		if containsAll(concepts, s.members[key]) {
			n += count
		}
	}
	return n
}

// settle closes the conflicts answering concepts settles.
func (s *conflictSets) settle(concepts []string) {
	maps.DeleteFunc(s.open, func(key string, _ int) bool {
		return containsAll(concepts, s.members[key])
	})
}

// better is whether a settles more than b, or as many with fewer
// concepts; keys come sorted, so a first-seen tie keeps the lower name.
func better(a, b *ConflictSet) bool {
	return cmp.Or(cmp.Compare(a.Settles, b.Settles),
		cmp.Compare(len(b.Concepts), len(a.Concepts))) > 0
}

// containsAll is whether set holds every one of names.
func containsAll(set, names []string) bool {
	for _, n := range names {
		if !slices.Contains(set, n) {
			return false
		}
	}
	return true
}
