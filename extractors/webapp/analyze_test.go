package main

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

// testGOROOT() stands in for a Go installation: two net/http status codes.
func testGOROOT() fstest.MapFS {
	return fstest.MapFS{httpStatusFile: {Data: []byte(`package http

const (
	StatusOK         = 200 // RFC 9110, 15.3.1
	StatusBadRequest = 400 // RFC 9110, 15.5.1
)
`)}}
}

// testInputs analyzes testdata/service against a small supergraph.
func testInputs(lang string) *Inputs {
	return &Inputs{
		Service:      os.DirFS(filepath.Join("testdata", "service")),
		GOROOT:       testGOROOT(),
		Supergraph:   os.DirFS(filepath.Join("testdata", "supergraph")),
		ServiceGraph: "USERS",
		Lang:         lang,
		AccessPkgs:   []string{"permissions"},
	}
}

// TestAnalyzeGolden pins the facts derived from testdata/service, in both
// languages. Regenerate with UPDATE_GOLDEN=1 go test ./... and review.
func TestAnalyzeGolden(t *testing.T) {
	for _, lang := range []string{"fr", "en"} {
		t.Run(lang, func(t *testing.T) {
			facts, _, err := Analyze(testInputs(lang))
			if err != nil {
				t.Fatalf("Analyze: %v", err)
			}
			got, err := json.MarshalIndent(facts, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			got = append(got, '\n')
			golden := filepath.Join("testdata", "golden."+lang+".json")
			if os.Getenv("UPDATE_GOLDEN") == "1" {
				if writeErr := os.WriteFile(golden, got, 0o600); writeErr != nil {
					t.Fatal(writeErr)
				}
				return
			}
			want, err := fs.ReadFile(os.DirFS("testdata"), "golden."+lang+".json")
			if err != nil {
				t.Fatalf("missing golden (run UPDATE_GOLDEN=1 go test ./...): %v", err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("facts differ from %s:\n%s", golden, got)
			}
		})
	}
}

func TestAnalyzeWarnings(t *testing.T) {
	_, warnings, err := Analyze(testInputs("en"))
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	joined := strings.Join(warnings, "\n")
	cases := map[string]string{
		"unparsable file is skipped":      "skip resolvers/broken.go",
		"field-less resolver is reported": "Query.Orphan matches no schema field",
		"kind without its struct":         "kind Orphan: struct Missing not found",
	}
	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			if !strings.Contains(joined, want) {
				t.Fatalf("warnings lack %q:\n%s", want, joined)
			}
		})
	}
}

func TestAnalyzeFacts(t *testing.T) {
	facts, _, err := Analyze(testInputs("en"))
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	byDomain := map[string][]string{} // domain → rule slugs
	domainFacts := 0
	for _, f := range facts {
		switch f.Kind {
		case kindRule:
			byDomain[f.Attrs[attrDomain]] = append(byDomain[f.Attrs[attrDomain]], f.Attrs[attrSlug])
		case kindDomain:
			domainFacts++
		}
		if f.Attrs[attrSlug] == "" && f.Kind == kindRule {
			t.Errorf("rule %s has no slug", f.ID)
		}
	}
	cases := map[string]struct {
		domain string
		want   int
	}{
		// addAuthEmail, addCoachRole, enableTotp
		"auth_emails.graphql operations": {"auth-emails", 3},
		// bio (nested), checkUsername, deviceAuthRequestInfo, emails,
		// recoveryCodes, 2 guards each of nickname and pendingEmailVerifications
		"user.graphql operations": {"user", 9},
		"field-less resolver":     {"resolvers", 1},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := len(byDomain[tc.domain]); got != tc.want {
				t.Fatalf(
					"domain %s has %d rules %v, want %d",
					tc.domain,
					got,
					byDomain[tc.domain],
					tc.want,
				)
			}
		})
	}
	// 11 resolver methods with a schema field (not helpers, not Orphan), plus
	// the whole of kaid.go, whose methods share one domain.
	if domainFacts != 12 {
		t.Errorf("got %d domain facts, want 12", domainFacts)
	}
	wholeFile := map[string]bool{}
	for _, f := range facts {
		if f.Kind == kindDomain && strings.HasPrefix(f.Source.Lines, "1-") {
			wholeFile[f.Source.Path] = true
		}
	}
	if !wholeFile["resolvers/kaid.go"] || wholeFile["resolvers/guards.go"] {
		t.Errorf(
			"whole-file domain facts = %v, want kaid.go only (guards.go spans two domains)",
			wholeFile,
		)
	}
}

// TestCatchAllSchemas: a schema file names no domain only when it holds
// most methods and spreads over many resolver files.
func TestCatchAllSchemas(t *testing.T) {
	// layout builds one resolver file per method; schemaOf[i] declares
	// method i's field.
	layout := func(schemaOf ...string) (schemaIndex, []*source, [][]resolverMethod) {
		idx := schemaIndex{}
		files := make([]*source, len(schemaOf))
		methods := make([][]resolverMethod, len(schemaOf))
		for i, schemaFile := range schemaOf {
			name := "M" + strings.Repeat("x", i)
			idx["Query"] = append(idx["Query"], schemaField{name: name, file: schemaFile})
			files[i] = &source{name: "resolvers/" + name + ".go"}
			methods[i] = []resolverMethod{{typeName: "Query", method: name}}
		}
		return idx, files, methods
	}
	repeat := func(file string, n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = file
		}
		return out
	}
	cases := map[string]struct {
		schemaOf []string
		want     bool // schema.graphql is a catch-all
	}{
		"dominant and spread": {
			append(repeat("schema.graphql", 10), repeat("user.graphql", 2)...), true,
		},
		"dominant in few files": {append(repeat("schema.graphql", 5), "user.graphql"), false},
		"spread but not dominant": {
			append(repeat("schema.graphql", 10), repeat("user.graphql", 10)...), false,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			idx, files, methods := layout(tc.schemaOf...)
			if got := catchAllSchemas(idx, files, methods)["schema.graphql"]; got != tc.want {
				t.Fatalf("catch-all = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestAccessPredicates: only the exported bool-first functions of an
// access package are checks.
func TestAccessPredicates(t *testing.T) {
	names, err := goSources(testInputs("en").Service)
	if err != nil {
		t.Fatal(err)
	}
	sources, _ := parseSources(testInputs("en").Service, names)
	got := accessPredicates(sources, []string{"permissions"})
	cases := map[string]bool{
		"permissions.UserCanViewEmails": true,
		"permissions.LoadVisibleEmails": false, // a loader
		"permissions.canSee":            false, // unexported
		"resolvers.helper":              false, // not an access package
	}
	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			if got[name] != want {
				t.Fatalf("predicate %s = %v, want %v", name, got[name], want)
			}
		})
	}
}
