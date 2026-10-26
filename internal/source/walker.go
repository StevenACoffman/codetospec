// Package source walks the analyzed repository and detects languages by
// file extension.
package source

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/StevenACoffman/codetospec/internal/sitter"
)

// File is one source file discovered under the repository root.
type File struct {
	Path     string // slash-separated, relative to the root
	AbsPath  string
	Language string // "" when no grammar matches the extension
	// Generated marks a file whose header says a tool wrote it (see
	// generatedHeader): its rules are its generator's, not its own.
	Generated bool
}

// Walk lists source files under root in deterministic (lexical) order,
// skipping excluded entries, hidden entries and binary files. An exclude
// entry matches directory and file names exactly or as a glob pattern
// (e.g. "*.md", "*.csv").
func Walk(root string, exclude []string) ([]File, error) {
	excluded := excluder(exclude)
	var files []File
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		skipped := strings.HasPrefix(name, ".") || excluded(name)
		switch {
		case d.IsDir() && path != root && skipped:
			return filepath.SkipDir
		case d.IsDir() || skipped:
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return fmt.Errorf("relativize %s: %w", path, err)
		}
		if kind := sniff(path); !kind.binary {
			files = append(files, File{
				Path:      filepath.ToSlash(rel),
				AbsPath:   path,
				Language:  sitter.LanguageForPath(name),
				Generated: kind.generated,
			})
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk %s: %w", root, err)
	}
	return files, nil
}

// excluder matches a file or directory name against the exclude entries,
// exactly or as a glob.
func excluder(exclude []string) func(name string) bool {
	var patterns []string
	for _, name := range exclude {
		if name = strings.TrimSpace(name); name != "" {
			patterns = append(patterns, name)
		}
	}
	return func(name string) bool {
		for _, p := range patterns {
			if ok, err := filepath.Match(p, name); p == name || err == nil && ok {
				return true
			}
		}
		return false
	}
}

// sniffBytes is how much of a file sniff reads: enough for a header and a
// fair sample of text.
const sniffBytes = 8000

// fileKind is what the first bytes of a file tell about it.
type fileKind struct {
	binary    bool // not source text (or unreadable)
	generated bool // source text a tool wrote (see generatedHeader)
}

// sniff reads the first bytes of a file to decide whether it is source
// text at all, and whether a tool generated it.
func sniff(path string) fileKind {
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return fileKind{binary: true}
	}
	defer func() {
		_ = f.Close() // read-only: a failed close loses nothing
	}()
	buf := make([]byte, sniffBytes)
	n, _ := f.Read(buf)
	if looksBinary(buf[:n]) {
		return fileKind{binary: true}
	}
	return fileKind{generated: generatedHeader(buf[:n])}
}

// generatedHeader reports whether head carries Go's generated-code marker,
// a "// Code generated … DO NOT EDIT." line before the first line that is
// neither a comment nor blank (https://go.dev/s/generatedcode). A generator
// that holds the marker in a string, after its package clause, is not
// generated itself.
func generatedHeader(head []byte) bool {
	for line := range strings.Lines(string(head)) {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "// Code generated ") && strings.HasSuffix(line, " DO NOT EDIT."):
			return true
		case line != "" && !strings.HasPrefix(line, "//"):
			return false
		}
	}
	return false
}

// looksBinary reports whether a byte sample is not decodable source text. A
// NUL byte, invalid UTF-8, or a high share of control bytes each mark it. The
// UTF-8 check is what catches protobuf index blobs (e.g. a .scip file) whose
// first bytes are ASCII tool/path strings and carry no early NUL — the older
// NUL-only sniff let those through and chunked them into phantom rules.
func looksBinary(buf []byte) bool {
	if len(buf) == 0 {
		return false
	}
	if bytes.IndexByte(buf, 0) >= 0 {
		return true
	}
	// A full read may cut the last rune; drop up to 3 trailing bytes before
	// judging UTF-8 so a truncated boundary rune is not misread as binary.
	trimmed := buf
	for i := 0; i < utf8.UTFMax-1 && len(trimmed) > 0 && !utf8.Valid(trimmed); i++ {
		trimmed = trimmed[:len(trimmed)-1]
	}
	if !utf8.Valid(trimmed) {
		return true
	}
	ctrl := 0
	for _, c := range buf {
		if c < 0x09 || (c > 0x0d && c < 0x20) || c == 0x7f {
			ctrl++
		}
	}
	return ctrl*100 > len(buf)*30
}
