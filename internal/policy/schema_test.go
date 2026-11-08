// White-box: TestCheckersSchemaEnums pins the schema to the unexported
// vocabulary lists (pinnable, pinnedScopes), which nothing else exports.
package policy //nolint:testpackage // pins the schema to unexported vocabulary lists

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

// TestCheckersSchemaKeys: a checkers file naming a key the format doesn't
// have stops with the entry and the key's path; the YAML decoder alone
// would ignore it and drop part of a pin.
func TestCheckersSchemaKeys(t *testing.T) {
	_, err := ParseCheckers([]byte(`{"x": {"means": "self", "subjectarg": 1}}`))
	if err == nil || !strings.Contains(err.Error(), "subjectarg") {
		t.Errorf("err = %v, want one naming subjectarg", err)
	}
	if _, err = ParseCheckers(
		[]byte(`{"x": {"any": [{"means": "login", "scop": "any"}]}}`),
	); err == nil ||
		!strings.Contains(err.Error(), "scop") {
		t.Errorf("err = %v, want one naming scop", err)
	}
}

// TestCheckersJSONAndYAML: the same checkers file reads alike as JSON
// and as YAML.
func TestCheckersJSONAndYAML(t *testing.T) {
	fromJSON, err := ParseCheckers([]byte(
		`{"x": {"subject_arg": 1, "any": [{"means": "self"}, {"means": "service_admin"}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	fromYAML, err := ParseCheckers(
		[]byte("x:\n  subject_arg: 1\n  any:\n    - {means: self}\n    - {means: service_admin}\n"),
	)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(fromJSON)
	b, _ := json.Marshal(fromYAML)
	if !bytes.Equal(a, b) {
		t.Errorf("JSON %s, YAML %s", a, b)
	}
}

// TestCheckersSchemaEnums: the schema's meanings, scopes and holders are
// the Go vocabulary's, so neither drifts from the other.
func TestCheckersSchemaEnums(t *testing.T) {
	var doc struct {
		Defs map[string]struct {
			Properties map[string]struct {
				Enum []string `json:"enum"`
			} `json:"properties"`
		} `json:"$defs"`
	}
	data, err := schemas.ReadFile("schema/" + SchemaCheckers)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	fields := doc.Defs["fields"].Properties
	for name, want := range map[string][]string{
		"means": pinnable(),
		"scope": pinnedScopes(),
		"of":    {HolderCaller, HolderResource, HolderContext},
	} {
		got := slices.Sorted(slices.Values(fields[name].Enum))
		if !slices.Equal(got, slices.Sorted(slices.Values(want))) {
			t.Errorf("schema %s = %v, want %v", name, got, want)
		}
	}
	var bugs struct {
		Defs struct {
			Entry struct {
				Properties struct {
					Bugs struct {
						Items struct {
							Properties struct {
								Kind struct {
									Enum []string `json:"enum"`
								} `json:"kind"`
							} `json:"properties"`
						} `json:"items"`
					} `json:"bugs"`
				} `json:"properties"`
			} `json:"entry"`
		} `json:"$defs"`
	}
	if err = json.Unmarshal(data, &bugs); err != nil {
		t.Fatal(err)
	}
	if got := bugs.Defs.Entry.Properties.Bugs.Items.Properties.Kind.Enum; !slices.Equal(
		slices.Sorted(slices.Values(got)), []string{BugCommentMismatch, BugDeadBranch},
	) {
		t.Errorf("schema bug kinds = %v", got)
	}
}
