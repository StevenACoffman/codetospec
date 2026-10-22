package atomicfs_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/StevenACoffman/codetospec/internal/atomicfs"
)

// Files the tests write: one only in the previous content, one only in the
// new content.
const (
	oldFile = "old.md"
	newFile = "new.md"
)

// TestReplaceDir: the target ends up holding exactly the new content, or,
// when fill fails, exactly the old one; no staging directory survives.
func TestReplaceDir(t *testing.T) {
	errFill := errors.New("fill failed")
	cases := []struct {
		name     string
		previous []string // files in dir before the call; nil = no dir
		fillErr  error
		want     []string // files in dir after the call; nil = no dir
	}{
		{"no previous dir", nil, nil, []string{newFile}},
		{"previous dir replaced", []string{oldFile}, nil, []string{newFile}},
		{"failed fill keeps previous", []string{oldFile}, errFill, []string{oldFile}},
		{"failed fill, no previous dir", nil, errFill, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			parent := t.TempDir()
			dir := filepath.Join(parent, "nodes")
			if tc.previous != nil {
				writeFiles(t, dir, tc.previous)
			}

			err := atomicfs.ReplaceDir(dir, func(staging string) error {
				writeFiles(t, staging, []string{newFile})
				return tc.fillErr
			})

			if !errors.Is(err, tc.fillErr) {
				t.Fatalf("err = %v, want %v", err, tc.fillErr)
			}
			if got := listFiles(t, dir); !slices.Equal(got, tc.want) {
				t.Errorf("dir holds %v, want %v", got, tc.want)
			}
			if got := listFiles(t, parent); !slices.Equal(got, wantParent(tc.want)) {
				t.Errorf("parent holds %v: a staging dir survived", got)
			}
		})
	}
}

// TestWriteFile: the file holds the data and no temporary file remains.
func TestWriteFile(t *testing.T) {
	cases := []struct {
		name     string
		previous string // "" = no file before the call
	}{
		{"new file", ""},
		{"overwrites", "old"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "sub", "graph.json")
			if tc.previous != "" {
				writeFiles(t, filepath.Dir(path), []string{"graph.json"})
			}

			if err := atomicfs.WriteFile(path, []byte("new")); err != nil {
				t.Fatal(err)
			}

			data, err := os.ReadFile(filepath.Clean(path))
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != "new" {
				t.Errorf("file holds %q, want %q", data, "new")
			}
			if got := listFiles(t, filepath.Dir(path)); !slices.Equal(got, []string{"graph.json"}) {
				t.Errorf("dir holds %v: a temporary file survived", got)
			}
		})
	}
}

// wantParent is what the parent of the replaced dir holds: the dir itself
// when it exists, nothing else.
func wantParent(dirFiles []string) []string {
	if dirFiles == nil {
		return []string{}
	}
	return []string{"nodes"}
}

func writeFiles(t *testing.T, dir string, names []string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// listFiles returns the sorted entry names of dir, or nil when dir does not
// exist.
func listFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}
