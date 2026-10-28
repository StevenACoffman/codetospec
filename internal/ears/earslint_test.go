package ears_test

import (
	"testing"

	"github.com/StevenACoffman/codetospec/internal/ears"
)

// refused marks a case Parse must refuse.
const refused = "refused"

// TestEarsLintCorpus runs the cases of another EARS linter,
// github.com/labeth/ears-lint-go (corpus_test.go, MIT), named by their id
// there: an outside check of what Parse reads and refuses. Each accepted
// sentence has the kind given and passes Check.
//
// Left out: I501, I502 and I506 (a dangling "and", an unclosed
// parenthesis, "or or"), which Parse keeps as condition text — none
// occurs in 19,081 real rules; A301, refused there only for a name its
// catalog lists twice. I410, a WHEN and an IF, is refused there (one
// trigger) and complex here, by design.
func TestEarsLintCorpus(t *testing.T) {
	cases := []struct{ id, sentence, want string }{
		{"V001", "The brake control system shall apply brake torque.", ears.Ubiquitous},
		{"V002", "While aircraft is on ground, the engine control system shall enable " +
			"reverse thrust.", ears.State},
		{"V003", "When brake is requested, the brake control system shall apply brake " +
			"torque.", ears.Event},
		{"V004", "Where sunroof is installed, the door control system shall display the " +
			"sunroof control.", ears.Optional},
		{"V005", "If an invalid credit card number is entered, then the ATM shall display " +
			"please re-enter credit card details.", ears.Unwanted},
		{"V006", "While aircraft is on ground, when reverse thrust is commanded, the engine " +
			"control system shall enable reverse thrust.", ears.Complex},
		{"V102", "While aircraft is on ground and (hydraulic pressure is available or " +
			"service mode is active), when reverse thrust is commanded, the engine control " +
			"system shall enable reverse thrust.", ears.Complex},
		{"V105", "Where remote start is enabled and premium navigation is enabled, the " +
			"power management system shall precondition the cabin.", ears.Optional},
		{"V108", "While aircraft is on ground, where reverse thrust is installed, when " +
			"reverse thrust is commanded, the engine control system shall enable reverse " +
			"thrust.", ears.Complex},
		{"V109", "While aircraft is on ground and not service mode is active, when reverse " +
			"thrust is commanded or reverse thrust is commanded by pilot, the engine " +
			"control system shall enable reverse thrust.", ears.Complex},
		{"V110", "When low voltage event and (sensor fault is detected or data link is " +
			"lost), the diagnostic system shall record a degraded-power fault.", ears.Event},
		{"V201", "When brake request, the brake controller shall apply brake torque.", ears.Event},
		{"V202", "While on ground, when reverse thrust commanded, the FADEC shall enable " +
			"reverse thrust.", ears.Complex},
		{
			"V204", "Where premium nav, the nav system shall display turn-by-turn guidance.",
			ears.Optional,
		},
		{"A303", "While degraded mode, when sensor fault, the diagnostics shall raise an " +
			"alert.", ears.Complex},
		{"I402", "If sensor fault is detected, the diagnostic system shall store a fault " +
			"record.", refused},
		{"I404", "While aircraft is on ground, the engine control system shall shall " +
			"enable reverse thrust.", refused},
		{"I405", "The engine control system enable reverse thrust.", refused},
		{"I406", "When reverse thrust is commanded, the engine control system.", refused},
		{"I410", "When reverse thrust is commanded, if emergency braking is active, then " +
			"the engine control system shall enable reverse thrust.", ears.Complex},
		{
			"N701", "The engine control system should enable reverse thrust when commanded.",
			refused,
		},
		{"S801", "  While   aircraft is on ground ,   when reverse thrust is commanded , " +
			"the engine control system shall enable reverse thrust . ", ears.Complex},
		{"S803", "IF sensor fault is detected, THEN the diagnostic system shall store a " +
			"fault record.", ears.Unwanted},
		{"K901", "While (aircraft is on ground and (hydraulic pressure is available or " +
			"(service mode is active and not degraded mode is active))), when ((reverse " +
			"thrust is commanded) or (emergency braking is active and not data link is " +
			"lost)), the engine control system shall enable reverse thrust.", ears.Complex},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			c, err := ears.Parse(tc.sentence)
			switch {
			case tc.want == refused && err == nil:
				t.Fatalf("Parse accepted it: %+v", c)
			case tc.want == refused:
				return
			case err != nil:
				t.Fatalf("Parse: %v", err)
			}
			if kind := ears.KindOf(&c); kind != tc.want {
				t.Errorf("kind = %s, want %s", kind, tc.want)
			}
			if issue := ears.Check(tc.want, tc.sentence); issue != "" {
				t.Errorf("Check: %s", issue)
			}
		})
	}
}
