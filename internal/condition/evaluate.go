package condition

import (
	"context"
	"encoding/json"
	"strings"

	ruleengine "github.com/faustbrian/go-rule-engine/v2"
)

// Outcome is what a condition says of one example.
type Outcome string

// The outcomes of Evaluate.
const (
	// Matches: the condition holds for the example.
	Matches Outcome = "matches"
	// DoesNotMatch: it does not hold.
	DoesNotMatch Outcome = "does_not_match"
	// Indeterminate: the example does not decide it: a variable the
	// condition compares is not given, or a value has the wrong type.
	Indeterminate Outcome = "indeterminate"
)

// IsOutcome reports whether s names an outcome an example may expect:
// Matches or DoesNotMatch.
func IsOutcome(s string) bool {
	return Outcome(s) == Matches || Outcome(s) == DoesNotMatch
}

// Evaluate decides c for one example, given its variables' values (as
// JSON decodes them: bool, number, string, or a list of those). A variable
// only tested with exists may be left out: that is the example saying it
// is absent.
//
// Requires: c is not the zero Condition.
// Ensures:  Indeterminate when a compared variable is missing from given
// or a value cannot be compared; never an error.
func (c *Condition) Evaluate(ctx context.Context, given map[string]any) Outcome {
	if c.IsZero() {
		return Indeterminate
	}
	for _, name := range c.compared() {
		if _, ok := given[name]; !ok {
			return Indeterminate
		}
	}
	facts := make([]ruleengine.Fact, 0, len(given))
	for name, raw := range given {
		path, err := ruleengine.NewPath(ruleengine.DefaultLimits(), strings.Split(name, ".")...)
		if err != nil {
			return Indeterminate
		}
		value, err := givenValue(raw)
		if err != nil {
			return Indeterminate
		}
		facts = append(facts, ruleengine.Fact{Path: path, Value: value})
	}
	snapshot, err := ruleengine.NewContext(facts...)
	if err != nil {
		return Indeterminate
	}
	switch c.plan.Evaluate(ctx, snapshot).Decision {
	case ruleengine.Matched:
		return Matches
	case ruleengine.Unmatched:
		return DoesNotMatch
	case ruleengine.Indeterminate:
	}
	return Indeterminate
}

// compared lists the variables c compares (var and var2), the ones an
// example must give.
func (c *Condition) compared() []string {
	var names []string
	c.walk(func(n *node) {
		names = append(names, n.Var, n.Var2)
	})
	out := names[:0]
	for _, name := range names {
		if name != "" {
			out = append(out, name)
		}
	}
	return out
}

// givenValue converts an example's value: a scalar, or a list of scalars.
func givenValue(raw any) (ruleengine.Value, error) {
	data, err := json.Marshal(raw)
	if err != nil {
		return ruleengine.Value{}, err
	}
	value, err := decodeLiteral(data)
	if err != nil {
		return ruleengine.Value{}, err
	}
	return engineValue(value)
}
