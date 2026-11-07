package grounding_test

import (
	"testing"
	"testing/fstest"

	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/graph"
	"github.com/StevenACoffman/codetospec/internal/grounding"
)

// retry.go: lines 1-3 retry while 0 to 2 attempts were made, for tier
// PRO.
// The retry file's name, and the lines rules cite in it.
const (
	retryName  = "retry.go"
	retryLines = "1-3"
)

const retryFile = "if attempts >= 0 && attempts < 3 && tier == \"PRO\" {\n\tretry()\n}\n"

// TestAnnotateConditions: a condition whose values the cited lines lack is
// dropped; a kept one that states other values than the EARS text, or
// conditions a rule whose text states none, is flagged, each value marked
// cited or not; an agreeing one is left alone.
func TestAnnotateConditions(t *testing.T) {
	fsys := fstest.MapFS{retryName: {Data: []byte(retryFile)}}
	const (
		agrees    = `{"var":"attempts","op":"less_than","value":3}`
		notInCode = `{"var":"attempts","op":"less_than","value":5}`
		withTier  = `{"all":[{"var":"attempts","op":"less_than","value":3},{"var":"tier","op":"equal","value":"PRO"}]}`
		noValues  = `{"exists":"session"}`
		// fewer states the check the code makes.
		fewer = "WHILE fewer than 3 attempts were made, the system shall retry."
	)
	cases := map[string]struct {
		requirement, condition string
		wantKept               bool
		wantDropped            string
		wantDisagreement       string
	}{
		"agrees": {
			fewer, agrees,
			true, "", "",
		},
		"value not in the code: dropped": {
			"WHILE fewer than 5 attempts were made, the system shall retry.", notInCode,
			false, "5", "",
		},
		// The condition stating more only says the text is vaguer: its
		// values are grounded already, so no question.
		"condition states more": {
			fewer, withTier,
			true, "", "",
		},
		"text states a value the code lacks": {
			"WHILE fewer than 30 attempts were made, the system shall retry.", agrees,
			true, "", "text 30 (not cited)",
		},
		"text names the variable": {
			"WHILE the tier is `PRO`, the system shall retry.",
			`{"exists":"tier.PRO"}`,
			true, "", "",
		},
		"text states a constant's value the variable names": {
			"WHILE fewer than 3 attempts were made, the system shall retry.",
			`{"exists":"retry.MaxAttempts"}`,
			true, "", "",
		},
		"zero, stated in words": {
			"WHILE the attempts are not negative, the system shall retry.",
			`{"var":"attempts","op":"greater_or_equal","value":0}`,
			true, "", "",
		},
		"text states no condition": {
			"The system shall retry.", noValues,
			true, "", "",
		},
		"no condition": {
			fewer, "",
			false, "", "",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			node := graph.Node{
				ID:      "rule.retry.attempts",
				Type:    graph.TypeRule,
				Body:    "**Requirement (EARS)**: " + tc.requirement,
				Sources: []extract.Ref{{Path: retryName, Lines: retryLines}},
				Extra:   map[string]string{},
			}
			if tc.condition != "" {
				node.Extra[graph.ExtraCondition] = tc.condition
			}
			got := grounding.Annotate(extract.Roots{Src: fsys}, []graph.Node{node},
				grounding.Options{DecimalSeparator: '.', Constants: map[string]float64{"MaxAttempts": 3}})[0]
			if kept := got.Extra[graph.ExtraCondition] != ""; kept != tc.wantKept {
				t.Errorf("condition kept = %v, want %v", kept, tc.wantKept)
			}
			if d := got.Extra[graph.ExtraConditionDropped]; d != tc.wantDropped {
				t.Errorf("dropped = %q, want %q", d, tc.wantDropped)
			}
			if d := got.Extra[graph.ExtraConditionDisagreement]; d != tc.wantDisagreement {
				t.Errorf("disagreement = %q, want %q", d, tc.wantDisagreement)
			}
		})
	}
}

// guardFile is an access guard whose scope its condition can't state (the
// notation has no call operands): admin's getIPRestrictionInfo.
const guardFile = "if !acl.ActorHasPermission(ctx, capabilities.CanModerateUsers, acl.GlobalScope) {\n" +
	"\treturn nil, errors.Unauthorized()\n}\n"

// TestDisagreementReadsCheckArgs: a value the EARS text names that the
// condition leaves out but the rule's check_args carries (a check's
// capability, its scope matcher) is no disagreement; without check_args
// it is one.
func TestDisagreementReadsCheckArgs(t *testing.T) {
	fsys := fstest.MapFS{"guard.go": {Data: []byte(guardFile)}}
	const (
		requirement = "IF the caller lacks permission `CanModerateUsers` for `acl.GlobalScope`, " +
			"THEN the system shall reject Query.getIPRestrictionInfo with the `Unauthorized` error."
		cond      = `{"not":{"var":"caller.hasPermission.CanModerateUsers","op":"equal","value":true}}`
		checkArgs = `[{"check":"acl.ActorHasPermission","var":"caller.hasPermission.CanModerateUsers",` +
			`"means":"capability","scope":"global","capability":"CanModerateUsers","matcher":"acl.GlobalScope"}]`
		// A call's labels (its variable, meanings) name; they carry no value.
		labelsOnly = `[{"var":"CanModerateUsers","means":"acl.GlobalScope"}]`
	)
	cases := []struct {
		name, checkArgs, want string
	}{
		// The condition's variable names the capability: only the scope,
		// which the notation can't state, is missing without check_args.
		{
			"without check_args",
			"",
			"text `acl.GlobalScope` (cited)",
		},
		{"with check_args", checkArgs, ""},
		{
			"labels only",
			labelsOnly,
			"text `acl.GlobalScope` (cited)",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			node := graph.Node{
				ID:      "rule.admin.authz-query-getiprestrictioninfo",
				Type:    graph.TypeRule,
				Body:    "**Requirement (EARS)**: " + requirement,
				Sources: []extract.Ref{{Path: "guard.go", Lines: retryLines}},
				Extra:   map[string]string{graph.ExtraCondition: cond},
			}
			if tc.checkArgs != "" {
				node.Extra[graph.ExtraCheckArgs] = tc.checkArgs
			}
			got := grounding.Annotate(extract.Roots{Src: fsys}, []graph.Node{node},
				grounding.Options{DecimalSeparator: '.'})[0]
			if d := got.Extra[graph.ExtraConditionDisagreement]; d != tc.want {
				t.Errorf("disagreement = %q, want %q", d, tc.want)
			}
		})
	}
}

// TestAnnotateReplacesDisagreement: annotating again recomputes the
// disagreement: one recorded before, which no longer holds, is gone.
func TestAnnotateReplacesDisagreement(t *testing.T) {
	fsys := fstest.MapFS{retryName: {Data: []byte(retryFile)}}
	node := graph.Node{
		ID:      "rule.retry.attempts",
		Type:    graph.TypeRule,
		Body:    "**Requirement (EARS)**: WHILE fewer than 3 attempts were made, the system shall retry.",
		Sources: []extract.Ref{{Path: retryName, Lines: retryLines}},
		Extra: map[string]string{
			graph.ExtraCondition:             `{"var":"attempts","op":"less_than","value":3}`,
			graph.ExtraConditionDisagreement: "condition 3 (cited)",
		},
	}
	got := grounding.Annotate(extract.Roots{Src: fsys}, []graph.Node{node},
		grounding.Options{DecimalSeparator: '.'})[0]
	if d, set := got.Extra[graph.ExtraConditionDisagreement]; set {
		t.Errorf("disagreement = %q, want none", d)
	}
}
