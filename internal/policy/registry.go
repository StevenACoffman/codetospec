package policy

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// Registry maps each property and relation a person declares (the PIP's
// registry) to what it is built from. A person writes it; codetospec never
// does. An entry's From lists:
//   - fields, "models.ClassroomDistrictInfo.AllTeacherUDIsIDs";
//   - loads, "load UserMetaInfo" (an existence property: a kind queried);
//
// and its Condition, free text the inventory shows but never evaluates
// ("when !IsGlobalAdmin", "minus grantees"). "Declared" means the same
// source, not the same meaning.
type Registry struct {
	entries map[string]RegistryEntry
}

// RegistryEntry is one property or relation of a Registry.
type RegistryEntry struct {
	From      []string `yaml:"from"`
	Condition string   `yaml:"condition"`
}

// loadPrefix marks a registry source that is a datastore kind.
const loadPrefix = "load "

// ParseRegistry reads a registry: a YAML map from entry name to entry. An
// entry with no source is an error, since it could declare nothing.
func ParseRegistry(data []byte) (Registry, error) {
	var entries map[string]RegistryEntry
	if err := yaml.Unmarshal(data, &entries); err != nil {
		return Registry{}, fmt.Errorf("policy: registry: %w", err)
	}
	for _, name := range slices.Sorted(maps.Keys(entries)) {
		if len(entries[name].From) == 0 {
			return Registry{}, fmt.Errorf("policy: registry entry %q lists no source in from", name)
		}
	}
	return Registry{entries: entries}, nil
}

// declares names the entry declaring in, sorted first by name; "" when
// none does or in is not a field or load input.
func (r Registry) declares(in Input) string {
	var want string
	switch in.Kind {
	case KindField:
		want = in.Name
	case KindLoad:
		want = loadPrefix + in.Name
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
