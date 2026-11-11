package domainmodel_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/StevenACoffman/codetospec/internal/alignment"
	"github.com/StevenACoffman/codetospec/internal/concept"
	"github.com/StevenACoffman/codetospec/internal/domainmodel"
	"github.com/StevenACoffman/codetospec/internal/graph"
)

// The shop model's scope and its Order entity, as concepts name them.
const (
	shopScope = "shop"
	shopOrder = shopScope + ".Order"
)

// TestConcepts: each model entity with the code entity it matched, its
// table and the entity owning it by an owned relationship; and every
// relationship and is-a link, qualified (Relations).
func TestConcepts(t *testing.T) {
	const shopCoupon = "shop.Coupon"
	path := filepath.Join(t.TempDir(), "shop.modelith.yaml")
	model := `kind: DomainModel
version: v1
title: Shop
entities:
  Order:
    definition: A customer's request to buy items.
    relationships:
      - entity: Line
        cardinality: "1:n"
        ownership: owned
      - entity: Coupon
        cardinality: "n:1"
        ownership: referenced
  Line:
    definition: One item of an order.
  Coupon:
    definition: A discount code.
  GiftCoupon:
    definition: A coupon bought as a gift.
    subtypeOf: Coupon
`
	if err := os.WriteFile(path, []byte(model), 0o600); err != nil {
		t.Fatal(err)
	}
	set, err := domainmodel.Load([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	nodes := []graph.Node{entity("entity.orders", "orders"), entity("entity.lines", "lines")}
	report := domainmodel.Compare(set, nodes, domainmodel.Options{})
	got := domainmodel.Concepts(set, &report)
	want := []concept.Concept{
		{Name: shopCoupon},
		{Name: "shop.GiftCoupon"},
		{Name: "shop.Line", Entity: "entity.lines", Table: "lines", Owner: shopOrder},
		{Name: shopOrder, Entity: "entity.orders", Table: "orders"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("concepts = %+v\nwant       %+v", got, want)
	}
	relations := []alignment.Relation{
		{From: "shop.GiftCoupon", To: shopCoupon},
		{From: shopOrder, To: "shop.Line"},
		{From: shopOrder, To: shopCoupon},
	}
	if got := domainmodel.Relations(set); !reflect.DeepEqual(got, relations) {
		t.Errorf("relations = %+v\nwant        %+v", got, relations)
	}
}

// TestCheckOwned: an owned scope or entity the models declare passes; an
// unknown one, or any without models, is refused.
func TestCheckOwned(t *testing.T) {
	set, loadErr := domainmodel.Load([]string{"testdata/shop.modelith.yaml"})
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	cases := map[string]struct {
		set     *domainmodel.Set
		owned   string
		wantErr bool
	}{
		"scope":          {set: set, owned: shopScope},
		"entity":         {set: set, owned: shopOrder},
		"unknown entity": {set: set, owned: "shop.Refund", wantErr: true},
		"no models":      {owned: shopScope, wantErr: true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := domainmodel.CheckOwned(tc.set, map[string]string{tc.owned: "the Shop"})
			if (err != nil) != tc.wantErr {
				t.Errorf("err = %v, want error %v", err, tc.wantErr)
			}
		})
	}
}
