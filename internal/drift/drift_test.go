package drift_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/StevenACoffman/codetospec/internal/drift"
	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/graph"
)

func writeSource(t *testing.T, dir, rel, content string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// Citation spans and the root name the tests share.
const (
	fBody      = "3-5"
	bothLines  = "1-2"
	sharedRoot = "webapp"
	calcFile   = "calc.go"
	sharedFile = "x.go"
	ruleID     = "rule.x.r"
)

// srcRoots reads citations from dir.
func srcRoots(dir string) extract.Roots { return extract.Roots{Src: os.DirFS(dir)} }

func ruleWithDigest(t *testing.T, root string, src extract.Ref) graph.Node {
	t.Helper()
	digest, err := drift.Digest(srcRoots(root), []extract.Ref{src})
	if err != nil {
		t.Fatalf("Digest: %v", err)
	}
	return graph.Node{
		ID:      ruleID,
		Type:    "rule",
		Sources: []extract.Ref{src},
		Extra:   map[string]string{"digest": digest},
	}
}

func TestCheckDetectsStaleAndOK(t *testing.T) {
	dir := t.TempDir()
	writeSource(t, dir, calcFile, "package x\n\nfunc f() int {\n\treturn 1\n}\n")
	rule := ruleWithDigest(t, dir, extract.Ref{Path: calcFile, Lines: fBody})

	// Unchanged -> ok.
	if r := drift.Check([]graph.Node{rule}, srcRoots(dir)); r.OK != 1 || r.Drifted() {
		t.Fatalf("unchanged rule should be ok, got %+v", r)
	}

	// Edit inside the cited span -> stale.
	writeSource(t, dir, calcFile, "package x\n\nfunc f() int {\n\treturn 2\n}\n")
	r := drift.Check([]graph.Node{rule}, srcRoots(dir))
	if r.Stale != 1 || !r.Drifted() {
		t.Fatalf("edited cited code should be stale, got %+v", r)
	}
	if r.Rules[0].Status != drift.StatusStale {
		t.Errorf("status = %q, want stale", r.Rules[0].Status)
	}
}

func TestCheckEditOutsideCitedSpanStaysOK(t *testing.T) {
	dir := t.TempDir()
	writeSource(t, dir, calcFile, "package x\n\nfunc f() int {\n\treturn 1\n}\n\nfunc g() {}\n")
	rule := ruleWithDigest(t, dir, extract.Ref{Path: calcFile, Lines: fBody})

	// Change a line OUTSIDE the cited span (line 7) -> still ok.
	writeSource(
		t,
		dir,
		calcFile,
		"package x\n\nfunc f() int {\n\treturn 1\n}\n\nfunc g() { println(1) }\n",
	)
	if r := drift.Check([]graph.Node{rule}, srcRoots(dir)); r.OK != 1 || r.Drifted() {
		t.Fatalf("edit outside cited span must not drift, got %+v", r)
	}
}

func TestCheckBrokenAndUnknown(t *testing.T) {
	dir := t.TempDir()
	writeSource(t, dir, calcFile, "package x\n\nfunc f() int {\n\treturn 1\n}\n")
	rule := ruleWithDigest(t, dir, extract.Ref{Path: calcFile, Lines: fBody})

	// Truncate the file so the cited span no longer resolves -> broken.
	writeSource(t, dir, calcFile, "package x\n")
	if r := drift.Check([]graph.Node{rule}, srcRoots(dir)); r.Broken != 1 {
		t.Fatalf("shrunk file should be broken, got %+v", r)
	}

	// A rule with no stored digest -> unknown.
	noDigest := graph.Node{
		ID:      "rule.x.r2",
		Type:    "rule",
		Sources: []extract.Ref{{Path: calcFile, Lines: "1-1"}},
	}
	if r := drift.Check([]graph.Node{noDigest}, srcRoots(dir)); r.Unknown != 1 || r.Drifted() {
		t.Fatalf("rule without digest should be unknown, got %+v", r)
	}
}

// TestDigestReadsNamedRoot: a citation of another root is hashed from it,
// and differs from the same path in --src.
func TestDigestReadsNamedRoot(t *testing.T) {
	src, shared := t.TempDir(), t.TempDir()
	writeSource(t, src, sharedFile, "a\nb\n")
	writeSource(t, shared, sharedFile, "a\nB\n")
	roots := extract.Roots{
		Src:   os.DirFS(src),
		Extra: map[string]fs.FS{sharedRoot: os.DirFS(shared)},
	}
	local, err := drift.Digest(roots, []extract.Ref{{Path: sharedFile, Lines: bothLines}})
	if err != nil {
		t.Fatal(err)
	}
	rooted, err := drift.Digest(
		roots,
		[]extract.Ref{{Path: sharedFile, Lines: bothLines, Root: sharedRoot}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if local == rooted {
		t.Fatal("a rooted citation hashed like the --src file")
	}
	if _, unrootedErr := drift.Digest(
		srcRoots(src),
		[]extract.Ref{{Path: sharedFile, Lines: bothLines, Root: sharedRoot}},
	); unrootedErr == nil {
		t.Fatal("an unknown root digested")
	}
}
