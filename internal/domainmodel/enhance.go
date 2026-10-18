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
	// Domain is the domain of its declaration, used when no rule touching
	// it says otherwise.
	Domain string
}

// EnhanceInput is what Enhance builds models from.
type EnhanceInput struct {
	Set     *Set // human models; nil when none was given
	Nodes   []graph.Node
	Tables  []Table
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
// invariants for single-entity ubiquitous rules no invariant states. Code
// entities no human model names go into one generated model per domain,
// defined by their description or, failing that, a placeholder.
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
	in      *EnhanceInput
	text    *locale.Text
	tables  map[string]*Table
	rules   map[string][]invariantRule // entity id → its invariant rules, by id
	domains map[string]string          // entity id → domain of its rules
	report  Report
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
	b.domains = ruleDomains(in.Nodes)
	for i := range in.Nodes {
		n := &in.Nodes[i]
		touches := touched(n)
		if n.Type != graph.TypeRule || n.Extra["ears"] != "ubiquitous" || len(touches) != 1 {
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

// ruleDomains gives each entity the domain most of the rules touching it
// belong to (ties to the smallest name).
func ruleDomains(nodes []graph.Node) map[string]string {
	counts := map[string]map[string]int{} // entity id → domain → rules
	for i := range nodes {
		n := &nodes[i]
		if n.Type != graph.TypeRule {
			continue
		}
		domain := ruleDomain(n)
		for _, id := range touched(n) {
			if counts[id] == nil {
				counts[id] = map[string]int{}
			}
			counts[id][domain]++
		}
	}
	domains := map[string]string{}
	for id, byDomain := range counts {
		domains[id] = majority(byDomain)
	}
	return domains
}

// ruleDomain is the domain a rule node belongs to.
func ruleDomain(n *graph.Node) string {
	for _, e := range n.Edges {
		if e.Type == graph.EdgeBelongsTo {
			return strings.TrimPrefix(e.To, "domain.")
		}
	}
	return ""
}

// majority is the key with the highest count, ties to the smallest key.
func majority(counts map[string]int) string {
	best := ""
	for key, n := range counts {
		if n > counts[best] || n == counts[best] && (best == "" || key < best) {
			best = key
		}
	}
	return best
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
	for i := range b.report.Entities {
		if id := b.report.Entities[i].Code.ID; id != "" {
			matched[id] = true
		}
	}
	byDomain := map[string][]*Table{}
	for i := range b.in.Tables {
		t := &b.in.Tables[i]
		if matched[t.ID] {
			continue
		}
		domain := cmp.Or(b.domains[t.ID], t.Domain, "root")
		byDomain[domain] = append(byDomain[domain], t)
	}
	domains := make([]string, 0, len(byDomain))
	for domain := range byDomain {
		domains = append(domains, domain)
	}
	slices.Sort(domains)
	for _, domain := range domains {
		if err := b.generateDomain(domain, byDomain[domain], out); err != nil {
			return err
		}
	}
	return nil
}

// generateDomain writes the generated model of one domain.
func (b *builder) generateDomain(domain string, tables []*Table, out *Enhanced) error {
	slices.SortFunc(tables, func(a, b *Table) int { return cmp.Compare(a.ID, b.ID) })
	file := "generated/" + domain + ".modelith.yaml"
	prov := map[string]string{}
	doc := modelYAML{
		Kind:        "DomainModel",
		Version:     schema.Current,
		Title:       domain,
		Description: fmt.Sprintf(b.text.GeneratedDescFmt, domain),
		Entities:    map[string]entityYAML{},
	}
	used := map[string]bool{}
	for _, t := range tables {
		name := uniqueName(entityName(t.Title), doc.Entities)
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
	data, err := encodeGenerated(&doc, domain)
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
		Definition string          `yaml:"definition"`
		Attributes []attributeYAML `yaml:"attributes,omitempty"`
		Invariants []invariantYAML `yaml:"invariants,omitempty"`
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
// schema and its origin.
func encodeGenerated(m *modelYAML, domain string) ([]byte, error) {
	var body yaml.Node
	if err := body.Encode(m); err != nil {
		return nil, err
	}
	doc := yaml.Node{
		Kind: yaml.DocumentNode,
		HeadComment: "# yaml-language-server: $schema=" + schema.URL + "\n" +
			"# Generated by codetospec from the code of the domain " + domain + ".\n" +
			"# Edit it as a model of your own: a definition you write is kept.",
		Content: []*yaml.Node{&body},
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
