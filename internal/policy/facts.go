package policy

import (
	"cmp"
	"maps"
	"slices"

	"github.com/StevenACoffman/codetospec/internal/extract"
)

// factIndex holds the facts the inventory reads, by kind.
type factIndex struct {
	checks    []extract.Fact // access_check facts
	calls     []call         // direct calls, interface dispatch left out
	inputs    []extract.Fact // reads, uses_value, loads and calls_method facts
	unguarded []Unguarded
	// valueKinds is each declared type's kind (value_kind facts):
	// KindStaff, KindFlag.
	valueKinds map[string]string
}

// call is one direct call fact: caller span to callee span; forwards when
// the callee is a variable holding a function (a cache wrapper), a hop
// reach doesn't count.
type call struct {
	caller, callee extract.Ref
	forwards       bool
}

// indexFacts sorts facts by the kinds the inventory reads.
func indexFacts(facts []extract.Fact) *factIndex {
	idx := &factIndex{valueKinds: map[string]string{}}
	for _, f := range facts {
		switch f.Kind {
		case extract.KindAccessCheck:
			idx.checks = append(idx.checks, f)
		case extract.KindCall:
			if !f.ViaDispatch() {
				idx.calls = append(idx.calls, call{
					caller: f.Source,
					callee: extract.Ref{
						Path:  f.Attrs[extract.AttrCalleePath],
						Lines: f.Attrs[extract.AttrCalleeLines],
					},
					forwards: f.Attrs[extract.AttrCalleeVar] == "true",
				})
			}
		case extract.KindReads, extract.KindUsesValue, extract.KindLoads, extract.KindCallsMethod:
			idx.inputs = append(idx.inputs, f)
		case extract.KindValueKind:
			idx.valueKinds[f.Attrs[extract.AttrType]] = f.Attrs[extract.AttrValueKind]
		case extract.KindUnguarded:
			idx.unguarded = append(
				idx.unguarded,
				Unguarded{Operation: f.Attrs[extract.AttrOperation], Source: f.Source},
			)
		}
	}
	slices.SortFunc(idx.unguarded, func(a, b Unguarded) int {
		return cmp.Or(
			cmp.Compare(a.Operation, b.Operation),
			cmp.Compare(a.Source.Path, b.Source.Path),
		)
	})
	return idx
}

// reached is def and every definition its calls reach within MaxDepth,
// following direct calls by span overlap as journeys do.
func (idx *factIndex) reached(def extract.Ref) []extract.Ref {
	return idx.reachedFrom([]extract.Ref{def}, nil)
}

// checksBelow names the recognized checks defined in the tree read (an
// access_check fact's citation) that the code at spans reaches, other
// than checked and what only they reach: the checks a handler applies
// beyond its guards'. Sorted.
func (idx *factIndex) checksBelow(spans []extract.Ref, checked []string) []string {
	var stop []extract.Ref // the definitions of the guards' own checks
	for _, f := range idx.checks {
		if slices.Contains(checked, f.Attrs[extract.AttrName]) && f.Source.Path != "" {
			stop = append(stop, f.Source)
		}
	}
	// Only what the spans call: a check enclosing a guard is that guard's.
	reached := slices.DeleteFunc(idx.reachedFrom(spans, stop), func(r extract.Ref) bool {
		return slices.Contains(spans, r)
	})
	var below []string
	for _, f := range idx.checks {
		name := f.Attrs[extract.AttrName]
		if f.Source.Path == "" || f.Source.Root != "" || slices.Contains(checked, name) ||
			slices.Contains(below, name) {
			continue
		}
		if coveredBy(f.Source, reached) {
			below = append(below, name)
		}
	}
	slices.Sort(below)
	return below
}

// reachedFrom is starts and every definition their calls reach within
// MaxDepth, never entering a callee that overlaps one of stop. A call
// into a forwarding variable (a cache wrapper) doesn't count toward the
// depth: what it wraps is one call from the caller.
func (idx *factIndex) reachedFrom(starts, stop []extract.Ref) []extract.Ref {
	seen := map[extract.Ref]bool{}
	for _, s := range starts {
		seen[s] = true
	}
	frontier := slices.Clone(starts)
	for depth := 0; depth < MaxDepth && len(frontier) > 0; depth++ {
		var next []extract.Ref
		for i := 0; i < len(frontier); i++ {
			for _, c := range idx.calls {
				if !overlaps(c.caller, frontier[i]) || seen[c.callee] || coveredBy(c.callee, stop) {
					continue
				}
				seen[c.callee] = true
				if c.forwards {
					frontier = append(frontier, c.callee) // the same depth
				} else {
					next = append(next, c.callee)
				}
			}
		}
		frontier = next
	}
	return slices.SortedFunc(maps.Keys(seen), func(a, b extract.Ref) int {
		return cmp.Or(cmp.Compare(a.Path, b.Path), cmp.Compare(a.Lines, b.Lines))
	})
}

// coveredBy reports whether src overlaps one of spans.
func coveredBy(src extract.Ref, spans []extract.Ref) bool {
	for _, s := range spans {
		if overlaps(src, s) {
			return true
		}
	}
	return false
}

// overlaps reports whether two citations of the same tree share a line.
func overlaps(a, b extract.Ref) bool {
	if a.Path != b.Path || a.Root != b.Root {
		return false
	}
	as, ae, errA := extract.ParseLines(a.Lines)
	bs, be, errB := extract.ParseLines(b.Lines)
	return errA == nil && errB == nil && as <= be && bs <= ae
}
