package main

import (
	"cmp"
	"sort"
	"strings"

	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/graph"
	"github.com/StevenACoffman/codetospec/internal/mapper"
)

// factIndex is what the LLM phases are allowed to cite or must know, per
// source path.
type factIndex struct {
	entities        []string
	allEndpoints    []string
	endpointsByPath map[string][]string
	symbols         map[string][]mapper.SymbolContext
	mechanical      map[string][]mapper.Note
	docs            map[string][]mapper.Note
}

// indexFacts gathers, from the merged facts, what the LLM phases may cite.
func indexFacts(facts []extract.Fact) factIndex {
	return factIndex{
		entities:        entityIDs(facts),
		allEndpoints:    allEndpointIDs(facts),
		endpointsByPath: endpointsByPath(facts),
		symbols:         preciseSymbols(facts),
		mechanical:      mechanicalRules(facts),
		docs:            docNotes(facts),
	}
}

// entityIDs lists the entity node ids the table facts give, sorted.
func entityIDs(facts []extract.Fact) []string {
	var ids []string
	for _, f := range facts {
		if f.Kind == "table" {
			ids = append(ids, graph.EntityID(&f))
		}
	}
	sort.Strings(ids)
	return dedup(ids)
}

// qualifiedSymbol is a symbol fact's file and names.
type qualifiedSymbol struct {
	path, fqn, name string
}

// qualifiedSymbols lists the symbol facts with their namespace-qualified
// name (PHP's Namespace\Name).
func qualifiedSymbols(facts []extract.Fact) []qualifiedSymbol {
	var symbols []qualifiedSymbol
	for _, f := range facts {
		if f.Kind != "symbol" {
			continue
		}
		name := f.Attrs["name"]
		fqn := name
		if ns := f.Attrs["namespace"]; ns != "" {
			fqn = ns + "\\" + name
		}
		symbols = append(symbols, qualifiedSymbol{path: f.Source.Path, fqn: fqn, name: name})
	}
	return symbols
}

// endpointsByPath maps each source file to the endpoint ids its code
// serves: the file holding a route's handler (handler_path), else the
// files declaring a symbol its controller or handler names. Ids are sorted
// and distinct (colliding routes share one).
func endpointsByPath(facts []extract.Fact) map[string][]string {
	symbols := qualifiedSymbols(facts)
	byPath := make(map[string][]string)
	for _, f := range facts {
		if f.Kind != "route" {
			continue
		}
		id := graph.EndpointID(&f)
		if handlerPath := f.Attrs[extract.AttrHandlerPath]; handlerPath != "" {
			byPath[handlerPath] = append(byPath[handlerPath], id)
			continue
		}
		target := cmp.Or(f.Attrs["controller"], f.Attrs["handler"])
		if target == "" {
			continue
		}
		for _, s := range symbols {
			if strings.Contains(target, s.fqn) {
				byPath[s.path] = append(byPath[s.path], id)
			}
		}
	}
	for path := range byPath {
		sort.Strings(byPath[path])
		byPath[path] = dedup(byPath[path])
	}
	return byPath
}

// preciseSymbols groups indexer-resolved symbol definitions (attr
// precise=true, e.g. from the SCIP converter) by file, for injection into
// the map prompt as citation anchors.
func preciseSymbols(facts []extract.Fact) map[string][]mapper.SymbolContext {
	byPath := make(map[string][]mapper.SymbolContext)
	for _, f := range facts {
		if f.Kind != "symbol" || f.Attrs["precise"] != "true" {
			continue
		}
		start, end, err := extract.ParseLines(f.Source.Lines)
		if err != nil {
			continue
		}
		byPath[f.Source.Path] = append(byPath[f.Source.Path], mapper.SymbolContext{
			Name:      f.Attrs["name"],
			StartLine: start,
			EndLine:   end,
			Kind:      f.Attrs["kind"],
			Signature: f.Attrs["signature"],
		})
	}
	return byPath
}

// mechanicalRules indexes the (validated) rule facts by cited file, for the
// map prompt's MECHANICAL_RULES section.
func mechanicalRules(facts []extract.Fact) map[string][]mapper.Note {
	byPath := make(map[string][]mapper.Note)
	for _, f := range facts {
		if f.Kind != extract.KindRule {
			continue
		}
		start, end, err := extract.ParseLines(f.Source.Lines)
		if err != nil {
			continue // ValidateRuleFacts already dropped these
		}
		byPath[f.Source.Path] = append(byPath[f.Source.Path], mapper.Note{
			StartLine: start,
			EndLine:   end,
			Text:      f.Attrs[extract.AttrRequirement],
		})
	}
	return byPath
}

// docNotes indexes the doc facts by file, for the map prompt's SCHEMA_DOCS
// section.
func docNotes(facts []extract.Fact) map[string][]mapper.Note {
	byPath := make(map[string][]mapper.Note)
	for _, f := range facts {
		if f.Kind != extract.KindDoc || f.Attrs[extract.AttrText] == "" {
			continue
		}
		start, end, err := extract.ParseLines(f.Source.Lines)
		if err != nil {
			continue
		}
		byPath[f.Source.Path] = append(byPath[f.Source.Path], mapper.Note{
			StartLine: start,
			EndLine:   end,
			Text:      f.Attrs[extract.AttrText],
		})
	}
	return byPath
}

func allEndpointIDs(facts []extract.Fact) []string {
	var ids []string
	for _, f := range facts {
		if f.Kind == "route" {
			ids = append(ids, graph.EndpointID(&f))
		}
	}
	sort.Strings(ids)
	return dedup(ids)
}

func dedup(sorted []string) []string {
	var out []string
	for i, s := range sorted {
		if i == 0 || s != sorted[i-1] {
			out = append(out, s)
		}
	}
	return out
}
