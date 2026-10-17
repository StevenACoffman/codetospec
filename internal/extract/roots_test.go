package extract_test

import (
	"errors"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/StevenACoffman/codetospec/internal/extract"
)

// TestRootsReadFile: a citation reads from --src, or from the root it
// names; an unknown root or an escaping path is an error.
func TestRootsReadFile(t *testing.T) {
	const local, webapp = "a.go", "webapp"
	roots := extract.Roots{
		Src: fstest.MapFS{local: {Data: []byte("src")}},
		Extra: map[string]fs.FS{
			webapp: fstest.MapFS{"pkg/web/x.go": {Data: []byte("shared")}},
		},
	}
	cases := map[string]struct {
		ref     extract.Ref
		want    string
		unknown bool
	}{
		"source tree":  {ref: extract.Ref{Path: local}, want: "src"},
		"named root":   {ref: extract.Ref{Path: "pkg/web/x.go", Root: webapp}, want: "shared"},
		"unknown root": {ref: extract.Ref{Path: local, Root: "other"}, unknown: true},
		"escape":       {ref: extract.Ref{Path: "../" + local}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			data, err := roots.ReadFile(tc.ref)
			switch {
			case tc.want != "":
				if err != nil || string(data) != tc.want {
					t.Fatalf("ReadFile = %q, %v; want %q", data, err, tc.want)
				}
			case err == nil:
				t.Fatalf("ReadFile(%+v) succeeded", tc.ref)
			case tc.unknown && !errors.Is(err, extract.ErrUnknownRoot):
				t.Fatalf("err = %v, want ErrUnknownRoot", err)
			}
		})
	}
}

// TestRefFile: a --src citation is named by its path, another by
// @root/path.
func TestRefFile(t *testing.T) {
	if got := (extract.Ref{Path: "a.go"}).File(); got != "a.go" {
		t.Errorf("File() = %q", got)
	}
	if got := (extract.Ref{Path: "pkg/x.go", Root: "webapp"}).File(); got != "@webapp/pkg/x.go" {
		t.Errorf("File() = %q", got)
	}
}
