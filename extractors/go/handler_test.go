package main

import (
	"go/token"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/tools/go/packages"
)

// routesFile is the fixture file registering the routes.
const routesFile = "routes.go"

func TestExtractRoutesResolvesHandlerFiles(t *testing.T) {
	const handlers = "handlers.go" // where the named handlers live
	files := map[string]string{
		"go.mod": "module srv\n\ngo 1.27\n",
		routesFile: `package srv

import "net/http"

func routes(mux *http.ServeMux) {
	mux.HandleFunc("/closure", func(w http.ResponseWriter, r *http.Request) { serveA(w, r) })
	mux.Handle("/conversion", http.HandlerFunc(serveB))
	mux.HandleFunc("/ident", serveC)
	mux.Handle("/wrapped", withAuth(serveD))
	mux.HandleFunc("/inline", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(204)
		_ = r
	})
	mux.Handle("/library", http.NotFoundHandler())
}

func withAuth(h http.HandlerFunc) http.Handler { return h }
`,
		handlers: `package srv

import "net/http"

func serveA(w http.ResponseWriter, r *http.Request) {}
func serveB(w http.ResponseWriter, r *http.Request) {}
func serveC(w http.ResponseWriter, r *http.Request) {}
func serveD(w http.ResponseWriter, r *http.Request) {}
`,
	}
	dir := t.TempDir()
	for name, src := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	fset := token.NewFileSet()
	pkgs, err := packages.Load(&packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax |
			packages.NeedTypes | packages.NeedTypesInfo | packages.NeedImports | packages.NeedDeps,
		Dir: dir, Fset: fset,
	}, "./...")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	got := map[string]Ref{}
	for _, r := range ExtractRoutes(pkgs, fset, dir) {
		got[r.Path] = r.HandlerRef
	}
	cases := map[string]Ref{
		"/closure":    {Path: handlers, Lines: "5-5"}, // the one function the closure calls
		"/conversion": {Path: handlers, Lines: "6-6"},
		"/ident":      {Path: handlers, Lines: "7-7"},
		"/wrapped":    {Path: handlers, Lines: "8-8"},     // the handler the wrapper takes
		"/inline":     {Path: routesFile, Lines: "10-13"}, // the closure is the handler
		// defined outside the module: the registration serves it
		"/library": {Path: routesFile, Lines: "14-14"},
	}
	for path, want := range cases {
		t.Run(path, func(t *testing.T) {
			if got[path] != want {
				t.Fatalf("handler of %s = %+v, want %+v", path, got[path], want)
			}
		})
	}
}
