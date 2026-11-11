package graph_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/StevenACoffman/codetospec/internal/graph"
)

// Ids the move tests share.
const (
	readDomain  = "domain.read"
	writeDomain = "domain.write"
	ruleA       = "rule.read.a"
	ruleC       = "rule.write.c"
	workSlug    = "work"
)

// moveFixture: domain read (partial, a gap, depending on a service and on
// domain write) holds rules a and b; domain write holds rule c.
func moveFixture() []graph.Node {
	rule := func(id, domain, concept string) graph.Node {
		n := graph.Node{
			ID: id, Type: graph.TypeRule, Title: strings.ToUpper(id[len(id)-1:]),
			Edges: []graph.Edge{{Type: graph.EdgeBelongsTo, To: "domain." + domain}},
		}
		if concept != "" {
			n.Extra = map[string]string{"concept": concept}
		}
		return n
	}
	return []graph.Node{
		{
			ID:   readDomain,
			Type: graph.TypeDomain,
			Body: "Reads assignments.\n\n## Rules\n\n- [A](../rules/read.a.md)\n- [B](../rules/read.b.md)",
			Extra: map[string]string{
				graph.ExtraCoverage:     graph.CoveragePartial,
				graph.ExtraCoverageGaps: "map-failed read.go:1-9",
			},
			Edges: []graph.Edge{
				{Type: graph.EdgeDependsOn, To: writeDomain},
				{Type: graph.EdgeDependsOn, To: "service.users"},
			},
		},
		{
			ID: writeDomain, Type: graph.TypeDomain,
			Body:  "Writes them.\n\n## Rules\n\n- [C](../rules/write.c.md)",
			Extra: map[string]string{graph.ExtraCoverage: graph.CoverageComplete},
		},
		rule(ruleA, "read", "work.Assignment"),
		rule("rule.read.b", "read", ""),
		rule(ruleC, "write", "work.Submission"),
	}
}

// TestMove: moved rules change domain, not id; the source keeps its
// summary and remaining rules; an emptied domain with no gap goes, and
// the edges to it; the target lists its rules by concept, inherits the
// gap and the service dependency, and depends on what it declares.
func TestMove(t *testing.T) {
	nodes := moveFixture()
	before := slices.Clone(nodes)
	out := graph.Move(nodes,
		map[string]string{ruleA: workSlug, ruleC: workSlug},
		[]graph.Target{{
			Slug: workSlug, Summary: "Coursework.", GroupBy: "concept",
			DependsOn: []string{readDomain, "domain.gone"},
		}},
		"en")
	if !slices.EqualFunc(nodes, before, func(a, b graph.Node) bool {
		return a.ID == b.ID && slices.Equal(a.Edges, b.Edges) && a.Body == b.Body
	}) {
		t.Fatal("Move modified its input")
	}
	byID := map[string]graph.Node{}
	var ids []string
	for _, n := range out {
		byID[n.ID] = n
		ids = append(ids, n.ID)
	}
	want := []string{readDomain, "domain.work", ruleA, "rule.read.b", ruleC}
	if !slices.Equal(ids, want) {
		t.Fatalf("ids = %v, want %v (write dropped, ids kept)", ids, want)
	}
	for id, domain := range map[string]string{
		ruleA: workSlug, "rule.read.b": "read", ruleC: workSlug,
	} {
		n := byID[id]
		if got := graph.RuleDomain(&n); got != domain {
			t.Errorf("%s in %s, want %s", id, got, domain)
		}
	}
	read := byID[readDomain]
	if read.Body != "Reads assignments.\n\n## Rules\n\n- [B](../rules/read.b.md)" {
		t.Errorf("read body = %q", read.Body)
	}
	if slices.Contains(read.Edges, graph.Edge{Type: graph.EdgeDependsOn, To: writeDomain}) {
		t.Errorf("edge to the dropped domain kept: %v", read.Edges)
	}
	work := byID["domain.work"]
	wantBody := "Coursework.\n\n## Rules\n\n### work.Assignment\n\n- [A](../rules/read.a.md)\n\n" +
		"### work.Submission\n\n- [C](../rules/write.c.md)"
	if work.Body != wantBody {
		t.Errorf("work body = %q\nwant        %q", work.Body, wantBody)
	}
	if work.Extra[graph.ExtraCoverage] != graph.CoveragePartial ||
		work.Extra[graph.ExtraCoverageGaps] != "map-failed read.go:1-9" {
		t.Errorf("work coverage = %v", work.Extra)
	}
	wantEdges := []graph.Edge{
		{Type: graph.EdgeDependsOn, To: readDomain},
		{Type: graph.EdgeDependsOn, To: "service.users"},
	}
	if !slices.Equal(work.Edges, wantEdges) {
		t.Errorf("work edges = %v, want %v (declared, inherited service, dropped none left)",
			work.Edges, wantEdges)
	}
}
