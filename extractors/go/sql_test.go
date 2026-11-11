package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// Names the replay cases share.
const (
	assignmentID = "assignment_id"
	idInt        = "id:int"
)

// replayCase is migrations, applied in order, and the tables they leave.
type replayCase struct {
	files []string
	want  []Table
}

func runReplay(t *testing.T, cases map[string]replayCase) {
	t.Helper()
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			s := newSchema()
			for _, f := range tc.files {
				s.apply("", f)
			}
			if got := s.list(); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("tables =\n%+v\nwant\n%+v", got, tc.want)
			}
		})
	}
}

// TestSchemaReplayKeys: primary and foreign keys declared in the CREATE
// TABLE or by a later ALTER TABLE, a reference NOT NULL when its columns
// are, whenever that was declared.
func TestSchemaReplayKeys(t *testing.T) {
	runReplay(t, map[string]replayCase{
		"primary key from ALTER TABLE ADD CONSTRAINT": {
			files: []string{
				`CREATE TABLE configs (assignment_id bigserial REFERENCES assignments NOT NULL);
ALTER TABLE configs
	ADD CONSTRAINT configs_pkey PRIMARY KEY (assignment_id);`,
			},
			want: []Table{{
				Name: "configs", Columns: []string{"assignment_id:bigserial"},
				References: []Reference{{[]string{assignmentID}, "assignments", true}},
				PrimaryKey: []string{assignmentID}, Line: 1,
			}},
		},
		"table-level primary key makes its columns not null": {
			files: []string{`CREATE TABLE data (
    kaid text,
    assignment_id bigint,
    PRIMARY KEY (kaid, assignment_id),
    FOREIGN KEY (kaid, assignment_id) REFERENCES students(kaid, assignment_id)
);`},
			want: []Table{{
				Name: "data", Columns: []string{"kaid:text", "assignment_id:bigint"},
				References: []Reference{{[]string{"kaid", assignmentID}, "students", true}},
				PrimaryKey: []string{"kaid", assignmentID}, Line: 1,
			}},
		},
		"set not null after the fact": {
			files: []string{
				"CREATE TABLE b (a_id bigint REFERENCES a);",
				"ALTER TABLE b ALTER COLUMN a_id SET NOT NULL;",
			},
			want: []Table{{
				Name: "b", Columns: []string{"a_id:bigint"},
				References: []Reference{{[]string{"a_id"}, "a", true}}, Line: 1,
			}},
		},
		"dropped column takes its keys": {
			files: []string{
				"CREATE TABLE b (a_id bigint PRIMARY KEY REFERENCES a, x int);",
				"ALTER TABLE b DROP COLUMN a_id;",
			},
			want: []Table{{Name: "b", Columns: []string{"x:int"}, Line: 1}},
		},
	})
}

// TestSchemaReplayChanges: columns and tables added, changed, renamed and
// dropped, rollback sections and other statements never applied.
func TestSchemaReplayChanges(t *testing.T) {
	runReplay(t, map[string]replayCase{
		"columns added, altered and dropped across files": {
			files: []string{
				"CREATE TABLE a (id bigserial PRIMARY KEY, teacher text NOT NULL, unit text);",
				"ALTER TABLE a ADD COLUMN kind text, ADD COLUMN IF NOT EXISTS unit int;",
				"ALTER TABLE a DROP COLUMN IF EXISTS teacher;",
				"ALTER TABLE ONLY a ALTER COLUMN kind SET NOT NULL, ALTER COLUMN unit TYPE bigint;",
			},
			want: []Table{{
				Name: "a", Columns: []string{"id:bigserial", "unit:bigint", "kind:text"},
				PrimaryKey: []string{"id"}, Line: 1,
			}},
		},
		"renames: the references follow": {
			files: []string{
				"CREATE TABLE a (id int);\nCREATE TABLE b (a_id int REFERENCES a NOT NULL);",
				"ALTER TABLE a RENAME TO parents;\nALTER TABLE b RENAME COLUMN a_id TO parent_id;",
			},
			want: []Table{
				{Name: "parents", Columns: []string{idInt}, Line: 1},
				{
					Name: "b", Columns: []string{"parent_id:int"},
					References: []Reference{{[]string{"parent_id"}, "parents", true}}, Line: 2,
				},
			},
		},
		"dropped table": {
			files: []string{
				"CREATE TABLE a (id int);\nCREATE TABLE b (id int);",
				"DROP TABLE IF EXISTS a CASCADE;",
			},
			want: []Table{{Name: "b", Columns: []string{idInt}, Line: 2}},
		},
		"goose rollback section": {
			files: []string{`-- +goose Up
CREATE TABLE a (id int);
-- +goose Down
DROP TABLE a;`},
			want: []Table{{Name: "a", Columns: []string{idInt}, Line: 2}},
		},
		"other constraints and changes ignored": {
			files: []string{
				"CREATE TABLE a (id int, CONSTRAINT u UNIQUE (id));",
				"ALTER TABLE a ADD CONSTRAINT c CHECK (id > 0) NOT VALID;\n" +
					"ALTER TABLE a DROP CONSTRAINT IF EXISTS c;\nALTER TABLE a SET (fillfactor = 70);\n" +
					"ALTER TABLE a ALTER COLUMN id SET STATISTICS 100;\nALTER TABLE missing ADD COLUMN x int;",
			},
			want: []Table{{Name: "a", Columns: []string{idInt}, Line: 1}},
		},
	})
}

// TestSchemaComments: a comment right above a CREATE TABLE describes the
// table; a column's, the one trailing its definition (even past the
// statement's semicolon), else the one right above it.
func TestSchemaComments(t *testing.T) {
	s := newSchema()
	s.apply("", `-- not this one

-- configuration for activity assignments.
-- One row per assignment.
CREATE TABLE config (
	assignment_id bigserial NOT NULL,
	title text, -- the title, with a comma
	-- the instructions,
	-- shown to students
	instruction text,
	kind text, plain text -- only plain's
);`)
	s.apply("", `ALTER TABLE config
    ADD COLUMN entity_id text; -- ID of the entity
ALTER TABLE config
    -- a placement
    ADD COLUMN IF NOT EXISTS is_placement boolean default false
;`)
	got := s.list()[0]
	if want := "configuration for activity assignments. One row per assignment."; got.Description != want {
		t.Errorf("description = %q, want %q", got.Description, want)
	}
	want := map[string]string{
		"title":        "the title, with a comma",
		"instruction":  "the instructions, shown to students",
		"plain":        "only plain's",
		"entity_id":    "ID of the entity",
		"is_placement": "a placement",
	}
	if !reflect.DeepEqual(got.ColumnDescriptions, want) {
		t.Errorf("column descriptions =\n%v\nwant\n%v", got.ColumnDescriptions, want)
	}
	if got.Line != 5 {
		t.Errorf("line = %d, want 5", got.Line)
	}
}

// TestTableFactsSkipRollbacks: a .down.sql file never runs, and each table
// cites the file that creates it.
func TestTableFactsSkipRollbacks(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"migrations/001_a.up.sql":   "CREATE TABLE a (id int, teacher text);",
		"migrations/001_a.down.sql": "DROP TABLE a;",
		"migrations/002_a.up.sql":   "ALTER TABLE a DROP COLUMN teacher;",
		"migrations/002_a.down.sql": "ALTER TABLE a ADD COLUMN teacher text;",
	}
	for name, data := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	facts := tableFacts(root, "", func(string, ...any) {})
	if len(facts) != 1 {
		t.Fatalf("facts = %+v, want one table", facts)
	}
	if got := facts[0].Attrs["columns"]; got != idInt {
		t.Errorf("columns = %q, want id:int", got)
	}
	if got := facts[0].Source.Path; got != "migrations/001_a.up.sql" {
		t.Errorf("source = %q, want the creating file", got)
	}
}
