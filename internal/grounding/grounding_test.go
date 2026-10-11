package grounding_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"codetospec/internal/extract"
	"codetospec/internal/grounding"
)

// billingPath is where billing lives in the test file system.
const billingPath = "app/Billing.php"

// billing is a small source file; line numbers matter to the cases below.
const billing = `<?php
class Billing
{
    const PLAN_PRO = 'PRO';
    const GRACE_DAYS = 30;

    public function fee(int $amount): float
    {
        if ($amount > 1_000) {
            return $amount * 0.10;
        }
        return $amount * 1.5;
    }
}
`

func TestCheck(t *testing.T) {
	fsys := fstest.MapFS{billingPath: {Data: []byte(billing)}}
	cases := map[string]struct {
		requirement string
		lines       string
		wantStatus  grounding.Status
		wantSummary string
	}{
		"no literal": {
			requirement: "QUAND un abonné active, le systeme doit facturer.",
			lines:       "7-13",
			wantStatus:  grounding.StatusUnchecked,
		},
		"threshold with thousands separator": {
			requirement: "SI le montant dépasse 1 000, ALORS le systeme doit appliquer des frais.",
			lines:       "9-10",
			wantStatus:  grounding.StatusGrounded,
		},
		"percent matches decimal ratio": {
			requirement: "QUAND le montant dépasse 1000, le systeme doit prélever 10 % de frais.",
			lines:       "8-10",
			wantStatus:  grounding.StatusGrounded,
		},
		"decimal comma": {
			requirement: "Le systeme doit multiplier le montant par 1,5.",
			lines:       "12-12",
			wantStatus:  grounding.StatusGrounded,
		},
		"quoted constant, elision is not a quote": {
			requirement: "QUAND l'abonné choisit le plan 'PRO', le systeme doit l'activer.",
			lines:       "4-4",
			wantStatus:  grounding.StatusGrounded,
		},
		"hallucinated threshold": {
			requirement: "SI le montant dépasse 5000, ALORS le systeme doit appliquer des frais.",
			lines:       "9-10",
			wantStatus:  grounding.StatusUngrounded,
			wantSummary: "5000",
		},
		"citation misses the line holding the value": {
			requirement: "Le systeme doit accorder 30 jours de grâce.",
			lines:       "7-13",
			wantStatus:  grounding.StatusUngrounded,
			wantSummary: "30 @app/Billing.php:5",
		},
		"invented quoted value": {
			requirement: `QUAND le plan vaut "GOLD", le systeme doit refuser.`,
			lines:       "4-5",
			wantStatus:  grounding.StatusUngrounded,
			wantSummary: `"GOLD"`,
		},
		"digits glued to a word are not values": {
			requirement: "Le systeme doit hacher en SHA256 selon ISO8601.",
			lines:       "4-4",
			wantStatus:  grounding.StatusUnchecked,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			citations, err := grounding.Load(
				fsys,
				[]extract.Ref{{Path: billingPath, Lines: tc.lines}},
			)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			got := grounding.Check(tc.requirement, citations)
			if got.Status != tc.wantStatus {
				t.Errorf(
					"status = %q, want %q (missing: %s)",
					got.Status,
					tc.wantStatus,
					got.Summary(),
				)
			}
			if summary := got.Summary(); summary != tc.wantSummary {
				t.Errorf("summary = %q, want %q", summary, tc.wantSummary)
			}
		})
	}
}

func TestLoadErrors(t *testing.T) {
	fsys := fstest.MapFS{billingPath: {Data: []byte(billing)}}
	cases := map[string]struct {
		ref     extract.Ref
		wantErr string
	}{
		"missing file": {extract.Ref{Path: "app/Nope.php", Lines: "1-2"}, "Nope.php"},
		"bad range":    {extract.Ref{Path: billingPath, Lines: "x"}, "Billing.php"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := grounding.Load(fsys, []extract.Ref{tc.ref})
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want it to mention %q", err, tc.wantErr)
			}
		})
	}
}

func TestLoadClampsToFileLength(t *testing.T) {
	fsys := fstest.MapFS{"a.go": {Data: []byte("x := 42\n")}}
	citations, err := grounding.Load(fsys, []extract.Ref{{Path: "a.go", Lines: "1-99"}})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := grounding.Check(
		"Le systeme doit renvoyer 42.",
		citations,
	); got.Status != grounding.StatusGrounded {
		t.Fatalf("status = %q, want grounded", got.Status)
	}
}
