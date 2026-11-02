package graph

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/locale"
)

// journeyDepth is how many calls deep a journey follows the handler: as
// for test reach (testreach.MaxDepth), deeper code is shared plumbing
// more than this endpoint's behavior.
const journeyDepth = 3

// journeySeparator joins ExtraJourney's rule ids.
const journeySeparator = ", "

// withJourneys returns nodes with, on each endpoint whose handler code a
// route fact gives, its journey: the rules whose cited code the handler
// reaches through direct call facts, in the order a request meets them —
// the handler's own rules first, then those of what it calls, nearest
// first — recorded in ExtraJourney and listed in the body with their
// requirements. The derivable part of a user journey: the code does not
// say which endpoint a person calls next.
//
// Requires: rules carry their final ids and citations of the build.
// Ensures:  nodes is not modified; an endpoint no rule's code is reached
// from gets nothing; the order is deterministic (depth, cited file and
// line, rule id).
func withJourneys(nodes, rules []Node, facts []extract.Fact, text *locale.Text) []Node {
	spans := handlerSpans(facts)
	if len(spans) == 0 {
		return nodes
	}
	calls := directCalls(facts)
	out := slices.Clone(nodes)
	for i := range out {
		if out[i].Type != TypeEndpoint {
			continue
		}
		var starts []span
		for _, s := range spans {
			if s.endpoint == out[i].ID {
				starts = append(starts, span{path: s.path, start: s.start, end: s.end})
			}
		}
		journey := journeyRules(rules, reachedFrom(starts, calls))
		if len(journey) == 0 {
			continue
		}
		ids := make([]string, len(journey))
		var list strings.Builder
		for k, r := range journey {
			ids[k] = r.ID
			fmt.Fprintf(&list, "\n%d. `%s`: %s", k+1, r.ID, RuleRequirement(r.Body))
		}
		out[i].Body += "\n\n" + text.Journey + list.String()
		out[i].Extra = maps.Clone(out[i].Extra)
		if out[i].Extra == nil {
			out[i].Extra = map[string]string{}
		}
		out[i].Extra[ExtraJourney] = strings.Join(ids, journeySeparator)
	}
	return out
}

// span is a definition's lines in a file of --src.
type span struct {
	path       string
	start, end int
}

func (s span) overlaps(path string, start, end int) bool {
	return s.path == path && start <= s.end && s.start <= end
}

// call is a caller definition and the definition it calls.
type call struct{ caller, callee span }

// directCalls reads the call facts of --src, those through an interface
// left out: their implementations include test doubles (as for drift's
// callees).
func directCalls(facts []extract.Fact) []call {
	var calls []call
	for i := range facts {
		f := &facts[i]
		if f.Kind != extract.KindCall || f.Source.Root != "" || f.ViaDispatch() {
			continue
		}
		a, b, errCaller := extract.ParseLines(f.Source.Lines)
		c, d, errCallee := extract.ParseLines(f.Attrs[extract.AttrCalleeLines])
		if errCaller != nil || errCallee != nil {
			continue
		}
		calls = append(calls, call{
			caller: span{f.Source.Path, a, b},
			callee: span{f.Attrs[extract.AttrCalleePath], c, d},
		})
	}
	return calls
}

// reachedFrom is every definition reached from starts within
// journeyDepth calls, at its least depth (0 for the handler code).
func reachedFrom(starts []span, calls []call) map[span]int {
	depth := map[span]int{}
	frontier := starts
	for d := 0; d <= journeyDepth && len(frontier) > 0; d++ {
		var next []span
		for _, s := range frontier {
			if _, seen := depth[s]; seen {
				continue
			}
			depth[s] = d
			for _, c := range calls {
				if c.caller.overlaps(s.path, s.start, s.end) {
					next = append(next, c.callee)
				}
			}
		}
		frontier = next
	}
	return depth
}

// journeyRules lists the rules citing reached code, ordered by the depth
// of the nearest reached definition they cite, then by their first
// citation, then by id.
func journeyRules(rules []Node, reached map[span]int) []*Node {
	type step struct {
		rule  *Node
		depth int
		at    string // first citation, path then zero-padded line
	}
	var steps []step
	for i := range rules {
		least := -1
		for _, src := range rules[i].Sources {
			a, b, err := extract.ParseLines(src.Lines)
			if src.Root != "" || err != nil {
				continue
			}
			for s, d := range reached {
				if s.overlaps(src.Path, a, b) && (least < 0 || d < least) {
					least = d
				}
			}
		}
		if least >= 0 {
			steps = append(steps, step{&rules[i], least, firstCitation(rules[i].Sources)})
		}
	}
	slices.SortFunc(steps, func(a, b step) int {
		return cmp.Or(cmp.Compare(a.depth, b.depth), cmp.Compare(a.at, b.at),
			cmp.Compare(a.rule.ID, b.rule.ID))
	})
	out := make([]*Node, len(steps))
	for k, s := range steps {
		out[k] = s.rule
	}
	return out
}

// firstCitation is a sortable key of the earliest citation of sources:
// its file, then its first line zero-padded.
func firstCitation(sources []extract.Ref) string {
	keys := make([]string, 0, len(sources))
	for _, src := range sources {
		a, _, err := extract.ParseLines(src.Lines)
		if err != nil {
			continue
		}
		keys = append(keys, src.File()+"\x00"+fmt.Sprintf("%09d", a))
	}
	slices.Sort(keys)
	if len(keys) == 0 {
		return ""
	}
	return keys[0]
}
