package alignment

import (
	"cmp"
	"maps"
	"path"
	"slices"
)

// Spread is how one concept's rules spread over directories (scattering),
// or one directory's rules over concepts (tangling). Rules stays beside
// Places: a concept with many rules reaches more directories by size
// alone.
type Spread struct {
	Name     string // the concept, or the directory
	Rules    int    // placed rules
	Places   int    // distinct directories, or concepts
	Top      string // the place holding most of its rules, by name on a tie
	TopRules int    // the rules Top holds
}

// scattering is, per concept, the directories its rules cite.
func scattering(placed []placedRule) []Spread {
	byConcept := map[string]map[string]int{} // concept → directory → rules
	rules := map[string]int{}
	for _, r := range placed {
		rules[r.concept]++
		for _, dir := range directories(r) {
			tally(byConcept, r.concept, dir)
		}
	}
	return spreads(byConcept, rules)
}

// tangling is, per directory, the concepts of the rules citing it.
func tangling(placed []placedRule) []Spread {
	byDir := map[string]map[string]int{} // directory → concept → rules
	rules := map[string]int{}
	for _, r := range placed {
		for _, dir := range directories(r) {
			rules[dir]++
			tally(byDir, dir, r.concept)
		}
	}
	return spreads(byDir, rules)
}

// directories are the directories r cites, each once, another root's
// prefixed "@root/": the language's package where a directory is one, and
// a stand-in where it isn't.
func directories(r placedRule) []string {
	dirs := make([]string, 0, len(r.sources))
	for _, src := range r.sources {
		dirs = append(dirs, path.Dir(src.File()))
	}
	slices.Sort(dirs)
	return slices.Compact(dirs)
}

func tally(m map[string]map[string]int, outer, inner string) {
	if m[outer] == nil {
		m[outer] = map[string]int{}
	}
	m[outer][inner]++
}

// spreads sorts the spreads: most places first, then most rules, then by
// name.
func spreads(places map[string]map[string]int, rules map[string]int) []Spread {
	out := make([]Spread, 0, len(places))
	for name, held := range places {
		s := Spread{Name: name, Rules: rules[name], Places: len(held)}
		for _, place := range slices.Sorted(maps.Keys(held)) {
			if held[place] > s.TopRules {
				s.Top, s.TopRules = place, held[place]
			}
		}
		out = append(out, s)
	}
	slices.SortFunc(out, func(a, b Spread) int {
		return cmp.Or(cmp.Compare(b.Places, a.Places), cmp.Compare(b.Rules, a.Rules),
			cmp.Compare(a.Name, b.Name))
	})
	return out
}
