// Package position derives a rule's nature from where its cited code
// lives, without an LLM: a rule whose citations all fall in an adapter,
// middleware or infrastructure package is technical by position, whatever
// its text says. The layers come from codetospec.yaml (nature_by_path) or
// from the target's anarch-lint model (anarch.json layers, nature_by_layer).
//
// Each rule's structural evidence (graph.ExtraNatureEvidence: entities,
// endpoints, conditions on an entity, a technical quality attribute,
// platform code) gives a nature of its own (graph.ExtraNatureByEvidence:
// business, technical, or open when it holds both): DOMAIN_PLAN.md's
// subject-matter / technical split, which concept grouping reads.
//
// The positional nature never overrides the LLM's: a disagreement is
// recorded (graph.ExtraNatureByPosition) beside that evidence, and asked
// as a question only when the evidence backs the position (Asks). Position is one signal among
// those of DOMAIN_PLAN.md's subject-matter / technical split
// (recommendation 4), never a vote: measured, the text is the better
// reading in most disagreements, and a layer whose rules disagree often
// says the mapping is wrong (Layer, in the output README).
package position

import (
	"cmp"
	"encoding/json"
	"fmt"
	"maps"
	"path"
	"slices"
	"strings"

	"github.com/StevenACoffman/codetospec/internal/condition"
	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/graph"
	"github.com/StevenACoffman/codetospec/internal/quality"
)

// The natures a layer can have, as the map and reduce write them.
const (
	Business     = "business"
	Presentation = "presentation"
	Technical    = "technical"
)

// Map is the nature of each directory pattern. The zero Map knows no
// layer: every rule's positional nature is unknown.
type Map struct {
	patterns []pattern // sorted, so the first match is deterministic
}

// pattern is one Go-style package pattern, as a slash path relative to
// --src: "internal/text" holds the files of that directory alone,
// "cmd/..." those of the whole tree under cmd.
type pattern struct {
	dir       string
	recursive bool
	nature    string
}

// Parse reads codetospec.yaml's nature_by_path: per nature, the package
// patterns ("internal/middleware", "cmd/...") of its layers.
//
// Ensures: an error names an unknown nature or an empty pattern.
func Parse(byNature map[string][]string) (Map, error) {
	var m Map
	for _, nature := range slices.Sorted(maps.Keys(byNature)) {
		if !isNature(nature) {
			return Map{}, fmt.Errorf("nature_by_path: unknown nature %q "+
				"(business, presentation or technical)", nature)
		}
		for _, p := range byNature[nature] {
			parsed, ok := parsePattern(p, nature)
			if !ok {
				return Map{}, fmt.Errorf("nature_by_path: %s: empty pattern", nature)
			}
			m.patterns = append(m.patterns, parsed)
		}
	}
	m.sort()
	return m, nil
}

// FromAnarch reads the layers of an anarch-lint model (anarch.json) and
// gives each the nature byLayer maps it to; a layer byLayer does not name
// is left out. Package patterns are import paths: modulePath (the target's
// go.mod module) is cut from each, a pattern outside the module dropped.
//
// Ensures: an error names an unknown nature, a layer byLayer names that
// the model does not have, or a model that does not parse.
func FromAnarch(model []byte, modulePath string, byLayer map[string]string) (Map, error) {
	var parsed struct {
		Layers []struct {
			Name     string   `json:"name"`
			Packages []string `json:"packages"`
		} `json:"layers"`
	}
	if err := json.Unmarshal(model, &parsed); err != nil {
		return Map{}, fmt.Errorf("anarch.json: %w", err)
	}
	var m Map
	found := map[string]bool{}
	for _, layer := range parsed.Layers {
		nature, mapped := byLayer[layer.Name]
		if !mapped {
			continue
		}
		if !isNature(nature) {
			return Map{}, fmt.Errorf("nature_by_layer: %s: unknown nature %q "+
				"(business, presentation or technical)", layer.Name, nature)
		}
		found[layer.Name] = true
		for _, pkg := range layer.Packages {
			rel, inModule := relativeTo(pkg, modulePath)
			if !inModule {
				continue
			}
			if parsed, ok := parsePattern(rel, nature); ok {
				m.patterns = append(m.patterns, parsed)
			}
		}
	}
	for _, name := range slices.Sorted(maps.Keys(byLayer)) {
		if !found[name] {
			return Map{}, fmt.Errorf("nature_by_layer: anarch.json has no layer %q", name)
		}
	}
	m.sort()
	return m, nil
}

// relativeTo cuts modulePath from an import path pattern: the module root
// itself is ".", "<module>/..." is "...".
func relativeTo(pkg, modulePath string) (string, bool) {
	if pkg == modulePath {
		return ".", true
	}
	rel, ok := strings.CutPrefix(pkg, modulePath+"/")
	return rel, ok
}

// parsePattern reads one package pattern; ok is false for an empty one.
func parsePattern(p, nature string) (pattern, bool) {
	p = strings.Trim(strings.TrimSpace(p), "/")
	dir, recursive := strings.CutSuffix(p, "...")
	dir = strings.TrimSuffix(dir, "/")
	if dir == "" && !recursive {
		return pattern{}, false
	}
	if dir == "" {
		dir = "." // "..." alone: the whole module
	}
	return pattern{dir: dir, recursive: recursive, nature: nature}, true
}

// sort orders the patterns most specific first (longest directory, exact
// before recursive), so a sub-layer overrides the tree it sits in.
func (m *Map) sort() {
	slices.SortFunc(m.patterns, func(a, b pattern) int {
		return cmp.Or(
			cmp.Compare(len(b.dir), len(a.dir)),
			compareBool(a.recursive, b.recursive),
			cmp.Compare(a.dir, b.dir),
			cmp.Compare(a.nature, b.nature),
		)
	})
}

// compareBool orders false before true.
func compareBool(a, b bool) int {
	switch {
	case a == b:
		return 0
	case a:
		return 1
	}
	return -1
}

func isNature(s string) bool {
	return s == Business || s == Presentation || s == Technical
}

// String is the pattern as written: "cmd/...", "internal/http", "...".
func (p *pattern) String() string {
	switch {
	case p.dir == "." && p.recursive:
		return "..."
	case p.recursive:
		return p.dir + "/..."
	}
	return p.dir
}

// layerOf is the index of the pattern holding file, -1 when none does.
func (m *Map) layerOf(file string) int {
	dir := path.Dir(file)
	for i, p := range m.patterns {
		switch {
		case p.dir == ".":
			if p.recursive || dir == "." {
				return i
			}
		case dir == p.dir, p.recursive && strings.HasPrefix(dir, p.dir+"/"):
			return i
		}
	}
	return -1
}

// layersOf is the indexes of the patterns holding sources under --src, in
// pattern order, each once; ok is false when one of them is in no layer.
func (m *Map) layersOf(sources []extract.Ref) ([]int, bool) {
	var layers []int
	for _, src := range sources {
		if src.Root != "" {
			continue
		}
		i := m.layerOf(src.Path)
		if i < 0 {
			return nil, false
		}
		if !slices.Contains(layers, i) {
			layers = append(layers, i)
		}
	}
	slices.Sort(layers)
	return layers, true
}

// Of is the nature of the layers sources fall in: the one nature every
// source under --src has, "" when a source is in no layer, the layers
// disagree, or no source is under --src (another root is not mapped).
func (m *Map) Of(sources []extract.Ref) string {
	layers, ok := m.layersOf(sources)
	if !ok || len(layers) == 0 {
		return ""
	}
	nature := m.patterns[layers[0]].nature
	for _, i := range layers[1:] {
		if m.patterns[i].nature != nature {
			return ""
		}
	}
	return nature
}

// Layer is how the rules of one layer compare with it: a high share of
// Disagree says the mapping, not the rules, is wrong.
type Layer struct {
	Pattern string // as configured: "cmd/...", "internal/http"
	Nature  string
	// Placed counts the rules with a positional nature citing code in the
	// layer (a rule citing several layers counts in each), Disagree those
	// of them whose own nature differs, Asked those of these whose
	// evidence backs the position (a nature_mismatch question each).
	Placed, Disagree, Asked int
}

// Annotate records on each rule its structural evidence
// (graph.ExtraNatureEvidence) and the nature that evidence alone gives
// (graph.ExtraNatureByEvidence, DOMAIN_PLAN.md recommendation 4), its
// facets (graph.ExtraFacets), and, when its positional nature differs
// from the nature it was given, the positional one
// (graph.ExtraNatureByPosition); and counts each layer's rules. entities
// are the names of the domain models' entities: a condition on one of
// their attributes, as on a column of an entity node's table, is business
// evidence.
//
// Requires: m from Parse or FromAnarch, or the zero Map (no layers).
// Ensures:  nodes is not modified; a rule with no evidence gets no
// evidence field, one whose position says nothing or agrees no positional
// field; no rule's nature changes; one Layer per pattern, most specific
// first, none with an empty Map.
func Annotate(nodes []graph.Node, m *Map, entities []string) ([]graph.Node, []Layer) {
	var layers []Layer
	for i := range m.patterns {
		layers = append(
			layers,
			Layer{Pattern: m.patterns[i].String(), Nature: m.patterns[i].nature},
		)
	}
	concepts := conceptsOf(nodes, entities)
	out := slices.Clone(nodes)
	for i := range out {
		if out[i].Type != graph.TypeRule {
			continue
		}
		rule := &out[i]
		ev := evidenceOf(rule, concepts)
		fields := map[string]string{}
		if summary := ev.String(); summary != "" {
			fields[graph.ExtraNatureEvidence] = summary
			fields[graph.ExtraNatureByEvidence] = ev.nature()
		}
		if facets := facetsOf(rule); facets != "" {
			fields[graph.ExtraFacets] = facets
		}
		if positional := m.disagreement(rule, ev, layers); positional != "" {
			fields[graph.ExtraNatureByPosition] = positional
		}
		if len(fields) > 0 {
			rule.Extra = maps.Clone(rule.Extra)
			if rule.Extra == nil {
				rule.Extra = map[string]string{}
			}
			maps.Copy(rule.Extra, fields)
		}
	}
	return out, layers
}

// disagreement counts rule in its layers and returns its positional
// nature when that differs from its nature; "" when its position says
// nothing or agrees.
func (m *Map) disagreement(rule *graph.Node, ev evidence, layers []Layer) string {
	positional := m.Of(rule.Sources)
	if positional == "" {
		return ""
	}
	in, _ := m.layersOf(rule.Sources)
	disagrees := positional != rule.Extra["nature"]
	for _, l := range in {
		layers[l].Placed++
		if disagrees {
			layers[l].Disagree++
		}
		if disagrees && ev.backs(positional) {
			layers[l].Asked++
		}
	}
	if !disagrees {
		return ""
	}
	return positional
}

// Asks reports whether rule's positional nature is a question: the
// position disagrees with its nature and its own evidence backs the
// position. Evidence that backs the text, or none, leaves the
// disagreement recorded and counted in its layer, not asked.
func Asks(rule *graph.Node) bool {
	positional := rule.Extra[graph.ExtraNatureByPosition]
	if positional == "" {
		return false
	}
	for group := range strings.SplitSeq(rule.Extra[graph.ExtraNatureEvidence], evidenceSeparator) {
		if n, _, _ := strings.Cut(group, ": "); n == positional && group != "" {
			return true
		}
	}
	return false
}

// evidenceSeparator separates the natures of an evidence summary.
const evidenceSeparator = "; "

// Open is the nature by evidence of a rule with both business and
// technical evidence: the evidence does not decide.
const Open = "open"

// FacetAccess is the facet of a rule that controls access (DOMAIN_PLAN.md
// recommendation 5): a facet of the concept it guards, never a domain.
const FacetAccess = "access"

// evidence is a rule's structural evidence, no LLM and no words of its
// text, each item "<kind> <what>" ("touches entity.a"), sorted.
type evidence struct {
	business, technical []string
}

// nature is the nature the evidence alone gives: business or technical
// when only one kind is present, Open when both are.
//
// Requires: e holds some evidence.
func (e evidence) nature() string {
	switch {
	case len(e.business) > 0 && len(e.technical) > 0:
		return Open
	case len(e.business) > 0:
		return Business
	default:
		return Technical
	}
}

// backs reports whether e holds evidence of nature.
func (e evidence) backs(nature string) bool {
	switch nature {
	case Business:
		return len(e.business) > 0
	case Technical:
		return len(e.technical) > 0
	}
	return false
}

// String is the evidence as the frontmatter states it: "business: touches
// entity.a, exposed_by endpoint.b; technical: quality_attribute
// reliability", "" for none.
func (e evidence) String() string {
	var groups []string
	if len(e.business) > 0 {
		groups = append(groups, Business+": "+strings.Join(e.business, ", "))
	}
	if len(e.technical) > 0 {
		groups = append(groups, Technical+": "+strings.Join(e.technical, ", "))
	}
	return strings.Join(groups, evidenceSeparator)
}

// isTechnicalQuality reports whether a quality attribute is technical
// evidence. Security, safety and compliance are not: an authorization
// rule serves security and is business; usability is presentation's.
func isTechnicalQuality(attribute string) bool {
	switch attribute {
	case quality.PerformanceEfficiency, quality.Reliability, quality.Availability,
		quality.Scalability, quality.Compatibility, quality.Maintainability,
		quality.Portability:
		return true
	}
	return false
}

// conceptsOf is the names a condition variable's owner may have to be
// business evidence: the tables of the entity nodes and the entities of
// the domain models.
func conceptsOf(nodes []graph.Node, entities []string) map[string]bool {
	concepts := map[string]bool{}
	for i := range nodes {
		if nodes[i].Type == graph.TypeEntity {
			concepts[nodes[i].Title] = true
		}
	}
	for _, e := range entities {
		concepts[e] = true
	}
	return concepts
}

// evidenceOf is rule's structural evidence: business, an entity it
// touches, an endpoint exposing it, a condition variable on a concept
// ("table.column"); technical, a technical quality attribute, or code that
// lives only in the platform: an inherited rule, or one citing only other
// roots than --src (DOMAIN_PLAN.md recommendation 19).
func evidenceOf(rule *graph.Node, concepts map[string]bool) evidence {
	var ev evidence
	for _, e := range rule.Edges {
		if e.Type == graph.EdgeTouches || e.Type == graph.EdgeExposedBy {
			ev.business = append(ev.business, e.Type+" "+e.To)
		}
	}
	if raw := rule.Extra[graph.ExtraCondition]; raw != "" {
		if c, err := condition.Parse([]byte(raw)); err == nil {
			for _, v := range c.Variables() {
				if dot := strings.LastIndex(v, "."); dot > 0 && concepts[v[:dot]] {
					ev.business = append(ev.business, "condition "+v)
				}
			}
		}
	}
	if attribute := rule.Extra[graph.ExtraQualityAttribute]; isTechnicalQuality(attribute) {
		ev.technical = append(ev.technical, graph.ExtraQualityAttribute+" "+attribute)
	}
	if from := rule.Extra[graph.ExtraInheritedFrom]; from != "" {
		ev.technical = append(ev.technical, graph.ExtraInheritedFrom+" "+from)
	} else if root := onlyRoot(rule.Sources); root != "" {
		ev.technical = append(ev.technical, "root "+root)
	}
	slices.Sort(ev.business)
	ev.business = slices.Compact(ev.business)
	slices.Sort(ev.technical)
	return ev
}

// onlyRoot is the root every source cites when none is under --src, the
// first in source order when they name several; "" otherwise.
func onlyRoot(sources []extract.Ref) string {
	for _, src := range sources {
		if src.Root == "" {
			return ""
		}
	}
	if len(sources) == 0 {
		return ""
	}
	return sources[0].Root
}

// facetsOf is rule's facets, comma-separated: access when it serves
// security and checks something before responding (a mechanical rule an
// extractor proved, or a structured condition); "" for none. A security
// rule that checks nothing (an audit trail, data scoping) has no facet.
func facetsOf(rule *graph.Node) string {
	if rule.Extra[graph.ExtraQualityAttribute] != quality.Security {
		return ""
	}
	if rule.Extra[graph.ExtraExtractedBy] != "" || rule.Extra[graph.ExtraCondition] != "" {
		return FacetAccess
	}
	return ""
}
