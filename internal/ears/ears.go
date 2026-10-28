// Package ears knows the EARS requirement patterns a rule is written in:
// the kinds a rule may declare, the components the models write
// (Components), the sentence and kind they make (Render, KindOf), how a
// sentence reads back into components (Parse), and whether a sentence has
// the form its kind names (Check), in every output language, since a graph
// is read back whatever --lang wrote it. The keywords of every language
// live here only (wordings). Render and RenderCapitals (Kiro's style)
// write a sentence; a response's action is not in it, so callers keep it
// beside the sentence (Actions, WithActions).
//
// The map and reduce models write components, so their sentences pass
// Check by construction (FuzzRenderParse); Check remains for the sentences
// nobody wrote as components: extractor rule facts, inherited rules, rules
// a person edited (docs/verification/ears-components-2026-10-27.md).
//
// A requirement with two or more keywords is complex — several triggers
// (WHEN and IF), preconditions before or after the trigger, features —
// with any actor "the <system name>"
// (docs/verification/ears-generic-syntax-2026-10-27.md); a sentence from
// a graph written before that kind, declared as the kind it opens with
// (or unwanted, IF … THEN after another clause), still passes: it is
// correct EARS. A condition
// after "shall" is not flagged: on real graphs most are defining
// predicates ("shall consider X expired when …") or restrictions
// ("shall serve / only when …", the webapp extractor's own templates,
// whose rule facts a failing check would drop), and only about a quarter
// are a guard that belongs in a clause
// (docs/verification/ears-clauses-2026-10-27.md).
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
	// Complex combines two or more of the above: WHILE <state>, WHEN
	// <trigger>, the system shall <response>.
	Complex = "complex"
)

// IsKind reports whether kind is an EARS kind.
func IsKind(kind string) bool {
	switch kind {
	case Ubiquitous, Event, State, Unwanted, Optional, Complex:
		return true
	}
	return false
}

// Check returns why requirement does not have the EARS form its kind names,
// or "" when it does. French and English are both accepted, accents and
// case ignored. One modal ("shall" / "doit") per rule, since two are two
// behaviors; its subject is an actor with its article. Event, state and
// optional open with their keyword; ubiquitous opens on the actor; unwanted holds
// IF … THEN, after an optional leading clause (WHILE x, IF y, THEN …);
// complex reads as components (Parse) of two or more patterns. The actor
// is any "the <system name>" (actorSpan), not only "the system".
func Check(kind, requirement string) string {
	if !IsKind(kind) {
		return fmt.Sprintf("unknown EARS kind %q", kind)
	}
	w, at, err := findModal(strings.TrimSpace(requirement))
	if err != nil {
		return err.Error()
	}
	if !hasSubject(&w, strings.TrimSpace(requirement)[:at]) {
		return `"shall" without "the <system name>" as its subject`
	}
	if kind == Complex {
		components, parseErr := Parse(requirement)
		return checkComplex(&components, parseErr)
	}
	if !opensAs(kind, requirement) {
		return kind + " requirement does not open with " + opening(kind)
	}
	return ""
}

// hasSubject reports whether before, a sentence up to its modal, names
// an actor with an article where Parse looks for one (actorSpan), as
// Parse requires.
func hasSubject(w *wording, before string) bool {
	before, _ = subjectText(w, before)
	start, err := actorSpan(w, before)
	return err == nil && startsWithArticle(before[start:])
}

// checkComplex returns why a complex requirement, read as components (or
// not, parseErr), does not combine two or more patterns.
func checkComplex(components *Components, parseErr error) string {
	switch {
	case parseErr != nil:
		return "complex requirement: " + parseErr.Error()
	case len(Patterns(components)) <= 1:
		return "complex requirement with fewer than two EARS keywords"
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

// opensAs reports whether requirement opens the way kind does.
func opensAs(kind, requirement string) bool {
	if kind == Ubiquitous {
		return startsWithArticle(strings.TrimSpace(requirement))
	}
	text := strings.Join(words(requirement), " ")
	for _, w := range wordings() {
		switch kind {
		case Event:
			if hasAnyPrefix(text, spaced(w.when)...) {
				return true
			}
		case State:
			if hasAnyPrefix(text, spaced(w.while)...) {
				return true
			}
		case Optional:
			if hasAnyPrefix(text, spaced(w.where)...) {
				return true
			}
		default: // Unwanted
			if hasCondition(text, fold(w.ifs[0]), fold(w.then[0])) {
				return true
			}
		}
	}
	return false
}

// spaced are keywords folded, each followed by a space.
func spaced(keywords []string) []string {
	out := make([]string, len(keywords))
	for i, kw := range keywords {
		out[i] = fold(kw) + " "
	}
	return out
}

// hasCondition reports whether text holds the word cond and, after it, the
// word then, opening the text or a clause after a leading keyword.
func hasCondition(text, cond, then string) bool {
	at := strings.Index(" "+text+" ", " "+cond+" ")
	return at >= 0 && strings.Contains(text[at:]+" ", " "+then+" ")
}

// fold lowercases s and strips the French accents, so "LÀ OÙ" reads "la ou".
func fold(s string) string {
	return strings.Map(foldRune, s)
}

// foldRune is fold of one rune: a rune always folds to one rune, so a
// folded match is a match rune for rune.
func foldRune(r rune) rune {
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
}

func hasAnyPrefix(s string, prefixes ...string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}
