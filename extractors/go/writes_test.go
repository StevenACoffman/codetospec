package main

import (
	"slices"
	"testing"
)

// writerSource writes the shop's state from outside its methods: a
// function, a keyed literal, an increment, and through a pointer field.
const writerSource = `package shop

type Counter int

const (
	Zero Counter = 0
	One  Counter = 1
)

type Ledger struct {
	Order  *Order
	Next   *Status
	Count  Counter
	Note   string
	Single Kind
}

func Ship(o *Order) { o.Status = Shipped }

func Open() Ledger {
	return Ledger{Order: &Order{Status: Pending}, Note: "new", Single: Only}
}

func (l *Ledger) Bump() {
	l.Count++
	l.Order.Status = Paid
}

func Reset(l *Ledger, s Status) {
	*l = Ledger{}
	l.Next = &s
	l.Count += One
}
`

// TestWriteFacts: a function's writes of enum-typed fields of the module's
// structs, by assignment, increment or keyed literal, each named by its
// struct; a method's writes of its own type, a field that holds no
// enumeration, and a one-constant type's field are not.
func TestWriteFacts(t *testing.T) {
	m, dir := loadModule(t, "example.com/shop", map[string]string{
		"shop.go":   orderSource,
		"writer.go": writerSource,
	})
	var got []string
	for _, f := range WriteFacts(m.pkgs, m.fset, dir) {
		got = append(got, f.Source.Lines+" "+f.Attrs[attrField]+" "+f.Attrs[attrFieldPackage]+
			" "+f.Attrs[attrEnum])
	}
	want := []string{
		"18-18 shop.Order.Status example.com/shop enum.shop.go#Status",
		"20-22 shop.Order.Status example.com/shop enum.shop.go#Status",
		"24-27 shop.Order.Status example.com/shop enum.shop.go#Status",
		"29-33 shop.Ledger.Count example.com/shop enum.writer.go#Counter",
		"29-33 shop.Ledger.Next example.com/shop enum.shop.go#Status",
	}
	if !slices.Equal(got, want) {
		t.Errorf("writes =\n%v\nwant\n%v", got, want)
	}
}
