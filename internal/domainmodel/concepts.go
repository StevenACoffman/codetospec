package domainmodel

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/StevenACoffman/codetospec/internal/concept"
)

// Concepts are the entities of the confirmed models as concept assignment
// reads them (concept.Assign): each with the code entity report matched to
// it, its table, and the entity that owns it, from the models' owned
// relationships (an owner names the entity it owns; a name without a
// scope is in the owner's model).
//
// Requires: report = Compare(set, …).
// Ensures:  sorted by name; an entity owned by several keeps the first
// owner by name, so the result does not depend on model order.
func Concepts(set *Set, report *Report) []concept.Concept {
	if set == nil {
		return nil
	}
	owner := owners(set)
	concepts := make([]concept.Concept, 0, len(report.Entities))
	for _, e := range report.Entities {
		concepts = append(concepts, concept.Concept{
			Name:   e.Name,
			Entity: e.Code.ID,
			Table:  e.Code.Title,
			Owner:  owner[e.Name],
		})
	}
	slices.SortFunc(
		concepts,
		func(a, b concept.Concept) int { return strings.Compare(a.Name, b.Name) },
	)
	return concepts
}

// owners maps each owned entity to the entity owning it, both qualified;
// an entity owned by several keeps the first owner by name.
func owners(set *Set) map[string]string {
	owner := map[string]string{}
	for _, m := range set.Models {
		for _, name := range m.Model.EntityNames() {
			for _, rel := range m.Model.Entities[name].Relationships {
				if rel.Ownership != ownershipOwned {
					continue
				}
				owned := rel.Entity
				if !strings.Contains(owned, ".") {
					owned = m.Scope + "." + owned
				}
				if prior, seen := owner[owned]; !seen || m.Scope+"."+name < prior {
					owner[owned] = m.Scope + "." + name
				}
			}
		}
	}
	return owner
}

// CheckOwned refuses an actor's owned concept that the models do not
// declare: a scope that is no model's, or an entity its model lacks (see
// config.Actors): a typo there would silently leave rules to "the system".
//
// Ensures: nil when owners is empty; the first unknown entry by name
// otherwise.
func CheckOwned(set *Set, owners map[string]string) error {
	known := map[string]bool{}
	if set != nil {
		for _, m := range set.Models {
			known[m.Scope] = true
			for _, name := range m.Model.EntityNames() {
				known[m.Scope+"."+name] = true
			}
		}
	}
	for _, owned := range slices.Sorted(maps.Keys(owners)) {
		if !known[owned] {
			return fmt.Errorf(
				"actors: %q owns %q, which no --domain-model declares",
				owners[owned],
				owned,
			)
		}
	}
	return nil
}
