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
		"unwanted after a clause": {
			ears.Unwanted,
			"WHEN converting rows, IF a row fails, THEN the system shall abort.", true,
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
		"unknown kind": {"complex", "The system shall round amounts.", false},
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
