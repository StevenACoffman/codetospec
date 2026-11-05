package main

import (
	"fmt"
	"io/fs"
	"maps"
	"slices"
	"strconv"

	"github.com/vektah/gqlparser/v2/ast"
	"github.com/vektah/gqlparser/v2/parser"
)

// The api_type fact (codetospec's extract.KindAPIType): a federated
// entity type of the service's schema, a seed for the business concepts
// codetospec proposes (DOMAIN_PLAN.md recommendation 7).
const (
	kindAPIType    = "api_type"
	attrOwned      = "owned"      // "true" when the service declares it, "false" when it extends it
	attrKey        = "key"        // its @key fields, as written
	attrDeprecated = "deprecated" // "true" when every field it declares is @deprecated
)

// apiTypeFacts returns one api_type fact per federated entity type (one
// with a @key) of the service's own schema files: owned when a file
// declares it (type X @key), a reference to another service's concept when
// the service only extends it (extend type X @key). A type both declared
// and extended is owned, at its declaration. Unparsable files are warnings
// (loadSchema reports them too).
//
// Ensures: facts sorted by id; at most one per type.
func apiTypeFacts(fsys fs.FS) []Fact {
	names, _, err := schemaFiles(fsys)
	if err != nil {
		return nil
	}
	byName := map[string]Fact{}
	for _, name := range names {
		src, readErr := fs.ReadFile(fsys, name)
		if readErr != nil {
			continue
		}
		doc, parseErr := parser.ParseSchema(&ast.Source{Name: name, Input: string(src)})
		if parseErr != nil {
			continue
		}
		for _, def := range doc.Definitions {
			addAPIType(byName, name, def, true)
		}
		for _, def := range doc.Extensions {
			addAPIType(byName, name, def, false)
		}
	}
	facts := make([]Fact, 0, len(byName))
	for _, id := range slices.Sorted(maps.Keys(byName)) {
		facts = append(facts, byName[id])
	}
	return facts
}

// addAPIType records def, declared in file, when it is an entity type;
// a declaration replaces an extension seen before it.
func addAPIType(byName map[string]Fact, file string, def *ast.Definition, declared bool) {
	key := def.Directives.ForName("key")
	if key == nil {
		return
	}
	id := kindAPIType + "." + def.Name
	if prior, seen := byName[id]; seen && (prior.Attrs[attrOwned] == goTrue || !declared) {
		return
	}
	attrs := map[string]string{
		attrName:  def.Name,
		attrOwned: strconv.FormatBool(declared),
	}
	if fields := key.Arguments.ForName("fields"); fields != nil && fields.Value != nil {
		attrs[attrKey] = fields.Value.Raw
	}
	if len(def.Fields) > 0 && !slices.ContainsFunc(def.Fields, func(f *ast.FieldDefinition) bool {
		return f.Directives.ForName("deprecated") == nil
	}) {
		attrs[attrDeprecated] = goTrue
	}
	line := 1
	if def.Position != nil {
		line = def.Position.Line
	}
	byName[id] = Fact{
		Kind:      kindAPIType,
		ID:        id,
		Attrs:     attrs,
		Source:    Ref{Path: file, Lines: fmt.Sprintf("%d-%d", line, line)},
		Origin:    origin,
		Certainty: certain,
	}
}
