package main

import (
	"fmt"
	"go/ast"
	"go/token"
	"strings"
)

// kindCapability is a capability the service registers (codetospec's
// extract.KindCapability): attrName its Go name, what checks and role
// definitions name; attrValue its stored name, what a grant holds;
// attrScopeKeys the scope keys a grant of it must set, comma-separated.
const (
	kindCapability = "capability"
	attrScopeKeys  = "scope_keys"
)

// capabilityFacts returns one fact per capability variable sources
// register (admin's permissions/capabilities): `var X =
// RegisterCapabilityName("x")` or `var X =
// RegisterCapabilityDefinition(Capability{Name: "x", RequiredScopeKeys:
// …})`, cited on the declaration.
func capabilityFacts(sources []source) []Fact {
	keyLists := scopeKeyLists(sources)
	var facts []Fact
	for i := range sources {
		s := &sources[i]
		for _, spec := range varSpecs(s.file) {
			if f, ok := capabilityFact(s, spec, keyLists); ok {
				facts = append(facts, f)
			}
		}
	}
	return facts
}

// varSpecs lists a file's package-level variable specs.
func varSpecs(file *ast.File) []*ast.ValueSpec {
	var specs []*ast.ValueSpec
	for _, decl := range file.Decls {
		gen, isGen := decl.(*ast.GenDecl)
		if !isGen || gen.Tok != token.VAR {
			continue
		}
		for _, spec := range gen.Specs {
			if vs, isValue := spec.(*ast.ValueSpec); isValue {
				specs = append(specs, vs)
			}
		}
	}
	return specs
}

// scopeKeyLists indexes the package-level lists of scope keys sources
// declare (`var UserScopeKeys = []models.ScopeType{models.UserKindScopeType}`)
// by name, so a capability naming one resolves to its keys.
func scopeKeyLists(sources []source) map[string][]string {
	lists := map[string][]string{}
	for i := range sources {
		for _, vs := range varSpecs(sources[i].file) {
			if len(vs.Names) != 1 || len(vs.Values) != 1 {
				continue
			}
			if keys, ok := selectorNames(vs.Values[0]); ok {
				lists[vs.Names[0].Name] = keys
			}
		}
	}
	return lists
}

// selectorNames reads a composite literal of selectors
// ([]models.ScopeType{models.LocaleScopeType}) as the selected names.
func selectorNames(e ast.Expr) ([]string, bool) {
	list, isList := e.(*ast.CompositeLit)
	if !isList {
		return nil, false
	}
	names := make([]string, 0, len(list.Elts))
	for _, k := range list.Elts {
		sel, isSel := k.(*ast.SelectorExpr)
		if !isSel {
			return nil, false
		}
		names = append(names, sel.Sel.Name)
	}
	return names, true
}

// capabilityFact is the fact of a capability variable's spec.
func capabilityFact(s *source, vs *ast.ValueSpec, keyLists map[string][]string) (Fact, bool) {
	if len(vs.Names) != 1 || len(vs.Values) != 1 {
		return Fact{}, false
	}
	call, isCall := vs.Values[0].(*ast.CallExpr)
	if !isCall || len(call.Args) != 1 {
		return Fact{}, false
	}
	var stored, keys string
	switch calledName(call) {
	case "RegisterCapabilityName":
		stored = stringValue(call.Args[0])
	case "RegisterCapabilityDefinition":
		def, isLit := call.Args[0].(*ast.CompositeLit)
		if !isLit {
			return Fact{}, false
		}
		stored, keys = capabilityDefinition(def, keyLists)
	default:
		return Fact{}, false
	}
	if stored == "" {
		return Fact{}, false
	}
	start, end := s.fset.Position(vs.Pos()).Line, s.fset.Position(vs.End()).Line
	return Fact{
		Kind: kindCapability,
		ID:   fmt.Sprintf("capability.webapp.%s:%d-%d", s.name, start, end),
		Attrs: map[string]string{
			attrName: vs.Names[0].Name, attrValue: stored, attrScopeKeys: keys,
		},
		Source:    Ref{Path: s.name, Lines: lines(start, end)},
		Origin:    origin,
		Certainty: certain,
	}, true
}

// capabilityDefinition reads a Capability{…} literal's stored name and its
// required scope keys (their names, comma-separated, in order), written in
// place or as a list keyLists holds.
func capabilityDefinition(def *ast.CompositeLit, keyLists map[string][]string) (string, string) {
	var stored string
	var keys []string
	for _, elt := range def.Elts {
		kv, isKV := elt.(*ast.KeyValueExpr)
		if !isKV {
			continue
		}
		key, isIdent := kv.Key.(*ast.Ident)
		if !isIdent {
			continue
		}
		switch key.Name {
		case "Name":
			stored = stringValue(kv.Value)
		case "RequiredScopeKeys":
			if names, ok := selectorNames(kv.Value); ok {
				keys = names
			} else if list, isList := kv.Value.(*ast.Ident); isList {
				keys = keyLists[list.Name]
			}
		}
	}
	return stored, strings.Join(keys, ",")
}
