// Package ears knows the EARS requirement patterns a rule is written in:
// the kinds a rule may declare, and whether its sentence has the form its
// kind names. The prompts give the model the patterns (locale.EARSPatterns);
// this package checks what comes back, in every output language, since a
// graph is read back whatever --lang wrote it.
package ears

import (
	"fmt"
	"strings"
	"unicode"
)

// The EARS kinds a rule declares (frontmatter `ears:`).
const (
	Ubiquitous = "ubiquitous" // The system shall <response>.
	Event      = "event"      // WHEN <trigger>, the system shall <response>.
	State      = "state"      // WHILE <state>, the system shall <response>.
	Unwanted   = "unwanted"   // IF <condition>, THEN the system shall <response>.
	Optional   = "optional"   // WHERE <feature>, the system shall <response>.
)

// IsKind reports whether kind is an EARS kind.
func IsKind(kind string) bool {
	switch kind {
	case Ubiquitous, Event, State, Unwanted, Optional:
		return true
	}
	return false
}

// Check returns why requirement does not have the EARS form its kind names,
// or "" when it does. French and English are both accepted, accents and
// case ignored. One modal ("shall" / "doit") per rule, since two are two
// behaviors; its subject is the system. Event, state and optional open with
// their keyword; ubiquitous opens on the system itself; unwanted holds
// IF … THEN, after an optional leading clause (WHEN x, IF y, THEN …: the
// complex form real rules use).
func Check(kind, requirement string) string {
	if !IsKind(kind) {
		return fmt.Sprintf("unknown EARS kind %q", kind)
	}
	words := strings.FieldsFunc(fold(requirement), func(r rune) bool {
		return !unicode.IsLetter(r)
	})
	switch modals := count(words, "shall") + count(words, "doit"); {
	case modals == 0:
		return `no "shall": not an EARS requirement`
	case modals > 1:
		return `more than one "shall": one behavior per rule`
	}
	text := strings.Join(words, " ")
	if !containsAny(text, "the system shall", "le systeme doit", "le systeme ne doit") {
		return `"shall" without "the system" as its subject`
	}
	if !opensAs(kind, text) {
		return kind + " requirement does not open with " + opening(kind)
	}
	return ""
}

// opening names, for messages, how kind opens.
func opening(kind string) string {
	switch kind {
	case Ubiquitous:
		return `"The system shall"`
	case Event:
		return "WHEN"
	case State:
		return "WHILE"
	case Optional:
		return "WHERE"
	default: // Unwanted
		return "IF … THEN"
	}
}

// opensAs reports whether text, folded to words, opens the way kind does.
func opensAs(kind, text string) bool {
	switch kind {
	case Ubiquitous:
		return hasAnyPrefix(text, "the system shall", "le systeme doit", "le systeme ne doit")
	case Event:
		return hasAnyPrefix(text, "when ", "quand ", "lorsque ")
	case State:
		return hasAnyPrefix(text, "while ", "tant que ")
	case Optional:
		return hasAnyPrefix(text, "where ", "la ou ")
	default: // Unwanted
		return hasCondition(text, "if", "then") || hasCondition(text, "si", "alors")
	}
}

// hasCondition reports whether text holds the word cond and, after it, the
// word then, opening the text or a clause after a leading keyword.
func hasCondition(text, cond, then string) bool {
	at := strings.Index(" "+text+" ", " "+cond+" ")
	return at >= 0 && strings.Contains(text[at:]+" ", " "+then+" ")
}

// fold lowercases s and strips the French accents, so "LÀ OÙ" reads "la ou".
func fold(s string) string {
	return strings.Map(func(r rune) rune {
		switch r = unicode.ToLower(r); r {
		case 'à', 'â', 'ä':
			return 'a'
		case 'é', 'è', 'ê', 'ë':
			return 'e'
		case 'î', 'ï':
			return 'i'
		case 'ô', 'ö':
			return 'o'
		case 'ù', 'û', 'ü':
			return 'u'
		case 'ç':
			return 'c'
		}
		return r
	}, s)
}

func count(words []string, word string) int {
	n := 0
	for _, w := range words {
		if w == word {
			n++
		}
	}
	return n
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func hasAnyPrefix(s string, prefixes ...string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}
