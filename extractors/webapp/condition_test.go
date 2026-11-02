package main

import (
	"encoding/json"
	"go/parser"
	"go/token"
	"testing"
)

// TestConditionNode: each guard expression states the check the
// requirement words, in the compact notation.
func TestConditionNode(t *testing.T) {
	cases := map[string]struct{ cond, want string }{
		"not logged in": {
			"!acl.IsLoggedIn(ktx)",
			`{"not":{"var":"caller.loggedIn","op":"equal","value":true}}`,
		},
		"phantom accounts": {
			"!acl.IsLoggedIn(ktx, acl.PhantomProhibited)",
			`{"not":{"var":"caller.loggedInNonPhantom","op":"equal","value":true}}`,
		},
		"permission": {
			"!acl.ActorHasPermission(ktx, capabilities.CanChangeUserData, acl.AnyScope)",
			`{"not":{"var":"caller.hasPermission.CanChangeUserData","op":"equal","value":true}}`,
		},
		"and chains flatten": {
			"!a(ktx) && !ktx.RequestIsServiceAdminQuery() && b(x)",
			`{"all":[{"not":{"var":"a","op":"equal","value":true}},` +
				`{"not":{"var":"request.isServiceAdmin","op":"equal","value":true}},` +
				`{"var":"b","op":"equal","value":true}]}`,
		},
		"or keeps a nested and": {
			"!acl.IsLoggedIn(ktx) || (acl.IsCoachedByActor(ktx, k) && x)",
			`{"any":[{"not":{"var":"caller.loggedIn","op":"equal","value":true}},` +
				`{"all":[{"var":"acl.IsCoachedByActor","op":"equal","value":true},` +
				`{"var":"x","op":"equal","value":true}]}]}`,
		},
		"rate limit": {
			`ratelimit.ByIP(ktx, "Login", ratelimit.MaxRequestsPerWindow(5))`,
			`{"var":"caller.overRateLimit.Login","op":"equal","value":true}`,
		},
		"literal comparison": {
			"count > 25",
			`{"var":"count","op":"greater_than","value":25}`,
		},
		"string comparison": {
			`role == "admin"`,
			`{"var":"role","op":"equal","value":"admin"}`,
		},
		"header read": {
			`r.Header.Get("X-Secret") == ""`,
			`{"var":"request.header.X-Secret","op":"equal","value":""}`,
		},
		"nil is absence": {
			"kaid == nil",
			`{"not":{"exists":"kaid"}}`,
		},
		"double negation collapses": {
			"!(kaid == nil)",
			`{"exists":"kaid"}`,
		},
		"predicate call by its function": {
			"!canSeeBioFields(ktx, *obj.Kaid)",
			`{"not":{"var":"canSeeBioFields","op":"equal","value":true}}`,
		},
		"opaque expression": {
			"x.y",
			`{"var":"x.y","op":"equal","value":true}`,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			expr, err := parser.ParseExpr(tc.cond)
			if err != nil {
				t.Fatalf("ParseExpr: %v", err)
			}
			c := conditioner{r: renderer{fset: token.NewFileSet(), marks: newCallMarks()}}
			data, err := json.Marshal(c.node(expr))
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != tc.want {
				t.Errorf("node = %s\nwant   %s", data, tc.want)
			}
		})
	}
}

// TestConditionOf: only access and rate-limit guards outside a loop get a
// condition; the others say nothing rather than guess.
func TestConditionOf(t *testing.T) {
	cond, err := parser.ParseExpr("!acl.IsLoggedIn(ktx)")
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]struct {
		g    guard
		want bool
	}{
		"access":     {guard{kind: ruleAccess, cond: cond}, true},
		"rate limit": {guard{kind: ruleRateLimit, cond: cond}, true},
		"open":       {guard{kind: ruleOpen}, false},
		"in a loop":  {guard{kind: ruleAccess, cond: cond, loop: &loop{}}, false},
		"permit":     {guard{kind: rulePermit, cond: cond}, false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := conditionOf(token.NewFileSet(), &tc.g)
			if (got != "") != tc.want {
				t.Errorf("conditionOf = %q, want one: %v", got, tc.want)
			}
		})
	}
}

// TestVariableName: source text becomes a name the notation accepts.
func TestVariableName(t *testing.T) {
	const dotted = "a.b"
	cases := map[string]string{
		dotted:     dotted,
		"a..b.":    dotted,
		"a/b\\c":   "a_b_c",
		"f(\n\tx)": "f(  x)",
		"":         "unknown",
		"...":      "unknown",
	}
	for src, want := range cases {
		t.Run(src, func(t *testing.T) {
			if got := variableName(src); got != want {
				t.Errorf("variableName(%q) = %q, want %q", src, got, want)
			}
		})
	}
}
