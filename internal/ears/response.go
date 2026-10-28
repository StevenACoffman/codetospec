package ears

import (
	"errors"
	"fmt"
	"strings"
)

// Response is one system response: Text as the sentence writes it after
// the modal, and Action, its opening words naming what is done — the
// verb, negation included ("not store", "ne pas stocker"); the rest is
// the object. Action is "" when the boundary is unknown: a sentence does
// not say where the verb ends ("shall only serve", "shall log out the
// user"), so Parse never guesses it; the model writes it, and a node file
// keeps it beside the sentence (WithActions).
type Response struct {
	Text   string `json:"text"`
	Action string `json:"action,omitempty"`
}

// Object is the response after its action: what the action is done to,
// "" when it names none; the whole text when the action is unknown.
func (r Response) Object() string {
	if r.Action == "" {
		return r.Text
	}
	return strings.TrimSpace(strings.TrimPrefix(r.Text, r.Action))
}

// Actions are c's response actions, in order, "" for an unknown one.
func Actions(c *Components) []string {
	actions := make([]string, len(c.Shall))
	for i, r := range c.Shall {
		actions[i] = r.Action
	}
	return actions
}

// WithActions returns c with the actions recorded beside its sentence
// (Actions of the components that wrote it): each that still opens its
// response, word for word; none when their count is not the responses'
// (an edit added or removed one, so positions say nothing).
func WithActions(c *Components, actions []string) Components {
	out := *c
	out.Shall = append([]Response(nil), c.Shall...)
	if len(actions) != len(out.Shall) {
		return out
	}
	for i := range out.Shall {
		if opensWith(out.Shall[i].Text, actions[i]) {
			out.Shall[i].Action = actions[i]
		}
	}
	return out
}

// opensWith reports whether action is the opening words of text, whole
// words: the one test of an action, for the model's components and for
// actions read back.
func opensWith(text, action string) bool {
	rest, ok := strings.CutPrefix(text, action)
	return ok && action != "" && (rest == "" || rest[0] == ' ')
}

// checkResponse returns why r, a response the model wrote, cannot be
// written in a sentence with its action recorded.
func checkResponse(r Response) error {
	if err := checkPart(r.Text, false); err != nil {
		return err
	}
	switch {
	case r.Action == "":
		return errors.New("action is required: the opening words of text naming what is done")
	case !opensWith(r.Text, r.Action):
		return fmt.Errorf("action %q is not the opening words of text %q", r.Action, r.Text)
	}
	return nil
}

// texts are the responses' texts, in order.
func texts(responses []Response) []string {
	out := make([]string, len(responses))
	for i, r := range responses {
		out[i] = r.Text
	}
	return out
}

// response is r without the subject a model repeats ("the system shall
// x"), a French negated one kept as "ne x".
func response(r string) string {
	r = trimPart(r)
	for _, w := range wordings() {
		if n := foldedWordPrefix(r, w.system+" "+w.modal); n > 0 {
			return trimPart(r[n:])
		}
		if w.negation == "" {
			continue
		}
		if n := foldedWordPrefix(r, w.system+" "+w.negation+" "+w.modal); n > 0 {
			return w.negation + " " + trimPart(r[n:])
		}
	}
	return r
}
