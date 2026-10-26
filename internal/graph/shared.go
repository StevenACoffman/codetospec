package graph

import (
	"slices"
	"strings"
)

// SharedEntity is an entity that rules of several domains touch.
type SharedEntity struct {
	ID, Title string
	Domains   []string // domain slugs, sorted
}

// SharedEntities lists the entities touched by rules of two or more
// domains: one concept several domains read or write is either a shared
// kernel to name as such, or a sign a domain boundary runs through it.
// Rule→entity links come from the reduce step, so a link it missed
// hides an entity here; nothing is inferred beyond them.
//
// Ensures: entities sorted by id, each with its domains sorted; an entity
// touched from one domain only is absent.
func SharedEntities(nodes []Node) []SharedEntity {
	titles := map[string]string{}
	domains := map[string][]string{} // entity id → domain slugs
	for i := range nodes {
		n := &nodes[i]
		if n.Type == TypeEntity {
			titles[n.ID] = n.Title
		}
		if n.Type != TypeRule {
			continue
		}
		domain := ""
		for _, e := range n.Edges {
			if e.Type == EdgeBelongsTo {
				domain = strings.TrimPrefix(e.To, "domain.")
			}
		}
		for _, e := range n.Edges {
			if e.Type == EdgeTouches && domain != "" {
				domains[e.To] = append(domains[e.To], domain)
			}
		}
	}
	var shared []SharedEntity
	for id, list := range domains {
		slices.Sort(list)
		list = slices.Compact(list)
		if title, isEntity := titles[id]; isEntity && len(list) > 1 {
			shared = append(shared, SharedEntity{ID: id, Title: title, Domains: list})
		}
	}
	slices.SortFunc(shared, func(a, b SharedEntity) int { return strings.Compare(a.ID, b.ID) })
	return shared
}
