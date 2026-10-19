package config_test

import (
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
