// Package vocabulary lists the variable names a rule condition may use, so
// that the map's conditions and the extractors' name the same check the
// same way and hash equal (see package condition). It reads, in order of
// preference: the variables of the extractors' rule conditions, the schema
// facts (tables and their columns, enumerations, struct fields), and the human domain
// models (entities and their attributes, enumerations). Each chunk is
// offered only the terms that appear in it; a variable outside them is
// coined under Local. No LLM, no I/O.
package vocabulary

import (
	"cmp"
	"maps"
	"slices"
	"strings"
	"unicode"

	"github.com/StevenACoffman/codetospec/internal/condition"
	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/indomain/pkg/model"
)

// Local prefixes a variable the model coins because no term of the
// vocabulary names it.
const Local = "local."

// MaxTerms bounds the terms offered for one chunk: enough for the checks
// one chunk makes, few enough to keep the prompt short.
const MaxTerms = 60

// Source is where a term comes from; the order of preference is the order
// of the constants.
type Source int

// The sources of terms, most preferred first.
const (
	FromRule   Source = iota // a variable of an extractor rule's condition
	FromSchema               // a table column, an enumeration or a struct field of the code
	FromModel                // an attribute or enumeration of a domain model
)

// Term is one variable name a condition may use.
type Term struct {
	Name   string
	Source Source
	// Values lists an enumeration's values, the literals it compares with.
	Values []string
	// file scopes a rule variable to the file of its rule: offered to every
	// chunk of that file. "" for the other terms, matched by name.
	file string
	// key is the normalized last part of Name, matched against the words
	// of a chunk.
	key string
	// owners gate a struct field: one of them must be a word of the chunk
	// too, or every struct's Status would be offered wherever "status"
	// occurs. They are the struct's name and its last camelCase part, as a
	// variable usually names its type (job for RosterJob). Empty for the
	// other terms.
	owners []string
}

// Vocabulary is every term of a run, sorted by source then name.
type Vocabulary struct {
	terms []Term
}

// Build gathers the terms of facts and models.
//
// Requires: facts passed graph.ValidateRuleFacts (a rule fact's condition
// parses); models may be nil.
// Ensures:  each name once, at its most preferred source; a term whose
// name has no word character is left out.
func Build(facts []extract.Fact, models []*model.Model) Vocabulary {
	byName := make(map[string]Term)
	add := func(t Term) {
		t.key = normalize(lastPart(t.Name))
		if t.key == "" {
			return
		}
		if old, seen := byName[t.Name]; seen && old.Source <= t.Source {
			return
		}
		byName[t.Name] = t
	}
	for i := range facts {
		for _, t := range factTerms(facts, i) {
			add(t)
		}
	}
	for _, m := range models {
		for _, t := range modelTerms(m) {
			add(t)
		}
	}
	terms := slices.Collect(maps.Values(byName))
	slices.SortFunc(terms, func(a, b Term) int {
		return cmp.Or(cmp.Compare(a.Source, b.Source), cmp.Compare(a.Name, b.Name))
	})
	return Vocabulary{terms: terms}
}

// factTerms are the terms fact i of facts gives: a rule fact's condition
// variables, a table's columns, an enumeration with its values.
func factTerms(facts []extract.Fact, i int) []Term {
	f := &facts[i]
	switch f.Kind {
	case extract.KindRule:
		c, err := condition.Parse([]byte(f.Attrs[extract.AttrCondition]))
		if err != nil {
			return nil
		}
		terms := make([]Term, 0, len(c.Variables()))
		for _, name := range c.Variables() {
			terms = append(terms, Term{Name: name, Source: FromRule, file: f.Source.File()})
		}
		return terms
	case extract.KindTable:
		table := f.Attrs[extract.AttrName]
		if table == "" {
			table = strings.TrimPrefix(f.ID, "table.")
		}
		var terms []Term
		for column := range strings.SplitSeq(f.Attrs[extract.AttrColumns], ",") {
			name, _, _ := strings.Cut(strings.TrimSpace(column), ":")
			if name != "" {
				terms = append(terms, Term{Name: table + "." + name, Source: FromSchema})
			}
		}
		return terms
	case extract.KindEnum:
		return []Term{
			{Name: f.Attrs[extract.AttrName], Source: FromSchema, Values: enumValues(facts, f.ID)},
		}
	case extract.KindStruct:
		return structTerms(f)
	}
	return nil
}

// structTerms are a struct fact's fields, each gated by the struct's name.
func structTerms(f *extract.Fact) []Term {
	name := f.Attrs[extract.AttrName]
	parts := splitIdentifier(lastPart(name))
	owners := []string{normalize(lastPart(name))}
	if tail := normalize(parts[len(parts)-1]); tail != owners[0] {
		owners = append(owners, tail)
	}
	var terms []Term
	for field := range strings.SplitSeq(f.Attrs[extract.AttrFields], ",") {
		if field = strings.TrimSpace(field); field != "" {
			terms = append(
				terms,
				Term{Name: name + "." + field, Source: FromSchema, owners: owners},
			)
		}
	}
	return terms
}

// enumValues lists the values of the enumeration whose fact id is enum,
// in fact order.
func enumValues(facts []extract.Fact, enum string) []string {
	var values []string
	for i := range facts {
		if facts[i].Kind == extract.KindEnumValue && facts[i].Attrs[extract.AttrEnum] == enum {
			values = append(values, facts[i].Attrs[extract.AttrName])
		}
	}
	return values
}

// modelTerms are a domain model's entity attributes and enumerations.
func modelTerms(m *model.Model) []Term {
	var terms []Term
	for _, name := range slices.Sorted(maps.Keys(m.Entities)) {
		for _, a := range m.Entities[name].Attributes {
			terms = append(terms, Term{Name: name + "." + a.Name, Source: FromModel})
		}
	}
	for _, name := range slices.Sorted(maps.Keys(m.Enums)) {
		values := make([]string, 0, len(m.Enums[name].Values))
		for _, v := range m.Enums[name].Values {
			values = append(values, v.Name)
		}
		terms = append(terms, Term{Name: name, Source: FromModel, Values: values})
	}
	return terms
}

// For returns the terms offered to a chunk of file whose code is content:
// the rule variables of file, and every term whose last part occurs as a
// word of content (case, _ and - ignored; camelCase split) — for a struct
// field, with its struct's name or that name's last part —, in vocabulary
// order, at most MaxTerms.
func (v *Vocabulary) For(file, content string) []Term {
	words := wordsOf(content)
	var out []Term
	for _, t := range v.terms {
		if len(out) == MaxTerms {
			break
		}
		if t.file == file || (t.file == "" && words[t.key] && ownedIn(&t, words)) {
			out = append(out, t)
		}
	}
	return out
}

// ownedIn reports whether t has no owner, or one of its owners is one of
// words.
func ownedIn(t *Term, words map[string]bool) bool {
	return len(t.owners) == 0 ||
		slices.ContainsFunc(t.owners, func(o string) bool { return words[o] })
}

// Names lists the names of terms.
func Names(terms []Term) []string {
	names := make([]string, len(terms))
	for i := range terms {
		names[i] = terms[i].Name
	}
	return names
}

// Coined returns c with each variable that is neither one of names nor
// already under Local moved under Local, and how many were moved: a
// variable the vocabulary lacks is the model's own coinage, kept and
// marked rather than refused.
//
// Requires: c parsed.
// Ensures:  c is unchanged; the result parses (Local plus a valid name is
// valid).
func Coined(c *condition.Condition, names []string) (condition.Condition, int) {
	known := make(map[string]bool, len(names))
	for _, n := range names {
		known[n] = true
	}
	moved := 0
	renamed, err := c.Renamed(func(name string) string {
		if known[name] || strings.HasPrefix(name, Local) {
			return name
		}
		moved++
		return Local + name
	})
	if err != nil {
		return *c, 0
	}
	return renamed, moved
}

// lastPart is the part of a dotted name after its last dot.
func lastPart(name string) string {
	if i := strings.LastIndex(name, "."); i >= 0 {
		return name[i+1:]
	}
	return name
}

// normalize lowercases s and keeps its letters and digits only, so
// created_at, createdAt and CreatedAt read alike.
func normalize(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

// wordsOf is the set of normalized words of content: each identifier as a
// whole and each of its camelCase or snake_case parts.
func wordsOf(content string) map[string]bool {
	words := make(map[string]bool)
	identifiers := strings.FieldsFunc(content, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_'
	})
	for _, id := range identifiers {
		words[normalize(id)] = true
		for _, part := range splitIdentifier(id) {
			words[normalize(part)] = true
		}
	}
	delete(words, "")
	return words
}

// splitIdentifier splits an identifier at underscores and at each upper
// case letter that follows a lower case one or a digit.
func splitIdentifier(id string) []string {
	var parts []string
	start := 0
	runes := []rune(id)
	for i := 1; i < len(runes); i++ {
		boundary := runes[i] == '_' ||
			(unicode.IsUpper(runes[i]) && !unicode.IsUpper(runes[i-1]) && runes[i-1] != '_')
		if boundary {
			parts = append(parts, string(runes[start:i]))
			start = i
		}
	}
	return append(parts, string(runes[start:]))
}
