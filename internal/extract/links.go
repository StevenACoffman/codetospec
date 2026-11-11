package extract

import (
	"cmp"
	"slices"
	"strings"
)

// ColumnLink is a column its table declares no foreign key on, named as
// the single column of a foreign key another table declares to Target: a
// link the code may hold without saying so (a document store has no
// foreign keys), for a person to confirm. Never asserted: the name is
// the only evidence.
type ColumnLink struct {
	Table, Column, Target string
}

// String is the link as its question names it: "table.column → target".
func (l ColumnLink) String() string {
	return l.Table + "." + l.Column + " → " + l.Target
}

// ColumnLinks finds the column links of the table facts: for each column
// outside its table's foreign keys, each table that a single-column
// foreign key of the same name references elsewhere, unless the table
// already references it.
//
// Ensures: sorted by table, column, target; no table links to itself.
func ColumnLinks(facts []Fact) []ColumnLink {
	tables := linkTables(facts)
	targets := keyTargets(tables)
	var links []ColumnLink
	for _, t := range tables {
		for _, column := range t.columns {
			if t.keyed(column) {
				continue
			}
			for _, target := range targets[column] {
				if target != t.name && !t.references(target) {
					links = append(links, ColumnLink{Table: t.name, Column: column, Target: target})
				}
			}
		}
	}
	slices.SortFunc(links, func(a, b ColumnLink) int {
		return cmp.Or(cmp.Compare(a.Table, b.Table), cmp.Compare(a.Column, b.Column),
			cmp.Compare(a.Target, b.Target))
	})
	return links
}

// keyTargets maps each column a one-column foreign key uses to the
// tables those keys reference.
func keyTargets(tables []linkTable) map[string][]string {
	targets := map[string][]string{}
	for _, t := range tables {
		for _, r := range t.refs {
			if len(r.Columns) == 1 && !slices.Contains(targets[r.Columns[0]], r.Table) {
				targets[r.Columns[0]] = append(targets[r.Columns[0]], r.Table)
			}
		}
	}
	return targets
}

// linkTable is a table fact as ColumnLinks reads it.
type linkTable struct {
	name    string
	columns []string
	refs    []Reference
}

func linkTables(facts []Fact) []linkTable {
	var tables []linkTable
	for i := range facts {
		f := &facts[i]
		if f.Kind != KindTable {
			continue
		}
		t := linkTable{
			name: cmp.Or(f.Attrs[AttrName], strings.TrimPrefix(f.ID, "table.")),
			refs: ParseReferences(f.Attrs[AttrReferences]),
		}
		for column := range strings.SplitSeq(f.Attrs[AttrColumns], ",") {
			if name, _, _ := strings.Cut(strings.TrimSpace(column), ":"); name != "" {
				t.columns = append(t.columns, name)
			}
		}
		tables = append(tables, t)
	}
	return tables
}

// keyed reports whether a foreign key of t uses column.
func (t *linkTable) keyed(column string) bool {
	return slices.ContainsFunc(t.refs, func(r Reference) bool {
		return slices.Contains(r.Columns, column)
	})
}

// references reports whether a foreign key of t references target.
func (t *linkTable) references(target string) bool {
	return slices.ContainsFunc(t.refs, func(r Reference) bool { return r.Table == target })
}
