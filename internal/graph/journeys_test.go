package graph_test

import (
	"strings"
	"testing"

	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/graph"
	"github.com/StevenACoffman/codetospec/internal/reducer"
)

// TestBuildJourneys: an endpoint lists the rules its handler applies, then
// those of what it calls, nearest first; code reached only through an
// interface, or not reached, is not part of it.
func TestBuildJourneys(t *testing.T) {
	const handlerFile = "api/handler.go"
	ref := func(path, lines string) extract.Ref { return extract.Ref{Path: path, Lines: lines} }
	call := func(caller, callee extract.Ref, dispatch string) extract.Fact {
		attrs := map[string]string{
			extract.AttrCalleePath: callee.Path, extract.AttrCalleeLines: callee.Lines,
		}
		if dispatch != "" {
			attrs[extract.AttrDispatch] = dispatch
		}
		return extract.Fact{Kind: extract.KindCall, Source: caller, Attrs: attrs}
	}
	handler, charge := ref(handlerFile, "1-10"), ref("billing/charge.go", "20-30")
	round, iface := ref("billing/round.go", "5-8"), ref("billing/iface.go", "1-4")
	facts := []extract.Fact{
		{
			Kind: extract.KindRoute, ID: "route.post./charge",
			Attrs: map[string]string{
				extract.AttrMethod: "POST", extract.AttrPath: "/charge",
				extract.AttrHandlerPath: handlerFile, extract.AttrHandlerLines: "1-10",
			},
			Source: ref("routes.go", "3-3"),
		},
		call(handler, charge, ""),
		call(charge, round, ""),
		call(handler, iface, extract.DispatchInterface),
	}
	rule := func(slug string, cites extract.Ref) reducer.Rule {
		return reducer.Rule{
			Slug: slug, Title: slug, EarsKind: "ubiquitous",
			Requirement: "The system shall " + slug + ".",
			Citations:   []extract.Ref{cites}, Nature: natureBusiness, Origin: originExplicit,
		}
	}
	reduced := []reducer.Output{
		{Domain: "api", Rules: []reducer.Rule{rule("check-input", ref(handlerFile, "3-5"))}},
		{Domain: billingDomain, Rules: []reducer.Rule{
			rule("round", round),
			rule("charge", charge),
			rule("via-iface", iface),
			rule("unrelated", ref("billing/other.go", "1-3")),
		}},
	}

	nodes := graph.Build(facts, reduced, extract.DomainResolver{}, "en", nil)

	var endpoint *graph.Node
	for i := range nodes {
		if nodes[i].ID == "endpoint.post-charge" {
			endpoint = &nodes[i]
		}
	}
	if endpoint == nil {
		t.Fatal("no endpoint node")
	}
	want := "rule.api.check-input, rule.billing.charge, rule.billing.round"
	if got := endpoint.Extra[graph.ExtraJourney]; got != want {
		t.Errorf("journey = %q, want %q", got, want)
	}
	if !strings.Contains(endpoint.Body,
		"**Journey** (the rules the handler applies, then those of what it calls):\n"+
			"1. `rule.api.check-input`: The system shall check-input.") {
		t.Errorf("body does not list the journey:\n%s", endpoint.Body)
	}
}
