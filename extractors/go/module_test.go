package main

import (
	"os"
	"path/filepath"
	"testing"
)

// loadModule writes a module named module holding files into a temporary
// directory and loads it as the extractor does; it returns the module and
// its root.
func loadModule(t *testing.T, module string, files map[string]string) (loadedModule, string) {
	t.Helper()
	dir := t.TempDir()
	files["go.mod"] = "module " + module + "\n\ngo 1.26\n"
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	m, ok := loadPackages(dir, func(format string, a ...any) { t.Logf(format, a...) })
	if !ok {
		t.Fatal("module did not load")
	}
	return m, dir
}
