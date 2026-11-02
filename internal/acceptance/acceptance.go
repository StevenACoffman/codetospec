// Package acceptance writes the acceptance facts EARS does not carry, for
// the rules an LLM wrote (--acceptance): how an observer tells the rule's
// success from a plausible imitation (the observable: where to look, what
// to check, read from a test or the code), what the rule does not claim,
// and, for a rule with a condition, examples that test the condition at
// its boundaries (IMPROVEMENTS #5).
//
// One LLM call per rule reads its cited code and the nearest tests
// exercising it, cached by hash outside the map and reduce caches (no
// remap needed). What the reply states is then checked without an LLM:
// the observable's values against the code or tests it was read from
// (grounding), each example against the condition (go-rule-engine, see
// package condition). A failed call leaves the rule without facts, asked
// again on the next run, never a run failure.
package acceptance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/StevenACoffman/codetospec/internal/condition"
	"github.com/StevenACoffman/codetospec/internal/grounding"
	"github.com/StevenACoffman/codetospec/internal/llm"
)

// Channel is where an observer sees a rule's outcome.
type Channel string

// The channels.
const (
	Response Channel = "response" // what the caller gets back
	State    Channel = "state"    // what is stored
	Event    Channel = "event"    // what is emitted or sent
	Log      Channel = "log"      // what is logged
)

// Source is what an observable was read from.
type Source string

// The sources of an observable.
const (
	FromTest Source = "test" // a test's assertion
	FromCode Source = "code" // the cited code
)

// Limits of a reply: a few non-claims and examples are worth reading;
// more is padding.
const (
	maxNonClaims = 3
	maxExamples  = 4
)

// Facts are a rule's acceptance facts, as the model writes them.
type Facts struct {
	Observable Observable `json:"observable"`
	NonClaims  []string   `json:"non_claims"`
	Examples   []Example  `json:"examples"`
}

// Observable is how an observer tells the rule held.
type Observable struct {
	Channel Channel `json:"channel"`
	Check   string  `json:"check"`
	Source  Source  `json:"source"`
}

// Example is one row testing a condition: the values of its variables,
// and whether the condition should hold for them.
type Example struct {
	Given  map[string]any `json:"given"`
	Expect string         `json:"expect"`
}

// Validate reads a reply into Facts and checks them against the rule's
// condition c (zero when it has none) and whether tests were shown.
//
// Requires: nothing.
// Ensures:  an error names the offending field; returned Facts have a known
// channel and source, a non-empty check, at most maxNonClaims non-empty
// non-claims, and examples only when c is set, each naming only c's
// variables and expecting matches or does_not_match.
func Validate(reply string, c *condition.Condition, testsShown bool) (Facts, error) {
	dec := json.NewDecoder(strings.NewReader(llm.StripFences(reply)))
	dec.DisallowUnknownFields()
	dec.UseNumber()
	var f Facts
	if err := dec.Decode(&f); err != nil {
		return Facts{}, fmt.Errorf("invalid JSON: %w", err)
	}
	if err := f.Observable.check(testsShown); err != nil {
		return Facts{}, err
	}
	if len(f.NonClaims) > maxNonClaims {
		return Facts{}, fmt.Errorf(
			"non_claims has %d items, at most %d",
			len(f.NonClaims),
			maxNonClaims,
		)
	}
	for i, claim := range f.NonClaims {
		if strings.TrimSpace(claim) == "" {
			return Facts{}, fmt.Errorf("non_claims[%d] is empty", i)
		}
	}
	if err := checkExamples(f.Examples, c); err != nil {
		return Facts{}, err
	}
	return f, nil
}

// check validates an observable.
func (o *Observable) check(testsShown bool) error {
	switch o.Channel {
	case Response, State, Event, Log:
	default:
		return fmt.Errorf("observable.channel %q is not one of response|state|event|log", o.Channel)
	}
	switch o.Source {
	case FromCode:
	case FromTest:
		if !testsShown {
			return errors.New(`observable.source is "test" but no TESTS were shown; use "code"`)
		}
	default:
		return fmt.Errorf("observable.source %q is not one of test|code", o.Source)
	}
	if strings.TrimSpace(o.Check) == "" {
		return errors.New("observable.check is empty")
	}
	return nil
}

// checkExamples validates examples against the condition they test.
func checkExamples(examples []Example, c *condition.Condition) error {
	if c.IsZero() {
		if len(examples) > 0 {
			return errors.New(`examples must be [] when CONDITION is "-"`)
		}
		return nil
	}
	if len(examples) > maxExamples {
		return fmt.Errorf("examples has %d items, at most %d", len(examples), maxExamples)
	}
	variables := c.Variables()
	for i, ex := range examples {
		if !condition.IsOutcome(ex.Expect) {
			return fmt.Errorf("examples[%d].expect %q is not matches|does_not_match", i, ex.Expect)
		}
		for name := range ex.Given {
			if !slices.Contains(variables, name) {
				return fmt.Errorf("examples[%d].given names %q, not a CONDITION variable (%s)",
					i, name, strings.Join(variables, ", "))
			}
		}
	}
	return nil
}

// Assessment is what the checks without an LLM say of a rule's Facts.
type Assessment struct {
	// Grounding is whether the observable's values occur in what it was
	// read from: the cited code, or the tests.
	Grounding grounding.Status
	// Held counts the examples whose expectation the condition meets;
	// Contradicted lists, 1-based, those it contradicts. An example the
	// condition cannot decide (a variable not given) is neither.
	Held         int
	Contradicted []int
}

// Assess checks f without an LLM: the observable's values against the
// cited code (code) or the tests (tests), whichever it was read from, and
// each example against c.
//
// Requires: f passed Validate with c.
// Ensures:  Held + len(Contradicted) <= len(f.Examples).
func Assess(
	ctx context.Context,
	f *Facts,
	c *condition.Condition,
	code, tests []grounding.Citation,
	opts grounding.Options,
) Assessment {
	from := code
	if f.Observable.Source == FromTest {
		from = tests
	}
	a := Assessment{Grounding: grounding.Check(f.Observable.Check, from, opts).Status}
	for i, ex := range f.Examples {
		got := c.Evaluate(ctx, ex.Given)
		switch {
		case string(got) == ex.Expect:
			a.Held++
		case got != condition.Indeterminate:
			a.Contradicted = append(a.Contradicted, i+1)
		}
	}
	return a
}

// givenJSON renders an example's values on one line, keys sorted.
func givenJSON(given map[string]any) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(given); err != nil {
		return "{}"
	}
	return strings.TrimSpace(b.String())
}
