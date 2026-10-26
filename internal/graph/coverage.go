package graph

import (
	"cmp"
	"maps"
	"slices"
	"strings"
)

// Coverage values of a domain node (frontmatter `coverage`).
const (
	// CoverageComplete: every chunk placed in the domain was mapped and its
	// candidates reduced. Paths excluded by --exclude and files without a
	// grammar belong to no domain, so they never make a domain partial;
	// the output README counts them.
	CoverageComplete = "complete"
	// CoveragePartial: some of the domain's code produced no rule because a
	// phase failed or skipped it; coverage_gaps lists where.
	CoveragePartial = "partial"
)

// Frontmatter keys of a domain's coverage.
const (
	ExtraCoverage     = "coverage"
	ExtraCoverageGaps = "coverage_gaps"
)

// GapReason says why some of a domain's code produced no rule.
type GapReason string

// The gap reasons, in the order the output lists them.
const (
	GapGenerated    GapReason = "generated"     // a generated file, never mapped
	GapMapFailed    GapReason = "map-failed"    // a chunk whose map call failed
	GapReduceFailed GapReason = "reduce-failed" // the domain's reduce failed
)

// CoverageGap is one piece of a domain's code that produced no rule.
type CoverageGap struct {
	Domain string
	Reason GapReason
	Path   string // "" when the gap is the whole domain (reduce-failed)
	Lines  string // "" when the gap is a whole file or the whole domain
}

// String renders the gap as the output lists it: "map-failed a.go:1-40".
func (g CoverageGap) String() string {
	where := g.Path
	if g.Lines != "" {
		where += ":" + g.Lines
	}
	if where == "" {
		return string(g.Reason)
	}
	return string(g.Reason) + " " + where
}

// SortGaps returns gaps ordered by domain, reason, path and lines, the
// order every rendering of them uses.
func SortGaps(gaps []CoverageGap) []CoverageGap {
	return slices.SortedStableFunc(slices.Values(gaps), func(a, b CoverageGap) int {
		return cmp.Or(
			cmp.Compare(a.Domain, b.Domain),
			cmp.Compare(a.Reason, b.Reason),
			cmp.Compare(a.Path, b.Path),
			cmp.Compare(a.Lines, b.Lines),
		)
	})
}

// AnnotateCoverage returns nodes with every domain node stamped with its
// coverage: partial, with its gaps joined by "; ", when a gap names its
// domain; complete otherwise. The input nodes are not modified.
//
// Ensures: only domain nodes differ from the input; a gap whose domain has
// no node (a failed reduce with no extractor rule) changes nothing here,
// the output README still lists it.
func AnnotateCoverage(nodes []Node, gaps []CoverageGap) []Node {
	byDomain := make(map[string][]string)
	for _, g := range SortGaps(gaps) {
		byDomain[g.Domain] = append(byDomain[g.Domain], g.String())
	}
	out := slices.Clone(nodes)
	for i := range out {
		if out[i].Type != TypeDomain {
			continue
		}
		extra := maps.Clone(out[i].Extra)
		if extra == nil {
			extra = map[string]string{}
		}
		listed := byDomain[strings.TrimPrefix(out[i].ID, "domain.")]
		if len(listed) == 0 {
			extra[ExtraCoverage] = CoverageComplete
		} else {
			extra[ExtraCoverage] = CoveragePartial
			extra[ExtraCoverageGaps] = strings.Join(listed, "; ")
		}
		out[i].Extra = extra
	}
	return out
}
