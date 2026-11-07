package grounding

import (
	"encoding/json"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/StevenACoffman/codetospec/internal/condition"
	"github.com/StevenACoffman/codetospec/internal/graph"
)

// checkCondition checks a rule's condition against its cited code and its
// EARS text, and returns the rule's frontmatter accordingly (a new map):
//   - a condition comparing with a value its cited lines do not hold is
//     removed, graph.ExtraConditionDropped saying which values;
//   - a kept condition whose EARS conditions (where, while, when, if)
//     state values it doesn't gets graph.ExtraConditionDisagreement: those
//     values, each marked cited or not (whether the cited lines hold it).
//     Neither side is changed: a person decides.
//
// Requires: citations are n's, loaded.
// Ensures:  n.Extra is not modified; without a condition, Extra as is.
func checkCondition(n *graph.Node, citations []Citation, opts Options) map[string]string {
	c := graph.RuleCondition(n)
	if c.IsZero() {
		return n.Extra
	}
	extra := maps.Clone(n.Extra)
	delete(extra, graph.ExtraConditionDisagreement) // recomputed below
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
	if detail := disagreement(n, &c, values, cited, opts); detail != "" {
		extra[graph.ExtraConditionDisagreement] = detail
	}
	return extra
}

// disagreement describes the values a rule's EARS conditions state that
// its condition c, whose values are given, doesn't; "" when there are none
// or the sentence does not parse. Only the text's side is a question: the
// condition's values are grounded in the cited code already, so a value
// the text leaves out only says the text is vaguer. A text value counts
// as stated when it is one of c's values, a value the extractor carries
// beside the condition (check_args), part of one of c's variable names
// (caller.hasPermission.CanModerateUsers), or a constant's value a
// variable names (opts.Constants: retry.MaxAttempts for 3).
func disagreement(
	n *graph.Node,
	c *condition.Condition,
	values []literal,
	cited []string,
	opts Options,
) string {
	components, ok := graph.RuleComponents(n)
	if !ok {
		return ""
	}
	clauses := append(append(append([]string(nil), components.Where...), components.While...),
		components.When, components.If)
	text := strings.TrimSpace(strings.Join(clauses, "\n"))
	if text == "" {
		return ""
	}
	written := append(carriedValues(n), c.Variables()...)
	for i := range values {
		written = append(written, values[i].text)
	}
	var parts []string
	for _, lit := range extractLiterals(text, opts.DecimalSeparator) {
		if !lit.foundIn(written, opts.Constants) {
			where := "not cited"
			if lit.foundIn(cited, opts.Constants) {
				where = "cited"
			}
			parts = append(parts, "text "+lit.text+" ("+where+")")
		}
	}
	return strings.Join(parts, "; ")
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

// carriedValues are the values an extractor carries beside a rule's
// condition, in graph.ExtraCheckArgs (JSON): every string in it, whatever
// its shape, but those of labels (labels), sorted. None when absent or
// malformed.
func carriedValues(n *graph.Node) []string {
	raw := n.Extra[graph.ExtraCheckArgs]
	if raw == "" {
		return nil
	}
	var decoded any
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return nil
	}
	var out []string
	var walk func(v any)
	walk = func(v any) {
		switch x := v.(type) {
		case string:
			out = append(out, x)
		case []any:
			for _, e := range x {
				walk(e)
			}
		case map[string]any:
			for k, e := range x {
				if !slices.Contains(labels(), k) {
					walk(e)
				}
			}
		}
	}
	walk(decoded)
	slices.Sort(out)
	return out
}

// labels are the check_args keys whose strings describe a call (the
// variable it became, what it means) rather than carry a value of the
// code.
func labels() []string {
	return []string{"var", "means", "scope"}
}
