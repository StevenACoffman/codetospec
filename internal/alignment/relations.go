package alignment

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/StevenACoffman/codetospec/internal/concept"
	"github.com/StevenACoffman/codetospec/internal/extract"
)

// Outcome is how a pair of concepts compares, model against code.
type Outcome string

// The outcomes of a pair.
const (
	// Converges: the model declares a relationship and the code shows one.
	Converges Outcome = "converges"
	// Diverges: the code relates the concepts, the model declares nothing.
	Diverges Outcome = "diverges"
	// Absent: the model declares a relationship the code doesn't show,
	// though it could (both concepts are observable).
	Absent Outcome = "absent"
	// Unobservable: the model declares a relationship and one concept has
	// neither placed rules nor a table: no fact could show it, so its
	// absence is unknown, not established.
	Unobservable Outcome = "unobservable"
)

// Pair is two concepts, model against code. A model relationship is
// structure, not a call direction, so a pair is unordered; each evidence
// line names its direction.
type Pair struct {
	A, B    string // qualified concepts, A < B
	Outcome Outcome
	// Evidence is what the code shows, sorted: "A → B: fk t(c) → u" or
	// "A → B: call path:l-l → path:l-l".
	Evidence []string
}

// pairs compares every pair the model declares or the code relates.
func pairs(placed []placedRule, in *Input) []Pair {
	idx := newSpanIndex(placed)
	tables := tablesOf(in.Facts, in.Concepts)
	evidence := shown(idx, tables, in.Facts)
	declared := map[[2]string]bool{}
	for _, r := range in.Declared {
		if r.From != r.To {
			declared[pairKey(r.From, r.To)] = true
		}
	}
	keys := slices.Collect(maps.Keys(evidence))
	for key := range declared {
		if _, seen := evidence[key]; !seen {
			keys = append(keys, key)
		}
	}
	slices.SortFunc(keys, func(a, b [2]string) int {
		return cmp.Or(cmp.Compare(a[0], b[0]), cmp.Compare(a[1], b[1]))
	})
	observable := func(name string) bool { return idx.hasRules[name] || tables.has[name] }
	out := make([]Pair, 0, len(keys))
	for _, key := range keys {
		lines := slices.Compact(slices.Sorted(slices.Values(evidence[key])))
		p := Pair{A: key[0], B: key[1], Evidence: lines}
		switch {
		case declared[key] && len(lines) > 0:
			p.Outcome = Converges
		case len(lines) > 0:
			p.Outcome = Diverges
		case observable(key[0]) && observable(key[1]):
			p.Outcome = Absent
		default:
			p.Outcome = Unobservable
		}
		out = append(out, p)
	}
	return out
}

// shown is what the code shows of each pair: foreign keys between the
// concepts' tables, and direct calls from code one concept's rules cite
// into code another's cite. Imports are left out: a package serves most
// concepts (assignments' 786 placed rules come from ~35 code domains), so
// an import would relate nearly every pair.
func shown(idx *spanIndex, tables conceptTables, facts []extract.Fact) map[[2]string][]string {
	evidence := map[[2]string][]string{}
	add := func(from, to, line string) {
		if from != to {
			key := pairKey(from, to)
			evidence[key] = append(evidence[key], from+" → "+to+": "+line)
		}
	}
	for _, fk := range tables.foreignKeys {
		add(fk.from, fk.to, fk.line)
	}
	for _, c := range extract.DirectCalls(facts) {
		line := fmt.Sprintf("call %s → %s", spanText(c.Caller), spanText(c.Callee))
		for _, from := range idx.conceptsAt(c.Caller) {
			for _, to := range idx.conceptsAt(c.Callee) {
				add(from, to, line)
			}
		}
	}
	return evidence
}

// pairKey orders a pair's concepts.
func pairKey(a, b string) [2]string { return [2]string{min(a, b), max(a, b)} }

// conceptTables are the foreign keys between concepts' tables and the
// concepts whose table the facts declare.
type conceptTables struct {
	foreignKeys []foreignKey
	has         map[string]bool
}

type foreignKey struct{ from, to, line string }

// tablesOf reads the table facts naming a concept's table.
func tablesOf(facts []extract.Fact, concepts []concept.Concept) conceptTables {
	byTable := map[string]string{}
	for _, c := range concepts {
		if c.Table != "" {
			byTable[c.Table] = c.Name
		}
	}
	out := conceptTables{has: map[string]bool{}}
	for i := range facts {
		f := &facts[i]
		from, ok := byTable[f.Attrs[extract.AttrName]]
		if f.Kind != extract.KindTable || !ok {
			continue
		}
		out.has[from] = true
		for _, ref := range extract.ParseReferences(f.Attrs[extract.AttrReferences]) {
			if to, known := byTable[ref.Table]; known {
				line := fmt.Sprintf("fk %s(%s) → %s",
					f.Attrs[extract.AttrName], strings.Join(ref.Columns, ", "), ref.Table)
				out.foreignKeys = append(out.foreignKeys, foreignKey{from, to, line})
			}
		}
	}
	return out
}

// spanIndex finds the concepts whose rules cite a span.
type spanIndex struct {
	byPath   map[string][]citedSpan
	hasRules map[string]bool
}

type citedSpan struct {
	span    extract.Span
	concept string
}

func newSpanIndex(placed []placedRule) *spanIndex {
	idx := &spanIndex{byPath: map[string][]citedSpan{}, hasRules: map[string]bool{}}
	for _, r := range placed {
		idx.hasRules[r.concept] = true
		for _, src := range r.sources {
			if s, ok := extract.SpanOf(src); ok {
				idx.byPath[s.Path] = append(idx.byPath[s.Path], citedSpan{s, r.concept})
			}
		}
	}
	return idx
}

// conceptsAt are the concepts with a rule citing code overlapping s,
// sorted and distinct.
func (idx *spanIndex) conceptsAt(s extract.Span) []string {
	var names []string
	for _, c := range idx.byPath[s.Path] {
		if c.span.Overlaps(s) {
			names = append(names, c.concept)
		}
	}
	slices.Sort(names)
	return slices.Compact(names)
}

func spanText(s extract.Span) string {
	return fmt.Sprintf("%s:%d-%d", s.Path, s.Start, s.End)
}
