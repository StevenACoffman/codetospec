package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/StevenACoffman/codetospec/internal/config"
)

// inheritFlag names the shared spec to inherit from.
const inheritFlag = "--inherit"

// TestParseRunInherit: an inherited spec needs the code it cites, given as
// the --ref-root of the same name.
func TestParseRunInherit(t *testing.T) {
	src, spec, code := t.TempDir(), t.TempDir(), t.TempDir()
	base := []string{"--src", src, "--out", t.TempDir(), "--base-url", "http://llm", "--model", "m"}
	cases := map[string]struct {
		args    []string
		wantErr string
	}{
		"with its ref-root": {
			args: []string{inheritFlag, "pkg=" + spec, "--ref-root", "pkg=" + code},
		},
		"without its ref-root": {
			args: []string{inheritFlag, "pkg=" + spec}, wantErr: "needs --ref-root pkg=",
		},
		"not name=dir": {
			args: []string{inheritFlag, spec}, wantErr: "want name=dir",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			cfg, err := config.ParseRun(append(append([]string(nil), base...), tc.args...))
			if tc.wantErr == "" {
				if err != nil || cfg.Inherit["pkg"] != spec {
					t.Fatalf("cfg = %+v, err %v", cfg, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want %q", err, tc.wantErr)
			}
		})
	}
}

// TestParseRunActors: the glossary of actors comes from codetospec.yaml;
// a name a sentence could not open with stops the run before any call.
func TestParseRunActors(t *testing.T) {
	cases := map[string]struct {
		yaml    string
		wantErr string
	}{
		"valid":      {yaml: "actors:\n  the scheduler: Runs the nightly jobs.\n"},
		"no article": {yaml: "actors:\n  scheduler: Runs jobs.\n", wantErr: `"scheduler"`},
		"default":    {yaml: "actors:\n  the system: Everything.\n", wantErr: "the system"},
		"aliases": {yaml: "actors:\n  the scheduler:\n    description: Runs the nightly " +
			"jobs.\n    aliases: [the job runner]\n"},
		"alias, no article": {
			yaml:    "actors:\n  the scheduler:\n    aliases: [runner]\n",
			wantErr: `alias "runner"`,
		},
		"alias is a name": {
			yaml: "actors:\n  the scheduler:\n    aliases: [the notifier]\n" +
				"  the notifier: Pages.\n",
			wantErr: "is the name of an actor",
		},
		"alias of two": {
			yaml: "actors:\n  the scheduler:\n    aliases: [the runner]\n" +
				"  the notifier:\n    aliases: [the runner]\n",
			wantErr: `already stands for "the notifier"`,
		},
		"not text": {yaml: "actors:\n  the scheduler: [1]\n", wantErr: `"the scheduler"`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			src := t.TempDir()
			if err := os.WriteFile(
				filepath.Join(src, "codetospec.yaml"),
				[]byte(tc.yaml),
				0o600,
			); err != nil {
				t.Fatal(err)
			}
			cfg, err := config.ParseRun([]string{
				"--src", src, "--out", t.TempDir(), "--base-url", "http://llm", "--model", "m",
			})
			if tc.wantErr == "" {
				if err != nil ||
					cfg.Actors.Descriptions["the scheduler"] != "Runs the nightly jobs." ||
					(name == "aliases") != (cfg.Actors.Aliases["the job runner"] == "the scheduler") {
					t.Fatalf("cfg.Actors = %v, err %v", cfg, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want one naming %q", err, tc.wantErr)
			}
		})
	}
}
