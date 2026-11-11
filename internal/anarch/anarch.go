// Package anarch writes the architecture rules codetospec proposes to
// anarch-lint (`export --format anarch`): who may write each enumerated
// field of the state a service stores, from the writers the code has today.
//
// It is the one place that knows anarch-lint's proposals format, pinned
// here at anarch-lint.proposals.v1 (anarch-lint `schema --proposals`, a copy
// in testdata): a file of proposals, each holding exactly one named fitness
// rule and the evidence it rests on. A person reviews them with
// `anarch-lint proposals report` and adds the ones they want with
// `proposals accept`; a rejected one is accepted with its gate off, so a
// rule the model declares, accepted or rejected, is never proposed again.
//
// A proposal states what the code does today, not what it should do. It
// says so in its reason, which opens like a definition codetospec drafts
// from the code.
package anarch

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/graph"
)

// FileName is the proposals file export writes.
const FileName = "anarch-proposals.json"

// The proposals format.
const (
	schemaName = "anarch-lint.proposals.v1"
	source     = "codetospec"
	// qualityAttribute is what a writers rule protects: whoever may change
	// a state is a matter of keeping the code changeable, in anarch-lint's
	// (ISO 25010) terms.
	qualityAttribute = "maintainability"
	// idLength is how many hex digits of the rule's hash name a proposal.
	idLength = 16
)

// File is a proposals file.
type File struct {
	Schema    string     `json:"schema"`
	Source    string     `json:"source"`
	Proposals []Proposal `json:"proposals"`
}

// Proposal is one proposed rule and its evidence.
type Proposal struct {
	ID       string     `json:"id"`
	Fitness  Fitness    `json:"fitness"`
	Evidence []Evidence `json:"evidence,omitempty"`
}

// Fitness holds a proposal's one rule.
type Fitness struct {
	Fields []FieldRule `json:"fields"`
}

// FieldRule is an anarch-lint fields rule: only the From packages may
// write the Fields. Optional, because a rule whose every writer later
// becomes a method of the field's own type matches nothing, which is the
// state kept to itself, not a broken rule.
type FieldRule struct {
	Name             string   `json:"name"`
	Reason           string   `json:"reason"`
	Optional         bool     `json:"optional"`
	QualityAttribute string   `json:"qualityAttribute"`
	Fields           []string `json:"fields"`
	Writes           bool     `json:"writes"`
	Only             bool     `json:"only"`
	From             []string `json:"from"`
}

// Evidence is a write the rule allows (Path, Lines) or a rule of the spec
// citing the writing code (ID).
type Evidence struct {
	Kind  string `json:"kind"`
	Path  string `json:"path,omitempty"`
	Lines string `json:"lines,omitempty"`
	ID    string `json:"id,omitempty"`
}

// The kinds of evidence.
const (
	EvidenceWrite = "write"
	EvidenceRule  = "rule"
)

// storedType is a struct a table fact says is stored: its import path and
// name.
type storedType struct {
	pkgPath, name string
}

// Propose returns one proposal per enumerated field of a stored struct that
// the code writes: a fields rule letting only today's writing packages
// write it. A struct is stored when a table fact (a Datastore kind) names
// it (AttrStoredBy, else the kind's name) in the package of the file
// declaring the kind; the writes facts name the fields and their writers,
// and the package facts resolve directories to import paths. A rule the
// model declares (declared) is left out, and so is a write from a file no
// package fact resolves or from another root. Each proposal's evidence is
// its writes, then the rules whose citations overlap a writing function.
//
// Ensures: sorted by rule name; ids are the first idLength hex digits of
// the rule's SHA-256, so the same writers give the same id.
func Propose(facts []extract.Fact, nodes []graph.Node, declared map[string]bool) File {
	pkgOf := packageDirs(facts)
	writes := storedWrites(facts, storedTypes(facts, pkgOf))
	rules := ruleSpans(nodes)
	out := File{Schema: schemaName, Source: source, Proposals: []Proposal{}}
	for field, ws := range writes {
		p, ok := proposal(field, ws, pkgOf, citing(rules, ws))
		if ok && !declared[p.Fitness.Fields[0].Name] {
			out.Proposals = append(out.Proposals, p)
		}
	}
	slices.SortFunc(out.Proposals, func(a, b Proposal) int {
		return cmp.Compare(a.Fitness.Fields[0].Name, b.Fitness.Fields[0].Name)
	})
	return out
}

// storedTypes maps each struct a table fact of the analyzed code says is
// stored (AttrStoredBy, else the kind's name, in the package of the file
// declaring the kind) to that table fact.
func storedTypes(facts []extract.Fact, pkgOf map[string]string) map[storedType]*extract.Fact {
	stored := map[storedType]*extract.Fact{}
	for i := range facts {
		f := &facts[i]
		if f.Kind != extract.KindTable || f.Source.Root != "" {
			continue
		}
		if pkgPath, ok := pkgOf[path.Dir(f.Source.Path)]; ok {
			name := cmp.Or(f.Attrs[extract.AttrStoredBy], f.Attrs[extract.AttrName])
			stored[storedType{pkgPath, name}] = f
		}
	}
	return stored
}

// storedWrites groups the writes facts of the analyzed code by field
// (pkg.Type.Field), keeping only the fields of stored structs, in fact
// order.
func storedWrites(
	facts []extract.Fact,
	stored map[storedType]*extract.Fact,
) map[string][]*extract.Fact {
	writes := map[string][]*extract.Fact{}
	for i := range facts {
		f := &facts[i]
		if f.Kind != extract.KindWrites || f.Source.Root != "" {
			continue
		}
		field := f.Attrs[extract.AttrField]
		_, typeAndField, _ := strings.Cut(field, ".")
		typeName, _, _ := strings.Cut(typeAndField, ".")
		if stored[storedType{f.Attrs[extract.AttrFieldPackage], typeName}] != nil {
			writes[field] = append(writes[field], f)
		}
	}
	return writes
}

// proposal is the writers rule of field (pkg.Type.Field), written by ws,
// with the rules citing the writing code; false when no write resolves to
// a package.
func proposal(
	field string,
	ws []*extract.Fact,
	pkgOf map[string]string,
	rules []string,
) (Proposal, bool) {
	var from []string
	var evidence []Evidence
	for _, w := range ws {
		pkgPath, ok := pkgOf[path.Dir(w.Source.Path)]
		if !ok {
			continue
		}
		if !slices.Contains(from, pkgPath) {
			from = append(from, pkgPath)
		}
		evidence = append(evidence, Evidence{
			Kind: EvidenceWrite, Path: w.Source.Path, Lines: w.Source.Lines,
		})
	}
	if len(from) == 0 {
		return Proposal{}, false
	}
	slices.Sort(from)
	slices.SortFunc(evidence, func(a, b Evidence) int {
		return cmp.Or(cmp.Compare(a.Path, b.Path), cmp.Compare(a.Lines, b.Lines))
	})
	for _, id := range rules {
		evidence = append(evidence, Evidence{Kind: EvidenceRule, ID: id})
	}
	_, typeAndField, _ := strings.Cut(field, ".")
	rule := FieldRule{
		Name: field + " writers",
		Reason: fmt.Sprintf("Draft, from the code: only these packages write %s today "+
			"(%d write(s); %d rule(s) of the spec cite the writing code).",
			field, len(ws), len(rules)),
		Optional:         true,
		QualityAttribute: qualityAttribute,
		Fields:           []string{ws[0].Attrs[extract.AttrFieldPackage] + "." + typeAndField},
		Writes:           true,
		Only:             true,
		From:             from,
	}
	return Proposal{
		ID: ruleID(&rule), Fitness: Fitness{Fields: []FieldRule{rule}}, Evidence: evidence,
	}, true
}

// ruleID is the first idLength hex digits of the rule's SHA-256.
func ruleID(r *FieldRule) string {
	src, _ := json.Marshal(r) // a FieldRule always encodes
	sum := sha256.Sum256(src)
	return hex.EncodeToString(sum[:])[:idLength]
}

// packageDirs maps each directory of the analyzed code (not another root)
// holding a package to its import path, from the package facts.
func packageDirs(facts []extract.Fact) map[string]string {
	out := map[string]string{}
	for i := range facts {
		f := &facts[i]
		if f.Kind == extract.KindPackage && f.Source.Root == "" {
			out[path.Dir(f.Source.Path)] = f.Attrs[extract.AttrName]
		}
	}
	return out
}

// ruleSpan is a rule and the spans its citations cover in the analyzed
// code.
type ruleSpan struct {
	id    string
	spans []extract.Span
}

// ruleSpans lists the rules with their cited spans, in node order.
func ruleSpans(nodes []graph.Node) []ruleSpan {
	var out []ruleSpan
	for i := range nodes {
		if nodes[i].Type != graph.TypeRule {
			continue
		}
		r := ruleSpan{id: nodes[i].ID}
		for _, src := range nodes[i].Sources {
			if s, ok := extract.SpanOf(src); ok {
				r.spans = append(r.spans, s)
			}
		}
		out = append(out, r)
	}
	return out
}

// citing returns the ids of the rules citing code of a writing function,
// sorted: the rules that say what the writes do.
func citing(rules []ruleSpan, ws []*extract.Fact) []string {
	var writes []extract.Span
	for _, w := range ws {
		if s, ok := extract.SpanOf(w.Source); ok {
			writes = append(writes, s)
		}
	}
	var ids []string
	for _, r := range rules {
		if slices.ContainsFunc(r.spans, func(s extract.Span) bool {
			return slices.ContainsFunc(writes, s.Overlaps)
		}) {
			ids = append(ids, r.id)
		}
	}
	slices.Sort(ids)
	return ids
}

// Marshal is the proposals file's text: indented JSON, ending in a newline.
func Marshal(f *File) []byte {
	src, _ := json.MarshalIndent(f, "", "  ") // a File always encodes
	return append(src, '\n')
}
