package main

import (
	"go/ast"
	"maps"
	"slices"
	"strings"
)

// kindValueKind says what a declared type's values are (codetospec's
// extract.KindValueKind): attrType the type, attrValueKind the kind.
const (
	kindValueKind = "value_kind"
	attrValueKind = "value_kind"
	attrType      = "type"
)

// Meanings of a check call (check_args' "means"): the generic checks
// codetospec's policy exports translate, so the core never names webapp's
// functions. A call this table doesn't know has no meaning; a person may
// pin one.
const (
	meansLogin        = "login"         // acl.IsLoggedIn
	meansLoginStrict  = "login_strict"  // acl.IsLoggedIn(…, acl.PhantomProhibited)
	meansCapability   = "capability"    // acl.ActorHasPermission[Err]
	meansSelf         = "self"          // acl.IsCurrentUser
	meansCoach        = "coach"         // acl.IsCoachedByActor
	meansServiceAdmin = "service_admin" // RequestIsServiceAdminQuery
	meansTask         = "task"          // RequestIsTask
	meansOkta         = "okta"          // RequestIsOktaLifecycleQuery
	meansRequest      = "request"       // another request predicate
	meansPush         = "push"          // pubsub.DecodeAndValidatePushMsg
)

// meaningOf is what the check call named name means, "" when unknown.
func meaningOf(r renderer, call *ast.CallExpr, name string) string {
	last := name[strings.LastIndex(name, ".")+1:]
	switch name {
	case aclPkg + "." + isLoggedIn:
		if r.hasArg(call, "PhantomProhibited") {
			return meansLoginStrict
		}
		return meansLogin
	case aclPkg + ".ActorHasPermission", aclPkg + ".ActorHasPermissionErr":
		return meansCapability
	case aclPkg + ".IsCurrentUser":
		return meansSelf
	case aclPkg + ".IsCoachedByActor":
		return meansCoach
	case "pubsub.DecodeAndValidatePushMsg":
		// Validates the push's OIDC token: the request is Pub/Sub's.
		return meansPush
	}
	switch {
	case last == isServiceAdminQuery:
		return meansServiceAdmin
	case last == "RequestIsTask":
		return meansTask
	case last == "RequestIsOktaLifecycleQuery":
		return meansOkta
	case strings.HasPrefix(name, "method.Request"):
		return meansRequest
	}
	return ""
}

// scopeOf is what an acl scope matcher means (check_args' "scope"): any
// grant, a global one, one on users, one on the subject's country; "" for
// a matcher the exports have no template for.
func scopeOf(matcher string) string {
	switch matcher {
	case aclPkg + ".AnyScope":
		return scopeAny
	case aclPkg + ".GlobalScope":
		return scopeGlobal
	case aclPkg + ".UserScope":
		return scopeUser
	case aclPkg + ".CountryScope":
		return scopeCountry
	}
	return ""
}

// The meanings scopeOf gives the acl matchers.
const (
	scopeAny     = "any"
	scopeGlobal  = "global"
	scopeUser    = "user"
	scopeCountry = "country"
)

// valueKinds are the declared types whose values are staff capabilities
// or feature flags (value_kind facts): what the policy inventory sorts a
// check's used values by.
func valueKinds() map[string]string {
	return map[string]string{
		"permissions.CapabilityName":   "staff",
		"capabilities.Capability":      "staff",
		"featureflags.FeatureFlagName": "flag",
	}
}

// valueKindFacts returns one value_kind fact per type of valueKinds,
// uncited (the table is this extractor's knowledge, not the code's),
// sorted by type.
func valueKindFacts() []Fact {
	kinds := valueKinds()
	facts := make([]Fact, 0, len(kinds))
	for _, typ := range slices.Sorted(maps.Keys(kinds)) {
		facts = append(facts, Fact{
			Kind:      kindValueKind,
			ID:        "value_kind.webapp." + typ,
			Attrs:     map[string]string{attrType: typ, attrValueKind: kinds[typ]},
			Origin:    origin,
			Certainty: certain,
		})
	}
	return facts
}
