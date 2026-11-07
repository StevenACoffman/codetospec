package policy

import (
	"cmp"
	"encoding/json"
	"slices"
	"strings"

	"github.com/StevenACoffman/codetospec/internal/condition"
	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/locale"
)

// Definition is what an inventory check means and how far that is
// verified: the code's allowing paths (the extractor's evidence), the
// person's pin, and the checks of one against the other. Each state is a
// boolean, so a reader never parses a reason to learn it.
type Definition struct {
	// Paths are the paths on which the check's code allows, in source
	// order; NotAnalyzable says the extractor couldn't read them,
	// NotAnalyzableReason why.
	Paths               []Path `json:"paths,omitempty"`
	NotAnalyzable       bool   `json:"not_analyzable"`
	NotAnalyzableReason string `json:"not_analyzable_reason,omitempty"`
	// Pinned says a checkers entry gives the check a meaning, Pin;
	// Unpinnable that its entry says why it has none (UnpinnableReason).
	// Approx: the person says the pin is stricter than the code.
	// Statement is the pin in words (Statement), or why it has none.
	Statement        string   `json:"statement,omitempty"`
	Pinned           bool     `json:"pinned"`
	Pin              *Checker `json:"pin,omitempty"`
	Unpinnable       bool     `json:"unpinnable"`
	UnpinnableReason string   `json:"unpinnable_reason,omitempty"`
	Approx           bool     `json:"approx"`
	// Stale: the pin was written for code whose digest has changed since.
	Stale bool `json:"stale"`
	// Leaves are the pin's leaves, each with its grounding in the code.
	Leaves []PinLeaf `json:"leaves,omitempty"`
	// Coverage compares the pin with the paths, when both are known.
	Coverage *Coverage `json:"path_coverage,omitempty"`
	// GlossaryMissing are the relations and attributes the pin names that
	// no registry entry declares; Concepts the registry concepts of those
	// it declares. Both sorted.
	GlossaryMissing []string `json:"glossary_missing,omitempty"`
	Concepts        []string `json:"concepts,omitempty"`
	// Comparison is how a second reader's pin compares (Compare).
	Comparison *Comparison `json:"comparison,omitempty"`
}

// Path is one path on which a check's code allows (extract.AttrPaths):
// the condition reaching it (the condition notation; empty when it always
// allows there), the lines allowing, and the check calls of the condition.
type Path struct {
	Condition string      `json:"condition,omitempty"`
	Lines     string      `json:"lines"`
	Checks    []CheckCall `json:"checks,omitempty"`
}

// PinLeaf is one leaf of a pin: its meaning, its operand (a capability's Go
// name, a relation's or attribute's name; none for the others) and its
// Grounding (Grounding*).
type PinLeaf struct {
	Means     string `json:"means"`
	Operand   string `json:"operand,omitempty"`
	Grounding string `json:"grounding"`
}

// Groundings of a leaf: its operand or meaning occurs in what the check's
// code reads or calls; it does not, though that code was read; or the
// code can't tell (not read, an operand from the call, a term the
// registry doesn't declare, a meaning no path names).
const (
	GroundingGrounded   = "grounded"
	GroundingUngrounded = "ungrounded"
	GroundingUnknown    = "unknown"
)

// Coverage matches a pin's alternatives (Parts, its disjunctive normal
// form, each "a ∧ b") with the code's paths, in both directions:
// UncoveredPaths and UnmatchedParts are indexes into Definition.Paths
// and Parts. Covered when both are empty.
//
// A path matches a part when the checks it requires (true in every match
// of its condition) are the part's leaves, a capability's matched by Go
// name when the part names one; a part with a relation or attribute also
// needs a path condition on data (a variable no check call became, or a
// call nothing states: no meaning, no pin, no readable paths of its own,
// which are otherwise inlined; a call whose variable isn't in the
// condition, a scope matcher, is an operand). A path with a stated
// check under an any reads no keys, so it matches nothing;
// a check a path requires to fail (an earlier allowance negated, a
// denial) is left out: negations and scopes are not compared.
type Coverage struct {
	Covered        bool     `json:"covered"`
	Parts          []string `json:"parts"`
	UncoveredPaths []int    `json:"uncovered_paths,omitempty"`
	UnmatchedParts []int    `json:"unmatched_parts,omitempty"`
}

// pathsOf reads an access_check fact's allowing paths and whether they
// could be read; a malformed paths attribute reads as not analyzable.
func pathsOf(f *extract.Fact) Definition {
	d := Definition{
		NotAnalyzable:       f.Attrs[extract.AttrNotAnalyzable] == "true",
		NotAnalyzableReason: f.Attrs[extract.AttrNotAnalyzableReason],
	}
	if raw := f.Attrs[extract.AttrPaths]; raw != "" {
		if err := json.Unmarshal([]byte(raw), &d.Paths); err != nil {
			return Definition{
				NotAnalyzable:       true,
				NotAnalyzableReason: "malformed paths: " + err.Error(),
			}
		}
	}
	return d
}

// Define gives each check of inv its pin from cs, the pin's statement in
// t's language and its verification against the check's paths, inputs
// and Digest, reading relations and attributes through reg. It changes
// only the checks' Definition pin, statement and verification fields, and
// Access (who can do what, by concept).
//
// Ensures: deterministic; every list sorted, Leaves in pin order.
func (inv *Inventory) Define(cs Checkers, reg Registry, t *locale.Text) {
	readable := readablePaths(inv.Checks)
	for i := range inv.Checks {
		c := &inv.Checks[i]
		pin, ok := cs[c.Name]
		if !ok {
			continue
		}
		d := &c.Definition
		d.Stale = pin.Digest != "" && c.Digest != "" && pin.Digest != c.Digest
		d.Approx = pin.Approx
		d.Statement = Statement(&pin, reg, t)
		if pin.Unpinnable != "" {
			d.Unpinnable, d.UnpinnableReason = true, pin.Unpinnable
			continue
		}
		d.Pinned, d.Pin = true, &pin
		d.Leaves = groundLeaves(c, leavesOf(&pin), reg)
		d.GlossaryMissing, d.Concepts = glossary(d.Leaves, reg)
		if !d.NotAnalyzable && len(d.Paths) > 0 {
			d.Coverage = pathCoverage(d.Paths, &pin, pathReader{cs: cs, checks: readable})
		}
	}
	inv.Access = inv.access(cs, reg, t)
}

// leavesOf lists c's leaves in order, each meaning and operand once.
func leavesOf(c *Checker) []PinLeaf {
	var out []PinLeaf
	var walk func(c *Checker)
	walk = func(c *Checker) {
		if c.Means != "" {
			l := PinLeaf{Means: c.Means, Operand: operandOf(c)}
			if !slices.Contains(out, l) {
				out = append(out, l)
			}
		}
		for i := range c.Any {
			walk(&c.Any[i])
		}
		for i := range c.All {
			walk(&c.All[i])
		}
		if c.Not != nil {
			walk(c.Not)
		}
		for i := range c.Cases {
			walk(&c.Cases[i].Checker)
		}
	}
	walk(c)
	return out
}

// operandOf is a leaf's operand: a capability's Go name (without its
// package), a relation's or attribute's name.
func operandOf(c *Checker) string {
	switch c.Means {
	case MeansCapability:
		return goName(c.Capability)
	case MeansRelation:
		return c.Relation
	case MeansAttribute:
		return c.Attribute
	}
	return ""
}

// goName is a qualified Go name's last part: capabilities.CanView → CanView.
func goName(name string) string {
	return name[strings.LastIndex(name, ".")+1:]
}

// groundLeaves sets each leaf's grounding in check c's code: a capability
// in a path's calls or c's inputs, a relation or attribute in an input its
// registry entry declares, any other meaning in a path's calls.
func groundLeaves(c *Check, leaves []PinLeaf, reg Registry) []PinLeaf {
	read := c.Source.Path != "" && c.Source.Root == ""
	ungrounded := GroundingUnknown
	if read {
		ungrounded = GroundingUngrounded
	}
	for i := range leaves {
		l := &leaves[i]
		switch {
		case l.Means == MeansCapability && l.Operand == "":
			l.Grounding = GroundingUnknown // the call names the capability
		case l.Means == MeansCapability:
			l.Grounding = cmp.Or(groundedIf(capabilityRead(c, l.Operand)), ungrounded)
		case l.Means == MeansRelation || l.Means == MeansAttribute:
			if _, declared := reg.entries[l.Operand]; !declared {
				l.Grounding = GroundingUnknown
				continue
			}
			l.Grounding = cmp.Or(groundedIf(slices.ContainsFunc(c.Inputs, func(in Input) bool {
				return in.DeclaredBy == l.Operand
			})), ungrounded)
		default:
			l.Grounding = cmp.Or(
				groundedIf(pathMeans(c.Definition.Paths, l.Means)),
				GroundingUnknown,
			)
		}
	}
	return leaves
}

// groundedIf is GroundingGrounded when ok, else "".
func groundedIf(ok bool) string {
	if ok {
		return GroundingGrounded
	}
	return ""
}

// capabilityRead reports whether c's paths call a check of capability
// name or c reads a value of that name.
func capabilityRead(c *Check, name string) bool {
	for _, p := range c.Definition.Paths {
		for _, call := range p.Checks {
			if call.Means == MeansCapability && goName(call.Capability) == name {
				return true
			}
		}
	}
	return slices.ContainsFunc(c.Inputs, func(in Input) bool {
		return in.Kind != KindField && in.Kind != KindLoad && goName(in.Name) == name
	})
}

// pathMeans reports whether a path calls a check meaning means.
func pathMeans(paths []Path, means string) bool {
	for _, p := range paths {
		for _, call := range p.Checks {
			if call.Means == means {
				return true
			}
		}
	}
	return false
}

// glossary returns the relation and attribute leaves the registry doesn't
// declare, and the concepts of those it does, both sorted and unique.
func glossary(leaves []PinLeaf, reg Registry) ([]string, []string) {
	var missing, concepts []string
	for _, l := range leaves {
		if l.Means != MeansRelation && l.Means != MeansAttribute {
			continue
		}
		if _, declared := reg.entries[l.Operand]; !declared {
			missing = append(missing, l.Operand)
		} else if concept := reg.Concept(l.Operand); concept != "" {
			concepts = append(concepts, concept)
		}
	}
	slices.Sort(missing)
	slices.Sort(concepts)
	return slices.Compact(missing), slices.Compact(concepts)
}

// leafKey is what coverage compares of a leaf: its meaning, and a
// capability's Go name ("" when the call names it).
type leafKey struct {
	means, capability string
}

// alternative is one conjunction coverage compares: its check leaves, and
// whether it needs a condition on data.
type alternative struct {
	keys []leafKey
	data bool
}

// pathCoverage matches pin's alternatives with paths (see Coverage); r
// expands a path's call of another checker into its pin or its paths.
func pathCoverage(paths []Path, pin *Checker, r pathReader) *Coverage {
	parts := dnf(pin)
	cov := &Coverage{Parts: make([]string, len(parts))}
	for i, part := range parts {
		cov.Parts[i] = part.text()
	}
	matched := make([]bool, len(parts))
	decided := decidedChecks(paths)
	for i := range paths {
		covered := false
		for _, alt := range r.alternatives(&paths[i], decided) {
			for j, part := range parts {
				if alt.matches(part) {
					matched[j], covered = true, true
				}
			}
		}
		if !covered {
			cov.UncoveredPaths = append(cov.UncoveredPaths, i)
		}
	}
	for j, ok := range matched {
		if !ok {
			cov.UnmatchedParts = append(cov.UnmatchedParts, j)
		}
	}
	cov.Covered = len(cov.UncoveredPaths) == 0 && len(cov.UnmatchedParts) == 0
	return cov
}

// dnf is c's disjunctive normal form over its leaves: a not's part is
// left out (negations are not compared), a relation or attribute sets the
// alternative's data need.
func dnf(c *Checker) []alternative {
	switch {
	case c.Means == MeansRelation || c.Means == MeansAttribute:
		return []alternative{{data: true}}
	case c.Means != "":
		return []alternative{{keys: []leafKey{{c.Means, goName(c.Capability)}}}}
	case c.Not != nil:
		return []alternative{{}}
	case c.Cases != nil:
		// The code's paths don't know the call's arguments: every case's
		// alternatives are the checker's.
		var out []alternative
		for i := range c.Cases {
			out = append(out, dnf(&c.Cases[i].Checker)...)
		}
		return out
	case c.Any != nil:
		var out []alternative
		for i := range c.Any {
			out = append(out, dnf(&c.Any[i])...)
		}
		return out
	}
	out := []alternative{{}}
	for i := range c.All {
		out = cross(out, dnf(&c.All[i]))
	}
	return out
}

// cross is every conjunction of one alternative of a with one of b.
func cross(a, b []alternative) []alternative {
	out := make([]alternative, 0, len(a)*len(b))
	for _, x := range a {
		for _, y := range b {
			out = append(out, alternative{
				keys: append(slices.Clone(x.keys), y.keys...),
				data: x.data || y.data,
			})
		}
	}
	return out
}

// alternatives reads the conjunctions path p requires: one per
// alternative of each checker it calls (its pin, or its own paths); none
// when a stated check of its condition is required neither to pass nor to
// fail (it sits under an any) or the condition doesn't parse. decided
// names the checks whose decision has a variable of its own on some path
// (decidedChecks). A call whose variable isn't in the condition is an
// operand (a scope matcher), a call's failure whose decision has its own
// variable is its error (not a decision), and one nothing states (no
// meaning, no pin, no readable paths) a condition on data, like any
// variable no call became.
func (r pathReader) alternatives(p *Path, decided map[string]bool) []alternative {
	c, err := condition.Parse([]byte(p.Condition))
	if err != nil {
		return nil
	}
	vars := c.Variables()
	required := c.Required()
	checkVars := map[string]bool{}
	out := []alternative{{}}
	for _, call := range p.Checks {
		if call.Var == "" || !slices.Contains(vars, call.Var) {
			continue
		}
		if call.Fails && decided[call.Check] {
			// The call's error, its decision named apart: a system failure,
			// not what the check decides.
			checkVars[call.Var] = true
			continue
		}
		alts, stated := r.call(&call)
		if !stated {
			continue
		}
		checkVars[call.Var] = true
		v, ok := required[call.Var]
		switch {
		case !ok:
			return nil
		case v == call.Fails:
			continue // negated: negations are not compared
		}
		out = cross(out, alts)
	}
	data := slices.ContainsFunc(vars, func(v string) bool { return !checkVars[v] })
	for i := range out {
		out[i].data = out[i].data || data
	}
	return out
}

// decidedChecks names the checks whose decision has a variable of its
// own on some of paths (a call not marked Fails): their failure variable
// is their error.
func decidedChecks(paths []Path) map[string]bool {
	decided := map[string]bool{}
	for _, p := range paths {
		for _, call := range p.Checks {
			if !call.Fails && call.Var != "" {
				decided[call.Check] = true
			}
		}
	}
	return decided
}

// pathReader reads the conjunctions paths require: cs gives the pinned
// meanings, checks each analyzed check's paths (to inline a call of one
// nothing states), inlining the checks being read (cut at MaxDepth, and
// at a cycle).
type pathReader struct {
	cs       Checkers
	checks   map[string][]Path
	inlining []string
}

// readablePaths maps each check whose paths were read whole to them.
func readablePaths(checks []Check) map[string][]Path {
	out := map[string][]Path{}
	for i := range checks {
		if d := &checks[i].Definition; !d.NotAnalyzable && len(d.Paths) > 0 {
			out[checks[i].Name] = d.Paths
		}
	}
	return out
}

// call is what a check call contributes: its meaning, else the pin of the
// checker it calls, else the alternatives of that checker's own paths
// when every one reads; false when nothing states it.
func (r pathReader) call(call *CheckCall) ([]alternative, bool) {
	if call.Means != "" {
		return []alternative{{keys: []leafKey{{call.Means, goName(call.Capability)}}}}, true
	}
	if pin, ok := r.cs[call.Check]; ok && pin.Unpinnable == "" {
		if meant, matched := pin.For(call); matched {
			meant = meant.atCall()
			return dnf(&meant), true
		}
	}
	return r.inline(call.Check)
}

// inline is the alternatives of check name's own paths, every one read;
// false when it has none, one doesn't read, or inlining it would pass
// MaxDepth or loop.
func (r pathReader) inline(name string) ([]alternative, bool) {
	paths, ok := r.checks[name]
	if !ok || len(r.inlining) >= MaxDepth || slices.Contains(r.inlining, name) {
		return nil, false
	}
	inner := pathReader{
		cs:       r.cs,
		checks:   r.checks,
		inlining: append(slices.Clone(r.inlining), name),
	}
	decided := decidedChecks(paths)
	var out []alternative
	for i := range paths {
		alts := inner.alternatives(&paths[i], decided)
		if alts == nil {
			return nil, false
		}
		out = append(out, alts...)
	}
	return out, true
}

// matches reports whether path alternative a matches pin part: each key
// of either has a matching key in the other, and part's data need is met.
func (a alternative) matches(part alternative) bool {
	if part.data && !a.data {
		return false
	}
	match := func(pathKey, partKey leafKey) bool {
		return pathKey.means == partKey.means &&
			(partKey.capability == "" || pathKey.capability == partKey.capability)
	}
	for _, k := range a.keys {
		if !slices.ContainsFunc(part.keys, func(p leafKey) bool { return match(k, p) }) {
			return false
		}
	}
	for _, p := range part.keys {
		if !slices.ContainsFunc(a.keys, func(k leafKey) bool { return match(k, p) }) {
			return false
		}
	}
	return true
}

// text renders a part: its leaves joined by ∧, "data" for its data need,
// "always" when it needs nothing.
func (a alternative) text() string {
	terms := make([]string, 0, len(a.keys)+1)
	for _, k := range a.keys {
		if k.capability != "" {
			terms = append(terms, k.means+" "+k.capability)
		} else {
			terms = append(terms, k.means)
		}
	}
	if a.data {
		terms = append(terms, "data")
	}
	if len(terms) == 0 {
		return "always"
	}
	return strings.Join(terms, " ∧ ")
}
