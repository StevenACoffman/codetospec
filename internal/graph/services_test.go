package graph_test

import (
	"strings"
	"testing"

	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/graph"
)

// caller is the file that makes the service calls.
const caller = "tasks/nickname.go"

// dependency is a call from caller's lines to an operation of service.
func dependency(lines, service, operation string) extract.Fact {
	return extract.Fact{
		Kind:   extract.KindDependency,
		ID:     "dependency." + caller + ":" + lines + ":" + service,
		Attrs:  map[string]string{extract.AttrService: service, extract.AttrOperation: operation},
		Source: extract.Ref{Path: caller, Lines: lines},
	}
}

func TestBuildServiceNodes(t *testing.T) {
	facts := []extract.Fact{
		{
			Kind:   extract.KindModule,
			Attrs:  map[string]string{extract.AttrName: "tasks"},
			Source: extract.Ref{Path: caller, Lines: firstLine},
		},
		dependency("40-40", "assignments", "Mutation.acceptInvitation"),
		dependency("12-12", "assignments", "Query.classroom"),
		dependency("60-60", "assignments", "Query.roster"),
		dependency("50-50", "Not A Slug", "Query.ignored"),
		ruleFact("rule.x"), // gives the caller's domain a domain node
	}
	facts[5].Attrs[extract.AttrDomain] = "tasks"
	facts[3].Attrs[extract.AttrAs] = extract.AsServiceAdmin
	nodes := graph.Build(facts, nil, extract.DomainResolver{Strategy: "namespace"}, "en", nil)

	byID := make(map[string]graph.Node)
	for _, n := range nodes {
		byID[n.ID] = n
	}
	service, ok := byID["service.assignments"]
	if !ok {
		t.Fatalf("no service node; got %v", nodeIDs(nodes))
	}
	if len(service.Sources) != 3 || service.Sources[0].Lines != "40-40" {
		t.Errorf("service sources = %v, want the call sites sorted by operation", service.Sources)
	}
	const adminCall = "`Query.roster` — `tasks/nickname.go:60-60` (service-admin query)"
	if !strings.Contains(service.Body, adminCall) ||
		strings.Contains(service.Body, "12-12` (service-admin") {
		t.Errorf("service body does not mark exactly the service-admin call:\n%s", service.Body)
	}
	if !strings.Contains(service.Body, "`Query.classroom` — `tasks/nickname.go:12-12`") {
		t.Errorf("service body lacks the call site:\n%s", service.Body)
	}
	for id := range byID {
		if strings.Contains(id, "Not A Slug") {
			t.Errorf("invalid service name became node %s", id)
		}
	}
	domain := byID["domain.tasks"]
	found := false
	for _, e := range domain.Edges {
		found = found || (e.Type == graph.EdgeDependsOn && e.To == "service.assignments")
	}
	if !found {
		t.Errorf("domain.tasks edges %v lack depends_on service.assignments", domain.Edges)
	}
}
