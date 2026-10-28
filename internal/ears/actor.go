package ears

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// ValidateActor returns why system cannot name an actor of a requirement
// ("the scheduler"), or nil: for a glossary of the actors a model may
// name, checked before any model is called.
func ValidateActor(system string) error {
	if strings.TrimSpace(system) == "" {
		return errors.New("empty actor")
	}
	return checkActor(system)
}

// actor is a System as a sentence writes it after its conditions: the
// article lowercase ("THE Issue_Manager" → "the Issue_Manager"), the name
// as written, "" for the default actor of any language.
func actor(system string) string {
	system = lowerArticle(trimPart(system))
	for _, w := range wordings() {
		if fold(system) == fold(w.system) {
			return ""
		}
	}
	return system
}

// ActorName is system without its article, as a glossary names it
// ("the scheduler" → "scheduler", "l'application" → "application").
func ActorName(system string) string {
	for _, w := range wordings() {
		for _, art := range w.articles {
			elided, isElided := strings.CutSuffix(art, "'")
			n := foldedWordPrefix(system, elided)
			if n == 0 {
				continue
			}
			rest := system[n:]
			if isElided {
				_, size := utf8.DecodeRuneInString(rest)
				return rest[size:]
			}
			return strings.TrimSpace(rest)
		}
	}
	return system
}

// lowerArticle is s with its opening article, if any, in lowercase.
func lowerArticle(s string) string {
	for _, w := range wordings() {
		for _, art := range w.articles {
			if n := foldedWordPrefix(s, strings.TrimSuffix(art, "'")); n > 0 {
				return strings.ToLower(s[:n]) + s[n:]
			}
		}
	}
	return s
}

// upperArticle is s with its opening article in capitals, as Kiro writes
// an actor ("THE Issue_Manager").
func upperArticle(s string) string {
	for _, w := range wordings() {
		for _, art := range w.articles {
			if n := foldedWordPrefix(s, strings.TrimSuffix(art, "'")); n > 0 {
				return strings.ToUpper(s[:n]) + s[n:]
			}
		}
	}
	return s
}

// checkActor returns why a System cannot open the sentence's subject:
// it must name the actor with its article, lowercase, and hold no comma
// (Parse finds the actor after the last one) nor modal.
func checkActor(system string) error {
	if system == "" {
		return nil
	}
	if err := checkPart(system, false); err != nil {
		return err
	}
	switch {
	case strings.Contains(system, ","):
		return errors.New(`holds ","; name the actor alone`)
	case !startsWithArticle(system):
		return errors.New(`opens with no article ("the software")`)
	case system != actor(system):
		return errors.New(`write its article lowercase, or leave it out for "the system"`)
	}
	for _, w := range wordings() {
		if w.negation != "" && foldedWordSuffix(system, w.negation) > 0 {
			return fmt.Errorf("ends with %q, which the sentence adds", w.negation)
		}
		if lastWord(system+" ", w.then) >= 0 {
			return fmt.Errorf("holds %q, which closes the conditions", w.then[0])
		}
		if _, _, ok := openingClause(&w, system); ok {
			return errors.New("opens like a condition keyword")
		}
	}
	return nil
}

// startsWithArticle reports whether s opens with an article of any
// language followed by the actor's name.
func startsWithArticle(s string) bool {
	for _, w := range wordings() {
		for _, art := range w.articles {
			if elided, ok := strings.CutSuffix(art, "'"); ok {
				n := foldedWordPrefix(s, elided)
				apostrophe, size := utf8.DecodeRuneInString(s[min(n, len(s)):])
				if n > 0 && strings.ContainsRune("'’", apostrophe) && len(s) > n+size {
					return true
				}
				continue
			}
			if n := foldedWordPrefix(s, art); n > 0 && n+1 < len(s) && s[n] == ' ' {
				return true
			}
		}
	}
	return false
}
