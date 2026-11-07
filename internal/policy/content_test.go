package policy_test

import (
	"strings"
	"testing"

	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/graph"
	"github.com/StevenACoffman/codetospec/internal/locale"
	"github.com/StevenACoffman/codetospec/internal/policy"
)

// editContent is a checker granting by content kind.
const editContent = "permissions.CanEditContent"

// TestContentScope: a content scope names its kind or the argument that
// does, never both or neither, and nothing else names one; its statement
// says the kind.
func TestContentScope(t *testing.T) {
	for name, bad := range map[string]string{
		"no kind":    `{"x": {"means": "capability", "capability": "CanEdit", "scope": "content"}}`,
		"both kinds": `{"x": {"means": "capability", "capability": "CanEdit", "scope": "content", "content_kind": "Video", "content_kind_arg": 2}}`,
		"elsewhere":  `{"x": {"means": "capability", "capability": "CanEdit", "scope": "locale", "content_kind": "Video"}}`,
	} {
		if _, err := policy.ParseCheckers([]byte(bad)); err == nil {
			t.Errorf("%s: parsed", name)
		}
	}
	cs, err := policy.ParseCheckers([]byte(
		`{"x": {"means": "capability", "capability": "capabilities.CanEdit", "scope": "content", "content_kind": "Video"}}`,
	))
	if err != nil {
		t.Fatal(err)
	}
	pin := cs["x"]
	want := `Allowed when the caller holds the capability "Edit" for the request's language and content of kind "Video".`
	if got := policy.Statement(&pin, policy.Registry{}, locale.For("en")); got != want {
		t.Errorf("statement:\n got %s\nwant %s", got, want)
	}
}

// TestContentKindArg: the content kind a call's string literal names is
// the leaf's; a call naming it through anything else leaves its operation
// out, with the reason.
func TestContentKindArg(t *testing.T) {
	cs, err := policy.ParseCheckers([]byte(`{"` + editContent + `": {"means": "capability",
		"capability": "CanEdit", "scope": "content", "content_kind_arg": 2}}`))
	if err != nil {
		t.Fatal(err)
	}
	facts := []extract.Fact{fact(extract.KindCapability, "capability.1", at("caps.go", "1-1"),
		map[string]string{extract.AttrName: "CanEdit", extract.AttrValue: "can_edit"})}
	guard := func(id, op, kind string) graph.Node {
		return guardRule(id, op, `{"var":"edit.fails","op":"equal","value":true}`,
			`[{"check":"`+editContent+`","var":"edit.fails","fails":true,`+
				`"args":[{"text":"ctx"},{"text":"kaid"},{"text":`+kind+`}]}]`)
	}
	m := policy.BuildModel(facts, []graph.Node{
		guard("rule.literal", "Mutation.edit", `"\"Article\""`),
		guard("rule.variable", "Mutation.editAny", `"kind"`),
	}, cs)
	if len(m.Operations) != 1 || m.Operations[0].Leaves()[0].ContentKind != "Article" {
		t.Errorf("operations = %+v, want Mutation.edit on Article", m.Operations)
	}
	if len(m.Skipped) != 1 || !strings.Contains(m.Skipped[0].Reason, "content kind") {
		t.Errorf("skipped = %+v, want Mutation.editAny for its kind", m.Skipped)
	}
}
