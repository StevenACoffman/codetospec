package ears_test

import (
	"testing"

	"github.com/StevenACoffman/codetospec/internal/ears"
)

// TestCheck: each kind's own form passes, in both languages; a sentence
// without a modal, with two, or opening the way another kind does fails.
func TestCheck(t *testing.T) {
	cases := map[string]struct {
		kind, requirement string
		ok                bool
	}{
		"ubiquitous": {ears.Ubiquitous, "The system shall round amounts to cents.", true},
		"ubiquitous negative": {
			ears.Ubiquitous, "The system shall not store card numbers.", true,
		},
		"ubiquitous french": {
			ears.Ubiquitous, "Le système doit arrondir les montants au centime.", true,
		},
		"ubiquitous french negative": {
			ears.Ubiquitous, "Le systeme ne doit accorder aucune capacité au rôle `x`.", true,
		},
		"event": {
			ears.Event,
			"WHEN a user signs up, the system shall send a welcome email.",
			true,
		},
		"event french": {
			ears.Event,
			"QUAND un abonnement démarre, le systeme doit facturer.",
			true,
		},
		"state": {
			ears.State,
			"WHILE the account is locked, the system shall reject logins.",
			true,
		},
		"state french": {
			ears.State,
			"TANT QUE le compte est bloqué, le systeme doit refuser.",
			true,
		},
		"optional": {
			ears.Optional,
			"WHERE SSO is enabled, the system shall hide the password form.",
			true,
		},
		"optional french": {
			ears.Optional, "LÀ OÙ le SSO est actif, le systeme doit masquer le formulaire.", true,
		},
		"unwanted": {
			ears.Unwanted,
			"IF the amount is negative, THEN the system shall reject it.",
			true,
		},
		"unwanted french": {
			ears.Unwanted,
			"SI le montant est négatif, ALORS le systeme doit le rejeter.",
			true,
		},
		"no modal": {
			ears.Ubiquitous, "The system does not check access to `Query.x` yet.", false,
		},
		"two modals": {
			ears.Event,
			"WHEN a user signs up, the system shall log it and shall email them.",
			false,
		},
		"no system subject": {ears.Ubiquitous, "Users shall be notified.", false},
		"ubiquitous opening as unwanted": {
			ears.Ubiquitous,
			"IF the target is missing, THEN the system shall use the default.",
			false,
		},
		"event without WHEN": {ears.Event, "The system shall send a welcome email.", false},
		"unwanted without THEN": {
			ears.Unwanted, "IF the amount is negative, the system shall reject it.", false,
		},
		"unknown kind": {"compound", "The system shall round amounts.", false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := ears.Check(tc.kind, tc.requirement)
			if (got == "") != tc.ok {
				t.Errorf("Check(%q, %q) = %q, want ok=%v", tc.kind, tc.requirement, got, tc.ok)
			}
		})
	}
}

// TestCheckComplex: a complex requirement reads as components of two or
// more patterns — two triggers included, a precondition before or after
// the trigger, IF … THEN last — and its actor may be any "the <system
// name>".
func TestCheckComplex(t *testing.T) {
	cases := map[string]struct {
		kind, requirement string
		ok                bool
	}{
		"WHEN and IF, declared as before the complex kind": {
			ears.Unwanted,
			"WHEN converting rows, IF a row fails, THEN the system shall abort.", true,
		},
		"WHEN and IF, complex": {
			ears.Complex,
			"WHEN converting rows, IF a row fails, THEN the system shall abort.", true,
		},
		"actor": {ears.Event, "WHEN mute is selected, the laptop shall mute.", true},
		"complex": {
			ears.Complex,
			"WHILE the queue is paused, WHEN a job arrives, the system shall hold it.", true,
		},
		"complex with one keyword": {
			ears.Complex, "WHEN a job arrives, the system shall hold it.", false,
		},
		"complex, trigger first": {
			ears.Complex,
			"WHEN a job arrives, WHILE the queue is paused, the system shall hold it.", true,
		},
		"complex, IF not last": {
			ears.Complex,
			"IF a job fails, WHEN it is retried, THEN the system shall log it.", false,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := ears.Check(tc.kind, tc.requirement)
			if (got == "") != tc.ok {
				t.Errorf("Check(%q, %q) = %q, want ok=%v", tc.kind, tc.requirement, got, tc.ok)
			}
		})
	}
}

// TestCheckStacked: stacked clauses declared as the kind they open with
// (graphs written before the complex kind) keep passing, and a condition
// after "shall" passes: a defining predicate or a restriction is a
// legitimate response (see the package doc).
func TestCheckStacked(t *testing.T) {
	cases := map[string]struct {
		kind, requirement string
	}{
		"unwanted after a state": {
			ears.Unwanted,
			"WHILE a sync runs, IF a row fails, THEN the system shall abort.",
		},
		"unwanted after a feature": {
			ears.Unwanted,
			"WHERE audits are on, IF a write fails, THEN the system shall log it.",
		},
		"event after a state": {
			ears.State,
			"WHILE the queue is paused, WHEN a job arrives, the system shall hold it.",
		},
		"defining predicate": {
			ears.Ubiquitous,
			"The system shall consider a lock expired when it is over 50 hours old.",
		},
		"restriction": {
			ears.Ubiquitous,
			"The system shall serve / only when the request is authenticated.",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := ears.Check(tc.kind, tc.requirement); got != "" {
				t.Errorf("Check(%q, %q) = %q, want ok", tc.kind, tc.requirement, got)
			}
		})
	}
}
