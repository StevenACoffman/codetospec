package extract

import "slices"

// DescribesCitation reports whether a fact of kind states something about
// the code it cites, so that excluding that code must drop it too: an
// extractor's rule, a domain hint, an access check's definition, a
// handler found unguarded, what a definition reads. Other
// kinds stay whatever their file: a test fact cites the test that is
// evidence even though tests are excluded from the map, and a call fact is
// only an edge.
func DescribesCitation(kind string) bool {
	switch kind {
	case KindRule, KindDomain, KindAccessCheck, KindUnguarded,
		KindReads, KindUsesValue, KindLoads, KindCapability:
		return true
	}
	return false
}

// WithoutExcluded returns facts minus those that describe excluded code
// (see DescribesCitation), with the dropped facts' IDs. excluded reports
// whether a path under --src is excluded; a fact citing another root (an
// inherited rule, shared code) is kept, since --exclude applies to --src.
//
// Ensures: kept preserves the order of facts; dropped is sorted; facts is
// not modified.
func WithoutExcluded(facts []Fact, excluded func(path string) bool) ([]Fact, []string) {
	kept := make([]Fact, 0, len(facts))
	var dropped []string
	for _, f := range facts {
		if DescribesCitation(f.Kind) && f.Source.Root == "" && f.Source.Path != "" &&
			excluded(f.Source.Path) {
			dropped = append(dropped, f.ID)
			continue
		}
		kept = append(kept, f)
	}
	slices.Sort(dropped)
	return kept, dropped
}
