package locale_test

import (
	"reflect"
	"regexp"
	"slices"
	"testing"

	"github.com/StevenACoffman/codetospec/internal/locale"
)

// TestTextsAreComplete guards the translation table: every language fills
// every field, with the same fmt verbs as French, so no output line is ever
// blank or garbled in one language only.
func TestTextsAreComplete(t *testing.T) {
	verbs := regexp.MustCompile(`%[a-z]`)
	french := reflect.ValueOf(locale.For("fr")).Elem()
	for _, text := range locale.All() {
		v := reflect.ValueOf(text).Elem()
		for _, f := range reflect.VisibleFields(v.Type()) {
			if f.Type.Kind() != reflect.String {
				continue // a group (Labels, Readme) or a non-text setting
			}
			t.Run(text.Lang+"/"+f.Name, func(t *testing.T) {
				got := v.FieldByIndex(f.Index).String()
				if got == "" {
					t.Fatal("empty")
				}
				want := verbs.FindAllString(french.FieldByIndex(f.Index).String(), -1)
				if have := verbs.FindAllString(got, -1); !slices.Equal(have, want) {
					t.Fatalf("fmt verbs %v, want %v (as in French)", have, want)
				}
			})
		}
	}
}

func TestFor(t *testing.T) {
	cases := map[string]struct {
		lang        string
		want        string
		wantDecimal byte
	}{
		"french":            {"fr", "fr", ','},
		"english":           {"en", "en", '.'},
		"unknown is french": {"de", "fr", ','},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			text := locale.For(tc.lang)
			if text.Lang != tc.want {
				t.Fatalf("For(%q).Lang = %q, want %q", tc.lang, text.Lang, tc.want)
			}
			if text.DecimalSeparator != tc.wantDecimal {
				t.Fatalf("For(%q).DecimalSeparator = %q, want %q",
					tc.lang, text.DecimalSeparator, tc.wantDecimal)
			}
		})
	}
}

func TestPromptFillsPlaceholders(t *testing.T) {
	got := locale.For("en").Prompt("Write in <LANG>:\n<EARS>")
	want := "Write in English:\n" + locale.For("en").EARSPatterns
	if got != want {
		t.Fatalf("Prompt = %q, want %q", got, want)
	}
}
