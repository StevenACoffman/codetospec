package policy_test

import (
	"strings"
	"testing"

	"github.com/StevenACoffman/codetospec/internal/graph"
	"github.com/StevenACoffman/codetospec/internal/locale"
	"github.com/StevenACoffman/codetospec/internal/policy"
)

// visibleFilter is an item filter with a call-level meaning: an item is
// visible to its own user or a coach of that user; a guard testing the
// filter for one user (argument 1) passes when that user is the caller or
// one the caller coaches.
const visibleFilter = `{"permissions.Visible": {"item": "Note", "any": [{"means": "self"}, {"means": "coach"}],
	"call": {"subject_arg": 1, "any": [{"means": "self"}, {"means": "coach"}]}}}`

// TestCallParse: call states an item filter's call-level meaning, only on
// an item entry.
func TestCallParse(t *testing.T) {
	if _, err := policy.ParseCheckers([]byte(visibleFilter)); err != nil {
		t.Fatal(err)
	}
	for name, bad := range map[string]string{
		"not an item":          `{"x": {"means": "login", "call": {"means": "login"}}}`,
		"in a part":            `{"x": {"item": "Note", "any": [{"means": "self", "call": {"means": "login"}}]}}`,
		"self without subject": `{"x": {"item": "Note", "means": "self", "call": {"means": "self"}}}`,
	} {
		if _, err := policy.ParseCheckers([]byte(bad)); err == nil {
			t.Errorf("%s: parsed", name)
		}
	}
}

// TestCallModel: a guard testing an item filter for the call translates
// through its call-level meaning; without one, the guard doesn't.
func TestCallModel(t *testing.T) {
	cs, err := policy.ParseCheckers([]byte(visibleFilter))
	if err != nil {
		t.Fatal(err)
	}
	guard := guardRule(
		"rule.call",
		"User.notes",
		`{"var":"permissions.Visible.fails","op":"equal","value":true}`,
		`[{"check":"permissions.Visible","var":"permissions.Visible.fails","fails":true,`+
			`"args":[{"text":"ktx"},{"text":"user.Kaid","from":"parent:Kaid"}]}]`,
	)
	m := policy.BuildModel(nil, []graph.Node{guard}, cs)
	if len(m.Operations) != 1 || m.Operations[0].Subject != parentKaid {
		t.Fatalf(
			"operations = %+v, skipped %+v; want User.notes about the parent",
			m.Operations,
			m.Skipped,
		)
	}
	without := policy.Checkers{
		"permissions.Visible": {Item: "Note", Any: cs["permissions.Visible"].Any},
	}
	if m = policy.BuildModel(nil, []graph.Node{guard}, without); len(m.Operations) != 0 {
		t.Errorf("without call: operations = %+v, want none", m.Operations)
	}
	pin := cs["permissions.Visible"]
	if s := policy.Statement(
		&pin,
		policy.Registry{},
		locale.For("en"),
	); !strings.Contains(
		s,
		"When tested for one call",
	) {
		t.Errorf("statement %q names no call-level meaning", s)
	}
}
