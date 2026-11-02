package reducer

import (
	"encoding/json"
	"slices"

	"github.com/StevenACoffman/codetospec/internal/condition"
	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/mapper"
)

// settleConditions decides the condition of each rule a reduce reply
// wrote from candidates: when the candidates it merged (those whose
// citations it all kept) state one condition, with one hash, that one,
// whatever the model wrote; otherwise the model's, kept only when it
// parses. Conditions are returned in canonical form, with how many the
// model wrote that did not parse (dropped: a bad condition never costs
// its rule). The literals are grounded at build, where the cited lines
// are read.
//
// Requires: rules passed validateReduceReply against candidates.
// Ensures:  rules is not modified; every condition kept parses.
func settleConditions(rules []Rule, candidates []mapper.Rule) ([]Rule, int) {
	out := slices.Clone(rules)
	dropped := 0
	for i := range out {
		if shared, ok := mergedCondition(&out[i], candidates); ok {
			out[i].Condition = shared
			continue
		}
		c, err := condition.Parse(out[i].Condition)
		switch {
		case err != nil:
			out[i].Condition = nil
			dropped++
		case c.IsZero():
			out[i].Condition = nil
		default:
			out[i].Condition = json.RawMessage(c.Canonical())
		}
	}
	return out, dropped
}

// mergedCondition is the one condition every candidate rule merged states,
// canonical; false when it merged none, or they do not all state the same.
func mergedCondition(rule *Rule, candidates []mapper.Rule) (json.RawMessage, bool) {
	kept := make(map[string]bool, len(rule.Citations))
	for _, c := range rule.Citations {
		kept[citationKey(c)] = true
	}
	var shared condition.Condition
	merged := 0
	for i := range candidates {
		if !allKept(candidates[i].Citations, kept) {
			continue
		}
		c, err := condition.Parse(candidates[i].Condition)
		if err != nil || c.IsZero() || (merged > 0 && c.Hash() != shared.Hash()) {
			return nil, false
		}
		shared = c
		merged++
	}
	if merged == 0 {
		return nil, false
	}
	return json.RawMessage(shared.Canonical()), true
}

// allKept reports whether every citation of a candidate is among kept.
func allKept(citations []extract.Ref, kept map[string]bool) bool {
	for _, c := range citations {
		if !kept[citationKey(c)] {
			return false
		}
	}
	return len(citations) > 0
}

// sharedCondition is the condition two copies of one rule keep: theirs
// when both state the same (equal hash), else none.
func sharedCondition(a, b json.RawMessage) json.RawMessage {
	if conditionHash(a) == "" || conditionHash(a) != conditionHash(b) {
		return nil
	}
	return a
}

// conditionHash is the hash of a raw condition, "" when it is none or
// does not parse.
func conditionHash(raw json.RawMessage) string {
	c, err := condition.Parse(raw)
	if err != nil {
		return ""
	}
	return c.Hash()
}
