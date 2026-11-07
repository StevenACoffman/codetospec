package policy_test

import (
	"testing"

	"github.com/StevenACoffman/codetospec/internal/locale"
	"github.com/StevenACoffman/codetospec/internal/policy"
)

// TestStatement: each pin shape reads as one sentence: nested groups in
// parentheses, registry phrases in place of names (the bare name when
// there is none), capabilities in words, a filter's item and an
// approximate pin's caveat; French alike.
func TestStatement(t *testing.T) {
	reg, err := policy.ParseRegistry([]byte(`{
		"teacher_of": {"from": ["x.Y"], "phrase": {"en": "teaches the student", "fr": "enseigne à l'élève"}},
		"is_child": {"from": ["x.Z"], "phrase": {"en": "is a child account"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	cs, err := policy.ParseCheckers([]byte(`{
		"nested": {"any": [{"means": "service_admin"},
			{"all": [{"means": "login_strict"}, {"means": "relation", "relation": "teacher_of"}]}]},
		"capability": {"means": "capability", "capability": "capabilities.CanViewCDIReports", "scope": "user"},
		"fromCall": {"means": "capability", "scope": "global", "capability_arg": 1},
		"filter": {"item": "Note", "means": "coach"},
		"attribute": {"all": [{"means": "login"},
			{"not": {"means": "attribute", "of": "caller", "attribute": "is_child"}},
			{"means": "attribute", "of": "resource", "attribute": "published"}], "approx": true},
		"cross": {"unpinnable": "cross_service"}}`))
	if err != nil {
		t.Fatal(err)
	}
	en, fr := locale.For("en"), locale.For("fr")
	for _, tc := range []struct {
		name string
		text *locale.Text
		want string
	}{
		{"nested", en, "Allowed when the request is an administrator call from another service, or " +
			"(the caller is signed in with a full account (not a phantom one) and the caller teaches the student)."},
		{"nested", fr, "Autorisé si la requête est un appel administrateur d'un autre service, ou " +
			"(l'appelant est connecté avec un compte complet (pas un compte fantôme) et l'appelant enseigne à l'élève)."},
		{"capability", en, `Allowed when the caller holds the capability "View CDI reports" for the user concerned.`},
		{"fromCall", en, "Allowed when the caller holds the capability the request names everywhere."},
		{"filter", en, "An item of kind Note is visible when the caller coaches the user concerned."},
		{"attribute", en, "Allowed when the caller is signed in, it is not the case that the caller is a child account " +
			`and the resource has the attribute "published". ` +
			"(Approximate: the code also allows some requests this statement leaves out.)"},
		{"cross", fr, "Non énoncé : cross_service."},
	} {
		pin := cs[tc.name]
		if got := policy.Statement(&pin, reg, tc.text); got != tc.want {
			t.Errorf("%s (%s):\n got %s\nwant %s", tc.name, tc.text.Lang, got, tc.want)
		}
	}
}
