package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestPackageFacts: each analyzed package is named by its import path,
// whatever it is called (cmd/pull-topic holds package pull_topic), and so
// is each shared package the module imports, cited under its root.
func TestPackageFacts(t *testing.T) {
	const firstLine = "1-1"
	ws := writeWorkspace(t)
	topic := filepath.Join(ws, "app", "cmd", "pull-topic", "topic.go")
	if err := os.MkdirAll(filepath.Dir(topic), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(topic, []byte("package pull_topic\n\n// Run pulls.\nfunc Run() {}\n"),
		0o600); err != nil {
		t.Fatal(err)
	}
	shared, err := sharedRoots([]string{sharedName + "=" + filepath.Join(ws, "shared")})
	if err != nil {
		t.Fatal(err)
	}
	app := filepath.Join(ws, "app")
	m, ok := loadPackages(app, func(format string, a ...any) { t.Logf(format, a...) })
	if !ok {
		t.Fatal("module did not load")
	}
	got := map[string]Ref{}
	for _, f := range PackageFacts(m.pkgs, app, shared) {
		if f.Kind != kindPackage || f.ID != kindPackage+"."+f.Attrs[attrName] {
			t.Errorf("malformed fact %+v", f)
		}
		got[f.Attrs[attrName]] = f.Source
	}
	want := map[string]Ref{
		"example.com/app":                {Path: "main.go", Lines: firstLine},
		"example.com/app/cmd/pull-topic": {Path: "cmd/pull-topic/topic.go", Lines: firstLine},
		"example.com/shared":             {Path: "lib.go", Lines: firstLine, Root: sharedName},
	}
	if len(got) != len(want) {
		t.Errorf("packages = %v, want %v", got, want)
	}
	for path, ref := range want {
		if got[path] != ref {
			t.Errorf("%s cites %+v, want %+v", path, got[path], ref)
		}
	}
}
