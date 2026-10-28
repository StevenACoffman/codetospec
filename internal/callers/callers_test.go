package callers_test

import (
	"reflect"
	"slices"
	"testing"

	"github.com/StevenACoffman/codetospec/internal/callers"
	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/sitter"
)

const (
	file = "a.go" // the file the test chunks and calls lie in
	user = "user" // the domain callers agree on
)

// chunk is a chunk of a.go at start-end in domain.
func chunk(start, end int, domain string) sitter.Chunk {
	return chunkOf(file, start, end, domain)
}

// chunkOf is a chunk of path at start-end in domain.
func chunkOf(path string, start, end int, domain string) sitter.Chunk {
	return sitter.Chunk{Path: path, StartLine: start, EndLine: end, Domain: domain}
}

// call is a call fact from the definition at caller to the one at callee
// (both "start-end" in a.go).
func call(caller, callee string) extract.Fact {
	return callAcross(file, caller, file, callee)
}

// callAcross is a call fact from callerPath:caller to calleePath:callee.
func callAcross(callerPath, caller, calleePath, callee string) extract.Fact {
	return extract.Fact{
		Kind:   extract.KindCall,
		ID:     "call." + callerPath + ":" + caller + "#" + calleePath + ":" + callee,
		Source: extract.Ref{Path: callerPath, Lines: caller},
		Attrs: map[string]string{
			extract.AttrCalleePath:  calleePath,
			extract.AttrCalleeLines: callee,
		},
	}
}

// viaInterface marks facts as uses through an interface.
func viaInterface(facts ...extract.Fact) []extract.Fact {
	for i := range facts {
		facts[i].Attrs[extract.AttrDispatch] = extract.DispatchInterface
	}
	return facts
}

func domains(chunks []sitter.Chunk) []string {
	out := make([]string, len(chunks))
	for i := range chunks {
		out[i] = chunks[i].Domain
	}
	return out
}

func TestReassign(t *testing.T) {
	cases := map[string]struct {
		chunks []sitter.Chunk
		calls  []extract.Fact
		hinted int // line an extractor domain fact covers; 0 for none
		want   []string
	}{
		"callers in one domain": {
			chunks: []sitter.Chunk{
				chunk(1, 9, user),
				chunk(10, 19, user),
				chunk(20, 29, "helpers"),
			},
			calls: []extract.Fact{call("1-9", "20-29"), call("10-19", "20-29")},
			want:  []string{user, user, user},
		},
		"callers in two domains": {
			chunks: []sitter.Chunk{
				chunk(1, 9, user),
				chunk(10, 19, "billing"),
				chunk(20, 29, "helpers"),
			},
			calls: []extract.Fact{call("1-9", "20-29"), call("10-19", "20-29")},
			want:  []string{user, "billing", "helpers"},
		},
		"no caller": {
			chunks: []sitter.Chunk{chunk(1, 9, user), chunk(20, 29, "helpers")},
			want:   []string{user, "helpers"},
		},
		"a call through an interface is no caller": {
			chunks: []sitter.Chunk{chunk(1, 9, user), chunk(20, 29, "lib")},
			calls:  viaInterface(call("1-9", "20-29")),
			want:   []string{user, "lib"},
		},
		"helper of a helper follows the top caller": {
			chunks: []sitter.Chunk{
				chunk(1, 9, user),
				chunk(10, 19, "util"),
				chunk(20, 29, "misc"),
			},
			calls: []extract.Fact{call("1-9", "10-19"), call("10-19", "20-29")},
			want:  []string{user, user, user},
		},
		"hinted chunk keeps its domain": {
			chunks: []sitter.Chunk{chunk(1, 9, user), chunk(20, 29, "billing")},
			calls:  []extract.Fact{call("1-9", "20-29")},
			hinted: 20,
			want:   []string{user, "billing"},
		},
		"call cycle keeps its domains": {
			chunks: []sitter.Chunk{chunk(1, 9, "a"), chunk(10, 19, "b")},
			calls:  []extract.Fact{call("1-9", "10-19"), call("10-19", "1-9")},
			want:   []string{"a", "b"},
		},
		"callers in another directory leave the package's domain": {
			chunks: []sitter.Chunk{
				chunkOf("main.go", 1, 9, "main"),
				chunkOf("logic/l.go", 1, 9, "logic"),
			},
			calls: []extract.Fact{callAcross("main.go", "1-9", "logic/l.go", "1-9")},
			want:  []string{"main", "logic"},
		},
		"only callers of the same directory count": {
			chunks: []sitter.Chunk{
				chunkOf("models/m.go", 1, 9, "models"),
				chunkOf("models/m.go", 10, 19, "util"),
				chunkOf("servicetest/s.go", 1, 9, "servicetest"),
			},
			calls: []extract.Fact{
				callAcross("models/m.go", "1-9", "models/m.go", "10-19"),
				callAcross("servicetest/s.go", "1-9", "models/m.go", "10-19"),
			},
			want: []string{"models", "models", "servicetest"},
		},
		"another file of the directory counts": {
			chunks: []sitter.Chunk{
				chunkOf("r/users.go", 1, 9, "user"),
				chunkOf("r/guards.go", 1, 9, "guards"),
			},
			calls: []extract.Fact{callAcross("r/users.go", "1-9", "r/guards.go", "1-9")},
			want:  []string{user, user},
		},
		"self call is no caller": {
			chunks: []sitter.Chunk{chunk(1, 9, "a")},
			calls:  []extract.Fact{call("1-9", "1-9")},
			want:   []string{"a"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			hinted := func(_ string, line int) bool { return line == tc.hinted }
			before := domains(tc.chunks)
			got := callers.Reassign(tc.chunks, tc.calls, hinted)
			if d := domains(got); !reflect.DeepEqual(d, tc.want) {
				t.Fatalf("domains = %v, want %v", d, tc.want)
			}
			if !reflect.DeepEqual(domains(tc.chunks), before) {
				t.Fatal("the input chunks were modified")
			}
		})
	}
}

// TestReassignIgnoresFactOrder: reversed facts give the same domains.
func TestReassignIgnoresFactOrder(t *testing.T) {
	chunks := []sitter.Chunk{chunk(1, 9, user), chunk(10, 19, "util"), chunk(20, 29, "misc")}
	calls := []extract.Fact{call("1-9", "10-19"), call("10-19", "20-29"), call("1-9", "20-29")}
	none := func(string, int) bool { return false }
	want := domains(callers.Reassign(chunks, calls, none))
	slices.Reverse(calls)
	if got := domains(callers.Reassign(chunks, calls, none)); !reflect.DeepEqual(got, want) {
		t.Fatalf("reversed facts: %v, want %v", got, want)
	}
}
