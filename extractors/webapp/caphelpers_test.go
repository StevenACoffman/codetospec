package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// TestCapabilityHelpers: a function whose decision is admin's
// hasPermission for one capability is a capability check: its scope from
// the query's enum, its capability the Go name of a literal, or the
// argument its variable is bound to; a function combining two is none.
func TestCapabilityHelpers(t *testing.T) {
	in := &Inputs{
		Service: os.DirFS(filepath.Join("testdata", "service")),
		Webapp:  os.DirFS(filepath.Join("testdata", "webapp")),
	}
	names, err := goSources(in.Service, nil)
	if err != nil {
		t.Fatal(err)
	}
	sources, _ := parseSources(in.Service, names)
	got := capabilityHelpers(in, sources)
	want := map[string]capHelper{
		"cross_service.ActorCanEditAnyContent": {
			capability: "CanEditContent", capabilityArg: -1, scope: scopeLocale,
		},
		"cross_service.ActorCanMapStandards": {
			capability: "CanMapStandards", capabilityArg: -1, scope: scopeLocale,
		},
		"cross_service.UserKAIDIfActorHasCapability": {
			capabilityArg: 1, scope: scopeLocale, results: checker{nilDenies: true},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("capabilityHelpers = %+v\nwant %+v", got, want)
	}

	// A call of one is a known check with that meaning, the capability
	// taken from its argument when the query's name is a variable.
	known, _ := knownChecks(sources, nil, in)
	expr, err := parser.ParseExpr(
		"cross_service.UserKAIDIfActorHasCapability(ctx, string(capabilities.CanPublishSiteEditor))",
	)
	if err != nil {
		t.Fatal(err)
	}
	call, _ := expr.(*ast.CallExpr)
	name, isKnown := known.nameOf(call)
	if !isKnown || known.formOf(name) != formCapability {
		t.Fatalf(
			"nameOf = %q, %v (form %q); want a capability check",
			name,
			isKnown,
			known.formOf(name),
		)
	}
	cc := callOperands(token.NewFileSet(), call, name)
	cc.withHelper(known.capHelpers[name], call)
	if cc.Means != meansCapability || cc.Scope != scopeLocale ||
		cc.Capability != "CanPublishSiteEditor" {
		t.Errorf("call = %+v, want CanPublishSiteEditor on the request's locale", cc)
	}
}
