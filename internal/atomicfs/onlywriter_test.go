package atomicfs_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// repoRoot is the repository root, relative to this package's directory
// (go test runs in it): every module is checked, the extractors included.
const repoRoot = "../.."

// isFileMutator reports whether the function name of the package at
// importPath creates, changes, renames or removes a file or directory.
func isFileMutator(importPath, name string) bool {
	switch importPath {
	case "os":
		switch name {
		case "WriteFile", "Create", "CreateTemp", "OpenFile", "Rename", "Remove",
			"RemoveAll", "Mkdir", "MkdirAll", "MkdirTemp", "Chmod", "Chtimes",
			"Truncate", "Symlink", "Link":
			return true
		}
	case "io/ioutil":
		switch name {
		case "WriteFile", "TempFile", "TempDir":
			return true
		}
	}
	return false
}

// TestOnlyAtomicfsWrites enforces the AGENTS.md rule that internal/atomicfs
// is the only writer: an interrupted run must leave the previous output
// intact, which holds only if every write is a rename. Tests are free to
// write their fixtures.
func TestOnlyAtomicfsWrites(t *testing.T) {
	var found []string
	err := filepath.WalkDir(repoRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != repoRoot && skippedDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		found = append(found, mutations(t, path)...)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range found {
		t.Errorf("%s: only internal/atomicfs writes (AGENTS.md); write with "+
			"atomicfs.WriteFile, or atomicfs.ReplaceDir for a directory (an extractor, "+
			"a separate module, emits its facts on stdout instead)", f)
	}
}

// skippedDir reports whether a directory holds no production code of
// codetospec: VCS data, fixtures, dependencies, build output, and
// atomicfs itself.
func skippedDir(name string) bool {
	switch name {
	case "testdata", "vendor", "node_modules", "bin", "atomicfs":
		return true
	}
	return strings.HasPrefix(name, ".")
}

// mutations lists the calls of path to an isFileMutator function, as
// "file:line: pkg.Func".
func mutations(t *testing.T, path string) []string {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	// The name each checked package goes by in this file.
	imported := map[string]string{}
	for _, imp := range file.Imports {
		importPath, _ := strconv.Unquote(imp.Path.Value)
		if importPath != "os" && importPath != "io/ioutil" {
			continue
		}
		name := filepath.Base(importPath)
		if imp.Name != nil {
			name = imp.Name.Name
		}
		imported[name] = importPath
	}
	var found []string
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		if importPath, isImport := imported[pkg.Name]; isImport &&
			isFileMutator(importPath, sel.Sel.Name) {
			pos := fset.Position(call.Pos())
			rel, _ := filepath.Rel(repoRoot, pos.Filename)
			found = append(found, filepath.ToSlash(rel)+":"+strconv.Itoa(pos.Line)+": "+
				pkg.Name+"."+sel.Sel.Name)
		}
		return true
	})
	return found
}
