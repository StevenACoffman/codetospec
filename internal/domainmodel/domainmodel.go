// Package domainmodel compares human-written modelith domain models
// (*.modelith.yaml) with the graph codetospec built, without an LLM: which
// model entities the code has, which invariants a rule states, and which
// rules about modeled entities the model does not state. It only reads the
// models; nothing is written back.
package domainmodel

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	"github.com/StevenACoffman/codetospec/internal/graph"
	"github.com/StevenACoffman/codetospec/internal/textsim"
	"github.com/StevenACoffman/indomain/pkg/lint"
	"github.com/StevenACoffman/indomain/pkg/model"
)

// DefaultThreshold is the word-set similarity at which a rule is taken to
// state an invariant. Invariants are prose and rules EARS, so they share
// fewer words than two rules do (consistency.DefaultThreshold is 0.8); the
// value is a first guess, to be measured on real models.
const DefaultThreshold = 0.3

// Set is the loaded models, in the order given.
type Set struct {
	Models []Model
	// Warnings counts the lint warnings across the models (semantic and
	// completeness); errors fail Load instead.
	Warnings int
}

// Model is one loaded model file.
type Model struct {
	Path   string
	Scope  string // the scope an import of this file binds: its basename
	Model  *model.Model
	Source []byte // the file as read, for editing it without losing comments
}

// Load reads and lints the model files, then parses them. Any lint error,
// in a model or in a model it imports, fails the load: an invalid model
// must not steer the comparison.
//
// Requires: paths name modelith model files (the user's --domain-model).
// Ensures:  on success, Set.Models holds one parsed model per path, in
// order, and Set.Warnings counts every non-error lint finding.
func Load(paths []string) (*Set, error) {
	inputs := make([]lint.Input, 0, len(paths))
	for _, p := range paths {
		src, err := os.ReadFile(filepath.Clean(p))
		if err != nil {
			return nil, fmt.Errorf("read domain model: %w", err)
		}
		inputs = append(inputs, lint.Input{Path: p, Source: src})
	}
	results, err := lint.Plan(inputs, nil)
	if err != nil {
		return nil, fmt.Errorf("lint domain models: %w", err)
	}
	set := &Set{}
	var errs []string
	for _, r := range results {
		for _, f := range r.Findings {
			if f.Severity == lint.SeverityError {
				errs = append(errs, fmt.Sprintf("%s:%s: %s", r.File, f.Path, f.Message))
			} else {
				set.Warnings++
			}
		}
	}
	if len(errs) > 0 {
		return nil, fmt.Errorf("domain models have %d lint error(s):\n  %s",
			len(errs), strings.Join(errs, "\n  "))
	}
	for _, in := range inputs {
		m, parseErr := model.Parse(in.Source)
		if parseErr != nil {
			return nil, fmt.Errorf("parse domain model %s: %w", in.Path, parseErr)
		}
		set.Models = append(
			set.Models,
			Model{
				Path: in.Path, Scope: model.ScopeFromPath(in.Path), Model: m, Source: in.Source,
			},
		)
	}
	return set, nil
}

// Report is the comparison of the models with the graph. Every list is
// sorted, so a report is deterministic for given models and nodes.
type Report struct {
	Models   []string // model file paths, as given
	Warnings int      // lint warnings across the models
	// Entities lists every model entity, by qualified name.
	Entities []Entity
	// Invariants lists the model-level invariants (no single owner),
	// compared with every rule.
	Invariants []Invariant
	// Unmodeled lists the code entities no model entity matches.
	Unmodeled []CodeEntity
	// StaleMappings lists configured mappings that matched nothing, as
	// "Model → code".
	StaleMappings []string
}

// Entity is one model entity and what the code has of it.
type Entity struct {
	Name string // scope.Entity
	// Code is the matched code entity; empty when none or several match.
	Code CodeEntity
	// Ambiguous lists the code entities matching equally well, when more
	// than one does.
	Ambiguous  []CodeEntity
	Invariants []Invariant
	// Unstated lists the rules touching Code that no invariant of the
	// entity, nor of its model, matches.
	Unstated []string
}

// CodeEntity is an entity node of the graph.
type CodeEntity struct {
	ID, Title string
}

// Invariant is a model invariant and the rule closest to it.
type Invariant struct {
	Owner string // scope.Entity, or the scope for a model-level invariant
	ID    string // the invariant id, unique within its model
	// Rule is the closest rule when at or above the threshold: the rule
	// taken to state the invariant. Empty otherwise.
	Rule string
	// Closest is the most similar rule, even below the threshold, and
	// Similarity its similarity; empty and 0 when no rule shares a word.
	Closest    string
	Similarity float64
}

// Options tunes Compare.
type Options struct {
	// Entities maps a model entity ("scope.Entity" or bare "Entity") to the
	// code entity it is, by title or node id, for names that differ.
	Entities map[string]string
	// Threshold overrides DefaultThreshold when positive.
	Threshold float64
}

// rule is a rule node with what Compare needs of it.
type rule struct {
	id      string
	words   map[string]bool
	touches []string // entity node ids
}

// Compare matches the models' entities to the graph's entity nodes by
// name (case and punctuation ignored, a regular plural accepted, or as
// Options.Entities maps them), then each invariant to the most similar rule
// touching its entity.
//
// Requires: set comes from Load.
// Ensures:  every model entity appears once in Report.Entities; every code
// entity no model entity matches appears in Report.Unmodeled; all lists are
// sorted, so the report depends on neither node nor map order.
func Compare(set *Set, nodes []graph.Node, opts Options) Report {
	threshold := opts.Threshold
	if threshold <= 0 {
		threshold = DefaultThreshold
	}
	code, rules := index(nodes)
	report := Report{Warnings: set.Warnings}
	matched := map[string]bool{}
	usedMapping := map[string]bool{}
	for _, m := range set.Models {
		report.Models = append(report.Models, m.Path)
		modelLevel := words(m.Model.Invariants)
		for _, name := range m.Model.EntityNames() {
			e := Entity{Name: m.Scope + "." + name}
			candidates, key := matchEntity(e.Name, name, code, opts.Entities)
			if key != "" {
				usedMapping[key] = true
				if len(candidates) == 0 {
					report.StaleMappings = append(
						report.StaleMappings,
						key+" → "+opts.Entities[key],
					)
				}
			}
			switch len(candidates) {
			case 0:
			case 1:
				e.Code = candidates[0]
				matched[e.Code.ID] = true
			default:
				e.Ambiguous = candidates
			}
			own := words(m.Model.Entities[name].Invariants)
			touching := rulesTouching(rules, e.Code.ID)
			e.Invariants = closest(e.Name, own, touching, threshold)
			e.Unstated = unstated(touching, slices.Concat(own, modelLevel), threshold)
			report.Entities = append(report.Entities, e)
		}
		report.Invariants = append(
			report.Invariants,
			closest(m.Scope, modelLevel, rules, threshold)...)
	}
	for key, target := range opts.Entities {
		if !usedMapping[key] {
			report.StaleMappings = append(report.StaleMappings, key+" → "+target)
		}
	}
	for _, c := range code {
		if !matched[c.ID] {
			report.Unmodeled = append(report.Unmodeled, c)
		}
	}
	slices.SortFunc(report.Entities, func(a, b Entity) int { return cmp.Compare(a.Name, b.Name) })
	slices.SortFunc(report.Invariants, func(a, b Invariant) int {
		return cmp.Or(cmp.Compare(a.Owner, b.Owner), cmp.Compare(a.ID, b.ID))
	})
	slices.Sort(report.StaleMappings)
	// A bare mapping key shared by two models' entities is reported once.
	report.StaleMappings = slices.Compact(report.StaleMappings)
	return report
}

// index returns the graph's entity nodes, sorted by id, and its rules.
func index(nodes []graph.Node) ([]CodeEntity, []rule) {
	var code []CodeEntity
	var rules []rule
	for _, n := range nodes {
		switch n.Type {
		case graph.TypeEntity:
			code = append(code, CodeEntity{ID: n.ID, Title: n.Title})
		case graph.TypeRule:
			r := rule{id: n.ID, words: contentWords(graph.RuleRequirement(n.Body))}
			for _, e := range n.Edges {
				if e.Type == graph.EdgeTouches {
					r.touches = append(r.touches, e.To)
				}
			}
			rules = append(rules, r)
		}
	}
	slices.SortFunc(code, func(a, b CodeEntity) int { return cmp.Compare(a.ID, b.ID) })
	slices.SortFunc(rules, func(a, b rule) int { return cmp.Compare(a.id, b.id) })
	return code, rules
}

// matchEntity returns the code entities a model entity names, and the
// Options.Entities key that mapped it ("" when none did). A mapping is
// matched against titles and ids exactly; a plain name, normalized, then
// in its plural forms.
func matchEntity(
	qualified, name string,
	code []CodeEntity,
	mapping map[string]string,
) ([]CodeEntity, string) {
	for _, key := range []string{qualified, name} {
		target, ok := mapping[key]
		if !ok {
			continue
		}
		var found []CodeEntity
		for _, c := range code {
			if c.Title == target || c.ID == target {
				found = append(found, c)
			}
		}
		return found, key
	}
	// Tables are often named in the plural ("invoices") and model entities
	// in the singular: the plural forms count when no exact name does.
	singular := normalize(name)
	for _, forms := range [][]string{{singular}, plurals(singular)} {
		var found []CodeEntity
		for _, c := range code {
			if slices.Contains(forms, normalize(c.Title)) {
				found = append(found, c)
			}
		}
		if len(found) > 0 {
			return found, ""
		}
	}
	return nil, ""
}

// plurals returns the regular English plurals of a normalized name:
// invoice → invoices, address → addresses, policy → policies.
func plurals(name string) []string {
	forms := []string{name + "s", name + "es"}
	if base, ok := strings.CutSuffix(name, "y"); ok {
		forms = append(forms, base+"ies")
	}
	return forms
}

// normalize keeps the lowercased letters and digits of s, so "UserProfile",
// "user_profile" and "user-profile" compare equal.
func normalize(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

func rulesTouching(rules []rule, entityID string) []rule {
	if entityID == "" {
		return nil
	}
	var out []rule
	for _, r := range rules {
		if slices.Contains(r.touches, entityID) {
			out = append(out, r)
		}
	}
	return out
}

// isEARSWord reports whether w is a word of the EARS sentences (see
// ears.Render) in a supported language: every rule has them and no prose
// invariant does, so they would only dilute the similarity.
func isEARSWord(w string) bool {
	switch w {
	case "the", "system", "shall", "when", "while", "then", "where",
		"systeme", "système", "doit", "quand", "tant", "que", "alors":
		return true
	}
	return false
}

// contentWords is the word set of s without the EARS template words.
func contentWords(s string) map[string]bool {
	words := textsim.Words(s)
	for w := range words {
		if isEARSWord(w) {
			delete(words, w)
		}
	}
	return words
}

// statement is an invariant with its word set.
type statement struct {
	id    string
	words map[string]bool
}

func words(invariants []model.Invariant) []statement {
	out := make([]statement, 0, len(invariants))
	for _, inv := range invariants {
		out = append(out, statement{id: inv.ID, words: contentWords(inv.Statement)})
	}
	return out
}

// closest pairs each invariant with its most similar rule (ties to the
// smallest id, as rules are sorted); Rule stays empty below threshold.
func closest(owner string, invariants []statement, rules []rule, threshold float64) []Invariant {
	out := make([]Invariant, 0, len(invariants))
	for _, inv := range invariants {
		best := Invariant{Owner: owner, ID: inv.id}
		for _, r := range rules {
			if s := textsim.Jaccard(inv.words, r.words); s > best.Similarity {
				best.Similarity, best.Closest = s, r.id
			}
		}
		if best.Similarity >= threshold {
			best.Rule = best.Closest
		}
		out = append(out, best)
	}
	return out
}

// unstated returns the rules no invariant is similar enough to.
func unstated(rules []rule, invariants []statement, threshold float64) []string {
	var out []string
	for _, r := range rules {
		stated := false
		for _, inv := range invariants {
			if textsim.Jaccard(inv.words, r.words) >= threshold {
				stated = true
				break
			}
		}
		if !stated {
			out = append(out, r.id)
		}
	}
	return out
}
