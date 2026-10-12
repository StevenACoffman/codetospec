package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestRun(t *testing.T) {
	service := filepath.Join("testdata", "service")
	const root, lang = "-root", "-lang"
	cases := map[string]struct {
		args       []string
		wantCode   int
		wantStderr string
	}{
		"ok": {args: []string{root, service, lang, "en"}, wantCode: exitOK},
		"missing root": {
			args:       []string{lang, "en"},
			wantCode:   exitUsage,
			wantStderr: "-root is required",
		},
		"bad lang": {
			args:       []string{root, service, lang, "de"},
			wantCode:   exitUsage,
			wantStderr: "-lang must be",
		},
		"unknown flag": {args: []string{"-nope"}, wantCode: exitUsage},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := run(tc.args, &stdout, &stderr); code != tc.wantCode {
				t.Fatalf("run = %d, want %d; stderr:\n%s", code, tc.wantCode, stderr.String())
			}
			if !strings.Contains(stderr.String(), tc.wantStderr) {
				t.Fatalf("stderr lacks %q:\n%s", tc.wantStderr, stderr.String())
			}
			if tc.wantCode != 0 {
				return
			}
			var env Envelope
			if err := json.Unmarshal(
				stdout.Bytes(),
				&env,
			); err != nil || env.Schema != schemaID ||
				len(env.Facts) == 0 {
				t.Fatalf("stdout is not a facts envelope (err %v):\n%s", err, stdout.String())
			}
		})
	}
}
