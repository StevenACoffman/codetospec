package graph_test

import (
	"strings"
	"testing"

	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/graph"
)

// The status enumeration's fact id and file.
const (
	statusEnum = "enum.shop/order.go#Status"
	orderFile  = "shop/order.go"
)

// enumFacts is a status enumeration of three values, two transitions, and
// a value of an enumeration no fact declares.
func enumFacts() []extract.Fact {
	value := func(name, value, line string) extract.Fact {
		return extract.Fact{
			Kind: extract.KindEnumValue,
			ID:   "enum_value." + orderFile + "#" + name,
			Attrs: map[string]string{
				extract.AttrEnum:  statusEnum,
				extract.AttrName:  name,
				extract.AttrValue: value,
			},
			Source: extract.Ref{Path: orderFile, Lines: line + "-" + line},
		}
	}
	transition := func(from, to, lines string) extract.Fact {
		return extract.Fact{
			Kind: extract.KindTransition,
			Attrs: map[string]string{
				extract.AttrEnum: statusEnum,
				extract.AttrFrom: from,
				extract.AttrTo:   to,
			},
			Source: extract.Ref{Path: "shop/pay.go", Lines: lines},
		}
	}
	orphan := value("Lost", "lost", "40")
	orphan.Attrs[extract.AttrEnum] = "enum.other.go#Gone"
	return []extract.Fact{
		{
			Kind: extract.KindEnum, ID: statusEnum,
			Attrs:  map[string]string{extract.AttrName: "shop.Status"},
			Source: extract.Ref{Path: orderFile, Lines: "2-4"},
		},
		// Sorted by id, as merged facts are: not in declaration order.
		value(
			"Paid",
			"paid",
			"7",
		),
		value("Pending", "pending", "6"),
		value("Shipped", "shipped", "8"),
		transition("Pending", "Paid", "19-21"),
		transition("Paid", "Shipped", "26-27"),
		orphan,
	}
}

// TestBuildEnumNode: an enumeration becomes one node listing its values in
// declaration order and its transitions sorted, citing all of them.
func TestBuildEnumNode(t *testing.T) {
	nodes := graph.Build(enumFacts(), nil, extract.DomainResolver{}, "en", nil)

	var enums []graph.Node
	for _, n := range nodes {
		if n.Type == graph.TypeEnum {
			enums = append(enums, n)
		}
	}
	if len(enums) != 1 {
		t.Fatalf("enum nodes = %d, want 1: %+v", len(enums), enums)
	}
	n := enums[0]
	wantBody := "**Values**:\n\n- `Pending` = `pending`\n- `Paid` = `paid`\n- `Shipped` = `shipped`" +
		"\n\n**Transitions** (code that, while the value is From, sets it to To):\n\n" +
		"| From | To | Code |\n|------|----|------|\n" +
		"| `Paid` | `Shipped` | `shop/pay.go:26-27` |\n| `Pending` | `Paid` | `shop/pay.go:19-21` |"
	cases := map[string]struct{ got, want string }{
		"id":          {n.ID, "enum.shop-status"},
		"title":       {n.Title, "shop.Status"},
		"body":        {n.Body, wantBody},
		"transitions": {n.Extra[graph.ExtraTransitions], "2"},
		"sources": {fmtRefs(n.Sources), strings.Join([]string{
			"shop/order.go:2-4", "shop/order.go:6-6", "shop/order.go:7-7", "shop/order.go:8-8",
			"shop/pay.go:19-21", "shop/pay.go:26-27",
		}, " ")},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("got\n%s\nwant\n%s", tc.got, tc.want)
			}
		})
	}
}

// fmtRefs renders refs as "file:lines", space-separated.
func fmtRefs(refs []extract.Ref) string {
	out := make([]string, 0, len(refs))
	for _, r := range refs {
		out = append(out, r.File()+":"+r.Lines)
	}
	return strings.Join(out, " ")
}
