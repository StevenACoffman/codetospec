package ears

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Parse reads an EARS sentence in any language back into its components:
// conditions (WHERE, WHILE, WHEN, IF … THEN, in any order but IF last,
// which THEN closes), several joined by a capital AND, then the actor and
// one "shall" (or "doit", "ne doit"), responses separated by ";". It
// inverts Render, and reads the sentences of rules nobody wrote as
// components (extractor templates, edited rules, older graphs); a
// sentence it cannot read is an error, not a guess.
func Parse(sentence string) (Components, error) {
	s := strings.TrimSuffix(strings.TrimSpace(sentence), ".")
	w, at, err := findModal(s)
	if err != nil {
		return Components{}, err
	}
	before, negated := subjectText(&w, s[:at])
	start, err := actorSpan(&w, before)
	if err != nil {
		return Components{}, err
	}
	if !startsWithArticle(before[start:]) {
		return Components{}, errNoActor(&w)
	}

	c := Components{System: actor(before[start:])}
	for r := range strings.SplitSeq(s[at+len(w.modal):], ";") {
		c.Shall = append(c.Shall, Response{Text: strings.TrimSpace(r)})
	}
	if negated {
		c.Shall[0].Text = w.negation + " " + c.Shall[0].Text
	}
	if slices.Contains(texts(c.Shall), "") {
		return Components{}, errors.New("empty response")
	}
	if err = parseClauses(&w, before[:start], &c); err != nil {
		return Components{}, err
	}
	return c, nil
}

// subjectText is before, the sentence up to its modal, without the
// trailing spaces and French negation ("le système ne doit"), and whether
// it had one.
func subjectText(w *wording, before string) (string, bool) {
	before = strings.TrimRight(before, " ")
	if n := foldedWordSuffix(before, w.negation); w.negation != "" && n > 0 {
		return strings.TrimRight(before[:len(before)-n], " "), true
	}
	return before, false
}

// actorSpan returns where the actor starts in before, the sentence up to
// its modal: all of it when no keyword opens it (a ubiquitous
// requirement, which then holds no comma); else after the last ", " and
// any THEN after it; else, without a comma, after the last THEN or from
// the last article. Parse and Check share it, so both name the same
// actor.
func actorSpan(w *wording, before string) (int, error) {
	if _, _, ok := openingClause(w, before); !ok {
		if strings.Contains(before, ",") {
			return 0, errors.New("text before the system that opens with no EARS keyword")
		}
		return 0, nil
	}
	start := 0
	if i := strings.LastIndex(before, ", "); i >= 0 {
		start = i + len(", ")
	}
	if then := lastWord(before[start:], w.then); then >= 0 {
		return start + then, nil
	}
	if start > 0 {
		return start, nil
	}
	if art := lastArticle(before); art >= 0 {
		return art, nil
	}
	return 0, errNoActor(w)
}

// errNoActor is the error for a sentence whose modal has no actor.
func errNoActor(w *wording) error {
	return fmt.Errorf(`%q without "the <system name>" as its subject`, w.modal)
}

// lastWord returns the offset in s just after the last of words (folded,
// as a whole word, followed by a space); -1 when none is there.
func lastWord(s string, words []string) int {
	end := -1
	for i := range s {
		if i > 0 && isLetterAt(s, i-1) {
			continue
		}
		for _, word := range words {
			if n := foldedWordPrefix(s[i:], word); n > 0 && i+n < len(s) && s[i+n] == ' ' {
				end = i + n + 1
			}
		}
	}
	return end
}

// lastArticle returns the offset of the last article in s; -1 when none.
func lastArticle(s string) int {
	last := -1
	for i := range s {
		if (i == 0 || !isLetterAt(s, i-1)) && startsWithArticle(s[i:]) {
			last = i
		}
	}
	return last
}

// findModal returns the wording of s's one modal and its byte offset.
func findModal(s string) (wording, int, error) {
	var (
		found wording
		at    = -1
	)
	for i := range s {
		if i > 0 && isLetterAt(s, i-1) {
			continue
		}
		for _, w := range wordings() {
			if n := foldedWordPrefix(s[i:], w.modal); n > 0 {
				if at >= 0 {
					return wording{}, 0, errors.New(`more than one "shall": one behavior per rule`)
				}
				found, at = w, i
			}
		}
	}
	if at < 0 {
		return wording{}, 0, errors.New(`no "shall": not an EARS requirement`)
	}
	return found, at, nil
}

// isLetterAt reports whether the rune ending at byte i of s is a letter.
func isLetterAt(s string, i int) bool {
	r, _ := utf8.DecodeLastRuneInString(s[:i+1])
	return unicode.IsLetter(r)
}

// parseClauses reads prefix, the sentence before its actor, into c's
// conditions: each opens with a keyword of w, after ", " for all but the
// first; at most one WHEN and one IF, the IF last, closed by THEN.
func parseClauses(w *wording, prefix string, c *Components) error {
	prefix = strings.TrimSuffix(strings.TrimSpace(prefix), ",")
	if prefix == "" {
		return nil
	}
	then := false
	for _, kw := range w.then {
		if n := foldedWordSuffix(prefix, kw); n > 0 {
			prefix = strings.TrimSuffix(strings.TrimSpace(prefix[:len(prefix)-n]), ",")
			then = true
		}
	}
	for i, text := range splitClauses(w, prefix) {
		cl, body, ok := openingClause(w, text)
		switch {
		case !ok && i == 0:
			return errors.New("text before the system that opens with no EARS keyword")
		case !ok:
			return fmt.Errorf("clause %q opens with no EARS keyword", text)
		case c.If != "":
			return fmt.Errorf("%s … %s must be the last condition", w.ifs[0], w.then[0])
		case cl == clauseWhen && c.When != "":
			return fmt.Errorf("more than one %s", w.when[0])
		}
		addClause(c, cl, strings.Split(body, " "+w.and+" "))
	}
	if (c.If != "") != then {
		return fmt.Errorf("%s without %s, or %s without %s",
			w.ifs[0], w.then[0], w.then[0], w.ifs[0])
	}
	return nil
}

// addClause adds to c a clause's parts, split on the capital AND: each a
// feature or precondition for WHERE and WHILE; for WHEN and IF, the
// trigger, then preconditions ("WHEN t AND p").
func addClause(c *Components, cl clause, parts []string) {
	switch cl {
	case clauseWhere:
		c.Where = append(c.Where, parts...)
		return
	case clauseWhile:
		c.While = append(c.While, parts...)
		return
	case clauseWhen:
		c.When = parts[0]
	case clauseIf:
		c.If = parts[0]
	}
	c.While = append(c.While, parts[1:]...)
}

// splitClauses cuts prefix before each comma that a keyword of w follows,
// whitespace between them ignored ("x ,  WHEN y"): the one place that
// knows what separates two clauses, for Parse and for Validate
// (checkClause).
func splitClauses(w *wording, prefix string) []string {
	var out []string
	start := 0
	for i := range prefix {
		if prefix[i] != ',' {
			continue
		}
		rest := prefix[i+1:]
		next := strings.TrimLeftFunc(rest, unicode.IsSpace)
		if len(next) == len(rest) {
			continue
		}
		if _, _, ok := openingClause(w, next); ok {
			out = append(out, prefix[start:i])
			start = len(prefix) - len(next)
		}
	}
	return append(out, prefix[start:])
}

// openingClause returns the clause whose keyword opens text, followed by a
// space, and the text after it.
func openingClause(w *wording, text string) (clause, string, bool) {
	for cl := clauseWhere; cl <= clauseIf; cl++ {
		for _, kw := range w.keywords(cl) {
			if n := foldedWordPrefix(text, kw); n > 0 && n < len(text) && text[n] == ' ' {
				return cl, strings.TrimSpace(text[n:]), true
			}
		}
	}
	return 0, "", false
}
