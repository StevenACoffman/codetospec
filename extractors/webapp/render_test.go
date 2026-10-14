package main

import (
	"go/parser"
	"go/token"
	"testing"
)

func TestRenderCondition(t *testing.T) {
	cases := map[string]struct {
		lang, cond, want string
	}{
		"negated check": {
			"en", "!acl.IsCurrentUser(ktx, *user.Kaid)",
			"the caller is not the user `user.Kaid`",
		},
		"de morgan on negated and": {
			"en", "!(acl.IsCurrentUser(ktx, k) && ktx.RequestIsServiceAdminQuery())",
			"the caller is not the user `k` OR the request is not a service-admin query",
		},
		"mixed operators keep grouping": {
			"en", "!acl.IsLoggedIn(ktx) || (acl.IsCoachedByActor(ktx, k) && x)",
			"the caller is not logged in OR (the user `k` is coached by the caller AND `x` holds)",
		},
		"negated conjunction keeps its grouping": {
			"en",
			"!(acl.IsLoggedIn(ktx) && x) && !ktx.RequestIsServiceAdminQuery()",
			"(the caller is not logged in OR `x` does not hold) AND the request is not a service-admin query",
		},
		"rate limit, negated": {
			"en",
			`!ratelimit.ByIP(ktx, "Login", ratelimit.MaxRequestsPerWindow(5), ratelimit.LimitWindow(2*time.Hour))`,
			"the caller is within the `Login` rate limit by IP (5 requests per 2 hours)",
		},
		"rate limit without options": {
			"fr", `ratelimit.ByAccount(ktx, "Login", kaid)`,
			"l'appelant dépasse la limite de débit `Login` par compte (sa limite par défaut)",
		},
		"rate limit with a computed window": {
			"en",
			`ratelimit.ByIP(ktx, key, ratelimit.MaxRequestsPerWindow(n), ratelimit.LimitWindow(window))`,
			"the caller exceeds the `key` rate limit by IP (`n` requests per `window`)",
		},
		"phantom accounts": {
			"en", "!acl.IsLoggedIn(ktx, acl.PhantomProhibited)",
			"the caller is not logged in with a non-phantom account",
		},
		"permission for a user": {
			"en",
			"!acl.ActorHasPermission(ktx, capabilities.CanViewUserIdentity, acl.UserScope(k))",
			"the caller lacks permission `CanViewUserIdentity` for the user `k`",
		},
		"permission for any user": {
			"en", "!acl.ActorHasPermission(ktx, capabilities.CanChangeUserData, acl.AnyScope)",
			"the caller lacks permission `CanChangeUserData` for any user",
		},
		"unknown predicate is quoted verbatim": {
			"en", "!canSeeBioFields(ktx, *obj.Kaid)",
			"`canSeeBioFields(ktx, *obj.Kaid)` does not hold",
		},
		"french": {
			"fr",
			"!acl.IsCurrentUser(ktx, k) && !ktx.RequestIsServiceAdminQuery()",
			"l'appelant n'est pas l'utilisateur `k` ET la requête n'est pas une requête service-admin",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			expr, err := parser.ParseExpr(tc.cond)
			if err != nil {
				t.Fatalf("ParseExpr: %v", err)
			}
			w := wordingFor(tc.lang)
			r := renderer{w: &w, fset: token.NewFileSet()}
			if got := r.cond(expr, false); got != tc.want {
				t.Fatalf("cond = %q\nwant   %q", got, tc.want)
			}
		})
	}
}

func TestSourceLine(t *testing.T) {
	const flat = "f(a, b)"
	cases := map[string]struct{ src, want string }{
		"single line": {flat, flat},
		"args on their own lines": {
			"acl.ActorHasPermissionErr(\n\tktx, capabilities.CanEditContent, acl.AnyScope)",
			"acl.ActorHasPermissionErr(ktx, capabilities.CanEditContent, acl.AnyScope)",
		},
		"trailing comma": {"f(\n\ta,\n\tb,\n)", flat},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := sourceLine(tc.src); got != tc.want {
				t.Fatalf("sourceLine(%q) = %q, want %q", tc.src, got, tc.want)
			}
		})
	}
}
