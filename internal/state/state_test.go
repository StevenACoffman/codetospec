package state_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/StevenACoffman/codetospec/internal/state"
)

// TestTokensLastRun: a new run (Open) resets the latest-run counters and
// keeps the cumulative ones.
func TestTokensLastRun(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	run := func(prompt, completion int) state.PhaseTokens {
		t.Helper()
		st, err := state.Open(path, "src")
		if err != nil {
			t.Fatal(err)
		}
		add := func(s *state.State) { s.AddTokens("map", prompt, completion) }
		if err = st.Update(add); err != nil {
			t.Fatal(err)
		}
		return *st.Snapshot().Tokens["map"]
	}
	cases := []struct {
		name               string
		prompt, completion int
		want               state.PhaseTokens
	}{
		{"first run", 100, 10, state.PhaseTokens{
			Prompt: 100, Completion: 10, LastPrompt: 100, LastCompletion: 10,
		}},
		{"warm-cache run", 0, 0, state.PhaseTokens{Prompt: 100, Completion: 10}},
		{"third run", 5, 1, state.PhaseTokens{
			Prompt: 105, Completion: 11, LastPrompt: 5, LastCompletion: 1,
		}},
	}
	for _, tc := range cases { // in order: each run builds on the last
		if got := run(tc.prompt, tc.completion); got != tc.want {
			t.Fatalf("%s: tokens = %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

// TestOpenRefusesAnotherSource: an output's state may be reused for its own
// source, given relative or absolute, but not for another one.
func TestOpenRefusesAnotherSource(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "repo")
	cases := []struct {
		name    string
		first   string // source of the existing state; "" for none
		second  string
		refused bool
	}{
		{"fresh output", "", src, false},
		{"same source", src, src, false},
		{"same source, unclean", src, src + "/.", false},
		{"another source", src, filepath.Join(dir, "other"), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "state.json")
			if tc.first != "" {
				st, err := state.Open(path, tc.first)
				if err != nil {
					t.Fatal(err)
				}
				if err = st.Update(func(*state.State) {}); err != nil {
					t.Fatal(err)
				}
			}
			_, err := state.Open(path, tc.second)
			if got := errors.Is(err, state.ErrOtherSource); got != tc.refused {
				t.Fatalf("refused = %v (err %v), want %v", got, err, tc.refused)
			}
		})
	}
}

// TestOpenAcceptsLegacyRelativeSource: a state an older version wrote with
// --src as given (relative) is reused, and then records the absolute path.
func TestOpenAcceptsLegacyRelativeSource(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(
		path,
		[]byte(`{"version": 1, "src_path": "testdata/fixture"}`),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(t.TempDir(), "repo")
	st, err := state.Open(path, src)
	if err != nil {
		t.Fatal(err)
	}
	if got := st.Snapshot().SrcPath; got != src {
		t.Fatalf("src_path = %q, want %q", got, src)
	}
}
