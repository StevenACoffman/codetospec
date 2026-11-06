//go:build cerboslocal

package cerbos_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/StevenACoffman/codetospec/internal/cerbos"
)

// TestCerbosCompile runs `cerbos compile` on the export: it checks the
// policy (CEL included) and runs the generated test suite, so the CEL
// templates must decide every case as the rules' conditions do. The binary
// is $CERBOS_BIN, else cerbos on PATH; skipped when neither exists.
//
//	go test -tags cerboslocal ./internal/cerbos
func TestCerbosCompile(t *testing.T) {
	bin := os.Getenv("CERBOS_BIN")
	if bin == "" {
		found, err := exec.LookPath("cerbos")
		if err != nil {
			t.Skip("no cerbos binary (set CERBOS_BIN or put cerbos on PATH)")
		}
		bin = found
	}
	m := usersModel()
	spec, err := cerbos.Export(&m, service)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for rel, data := range spec.Files {
		path := filepath.Join(dir, filepath.FromSlash(rel))
		if err = os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.CommandContext(t.Context(), bin, "compile", dir) //nolint:gosec // the test's binary
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("cerbos compile: %v\n%s", err, out)
	}
	t.Logf("%s", out)
}
