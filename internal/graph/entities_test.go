package graph_test

import (
	"testing"

	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/graph"
)

// TestEntityDescription: a table fact's authored description opens the
// entity page, between the table name and its columns.
func TestEntityDescription(t *testing.T) {
	cases := map[string]struct {
		description string
		want        string // expected entity body
	}{
		"described": {
			description: "A bill sent to a subscriber.",
			want: "**Table**: `invoices`\n\nA bill sent to a subscriber.\n\n" +
				"**Columns**:\n- `id:id`",
		},
		"undescribed": {
			want: "**Table**: `invoices`\n\n**Columns**:\n- `id:id`",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			attrs := map[string]string{extract.AttrName: "invoices", extract.AttrColumns: "id:id"}
			if tc.description != "" {
				attrs[extract.AttrDescription] = tc.description
			}
			fact := extract.Fact{
				Kind: extract.KindTable, ID: "table.invoices", Attrs: attrs,
				Source: extract.Ref{Path: "db/schema.sql", Lines: "1-3"},
			}
			for _, n := range graph.Build([]extract.Fact{fact}, nil, extract.DomainResolver{}, "en") {
				if n.Type != graph.TypeEntity {
					continue
				}
				if n.Body != tc.want {
					t.Fatalf("body:\n%s\nwant:\n%s", n.Body, tc.want)
				}
				return
			}
			t.Fatal("no entity node")
		})
	}
}
