// Package condition reads, compares and evaluates the structured condition
// of a rule: the checks the code makes before the rule's response, as a
// tree of comparisons over named variables. A rule's EARS text says the
// same in words; the condition is what a machine can hash and evaluate.
//
// Conditions are written in a compact JSON notation that the map, the
// reduce and the extractors share (the extract.AttrCondition contract):
//
//	{"all": [c, …]}   {"any": [c, …]}   {"not": c}
//	{"exists": "a.b"}
//	{"var": "a.b", "op": "<operator>", "value": <literal>}
//	{"var": "a.b", "op": "<operator>", "var2": "c.d"}
//
// The operators are go-rule-engine's built-in ones but "matches" (a
// regular expression cannot be grounded in the cited code). A literal is a
// JSON bool, number or string, or for "in" and "not_in" a list of them;
// every number is compared as a float, so 3 and 3.0 are the same.
// Each condition compiles into a go-rule-engine plan
// (github.com/faustbrian/go-rule-engine/v2), which owns the semantics, the
// canonical form behind Hash, and evaluation; none of its types cross this
// package's API.
package condition

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	ruleengine "github.com/faustbrian/go-rule-engine/v2"
)

// maxDepth bounds the nesting of a condition: deeper ones are not what
// code checks before one response, and the engine bounds depth too.
const maxDepth = 16

// ruleID names the one rule of the set a condition compiles into.
const ruleID = "condition"

// Condition is a parsed rule condition. The zero Condition is none: the
// rule's response is unconditional.
type Condition struct {
	root      *node
	canonical string
	hash      string
	plan      ruleengine.Plan
}

// node is one element of the notation, as decoded: exactly one shape is
// set (see validate).
type node struct {
	All    []node          `json:"all,omitempty"`
	Any    []node          `json:"any,omitempty"`
	Not    *node           `json:"not,omitempty"`
	Exists string          `json:"exists,omitempty"`
	Var    string          `json:"var,omitempty"`
	Op     string          `json:"op,omitempty"`
	Value  json.RawMessage `json:"value,omitempty"`
	Var2   string          `json:"var2,omitempty"`
}

// Parse reads a condition in the compact notation. Empty input, or JSON
// null, is the zero Condition.
//
// Requires: nothing.
// Ensures:  an error names the offending element by its JSON path (e.g.
// "all[1].op"); a returned Condition compiles, so Hash and Evaluate never
// fail on it.
func Parse(raw []byte) (Condition, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return Condition{}, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var root node
	if err := dec.Decode(&root); err != nil {
		return Condition{}, fmt.Errorf("condition: %w", err)
	}
	if dec.More() {
		return Condition{}, errors.New("condition: trailing data after the JSON object")
	}
	predicate, err := root.predicate("condition", 0)
	if err != nil {
		return Condition{}, err
	}
	set := ruleengine.RuleSet{
		ID:    ruleID,
		Rules: []ruleengine.Rule{{ID: ruleID, When: predicate}},
	}
	plan, diagnostics, err := ruleengine.NewCompiler(ruleengine.DefaultLimits()).
		Compile(context.Background(), set)
	if err != nil {
		return Condition{}, fmt.Errorf("condition: %w%s", err, describe(diagnostics))
	}
	hash, err := ruleengine.CanonicalHash(set)
	if err != nil {
		return Condition{}, fmt.Errorf("condition: %w", err)
	}
	canonical, err := json.Marshal(root.normalized())
	if err != nil {
		return Condition{}, fmt.Errorf("condition: %w", err)
	}
	return Condition{root: &root, canonical: string(canonical), hash: hash, plan: plan}, nil
}

// describe renders the engine's diagnostics after an error, "" when none.
func describe(diagnostics []ruleengine.Diagnostic) string {
	parts := make([]string, 0, len(diagnostics))
	for _, d := range diagnostics {
		parts = append(parts, fmt.Sprintf("%v", d))
	}
	if len(parts) == 0 {
		return ""
	}
	return " (" + strings.Join(parts, "; ") + ")"
}

// IsZero reports whether c is no condition.
func (c *Condition) IsZero() bool { return c.root == nil }

// Canonical is c in the compact notation, one line, fields in a fixed
// order: what the rule frontmatter stores. "" for the zero Condition.
func (c *Condition) Canonical() string { return c.canonical }

// Hash is go-rule-engine's canonical hash of c: two conditions that check
// the same thing the same way hash equal however their JSON is spaced or
// ordered. "" for the zero Condition.
func (c *Condition) Hash() string { return c.hash }

// Variables lists the variables c reads, sorted, each once.
func (c *Condition) Variables() []string {
	var names []string
	c.walk(func(n *node) {
		names = append(names, n.Exists, n.Var, n.Var2)
	})
	names = slices.DeleteFunc(names, func(s string) bool { return s == "" })
	slices.Sort(names)
	return slices.Compact(names)
}

// Literals lists the values c compares with, as the cited code must state
// them: each number as written, each string quoted ("admin"), booleans
// left out (code states a check, not the word true). Sorted, each once.
func (c *Condition) Literals() []string {
	var values []string
	c.walk(func(n *node) {
		if len(n.Value) > 0 {
			values = appendLiterals(values, n.Value)
		}
	})
	slices.Sort(values)
	return slices.Compact(values)
}

// Required maps each variable c can match only with a given boolean
// value to that value: a conjunct of c (c itself, or a part of its
// top-level all, nested alls flattened) that is "var equal true|false",
// negated by a not. A variable compared elsewhere (under any, against a
// non-boolean) is not in the map.
func (c *Condition) Required() map[string]bool {
	out := map[string]bool{}
	if c.root != nil {
		c.root.required(out, false)
	}
	return out
}

// required adds n's required booleans to out, each flipped when negated.
func (n *node) required(out map[string]bool, negated bool) {
	switch {
	case n.All != nil && !negated:
		for i := range n.All {
			n.All[i].required(out, false)
		}
	case n.Not != nil:
		n.Not.required(out, !negated)
	case n.Var != "" && n.Op == "equal" && n.Var2 == "":
		var b bool
		if json.Unmarshal(n.Value, &b) == nil {
			out[n.Var] = b != negated
		}
	}
}

// appendLiterals appends the numbers and strings of a valid literal.
func appendLiterals(values []string, raw json.RawMessage) []string {
	value, _ := decodeLiteral(raw)
	switch v := value.(type) {
	case json.Number:
		return append(values, v.String())
	case string:
		return append(values, strconv.Quote(v))
	case []any:
		for _, item := range v {
			data, _ := json.Marshal(item)
			values = appendLiterals(values, data)
		}
	}
	return values
}

// Renamed returns c with each variable replaced by rename's result.
//
// Requires: rename returns names made of non-empty dot-separated
// segments.
// Ensures:  an error only when a new name is not a valid variable; c is
// unchanged.
func (c *Condition) Renamed(rename func(string) string) (Condition, error) {
	if c.IsZero() {
		return Condition{}, nil
	}
	root := c.root.renamed(rename)
	data, err := json.Marshal(root)
	if err != nil {
		return Condition{}, fmt.Errorf("condition: %w", err)
	}
	return Parse(data)
}

// walk calls visit on every node of c, depth first.
func (c *Condition) walk(visit func(*node)) {
	if c.root != nil {
		c.root.walk(visit)
	}
}

func (n *node) walk(visit func(*node)) {
	visit(n)
	for i := range n.All {
		n.All[i].walk(visit)
	}
	for i := range n.Any {
		n.Any[i].walk(visit)
	}
	if n.Not != nil {
		n.Not.walk(visit)
	}
}

// renamed returns a copy of n with its variables renamed.
func (n *node) renamed(rename func(string) string) node {
	out := *n
	out.All = renamedAll(n.All, rename)
	out.Any = renamedAll(n.Any, rename)
	if n.Not != nil {
		not := n.Not.renamed(rename)
		out.Not = &not
	}
	for _, name := range []*string{&out.Exists, &out.Var, &out.Var2} {
		if *name != "" {
			*name = rename(*name)
		}
	}
	return out
}

func renamedAll(nodes []node, rename func(string) string) []node {
	if nodes == nil {
		return nil
	}
	out := make([]node, len(nodes))
	for i := range nodes {
		out[i] = nodes[i].renamed(rename)
	}
	return out
}

// normalized returns n with every literal in compact JSON, so Canonical
// does not keep the model's spacing.
func (n *node) normalized() node {
	out := *n
	out.All = normalizedAll(n.All)
	out.Any = normalizedAll(n.Any)
	if n.Not != nil {
		not := n.Not.normalized()
		out.Not = &not
	}
	if len(n.Value) > 0 {
		out.Value = canonicalLiteral(n.Value)
	}
	return out
}

// canonicalLiteral writes a valid literal compactly, each number in its
// shortest decimal form (1000.0 → 1000), as Hash sees it.
func canonicalLiteral(raw json.RawMessage) json.RawMessage {
	value, err := decodeLiteral(raw)
	if err != nil {
		return raw
	}
	data, err := json.Marshal(canonicalNumbers(value))
	if err != nil {
		return raw
	}
	return data
}

// canonicalNumbers rewrites the numbers of a decoded literal.
func canonicalNumbers(value any) any {
	switch v := value.(type) {
	case json.Number:
		if f, err := v.Float64(); err == nil {
			return json.Number(strconv.FormatFloat(f, 'f', -1, 64))
		}
	case []any:
		out := make([]any, len(v))
		for i := range v {
			out[i] = canonicalNumbers(v[i])
		}
		return out
	}
	return value
}

func normalizedAll(nodes []node) []node {
	if nodes == nil {
		return nil
	}
	out := make([]node, len(nodes))
	for i := range nodes {
		out[i] = nodes[i].normalized()
	}
	return out
}

// shapes counts the shapes set on n: exactly one is valid.
func (n *node) shapes() int {
	count := 0
	for _, set := range []bool{
		n.All != nil, n.Any != nil, n.Not != nil, n.Exists != "", n.Var != "",
	} {
		if set {
			count++
		}
	}
	return count
}

// predicate validates n, at JSON path at, and builds its engine predicate.
func (n *node) predicate(at string, depth int) (ruleengine.Predicate, error) {
	if depth > maxDepth {
		return nil, fmt.Errorf("%s: nested deeper than %d", at, maxDepth)
	}
	if n.shapes() != 1 {
		return nil, fmt.Errorf("%s: needs exactly one of all, any, not, exists, var", at)
	}
	if n.Var == "" && (n.Op != "" || len(n.Value) > 0 || n.Var2 != "") {
		return nil, fmt.Errorf("%s: op, value and var2 belong with var", at)
	}
	switch {
	case n.All != nil:
		children, err := predicates(n.All, at+".all", depth)
		if err != nil {
			return nil, err
		}
		return ruleengine.All(children...), nil
	case n.Any != nil:
		children, err := predicates(n.Any, at+".any", depth)
		if err != nil {
			return nil, err
		}
		return ruleengine.Any(children...), nil
	case n.Not != nil:
		child, err := n.Not.predicate(at+".not", depth+1)
		if err != nil {
			return nil, err
		}
		return ruleengine.Not(child), nil
	case n.Exists != "":
		path, err := variable(n.Exists, at+".exists")
		if err != nil {
			return nil, err
		}
		return ruleengine.Exists(path), nil
	}
	return n.comparison(at)
}

// predicates builds the children of an all or any.
func predicates(nodes []node, at string, depth int) ([]ruleengine.Predicate, error) {
	if len(nodes) == 0 {
		return nil, fmt.Errorf("%s: is empty", at)
	}
	out := make([]ruleengine.Predicate, 0, len(nodes))
	for i := range nodes {
		p, err := nodes[i].predicate(fmt.Sprintf("%s[%d]", at, i), depth+1)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// comparison builds a var node's comparison: against a literal (value) or
// another variable (var2), never both.
func (n *node) comparison(at string) (ruleengine.Predicate, error) {
	left, err := variable(n.Var, at+".var")
	if err != nil {
		return nil, err
	}
	op, ok := operator(n.Op)
	if !ok {
		return nil, fmt.Errorf(
			"%s.op: %q is not one of %s",
			at,
			n.Op,
			strings.Join(Operators(), ", "),
		)
	}
	if (len(n.Value) > 0) == (n.Var2 != "") {
		return nil, fmt.Errorf("%s: needs exactly one of value and var2", at)
	}
	if n.Var2 != "" {
		right, rightErr := variable(n.Var2, at+".var2")
		if rightErr != nil {
			return nil, rightErr
		}
		return ruleengine.Compare(op, ruleengine.Variable(left), ruleengine.Variable(right)), nil
	}
	listOp := op == ruleengine.OpIn || op == ruleengine.OpNotIn
	value, err := literal(n.Value, listOp)
	if err != nil {
		return nil, fmt.Errorf("%s.value: %w", at, err)
	}
	return ruleengine.Compare(op, ruleengine.Variable(left), ruleengine.Literal(value)), nil
}

// Operators lists the operators a comparison may use, sorted.
func Operators() []string {
	return []string{
		"contains", "ends_with", "equal", "greater_or_equal", "greater_than", "in",
		"less_or_equal", "less_than", "not_equal", "not_in", "starts_with",
	}
}

// operator maps a notation operator to the engine's.
func operator(name string) (ruleengine.OperatorName, bool) {
	if !slices.Contains(Operators(), name) {
		return "", false
	}
	return ruleengine.OperatorName(name), true
}

// variable reads a dotted variable name into an engine path.
func variable(name, at string) (ruleengine.Path, error) {
	path, err := ruleengine.NewPath(ruleengine.DefaultLimits(), strings.Split(name, ".")...)
	if err != nil {
		return ruleengine.Path{}, fmt.Errorf("%s: %q is not a variable name "+
			"(dot-separated, no empty part, no / or \\)", at, name)
	}
	return path, nil
}

// literal reads a literal: a list only when list is set, of scalars only.
func literal(raw json.RawMessage, list bool) (ruleengine.Value, error) {
	value, err := decodeLiteral(raw)
	if err != nil {
		return ruleengine.Value{}, err
	}
	switch _, isList := value.([]any); {
	case isList && !list:
		return ruleengine.Value{}, errors.New("a list is only allowed with in and not_in")
	case !isList && list:
		return ruleengine.Value{}, errors.New("in and not_in need a list")
	}
	return engineValue(value)
}

// engineValue converts a decoded scalar, or list of scalars, to an engine
// value.
func engineValue(value any) (ruleengine.Value, error) {
	items, isList := value.([]any)
	if !isList {
		return scalar(value)
	}
	values := make([]ruleengine.Value, 0, len(items))
	for _, item := range items {
		v, err := scalar(item)
		if err != nil {
			return ruleengine.Value{}, err
		}
		values = append(values, v)
	}
	return ruleengine.List(values...), nil
}

// decodeLiteral decodes raw with its numbers kept as written.
func decodeLiteral(raw json.RawMessage) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		return nil, fmt.Errorf("not a JSON value: %w", err)
	}
	return value, nil
}

// scalar converts a decoded bool, number or string to an engine value.
// Every number is a float: the engine refuses to compare an int with a
// float, and a model writes 0 and 0.0 alike, so 3 and 3.0 are one value
// (and hash equal).
func scalar(value any) (ruleengine.Value, error) {
	switch v := value.(type) {
	case bool:
		return ruleengine.Bool(v), nil
	case string:
		return ruleengine.String(v), nil
	case json.Number:
		f, err := v.Float64()
		if err != nil {
			return ruleengine.Value{}, fmt.Errorf("number %s is out of range", v)
		}
		return ruleengine.Float(f), nil
	}
	return ruleengine.Value{}, fmt.Errorf("%s is not a bool, number or string", kindOf(value))
}

// kindOf names the JSON kind of a decoded value for an error.
func kindOf(value any) string {
	switch value.(type) {
	case nil:
		return "null"
	case []any:
		return "a nested list"
	case map[string]any:
		return "an object"
	}
	return fmt.Sprintf("%T", value)
}
