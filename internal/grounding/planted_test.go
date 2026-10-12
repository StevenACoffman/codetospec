package grounding_test

import (
	"os"
	"testing"

	"codetospec/internal/extract"
	"codetospec/internal/grounding"
)

// TestPlantedFaults measures the grounding check against known defects
// planted in rules over the real fixture code. Each case is either caught
// (must come out ungrounded) or a recorded blind spot (must come out
// grounded): a fault this mechanical check cannot see, left to the LLM
// crosscheck. A blind spot that starts being caught fails the test, so the
// list stays honest in both directions.
func TestPlantedFaults(t *testing.T) {
	fsys := os.DirFS("../../testdata/fixture")
	const (
		prorata = "app/Services/Billing/ProrataCalculator.php"
		cited   = "11-24" // the calculate method; every case cites it
	)
	cases := map[string]struct {
		requirement string
		want        grounding.Status
		wantMissing string // Result.Summary for caught faults
	}{
		"faithful rule (control)": {
			requirement: "SI le jour d'activation est inférieur ou égal à 1, ALORS le systeme doit " +
				"facturer le montant mensuel complet.",
			want: grounding.StatusGrounded, // nothing planted
		},
		"changed threshold": {
			requirement: "SI le jour d'activation est inférieur ou égal à 5, ALORS le systeme doit " +
				"facturer le montant mensuel complet.",
			want:        grounding.StatusUngrounded,
			wantMissing: "5",
		},
		"citation too narrow": {
			requirement: "Le systeme doit arrondir le prorata à 2 décimales.",
			want:        grounding.StatusUngrounded,
			wantMissing: "2 @" + prorata + ":9",
		},
		"invented error message": {
			requirement: `SI le montant mensuel est négatif, ALORS le systeme doit rejeter le calcul ` +
				`avec "amount too large".`,
			want:        grounding.StatusUngrounded,
			wantMissing: `"amount too large"`,
		},
		"negated condition": {
			requirement: "SI le montant mensuel est supérieur ou égal à 0, ALORS le systeme doit " +
				"rejeter le calcul.",
			want: grounding.StatusGrounded, // blind spot: 0 is present, only the comparison is wrong
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			citations, err := grounding.Load(fsys, []extract.Ref{{Path: prorata, Lines: cited}})
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			got := grounding.Check(tc.requirement, citations, ',') // French requirements
			if got.Status != tc.want {
				t.Fatalf("status = %q, want %q (missing: %q)", got.Status, tc.want, got.Summary())
			}
			if summary := got.Summary(); summary != tc.wantMissing {
				t.Fatalf("missing = %q, want %q", summary, tc.wantMissing)
			}
		})
	}
}
