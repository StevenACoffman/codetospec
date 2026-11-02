// Package quire exports a graph's rules as ISO 29148 requirements in the
// Quire format, so a team can carry a spec recovered from existing code
// into quoin's authoring and review workflow.
//
// The format is that of the spec-artifacts-iso module, version 0.3.0
// (agent-ix/spec-artifacts-iso at 9f17d63). An FR: frontmatter schema
// schemas/fr-frontmatter.schema.json, required sections Description,
// Acceptance Criteria (an ID | Criteria | Verification table, ids
// <doc-id>-AC-<n>) and Dependencies. An NFR: schemas/nfr-frontmatter.
// schema.json (quality_attribute from its enum), required sections
// Statement, Measurement and Evaluation (a Metric | Target | Threshold |
// Method table of at least one row, every cell non-empty) and
// Verification; Acceptance Criteria and Dependencies optional. A later
// version of the module may change them; this package is the one place
// that knows them.
//
// A technical rule serving a quality (quality.IsNFR) is an NFR; every
// other rule an FR, a business rule serving a quality included (an
// authorization rule is what the system does). The code rarely states an
// NFR's target: the Measurement row then says it is unknown and names the
// question (questions.KindNFRTarget) a person answers, in targets.yaml
// (package nfrtarget). A target set there wins over one the code states, the
// person's target being the requirement; each kind maps to the fields the
// NFR skeleton gives it: a measure fills the Measurement row (Planguage's
// scale, plan, must and meter), a policy becomes an Acceptance Criteria row
// (the skeleton's place for a policy NFR), an example the reference case
// of the row. Enumerations are not exported: they map to object types of
// another module.
package quire

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/StevenACoffman/codetospec/internal/crosscheck"
	"github.com/StevenACoffman/codetospec/internal/graph"
	"github.com/StevenACoffman/codetospec/internal/nfrtarget"
	"github.com/StevenACoffman/codetospec/internal/quality"
	"github.com/StevenACoffman/codetospec/internal/questions"
	"gopkg.in/yaml.v3"
)

// Directory is where the FRs and their index go, relative to the export
// root: Quire's layout for functional requirements. NFRDirectory is the
// same for non-functional requirements.
const (
	Directory    = "functional"
	NFRDirectory = "non-functional"
)

// codetospec_threshold values of an NFR: its code states the threshold, a
// person set it (targets.yaml), or a person has to (codetospec_question
// names the question).
const (
	ThresholdStated  = "stated"
	ThresholdSet     = "set"
	ThresholdUnknown = "unknown"
)

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
	Files     map[string][]byte
	Refuted   []string // rule ids, sorted
	FRs, NFRs int      // documents written of each kind
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
	Criteria   string `yaml:"codetospec_criteria,omitempty"`  // CriteriaFromRequirement
	Threshold  string `yaml:"codetospec_threshold,omitempty"` // NFR: ThresholdStated, Set or Unknown
	Target     string `yaml:"codetospec_target,omitempty"`    // NFR, ThresholdSet: the nfrtarget.Kind
	// Question names, for an NFR, the question asking for its target, or,
	// when the code changed since a person set it, the one asking whether
	// it still holds.
	Question string `yaml:"codetospec_question,omitempty"`
	// Observable names the criterion that is the acceptance pass's
	// observable ("FR-001-AC-3"); NonClaims what the rule does not claim,
	// which as criteria would read as requirements to test an absence.
	Observable string   `yaml:"codetospec_observable,omitempty"`
	NonClaims  []string `yaml:"codetospec_non_claims,omitempty"`
}

// indexFrontmatter is the index document's frontmatter.
type indexFrontmatter struct {
	Type        string `yaml:"type"`
	Title       string `yaml:"title"`
	Description string `yaml:"description"`
}

// Export renders the rules of nodes as FRs numbered FR-001… and NFRs
// numbered NFR-001…, each in rule id order (so by domain), with an index
// per kind.
//
// Requires: nodes as render.LoadDir reads them; nfrTargets from
// nfrtarget.Load of the same output (nil when none).
// Ensures:  one file per rule not refuted, plus Directory/index.md, and
// NFRDirectory/index.md when there is an NFR; the same inputs give the
// same bytes.
func Export(nodes []graph.Node, nfrTargets map[string]nfrtarget.Target) (Spec, error) {
	var rules []*graph.Node
	for i := range nodes {
		if nodes[i].Type == graph.TypeRule {
			rules = append(rules, &nodes[i])
		}
	}
	slices.SortFunc(rules, func(a, b *graph.Node) int { return cmp.Compare(a.ID, b.ID) })

	export := Spec{Files: map[string][]byte{}}
	var frIndex, nfrIndex strings.Builder
	for _, rule := range rules {
		if rule.Extra["crosscheck"] == crosscheck.VerdictUnsupported {
			export.Refuted = append(export.Refuted, rule.ID)
			continue
		}
		dir, prefix, count, index, write := Directory, "FR", &export.FRs, &frIndex, frDocument
		if quality.IsNFR(rule) {
			dir, prefix, count, index = NFRDirectory, "NFR", &export.NFRs, &nfrIndex
			write = func(id string, rule *graph.Node) ([]byte, error) {
				return nfrDocument(id, rule, targetOf(nfrTargets, rule.ID))
			}
		}
		*count++
		id := fmt.Sprintf("%s-%03d", prefix, *count)
		name := id + "-" + strings.ReplaceAll(
			strings.TrimPrefix(rule.ID, graph.TypeRule+"."),
			".",
			"-",
		) + ".md"
		doc, err := write(id, rule)
		if err != nil {
			return Spec{}, err
		}
		export.Files[dir+"/"+name] = doc
		fmt.Fprintf(index, "* [%s: %s](./%s)\n", id, oneLine(rule.Title), name)
	}
	indexes := []struct {
		dir, title, description string
		entries                 *strings.Builder
	}{
		{
			Directory, "Functional Requirements",
			"Functional requirements recovered from the code by codetospec.", &frIndex,
		},
		{
			NFRDirectory, "Non-Functional Requirements",
			"Non-functional requirements recovered from the code by codetospec.", &nfrIndex,
		},
	}
	for _, ix := range indexes {
		if ix.dir == NFRDirectory && export.NFRs == 0 {
			continue
		}
		front, err := yaml.Marshal(indexFrontmatter{
			Type: "index", Title: ix.title, Description: ix.description,
		})
		if err != nil {
			return Spec{}, fmt.Errorf("encode index frontmatter: %w", err)
		}
		export.Files[ix.dir+"/index.md"] = fmt.Appendf(nil,
			"---\n%s---\n\n# %s\n\n## Contents\n\n%s", front, ix.title, ix.entries.String())
	}
	return export, nil
}

// frDocument renders one rule as the FR id.
func frDocument(id string, rule *graph.Node) ([]byte, error) {
	requirement := graph.RuleRequirement(rule.Body)
	criteria := ruleCriteria(rule)
	front := baseFrontmatter(id, "FR", rule)
	if len(criteria) == 0 {
		criteria, front.Criteria = []string{requirement}, CriteriaFromRequirement
	}
	if observable := graph.RuleObservable(rule.Body); observable != "" {
		criteria = append(criteria, observable)
		front.Observable = fmt.Sprintf("%s-AC-%d", id, len(criteria))
	}
	var b strings.Builder
	if err := writeHeading(&b, &front); err != nil {
		return nil, err
	}
	fmt.Fprintf(&b, "## Description\n\n%s\n\n", requirement)
	writeCriteria(&b, id, tested(criteria))
	b.WriteString("\n")
	writeDependencies(&b, rule)
	return []byte(b.String()), nil
}

// nfrDocument renders one non-functional rule as the NFR id: its
// requirement as the Statement, and one Measurement row whose target and
// threshold are those a person set (target, nil when none), else those the
// code states, else unknown with the question asking a person for them.
func nfrDocument(id string, rule *graph.Node, target *nfrtarget.Target) ([]byte, error) {
	front := baseFrontmatter(id, "NFR", rule)
	m := nfrMeasurement(rule, target)
	front.Threshold, front.Target, front.Question = m.threshold, m.kind, m.question
	criteria := tested(ruleCriteria(rule))
	if m.policy != "" {
		criteria = append(criteria, criterion{text: m.policy, verification: "Inspection"})
		m.target = fmt.Sprintf("Compliance with the policy (%s-AC-%d)", id, len(criteria))
		m.thresholdCell = m.target
	}
	var b strings.Builder
	if err := writeHeading(&b, &front); err != nil {
		return nil, err
	}
	fmt.Fprintf(&b, "## Statement\n\n%s\n\n", graph.RuleRequirement(rule.Body))
	b.WriteString("## Measurement and Evaluation\n\n" +
		"| Metric | Target | Threshold | Method |\n|--------|--------|-----------|--------|\n")
	fmt.Fprintf(&b, "| %s | %s | %s | %s |\n\n",
		cell(cmp.Or(m.metric, front.Title)), cell(m.target), cell(m.thresholdCell), cell(m.method))
	fmt.Fprintf(&b, "## Verification\n\n%s\n\n", m.verification)
	if len(criteria) > 0 {
		writeCriteria(&b, id, criteria)
		b.WriteString("\n")
	}
	writeDependencies(&b, rule)
	return []byte(b.String()), nil
}

// measurement is what an NFR's Measurement and Verification sections say:
// the threshold a person set or the code states, or that a person has to
// set it.
type measurement struct {
	threshold, kind, question string // codetospec_threshold, codetospec_target, codetospec_question
	// The Measurement row; metric "" is the document's title.
	metric, target, thresholdCell, method string
	verification                          string
	// policy is a policy target, written as an Acceptance Criteria row the
	// target cell points to.
	policy string
}

// nfrMeasurement is rule's measurement: the target a person set (target,
// nil when none), else the threshold its code states
// (graph.ExtraThreshold), else unknown, naming the question asking for it.
func nfrMeasurement(rule *graph.Node, target *nfrtarget.Target) measurement {
	if target != nil {
		return setMeasurement(rule, target)
	}
	if stated := rule.Extra[graph.ExtraThreshold]; stated != "" {
		inCode := stated + " (stated in the code)"
		return measurement{
			threshold: ThresholdStated,
			target:    inCode, thresholdCell: inCode,
			method: "Inspection of the cited code",
			verification: "Inspection: the cited code sets the threshold, and codetospec's " +
				"grounding check found each stated value in it.",
		}
	}
	q := questionOf(rule, questions.KindNFRTarget)
	unknown := "Unknown: the code states none (question " + q + ")"
	return measurement{
		threshold: ThresholdUnknown,
		question:  q,
		target:    unknown, thresholdCell: unknown,
		method: "To be decided with the target",
		verification: "Not verifiable until a person sets the target and threshold: " +
			"question " + q + " in questions.json.",
	}
}

// setMeasurement is the measurement of the target a person set for rule,
// naming the stale_target question when the code changed since.
func setMeasurement(rule *graph.Node, target *nfrtarget.Target) measurement {
	m := measurement{
		threshold: ThresholdSet,
		kind:      string(target.Kind()),
		question:  questionOf(rule, questions.KindStaleTarget),
	}
	switch target.Kind() {
	case nfrtarget.KindMeasure:
		measure := target.Measure
		m.metric, m.method = measure.Scale, measure.Meter
		m.target = cmp.Or(measure.Plan, measure.Must)
		m.thresholdCell = cmp.Or(measure.Must, measure.Plan)
		m.verification = "Measurement: " + measure.Meter + ", against the target set in targets.yaml."
	case nfrtarget.KindPolicy:
		m.policy = target.Policy
		m.method = "Review against the policy"
		m.verification = "Inspection: a reviewer checks the code against the policy set in " +
			"targets.yaml (the Acceptance Criteria below)."
	case nfrtarget.KindExample:
		m.target = "As the reference case: " + target.Example
		m.thresholdCell = "No worse than the reference case"
		m.method = "Comparison with the reference case"
		m.verification = "Demonstration: the system is compared with the reference case set in " +
			"targets.yaml."
	}
	return m
}

// targetOf is the target set for the rule id, nil when none.
func targetOf(nfrTargets map[string]nfrtarget.Target, id string) *nfrtarget.Target {
	if t, ok := nfrTargets[id]; ok {
		return &t
	}
	return nil
}

// questionOf is the id of rule's question of kind, "" when it raises none.
func questionOf(rule *graph.Node, kind string) string {
	for _, q := range questions.List([]graph.Node{*rule}, nil) {
		if q.Kind == kind {
			return q.ID
		}
	}
	return ""
}

// baseFrontmatter is what an FR's and an NFR's frontmatter share.
func baseFrontmatter(id, kind string, rule *graph.Node) frontmatter {
	front := frontmatter{
		ID:         id,
		Title:      oneLine(rule.Title),
		Type:       kind,
		Rule:       rule.ID,
		Crosscheck: rule.Extra["crosscheck"],
		NonClaims:  graph.RuleNonClaims(rule.Body),
	}
	if attribute := rule.Extra[graph.ExtraQualityAttribute]; quality.IsAttribute(attribute) {
		front.Quality = attribute
	}
	return front
}

// writeHeading writes front as the document's frontmatter, then its title.
func writeHeading(b *strings.Builder, front *frontmatter) error {
	data, err := yaml.Marshal(front)
	if err != nil {
		return fmt.Errorf("encode %s frontmatter: %w", front.ID, err)
	}
	fmt.Fprintf(b, "---\n%s---\n\n# %s: %s\n\n", data, front.ID, front.Title)
	return nil
}

// ruleCriteria are the rule's acceptance criteria, as plain text.
func ruleCriteria(rule *graph.Node) []string {
	criteria := graph.RuleCriteria(rule.Body)
	for k := range criteria {
		criteria[k] = graph.CriterionText(criteria[k])
	}
	return criteria
}

// criterion is one Acceptance Criteria row: its text and how it is
// verified (Test, Inspection).
type criterion struct{ text, verification string }

// tested is criteria as rows verified by test.
func tested(criteria []string) []criterion {
	rows := make([]criterion, len(criteria))
	for k, text := range criteria {
		rows[k] = criterion{text: text, verification: "Test"}
	}
	return rows
}

// writeCriteria writes the Acceptance Criteria table, ids <id>-AC-<n>.
func writeCriteria(b *strings.Builder, id string, criteria []criterion) {
	b.WriteString(
		"## Acceptance Criteria\n\n| ID | Criteria | Verification |\n|----|----------|--------------|\n",
	)
	for k, row := range criteria {
		fmt.Fprintf(b, "| %s-AC-%d | %s | %s |\n", id, k+1, cell(row.text), row.verification)
	}
}

// writeDependencies writes the Dependencies section: the rule's domain,
// the code it cites and the tests that reach it.
func writeDependencies(b *strings.Builder, rule *graph.Node) {
	b.WriteString("## Dependencies\n\n")
	if domain := graph.RuleDomain(rule); domain != "" {
		fmt.Fprintf(b, "- **Domain**: `%s`\n", domain)
	}
	var code []string
	for _, src := range rule.Sources {
		code = append(code, "`"+src.File()+":"+src.Lines+"`")
	}
	fmt.Fprintf(b, "- **Code**: %s\n", strings.Join(code, ", "))
	if tests := rule.Extra[graph.ExtraTestedBy]; tests != "" {
		fmt.Fprintf(b, "- **Tests**: `%s`\n", strings.ReplaceAll(tests, ", ", "`, `"))
	}
}

// cell makes text safe in a table cell: one line, pipes escaped.
func cell(text string) string {
	return strings.ReplaceAll(oneLine(text), "|", `\|`)
}

// oneLine joins text's lines with spaces.
func oneLine(text string) string {
	return strings.Join(strings.Fields(text), " ")
}
