// Package quire exports a graph's rules as ISO 29148 functional
// requirements in the Quire format, so a team can carry a spec recovered
// from existing code into quoin's authoring and review workflow.
//
// The format is that of the spec-artifacts-iso module, version 0.3.0
// (agent-ix/spec-artifacts-iso at 9f17d63): frontmatter schema
// schemas/fr-frontmatter.schema.json, required sections Description,
// Acceptance Criteria (an ID | Criteria | Verification table, ids
// <doc-id>-AC-<n>) and Dependencies. A later version of the module may
// change them; this package is the one place that knows them.
//
// Only rules are exported, as FRs. An NFR needs a measured target and
// threshold, which code does not state, and enumerations map to object
// types of another module.
package quire

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/StevenACoffman/codetospec/internal/crosscheck"
	"github.com/StevenACoffman/codetospec/internal/graph"
	"github.com/StevenACoffman/codetospec/internal/quality"
	"gopkg.in/yaml.v3"
)

// Directory is where the FRs and their index go, relative to the export
// root: Quire's layout for functional requirements.
const Directory = "functional"

// CriteriaFromRequirement is codetospec_criteria for an FR whose rule
// states no acceptance criteria (a reduce in merge mode keeps such rules):
// the Acceptance Criteria table needs a row, and an EARS requirement is
// itself a testable condition, so it is the one criterion — restated,
// never invented.
const CriteriaFromRequirement = "requirement"

// Spec is the exported spec: file contents by path relative to the
// export root, and the rules left out because the crosscheck refuted them
// (the cited code does not support them).
type Spec struct {
	Files   map[string][]byte
	Refuted []string // rule ids, sorted
}

// frontmatter is an FR's YAML frontmatter, in Quire's key order; the
// codetospec_ keys are extensions the schema admits (additionalProperties).
type frontmatter struct {
	ID         string `yaml:"id"`
	Title      string `yaml:"title"`
	Type       string `yaml:"type"`
	Quality    string `yaml:"quality_attribute,omitempty"`
	Rule       string `yaml:"codetospec_rule"`
	Crosscheck string `yaml:"codetospec_crosscheck,omitempty"`
	Criteria   string `yaml:"codetospec_criteria,omitempty"` // CriteriaFromRequirement
}

// indexFrontmatter is the index document's frontmatter.
type indexFrontmatter struct {
	Type        string `yaml:"type"`
	Title       string `yaml:"title"`
	Description string `yaml:"description"`
}

// Export renders the rules of nodes as FRs numbered FR-001… in rule id
// order (so by domain), with an index.
//
// Requires: nodes as render.LoadDir reads them.
// Ensures:  one file per rule not refuted, plus Directory/index.md; the
// same nodes give the same bytes.
func Export(nodes []graph.Node) (Spec, error) {
	var rules []*graph.Node
	for i := range nodes {
		if nodes[i].Type == graph.TypeRule {
			rules = append(rules, &nodes[i])
		}
	}
	slices.SortFunc(rules, func(a, b *graph.Node) int { return cmp.Compare(a.ID, b.ID) })

	export := Spec{Files: map[string][]byte{}}
	var index strings.Builder
	n := 0
	for _, rule := range rules {
		if rule.Extra["crosscheck"] == crosscheck.VerdictUnsupported {
			export.Refuted = append(export.Refuted, rule.ID)
			continue
		}
		n++
		id := fmt.Sprintf("FR-%03d", n)
		name := id + "-" + strings.ReplaceAll(
			strings.TrimPrefix(rule.ID, graph.TypeRule+"."),
			".",
			"-",
		) + ".md"
		doc, err := document(id, rule)
		if err != nil {
			return Spec{}, err
		}
		export.Files[Directory+"/"+name] = doc
		fmt.Fprintf(&index, "* [%s: %s](./%s)\n", id, oneLine(rule.Title), name)
	}
	front, err := yaml.Marshal(indexFrontmatter{
		Type:        "index",
		Title:       "Functional Requirements",
		Description: "Functional requirements recovered from the code by codetospec.",
	})
	if err != nil {
		return Spec{}, fmt.Errorf("encode index frontmatter: %w", err)
	}
	export.Files[Directory+"/index.md"] = fmt.Appendf(nil,
		"---\n%s---\n\n# Functional Requirements\n\n## Contents\n\n%s", front, index.String())
	return export, nil
}

// document renders one rule as the FR id.
func document(id string, rule *graph.Node) ([]byte, error) {
	requirement := graph.RuleRequirement(rule.Body)
	criteria := graph.RuleCriteria(rule.Body)
	for k := range criteria {
		criteria[k] = unnumbered(criteria[k])
	}
	front := frontmatter{
		ID:         id,
		Title:      oneLine(rule.Title),
		Type:       "FR",
		Rule:       rule.ID,
		Crosscheck: rule.Extra["crosscheck"],
	}
	if len(criteria) == 0 {
		criteria, front.Criteria = []string{requirement}, CriteriaFromRequirement
	}
	if attribute := rule.Extra[graph.ExtraQualityAttribute]; quality.IsAttribute(attribute) {
		front.Quality = attribute
	}
	data, err := yaml.Marshal(front)
	if err != nil {
		return nil, fmt.Errorf("encode %s frontmatter: %w", id, err)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "---\n%s---\n\n# %s: %s\n\n", data, id, front.Title)
	fmt.Fprintf(&b, "## Description\n\n%s\n\n", requirement)
	b.WriteString(
		"## Acceptance Criteria\n\n| ID | Criteria | Verification |\n|----|----------|--------------|\n",
	)
	for k, criterion := range criteria {
		fmt.Fprintf(&b, "| %s-AC-%d | %s | Test |\n", id, k+1, cell(criterion))
	}
	b.WriteString("\n## Dependencies\n\n")
	if domain := domainOf(rule); domain != "" {
		fmt.Fprintf(&b, "- **Domain**: `%s`\n", domain)
	}
	var code []string
	for _, src := range rule.Sources {
		code = append(code, "`"+src.File()+":"+src.Lines+"`")
	}
	fmt.Fprintf(&b, "- **Code**: %s\n", strings.Join(code, ", "))
	if tests := rule.Extra[graph.ExtraTestedBy]; tests != "" {
		fmt.Fprintf(&b, "- **Tests**: `%s`\n", strings.ReplaceAll(tests, ", ", "`, `"))
	}
	return []byte(b.String()), nil
}

// unnumbered is a criterion line without its "1. " prefix.
func unnumbered(line string) string {
	rest := strings.TrimLeft(line, "0123456789")
	if after, found := strings.CutPrefix(rest, "."); found && len(rest) < len(line) {
		return strings.TrimSpace(after)
	}
	return line
}

// domainOf is the domain a rule belongs to, by its belongs_to edge.
func domainOf(rule *graph.Node) string {
	for _, e := range rule.Edges {
		if e.Type == graph.EdgeBelongsTo {
			return strings.TrimPrefix(e.To, graph.TypeDomain+".")
		}
	}
	return ""
}

// cell makes text safe in a table cell: one line, pipes escaped.
func cell(text string) string {
	return strings.ReplaceAll(oneLine(text), "|", `\|`)
}

// oneLine joins text's lines with spaces.
func oneLine(text string) string {
	return strings.Join(strings.Fields(text), " ")
}
