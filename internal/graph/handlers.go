package graph

import (
	"slices"

	"github.com/StevenACoffman/codetospec/internal/extract"
)

// handlerSpan is the code serving one endpoint (a route fact's
// handler_path and handler_lines).
type handlerSpan struct {
	path       string
	start, end int
	endpoint   string
}

// linkHandlers adds to each rule an exposed_by edge to every endpoint whose
// handler code one of its citations overlaps: the rule describes how that
// endpoint behaves. Mechanical, so it holds on a warm map cache, where the
// LLM is never asked again which endpoints a chunk serves.
func linkHandlers(rules []Node, facts []extract.Fact) {
	spans := handlerSpans(facts)
	if len(spans) == 0 {
		return
	}
	for i := range rules {
		for _, src := range rules[i].Sources {
			a, b, err := extract.ParseLines(src.Lines)
			if err != nil {
				continue
			}
			for _, s := range spans {
				edge := Edge{Type: EdgeExposedBy, To: s.endpoint}
				// Handlers are in --src: a citation of another root is not.
				if src.Root == "" && s.path == src.Path && a <= s.end && s.start <= b &&
					!slices.Contains(rules[i].Edges, edge) {
					rules[i].Edges = append(rules[i].Edges, edge)
				}
			}
		}
		sortEdges(rules[i].Edges)
	}
}

// handlerSpans lists the handler code of the route facts that give one.
func handlerSpans(facts []extract.Fact) []handlerSpan {
	var spans []handlerSpan
	for _, f := range facts {
		path, lines := f.Attrs[extract.AttrHandlerPath], f.Attrs[extract.AttrHandlerLines]
		if f.Kind != extract.KindRoute || path == "" || lines == "" {
			continue
		}
		if a, b, err := extract.ParseLines(lines); err == nil {
			spans = append(
				spans,
				handlerSpan{path: path, start: a, end: b, endpoint: EndpointID(&f)},
			)
		}
	}
	return spans
}
