package extract_test

import (
	"testing"

	"github.com/StevenACoffman/codetospec/internal/extract"
)

// hintPath is the file every test hint covers.
const hintPath = "r/a.go"

func domainFact(lines, domain string) extract.Fact {
	return extract.Fact{
		Kind:   extract.KindDomain,
		ID:     "domain." + lines + ":" + domain,
		Attrs:  map[string]string{extract.AttrDomain: domain},
		Source: extract.Ref{Path: hintPath, Lines: lines},
	}
}

func TestDomainHints(t *testing.T) {
	hints := extract.NewDomainHints([]extract.Fact{
		domainFact("1-100", "outer"),
		domainFact("10-20", "inner"),
		domainFact("30-40", "zeta"),
		domainFact("30-40", "alpha"), // same span: name breaks the tie
		domainFact("50-x", "broken"),
		domainFact("60-70", "Not A Slug"),
		{
			Kind:   "symbol",
			Attrs:  map[string]string{extract.AttrDomain: "ignored"},
			Source: extract.Ref{Path: hintPath, Lines: "1-200"},
		},
	})
	cases := map[string]struct {
		path string
		line int
		want string
	}{
		"smallest covering span wins": {hintPath, 15, "inner"},
		"outer span elsewhere":        {hintPath, 5, "outer"},
		"tie broken by domain name":   {hintPath, 35, "alpha"},
		"invalid hints are ignored":   {hintPath, 65, "outer"},
		"no hint past the spans":      {hintPath, 150, ""},
		"other kinds are ignored":     {hintPath, 150, ""},
		"other file":                  {"r/b.go", 15, ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := hints.At(tc.path, tc.line); got != tc.want {
				t.Fatalf("At(%s, %d) = %q, want %q", tc.path, tc.line, got, tc.want)
			}
		})
	}
}
