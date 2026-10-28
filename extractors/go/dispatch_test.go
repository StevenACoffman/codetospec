package main

import (
	"maps"
	"slices"
	"testing"
)

// storeFile is the one non-test file of the store module.
const storeFile = "store.go"

// storeModule is a module whose code and tests call an implementation
// through a module interface, directly, and through an interface the
// standard library declares.
func storeModule() map[string]string {
	return map[string]string{
		storeFile: `package store

type Store interface{ Save() int }

type db struct{}

func (db) Save() int { return 1 }

func New() Store { return db{} }

func Use(s Store) int { return s.Save() }

func Both(s Store) int { return s.Save() + db{}.Save() }

type failure struct{}

func (failure) Error() string { return "failed" }

func Describe(err error) string { return err.Error() }
`,
		"store_test.go": `package store

import "testing"

func TestSave(t *testing.T) {
	if New().Save() != 1 {
		t.Fatal()
	}
}
`,
	}
}

// TestDispatch: a call of a module interface's method reaches each
// implementation, marked as a dispatch, unless the same caller also uses
// it directly; a call of the standard library's error interface reaches
// nothing.
func TestDispatch(t *testing.T) {
	m, dir := loadModule(t, "example.com/store", storeModule())

	calls := map[string]string{} // caller lines → callee lines + dispatch
	for _, f := range CallFacts(m.pkgs, m.fset, dir) {
		calls[f.Source.Lines] += f.Attrs["callee_lines"] + ":" + f.Attrs[attrDispatch] + " "
	}
	wantCalls := map[string]string{
		"9-9":   "5-5: ",          // New builds a db
		"11-11": "7-7:interface ", // Use calls db.Save through Store
		"13-13": "5-5: 7-7: ",     // Both also calls db.Save directly
	}
	if !maps.Equal(calls, wantCalls) {
		t.Errorf("calls = %q\nwant    %q", calls, wantCalls)
	}

	var tests []string
	for _, f := range TestFacts(&m, dir) {
		tests = append(tests, f.Attrs["callee_lines"]+":"+f.Attrs[attrDispatch])
	}
	if want := []string{"7-7:interface", "9-9:"}; !slices.Equal(tests, want) {
		t.Errorf("TestSave uses %v, want %v", tests, want)
	}
}
