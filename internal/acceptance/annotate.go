package acceptance

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/StevenACoffman/codetospec/internal/condition"
	"github.com/StevenACoffman/codetospec/internal/graph"
	"github.com/StevenACoffman/codetospec/internal/locale"
)

// Result is one rule's acceptance facts and their assessment.
type Result struct {
	Facts      Facts
	Assessment Assessment
}

// Annotate writes each rule's acceptance facts into its node: the body
// sections (observable, non-claims, examples, labelled in text's
// language) and the frontmatter (graph.ExtraObservable and the keys after
// it). A rule without a result is left as is.
//
// Requires: results keyed by rule id.
// Ensures:  nodes is not modified.
func Annotate(nodes []graph.Node, results map[string]Result, text *locale.Text) []graph.Node {
	out := slices.Clone(nodes)
	for i := range out {
		r, ok := results[out[i].ID]
		if !ok {
			continue
		}
		out[i].Body = out[i].Body + "\n\n" + body(&r, text)
		out[i].Extra = maps.Clone(out[i].Extra)
		if out[i].Extra == nil {
			out[i].Extra = map[string]string{}
		}
		extra := out[i].Extra
		extra[graph.ExtraObservable] = string(r.Facts.Observable.Channel)
		extra[graph.ExtraObservableSource] = string(r.Facts.Observable.Source)
		extra[graph.ExtraObservableGrounding] = string(r.Assessment.Grounding)
		if n := len(r.Facts.Examples); n > 0 {
			extra[graph.ExtraExamples] = fmt.Sprintf("%d/%d", r.Assessment.Held, n)
		}
		if len(r.Assessment.Contradicted) > 0 {
			numbers := make([]string, len(r.Assessment.Contradicted))
			for j, n := range r.Assessment.Contradicted {
				numbers[j] = strconv.Itoa(n)
			}
			extra[graph.ExtraExamplesContradicted] = strings.Join(numbers, ", ")
		}
	}
	return out
}

// body renders a rule's acceptance sections, in text's language.
func body(r *Result, text *locale.Text) string {
	var b strings.Builder
	o := r.Facts.Observable
	fmt.Fprintf(&b, "%s(%s, %s) %s", text.Observable, o.Channel, o.Source, o.Check)
	if len(r.Facts.NonClaims) > 0 {
		b.WriteString("\n\n" + text.NonClaims)
		for _, claim := range r.Facts.NonClaims {
			b.WriteString("\n- " + claim)
		}
	}
	if len(r.Facts.Examples) > 0 {
		b.WriteString("\n\n" + text.Examples)
		for i, ex := range r.Facts.Examples {
			expect := text.ExpectMatches
			if condition.Outcome(ex.Expect) == condition.DoesNotMatch {
				expect = text.ExpectNoMatch
			}
			b.WriteString("\n" + fmt.Sprintf(text.ExampleFmt, givenJSON(ex.Given), expect))
			if slices.Contains(r.Assessment.Contradicted, i+1) {
				b.WriteString(text.ExampleBroken)
			}
		}
	}
	return b.String()
}
