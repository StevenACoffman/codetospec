package extract_test

import (
	"reflect"
	"testing"

	"github.com/StevenACoffman/codetospec/internal/extract"
)

// TestColumnLinks: a column no key declares, named as a one-column key to
// another table elsewhere, is a candidate link; keyed columns, composite
// keys, self links and tables already referenced are not.
func TestColumnLinks(t *testing.T) {
	table := func(name, columns, references string) extract.Fact {
		return extract.Fact{
			Kind: extract.KindTable, ID: "table." + name,
			Attrs: map[string]string{
				extract.AttrName: name, extract.AttrColumns: columns,
				extract.AttrReferences: references,
			},
		}
	}
	facts := []extract.Fact{
		table("assignments", "id:bigserial, assignment_id:text", ""),
		table("configs", "assignment_id:bigserial", "assignment_id:assignments:not_null"),
		table(
			"students",
			"kaid:text, assignment_id:bigserial",
			"assignment_id:assignments:not_null",
		),
		table("tasks", "kaid:text, assignment_id:bigserial, task:text",
			"kaid+assignment_id:students:not_null"),
		table("ChangeEvent", "assignment_id:int64, kaid:string", ""),
		table("notes", "assignment_id:text, student:text", "student:assignments:null"),
		{
			Kind:  extract.KindSymbol,
			ID:    "symbol.x",
			Attrs: map[string]string{extract.AttrColumns: "assignment_id"},
		},
	}
	want := []extract.ColumnLink{
		{Table: "ChangeEvent", Column: "assignment_id", Target: "assignments"},
	}
	if got := extract.ColumnLinks(facts); !reflect.DeepEqual(got, want) {
		t.Errorf("links = %+v, want %+v", got, want)
	}
	if got, want := want[0].String(), "ChangeEvent.assignment_id → assignments"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}
