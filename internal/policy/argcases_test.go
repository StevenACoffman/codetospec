package policy_test

import (
	"strings"
	"testing"

	"github.com/StevenACoffman/codetospec/internal/graph"
	"github.com/StevenACoffman/codetospec/internal/locale"
	"github.com/StevenACoffman/codetospec/internal/policy"
)

// hasRole is a checker whose meaning depends on its third argument.
const hasRole = "permissions.HasRole"

// roleCases pins hasRole by its role argument, as written.
const roleCases = `{"` + hasRole + `": {"cases": [
	{"when_arg": {"2": "\"admin\""}, "means": "service_admin"},
	{"when_arg": {"2": "\"editor\""}, "all": [{"means": "login"}, {"means": "relation", "relation": "editor_of"}]}]}}`

// TestArgCasesParse: a case is a whole checker keyed on argument indexes;
// cases beside another shape, inside a part, or keyed on no index fail.
func TestArgCasesParse(t *testing.T) {
	if _, err := policy.ParseCheckers([]byte(roleCases)); err != nil {
		t.Fatal(err)
	}
	for name, bad := range map[string]string{
		"beside means": `{"x": {"means": "login", "cases": [{"when_arg": {"1": "a"}, "means": "login"}]}}`,
		"in a part":    `{"x": {"any": [{"cases": [{"when_arg": {"1": "a"}, "means": "login"}]}]}}`,
		"no index":     `{"x": {"cases": [{"when_arg": {}, "means": "login"}]}}`,
		"not an index": `{"x": {"cases": [{"when_arg": {"one": "a"}, "means": "login"}]}}`,
	} {
		if _, err := policy.ParseCheckers([]byte(bad)); err == nil {
			t.Errorf("%s: parsed", name)
		}
	}
}

// TestArgCasesModel: a call's argument, as written, picks its case; a
// call matching no case leaves its operation out, with the reason.
func TestArgCasesModel(t *testing.T) {
	cs, err := policy.ParseCheckers([]byte(roleCases))
	if err != nil {
		t.Fatal(err)
	}
	guard := func(id, op, role string) graph.Node {
		return guardRule(id, op, `{"var":"role.fails","op":"equal","value":true}`,
			`[{"check":"`+hasRole+`","var":"role.fails","fails":true,`+
				`"args":[{"text":"ctx"},{"text":"kaid"},{"text":"\"`+role+`\""}]}]`)
	}
	m := policy.BuildModel(nil, []graph.Node{
		guard("rule.admin", "Mutation.purge", "admin"),
		guard("rule.viewer", "Query.peek", "viewer"),
	}, cs)
	if len(m.Operations) != 1 || m.Operations[0].Action != "Mutation.purge" {
		t.Fatalf("operations = %+v, want Mutation.purge", m.Operations)
	}
	if leaves := m.Operations[0].Leaves(); len(leaves) != 1 ||
		leaves[0].Kind != policy.LeafServiceAdmin {
		t.Errorf("Mutation.purge leaves = %+v, want the admin case's", leaves)
	}
	if len(m.Skipped) != 1 || !strings.Contains(m.Skipped[0].Reason, "no case of "+hasRole) {
		t.Errorf("skipped = %+v, want Query.peek with no matching case", m.Skipped)
	}
}

// TestArgCasesStatementAndCompare: a statement states each case; two pins
// compare case by case, naming the first that differs.
func TestArgCasesStatementAndCompare(t *testing.T) {
	cs, err := policy.ParseCheckers([]byte(roleCases))
	if err != nil {
		t.Fatal(err)
	}
	pin := cs[hasRole]
	want := `Depending on the call's arguments: with argument 2 equal to "admin", allowed when ` +
		`the request is an administrator call from another service; with argument 2 equal to "editor", ` +
		`allowed when the caller is signed in and the caller has the relation "editor_of".`
	if got := policy.Statement(&pin, policy.Registry{}, locale.For("en")); got != want {
		t.Errorf("statement:\n got %s\nwant %s", got, want)
	}
	other, err := policy.ParseCheckers(
		[]byte(strings.Replace(roleCases, `"means": "login"`, `"means": "login_strict"`, 1)),
	)
	if err != nil {
		t.Fatal(err)
	}
	b := other[hasRole]
	if c := policy.Equivalent(&pin, &pin); !c.Equal {
		t.Errorf("a pin and itself: %+v", c)
	}
	if c := policy.Equivalent(
		&pin,
		&b,
	); c.Equal || c.Case != `2="editor"` ||
		c.Counterexample == nil {
		t.Errorf("editor differs: %+v", c)
	}
}

// TestArgCasesCaller: a case keyed on "@caller" matches an argument the
// extractor bound to the request user, whatever its text, and no other.
func TestArgCasesCaller(t *testing.T) {
	cs, err := policy.ParseCheckers([]byte(`{"x": {"cases": [
		{"when_arg": {"1": "` + policy.CallerArg + `"}, "means": "relation", "relation": "teacher_of"}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	pin := cs["x"]
	for text, from := range map[string]string{"actor.Kaid": policy.FromCaller, "parentUser.Kaid": parentKaid} {
		call := policy.CheckCall{
			Check: "x",
			Args:  []policy.CallArg{{Text: "ktx"}, {Text: text, From: from}},
		}
		got, ok := pin.For(&call)
		if want := from == policy.FromCaller; ok != want || ok && got.Relation != "teacher_of" {
			t.Errorf("%s (%s): For = %+v, %v; want matched %v", text, from, got, ok, want)
		}
	}
}
