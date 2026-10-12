package graph_test

import (
	"strings"
	"testing"

	"codetospec/internal/extract"
	"codetospec/internal/graph"
)

func dependency(path, lines, service, operation string) extract.Fact {
	return extract.Fact{
		Kind:   extract.KindDependency,
		ID:     "dependency." + path + ":" + lines + ":" + service,
		Attrs:  map[string]string{extract.AttrService: service, extract.AttrOperation: operation},
		Source: extract.Ref{Path: path, Lines: lines},
	}
}

func TestBuildServiceNodes(t *testing.T) {
	const caller = "tasks/nickname.go"
	facts := []extract.Fact{
		{
			Kind:   "module",
			Attrs:  map[string]string{"name": "tasks"},
			Source: extract.Ref{Path: caller, Lines: "1-1"},
		},
		dependency(caller, "40-40", "assignments", "Mutation.acceptInvitation"),
		dependency(caller, "12-12", "assignments", "Query.classroom"),
		dependency(caller, "50-50", "Not A Slug", "Query.ignored"),
		ruleFact("rule.x"), // gives the caller's domain a domain node
	}
	facts[4].Attrs[extract.AttrDomain] = "tasks"
	nodes := graph.Build(facts, nil, extract.DomainResolver{Strategy: "namespace"}, "en")

	byID := make(map[string]graph.Node)
	for _, n := range nodes {
		byID[n.ID] = n
	}
	service, ok := byID["service.assignments"]
	if !ok {
		t.Fatalf("no service node; got %v", nodeIDs(nodes))
	}
	if len(service.Sources) != 2 || service.Sources[0].Lines != "40-40" {
		t.Errorf("service sources = %v, want both call sites sorted by operation", service.Sources)
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
