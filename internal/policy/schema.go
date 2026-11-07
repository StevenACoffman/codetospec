package policy

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
	"gopkg.in/yaml.v3"
)

// schemas are the JSON Schemas of the files the policy package reads and
// writes (schema/*.schema.json): the one statement of each format's shape.
//
//go:embed schema/*.schema.json
var schemas embed.FS

// The schema files, by format.
const (
	SchemaCheckers  = "checkers.schema.json"  // a person's checkers file
	SchemaRegistry  = "registry.schema.json"  // a person's registry, the glossary too
	SchemaInventory = "inventory.schema.json" // the inventory policies writes
)

// validated decodes data, JSON or the same document in YAML, checks it
// against the named schema and returns it as JSON, ready to decode into
// the format's types.
//
// Ensures: an error naming the instance location (the JSON path) of each
// violation; YAML and JSON of one document validate and decode alike.
func validated(name string, data []byte) ([]byte, error) {
	var doc any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	normalized, err := json.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(normalized))
	if err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	schema, err := compiled(name)
	if err != nil {
		return nil, err
	}
	if err = schema.Validate(instance); err != nil {
		return nil, fmt.Errorf("does not match %s: %s", name, violations(err, schemaKeys(name)))
	}
	return normalized, nil
}

// violations renders a schema validation error as its violations, each
// "<JSON path>: <message>", sorted by path, without the schema's file
// location (it depends on where the binary was built); err's own text when
// it is no validation error. A key no definition of the schema names
// reads "unknown key"; the echoes another failure leaves through
// unevaluatedProperties (a failed definition evaluates none of its keys)
// are dropped.
func violations(err error, known map[string]bool) string {
	var ve *jsonschema.ValidationError
	if !errors.As(err, &ve) {
		return err.Error()
	}
	type leaf struct {
		path  string
		kind  jsonschema.ErrorKind
		false bool
	}
	var leaves []leaf
	var walk func(e *jsonschema.ValidationError)
	walk = func(e *jsonschema.ValidationError) {
		if len(e.Causes) == 0 {
			_, isFalse := e.ErrorKind.(*kind.FalseSchema)
			leaves = append(
				leaves,
				leaf{"/" + strings.Join(e.InstanceLocation, "/"), e.ErrorKind, isFalse},
			)
		}
		for _, c := range e.Causes {
			walk(c)
		}
	}
	walk(ve)
	failed := map[string]bool{} // paths with a real failure
	for _, l := range leaves {
		if !l.false {
			failed[l.path] = true
		}
	}
	p := message.NewPrinter(language.English)
	var out []string
	for _, l := range leaves {
		key := l.path[strings.LastIndex(l.path, "/")+1:]
		switch {
		case l.false && (failed[l.path] || known[key]):
			continue
		case l.false:
			out = append(out, l.path+": unknown key")
		default:
			out = append(out, l.path+": "+l.kind.LocalizedString(p))
		}
	}
	slices.Sort(out)
	return strings.Join(slices.Compact(out), "; ")
}

// schemaBase is the URL the embedded schemas' $ids share: a $ref from one
// to another ("checkers.schema.json#/$defs/entry") resolves against it.
const schemaBase = "https://github.com/StevenACoffman/codetospec/internal/policy/schema/"

// compiled compiles the embedded schema named name, every embedded schema
// loaded so that its $refs to the others resolve.
func compiled(name string) (*jsonschema.Schema, error) {
	files, err := schemas.ReadDir("schema")
	if err != nil {
		return nil, fmt.Errorf("schema %s: %w", name, err)
	}
	c := jsonschema.NewCompiler()
	for _, f := range files {
		data, readErr := schemas.ReadFile("schema/" + f.Name())
		if readErr != nil {
			return nil, fmt.Errorf("schema %s: %w", f.Name(), readErr)
		}
		doc, decodeErr := jsonschema.UnmarshalJSON(bytes.NewReader(data))
		if decodeErr != nil {
			return nil, fmt.Errorf("schema %s: %w", f.Name(), decodeErr)
		}
		if err = c.AddResource(schemaBase+f.Name(), doc); err != nil {
			return nil, fmt.Errorf("schema %s: %w", f.Name(), err)
		}
	}
	return c.Compile(schemaBase + name)
}

// schemaKeys are the property names the named schema's definitions
// declare: the keys a document may hold.
func schemaKeys(name string) map[string]bool {
	keys := map[string]bool{}
	data, err := schemas.ReadFile("schema/" + name)
	if err != nil {
		return keys
	}
	var doc struct {
		Properties map[string]json.RawMessage `json:"properties"`
		Defs       map[string]struct {
			Properties map[string]json.RawMessage `json:"properties"`
		} `json:"$defs"`
	}
	if json.Unmarshal(data, &doc) != nil {
		return keys
	}
	for k := range doc.Properties {
		keys[k] = true
	}
	for _, def := range doc.Defs {
		for k := range def.Properties {
			keys[k] = true
		}
	}
	return keys
}
