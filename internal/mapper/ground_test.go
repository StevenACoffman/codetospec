package mapper_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/StevenACoffman/codetospec/internal/ears"
	"github.com/StevenACoffman/codetospec/internal/llm"
	"github.com/StevenACoffman/codetospec/internal/mapper"
	"github.com/StevenACoffman/codetospec/internal/sitter"
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
		Path:      chunkPath,
		StartLine: 10,
		EndLine:   12,
		Language:  langPHP,
		Domain:    "x",
		Content:   "function charge() {\n  $limit = 30;\n}",
	}
}

// ruleReply is a valid map reply with one rule per requirement, written
// as its EARS components, each citing lines 10-12.
func ruleReply(requirements ...string) string {
	rules := make([]string, len(requirements))
	for i, r := range requirements {
		components, err := ears.Parse(r)
		if err != nil {
			panic(err)
		}
		for i, response := range components.Shall {
			components.Shall[i].Action = strings.Fields(response.Text)[0]
		}
		data, err := json.Marshal(components)
		if err != nil {
			panic(err)
		}
		rules[i] = `{"title": "T", "ears": ` + string(data) + `,
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
				Log:          slog.New(slog.NewTextHandler(t.Output(), nil)),
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

// TestRegroundCachedOutputs: under Reground, a cached output with an
// ungrounded rule gets one correction exchange, saved so a rerun pays
// nothing; without Reground the cache is used as is.
func TestRegroundCachedOutputs(t *testing.T) {
	const (
		ungrounded = "WHEN charging, the system shall cap at 50."
		grounded   = "WHEN charging, the system shall cap at 30."
	)
	cases := map[string]struct {
		reground        bool
		wantCalls       []int // per run, after the run that fills the cache
		wantRequirement string
	}{
		"reground once": {true, []int{1, 0}, grounded},
		"cache as is":   {false, []int{0, 0}, ungrounded},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			run := func(reground bool, replies ...string) (int, mapper.Output) {
				t.Helper()
				chat := &scripted{replies: replies}
				m := &mapper.Mapper{
					Log: slog.New(slog.NewTextHandler(t.Output(), nil)),
					LLM: chat, Lang: "en", Workers: 1, OutDir: dir,
					EndpointsFor: func(string) []string { return nil },
					Ungrounded:   numbersMissing,
					Reground:     reground,
				}
				outputs, err := m.Run(context.Background(), []sitter.Chunk{groundChunk()})
				if err != nil {
					t.Fatalf("Run: %v", err)
				}
				return chat.calls, outputs[0]
			}
			// Fill the cache without a checker, as an older run did.
			fill := &mapper.Mapper{
				Log: slog.New(slog.NewTextHandler(t.Output(), nil)),
				LLM: &scripted{replies: []string{ruleReply(ungrounded)}}, Lang: "en", Workers: 1,
				OutDir: dir, EndpointsFor: func(string) []string { return nil },
			}
			if _, err := fill.Run(context.Background(), []sitter.Chunk{groundChunk()}); err != nil {
				t.Fatal(err)
			}
			var out mapper.Output
			for i, want := range tc.wantCalls {
				var calls int
				calls, out = run(tc.reground, ruleReply(grounded))
				if calls != want {
					t.Fatalf("run %d made %d calls, want %d", i+1, calls, want)
				}
			}
			if got := out.Rules[0].Requirement; got != tc.wantRequirement {
				t.Fatalf("requirement = %q, want %q", got, tc.wantRequirement)
			}
		})
	}
}
