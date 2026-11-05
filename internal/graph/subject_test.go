package graph_test

import (
	"testing"

	"github.com/StevenACoffman/codetospec/internal/graph"
)

// TestWithSubject: the actor changes, the rest of the sentence and of the
// body stays; a sentence already naming it, or one that does not parse,
// is left as is.
func TestWithSubject(t *testing.T) {
	body := "**Requirement (EARS)**: WHEN an assignment is archived, the system shall " +
		"notify its students.\n\n**Rationale**: kept."
	cases := map[string]struct {
		body, system, want string
		changed            bool
	}{
		"system → actor": {
			body:    body,
			system:  "the Assignment_Manager",
			changed: true,
			want: "**Requirement (EARS)**: WHEN an assignment is archived, the Assignment_Manager " +
				"shall notify its students.\n\n**Rationale**: kept.",
		},
		"already the actor": {body: body, system: "", want: body},
		"does not parse": {
			body:   "**Requirement (EARS)**: notify students",
			system: "the X",
			want:   "**Requirement (EARS)**: notify students",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			rule := graph.Node{
				ID: "rule.a.b", Type: graph.TypeRule, Body: tc.body,
				Extra: map[string]string{"ears": "event"},
			}
			got, changed := graph.WithSubject(&rule, tc.system)
			if changed != tc.changed || got.Body != tc.want {
				t.Errorf("got %v %q\nwant %v %q", changed, got.Body, tc.changed, tc.want)
			}
			if got.Extra[graph.ExtraEARSIssue] != "" {
				t.Errorf("ears_issue = %q", got.Extra[graph.ExtraEARSIssue])
			}
			if rule.Body != tc.body {
				t.Error("the input node was modified")
			}
		})
	}
}
