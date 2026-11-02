package mapper_test

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/StevenACoffman/codetospec/internal/llm"
	"github.com/StevenACoffman/codetospec/internal/mapper"
	"github.com/StevenACoffman/codetospec/internal/sitter"
	"github.com/StevenACoffman/codetospec/internal/vocabulary"
)

// capped is a grounded requirement of groundChunk.
const capped = "WHEN charging, the system shall cap at 30."

// conditionReply is ruleReply(capped) with the rule's condition set to
// cond (JSON).
func conditionReply(cond string) string {
	return strings.Replace(
		ruleReply(capped),
		`{"title": "T", `,
		`{"title": "T", "condition": `+cond+`, `,
		1,
	)
}

// offered is the vocabulary a test chunk gets.
func offered(string, string) []vocabulary.Term {
	return []vocabulary.Term{{Name: "caller.loggedIn"}}
}

// conditionMapper is a mapper over groundChunk with grounding and a
// vocabulary.
func conditionMapper(t *testing.T, chat llm.Chatter) *mapper.Mapper {
	t.Helper()
	return &mapper.Mapper{
		Log:           slog.New(slog.NewTextHandler(t.Output(), nil)),
		LLM:           chat,
		Lang:          "en",
		Workers:       1,
		OutDir:        t.TempDir(),
		EndpointsFor:  func(string) []string { return nil },
		VocabularyFor: offered,
		Ungrounded:    numbersMissing,
	}
}

// TestMapConditions: a valid condition is kept in canonical form, its
// unknown variables coined under local.; an invalid or ungrounded one gets
// the repair exchange, and is dropped if still invalid, never its rule.
func TestMapConditions(t *testing.T) {
	const (
		known   = `{"not": {"var": "caller.loggedIn", "op": "equal", "value": true}}`
		coined  = `{"var": "limit", "op": "greater_than", "value": 30}`
		invalid = `{"var": "limit", "op": "exceeds", "value": 30}`
		off     = `{"var": "limit", "op": "greater_than", "value": 50}`
		// settled is coined, canonical and moved under local.
		settled = `{"var":"local.limit","op":"greater_than","value":30}`
	)
	cases := map[string]struct {
		replies   []string
		wantCalls int
		want      string // the kept rule's condition
	}{
		"known variable": {
			[]string{conditionReply(known)},
			1,
			`{"not":{"var":"caller.loggedIn","op":"equal","value":true}}`,
		},
		"coined variable": {
			[]string{conditionReply(coined)},
			1,
			settled,
		},
		"unconditional": {[]string{ruleReply(capped)}, 1, ""},
		"invalid, repaired": {
			[]string{conditionReply(invalid), conditionReply(coined)},
			2,
			settled,
		},
		"invalid, dropped": {
			[]string{conditionReply(invalid), conditionReply(invalid)}, 2, "",
		},
		"ungrounded, repaired": {
			[]string{conditionReply(off), conditionReply(coined)},
			2,
			settled,
		},
		"ungrounded, kept when unrepaired": {
			[]string{conditionReply(off), conditionReply(off)},
			2,
			`{"var":"local.limit","op":"greater_than","value":50}`,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			chat := &scripted{replies: tc.replies}
			outputs, err := conditionMapper(
				t,
				chat,
			).Run(context.Background(), []sitter.Chunk{groundChunk()})
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			out := outputs[0]
			if out.Failed || len(out.Rules) != 1 {
				t.Fatalf("failed=%v rules=%d, want one rule", out.Failed, len(out.Rules))
			}
			if got := string(out.Rules[0].Condition); got != tc.want || chat.calls != tc.wantCalls {
				t.Errorf(
					"condition %s after %d calls, want %s after %d",
					got,
					chat.calls,
					tc.want,
					tc.wantCalls,
				)
			}
		})
	}
}

// TestMapPromptOffersVocabulary: the prompt lists the chunk's vocabulary,
// which stays out of the cache key: another vocabulary reuses the cached
// output.
func TestMapPromptOffersVocabulary(t *testing.T) {
	var prompts []string
	chat := chatFunc(func(_ context.Context, msgs []llm.Message) (string, llm.Usage, error) {
		prompts = append(prompts, msgs[len(msgs)-1].Content)
		return ruleReply(capped), llm.Usage{}, nil
	})
	m := conditionMapper(t, chat)
	if _, err := m.Run(context.Background(), []sitter.Chunk{groundChunk()}); err != nil {
		t.Fatal(err)
	}
	if len(prompts) != 1 || !strings.Contains(prompts[0], `VOCABULARY: ["caller.loggedIn"]`) {
		t.Fatalf("prompt does not offer the vocabulary:\n%s", strings.Join(prompts, "\n---\n"))
	}
	m.VocabularyFor = func(string, string) []vocabulary.Term { return nil }
	if _, err := m.Run(context.Background(), []sitter.Chunk{groundChunk()}); err != nil {
		t.Fatal(err)
	}
	if len(prompts) != 1 {
		t.Errorf("another vocabulary re-mapped the chunk: %d calls", len(prompts))
	}
}
