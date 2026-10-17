package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"io/fs"
	"path"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// A service builds on webapp's shared code (pkg/), whose own business rules
// an LLM finds only when pkg/ is scanned as a spec of its own (codetospec
// with --src <webapp>/pkg). inheritFacts brings into the service the rules
// of that spec about code the service reaches (see reached): rule facts
// cited in pkg/ (webappRoot), labeled inherited_from with the spec's verdict,
// never presented as mechanical.

// requirementLabel is the label codetospec writes before a rule's EARS
// requirement in lang (internal/locale's Labels.Requirement).
func requirementLabel(lang string) string {
	if lang == "en" {
		return "**Requirement (EARS)**: "
	}
	return "**Exigence (EARS)** : "
}

// specRule is what a rule node of a spec says: its frontmatter and the
// title and requirement of its body.
type specRule struct {
	ID      string `yaml:"id"`
	Sources []struct {
		Path  string `yaml:"path"`
		Lines string `yaml:"lines"`
		Root  string `yaml:"root"`
	} `yaml:"sources"`
	EARS          string `yaml:"ears"`
	Nature        string `yaml:"nature"`
	Origin        string `yaml:"origin"`
	Confidence    string `yaml:"confidence"`
	Crosscheck    string `yaml:"crosscheck"`
	ExtractedBy   string `yaml:"extracted_by"`
	InheritedFrom string `yaml:"inherited_from"`
	title         string
	requirement   string
}

// inheritFacts returns the rules of spec (a codetospec graph of webapp's
// pkg/, named name) that cite code the service reaches, as inherited rule
// facts. Only the spec's own LLM rules are inherited (not its extractor's,
// nor what it inherited); a rule citing several files is skipped, Fact
// having one citation. Warnings count what could not be inherited.
func inheritFacts(
	spec fs.FS,
	name, lang string,
	shared sharedCode,
	reach map[declKey]bool,
) ([]Fact, []string) {
	rules, warnings := specRules(spec, lang)
	spans := reachedSpans(shared, reach)
	var facts []Fact
	multiFile := 0
	for i := range rules {
		r := &rules[i]
		file, start, end, ok := r.span()
		if !ok {
			multiFile++
			continue
		}
		if file = path.Join(sharedDir, file); spans.overlaps(file, start, end) {
			facts = append(facts, r.fact(name, file, start, end))
		}
	}
	if multiFile > 0 {
		warnings = append(warnings, fmt.Sprintf(
			"inherit %s: %d rules citing several files not inherited", name, multiFile))
	}
	return facts, warnings
}

// specRules parses the LLM rule nodes of spec (nodes/rules/*.md) written in
// lang; a spec in another language is a warning and yields none.
func specRules(spec fs.FS, lang string) ([]specRule, []string) {
	names, err := fs.Glob(spec, "nodes/rules/*.md")
	if err != nil || len(names) == 0 {
		return nil, []string{"inherit: no rule nodes in the spec (scan pkg/ first)"}
	}
	label := requirementLabel(lang)
	var (
		rules    []specRule
		warnings []string
	)
	foreign := 0
	for _, name := range names {
		data, readErr := fs.ReadFile(spec, name)
		if readErr != nil {
			warnings = append(warnings, fmt.Sprintf("inherit: read %s: %v", name, readErr))
			continue
		}
		r, ok := parseSpecRule(data, label)
		switch {
		case !ok:
			foreign++
		case r.ExtractedBy == "" && r.InheritedFrom == "":
			rules = append(rules, r)
		}
	}
	if foreign > 0 {
		warnings = append(warnings, fmt.Sprintf(
			"inherit: %d rule nodes unreadable or not in %s (rescan pkg/ with -lang %s)",
			foreign, lang, lang))
	}
	return rules, warnings
}

// parseSpecRule reads a rule node: YAML frontmatter between --- lines, a
// "# title" and the line after label.
func parseSpecRule(data []byte, label string) (specRule, bool) {
	front, body, ok := bytes.Cut(bytes.TrimPrefix(data, []byte("---\n")), []byte("\n---\n"))
	var r specRule
	if !ok || yaml.Unmarshal(front, &r) != nil || r.ID == "" {
		return specRule{}, false
	}
	for _, line := range strings.Split(string(body), "\n") {
		if title, isTitle := strings.CutPrefix(line, "# "); isTitle && r.title == "" {
			r.title = title
		}
		if req, isReq := strings.CutPrefix(line, label); isReq {
			r.requirement = req
		}
	}
	return r, r.requirement != ""
}

// span is the one file a rule cites and the lines covering all its
// citations; false when it cites several files, or none.
func (r *specRule) span() (string, int, int, bool) {
	if len(r.Sources) == 0 {
		return "", 0, 0, false
	}
	file, start, end := r.Sources[0].Path, 0, 0
	for _, s := range r.Sources {
		var a, b int
		if s.Path != file || s.Root != "" {
			return "", 0, 0, false
		}
		if _, err := fmt.Sscanf(s.Lines, "%d-%d", &a, &b); err != nil {
			return "", 0, 0, false
		}
		if start == 0 || a < start {
			start = a
		}
		end = max(end, b)
	}
	return file, start, end, true
}

// fact makes the inherited rule fact of r, cited on file:start-end of
// webapp (file is relative to the checkout).
func (r *specRule) fact(spec, file string, start, end int) Fact {
	domain, slug := ruleDomainSlug(r.ID)
	attrs := map[string]string{
		attrSlug:          slug,
		attrTitle:         r.title,
		attrRequirement:   r.requirement,
		attrEARS:          r.EARS,
		attrNature:        r.Nature,
		attrDomain:        slugify(sharedDomainPrefix + domain), // like derived shared rules
		attrInheritedFrom: spec,
		attrCrosscheck:    r.Crosscheck,
		attrOrigin:        r.Origin,
		attrConfidence:    r.Confidence,
	}
	return Fact{
		Kind:      kindRule,
		ID:        "rule.inherited." + spec + "." + strings.TrimPrefix(r.ID, "rule."),
		Attrs:     attrs,
		Source:    Ref{Path: file, Lines: lines(start, end), Root: webappRoot},
		Origin:    origin,
		Certainty: certain,
	}
}

// ruleDomainSlug splits a rule node id, rule.<domain>.<slug>.
func ruleDomainSlug(id string) (string, string) {
	rest := strings.TrimPrefix(id, "rule.")
	domain, slug, ok := strings.Cut(rest, ".")
	if !ok {
		return "pkg", rest
	}
	return domain, slug
}

// declSpans are the lines of reached declarations, by file.
type declSpans map[string][][2]int

// reachedSpans returns the lines of the shared declarations the service
// reaches: functions and methods, variables and types.
func reachedSpans(shared sharedCode, reach map[declKey]bool) declSpans {
	spans := make(declSpans)
	for i := range shared.sources {
		s := &shared.sources[i]
		dir := path.Dir(s.name)
		add := func(name string, n ast.Node) {
			if reach[declKey{dir, name}] {
				spans[s.name] = append(spans[s.name], [2]int{
					s.fset.Position(n.Pos()).Line, s.fset.Position(n.End()).Line,
				})
			}
		}
		for _, fn := range funcDecls(s.file) {
			if fn.Recv == nil {
				add(fn.Name.Name, fn)
			} else if recv := receiverName(fn); recv != "" {
				add(recv+"."+fn.Name.Name, fn)
			}
		}
		for _, v := range packageVars(s.file) {
			add(v.name, v.value)
		}
		for _, t := range typeSpecs(s.file) {
			add(t.Name.Name, t)
		}
	}
	return spans
}

// overlaps reports whether file:start-end overlaps a reached declaration.
func (d declSpans) overlaps(file string, start, end int) bool {
	return slices.ContainsFunc(d[file], func(s [2]int) bool { return start <= s[1] && s[0] <= end })
}
