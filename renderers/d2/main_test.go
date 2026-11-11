package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// The test sources are internal/d2's output for one diagram in each
// palette (a container, a focal node, a decision, a terminal, labelled
// edges): what codetospec sends.
func sources(t *testing.T) []request {
	t.Helper()
	var reqs []request
	for _, f := range []struct{ name, path string }{
		{"light", "testdata/light.d2"}, {"dark", "testdata/dark.d2"},
	} {
		src, err := os.ReadFile(f.path)
		if err != nil {
			t.Fatal(err)
		}
		reqs = append(reqs, request{Name: f.name, Source: string(src)})
	}
	return reqs
}

// drawBatch runs one batch through run, failing on a nonzero exit.
func drawBatch(t *testing.T, reqs []request) []result {
	t.Helper()
	in, err := json.Marshal(batch{Diagrams: reqs})
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run(context.Background(), bytes.NewReader(in), &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	var out batch
	if err = json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out.Results
}

// TestDrawsBothPalettes: each source becomes an SVG, in request order, in
// its palette's paper, and two runs give the same bytes.
func TestDrawsBothPalettes(t *testing.T) {
	reqs := sources(t)
	first := drawBatch(t, reqs)
	if len(first) != len(reqs) {
		t.Fatalf("%d results for %d diagrams", len(first), len(reqs))
	}
	for i, paper := range []string{"#f5f5f5", "#2d3142"} {
		r := first[i]
		if r.Name != reqs[i].Name || r.Error != "" || !strings.Contains(r.SVG, "<svg") {
			t.Fatalf("result %d = %s %q, want an SVG of %s", i, r.Name, r.Error, reqs[i].Name)
		}
		if !strings.Contains(strings.ToLower(r.SVG), paper) {
			t.Errorf("%s: SVG lacks its paper %s", r.Name, paper)
		}
	}
	second := drawBatch(t, reqs)
	for i := range first {
		if first[i].SVG != second[i].SVG {
			t.Errorf("%s: two runs differ", first[i].Name)
		}
	}
}

// TestRejectedSourceAlone: a source D2 rejects gets its error; the rest
// of the batch is drawn.
func TestRejectedSourceAlone(t *testing.T) {
	reqs := append([]request{{Name: "bad", Source: "a -> {"}}, sources(t)[0])
	got := drawBatch(t, reqs)
	if got[0].Error == "" || got[0].SVG != "" {
		t.Errorf("bad source = %+v, want an error alone", got[0])
	}
	if got[1].Error != "" || got[1].SVG == "" {
		t.Errorf("good source = %q, want its SVG", got[1].Error)
	}
}

// TestBadInput: input that isn't a batch fails the run.
func TestBadInput(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run(context.Background(), strings.NewReader("{"), &stdout, &stderr); code == 0 {
		t.Error("exit 0 on malformed input")
	}
}
