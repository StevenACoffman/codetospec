package grounding

import (
	"maps"
	"strconv"
	"strings"

	"github.com/StevenACoffman/codetospec/internal/condition"
	"github.com/StevenACoffman/codetospec/internal/graph"
)

// checkCondition checks a rule's condition against its cited code and its
// EARS text, and returns the rule's frontmatter accordingly (a new map):
//   - a condition comparing with a value its cited lines do not hold is
//     removed, graph.ExtraConditionDropped saying which values;
//   - a kept condition that disagrees with the EARS conditions (where,
//     while, when, if) gets graph.ExtraConditionDisagreement: the values
//     one side states and the other does not, each marked cited or not
//     (whether the cited lines hold it), or a condition on a rule whose
//     text states none. Neither side is changed: a person decides.
//
// Requires: citations are n's, loaded.
// Ensures:  n.Extra is not modified; without a condition, Extra as is.
func checkCondition(n *graph.Node, citations []Citation, opts Options) map[string]string {
	c := graph.RuleCondition(n)
	if c.IsZero() {
		return n.Extra
	}
	extra := maps.Clone(n.Extra)
	cited := citedLines(citations)
	values := conditionLiterals(&c)
	var missing []string
	for i := range values {
		if !values[i].foundIn(cited, opts.Constants) {
			missing = append(missing, values[i].text)
		}
	}
	if len(missing) > 0 {
		delete(extra, graph.ExtraCondition)
		delete(extra, graph.ExtraConditionHash)
		extra[graph.ExtraConditionDropped] = strings.Join(missing, "; ")
		return extra
	}
	if detail := disagreement(n, values, cited, opts); detail != "" {
		extra[graph.ExtraConditionDisagreement] = detail
	}
	return extra
}

// disagreement describes how a rule's condition, whose values are given,
// and its EARS conditions differ; "" when they agree or the sentence does
// not parse.
func disagreement(n *graph.Node, values []literal, cited []string, opts Options) string {
	components, ok := graph.RuleComponents(n)
	if !ok {
		return ""
	}
	clauses := append(append(append([]string(nil), components.Where...), components.While...),
		components.When, components.If)
	text := strings.TrimSpace(strings.Join(clauses, "\n"))
	if text == "" {
		return "text none"
	}
	where := func(lit *literal) string {
		if lit.foundIn(cited, opts.Constants) {
			return "cited"
		}
		return "not cited"
	}
	var parts []string
	var written []string
	for i := range values {
		written = append(written, values[i].text)
		if !values[i].implicit() && !values[i].foundIn([]string{text}, nil) {
			parts = append(parts, "condition "+values[i].text+" ("+where(&values[i])+")")
		}
	}
	for _, lit := range extractLiterals(text, opts.DecimalSeparator) {
		if !lit.foundIn(written, nil) {
			parts = append(parts, "text "+lit.text+" ("+where(&lit)+")")
		}
	}
	return strings.Join(parts, "; ")
}

// implicit reports whether a condition value is one prose states in
// words rather than digits or quotes: zero ("negative", "positive") and
// the empty string ("empty", "missing"). Its absence from the EARS text is
// no disagreement.
func (l *literal) implicit() bool {
	if l.number {
		return l.value == 0
	}
	return l.folded == ""
}

// conditionLiterals reads c's values as literals: numbers as JSON writes
// them (a dot before decimals, whatever the output language), strings
// quoted.
func conditionLiterals(c *condition.Condition) []literal {
	var out []literal
	for _, v := range c.Literals() {
		if inner, err := strconv.Unquote(v); err == nil {
			if lit, ok := quoted(v, inner); ok {
				out = append(out, lit)
			}
			continue
		}
		if value, err := strconv.ParseFloat(v, 64); err == nil {
			out = append(out, literal{text: v, number: true, value: value})
		}
	}
	return out
}

// citedLines are the lines of every citation's range.
func citedLines(citations []Citation) []string {
	var cited []string
	for _, c := range citations {
		cited = append(cited, c.File[c.Start-1:c.End]...)
	}
	return cited
}
