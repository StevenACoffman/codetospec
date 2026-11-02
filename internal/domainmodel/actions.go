package domainmodel

import (
	"maps"
	"slices"
	"unicode"

	"github.com/StevenACoffman/codetospec/internal/graph"
	"github.com/StevenACoffman/indomain/pkg/model"
)

// Action is one action a model gives an entity, and the rules touching
// the entity's code that perform it.
type Action struct {
	Name  string   // as the model writes it
	Rules []string // rule ids, sorted; none when no rule performs it
}

// RuleAction is an action rules perform on a modeled entity that no model
// action of the entity names: a lead for the model, as Unstated is for
// invariants.
type RuleAction struct {
	Verb  string   // the action's verb, as verb reads it
	Rules []string // rule ids, sorted
}

// ruleVerbs reads the verb of each EARS response of n whose action is
// known (recorded beside the sentence): an unknown action is skipped,
// never guessed from the sentence, which does not say where the verb ends.
func ruleVerbs(n *graph.Node) []string {
	c, ok := graph.RuleComponents(n)
	if !ok {
		return nil
	}
	var verbs []string
	for _, r := range c.Shall {
		if v := verb(r.Action); v != "" && !slices.Contains(verbs, v) {
			verbs = append(verbs, v)
		}
	}
	return verbs
}

// verb is the first word of an action that names what is done, lowercased:
// negation and restriction words are skipped, so "not store" and "only
// serve" perform store and serve (a rule forbidding an action is about
// it). A camelCase or snake_case model action name splits into words
// first: "assignTeacher" → "assign". "" when no word is left.
//
// Phrasal verbs share their first word ("log in", "log out" → "log"): the
// pairings are leads for a person, not proof.
func verb(action string) string {
	for _, w := range splitWords(action) {
		if !isModifier(w) {
			return w
		}
	}
	return ""
}

// isModifier reports whether w is a negation or restriction word an EARS
// action opens with, in a supported language.
func isModifier(w string) bool {
	switch w {
	case "not", "never", "only", "also", "always",
		"ne", "pas", "jamais", "plus", "que", "seulement", "toujours":
		return true
	}
	return false
}

// splitWords lowercases s into words, splitting on anything but letters
// and digits and before an uppercase letter that follows a lowercase one.
func splitWords(s string) []string {
	var words []string
	var cur []rune
	flush := func() {
		if len(cur) > 0 {
			words = append(words, string(cur))
			cur = cur[:0]
		}
	}
	prevLower := false
	for _, r := range s {
		switch {
		case !unicode.IsLetter(r) && !unicode.IsDigit(r):
			flush()
			prevLower = false
			continue
		case unicode.IsUpper(r) && prevLower:
			flush()
		}
		cur = append(cur, unicode.ToLower(r))
		prevLower = unicode.IsLower(r)
	}
	flush()
	return words
}

// matchActions pairs each model action with the rules performing it, and
// lists the verbs of rules that no model action names.
//
// Requires: rules touch the entity the actions belong to, sorted by id.
// Ensures:  one Action per model action, in model order; RuleAction
// sorted by verb; every rule id list sorted.
func matchActions(actions []model.Action, rules []rule) ([]Action, []RuleAction) {
	byVerb := map[string][]string{}
	for _, r := range rules {
		for _, v := range r.verbs {
			byVerb[v] = append(byVerb[v], r.id)
		}
	}
	out := make([]Action, 0, len(actions))
	named := map[string]bool{}
	for _, a := range actions {
		v := verb(a.Name)
		named[v] = true
		out = append(out, Action{Name: a.Name, Rules: byVerb[v]})
	}
	var unmodeled []RuleAction
	for _, v := range slices.Sorted(maps.Keys(byVerb)) {
		if !named[v] {
			unmodeled = append(unmodeled, RuleAction{Verb: v, Rules: byVerb[v]})
		}
	}
	return out, unmodeled
}
