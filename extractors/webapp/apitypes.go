package main

import (
	"fmt"
	"io/fs"
	"maps"
	"slices"
	"strconv"
	"strings"

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
	// attrImplements names the interfaces a type declares it implements,
	// sorted and comma-separated; attrInterface is "true" on the fact of
	// an interface (codetospec's extract.AttrImplements, AttrInterface).
	attrImplements = "implements"
	attrInterface  = "interface"
)

// apiTypeFacts returns one api_type fact per federated entity type (one
// with a @key) of the service's own schema files: owned when a file
// declares it (type X @key), a reference to another service's concept when
// the service only extends it (extend type X @key). A type both declared
// and extended is owned, at its declaration. Each carries its description
// and the interfaces it implements; an interface an owned entity type
// implements, declared in those files, gets a fact of its own. Unparsable
// files are warnings (loadSchema reports them too).
//
// Ensures: facts sorted by id; at most one per type.
func apiTypeFacts(fsys fs.FS) []Fact {
	names, _, err := schemaFiles(fsys)
	if err != nil {
		return nil
	}
	byName := map[string]Fact{}
	interfaces := map[string]declared{}
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
			if def.Kind == ast.Interface {
				interfaces[def.Name] = declared{name, def}
			}
		}
		for _, def := range doc.Extensions {
			addAPIType(byName, name, def, false)
		}
	}
	addInterfaces(byName, interfaces)
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
	if len(def.Interfaces) > 0 {
		attrs[attrImplements] = strings.Join(slices.Sorted(slices.Values(def.Interfaces)), ",")
	}
	byName[id] = apiTypeFact(id, file, def, attrs)
}

// declared is a definition and the schema file declaring it.
type declared struct {
	file string
	def  *ast.Definition
}

// addInterfaces records the interfaces the owned types implement, when a
// schema file declares them: a concept the types are kinds of.
func addInterfaces(byName map[string]Fact, interfaces map[string]declared) {
	implemented := map[string]bool{}
	for _, f := range byName {
		if f.Attrs[attrOwned] != goTrue || f.Attrs[attrImplements] == "" {
			continue
		}
		for name := range strings.SplitSeq(f.Attrs[attrImplements], ",") {
			implemented[name] = true
		}
	}
	for name := range implemented {
		d, ok := interfaces[name]
		if !ok {
			continue
		}
		id := kindAPIType + "." + name
		byName[id] = apiTypeFact(id, d.file, d.def, map[string]string{
			attrName: name, attrOwned: goTrue, attrInterface: goTrue,
		})
	}
}

// apiTypeFact is the api_type fact of def, declared in file, with attrs
// and its description.
func apiTypeFact(id, file string, def *ast.Definition, attrs map[string]string) Fact {
	if d := oneLine(def.Description); d != "" {
		attrs[attrDescription] = d
	}
	line := 1
	if def.Position != nil {
		line = def.Position.Line
	}
	return Fact{
		Kind:      kindAPIType,
		ID:        id,
		Attrs:     attrs,
		Source:    Ref{Path: file, Lines: fmt.Sprintf("%d-%d", line, line)},
		Origin:    origin,
		Certainty: certain,
	}
}
