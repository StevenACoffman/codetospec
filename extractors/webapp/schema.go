package main

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"

	"github.com/vektah/gqlparser/v2/ast"
	"github.com/vektah/gqlparser/v2/parser"
	"gopkg.in/yaml.v3"
)

// schemaIndex maps GraphQL types to their fields and the schema file that
// declares each one.
type schemaIndex map[string][]schemaField

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

// lookup finds the field a resolver method implements. gqlgen derives Go
// method names from field names with initialism rules (userID → UserID), so
// the match ignores case. Without a match, it returns a field named after
// the method (lower-case first letter) with no file.
func (idx schemaIndex) lookup(typeName, method string) schemaField {
	for _, f := range idx[typeName] {
		if strings.EqualFold(f.name, method) {
			return f
		}
	}
	return schemaField{name: strings.ToLower(method[:1]) + method[1:]}
}
