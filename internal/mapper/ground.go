package mapper

import (
	"context"
	"fmt"
	"strings"

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

// reground asks the model, once, to fix the rules of a valid reply whose
// requirements state values their cited lines do not hold. It is never
// worse: the corrected reply is adopted only when it is valid, keeps every
// rule and has fewer ungrounded rules; otherwise payload is kept as is.
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

// miss is one rule whose requirement states values its citations lack.
type miss struct {
	rule   int      // index in the payload
	values []string // as written in the requirement
	lines  []string // the rule's citations, "a-b"
}

// misses checks every rule of payload against its cited lines in chunk.
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
			cited = append(cited, lines[a-chunk.StartLine:min(b-chunk.StartLine+1, len(lines))]...)
			refs = append(refs, c.Lines)
		}
		if values := m.Ungrounded(rule.Requirement, cited); len(values) > 0 {
			found = append(found, miss{rule: i, values: values, lines: refs})
		}
	}
	return found
}

// regroundRequest is the correction naming each ungrounded rule.
func regroundRequest(misses []miss) string {
	var b strings.Builder
	for _, mi := range misses {
		fmt.Fprintf(&b, "rules[%d] states %s, which its cited lines %s do not contain.\n",
			mi.rule, strings.Join(mi.values, ", "), strings.Join(mi.lines, ", "))
	}
	b.WriteString("For each, cite the lines that hold the value, or remove the value " +
		"from the rule's ears components. Keep every rule, change nothing else, and " +
		"resend the full corrected JSON only.")
	return b.String()
}
