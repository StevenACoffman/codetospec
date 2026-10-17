package sitter_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/StevenACoffman/codetospec/internal/sitter"
)

// goFuncs builds a Go file of n functions, each bodyLines long plus its
// signature and closing brace, separated by one blank line.
func goFuncs(n, bodyLines int) []byte {
	var b strings.Builder
	b.WriteString("package p\n")
	for i := range n {
		fmt.Fprintf(&b, "\nfunc f%d() {\n", i)
		for range bodyLines {
			b.WriteString("\t_ = 1\n")
		}
		b.WriteString("}\n")
	}
	return []byte(b.String())
}

// chunkGo parses and chunks a Go file with the given domain function.
func chunkGo(
	t *testing.T,
	content []byte,
	domainOf func(namespace, path string, line int) string,
) []sitter.Chunk {
	t.Helper()
	info, err := sitter.Parse("go", content)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return sitter.ChunkFile("p/p.go", "go", content, info, domainOf)
}

func oneDomain(string, string, int) string { return "d" }

func TestChunkFilePacksSmallDefinitions(t *testing.T) {
	cases := map[string]struct {
		content   []byte
		domainOf  func(namespace, path string, line int) string
		wantSpans []string
	}{
		"small neighbours share one chunk": {
			content:   goFuncs(3, 2), // f0 3-6, f1 8-11, f2 13-16
			domainOf:  oneDomain,
			wantSpans: []string{"3-16"},
		},
		"pack never exceeds the target": {
			content:   goFuncs(4, 60), // 62-line functions: two fit in 150, three do not
			domainOf:  oneDomain,
			wantSpans: []string{"3-127", "129-253"},
		},
		"a definition over the target stays alone": {
			content:   goFuncs(2, 200),
			domainOf:  oneDomain,
			wantSpans: []string{"3-204", "206-407"},
		},
		"a domain change splits the pack": {
			content: goFuncs(3, 2),
			domainOf: func(_, _ string, line int) string {
				if line >= 13 {
					return "other"
				}
				return "d"
			},
			wantSpans: []string{"3-11", "13-16"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			chunks := chunkGo(t, tc.content, tc.domainOf)
			spans := make([]string, len(chunks))
			for i, c := range chunks {
				spans[i] = fmt.Sprintf("%d-%d", c.StartLine, c.EndLine)
			}
			if got, want := strings.Join(spans, ","), strings.Join(tc.wantSpans, ","); got != want {
				t.Fatalf("spans = %s, want %s", got, want)
			}
		})
	}
}

func TestChunkFilePackKeepsExactSource(t *testing.T) {
	content := goFuncs(3, 2)
	chunks := chunkGo(t, content, oneDomain)
	if len(chunks) != 1 {
		t.Fatalf("got %d chunks, want 1", len(chunks))
	}
	lines := strings.Split(string(content), "\n")
	want := strings.Join(lines[chunks[0].StartLine-1:chunks[0].EndLine], "\n")
	if chunks[0].Content != want {
		t.Fatalf(
			"packed content is not the exact source span:\n%s\nwant:\n%s",
			chunks[0].Content,
			want,
		)
	}
}

// TestChunkFileNeverPacksContextChunks: a container too large for one chunk
// is split per method, each with the class header as Context; those method
// chunks must stay separate however small they are.
func TestChunkFileNeverPacksContextChunks(t *testing.T) {
	var b strings.Builder
	b.WriteString("<?php\n\nclass Big\n{\n    public $prop = 1;\n")
	const methods = 40
	for m := range methods {
		fmt.Fprintf(&b, "\n    public function m%d()\n    {\n", m)
		for range 6 {
			b.WriteString("        $x = 1;\n")
		}
		b.WriteString("    }\n")
	}
	b.WriteString("}\n")
	content := []byte(b.String())
	info, err := sitter.Parse("php", content)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	chunks := sitter.ChunkFile("Big.php", "php", content, info, oneDomain)
	if len(chunks) != methods {
		t.Fatalf("got %d chunks, want %d (one per method, unpacked)", len(chunks), methods)
	}
	for _, c := range chunks {
		if c.Context == "" {
			t.Fatalf("chunk %d-%d lost its class header context", c.StartLine, c.EndLine)
		}
	}
}

func TestChunkFileIsDeterministic(t *testing.T) {
	content := goFuncs(7, 9)
	a, b := chunkGo(t, content, oneDomain), chunkGo(t, content, oneDomain)
	if len(a) != len(b) {
		t.Fatalf("chunk counts differ: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i].ID != b[i].ID {
			t.Fatalf("chunk %d id differs: %s vs %s", i, a[i].ID, b[i].ID)
		}
	}
}
