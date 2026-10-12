package extract

import "sort"

// DomainHints answers which domain an extractor assigned to a source
// location, from KindDomain facts. It is immutable once built.
type DomainHints struct {
	byPath map[string][]domainHint
}

type domainHint struct {
	start, end int
	domain     string
}

// NewDomainHints indexes the KindDomain facts among facts. Facts with an
// unparsable range or a domain that is not a slug are ignored.
func NewDomainHints(facts []Fact) DomainHints {
	h := DomainHints{byPath: make(map[string][]domainHint)}
	for _, f := range facts {
		domain := f.Attrs[AttrDomain]
		if f.Kind != KindDomain || domain == "" || Slugify(domain) != domain {
			continue
		}
		start, end, err := ParseLines(f.Source.Lines)
		if err != nil {
			continue
		}
		h.byPath[f.Source.Path] = append(h.byPath[f.Source.Path], domainHint{start, end, domain})
	}
	for _, hints := range h.byPath {
		// Smallest span first, then domain name: the first covering hint is
		// the most specific one, deterministically.
		sort.Slice(hints, func(i, j int) bool {
			if a, b := hints[i].end-hints[i].start, hints[j].end-hints[j].start; a != b {
				return a < b
			}
			return hints[i].domain < hints[j].domain
		})
	}
	return h
}

// At returns the domain hinted for line in path: the hint with the smallest
// span covering it, ties broken by domain name; "" when no hint covers it.
func (h DomainHints) At(path string, line int) string {
	for _, hint := range h.byPath[path] {
		if hint.start <= line && line <= hint.end {
			return hint.domain
		}
	}
	return ""
}
