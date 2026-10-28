package ears

import (
	"cmp"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Components are an EARS requirement split into its components, after
// the generic syntax [Trigger] [Precondition] Actor Action [Object]: what
// the map and reduce models write instead of a sentence, so the sentence
// (Render) and the kind (KindOf) are derived rather than guessed back
// from keywords.
//
// Preconditions are While, written before the trigger ("WHILE p, WHEN
// t") or after it ("WHEN t AND p"): the same component. When and If may
// both be set — a complex requirement with two triggers ("WHEN the button
// is pressed, IF the gear does not lock, THEN …"). Action and object stay
// one response's Text, with Action its opening words: a sentence cannot
// say where the verb ends (adverbs, phrasal verbs, negation), so the
// boundary is kept beside it (Response, WithActions).
type Components struct {
	Where []string `json:"where,omitempty"` // features, 0..n
	While []string `json:"while,omitempty"` // preconditions, 0..n
	When  string   `json:"when,omitempty"`  // triggering event, 0..1
	If    string   `json:"if,omitempty"`    // unwanted condition, 0..1
	// System is the actor, with its article ("the software"); "" for the
	// language's "the system", the repository codetospec reads.
	System string     `json:"system,omitempty"`
	Shall  []Response `json:"shall"` // responses, 1..n
}

// wording is how one output language writes EARS. Render writes each
// keyword's first form; Parse, Normalize and Check match every form,
// folded (case and accents ignored).
type wording struct {
	lang                          string // --lang code
	where, while, when, ifs, then []string
	system                        string // the default actor, lowercase
	modal                         string
	// and joins several features or preconditions in one clause, and
	// preconditions to a trigger ("WHEN t AND p"). Matched in capitals
	// only: a lowercase "and" is prose ("signs up and confirms").
	and string
	// articles open an actor ("the software"); a form ending in an
	// apostrophe is elided ("l'application").
	articles []string
	// negation is French "ne": a response "ne pas x" is written
	// "le système ne doit pas x", the negation before the modal.
	negation string
}

// wordings are the languages EARS is written in, read in any of them
// since a graph is read back whatever --lang wrote it.
func wordings() []wording {
	return []wording{
		{
			lang:  "fr",
			where: []string{"LÀ OÙ"}, while: []string{"TANT QUE"},
			when: []string{"QUAND", "LORSQUE"}, ifs: []string{"SI"}, then: []string{"ALORS"},
			system: "le système", modal: "doit", negation: "ne",
			and: "ET", articles: []string{"le", "la", "les", "l'"},
		},
		{
			lang:  "en",
			where: []string{"WHERE"}, while: []string{"WHILE"},
			when: []string{"WHEN"}, ifs: []string{"IF"}, then: []string{"THEN"},
			system: "the system", modal: "shall",
			and: "AND", articles: []string{"the"},
		},
	}
}

// wordingFor is the wording of a --lang code; French for any other code,
// as locale.For.
func wordingFor(lang string) wording {
	all := wordings()
	for _, w := range all {
		if w.lang == lang {
			return w
		}
	}
	return all[0]
}

// clause is one condition component, in canonical order.
type clause int

const (
	clauseWhere clause = iota
	clauseWhile
	clauseWhen
	clauseIf
)

// kind is the simple EARS pattern a clause makes.
func (c clause) kind() string {
	switch c {
	case clauseWhere:
		return Optional
	case clauseWhile:
		return State
	case clauseWhen:
		return Event
	case clauseIf:
		return Unwanted
	}
	return ""
}

// keywords are the forms of a clause's keyword in w.
func (w *wording) keywords(c clause) []string {
	switch c {
	case clauseWhere:
		return w.where
	case clauseWhile:
		return w.while
	case clauseWhen:
		return w.when
	case clauseIf:
		return w.ifs
	}
	return nil
}

// Patterns are the simple EARS patterns c combines, in clause order
// (optional, state, event, unwanted); none for a ubiquitous requirement.
func Patterns(c *Components) []string {
	present := []bool{len(c.Where) > 0, len(c.While) > 0, c.When != "", c.If != ""}
	var out []string
	for cl := clauseWhere; cl <= clauseIf; cl++ {
		if present[cl] {
			out = append(out, cl.kind())
		}
	}
	return out
}

// KindOf is the EARS kind c is written in: ubiquitous with no keyword,
// the pattern of its one keyword, complex with more.
func KindOf(c *Components) string {
	switch patterns := Patterns(c); len(patterns) {
	case 0:
		return Ubiquitous
	case 1:
		return patterns[0]
	default:
		return Complex
	}
}

// Normalized returns c with the slips models make repaired mechanically,
// cheaper than a correction round: spaces and a trailing "." or "," around
// each part, a part's own keyword opening it ("WHEN x" as when), THEN
// closing the IF, "the system shall" opening a response. Empty parts are
// kept, for Validate to name.
func (c *Components) Normalized() Components {
	all := wordings()
	strip := func(part string, cl clause) string {
		part = trimPart(part)
		for i := range all {
			for _, kw := range all[i].keywords(cl) {
				if n := foldedWordPrefix(part, kw); n > 0 {
					part = trimPart(part[n:])
				}
			}
		}
		return part
	}
	out := Components{When: strip(c.When, clauseWhen), If: strip(c.If, clauseIf)}
	for _, p := range c.Where {
		out.Where = append(out.Where, strip(p, clauseWhere))
	}
	for _, p := range c.While {
		out.While = append(out.While, strip(p, clauseWhile))
	}
	for i := range all {
		for _, kw := range all[i].then {
			if n := foldedWordSuffix(out.If, kw); n > 0 {
				out.If = trimPart(out.If[:len(out.If)-n])
			}
		}
	}
	for _, r := range c.Shall {
		out.Shall = append(out.Shall, Response{Text: response(r.Text), Action: response(r.Action)})
	}
	out.System = actor(c.System)
	return out
}

// trimPart trims spaces and the punctuation a part does not end with in a
// sentence.
func trimPart(part string) string {
	return strings.TrimRight(strings.TrimSpace(part), " \t\n.,")
}

// Validate returns why c cannot be written as one EARS sentence that
// Parse reads back to c, or nil. Errors name the component ("ears.when:
// …") for the correction sent to the model.
func Validate(c *Components) error {
	if len(c.Shall) == 0 {
		return errors.New("ears.shall: at least one response is required")
	}
	if err := checkActor(c.System); err != nil {
		return fmt.Errorf("ears.system: %w", err)
	}
	parts := []struct {
		name   string
		values []string
		clause bool
	}{
		{"where", c.Where, true},
		{"while", c.While, true},
		{"when", optional(c.When), true},
		{"if", optional(c.If), true},
	}
	for _, p := range parts {
		for _, v := range p.values {
			if err := checkPart(v, p.clause); err != nil {
				return fmt.Errorf("ears.%s: %w", p.name, err)
			}
		}
	}
	for i, r := range c.Shall {
		if err := checkResponse(r); err != nil {
			return fmt.Errorf("ears.shall[%d]: %w", i, err)
		}
	}
	return nil
}

// optional is a 0..1 component as the parts it holds.
func optional(part string) []string {
	if part == "" {
		return nil
	}
	return []string{part}
}

// checkPart returns why part cannot be written in a sentence; a clause
// (a condition, not a response) must also not read as two clauses or end
// on THEN.
func checkPart(part string, isClause bool) error {
	switch {
	case strings.TrimSpace(part) == "":
		return errors.New("empty part")
	case part != strings.TrimSpace(part):
		return errors.New("spaces around the part")
	}
	if strings.Contains(part, ";") {
		return errors.New(`";" separates responses; split the part instead`)
	}
	all := wordings()
	for _, word := range words(part) {
		for i := range all {
			if word == all[i].modal {
				return fmt.Errorf("%q belongs to the sentence, not to a part", all[i].modal)
			}
		}
	}
	if !isClause {
		return nil
	}
	return checkClause(part)
}

// checkClause returns why a condition part would not read back as one
// clause: it holds a second clause (splitClauses) or a capital AND, or
// ends on THEN.
func checkClause(part string) error {
	all := wordings()
	for i := range all {
		if strings.Contains(part, " "+all[i].and+" ") {
			return fmt.Errorf("holds %q, which joins components; give each its own", all[i].and)
		}
		if clauses := splitClauses(&all[i], part); len(clauses) > 1 {
			return fmt.Errorf("holds a second clause (%q); give it its own component",
				strings.TrimSpace(clauses[1]))
		}
		for _, kw := range all[i].then {
			if foldedWordSuffix(part, kw) > 0 {
				return fmt.Errorf("ends with %q, which the sentence adds", kw)
			}
		}
	}
	return nil
}

// Template is the sentence of every component, with placeholders: how
// the prompts show the model what its components become.
func Template(lang string) string {
	return Render(&Components{
		Where: []string{"<where>"},
		While: []string{"<while>", "<while>"},
		When:  "<when>",
		If:    "<if>",
		Shall: []Response{{Text: "<shall>"}, {Text: "<shall>"}},
	}, lang)
}

// Render writes c as one EARS sentence in lang, its clauses in canonical
// order: WHERE, WHILE, WHEN, IF … THEN, several features or
// preconditions joined by a capital AND; then the actor and its
// responses joined by "; ".
//
// Requires: Validate(c) == nil, for Parse to read the sentence back.
func Render(c *Components, lang string) string {
	return render(c, lang, false)
}

// RenderCapitals is Render in Kiro's style: the actor's article and the
// modal in capitals too ("WHEN x, THE SYSTEM SHALL y", "THE Issue_Manager
// SHALL"). Parse reads both.
func RenderCapitals(c *Components, lang string) string {
	return render(c, lang, true)
}

// InCapitals is sentence rewritten in Kiro's style (RenderCapitals), in
// its own language; an error when it does not parse, for the caller to
// keep it as written.
func InCapitals(sentence string) (string, error) {
	c, err := Parse(sentence)
	if err != nil {
		return "", err
	}
	w, _, err := findModal(strings.TrimSuffix(strings.TrimSpace(sentence), "."))
	if err != nil {
		return "", err
	}
	return RenderCapitals(&c, w.lang), nil
}

func render(c *Components, lang string, capitals bool) string {
	w := wordingFor(lang)
	var clauses []string
	add := func(cl clause, parts ...string) {
		if len(parts) > 0 {
			joined := strings.Join(parts, " "+w.and+" ")
			clauses = append(clauses, w.keywords(cl)[0]+" "+joined)
		}
	}
	add(clauseWhere, c.Where...)
	add(clauseWhile, c.While...)
	add(clauseWhen, optional(c.When)...)
	add(clauseIf, optional(c.If)...)

	system, modal, negation := cmp.Or(c.System, w.system), w.modal, w.negation
	if capitals {
		system, modal, negation = upperArticle(
			system,
		), strings.ToUpper(
			modal,
		), strings.ToUpper(
			negation,
		)
		if c.System == "" {
			system = strings.ToUpper(w.system)
		}
	}
	responses := strings.Join(texts(c.Shall), "; ")
	subject := system + " " + modal + " " + responses
	// "ne  x" stays as written: Parse trims the spaces after the modal.
	rest, ok := strings.CutPrefix(responses, w.negation+" ")
	if ok && w.negation != "" && rest == strings.TrimLeftFunc(rest, unicode.IsSpace) {
		subject = system + " " + negation + " " + modal + " " + rest
	}
	if len(clauses) == 0 {
		return capitalize(subject) + "."
	}
	if c.If != "" {
		subject = w.then[0] + " " + subject
	}
	return strings.Join(clauses, ", ") + ", " + subject + "."
}

func capitalize(s string) string {
	r, size := utf8.DecodeRuneInString(s)
	return string(unicode.ToUpper(r)) + s[size:]
}

// foldedWordPrefix returns how many bytes of s the word or words kw open
// it with, case and accents ignored and not followed by a letter; 0 when
// kw does not open s.
func foldedWordPrefix(s, kw string) int {
	n := 0
	for _, r := range fold(kw) {
		got, size := utf8.DecodeRuneInString(s[n:])
		if size == 0 || foldRune(got) != r {
			return 0
		}
		n += size
	}
	if next, _ := utf8.DecodeRuneInString(s[n:]); n < len(s) && unicode.IsLetter(next) {
		return 0
	}
	return n
}

// foldedWordSuffix is foldedWordPrefix at the end of s: how many bytes kw
// closes s with, not preceded by a letter.
func foldedWordSuffix(s, kw string) int {
	want := []rune(fold(kw))
	n := len(s)
	for i := len(want) - 1; i >= 0; i-- {
		got, size := utf8.DecodeLastRuneInString(s[:n])
		if size == 0 || foldRune(got) != want[i] {
			return 0
		}
		n -= size
	}
	if n > 0 && isLetterAt(s, n-1) {
		return 0
	}
	return len(s) - n
}

// words are the folded words (letter runs) of s.
func words(s string) []string {
	return strings.FieldsFunc(fold(s), func(r rune) bool { return !unicode.IsLetter(r) })
}
