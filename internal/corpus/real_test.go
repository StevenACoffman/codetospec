//go:build corpus

package corpus_test

import (
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/StevenACoffman/codetospec/internal/corpus"
	"github.com/StevenACoffman/codetospec/internal/drift"
	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/grounding"
	"github.com/StevenACoffman/codetospec/internal/locale"
	"github.com/StevenACoffman/codetospec/internal/render"
	"github.com/StevenACoffman/codetospec/internal/verify"
)

// manifestPath is the corpus manifest, relative to this package.
const manifestPath = "testdata/corpus.json"

// TestCorpus re-runs grounding, verify and drift over every graph of the
// manifest and fails on any count worse than its baseline. Run it with
//
//	go test -tags corpus ./internal/corpus -v
//
// and record new baselines (after reviewing a deliberate change) with
// CODETOSPEC_CORPUS_UPDATE=1. An entry whose graph or source is missing,
// or whose source moved off the recorded commit, is skipped.
func TestCorpus(t *testing.T) {
	manifest, err := corpus.LoadManifest(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	update := os.Getenv("CODETOSPEC_CORPUS_UPDATE") == "1"
	for i := range manifest.Entries {
		entry := &manifest.Entries[i]
		t.Run(entry.Name, func(t *testing.T) {
			src, out := home(t, entry.Src), home(t, entry.Out)
			if !exists(src) || !exists(out) {
				t.Skipf("graph %s or source %s not found", out, src)
			}
			head := gitHead(t, src)
			if !update && head != entry.Commit {
				t.Skipf("source at %s, baseline recorded at %s: re-record it", head, entry.Commit)
			}

			got := measure(t, entry, src, out)

			t.Logf("%+v", got)
			if update {
				entry.Commit, entry.Baseline = head, got
				return
			}
			for _, worse := range corpus.Compare(entry.Baseline, got) {
				t.Error(worse)
			}
		})
	}
	if update {
		writeManifest(t, &manifest)
	}
}

// measure re-checks one graph with the current code.
func measure(t *testing.T, entry *corpus.Entry, src, out string) corpus.Measures {
	t.Helper()
	nodes, err := render.LoadDir(out)
	if err != nil {
		t.Fatal(err)
	}
	facts, err := extract.LoadFactsFile(filepath.Join(out, ".codetospec", "facts.json"))
	if err != nil {
		t.Fatal(err)
	}
	extra := map[string]string{}
	for name, dir := range entry.RefRoots {
		extra[name] = home(t, dir)
	}
	roots := extract.Roots{Src: os.DirFS(src), Extra: dirFS(extra)}
	opts := grounding.Options{
		DecimalSeparator: locale.For(entry.Lang).DecimalSeparator,
		Constants:        grounding.Constants(facts),
	}
	nodes = grounding.Annotate(roots, nodes, opts)
	return corpus.Measure(nodes, len(verify.Run(nodes, roots)), drift.Check(nodes, roots))
}

func dirFS(dirs map[string]string) map[string]fs.FS {
	out := make(map[string]fs.FS, len(dirs))
	for name, dir := range dirs {
		out[name] = os.DirFS(dir)
	}
	return out
}

// home expands a leading "~/" to the user's home directory.
func home(t *testing.T, path string) string {
	t.Helper()
	rest, found := strings.CutPrefix(path, "~/")
	if !found {
		return path
	}
	dir, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, rest)
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// gitHead returns the commit the tree at dir is checked out at.
func gitHead(t *testing.T, dir string) string {
	t.Helper()
	// dir comes from the committed manifest, not outside input.
	cmd := exec.CommandContext( //nolint:gosec // G204: see above.
		t.Context(), "git", "-C", dir, "rev-parse", "HEAD")
	head, err := cmd.Output()
	if err != nil {
		t.Fatalf("git rev-parse in %s: %v", dir, err)
	}
	return strings.TrimSpace(string(head))
}

func writeManifest(t *testing.T, m *corpus.Manifest) {
	t.Helper()
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(manifestPath, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}
