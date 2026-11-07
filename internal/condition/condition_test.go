package condition_test

import (
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/StevenACoffman/codetospec/internal/condition"
)

// loggedOut is a one-check condition: the caller is not logged in.
const loggedOut = `{"var": "caller.loggedIn", "op": "equal", "value": false}`

// Variables of the test conditions.
const (
	country = "shipment.country"
	weight  = "shipment.weight"
	role    = "role"
	token   = "token"
)

// heavyFinnish combines two checks with a number and a string.
const heavyFinnish = `{"all": [
	{"var": "shipment.country", "op": "equal", "value": "FI"},
	{"var": "shipment.weight", "op": "greater_or_equal", "value": 1000}
]}`

func mustParse(t *testing.T, raw string) condition.Condition {
	t.Helper()
	c, err := condition.Parse([]byte(raw))
	if err != nil {
		t.Fatalf("Parse(%s): %v", raw, err)
	}
	return c
}

// TestParseRejects: every malformed condition is refused, naming where.
func TestParseRejects(t *testing.T) {
	cases := map[string]struct{ raw, want string }{
		"not JSON":    {`{"var": `, "condition:"},
		"unknown key": {`{"variable": "a"}`, "unknown field"},
		"two shapes": {
			`{"exists": "a", "var": "b", "op": "equal", "value": 1}`,
			"exactly one",
		},
		"no shape":         {`{}`, "exactly one"},
		"empty all":        {`{"all": []}`, "condition.all: is empty"},
		"unknown operator": {`{"var": "a", "op": "is", "value": 1}`, `condition.op: "is"`},
		"regex operator":   {`{"var": "a", "op": "matches", "value": "x"}`, "condition.op"},
		"value and var2": {
			`{"var": "a", "op": "equal", "value": 1, "var2": "b"}`,
			"exactly one of value and var2",
		},
		"neither value nor var2": {`{"var": "a", "op": "equal"}`, "exactly one of value"},
		"op without var":         {`{"exists": "a", "op": "equal"}`, "belong with var"},
		"list outside in": {
			`{"var": "a", "op": "equal", "value": [1]}`,
			"only allowed with in",
		},
		"in without list": {`{"var": "a", "op": "in", "value": 1}`, "need a list"},
		"nested list":     {`{"var": "a", "op": "in", "value": [[1]]}`, "nested list"},
		"null literal":    {`{"var": "a", "op": "equal", "value": null}`, "condition.value"},
		"object literal":  {`{"var": "a", "op": "equal", "value": {}}`, "an object"},
		"empty segment":   {`{"exists": "a..b"}`, "not a variable name"},
		"slash in name":   {`{"exists": "a/b"}`, "not a variable name"},
		"deep error path": {
			`{"all": [{"exists": "a"}, {"var": "b", "op": "bad", "value": 1}]}`,
			"condition.all[1].op",
		},
		"trailing data": {`{"exists": "a"} {}`, "trailing data"},
		"too deep": {
			strings.Repeat(`{"not": `, 18) + `{"exists": "a"}` + strings.Repeat("}", 18),
			"deeper",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := condition.Parse([]byte(tc.raw))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("Parse error = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}

// TestParseAccepts: the notation's forms parse; none at all is the zero
// condition.
func TestParseAccepts(t *testing.T) {
	cases := map[string]struct {
		raw  string
		zero bool
	}{
		"empty":      {"", true},
		"null":       {"null", true},
		"comparison": {loggedOut, false},
		"two variables": {
			`{"var": "request.user", "op": "not_equal", "var2": "caller.user"}`,
			false,
		},
		"in a list":         {`{"var": "role", "op": "in", "value": ["admin", "owner"]}`, false},
		"exists":            {`{"exists": "headers.authorization"}`, false},
		"not any all":       {`{"not": {"any": [{"exists": "a"}, ` + heavyFinnish + `]}}`, false},
		"float":             {`{"var": "ratio", "op": "less_than", "value": 0.5}`, false},
		"surrounding space": {"  " + loggedOut + "\n", false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			c := mustParse(t, tc.raw)
			if c.IsZero() != tc.zero {
				t.Errorf("IsZero = %v, want %v", c.IsZero(), tc.zero)
			}
			if !tc.zero && (c.Hash() == "" || c.Canonical() == "") {
				t.Errorf("Hash %q, Canonical %q: want both set", c.Hash(), c.Canonical())
			}
		})
	}
}

// TestHash: spacing and key order do not change the hash; any change of
// variable, operator or literal does.
func TestHash(t *testing.T) {
	base := mustParse(t, heavyFinnish)
	cases := map[string]struct {
		raw  string
		same bool
	}{
		"same, spaced and reordered": {
			`{"all":[{"value":"FI","op":"equal","var":"shipment.country"},` +
				`{"op":"greater_or_equal","var":"shipment.weight","value":1000}]}`,
			true,
		},
		"other literal": {strings.Replace(heavyFinnish, "1000", "2000", 1), false},
		"other variable": {
			strings.Replace(heavyFinnish, country, "shipment.origin", 1),
			false,
		},
		"other operator": {
			strings.Replace(heavyFinnish, "greater_or_equal", "greater_than", 1),
			false,
		},
		"3 and 3.0 alike": {strings.Replace(heavyFinnish, "1000", "1000.0", 1), true},
		"any, not all":    {strings.Replace(heavyFinnish, `"all"`, `"any"`, 1), false},
		"children reordered": {
			`{"all": [{"var": "shipment.weight", "op": "greater_or_equal", "value": 1000}, ` +
				`{"var": "shipment.country", "op": "equal", "value": "FI"}]}`,
			false,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			other := mustParse(t, tc.raw)
			if (other.Hash() == base.Hash()) != tc.same {
				t.Errorf("hash equal = %v, want %v", other.Hash() == base.Hash(), tc.same)
			}
			if tc.same && other.Canonical() != base.Canonical() {
				t.Errorf("canonical %s, want %s", other.Canonical(), base.Canonical())
			}
		})
	}
}

// TestVariablesAndLiterals: what a condition reads and the values the
// cited code must state.
func TestVariablesAndLiterals(t *testing.T) {
	cases := map[string]struct {
		raw                 string
		variables, literals []string
	}{
		"boolean check": {loggedOut, []string{"caller.loggedIn"}, nil},
		"number and string": {
			heavyFinnish,
			[]string{country, weight},
			[]string{`"FI"`, "1000"},
		},
		"list, exists and var2": {
			`{"all": [{"var": "role", "op": "in", "value": ["admin", 3]}, {"exists": "token"}, ` +
				`{"var": "a", "op": "equal", "var2": "b"}, {"exists": "token"}]}`,
			[]string{"a", "b", role, token},
			[]string{`"admin"`, "3"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			c := mustParse(t, tc.raw)
			if got := c.Variables(); !slices.Equal(got, tc.variables) {
				t.Errorf("Variables = %q, want %q", got, tc.variables)
			}
			if got := c.Literals(); !slices.Equal(got, tc.literals) {
				t.Errorf("Literals = %q, want %q", got, tc.literals)
			}
		})
	}
}

// TestRequired: only the booleans every match needs, a not flipping
// them; a variable under any or against a number is left out.
func TestRequired(t *testing.T) {
	c := mustParse(t, `{"all": [`+loggedOut+`,
		{"not": {"var": "a", "op": "equal", "value": true}},
		{"all": [{"var": "b", "op": "equal", "value": true}]},
		{"any": [{"var": "c", "op": "equal", "value": true}, {"var": "d", "op": "equal", "value": true}]},
		{"not": {"all": [{"var": "e", "op": "equal", "value": true}]}},
		{"var": "shipment.weight", "op": "equal", "value": 3}
	]}`)
	got := c.Required()
	want := map[string]bool{"caller.loggedIn": false, "a": false, "b": true}
	if !maps.Equal(got, want) {
		t.Errorf("Required = %v, want %v", got, want)
	}
	if zero := (condition.Condition{}); len(zero.Required()) != 0 {
		t.Error("the zero Condition requires something")
	}
}

// TestRenamed: every variable goes through rename; the original is kept.
func TestRenamed(t *testing.T) {
	c := mustParse(t, `{"all": [{"exists": "token"}, {"var": "a", "op": "equal", "var2": "b"}]}`)
	cases := map[string]struct {
		rename func(string) string
		want   []string
		fails  bool
	}{
		"prefix all": {
			func(s string) string { return "local." + s },
			[]string{"local.a", "local.b", "local.token"},
			false,
		},
		"identity":     {func(s string) string { return s }, []string{"a", "b", token}, false},
		"invalid name": {func(string) string { return "a/b" }, nil, true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			renamed, err := c.Renamed(tc.rename)
			if (err != nil) != tc.fails {
				t.Fatalf("err = %v, want failure %v", err, tc.fails)
			}
			if got := renamed.Variables(); !tc.fails && !slices.Equal(got, tc.want) {
				t.Errorf("Variables = %q, want %q", got, tc.want)
			}
			if got := c.Variables(); !slices.Equal(got, []string{"a", "b", token}) {
				t.Errorf("the original changed: %q", got)
			}
		})
	}
}

// TestEvaluate: an example matches, does not, or does not decide.
func TestEvaluate(t *testing.T) {
	heavy := mustParse(t, heavyFinnish)
	present := mustParse(t, `{"exists": "token"}`)
	roles := mustParse(t, `{"var": "role", "op": "in", "value": ["admin", "owner"]}`)
	cases := map[string]struct {
		c     condition.Condition
		given map[string]any
		want  condition.Outcome
	}{
		"matches": {
			heavy,
			map[string]any{country: "FI", weight: 1500},
			condition.Matches,
		},
		"too light": {
			heavy,
			map[string]any{country: "FI", weight: 999},
			condition.DoesNotMatch,
		},
		"variable not given": {
			heavy, map[string]any{country: "FI"}, condition.Indeterminate,
		},
		"wrong type": {
			heavy,
			map[string]any{country: "FI", weight: "heavy"},
			condition.Indeterminate,
		},
		"object value": {
			heavy,
			map[string]any{country: "FI", weight: map[string]any{}},
			condition.Indeterminate,
		},
		"exists, absent":  {present, map[string]any{}, condition.DoesNotMatch},
		"exists, present": {present, map[string]any{token: "x"}, condition.Matches},
		"in list":         {roles, map[string]any{role: "owner"}, condition.Matches},
		"not in list":     {roles, map[string]any{role: "guest"}, condition.DoesNotMatch},
		"zero condition":  {condition.Condition{}, map[string]any{}, condition.Indeterminate},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := tc.c.Evaluate(t.Context(), tc.given); got != tc.want {
				t.Errorf("Evaluate = %s, want %s", got, tc.want)
			}
		})
	}
}

// FuzzParse: Parse never panics, and a condition's canonical form parses
// back to the same hash and form.
func FuzzParse(f *testing.F) {
	for _, seed := range []string{
		loggedOut, heavyFinnish, `{"exists": "a"}`, `{"not": {"exists": "a"}}`,
		`{"var": "role", "op": "in", "value": ["admin", 3, true]}`, `{"all": []}`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		c, err := condition.Parse(raw)
		if err != nil || c.IsZero() {
			return
		}
		again, err := condition.Parse([]byte(c.Canonical()))
		if err != nil {
			t.Fatalf("canonical %s does not parse: %v", c.Canonical(), err)
		}
		if again.Hash() != c.Hash() || again.Canonical() != c.Canonical() {
			t.Fatalf("round trip changed %s into %s", c.Canonical(), again.Canonical())
		}
	})
}
