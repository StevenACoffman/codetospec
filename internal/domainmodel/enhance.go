package domainmodel

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"path"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/StevenACoffman/codetospec/internal/ears"
	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/graph"
	"github.com/StevenACoffman/codetospec/internal/locale"
	"github.com/StevenACoffman/indomain/pkg/lint"
	"github.com/StevenACoffman/indomain/pkg/schema"
	"gopkg.in/yaml.v3"
)

// Table is a code entity as its table fact describes it.
type Table struct {
	ID          string   // the graph entity node id
	Title       string   // the table or kind name
	Columns     []string // "name:type"
	Description string   // authored description; "" when none
	Source      extract.Ref
	// References are its foreign keys (extract.ParseReferences).
	References []extract.Reference
}

// APIType is an entity type of an API the code serves (an api_type fact):
// a concept seed.
type APIType struct {
	Name string
	// Owned is false for a type the code only extends: another service's
	// concept, which the proposal names but never defines.
	Owned bool
	// Deprecated marks a type every field of which is deprecated: cruft,
	// left out of the proposal.
	Deprecated bool
	Source     extract.Ref
}

// EnhanceInput is what Enhance builds models from.
type EnhanceInput struct {
	Set      *Set // human models; nil when none was given
	Nodes    []graph.Node
	Tables   []Table
	APITypes []APIType
	// Title names the code the proposed concepts model covers (the
	// output's source); "" names it after the generated file.
	Title   string
	Lang    string  // --lang: placeholder and description text
	Options Options // entity mapping, as for Compare
	// MirrorPath is where a human model's enhanced copy goes, relative to
	// the models directory; nil writes it under its base name.
	MirrorPath func(modelPath string) string
}

// File is one model file to write, at Path relative to the models
// directory.
type File struct {
	Path string
	Data []byte
}

// Enhanced is the result of Enhance.
type Enhanced struct {
	Files []File // sorted by path
	// Provenance says, per file and JSON pointer, where each field
	// codetospec decided comes from: "human", "description <path:lines>",
	// "placeholder", "column <path:lines>" or "rule <id>".
	Provenance map[string]map[string]string
	// Undefined lists, per file, the entities whose definition is still a
	// placeholder, sorted.
	Undefined map[string][]string
}

// minNameLen is the shortest entity name or invariant id the modelith
// schema accepts.
const minNameLen = 2

// yamlString is the YAML tag of a string scalar.
const yamlString = "!!str"

// Origins recorded in Enhanced.Provenance.
const (
	originHuman       = "human"
	originPlaceholder = "placeholder"
)

// Enhance writes modelith models for the graph. Each human model is copied
// with what the code adds: a definition from the table's description where
// the human one is a placeholder, attributes for columns it lacks, and
// invariants for single-entity ubiquitous rules no invariant states. The
// concepts no human model names go into one proposed model
// (ConceptsFile, DOMAIN_PLAN.md recommendation 2: never one per current
// domain, whose names are code layers): code tables, defined by their
// description or a placeholder, and owned API entity types no table
// stands for; a table with a NOT NULL foreign key to a single parent is
// owned by it, other keys are references. The types the code only extends
// (other services' concepts) and the deprecated ones (cruft) are named in
// its description, never defined.
//
// Requires: in.Set (when given) comes from Load; table ids are entity node
// ids.
// Ensures:  every file passes the modelith schema, generated ones the full
// lint; human definitions, comments and order are kept; the result depends
// on neither node nor map order.
func Enhance(in *EnhanceInput) (*Enhanced, error) {
	b := newBuilder(in)
	out := &Enhanced{Provenance: map[string]map[string]string{}, Undefined: map[string][]string{}}
	if in.Set != nil {
		for i := range in.Set.Models {
			if err := b.mirror(&in.Set.Models[i], out); err != nil {
				return nil, err
			}
		}
	}
	if err := b.generate(out); err != nil {
		return nil, err
	}
	slices.SortFunc(out.Files, func(a, b File) int { return cmp.Compare(a.Path, b.Path) })
	for file := range out.Undefined {
		slices.Sort(out.Undefined[file])
	}
	return out, nil
}

// invariantRule is a rule that can become an invariant: ubiquitous, and
// touching a single entity.
type invariantRule struct {
	id, statement string
}

// builder holds what Enhance derives once from its input.
type builder struct {
	in     *EnhanceInput
	text   *locale.Text
	tables map[string]*Table
	rules  map[string][]invariantRule // entity id → its invariant rules, by id
	report Report
}

func newBuilder(in *EnhanceInput) *builder {
	b := &builder{
		in:     in,
		text:   locale.For(in.Lang),
		tables: map[string]*Table{},
		rules:  map[string][]invariantRule{},
	}
	for i := range in.Tables {
		b.tables[in.Tables[i].ID] = &in.Tables[i]
	}
	for i := range in.Nodes {
		n := &in.Nodes[i]
		touches := touched(n)
		if n.Type != graph.TypeRule || n.Extra["ears"] != ears.Ubiquitous || len(touches) != 1 {
			continue
		}
		statement := strings.ReplaceAll(graph.RuleRequirement(n.Body), "`", "")
		b.rules[touches[0]] = append(b.rules[touches[0]], invariantRule{n.ID, statement})
	}
	for id := range b.rules {
		slices.SortFunc(
			b.rules[id],
			func(a, b invariantRule) int { return cmp.Compare(a.id, b.id) },
		)
	}
	if in.Set != nil {
		b.report = Compare(in.Set, in.Nodes, in.Options)
	}
	return b
}

// touched lists the entities a rule node touches.
func touched(n *graph.Node) []string {
	var ids []string
	for _, e := range n.Edges {
		if e.Type == graph.EdgeTouches {
			ids = append(ids, e.To)
		}
	}
	return ids
}

// isPlaceholder reports whether a definition is one codetospec wrote, in
// any language.
func isPlaceholder(definition string) bool {
	for _, text := range locale.All() {
		if strings.HasPrefix(strings.TrimSpace(definition), text.PlaceholderPrefix) {
			return true
		}
	}
	return false
}

// placeholder is the definition of an entity nobody described.
func (b *builder) placeholder(t *Table) string {
	return fmt.Sprintf(b.text.PlaceholderFmt, b.text.PlaceholderPrefix, t.Title, sourceOf(t))
}

func sourceOf(t *Table) string {
	return t.Source.Path + ":" + t.Source.Lines
}

// mirror enhances one human model through its YAML node tree, so its
// comments, order and definitions survive.
func (b *builder) mirror(m *Model, out *Enhanced) error {
	var doc yaml.Node
	if err := yaml.Unmarshal(m.Source, &doc); err != nil || len(doc.Content) == 0 {
		return fmt.Errorf("edit domain model %s: %w", m.Path, cmp.Or(err, errEmptyModel))
	}
	file := path.Base(m.Path)
	if b.in.MirrorPath != nil {
		file = b.in.MirrorPath(m.Path)
	}
	prov := map[string]string{}
	used := invariantIDs(m)
	entities := mappingValue(doc.Content[0], "entities")
	for _, name := range m.Model.EntityNames() {
		node := mappingValue(entities, name)
		if node == nil {
			continue
		}
		match := b.matchOf(m.Scope + "." + name)
		pointer := "/entities/" + name
		if undefined := b.mirrorDefinition(m, name, node, match, prov); undefined {
			out.Undefined[file] = append(out.Undefined[file], name)
		}
		if match == nil {
			continue
		}
		if err := b.addAttributes(m, name, node, match, prov); err != nil {
			return fmt.Errorf("edit domain model %s: %w", m.Path, err)
		}
		if err := b.addInvariants(node, pointer, match, used, prov); err != nil {
			return fmt.Errorf("edit domain model %s: %w", m.Path, err)
		}
	}
	data, err := encode(&doc)
	if err != nil {
		return fmt.Errorf("edit domain model %s: %w", m.Path, err)
	}
	if findings := lint.Structural(data); len(findings) > 0 {
		return fmt.Errorf("enhanced model %s is invalid: %s", file, findings[0].Message)
	}
	out.Files = append(out.Files, File{Path: file, Data: data})
	out.Provenance[file] = prov
	return nil
}

// errEmptyModel is a model file holding no YAML document.
var errEmptyModel = errors.New("no YAML document")

// matchOf is the comparison of a human entity, by qualified name, when it
// matched a code entity; nil otherwise.
func (b *builder) matchOf(qualified string) *Entity {
	for i := range b.report.Entities {
		e := &b.report.Entities[i]
		if e.Name == qualified && e.Code.ID != "" {
			return e
		}
	}
	return nil
}

// mirrorDefinition keeps a human definition; a placeholder gives way to the
// table's description when there is one. It reports whether the entity is
// still undefined.
func (b *builder) mirrorDefinition(
	m *Model,
	name string,
	node *yaml.Node,
	match *Entity,
	prov map[string]string,
) bool {
	pointer := "/entities/" + name + "/definition"
	if !isPlaceholder(m.Model.Entities[name].Definition) {
		prov[pointer] = originHuman
		return false
	}
	if match != nil {
		if t := b.tables[match.Code.ID]; t != nil && t.Description != "" {
			setMappingValue(node, "definition", t.Description)
			prov[pointer] = "description " + sourceOf(t)
			return false
		}
	}
	prov[pointer] = originPlaceholder
	return true
}

// addAttributes appends the table's columns a human entity lacks.
func (b *builder) addAttributes(
	m *Model,
	name string,
	node *yaml.Node,
	match *Entity,
	prov map[string]string,
) error {
	t := b.tables[match.Code.ID]
	if t == nil {
		return nil
	}
	have := map[string]bool{}
	for _, a := range m.Model.Entities[name].Attributes {
		have[a.Name] = true
	}
	for _, column := range t.Columns {
		attr := b.attribute(column)
		if have[attr.Name] {
			continue
		}
		have[attr.Name] = true
		index, err := appendEncoded(node, "attributes", attr)
		if err != nil {
			return err
		}
		prov[fmt.Sprintf("/entities/%s/attributes/%d", name, index)] = "column " + sourceOf(t)
	}
	return nil
}

// addInvariants appends the entity's invariant rules no invariant states.
func (b *builder) addInvariants(
	node *yaml.Node,
	pointer string,
	match *Entity,
	used map[string]bool,
	prov map[string]string,
) error {
	unstated := map[string]bool{}
	for _, id := range match.Unstated {
		unstated[id] = true
	}
	for _, r := range b.rules[match.Code.ID] {
		if !unstated[r.id] {
			continue
		}
		inv := invariantYAML{ID: uniqueID(r.id, used), Statement: r.statement}
		index, err := appendEncoded(node, "invariants", inv)
		if err != nil {
			return err
		}
		prov[fmt.Sprintf("%s/invariants/%d", pointer, index)] = "rule " + r.id
	}
	return nil
}

// invariantIDs lists the ids a model's invariants already use.
func invariantIDs(m *Model) map[string]bool {
	used := map[string]bool{}
	for _, inv := range m.Model.Invariants {
		used[inv.ID] = true
	}
	for _, e := range m.Model.Entities {
		for _, inv := range e.Invariants {
			used[inv.ID] = true
		}
	}
	return used
}

// uniqueID derives a modelith invariant id from a rule id
// ("rule.billing.min-amount" → "billing-min-amount"), unique among used,
// which it extends.
func uniqueID(ruleID string, used map[string]bool) string {
	base := extract.Slugify(strings.TrimPrefix(ruleID, "rule."))
	if base == "" || !unicode.IsLetter(rune(base[0])) || len(base) < minNameLen {
		base = "rule-" + base
	}
	id := base
	for n := 2; used[id]; n++ {
		id = base + "-" + strconv.Itoa(n)
	}
	used[id] = true
	return id
}

// generated groups the code entities no human model names, one model per
// domain.
func (b *builder) generate(out *Enhanced) error {
	matched := map[string]bool{}
	named := map[string]bool{} // normalized entity names of the human models
	for i := range b.report.Entities {
		if id := b.report.Entities[i].Code.ID; id != "" {
			matched[id] = true
		}
		_, name, _ := strings.Cut(b.report.Entities[i].Name, ".")
		named[normalize(name)] = true
	}
	var tables []*Table
	for i := range b.in.Tables {
		if t := &b.in.Tables[i]; !matched[t.ID] {
			tables = append(tables, t)
		}
	}
	slices.SortFunc(tables, func(a, b *Table) int { return cmp.Compare(a.ID, b.ID) })
	types := b.proposedTypes(named)
	if len(tables) == 0 && len(types.owned) == 0 {
		return nil
	}
	return b.generateConcepts(tables, types, out)
}

// ConceptsFile is the proposed concepts model Enhance writes, relative to
// the models directory.
const ConceptsFile = "generated/concepts.modelith.yaml"

// proposedTypes are the API types as the proposal uses them: owned ones
// it defines, referenced and deprecated ones it names in its description.
type proposedTypes struct {
	owned                  []*APIType
	referenced, deprecated []string
}

// proposedTypes sorts the API types for the proposal: owned ones that no
// table and no human model entity stands for (by normalized name, plurals
// included) are defined; the others are named, sorted by name.
func (b *builder) proposedTypes(named map[string]bool) proposedTypes {
	tabled := map[string]bool{}
	for i := range b.in.Tables {
		tabled[normalize(b.in.Tables[i].Title)] = true
	}
	var p proposedTypes
	for i := range b.in.APITypes {
		api := &b.in.APITypes[i]
		name := normalize(api.Name)
		switch {
		case api.Deprecated:
			p.deprecated = append(p.deprecated, api.Name)
		case !api.Owned:
			p.referenced = append(p.referenced, api.Name)
		case named[name] || tabled[name] || slices.ContainsFunc(plurals(name), func(n string) bool {
			return tabled[n]
		}):
			// A table or a human model entity already stands for it.
		default:
			p.owned = append(p.owned, api)
		}
	}
	slices.SortFunc(p.owned, func(a, b *APIType) int { return cmp.Compare(a.Name, b.Name) })
	slices.Sort(p.referenced)
	slices.Sort(p.deprecated)
	return p
}

// generateConcepts writes ConceptsFile: tables and owned API types as
// entities, foreign keys as relationships.
func (b *builder) generateConcepts(tables []*Table, types proposedTypes, out *Enhanced) error {
	file := ConceptsFile
	title := cmp.Or(b.in.Title, "concepts")
	prov := map[string]string{}
	description := fmt.Sprintf(b.text.GeneratedDescFmt, title)
	if len(types.referenced) > 0 {
		description += " " + fmt.Sprintf(
			b.text.ReferencedTypesFmt,
			strings.Join(types.referenced, ", "),
		)
	}
	if len(types.deprecated) > 0 {
		description += " " + fmt.Sprintf(
			b.text.DeprecatedTypesFmt,
			strings.Join(types.deprecated, ", "),
		)
	}
	doc := modelYAML{
		Kind:        "DomainModel",
		Version:     schema.Current,
		Title:       title,
		Description: description,
		Entities:    map[string]entityYAML{},
	}
	names := map[string]string{} // table title → entity name
	used := map[string]bool{}
	for _, t := range tables {
		name := uniqueName(entityName(t.Title), doc.Entities)
		names[t.Title] = name
		pointer := "/entities/" + name
		e := entityYAML{Definition: t.Description}
		prov[pointer+"/definition"] = "description " + sourceOf(t)
		if t.Description == "" {
			e.Definition = b.placeholder(t)
			prov[pointer+"/definition"] = originPlaceholder
			out.Undefined[file] = append(out.Undefined[file], name)
		}
		for _, column := range t.Columns {
			prov[fmt.Sprintf("%s/attributes/%d", pointer, len(e.Attributes))] = "column " + sourceOf(
				t,
			)
			e.Attributes = append(e.Attributes, b.attribute(column))
		}
		for _, r := range b.rules[t.ID] {
			prov[fmt.Sprintf("%s/invariants/%d", pointer, len(e.Invariants))] = "rule " + r.id
			e.Invariants = append(e.Invariants,
				invariantYAML{ID: uniqueID(r.id, used), Statement: r.statement})
		}
		doc.Entities[name] = e
	}
	for _, api := range types.owned {
		name := uniqueName(entityName(api.Name), doc.Entities)
		pointer := "/entities/" + name
		src := api.Source.Path + ":" + api.Source.Lines
		doc.Entities[name] = entityYAML{
			Definition: fmt.Sprintf(
				b.text.PlaceholderTypeFmt,
				b.text.PlaceholderPrefix,
				api.Name,
				src,
			),
		}
		prov[pointer+"/definition"] = originPlaceholder
		out.Undefined[file] = append(out.Undefined[file], name)
	}
	addRelationships(&doc, tables, names, prov)
	slices.Sort(out.Undefined[file])
	return b.writeGenerated(file, &doc, title, prov, out)
}

// addRelationships records each table's foreign keys between the entities
// of doc: a NOT NULL key to a single parent makes the parent own the
// table (composition: the row cannot exist without it); any other key is
// a reference from the table. Keys to tables outside doc are left out.
func addRelationships(
	doc *modelYAML,
	tables []*Table,
	names map[string]string,
	prov map[string]string,
) {
	for _, t := range tables {
		child := names[t.Title]
		parents := map[string]bool{}
		for _, r := range t.References {
			if r.NotNull {
				parents[r.Table] = true
			}
		}
		for _, r := range t.References {
			parent, ok := names[r.Table]
			if !ok || parent == child {
				continue
			}
			src := "references " + sourceOf(t)
			if r.NotNull && len(parents) == 1 {
				e := doc.Entities[parent]
				if slices.ContainsFunc(e.Relationships, func(rel relationshipYAML) bool {
					return rel.Entity == child
				}) {
					continue
				}
				prov[fmt.Sprintf("/entities/%s/relationships/%d", parent, len(e.Relationships))] = src
				e.Relationships = append(e.Relationships,
					relationshipYAML{Entity: child, Cardinality: "1:n", Ownership: ownershipOwned})
				doc.Entities[parent] = e
				continue
			}
			e := doc.Entities[child]
			prov[fmt.Sprintf("/entities/%s/relationships/%d", child, len(e.Relationships))] = src
			e.Relationships = append(
				e.Relationships,
				relationshipYAML{
					Entity:      parent,
					Cardinality: "n:1",
					Ownership:   ownershipReferenced,
				},
			)
			doc.Entities[child] = e
		}
	}
}

// The modelith relationship ownerships the proposal writes.
const (
	ownershipOwned      = "owned"
	ownershipReferenced = "referenced"
)

// writeGenerated encodes doc to file, requires it to pass the full lint,
// and records it in out.
func (b *builder) writeGenerated(
	file string, doc *modelYAML, title string, prov map[string]string, out *Enhanced,
) error {
	data, err := encodeGenerated(doc, fmt.Sprintf(b.text.GeneratedHeaderFmt, title))
	if err != nil {
		return fmt.Errorf("generated model %s: %w", file, err)
	}
	result, err := lint.Run(file, data, nil)
	if err != nil {
		return fmt.Errorf("lint generated model %s: %w", file, err)
	}
	for _, f := range result.Findings {
		if f.Severity == lint.SeverityError {
			return fmt.Errorf("generated model %s is invalid: %s: %s", file, f.Path, f.Message)
		}
	}
	out.Files = append(out.Files, File{Path: file, Data: data})
	out.Provenance[file] = prov
	return nil
}

// entityName turns a table name into a modelith entity name, PascalCase:
// user_nonces → UserNonces, so the next run matches it back by name.
func entityName(title string) string {
	var b strings.Builder
	upper := true
	for _, r := range title {
		switch {
		case !unicode.IsLetter(r) && !unicode.IsDigit(r):
			upper = true
		case upper:
			b.WriteRune(unicode.ToUpper(r))
			upper = false
		default:
			b.WriteRune(r)
		}
	}
	name := b.String()
	if name == "" || !unicode.IsUpper(rune(name[0])) {
		name = "E" + name
	}
	if len(name) < minNameLen {
		name += "Entity"
	}
	return name
}

// uniqueName suffixes name until no entity of the model has it.
func uniqueName(name string, entities map[string]entityYAML) string {
	unique := name
	for n := 2; ; n++ {
		if _, taken := entities[unique]; !taken {
			return unique
		}
		unique = name + strconv.Itoa(n)
	}
}

// attribute is the modelith attribute of a "name:type" column.
func (b *builder) attribute(column string) attributeYAML {
	name, codeType, _ := strings.Cut(column, ":")
	primitive, exact := primitiveType(codeType)
	attr := attributeYAML{Name: strings.TrimSpace(name), Type: primitive}
	if !exact {
		attr.Description = fmt.Sprintf(b.text.CodeTypeFmt, codeType)
	}
	return attr
}

// primitiveType maps a Go or SQL column type to a modelith primitive; exact
// is false when nothing fits and the type falls back to string.
func primitiveType(codeType string) (string, bool) {
	t := strings.TrimLeft(strings.ToLower(strings.TrimSpace(codeType)), "*")
	hasPrefix := func(prefixes ...string) bool {
		return slices.ContainsFunc(prefixes, func(p string) bool { return strings.HasPrefix(t, p) })
	}
	switch {
	case t == "time.time" || t == "date" || hasPrefix("timestamp", "datetime"):
		return "timestamp", true
	case t == "bool" || t == "boolean":
		return "boolean", true
	case t == "id" || hasPrefix("int", "uint", "bigint", "smallint", "tinyint", "serial",
		"bigserial", "increments", "bigincrements"):
		// "id" and the increments are Laravel's auto-increment keys.
		return "integer", true
	case hasPrefix("float", "double", "real", "numeric", "decimal"):
		return "number", true
	case hasPrefix("string", "text", "varchar", "char", "uuid", "character"):
		return "string", true
	default:
		return "string", false
	}
}

// The generated model's YAML shape: the subset of the modelith schema
// codetospec writes, in schema order.
type (
	modelYAML struct {
		Kind        string                `yaml:"kind"`
		Version     string                `yaml:"version"`
		Title       string                `yaml:"title"`
		Description string                `yaml:"description"`
		Entities    map[string]entityYAML `yaml:"entities"`
	}
	entityYAML struct {
		Definition    string             `yaml:"definition"`
		Attributes    []attributeYAML    `yaml:"attributes,omitempty"`
		Relationships []relationshipYAML `yaml:"relationships,omitempty"`
		Invariants    []invariantYAML    `yaml:"invariants,omitempty"`
	}
	relationshipYAML struct {
		Entity      string `yaml:"entity"`
		Cardinality string `yaml:"cardinality"`
		Ownership   string `yaml:"ownership"`
	}
	attributeYAML struct {
		Name        string `yaml:"name"`
		Type        string `yaml:"type"`
		Description string `yaml:"description,omitempty"`
	}
	invariantYAML struct {
		ID        string `yaml:"id"`
		Statement string `yaml:"statement"`
	}
)

// encodeGenerated encodes a generated model under a header naming its
// schema, then origin, the comment lines saying where it comes from.
func encodeGenerated(m *modelYAML, origin string) ([]byte, error) {
	var body yaml.Node
	if err := body.Encode(m); err != nil {
		return nil, err
	}
	doc := yaml.Node{
		Kind:        yaml.DocumentNode,
		HeadComment: "# yaml-language-server: $schema=" + schema.URL + "\n" + origin,
		Content:     []*yaml.Node{&body},
	}
	return encode(&doc)
}

// yamlIndent is the indentation of written models, modelith's own.
const yamlIndent = 2

// encode writes a YAML document with modelith's indentation.
func encode(doc *yaml.Node) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(yamlIndent)
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// mappingValue is the value of key in a mapping node; nil when absent.
func mappingValue(mapping *yaml.Node, key string) *yaml.Node {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1]
		}
	}
	return nil
}

// setMappingValue replaces the scalar value of key, adding it when absent.
func setMappingValue(mapping *yaml.Node, key, value string) {
	if v := mappingValue(mapping, key); v != nil {
		v.Kind, v.Tag, v.Value, v.Style = yaml.ScalarNode, yamlString, value, 0
		return
	}
	mapping.Content = append(mapping.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: yamlString, Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: yamlString, Value: value})
}

// appendEncoded appends value, encoded as a node, to the sequence under
// key in mapping, creating the sequence on first use; it returns the
// value's index.
func appendEncoded(mapping *yaml.Node, key string, value any) (int, error) {
	var n yaml.Node
	if err := n.Encode(value); err != nil {
		return 0, err
	}
	seq := mappingValue(mapping, key)
	if seq == nil {
		seq = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		mapping.Content = append(mapping.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: yamlString, Value: key}, seq)
	}
	seq.Content = append(seq.Content, &n)
	return len(seq.Content) - 1, nil
}
