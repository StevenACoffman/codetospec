package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/StevenACoffman/codetospec/internal/config"
	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/position"
)

// TestLoadPositions: the layers come from nature_by_path, or from the
// target's anarch.json read against its go.mod module; a mapping that does
// not resolve, or both keys at once, is an error before any LLM call.
func TestLoadPositions(t *testing.T) {
	const (
		technical = position.Technical
		adapter   = "adapter"
		goMod     = "module \"example.com/app\"\n\ngo 1.27\n"
		anarch    = `{"layers": [{"name": "adapter", "packages": ["example.com/app/internal/http"]}]}`
	)
	src := t.TempDir()
	for name, data := range map[string]string{"go.mod": goMod, "anarch.json": anarch} {
		if err := os.WriteFile(filepath.Join(src, name), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	file := []extract.Ref{{Path: "internal/http/server.go", Lines: "1-2"}}
	cases := map[string]struct {
		cfg     config.Config
		want    string
		wantErr bool
	}{
		"by path": {
			cfg:  config.Config{NatureByPath: map[string][]string{technical: {"internal/http"}}},
			want: position.Technical,
		},
		"by anarch layer": {
			cfg:  config.Config{Src: src, NatureByLayer: map[string]string{adapter: technical}},
			want: position.Technical,
		},
		"no anarch.json": {
			cfg: config.Config{
				Src:           t.TempDir(),
				NatureByLayer: map[string]string{adapter: technical},
			},
			wantErr: true,
		},
		"both keys": {
			cfg: config.Config{
				Src:           src,
				NatureByPath:  map[string][]string{technical: {"cmd/..."}},
				NatureByLayer: map[string]string{adapter: technical},
			},
			wantErr: true,
		},
		"neither": {cfg: config.Config{}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			m, err := loadPositions(&tc.cfg)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, want error: %v", err, tc.wantErr)
			}
			if got := m.Of(file); got != tc.want {
				t.Errorf("Of = %q, want %q", got, tc.want)
			}
		})
	}
}
