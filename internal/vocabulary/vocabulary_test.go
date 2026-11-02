package vocabulary_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/StevenACoffman/codetospec/internal/condition"
	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/vocabulary"
	"github.com/StevenACoffman/indomain/pkg/model"
)

// Names the cases share.
const (
	guardFile  = "resolvers/visit.go" // the file of the extractor rule of facts
	otherFile  = "other.go"           // a file without extractor rules
	statusEnum = "enum.status"
	status     = "shop.Status"
	closedAt   = "visit.closedAt"
)

// facts is one rule with a condition, one table, one enumeration.
func facts() []extract.Fact {
	return []extract.Fact{
		{
			Kind: extract.KindRule, ID: "rule.x",
			Attrs: map[string]string{
				extract.AttrCondition: `{"all": [{"not": {"var": "caller.loggedIn", "op": "equal", "value": true}},` +
					` {"exists": "visit.closedAt"}]}`,
			},
			Source: extract.Ref{Path: guardFile, Lines: "3-9"},
		},
		{
			Kind: extract.KindTable,
			ID:   "table.visit",
			Attrs: map[string]string{
				extract.AttrName:    "Visit",
				extract.AttrColumns: "id:int, closed_at:time,status:text",
			},
		},
		{
			Kind:  extract.KindEnum,
			ID:    statusEnum,
			Attrs: map[string]string{extract.AttrName: status},
		},
		{
			Kind:  extract.KindEnumValue,
			Attrs: map[string]string{extract.AttrEnum: statusEnum, extract.AttrName: "Open"},
		},
		{
			Kind:  extract.KindEnumValue,
			Attrs: map[string]string{extract.AttrEnum: statusEnum, extract.AttrName: "Closed"},
		},
	}
}

// models is a domain model naming one attribute the schema also has.
func models() []*model.Model {
	return []*model.Model{
		{
			Entities: map[string]model.Entity{
				"Visit": {Attributes: []model.Attribute{{Name: "status"}, {Name: "mileage"}}},
			},
			Enums: map[string]model.Enum{
				"Priority": {Values: []model.EnumValue{{Name: "Low"}, {Name: "High"}}},
			},
		},
	}
}

// TestFor: a chunk gets its file's rule variables and the terms whose last
// part it mentions, by source then name.
func TestFor(t *testing.T) {
	v := vocabulary.Build(facts(), models())
	cases := map[string]struct {
		file, content string
		want          []string
	}{
		"the rule's file": {
			guardFile, "func f() {}",
			[]string{"caller.loggedIn", closedAt},
		},
		"columns by word, snake and camel alike": {
			otherFile, "if v.ClosedAt.IsZero() && v.Status == open {}",
			[]string{"Visit.closed_at", "Visit.status", status},
		},
		"model attribute": {
			otherFile, "total := mileage * rate",
			[]string{"Visit.mileage"},
		},
		"model enumeration": {
			otherFile, "if p == Priority(2) {}",
			[]string{"Priority"},
		},
		"nothing mentioned": {otherFile, "return nil", nil},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := vocabulary.Names(v.For(tc.file, tc.content)); !slices.Equal(got, tc.want) {
				t.Errorf("For = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestForStructFields: a struct field is offered only where its struct is
// named too, by its whole name or the last part a variable takes after.
func TestForStructFields(t *testing.T) {
	v := vocabulary.Build([]extract.Fact{{
		Kind: extract.KindStruct, ID: "struct.jobs/job.go#RosterJob",
		Attrs: map[string]string{
			extract.AttrName:   "jobs.RosterJob",
			extract.AttrFields: "Attempts,ExpiresAt",
		},
	}}, nil)
	cases := map[string]struct {
		content string
		want    []string
	}{
		"the type named": {
			"func (r *RosterJob) Run() { if r.Attempts > 3 {} }",
			[]string{"jobs.RosterJob.Attempts"},
		},
		"a variable after it": {
			"if job.ExpiresAt.Before(now) {}",
			[]string{"jobs.RosterJob.ExpiresAt"},
		},
		"the field alone": {"if task.Attempts > 3 {}", nil},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := vocabulary.Names(v.For(otherFile, tc.content)); !slices.Equal(got, tc.want) {
				t.Errorf("For = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestBuildPrefersSources: a name two sources give is kept once, at the
// most preferred source; an enumeration carries its values.
func TestBuildPrefersSources(t *testing.T) {
	extra := models()
	extra[0].Entities["Visit"] = model.Entity{Attributes: []model.Attribute{{Name: "status"}}}
	v := vocabulary.Build(facts(), extra)
	terms := v.For(otherFile, "status Status")
	byName := make(map[string]vocabulary.Term)
	for _, term := range terms {
		byName[term.Name] = term
	}
	cases := map[string]struct {
		source vocabulary.Source
		values []string
	}{
		"Visit.status": {vocabulary.FromSchema, nil},
		status:         {vocabulary.FromSchema, []string{"Open", "Closed"}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			term, ok := byName[name]
			if !ok {
				t.Fatalf("no term %s in %v", name, vocabulary.Names(terms))
			}
			if term.Source != tc.source || !slices.Equal(term.Values, tc.values) {
				t.Errorf("term = %+v, want source %d values %q", term, tc.source, tc.values)
			}
		})
	}
}

// TestForCaps: no chunk gets more than MaxTerms terms.
func TestForCaps(t *testing.T) {
	var columns, content strings.Builder
	for i := range vocabulary.MaxTerms + 10 {
		name := "c" + string(rune('a'+i%26)) + string(rune('a'+i/26))
		columns.WriteString(name + ":int,")
		content.WriteString(name + " ")
	}
	v := vocabulary.Build([]extract.Fact{{
		Kind: extract.KindTable, ID: "table.t",
		Attrs: map[string]string{extract.AttrName: "t", extract.AttrColumns: columns.String()},
	}}, nil)
	if got := len(v.For("x.go", content.String())); got != vocabulary.MaxTerms {
		t.Errorf("For returned %d terms, want %d", got, vocabulary.MaxTerms)
	}
}

// TestCoined: variables outside the names go under Local; known ones and
// already coined ones stay.
func TestCoined(t *testing.T) {
	c, err := condition.Parse([]byte(`{"all": [{"exists": "visit.closedAt"}, ` +
		`{"var": "local.retries", "op": "less_than", "value": 3}, ` +
		`{"var": "attempts", "op": "greater_than", "value": 1}]}`))
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]struct {
		names []string
		want  []string
		moved int
	}{
		"one unknown": {
			[]string{closedAt},
			[]string{"local.attempts", "local.retries", closedAt},
			1,
		},
		"none known": {
			nil,
			[]string{"local.attempts", "local.retries", "local.visit.closedAt"},
			2,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, moved := vocabulary.Coined(&c, tc.names)
			if moved != tc.moved || !slices.Equal(got.Variables(), tc.want) {
				t.Errorf(
					"Coined = %q (%d moved), want %q (%d)",
					got.Variables(),
					moved,
					tc.want,
					tc.moved,
				)
			}
		})
	}
}
