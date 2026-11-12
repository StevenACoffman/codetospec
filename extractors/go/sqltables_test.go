package main

import (
	"path/filepath"
	"slices"
	"testing"
)

// sqlNames reads the names after FROM, JOIN, INTO and UPDATE, unquoted,
// lowercased and schema-qualified as written, each once.
func TestSQLNames(t *testing.T) {
	for name, tc := range map[string]struct {
		text string
		want []string
	}{
		"select join": {
			"SELECT a.id FROM accounts a JOIN sessions s ON s.account_id = a.id",
			[]string{"accounts", "sessions"},
		},
		"insert update delete": {
			"INSERT INTO ledger (x) VALUES (1); UPDATE Ledger SET x = 2; DELETE FROM audit",
			[]string{"ledger", "audit"},
		},
		"quoted and qualified": {
			`SELECT * FROM "reports"."summary" JOIN reports . totals t ON true`,
			[]string{"reports.summary", "reports.totals"},
		},
		"cte name kept for the schema to drop": {
			"WITH recent AS (SELECT * FROM sessions) SELECT * FROM recent",
			[]string{"sessions", "recent"},
		},
		"no sql": {"hello from the other side of nothing", []string{"the"}},
	} {
		t.Run(name, func(t *testing.T) {
			if got := sqlNames(tc.text); !slices.Equal(got, tc.want) {
				t.Errorf("sqlNames = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestSharedTableFacts: the shared tables the module's calls query, on the
// concrete type, through the interface or a helper, are table facts cited
// under the shared root; a CTE name, a bare name matching one qualified
// table, and a table no called query names are handled.
func TestSharedTableFacts(t *testing.T) {
	ws := writeTree(t, map[string]string{
		"shared/migrations/001_init.sql": `CREATE TABLE accounts (id uuid PRIMARY KEY);
CREATE TABLE sessions (id uuid, account_id uuid NOT NULL REFERENCES accounts(id));
CREATE TABLE ledger (id uuid);
CREATE TABLE reports.summary (id uuid);
`,
		"shared/q/q.go": `package q

const getAccount = ` + "`SELECT id FROM accounts WHERE id = $1`" + `

const listSessions = ` + "`WITH recent AS (SELECT * FROM sessions) SELECT * FROM recent JOIN accounts a ON true`" + `

const sumLedger = ` + "`SELECT sum(id) FROM ledger`" + `

const readSummary = ` + "`SELECT * FROM summary`" + `

type Querier interface{ ListSessions() error }

type Queries struct{}

func (q *Queries) GetAccount() error   { _ = getAccount; return nil }
func (q *Queries) ListSessions() error { _ = listSessions; return nil }
func (q *Queries) SumLedger() error    { _ = sumLedger; return nil }
func (q *Queries) ReadSummary() error  { _ = readSummary; return nil }
`,
		appMainFile: `package main

import "example.com/shared/q"

func main() {
	qs := &q.Queries{}
	_ = qs.GetAccount()
	var i q.Querier = qs
	_ = i.ListSessions()
	helper()
}

func helper() { _ = (&q.Queries{}).ReadSummary() }
`,
	})
	shared, err := sharedRoots([]string{sharedName + "=" + filepath.Join(ws, "shared")})
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(ws, "app")
	logf := func(format string, a ...any) { t.Logf(format, a...) }
	m, ok := loadPackages(root, logf)
	if !ok {
		t.Fatal("workspace did not load")
	}
	schema := tableFacts(shared[0].Dir, "", logf)
	var got []string
	for _, f := range SharedTableFacts(m.pkgs, m.fset, root, &shared[0], schema) {
		got = append(got, f.ID+" "+f.Attrs[attrReferences]+" @"+f.Source.Root+"/"+f.Source.Path)
	}
	want := []string{
		"table.@pkg/accounts  @pkg/migrations/001_init.sql",
		"table.@pkg/reports.summary  @pkg/migrations/001_init.sql",
		"table.@pkg/sessions account_id:accounts:not_null @pkg/migrations/001_init.sql",
	}
	if !slices.Equal(got, want) {
		t.Errorf("got %q\nwant %q", got, want)
	}
}
