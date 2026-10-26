package render_test

import (
	"bytes"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/StevenACoffman/codetospec/internal/graph"
	"github.com/StevenACoffman/codetospec/internal/render"
)

// The files the reviewed tests edit, relative to the output directory.
const (
	ruleFile   = "nodes/rules/billing.prorata-activation.md"
	domainFile = "nodes/domains/billing.md"
	ruleID     = "rule.billing.prorata-activation"
)

// TestLoadPrior: a rule file edited since the run wrote it, or marked
// reviewed, is owned by a person; another edited node file is listed as
// overwritten; an untouched output owns nothing, and an output whose
// graph.json records no hashes only honors files marked reviewed.
func TestLoadPrior(t *testing.T) {
	edit := func(data []byte) []byte { return append(data, []byte("\nA reviewer's note.\n")...) }
	markReviewed := func(data []byte) []byte {
		return bytes.Replace(data, []byte("status: generated"), []byte("status: reviewed"), 1)
	}
	cases := map[string]struct {
		file            string
		change          func([]byte) []byte
		noHashes        bool // graph.json from before hashes
		wantReviewed    bool
		wantOverwritten bool
	}{
		"untouched":              {file: ruleFile, change: func(d []byte) []byte { return d }},
		"rule edited":            {file: ruleFile, change: edit, wantReviewed: true},
		"rule marked reviewed":   {file: ruleFile, change: markReviewed, wantReviewed: true},
		"domain edited":          {file: domainFile, change: edit, wantOverwritten: true},
		"rule edited, no hashes": {file: ruleFile, change: edit, noHashes: true},
		"marked reviewed, no hash": {
			file:         ruleFile,
			change:       markReviewed,
			noHashes:     true,
			wantReviewed: true,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			outDir := t.TempDir()
			if err := render.Write(outDir, testNodes(), new(testMeta())); err != nil {
				t.Fatalf("Write: %v", err)
			}
			rewrite(t, outDir, tc.file, tc.change)
			if tc.noHashes {
				rewrite(t, outDir, "graph.json", withoutHashes)
			}

			prior, err := render.LoadPrior(outDir)
			if err != nil {
				t.Fatalf("LoadPrior: %v", err)
			}

			checkOwned(t, &prior, tc.wantReviewed)
			if got := slices.Contains(prior.Overwritten, tc.file); got != tc.wantOverwritten {
				t.Errorf("overwritten = %v, want %s listed: %v",
					prior.Overwritten, tc.file, tc.wantOverwritten)
			}
			if len(prior.Nodes) != len(testNodes()) {
				t.Errorf("prior has %d nodes, want %d", len(prior.Nodes), len(testNodes()))
			}
		})
	}
}

// checkOwned: the test rule is owned when want is, and then marked
// reviewed in its node and its file.
func checkOwned(t *testing.T, prior *render.Prior, want bool) {
	t.Helper()
	r, owned := prior.Reviewed[ruleID]
	if owned != want {
		t.Fatalf("owned = %v, want %v", owned, want)
	}
	if owned && (r.Node.Status != graph.StatusReviewed ||
		!bytes.Contains(r.File, []byte("\nstatus: reviewed\n"))) {
		t.Errorf("owned rule not marked reviewed: status %q\n%s", r.Node.Status, r.File)
	}
}

// rewrite replaces the file rel of outDir with change applied to it.
func rewrite(t *testing.T, outDir, rel string, change func([]byte) []byte) {
	t.Helper()
	root, err := os.OpenRoot(outDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	data, err := root.ReadFile(rel)
	if err != nil {
		t.Fatal(err)
	}
	if err = root.WriteFile(rel, change(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

// withoutHashes is a graph.json as a run before hashes wrote it.
func withoutHashes(data []byte) []byte {
	var kept []string
	for line := range strings.SplitSeq(string(data), "\n") {
		if !strings.Contains(line, `"hash":`) {
			kept = append(kept, line)
		}
	}
	return []byte(strings.Join(kept, "\n"))
}

// readOutput returns the file rel of outDir.
func readOutput(t *testing.T, outDir, rel string) []byte {
	t.Helper()
	root, err := os.OpenRoot(outDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	data, err := root.ReadFile(rel)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// TestKeepReviewedSurvivesRuns: an edited rule is written back as edited
// by the next run, and stays owned by the run after it; a reviewed rule the
// run no longer produces is dropped.
func TestKeepReviewedSurvivesRuns(t *testing.T) {
	outDir := t.TempDir()
	if err := render.Write(outDir, testNodes(), new(testMeta())); err != nil {
		t.Fatalf("Write: %v", err)
	}
	rewrite(t, outDir, ruleFile, func(data []byte) []byte {
		return bytes.Replace(data, []byte("Cas nominal couvert."), []byte("Cas nominal revu."), 1)
	})

	for run := 1; run <= 2; run++ {
		prior, loadErr := render.LoadPrior(outDir)
		if loadErr != nil {
			t.Fatalf("run %d: LoadPrior: %v", run, loadErr)
		}
		nodes, files, dropped := render.KeepReviewed(testNodes(), prior.Reviewed)
		if len(dropped) != 0 {
			t.Fatalf("run %d: dropped %v", run, dropped)
		}
		meta := testMeta()
		meta.Reviewed = files
		if err := render.Write(outDir, nodes, &meta); err != nil {
			t.Fatalf("run %d: Write: %v", run, err)
		}
		got := readOutput(t, outDir, ruleFile)
		if !bytes.Contains(got, []byte("Cas nominal revu.")) ||
			!bytes.Contains(got, []byte("\nstatus: reviewed\n")) {
			t.Fatalf("run %d: the edit was not kept:\n%s", run, got)
		}
	}

	prior, err := render.LoadPrior(outDir)
	if err != nil {
		t.Fatal(err)
	}
	var others []graph.Node
	for _, n := range testNodes() {
		if n.ID != ruleID {
			others = append(others, n)
		}
	}
	if _, _, dropped := render.KeepReviewed(
		others,
		prior.Reviewed,
	); !slices.Equal(
		dropped,
		[]string{ruleID},
	) {
		t.Errorf("dropped = %v, want [%s]", dropped, ruleID)
	}
}
