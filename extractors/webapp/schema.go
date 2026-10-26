package main

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"

	"github.com/vektah/gqlparser/v2/ast"
	"github.com/vektah/gqlparser/v2/parser"
	"gopkg.in/yaml.v3"
)

// schemaIndex maps GraphQL types to their fields and the schema file that
// declares each one. Federation's entity types (those with a @key) are
// indexed as the "fields" of entityType: gqlgen resolves each through a
// finder method of its entity resolver.
type schemaIndex map[string][]schemaField

// entityType is the GraphQL type of gqlgen's federation entity resolver
// (entityResolver), whose finders resolve the gateway's _entities query.
const entityType = "Entity"

type schemaField struct {
	name string // as declared, e.g. "addAuthEmail"
	file string // e.g. "auth_emails.graphql"
	line int    // 1-based line of the declaration
	doc  string // description and argument descriptions, one line; "" if none
}

// maxDocLen caps a field's documentation in the map prompt: enough for the
// authors' intent, not a manual.
const maxDocLen = 400

// fieldDoc renders a field's description and its arguments' descriptions as
// one line, capped at maxDocLen runes.
func fieldDoc(f *ast.FieldDefinition) string {
	parts := []string{}
	if d := oneLine(f.Description); d != "" {
		parts = append(parts, d)
	}
	var args []string
	for _, a := range f.Arguments {
		if d := oneLine(a.Description); d != "" {
			args = append(args, a.Name+" — "+d)
		}
	}
	if len(args) > 0 {
		parts = append(parts, "Arguments: "+strings.Join(args, "; "))
	}
	doc := strings.Join(parts, " ")
	if runes := []rune(doc); len(runes) > maxDocLen {
		doc = string(runes[:maxDocLen-1]) + "…"
	}
	return doc
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// gqlgenConfig is the part of gqlgen.yml the extractor reads.
type gqlgenConfig struct {
	Schema []string `yaml:"schema"`
}

// loadSchema indexes the service's own schema files listed in gqlgen.yml.
// Entries outside the service (../../pkg/graphql/shared-schemas/…) are
// skipped: they are not this service's operations. A missing gqlgen.yml or
// an unparsable schema file is a warning, never an error.
func loadSchema(fsys fs.FS) (schemaIndex, []string, error) {
	index := schemaIndex{}
	raw, err := fs.ReadFile(fsys, "gqlgen.yml")
	if errors.Is(err, fs.ErrNotExist) {
		return index, []string{"no gqlgen.yml: resolvers keep the directory domain"}, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("read gqlgen.yml: %w", err)
	}
	var cfg gqlgenConfig
	if yamlErr := yaml.Unmarshal(raw, &cfg); yamlErr != nil {
		return index, []string{fmt.Sprintf("gqlgen.yml: %v", yamlErr)}, nil
	}

	var warnings []string
	for _, pattern := range cfg.Schema {
		if strings.HasPrefix(pattern, "../") || path.IsAbs(pattern) {
			continue
		}
		names, globErr := fs.Glob(fsys, path.Clean(pattern))
		if globErr != nil {
			warnings = append(warnings, fmt.Sprintf("gqlgen.yml schema %q: %v", pattern, globErr))
			continue
		}
		for _, name := range names {
			if w := index.add(fsys, name); w != "" {
				warnings = append(warnings, w)
			}
		}
	}
	return index, warnings, nil
}

// add indexes the type fields of one schema file; it returns a warning when
// the file cannot be read or parsed.
func (idx schemaIndex) add(fsys fs.FS, name string) string {
	src, err := fs.ReadFile(fsys, name)
	if err != nil {
		return fmt.Sprintf("read %s: %v", name, err)
	}
	doc, err := parser.ParseSchema(&ast.Source{Name: name, Input: string(src)})
	if err != nil {
		return fmt.Sprintf("parse %s: %v", name, err)
	}
	for _, defs := range []ast.DefinitionList{doc.Definitions, doc.Extensions} {
		for _, def := range defs {
			idx.addEntity(name, def)
			for _, f := range def.Fields {
				field := schemaField{name: f.Name, file: name, doc: fieldDoc(f)}
				if f.Position != nil {
					field.line = f.Position.Line
				}
				idx[def.Name] = append(idx[def.Name], field)
			}
		}
	}
	return ""
}

// addEntity indexes def as an entity type when it has a @key (a type
// declared then extended keeps its first declaration).
func (idx schemaIndex) addEntity(file string, def *ast.Definition) {
	if def.Directives.ForName("key") == nil || idx.entity(def.Name) {
		return
	}
	entity := schemaField{name: def.Name, file: file}
	if def.Position != nil {
		entity.line = def.Position.Line
	}
	idx[entityType] = append(idx[entityType], entity)
}

// entity reports whether name is an indexed entity type.
func (idx schemaIndex) entity(name string) bool {
	return slices.ContainsFunc(idx[entityType], func(f schemaField) bool { return f.name == name })
}

// lookup finds the field a resolver method implements. gqlgen derives Go
// method names from field names with initialism rules (userID → UserID), so
// the match ignores case. Without a match, it returns a field named after
// the method (lower-case first letter) with no file. An entity finder
// "matches" the declaration of the entity type it finds (see finderOf).
func (idx schemaIndex) lookup(typeName, method string) schemaField {
	named := schemaField{name: strings.ToLower(method[:1]) + method[1:]}
	if typeName == entityType {
		if entity, ok := idx.finderOf(method); ok {
			named.file, named.line = entity.file, entity.line
		}
		return named
	}
	for _, f := range idx[typeName] {
		if strings.EqualFold(f.name, method) {
			return f
		}
	}
	return named
}

// finderOf returns the entity type a finder method finds. gqlgen names
// finders FindMany<Type>By<Keys>s and Find<Type>By<Keys>; as a type name
// may itself contain "By" (RowForActivationReportByTeacherV2), the longest
// entity type that fits wins. A type whose @key lives in a shared schema
// (pkg/graphql/shared-schemas) is found among the types the service's own
// files declare or extend instead.
func (idx schemaIndex) finderOf(method string) (schemaField, bool) {
	rest, isFinder := strings.CutPrefix(method, "Find")
	if !isFinder {
		return schemaField{}, false
	}
	if found, ok := longestFit(rest, idx[entityType]); ok {
		return found, true
	}
	return longestFit(rest, idx.declaredTypes())
}

// longestFit returns the longest of types that rest ("ManyUserByKaids",
// "UserByKaid") finds.
func longestFit(rest string, types []schemaField) (schemaField, bool) {
	var best schemaField
	for _, t := range types {
		for _, prefix := range []string{"Many" + t.name + "By", t.name + "By"} {
			if strings.HasPrefix(rest, prefix) && len(t.name) > len(best.name) {
				best = t
			}
		}
	}
	return best, best.name != ""
}

// declaredTypes lists the types with fields in the service's schema, each
// at its first field, sorted by name.
func (idx schemaIndex) declaredTypes() []schemaField {
	var types []schemaField
	for name, fields := range idx {
		if name != entityType && len(fields) > 0 {
			types = append(
				types,
				schemaField{name: name, file: fields[0].file, line: fields[0].line},
			)
		}
	}
	slices.SortFunc(types, func(a, b schemaField) int { return strings.Compare(a.name, b.name) })
	return types
}
