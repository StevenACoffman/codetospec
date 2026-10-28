// Package callers places code in the domain of the code that uses it: a
// helper serves the operations of its package that call it. It reads call
// facts (caller and callee definition spans, from an extractor) and
// reassigns chunk domains, callers before callees.
package callers

import (
	"path"
	"slices"
	"sort"

	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/sitter"
)

// Reassign returns chunks with each chunk an extractor did not place
// (hinted false) moved into the domain all its callers agree on. A chunk's
// callers are the chunks of its own directory where the definitions
// calling a definition it holds begin: a caller elsewhere uses a package,
// whose domain stands (library code called from a main package, or a
// shared package whose production callers live in other modules). Callers
// are decided first, so a helper of a helper follows the operation at the
// top; a chunk whose callers disagree, or that nobody calls, keeps its
// domain, and so does code in a call cycle.
//
// Requires: chunks sorted by path, then start line; hinted(path, line)
// reports an extractor domain fact covering line.
// Ensures:  only Domain changes, each chunk's at most once; the result
// depends on neither fact nor map order.
func Reassign(
	chunks []sitter.Chunk,
	facts []extract.Fact,
	hinted func(path string, line int) bool,
) []sitter.Chunk {
	out := slices.Clone(chunks)
	callers := callerSets(out, facts)
	pending := make([]int, len(out)) // callers of each chunk not yet decided
	callees := make([][]int, len(out))
	for callee, set := range callers {
		pending[callee] = len(set)
		for caller := range set {
			callees[caller] = append(callees[caller], callee)
		}
	}
	var ready []int
	for i := range out {
		if pending[i] == 0 {
			ready = append(ready, i)
		}
	}
	for len(ready) > 0 {
		sort.Ints(ready)
		i := ready[0]
		ready = ready[1:]
		if domain, agreed := agreedDomain(out, callers[i]); agreed &&
			!hinted(out[i].Path, out[i].StartLine) {
			out[i].Domain = domain
		}
		for _, callee := range callees[i] {
			pending[callee]--
			if pending[callee] == 0 {
				ready = append(ready, callee)
			}
		}
	}
	return out
}

// callerSets maps each chunk to the chunks of its directory calling into
// it, itself excluded. A call through an interface (Fact.ViaDispatch) is
// not a caller: which implementation runs is not known, and placing code
// by a possible caller would move domains on a guess.
func callerSets(chunks []sitter.Chunk, facts []extract.Fact) map[int]map[int]bool {
	byPath := map[string][]int{}
	for i := range chunks {
		byPath[chunks[i].Path] = append(byPath[chunks[i].Path], i)
	}
	sets := map[int]map[int]bool{}
	for i := range facts {
		f := &facts[i]
		if f.Kind != extract.KindCall || f.ViaDispatch() || f.Source.Root != "" {
			continue
		}
		callerStart, _, err := extract.ParseLines(f.Source.Lines)
		if err != nil {
			continue
		}
		caller := chunkAt(chunks, byPath[f.Source.Path], callerStart)
		start, end, err := extract.ParseLines(f.Attrs[extract.AttrCalleeLines])
		if caller < 0 || err != nil {
			continue
		}
		for _, callee := range chunksIn(chunks, byPath[f.Attrs[extract.AttrCalleePath]], start, end) {
			if callee == caller || path.Dir(chunks[callee].Path) != path.Dir(chunks[caller].Path) {
				continue
			}
			if sets[callee] == nil {
				sets[callee] = map[int]bool{}
			}
			sets[callee][caller] = true
		}
	}
	return sets
}

// chunkAt is the chunk among inFile (indexes of one file's chunks) holding
// line, -1 when none does.
func chunkAt(chunks []sitter.Chunk, inFile []int, line int) int {
	for _, i := range inFile {
		if chunks[i].StartLine <= line && line <= chunks[i].EndLine {
			return i
		}
	}
	return -1
}

// chunksIn lists the chunks among inFile (indexes of one file's chunks)
// that a definition spanning start-end holds: those beginning inside it,
// or, for a definition within one chunk, that chunk.
func chunksIn(chunks []sitter.Chunk, inFile []int, start, end int) []int {
	var held []int
	for _, i := range inFile {
		if start <= chunks[i].StartLine && chunks[i].StartLine <= end {
			held = append(held, i)
		}
	}
	if len(held) == 0 {
		if i := chunkAt(chunks, inFile, start); i >= 0 {
			held = append(held, i)
		}
	}
	return held
}

// agreedDomain is the one domain of the callers, false when they disagree
// or there are none.
func agreedDomain(chunks []sitter.Chunk, callers map[int]bool) (string, bool) {
	agreed := ""
	for caller := range callers {
		switch d := chunks[caller].Domain; {
		case agreed == "":
			agreed = d
		case d != agreed:
			return "", false
		}
	}
	return agreed, agreed != ""
}
