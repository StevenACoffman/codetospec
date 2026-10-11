package llm_test

import (
	"testing"

	"codetospec/internal/llm"
)

func TestPromptKey(t *testing.T) {
	base := llm.PromptKey("model-a", "system", "user")
	cases := map[string]struct {
		key  string
		same bool
	}{
		"identical inputs":      {llm.PromptKey("model-a", "system", "user"), true},
		"other model":           {llm.PromptKey("model-b", "system", "user"), false},
		"edited prompt":         {llm.PromptKey("model-a", "system!", "user"), false},
		"shifted part boundary": {llm.PromptKey("model-a", "systemu", "ser"), false},
		"extra empty part":      {llm.PromptKey("model-a", "system", "user", ""), false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := tc.key == base; got != tc.same {
				t.Fatalf("key %s vs base %s: same = %v, want %v", tc.key, base, got, tc.same)
			}
			if len(tc.key) != 16 {
				t.Fatalf("key %q has length %d, want 16", tc.key, len(tc.key))
			}
		})
	}
}
