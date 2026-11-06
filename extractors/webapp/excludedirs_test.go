package main

import (
	"testing"
	"testing/fstest"
)

// TestExcludeDirs: a directory named in Inputs.ExcludeDirs is not analyzed, so a
// role a test helper registers there yields no rule (FIX.md F2), while the
// service's own roles still do.
func TestExcludeDirs(t *testing.T) {
	role := func(name string) *fstest.MapFile {
		return &fstest.MapFile{Data: []byte(`package roles

var R = RegisterRoleDefinition(RoleDefinition{
	Name:          "` + name + `",
	Description:   "a role",
	CapabilitySet: []capabilities.Capability{capabilities.CanViewUserPermissions},
})
`)}
	}
	service := fstest.MapFS{
		"permissions/roles/dev.go": role("dev_role"),
		"testutil/suite.go":        role("suite_role"),
	}
	cases := []struct {
		name        string
		excludeDirs []string
		want        map[string]bool // role name → has a rule
	}{
		{"default", nil, map[string]bool{"dev_role": true, "suite_role": true}},
		{
			"testutil skipped",
			[]string{"testutil"},
			map[string]bool{"dev_role": true, "suite_role": false},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			facts, _, err := Analyze(
				&Inputs{Service: service, Lang: "en", ExcludeDirs: tc.excludeDirs},
			)
			if err != nil {
				t.Fatal(err)
			}
			got := map[string]bool{}
			for _, f := range facts {
				if f.Kind == kindRule && f.Attrs[attrRole] != "" {
					got[f.Attrs[attrRole]] = true
				}
			}
			for name, want := range tc.want {
				if got[name] != want {
					t.Errorf("rule for role %s = %v, want %v", name, got[name], want)
				}
			}
		})
	}
}
