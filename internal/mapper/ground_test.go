package mapper_test

import (
	"context"
	"strings"
	"testing"

	"codetospec/internal/llm"
	"codetospec/internal/mapper"
	"codetospec/internal/sitter"
)

// scripted is an llm.Chatter replying with replies in order, the last one
// again once they run out; calls counts the calls.
type scripted struct {
	replies []string
	calls   int
}

func (s *scripted) Chat(context.Context, []llm.Message) (string, llm.Usage, error) {
	reply := s.replies[min(s.calls, len(s.replies)-1)]
	s.calls++
	return reply, llm.Usage{PromptTokens: 1}, nil
}

// groundChunk holds lines 10-12 of app/X.php; line 11 sets the limit to 30.
func groundChunk() sitter.Chunk {
	return sitter.Chunk{
		ID:        "abcdef0123456789",
		Path:      "app/X.php",
		StartLine: 10,
		EndLine:   12,
		Language:  "php",
		Domain:    "x",
		Content:   "function charge() {\n  $limit = 30;\n}",
	}
}

// ruleReply is a valid map reply with one rule per requirement, each citing
// lines 10-12.
func ruleReply(requirements ...string) string {
	rules := make([]string, len(requirements))
	for i, r := range requirements {
		rules[i] = `{"title": "T", "ears_kind": "event", "requirement": "` + r + `",
"citations": [{"path": "app/X.php", "lines": "10-12"}], "entities": [], "endpoints": [],
"nature": "business", "origin": "explicit", "confidence": 0.9}`
	}
	return `{"chunk_summary": "s", "rules": [` + strings.Join(rules, ",") + `]}`
}

// numbersMissing is a stand-in for grounding.Check: the listed numbers
// stated in the requirement but absent from the cited lines.
func numbersMissing(requirement string, cited []string) []string {
	code := strings.Join(cited, "\n")
	var missing []string
	for _, n := range []string{"30", "50"} {
		if strings.Contains(requirement, n) && !strings.Contains(code, n) {
			missing = append(missing, n)
		}
	}
	return missing
}

func TestRegroundIsNeverWorse(t *testing.T) {
	const (
		ungrounded = "WHEN charging, the system shall cap at 50."
		grounded   = "WHEN charging, the system shall cap at 30."
		other      = "WHEN charging, the system shall log."
	)
	cases := map[string]struct {
		first     string   // first reply
		fixes     []string // replies to the grounding correction, in order
		wantCalls int
		want      []string // requirements kept
	}{
		"grounded reply: no extra call": {
			first: ruleReply(grounded), wantCalls: 1, want: []string{grounded},
		},
		"fix adopted": {
			first: ruleReply(ungrounded, other), fixes: []string{ruleReply(grounded, other)},
			wantCalls: 2, want: []string{grounded, other},
		},
		"fix that drops a rule is refused": {
			first: ruleReply(ungrounded, other), fixes: []string{ruleReply(grounded)},
			wantCalls: 2, want: []string{ungrounded, other},
		},
		"fix that stays ungrounded is refused": {
			first: ruleReply(ungrounded), fixes: []string{ruleReply(ungrounded)},
			wantCalls: 2, want: []string{ungrounded},
		},
		"invalid fixes keep the valid reply": {
			first: ruleReply(ungrounded), fixes: []string{"not json", "still not json"},
			wantCalls: 3, want: []string{ungrounded},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			chat := &scripted{replies: append([]string{tc.first}, tc.fixes...)}
			m := &mapper.Mapper{
				LLM:          chat,
				Lang:         "en",
				Workers:      1,
				OutDir:       t.TempDir(),
				EndpointsFor: func(string) []string { return nil },
				Ungrounded:   numbersMissing,
			}
			outputs, err := m.Run(context.Background(), []sitter.Chunk{groundChunk()})
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			out := outputs[0]
			var got []string
			for _, r := range out.Rules {
				got = append(got, r.Requirement)
			}
			if out.Failed || chat.calls != tc.wantCalls ||
				strings.Join(got, "|") != strings.Join(tc.want, "|") {
				t.Fatalf("failed=%v calls=%d rules=%q, want calls=%d rules=%q",
					out.Failed, chat.calls, got, tc.wantCalls, tc.want)
			}
		})
	}
}
