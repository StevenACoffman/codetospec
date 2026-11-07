package policy

import (
	"cmp"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// Registry maps each property and relation a person declares (the PIP's
// registry) to what it is built from. A person writes it; codetospec never
// does. An entry's From lists:
//   - fields, "models.ClassroomDistrictInfo.AllTeacherUDIsIDs";
//   - loads, "load UserMetaInfo" (an existence property: a kind queried);
//   - methods, "models.Classroom.HasTeacher()" (a method the check calls);
//
// and its Condition, free text the inventory shows but never evaluates
// ("when !IsGlobalAdmin", "minus grantees"). "Declared" means the same
// source, not the same meaning.
type Registry struct {
	entries map[string]RegistryEntry
}

// RegistryEntry is one property, relation or attribute of a Registry:
// what it is built from, and how a person reads it (Phrase, by language,
// for the business statements; Concept, the domain concept it belongs
// to).
type RegistryEntry struct {
	From      []string          `json:"from"`
	Condition string            `json:"condition,omitempty"`
	Phrase    map[string]string `json:"phrase,omitempty"`
	Concept   string            `json:"concept,omitempty"`
	Note      string            `json:"note,omitempty"`
}

// loadPrefix marks a registry source that is a datastore kind,
// methodSuffix one that is a method.
const (
	loadPrefix   = "load "
	methodSuffix = "()"
)

// ParseRegistry reads a registry: a map from entry name to entry, JSON or
// the same document in YAML, checked against registry.schema.json (an
// entry with no source could declare nothing, so from is required).
func ParseRegistry(data []byte) (Registry, error) {
	normalized, err := validated(SchemaRegistry, data)
	if err != nil {
		return Registry{}, fmt.Errorf("policy: registry: %w", err)
	}
	var entries map[string]RegistryEntry
	if err = json.Unmarshal(normalized, &entries); err != nil {
		return Registry{}, fmt.Errorf("policy: registry: %w", err)
	}
	return Registry{entries: entries}, nil
}

// Phrase is how a person reads the entry name in lang ("en", "fr"): its
// phrase in that language, else in English, else the name itself.
func (r Registry) Phrase(name, lang string) string {
	phrase, _ := r.phrase(name, lang)
	return cmp.Or(phrase, name)
}

// phrase is the entry name's phrase in lang, else in English; false when
// it has neither.
func (r Registry) phrase(name, lang string) (string, bool) {
	phrases := r.entries[name].Phrase
	phrase := cmp.Or(phrases[lang], phrases["en"])
	return phrase, phrase != ""
}

// Concept is the domain concept the entry name belongs to; "" when none
// is given.
func (r Registry) Concept(name string) string {
	return r.entries[name].Concept
}

// declares names the entry declaring in, sorted first by name; "" when
// none does or in is not a field, load or method input.
func (r Registry) declares(in Input) string {
	var want string
	switch in.Kind {
	case KindField:
		want = in.Name
	case KindLoad:
		want = loadPrefix + in.Name
	case KindMethod:
		want = in.Name + methodSuffix
	default:
		return ""
	}
	for _, name := range r.names() {
		for _, src := range r.entries[name].From {
			if strings.TrimSpace(src) == want {
				return name
			}
		}
	}
	return ""
}

// names lists the entries' names, sorted.
func (r Registry) names() []string {
	return slices.Sorted(maps.Keys(r.entries))
}

// conditions maps each entry with a condition to it; nil when none has.
func (r Registry) conditions() map[string]string {
	var out map[string]string
	for name, e := range r.entries {
		if e.Condition == "" {
			continue
		}
		if out == nil {
			out = map[string]string{}
		}
		out[name] = e.Condition
	}
	return out
}
