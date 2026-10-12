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
		"auth_emails.graphql operations": {"auth-emails", 2},
		"user.graphql operations": {
			"user",
			4,
		}, // checkUsername, deviceAuthRequestInfo, 2 guards of pendingEmailVerifications
		"field-less resolver": {"resolvers", 1},
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
	if domainFacts != 5 { // every resolver method with a schema field; not helper, not Orphan
		t.Errorf("got %d domain facts, want 5", domainFacts)
	}
}
