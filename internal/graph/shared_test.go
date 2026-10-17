package graph_test

import (
	"reflect"
	"testing"

	"github.com/StevenACoffman/codetospec/internal/graph"
)

func TestSharedEntities(t *testing.T) {
	entity := func(id, title string) graph.Node {
		return graph.Node{ID: id, Type: graph.TypeEntity, Title: title}
	}
	rule := func(id, domain string, touches ...string) graph.Node {
		n := graph.Node{ID: id, Type: graph.TypeRule, Edges: []graph.Edge{
			{Type: graph.EdgeBelongsTo, To: "domain." + domain},
		}}
		for _, e := range touches {
			n.Edges = append(n.Edges, graph.Edge{Type: graph.EdgeTouches, To: e})
		}
		return n
	}
	cases := map[string]struct {
		nodes []graph.Node
		want  []graph.SharedEntity
	}{
		"touched from two domains": {
			nodes: []graph.Node{
				entity("entity.invoice", "Invoice"),
				rule("rule.billing.a", "billing", "entity.invoice"),
				rule("rule.tax.b", "tax", "entity.invoice"),
				rule("rule.billing.c", "billing", "entity.invoice"),
			},
			want: []graph.SharedEntity{
				{ID: "entity.invoice", Title: "Invoice", Domains: []string{"billing", "tax"}},
			},
		},
		"touched from one domain only": {
			nodes: []graph.Node{
				entity("entity.invoice", "Invoice"),
				rule("rule.billing.a", "billing", "entity.invoice"),
				rule("rule.billing.b", "billing", "entity.invoice"),
			},
		},
		"a link to no entity node is ignored": {
			nodes: []graph.Node{
				rule("rule.billing.a", "billing", "entity.ghost"),
				rule("rule.tax.b", "tax", "entity.ghost"),
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := graph.SharedEntities(tc.nodes); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}
