package testreach

import (
	"strconv"
	"strings"

	"github.com/StevenACoffman/codetospec/internal/extract"
)

// fileRun is what a profiled test run executed in one file.
type fileRun struct {
	executed   map[int]bool // lines the run executed
	statements map[int]bool // lines holding a statement, executed or not
}

// runs indexes the executed facts of --src by file; nil without any.
type runs map[string]fileRun

// runsOf reads the executed facts of --src.
func runsOf(facts []extract.Fact) runs {
	var byFile runs
	for i := range facts {
		f := &facts[i]
		if f.Kind != extract.KindExecuted || f.Source.Root != "" {
			continue
		}
		run := fileRun{executed: map[int]bool{}, statements: map[int]bool{}}
		addLines(run.executed, f.Attrs[extract.AttrExecuted])
		addLines(run.statements, f.Attrs[extract.AttrExecuted])
		addLines(run.statements, f.Attrs[extract.AttrUnexecuted])
		if byFile == nil {
			byFile = runs{}
		}
		byFile[f.Source.Path] = run
	}
	return byFile
}

// addLines adds the lines of comma-separated "a-b" spans to set; a
// malformed span is skipped.
func addLines(set map[int]bool, spans string) {
	for span := range strings.SplitSeq(spans, ",") {
		start, end, err := extract.ParseLines(span)
		if err != nil {
			continue
		}
		for line := start; line <= end; line++ {
			set[line] = true
		}
	}
}

// executed is how many statement lines of sources the run executed, "k/n".
//
// Requires: sources are a rule's citations.
// Ensures:  false when a source of --src lies in a file the run has no
// record of (unknown, not unexecuted), when the sources hold no statement
// line of --src (a type, a constant, another root), or when the citation
// is malformed; a line two sources share counts once.
func (r runs) executed(sources []extract.Ref) (string, bool) {
	type fileLine struct {
		path string
		line int
	}
	cited := map[fileLine]bool{}
	ran := 0
	for _, src := range sources {
		if src.Root != "" {
			continue
		}
		run, recorded := r[src.Path]
		start, end, err := extract.ParseLines(src.Lines)
		if !recorded || err != nil {
			return "", false
		}
		for line := start; line <= end; line++ {
			at := fileLine{src.Path, line}
			if !run.statements[line] || cited[at] {
				continue
			}
			cited[at] = true
			if run.executed[line] {
				ran++
			}
		}
	}
	if len(cited) == 0 {
		return "", false
	}
	return strconv.Itoa(ran) + "/" + strconv.Itoa(len(cited)), true
}
