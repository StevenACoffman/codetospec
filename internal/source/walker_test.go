package source_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/StevenACoffman/codetospec/internal/source"
)

// writeTree writes files (slash path → content) under a new root.
func writeTree(t *testing.T, files map[string][]byte) string {
	t.Helper()
	root := t.TempDir()
	for rel, data := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// walked lists the paths Walk keeps.
func walked(t *testing.T, root string, exclude []string) map[string]bool {
	t.Helper()
	files, err := source.Walk(root, exclude)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	got := make(map[string]bool, len(files))
	for _, f := range files {
		got[f.Path] = true
	}
	return got
}

// TestWalkSkipsBinaryFiles: a file is kept only when its first bytes read
// as source text.
func TestWalkSkipsBinaryFiles(t *testing.T) {
	// A SCIP protobuf index opens with ASCII tool/path strings (no early NUL)
	// then invalid-UTF-8 varint bytes: the case a NUL-only sniff missed.
	scip := append([]byte("\n7\x08\x01\x12$\n\x08scip-php\x12\x050.0.1\x1a\x11--memory-limit=2G"),
		0xb1, 0xc3, 0x28, 0xff, 0xfe, 0x93, 0x12)
	cases := map[string]struct {
		data []byte
		kept bool
	}{
		"empty":         {nil, true},
		"php source":    {[]byte("<?php\nfunction f() { return 1; }\n"), true},
		"utf8 accents":  {[]byte("// règle métier : activation prorata\n"), true},
		"nul byte":      {[]byte("abc\x00def"), false},
		"scip protobuf": {scip, false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			root := writeTree(t, map[string][]byte{"sample.php": tc.data})
			if got := walked(t, root, nil)["sample.php"]; got != tc.kept {
				t.Errorf("kept = %v, want %v", got, tc.kept)
			}
		})
	}
}

func TestWalkExcludesDirsAndGlobs(t *testing.T) {
	php := []byte("<?php echo 1;\n")
	root := writeTree(t, map[string][]byte{
		"index.php":         php,
		"docs/analyse.md":   php,
		"notes.md":          php,
		"data/export.csv":   php,
		"vendor/lib.php":    php,
		"usine/accueil.php": php,
		".hidden/x.php":     php,
	})
	got := walked(t, root, []string{"vendor", "*.md", "*.csv"})
	cases := map[string]bool{
		"index.php":         true,
		"usine/accueil.php": true,
		"notes.md":          false,
		"docs/analyse.md":   false,
		"data/export.csv":   false,
		"vendor/lib.php":    false,
		".hidden/x.php":     false,
	}
	for path, want := range cases {
		t.Run(path, func(t *testing.T) {
			if got[path] != want {
				t.Errorf("kept = %v, want %v", got[path], want)
			}
		})
	}
}
