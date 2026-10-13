package state_test

import (
	"path/filepath"
	"testing"

	"codetospec/internal/state"
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
