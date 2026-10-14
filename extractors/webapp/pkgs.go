package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strconv"
	"strings"
)

// sharedCode is the part of webapp's shared code (pkg/) a service builds
// on: the packages it imports, transitively, parsed.
type sharedCode struct {
	module  string // webapp's module path, e.g. github.com/Khan/webapp
	sources []source
}

// importedPackages parses the packages of webapp's shared code (pkg/) that
// sources import, and those they import in turn, so checks defined there
// are judged by their code: the pubsub signature check, serve's
// task-handler wrapper and GraphQL middleware. Packages are found by import
// path under webapp's module path, read from its go.mod. A nil webapp, or
// one without go.mod, yields none; a file that does not parse is a
// warning.
func importedPackages(webapp fs.FS, sources []source) (sharedCode, []string) {
	if webapp == nil {
		return sharedCode{}, nil
	}
	module, err := modulePath(webapp)
	if err != nil {
		return sharedCode{}, []string{fmt.Sprintf("shared packages: %v", err)}
	}
	code := sharedCode{module: module}
	var warnings []string
	seen := make(map[string]bool)
	for frontier := importedDirs(sources, module); len(frontier) > 0; {
		for _, dir := range frontier {
			seen[dir] = true
		}
		parsed, parseWarnings := parseSources(webapp, goFilesIn(webapp, frontier))
		code.sources = append(code.sources, parsed...)
		warnings = append(warnings, parseWarnings...)
		frontier = slices.DeleteFunc(
			importedDirs(parsed, module),
			func(dir string) bool { return seen[dir] },
		)
	}
	slices.SortFunc(code.sources, func(a, b source) int { return strings.Compare(a.name, b.name) })
	return code, warnings
}

// importedDirs returns the directories, relative to webapp's root, of the
// pkg/ packages sources import, sorted.
func importedDirs(sources []source, module string) []string {
	prefix := module + "/" + sharedDir + "/"
	var dirs []string
	for i := range sources {
		for _, spec := range sources[i].file.Imports {
			imported, err := strconv.Unquote(spec.Path.Value)
			if err != nil || !strings.HasPrefix(imported, prefix) {
				continue
			}
			if dir := strings.TrimPrefix(imported, module+"/"); !slices.Contains(dirs, dir) {
				dirs = append(dirs, dir)
			}
		}
	}
	slices.Sort(dirs)
	return dirs
}

// goFilesIn lists the non-test Go files of dirs; a directory absent from
// the checkout (generated, vendored) has none.
func goFilesIn(fsys fs.FS, dirs []string) []string {
	var names []string
	for _, dir := range dirs {
		entries, err := fs.ReadDir(fsys, dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			name := e.Name()
			if !e.IsDir() && strings.HasSuffix(name, ".go") &&
				!strings.HasSuffix(name, "_test.go") {
				names = append(names, path.Join(dir, name))
			}
		}
	}
	return names
}

// modulePath reads the module path of a go.mod at the root of fsys.
func modulePath(fsys fs.FS) (string, error) {
	mod, err := fs.ReadFile(fsys, "go.mod")
	if err != nil {
		return "", fmt.Errorf("read go.mod: %w", err)
	}
	lines := bufio.NewScanner(bytes.NewReader(mod))
	for lines.Scan() {
		if module, ok := strings.CutPrefix(strings.TrimSpace(lines.Text()), "module "); ok {
			return strings.Trim(strings.TrimSpace(module), `"`), nil
		}
	}
	return "", errors.New("go.mod declares no module")
}
