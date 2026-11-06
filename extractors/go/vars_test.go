package main

import "testing"

// TestCallFactsThroughVars: a package-level variable holding a function
// built by a call or written as a literal (webapp's cache.Cache wrappers and
// dataloaders) is a definition call facts can name: a use of it is a call
// of it, and its initializer's uses are its calls. A variable holding data
// stays out, as it was.
func TestCallFactsThroughVars(t *testing.T) {
	m, dir := loadModule(t, "example.com/cached", map[string]string{
		"cached.go": `package cached

func _uncachedScopes(kaid string) int { return len(kaid) }

func cache(f func(string) int) func(string) int { return f }

var Scopes = cache(_uncachedScopes)

func HasScope(kaid string) bool { return Scopes(kaid) > 0 }

var loadUDIs = func(kaid string) int { return query(kaid) }

func query(kaid string) int { return 0 }

func LoadActive(kaid string) int { return loadUDIs(kaid) }

var Table = []string{"a"}

func Size() int { return len(Table) }

var ErrEmpty = newError("empty")

func newError(msg string) error { return nil }

func Check(kaid string) error { return ErrEmpty }
`,
	})
	calls := map[string]bool{}
	for _, f := range CallFacts(m.pkgs, m.fset, dir) {
		calls[f.Source.Lines+">"+f.Attrs["callee_lines"]] = true
	}
	cases := []struct {
		name string
		pair string // caller lines > callee lines
		want bool
	}{
		{"a use of a cached function calls its variable", "9-9>7-7", true},
		{"the variable calls what its initializer names", "7-7>3-3", true},
		{"and the wrapper building it", "7-7>5-5", true},
		{"a use of a loader calls its variable", "15-15>11-11", true},
		{"a function literal calls what its body names", "11-11>13-13", true},
		{"a data variable is no callee", "19-19>17-17", false},
		{"a sentinel error built by a call is no callee", "25-25>21-21", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if calls[tc.pair] != tc.want {
				t.Errorf(
					"call %s = %v, want %v (calls: %v)",
					tc.pair,
					calls[tc.pair],
					tc.want,
					calls,
				)
			}
		})
	}
}
