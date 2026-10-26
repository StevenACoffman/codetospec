package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeModule writes files (path → content) into a new module "srv" and
// returns its directory.
func writeModule(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(dir, "go.mod"),
		[]byte("module srv\n\ngo 1.26\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// constantsModule refers to an aliased standard-library constant, local
// integer and float constants, a string constant, and one bare name that
// two packages bind to different values.
func constantsModule(t *testing.T) string {
	t.Helper()
	return writeModule(t, map[string]string{
		"server.go": `package srv

import h "net/http"

const (
	maxRetries = 3
	ratio      = 0.25
	label      = "x"
)

func Handle() (int, int, float64, string) {
	return h.StatusBadRequest, maxRetries, ratio, label
}
`,
		"a/a.go": "package a\n\nconst limit = 5\n\nfunc A() int { return limit }\n",
		"b/b.go": "package b\n\nconst limit = 7\n\nfunc B() int { return limit }\n",
	})
}

func TestConstantFacts(t *testing.T) {
	dir := constantsModule(t)
	m, ok := loadPackages(dir, func(format string, a ...any) { t.Logf(format, a...) })
	if !ok {
		t.Fatal("module did not load")
	}
	got := map[string]Fact{}
	for _, f := range ConstantFacts(m.pkgs, m.fset, dir) {
		got[f.Attrs["name"]] = f
	}

	cases := map[string]struct {
		name       string
		wantValue  string // "" when no fact is expected
		wantSource string
	}{
		"aliased standard-library constant": {
			name: "h.StatusBadRequest", wantValue: "400",
		},
		"local integer, cited at its declaration": {
			name: "maxRetries", wantValue: "3", wantSource: "server.go:6-6",
		},
		"local float": {
			name:       "ratio",
			wantValue:  "0.25",
			wantSource: "server.go:7-7",
		},
		"string constant is not numeric": {name: "label"},
		"bare name bound to two values":  {name: "limit"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f, found := got[tc.name]
			if found != (tc.wantValue != "") {
				t.Fatalf("fact for %s: found = %v, want %v (%+v)", tc.name, found, !found, f)
			}
			if f.Attrs["value"] != tc.wantValue {
				t.Errorf("value = %q, want %q", f.Attrs["value"], tc.wantValue)
			}
			if tc.wantSource != "" && f.Source.Path+":"+f.Source.Lines != tc.wantSource {
				t.Errorf("source = %+v, want %s", f.Source, tc.wantSource)
			}
		})
	}
}

// TestRun drives the command end to end: flags, envelope, exit code.
func TestRun(t *testing.T) {
	dir := constantsModule(t)
	cases := map[string]struct {
		args     []string
		wantCode int
		wantOut  string // substring of stdout
		wantErr  string // substring of stderr
	}{
		"constants in the envelope": {
			args: []string{"-root", dir}, wantCode: exitOK,
			wantOut: `"id":"constant.go.maxRetries"`, wantErr: "constant(s)",
		},
		"missing root": {
			args: nil, wantCode: exitError, wantErr: "--root is required",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := run(tc.args, &stdout, &stderr); code != tc.wantCode {
				t.Fatalf("exit = %d, want %d; stderr:\n%s", code, tc.wantCode, stderr.String())
			}
			if !strings.Contains(stdout.String(), tc.wantOut) ||
				!strings.Contains(stderr.String(), tc.wantErr) {
				t.Fatalf("stdout:\n%s\nstderr:\n%s", stdout.String(), stderr.String())
			}
			if tc.wantCode == exitOK {
				var env Envelope
				if err := json.Unmarshal(
					stdout.Bytes(),
					&env,
				); err != nil ||
					env.Schema != schemaID {
					t.Fatalf("envelope = %+v, err %v", env, err)
				}
			}
		})
	}
}
