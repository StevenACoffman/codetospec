package grounding_test

import (
	"testing"
	"testing/fstest"

	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/grounding"
)

// handler is a Go file whose values hide behind constants and templates.
const handler = `package auth

const maxRetries = 7

func deny(w http.ResponseWriter, appID string) {
	w.WriteHeader(http.StatusBadRequest)
	_ = "fbsr_" + appID
	_ = fmt.Sprintf("$p5k2$%x$%s", iterations, salt)
	for i := 0; i < maxRetries; i++ {
	}
}
`

func TestCheckConstantsAndPlaceholders(t *testing.T) {
	fsys := fstest.MapFS{"auth/handler.go": {Data: []byte(handler)}}
	constants := grounding.Constants([]extract.Fact{
		{
			Kind: extract.KindConstant,
			Attrs: map[string]string{
				extract.AttrName:  "http.StatusBadRequest",
				extract.AttrValue: "400",
			},
		},
		{
			Kind:  extract.KindConstant,
			Attrs: map[string]string{extract.AttrName: "maxRetries", extract.AttrValue: "7"},
		},
		{
			Kind:  extract.KindConstant,
			Attrs: map[string]string{extract.AttrName: "notNumeric", extract.AttrValue: "abc"},
		},
	})
	cases := map[string]struct {
		requirement string
		constants   map[string]float64
		want        grounding.Status
	}{
		"status code behind a qualified constant": {
			"IF the request is invalid, THEN the system shall respond with HTTP 400.",
			constants,
			grounding.StatusGrounded,
		},
		"same rule without constant facts": {
			"IF the request is invalid, THEN the system shall respond with HTTP 400.",
			nil,
			grounding.StatusUngrounded,
		},
		"bare constant": {
			"The system shall retry at most 7 times.", constants, grounding.StatusGrounded,
		},
		"bare constant without constant facts": {
			"The system shall retry at most 7 times.", nil, grounding.StatusUngrounded,
		},
		"template fixed part present": {
			`The system shall name the cookie "fbsr_<facebookAppID>".`,
			nil,
			grounding.StatusGrounded,
		},
		"template with several fixed parts in order": {
			`The system shall store the hash as "$p5k2$<hex_iterations>$<salt>".`,
			nil,
			grounding.StatusGrounded,
		},
		"template fixed part absent": {
			`The system shall name the cookie "fbm_<facebookAppID>".`,
			nil,
			grounding.StatusUngrounded,
		},
		"template with nothing fixed is not checked": {
			`The system shall echo "<value>".`, nil, grounding.StatusUnchecked,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			citations, err := grounding.Load(
				extract.Roots{Src: fsys},
				[]extract.Ref{{Path: "auth/handler.go", Lines: "5-11"}},
			)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			got := grounding.Check(
				tc.requirement,
				citations,
				grounding.Options{DecimalSeparator: '.', Constants: tc.constants},
			)
			if got.Status != tc.want {
				t.Fatalf("status = %q, want %q (missing: %s)", got.Status, tc.want, got.Summary())
			}
		})
	}
}
