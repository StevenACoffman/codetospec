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
				idx[def.Name] = append(idx[def.Name], schemaField{name: f.Name, file: name})
			}
		}
	}
	return ""
}

// lookup finds the field a resolver method implements. gqlgen derives Go
// method names from field names with initialism rules (userID → UserID), so
// the match ignores case. It returns the field name and the declaring schema
// file; without a match, the method name with a lower-case first letter and
// "".
func (idx schemaIndex) lookup(typeName, method string) (string, string) {
	for _, f := range idx[typeName] {
		if strings.EqualFold(f.name, method) {
			return f.name, f.file
		}
	}
	return strings.ToLower(method[:1]) + method[1:], ""
}
