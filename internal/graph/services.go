package graph

import (
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"

	"codetospec/internal/extract"
	"codetospec/internal/locale"
)

// TypeService is the Node.Type of an external service the code calls.
const TypeService = "service"

// EdgeDependsOn links a domain to a domain or service it calls.
const EdgeDependsOn = "depends_on"

// serviceCall is one call site of an operation owned by another service.
type serviceCall struct {
	operation string
	at        extract.Ref
	as        string // extract.AttrAs; "" when unknown
}

// serviceNodes builds one node per external service named by the
// extract.KindDependency facts, listing the operations called and where,
// and returns, per calling domain, the depends_on edges to those services.
// domainOf resolves a call site's domain.
func serviceNodes(
	facts []extract.Fact,
	domainOf func(path string) string,
	text *locale.Text,
) ([]Node, map[string][]Edge) {
	calls := make(map[string][]serviceCall)
	callers := make(map[string]map[string]bool) // domain → services
	for _, f := range facts {
		service := f.Attrs[extract.AttrService]
		if f.Kind != extract.KindDependency || service == "" ||
			extract.Slugify(service) != service {
			continue
		}
		calls[service] = append(
			calls[service],
			serviceCall{f.Attrs[extract.AttrOperation], f.Source, f.Attrs[extract.AttrAs]},
		)
		domain := domainOf(f.Source.Path)
		if callers[domain] == nil {
			callers[domain] = make(map[string]bool)
		}
		callers[domain][service] = true
	}

	var nodes []Node
	for _, service := range slices.Sorted(maps.Keys(calls)) {
		sites := calls[service]
		sort.Slice(sites, func(i, j int) bool {
			if sites[i].operation != sites[j].operation {
				return sites[i].operation < sites[j].operation
			}
			if sites[i].at.Path != sites[j].at.Path {
				return sites[i].at.Path < sites[j].at.Path
			}
			return sites[i].at.Lines < sites[j].at.Lines
		})
		sources := make([]extract.Ref, 0, len(sites))
		for _, c := range sites {
			sources = append(sources, c.at)
		}
		nodes = append(nodes, Node{
			ID:      TypeService + "." + service,
			Type:    TypeService,
			Status:  StatusGenerated,
			Sources: sources,
			Title:   fmt.Sprintf(text.ServiceTitleFmt, service),
			Body:    serviceBody(sites, text),
		})
	}

	edges := make(map[string][]Edge)
	for domain, services := range callers {
		for _, service := range slices.Sorted(maps.Keys(services)) {
			edges[domain] = append(
				edges[domain],
				Edge{Type: EdgeDependsOn, To: TypeService + "." + service},
			)
		}
	}
	return nodes, edges
}

// serviceBody lists a service's call sites, sorted, marking those made as
// a service-admin query.
func serviceBody(sites []serviceCall, text *locale.Text) string {
	var body strings.Builder
	body.WriteString(text.OperationsCalled + "\n")
	for _, c := range sites {
		fmt.Fprintf(&body, "\n- `%s` — `%s:%s`", c.operation, c.at.Path, c.at.Lines)
		if c.as == extract.AsServiceAdmin {
			body.WriteString(text.AsServiceAdminNote)
		}
	}
	return body.String()
}

// namespaceByPath maps each file to the first module (namespace/package)
// fact declared in it, for resolving a call site's domain.
func namespaceByPath(facts []extract.Fact) map[string]string {
	byPath := make(map[string]string)
	for _, f := range facts {
		if f.Kind == "module" && byPath[f.Source.Path] == "" {
			byPath[f.Source.Path] = f.Attrs["name"]
		}
	}
	return byPath
}
