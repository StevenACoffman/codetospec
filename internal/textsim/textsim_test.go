package textsim_test

import (
	"testing"

	"codetospec/internal/textsim"
)

func TestJaccard(t *testing.T) {
	cases := map[string]struct {
		a, b string
		want float64
	}{
		"identical":          {"the system shall lock", "The system shall LOCK", 1},
		"disjoint":           {"alpha beta", "gamma delta", 0},
		"half":               {"alpha beta", "alpha gamma", 1.0 / 3},
		"short words ignore": {"a to of alpha", "alpha", 1},
		"empty":              {"", "alpha", 0},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := textsim.Jaccard(textsim.Words(tc.a), textsim.Words(tc.b)); got != tc.want {
				t.Fatalf("Jaccard = %v, want %v", got, tc.want)
			}
		})
	}
}
