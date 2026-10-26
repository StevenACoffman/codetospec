package main

import (
	"slices"
	"testing"
)

// libFile is the one non-test file of the tested module.
const libFile = "lib.go"

// testedLib is a module whose tests reach its code directly, through a
// helper, and from an external test package.
func testedLib() map[string]string {
	return map[string]string{
		libFile: `package lib

func Open(id string) *Account { return &Account{ID: id} }

type Account struct{ ID string }

func (a *Account) Close() error { return check(a.ID) }

func check(id string) error { return nil }
`,
		"lib_test.go": `package lib

import "testing"

func TestOpen(t *testing.T) { mustOpen(t) }

func mustOpen(t *testing.T) *Account {
	t.Helper()
	return Open("x")
}

func Testify(t *testing.T) { _ = check("") }

func BenchmarkOpen(b *testing.B) { _ = Open("x") }

type accountSuite struct{}

func (s *accountSuite) TestCheck() { _ = check("") }

func (s *accountSuite) TestWith(t *testing.T) { _ = check("") }
`,
		"close_test.go": `package lib_test

import (
	"testing"

	"example.com/lib"
)

func TestClose(t *testing.T) { _ = (&lib.Account{}).Close() }
`,
	}
}

// TestTestFacts: a test's facts name what it uses itself and through its
// helpers, in or out of the package; benchmarks, and functions only named
// like tests, have none.
func TestTestFacts(t *testing.T) {
	m, dir := loadModule(t, "example.com/lib", testedLib())

	got := map[string][]string{} // test name → callee lines
	for _, f := range TestFacts(&m, dir) {
		if f.Kind != "test" || f.Attrs["callee_path"] != libFile {
			t.Errorf("fact %+v: want a test fact with a lib.go callee", f)
		}
		got[f.Attrs["name"]] = append(got[f.Attrs["name"]], f.Attrs["callee_lines"])
	}
	cases := map[string][]string{
		"TestOpen": {
			"3-3",
		}, // Open, through mustOpen (a signature is not a use)
		"TestClose":              {"5-5", "7-7"}, // Account, Close
		"Testify":                nil,
		"accountSuite.TestCheck": {"9-9"}, // a suite method
		"accountSuite.TestWith":  nil,     // a method with parameters is no test
		"BenchmarkOpen":          nil,
	}
	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			if !slices.Equal(got[name], want) {
				t.Errorf("callees = %v, want %v", got[name], want)
			}
		})
	}

	for _, f := range SymbolFacts(m.pkgs, m.fset, dir) {
		if f.Source.Path != libFile {
			t.Errorf("symbol fact from a test file: %+v", f)
		}
	}
}
