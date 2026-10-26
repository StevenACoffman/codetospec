package main

import (
	"os"
	"path/filepath"
	"testing"
)

// writeWorkspace writes a go.work of two modules: shared (example.com/shared,
// package lib) and app, which requires it the way districts-jobs modules
// require pkg/ (replace => ../shared). It returns the workspace root.
func writeWorkspace(t *testing.T) string {
	t.Helper()
	ws := t.TempDir()
	files := map[string]string{
		"go.work":          "go 1.26\n\nuse (\n\t./app\n\t./shared\n)\n",
		"shared/go.mod":    "module example.com/shared\n\ngo 1.26\n",
		"app/go.mod":       "module example.com/app\n\ngo 1.26\n\nrequire example.com/shared v0.0.0\n\nreplace example.com/shared => ../shared\n",
		"shared/lib.go":    sharedLib,
		"shared/unused.go": "package lib\n\n// Unused is reached by nobody.\nfunc Unused() {}\n",
		"app/main.go":      appMain,
	}
	for name, content := range files {
		path := filepath.Join(ws, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return ws
}

const sharedLib = `package lib

import "errors"

// Limit caps an account's sessions.
const Limit = 5

// Account is a user account.
type Account struct{ ID string }

// Close closes the account.
func (a *Account) Close() error { return check(a.ID) }

func check(id string) error {
	if id == "" {
		return errEmpty
	}
	return nil
}

var errEmpty = errors.New("empty id")

// Open opens an account.
func Open(id string) *Account { return &Account{ID: id} }
`

const appMain = `package main

import lib "example.com/shared"

func main() {
	a := lib.Open("x")
	_ = a.Close()
	_ = lib.Limit
}
`

func TestReachFacts(t *testing.T) {
	ws := writeWorkspace(t)
	shared, err := sharedRoots([]string{"pkg=" + filepath.Join(ws, "shared")})
	if err != nil {
		t.Fatal(err)
	}
	m, ok := loadPackages(filepath.Join(ws, "app"), func(format string, a ...any) {
		t.Logf(format, a...)
	})
	if !ok {
		t.Fatal("workspace did not load")
	}
	got := map[string]Fact{}
	for _, f := range ReachFacts(m.pkgs, m.fset, &shared[0]) {
		got[f.Attrs["symbol"]] = f
	}

	cases := map[string]struct {
		symbol  string
		reached bool
	}{
		"called function":           {"lib.Open", true},
		"type built by a callee":    {"lib.Account", true},
		"method of a reached type":  {"lib.Account.Close", true},
		"unexported callee":         {"lib.check", true},
		"variable a callee returns": {"lib.errEmpty", true},
		"constant the module reads": {"lib.Limit", true},
		"declaration nobody uses":   {"lib.Unused", false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f, found := got[tc.symbol]
			if found != tc.reached {
				t.Fatalf("reached = %v, want %v (facts: %v)", found, tc.reached, got)
			}
			if found && (f.Source.Root != "pkg" || f.Source.Path != "lib.go" || f.Kind != "reach") {
				t.Errorf("fact = %+v, want a reach fact citing @pkg/lib.go", f)
			}
		})
	}
}

// TestSharedRoots: a -shared value names a root and a directory holding a
// go.mod.
func TestSharedRoots(t *testing.T) {
	ws := writeWorkspace(t)
	cases := map[string]struct {
		spec       string
		wantModule string // "" when an error is expected
	}{
		"module read from go.mod": {"pkg=" + filepath.Join(ws, "shared"), "example.com/shared"},
		"no name":                 {"=" + ws, ""},
		"no go.mod":               {"pkg=" + ws, ""},
		"not name=dir":            {"pkg", ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			roots, err := sharedRoots([]string{tc.spec})
			if tc.wantModule == "" {
				if err == nil {
					t.Fatalf("roots = %+v, want an error", roots)
				}
				return
			}
			if err != nil || roots[0].Module != tc.wantModule {
				t.Fatalf("roots = %+v, err %v; want module %s", roots, err, tc.wantModule)
			}
		})
	}
}
