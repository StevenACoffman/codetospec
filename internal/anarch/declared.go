package anarch

import (
	"encoding/json"
	"fmt"
)

// Declared returns the names of every fitness rule an anarch-lint model
// (anarch.json) declares, whatever its kind and gate: a proposal named like
// one was decided already, accepted or rejected. A rule list is an array of
// rules; the onion rule is a single object. Rules without a name are
// skipped: anarch-lint names them by position, which no proposal uses.
func Declared(model []byte) (map[string]bool, error) {
	var m struct {
		Fitness map[string]json.RawMessage `json:"fitness"`
	}
	if err := json.Unmarshal(model, &m); err != nil {
		return nil, fmt.Errorf("anarch model: %w", err)
	}
	type named struct {
		Name string `json:"name"`
	}
	out := map[string]bool{}
	for _, raw := range m.Fitness {
		var rules []named
		if err := json.Unmarshal(raw, &rules); err != nil {
			var one named
			if json.Unmarshal(raw, &one) != nil {
				continue // a setting such as requireAssigned, not a rule
			}
			rules = []named{one}
		}
		for _, r := range rules {
			if r.Name != "" {
				out[r.Name] = true
			}
		}
	}
	return out, nil
}
