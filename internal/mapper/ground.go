package mapper

import (
	"context"
	"fmt"
	"strings"

	"github.com/StevenACoffman/codetospec/internal/condition"
	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/llm"
	"github.com/StevenACoffman/codetospec/internal/sitter"
)

// Ungrounded returns the values a requirement states that cited, the text of
// its cited lines, does not hold (see grounding.Check); none when grounded
// or when nothing in it is checkable.
type Ungrounded func(requirement string, cited []string) []string

// regroundRepairs bounds the structural corrections of the grounding repair
// exchange: one call, one correction at most.
const regroundRepairs = 1

// reground asks the model, once, to fix the rules of a valid reply that
// do not match their cited lines: a requirement or a condition stating a
// value the lines do not hold, or a condition that does not parse. It is
// never worse: the corrected reply is adopted only when it is valid, keeps
// every rule and has fewer such rules; otherwise payload is kept as is.
//
// Requires: payload passed validate; msgs is the conversation that produced
// it.
// Ensures:  returns the payload to keep, the usage of every call made
// (rejected ones included), and whether there was anything to correct.
func (m *Mapper) reground(
	ctx context.Context,
	msgs []llm.Message,
	payload mapPayload,
	chunk *sitter.Chunk,
	validate func(reply string) (mapPayload, error),
) (mapPayload, llm.Usage, bool) {
	var usage llm.Usage
	misses := m.misses(payload, chunk)
	if len(misses) == 0 {
		return payload, usage, false
	}
	conversation := append(append([]llm.Message(nil), msgs...),
		llm.Message{Role: "assistant", Content: mustJSON(payload)},
		llm.Message{Role: "user", Content: regroundRequest(misses)},
	)
	fixed, err := llm.ChatJSON(ctx, m.LLM, conversation, regroundRepairs,
		func(u llm.Usage) { usage.Add(u) }, validate)
	if err != nil || len(fixed.Rules) < len(payload.Rules) ||
		len(m.misses(fixed, chunk)) >= len(misses) {
		return payload, usage, true
	}
	return fixed, usage, true
}

// miss is one rule that does not match its cited lines.
type miss struct {
	rule   int      // index in the payload
	values []string // as written in the requirement
	lines  []string // the rule's citations, "a-b"
	// invalid is why the rule's condition does not parse; conditionValues
	// the values its condition compares with that the citations lack.
	invalid         string
	conditionValues []string
}

// misses checks every rule of payload against its cited lines in chunk:
// its requirement's and its condition's values when Ungrounded is set,
// and always whether its condition parses.
func (m *Mapper) misses(payload mapPayload, chunk *sitter.Chunk) []miss {
	lines := strings.Split(chunk.Content, "\n")
	var found []miss
	for i, rule := range payload.Rules {
		var cited, refs []string
		for _, c := range rule.Citations {
			a, b, err := extract.ParseLines(c.Lines)
			if err != nil || a < chunk.StartLine || b > chunk.EndLine {
				continue // validate rejects these; nothing to check
			}
			lo, hi := min(a-chunk.StartLine, len(lines)), min(b-chunk.StartLine+1, len(lines))
			cited = append(cited, lines[lo:hi]...)
			refs = append(refs, c.Lines)
		}
		mi := miss{rule: i, lines: refs}
		c, err := condition.Parse(rule.Condition)
		if err != nil {
			mi.invalid = err.Error()
		}
		if m.Ungrounded != nil {
			mi.values = m.Ungrounded(rule.Requirement, cited)
			if literals := c.Literals(); len(literals) > 0 {
				mi.conditionValues = m.Ungrounded(strings.Join(literals, " "), cited)
			}
		}
		if len(mi.values) > 0 || mi.invalid != "" || len(mi.conditionValues) > 0 {
			found = append(found, mi)
		}
	}
	return found
}

// regroundRequest is the correction naming each rule that does not match
// its cited lines, and why.
func regroundRequest(misses []miss) string {
	var b strings.Builder
	lines := func(mi *miss) string { return strings.Join(mi.lines, ", ") }
	for i := range misses {
		mi := &misses[i]
		if len(mi.values) > 0 {
			fmt.Fprintf(&b, "rules[%d] states %s, which its cited lines %s do not contain.\n",
				mi.rule, strings.Join(mi.values, ", "), lines(mi))
		}
		if mi.invalid != "" {
			fmt.Fprintf(&b, "rules[%d].condition is not valid: %s.\n", mi.rule, mi.invalid)
		}
		if len(mi.conditionValues) > 0 {
			fmt.Fprintf(
				&b,
				"rules[%d].condition compares with %s, which its cited lines %s do not contain.\n",
				mi.rule,
				strings.Join(mi.conditionValues, ", "),
				lines(mi),
			)
		}
	}
	b.WriteString("For each, cite the lines that hold the value, or remove the value " +
		"from the rule's ears components and condition; fix an invalid condition or " +
		"remove it. Keep every rule, change nothing else, and resend the full " +
		"corrected JSON only.")
	return b.String()
}
