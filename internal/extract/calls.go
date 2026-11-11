package extract

// Span is a definition's lines in a file of --src, 1-based inclusive.
type Span struct {
	Path       string
	Start, End int
}

// Overlaps reports whether s and o share a line of the same file.
func (s Span) Overlaps(o Span) bool {
	return s.Path == o.Path && s.Start <= o.End && o.Start <= s.End
}

// SpanOf is r's span; false for another root or unreadable lines.
func SpanOf(r Ref) (Span, bool) {
	start, end, err := ParseLines(r.Lines)
	if r.Root != "" || err != nil {
		return Span{}, false
	}
	return Span{Path: r.Path, Start: start, End: end}, true
}

// Call is a caller definition and the definition it calls.
type Call struct{ Caller, Callee Span }

// DirectCalls reads the call facts of --src, leaving out those through
// an interface: their implementations include test doubles outside
// _test.go files.
//
// Ensures: in fact order; a fact with unreadable lines is skipped.
func DirectCalls(facts []Fact) []Call {
	var calls []Call
	for i := range facts {
		f := &facts[i]
		if f.Kind != KindCall || f.ViaDispatch() {
			continue
		}
		caller, okCaller := SpanOf(f.Source)
		callee, okCallee := SpanOf(
			Ref{Path: f.Attrs[AttrCalleePath], Lines: f.Attrs[AttrCalleeLines]},
		)
		if okCaller && okCallee {
			calls = append(calls, Call{Caller: caller, Callee: callee})
		}
	}
	return calls
}
