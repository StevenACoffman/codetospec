// Package policy builds a policy inventory from a graph and its facts: for
// each enforcement point, the checks its guards call; for each check, what
// it reads, sorted into kinds by structure, and whether a registry (the
// PIP's, written by a person) declares each input. Deterministic, no LLM.
//
// The inventory is complete relative to what the extractors recognize: a
// check the webapp extractor doesn't know is invisible, an input read
// deeper than MaxDepth calls or through interface dispatch is missed, and
// gqlgen fields resolved by struct binding are not enforcement points.
//
// Kinds of input (the vocabulary this package pins):
//   - KindField: a struct field read (a reads fact);
//   - KindLoad: a datastore kind queried, with its filter properties;
//   - KindStaff, KindFlag: a value of a type a value_kind fact says is a
//     capability's or a feature flag's;
//   - KindValue: any other package-level value (a role constant);
//   - KindSelf, KindContext: kinds of a guard's check call (by its
//     meaning: about one user; a predicate on the request), not of a
//     check's inputs.
package policy

import (
	"cmp"
	"encoding/json"
	"maps"
	"slices"
	"sort"

	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/graph"
)

// Kinds of input and of check call.
const (
	KindField   = "field"
	KindLoad    = "load"
	KindStaff   = "staff"
	KindFlag    = "flag"
	KindValue   = "value"
	KindSelf    = "self"
	KindContext = "context"
	KindCheck   = "check" // a call of an access check, by its name
)

// MaxDepth is how many calls deep a check's inputs are looked for, as test
// reach and journeys go.
const MaxDepth = 3

// Inventory is the policy inventory of one graph.
type Inventory struct {
	Endpoints []Endpoint `json:"endpoints"`
	Checks    []Check    `json:"checks"`
	// Open and Todo are the guards that call no check: marked open, or
	// marked todo (the extractor's checks values). Unguarded are handlers
	// with no guard at all.
	Open      []Guard     `json:"open,omitempty"`
	Todo      []Guard     `json:"todo,omitempty"`
	Unguarded []Unguarded `json:"unguarded,omitempty"`
	// Undeclared are the field and load inputs no registry entry declares,
	// sorted; Unread the registry entries no check reads.
	Undeclared []string `json:"undeclared,omitempty"`
	Unread     []string `json:"unread,omitempty"`
	// Conditions are the registry entries' free-text conditions, by
	// entry: shown beside what they qualify, never evaluated.
	Conditions map[string]string `json:"conditions,omitempty"`
	Depth      int               `json:"depth"`
}

// Endpoint is one enforcement point and the guards of its handler.
type Endpoint struct {
	// Endpoint is the endpoint node's id, or for a handler with none (a
	// field resolver) the operation it serves, "User.emails".
	Endpoint string  `json:"endpoint"`
	Guards   []Guard `json:"guards"`
}

// Guard is one extractor rule guarding a handler.
type Guard struct {
	RuleID      string      `json:"rule"`
	Title       string      `json:"title"`
	Source      extract.Ref `json:"source"`
	Calls       []CheckCall `json:"calls,omitempty"`
	Kinds       []string    `json:"kinds,omitempty"` // per call, its kind
	Scope       string      `json:"scope,omitempty"` // "item" for a guard that drops one item
	DelegatesTo string      `json:"delegates_to,omitempty"`
	endpoints   []string    // the endpoints it guards
	marker      string      // "open" or "todo", else ""
}

// CheckCall is one check a guard calls, as the extractor's check_args
// records it: Var the condition variable it became (Fails: the variable
// holds when the call fails), Means what the check means and Scope what
// its matcher means, in the generic vocabulary (Means*, Scope*), unset
// when the extractor doesn't know.
type CheckCall struct {
	Check      string `json:"check"`
	Var        string `json:"var,omitempty"`
	Fails      bool   `json:"fails,omitempty"`
	Means      string `json:"means,omitempty"`
	Scope      string `json:"scope,omitempty"`
	Capability string `json:"capability,omitempty"`
	Matcher    string `json:"matcher,omitempty"`
	MatcherArg string `json:"matcher_arg,omitempty"`
	Subject    string `json:"subject,omitempty"`
	// SubjectFrom says where the user a self, coach or user-scope check is
	// about comes from: "arg:<param>[.field]", "parent[:field]"; unset
	// for a local. Args are an unknown check's arguments, the same way.
	SubjectFrom string     `json:"subject_from,omitempty"`
	Rate        *RateLimit `json:"rate,omitempty"`
	Args        []CallArg  `json:"args,omitempty"`
}

// CallArg is one argument of a check call as written, and where it comes
// from (as CheckCall.SubjectFrom).
type CallArg struct {
	Text string `json:"text"`
	From string `json:"from,omitempty"`
}

// RateLimit is a rate-limit check call's key and limit, as written.
type RateLimit struct {
	Key    string `json:"key"`
	Count  string `json:"count,omitempty"`
	Window string `json:"window,omitempty"`
}

// Check is one access check and what it reads.
type Check struct {
	Name   string      `json:"name"`
	Form   string      `json:"form"`
	Source extract.Ref `json:"source"` // empty when only its name is known
	// Digest is Source's digest (as drift hashes a span), set when the
	// cited code was read: what a checkers entry records (Checker.Digest).
	Digest       string        `json:"digest,omitempty"`
	Inputs       []Input       `json:"inputs,omitempty"`
	Descriptions []Description `json:"descriptions,omitempty"`
}

// Input is one thing a check reads.
type Input struct {
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	Detail     string `json:"detail,omitempty"`      // a value, a load's properties
	DeclaredBy string `json:"declared_by,omitempty"` // the registry entry, or ""
}

// Description is an LLM rule describing a check: its citation overlaps the
// check's definition. Advisory: what the check decides, in words.
type Description struct {
	RuleID     string `json:"rule"`
	Title      string `json:"title"`
	Crosscheck string `json:"crosscheck,omitempty"`
}

// Unguarded is a handler with no guard.
type Unguarded struct {
	Operation string      `json:"operation"`
	Source    extract.Ref `json:"source"`
}

// Build assembles the inventory of rules (the graph's rule nodes) and facts
// (the merged facts, .codetospec/facts.json), comparing inputs with reg.
//
// Requires: facts and rules come from the same run.
// Ensures:  every list sorted; rules and facts unmodified.
func Build(facts []extract.Fact, rules []graph.Node, reg Registry) Inventory {
	idx := indexFacts(facts)
	inv := Inventory{Depth: MaxDepth}
	byEndpoint := map[string][]Guard{}
	for i := range rules {
		g, ok := guardOf(&rules[i])
		if !ok {
			continue
		}
		switch g.marker {
		case markerOpen:
			inv.Open = append(inv.Open, g)
			continue
		case markerTodo:
			inv.Todo = append(inv.Todo, g)
			continue
		}
		for _, e := range g.endpoints {
			byEndpoint[e] = append(byEndpoint[e], g)
		}
	}
	for _, e := range slices.Sorted(maps.Keys(byEndpoint)) {
		guards := byEndpoint[e]
		sortGuards(guards)
		inv.Endpoints = append(inv.Endpoints, Endpoint{Endpoint: e, Guards: guards})
	}
	inv.Checks = checks(idx, rules, reg)
	inv.Undeclared, inv.Unread = coverage(inv.Checks, reg)
	inv.Unguarded = idx.unguarded
	inv.Conditions = reg.conditions()
	sortGuards(inv.Open)
	sortGuards(inv.Todo)
	return inv
}

// Markers of a guard that calls no check (extract.AttrChecks values).
const (
	markerOpen = "open"
	markerTodo = "todo"
)

// sortGuards orders guards by rule id, by index: a Guard is too large to
// copy per comparison.
func sortGuards(guards []Guard) {
	sort.Slice(guards, func(i, j int) bool { return guards[i].RuleID < guards[j].RuleID })
}

// guardOf reads the guard an extractor rule node states; false for a rule
// that names no check (an LLM rule, a role table).
func guardOf(n *graph.Node) (Guard, bool) {
	checks := n.Extra[graph.ExtraChecks]
	if n.Type != graph.TypeRule || checks == "" {
		return Guard{}, false
	}
	g := Guard{
		RuleID:      n.ID,
		Title:       n.Title,
		Scope:       n.Extra[graph.ExtraCheckScope],
		DelegatesTo: n.Extra[graph.ExtraDelegatesTo],
	}
	if len(n.Sources) > 0 {
		g.Source = n.Sources[0]
	}
	for _, e := range n.Edges {
		if e.Type == graph.EdgeExposedBy {
			g.endpoints = append(g.endpoints, e.To)
		}
	}
	if len(g.endpoints) == 0 && n.Extra[graph.ExtraOperation] != "" {
		// A field resolver has no endpoint node: its operation is the point.
		g.endpoints = []string{n.Extra[graph.ExtraOperation]}
	}
	if checks == markerOpen || checks == markerTodo {
		g.marker = checks
		return g, true
	}
	if raw := n.Extra[graph.ExtraCheckArgs]; raw != "" {
		// A malformed check_args leaves the guard without its calls: it still
		// guards, and the inventory says it names no call.
		_ = json.Unmarshal([]byte(raw), &g.Calls)
	}
	for _, c := range g.Calls {
		g.Kinds = append(g.Kinds, callKind(c.Means))
	}
	return g, true
}

// callKind is the kind of a guard's call of a check meaning means.
func callKind(means string) string {
	switch means {
	case MeansSelf, MeansCoach:
		return KindSelf
	case MeansServiceAdmin, MeansTask, MeansOkta, MeansRequest:
		return KindContext
	}
	return KindCheck
}

// checks lists every check the facts name, with its inputs and the LLM
// rules describing it, sorted by name.
func checks(idx *factIndex, rules []graph.Node, reg Registry) []Check {
	out := make([]Check, 0, len(idx.checks))
	for _, f := range idx.checks {
		c := Check{
			Name:   f.Attrs[extract.AttrName],
			Form:   f.Attrs[extract.AttrForm],
			Source: f.Source,
		}
		if c.Source.Path != "" && c.Source.Root == "" {
			c.Inputs = inputsOf(idx, idx.reached(c.Source), reg)
			c.Descriptions = descriptions(rules, c.Source)
		}
		out = append(out, c)
	}
	slices.SortFunc(out, func(a, b Check) int { return cmp.Compare(a.Name, b.Name) })
	return out
}

// descriptions are the LLM rules whose citation overlaps def.
func descriptions(rules []graph.Node, def extract.Ref) []Description {
	var out []Description
	for i := range rules {
		n := &rules[i]
		if n.Type != graph.TypeRule || n.Extra[graph.ExtraExtractedBy] != "" ||
			n.Extra[graph.ExtraInheritedFrom] != "" {
			continue
		}
		for _, src := range n.Sources {
			if overlaps(src, def) {
				out = append(
					out,
					Description{RuleID: n.ID, Title: n.Title, Crosscheck: n.Extra["crosscheck"]},
				)
				break
			}
		}
	}
	slices.SortFunc(out, func(a, b Description) int { return cmp.Compare(a.RuleID, b.RuleID) })
	return out
}

// inputsOf lists the inputs read by the definitions at spans, one per kind
// and name, sorted by kind then name.
func inputsOf(idx *factIndex, spans []extract.Ref, reg Registry) []Input {
	seen := map[[2]string]Input{}
	add := func(in Input) {
		in.DeclaredBy = reg.declares(in)
		seen[[2]string{in.Kind, in.Name}] = in
	}
	for _, f := range idx.inputs {
		if !coveredBy(f.Source, spans) {
			continue
		}
		switch f.Kind {
		case extract.KindReads:
			add(Input{Kind: KindField, Name: f.Attrs[extract.AttrField]})
		case extract.KindLoads:
			add(
				Input{
					Kind:   KindLoad,
					Name:   f.Attrs[extract.AttrDatastoreKind],
					Detail: f.Attrs[extract.AttrProperties],
				},
			)
		case extract.KindUsesValue:
			add(
				Input{
					Kind:   cmp.Or(idx.valueKinds[f.Attrs[extract.AttrType]], KindValue),
					Name:   f.Attrs[extract.AttrName],
					Detail: f.Attrs[extract.AttrValue],
				},
			)
		}
	}
	out := slices.Collect(maps.Values(seen))
	slices.SortFunc(out, func(a, b Input) int {
		return cmp.Or(cmp.Compare(a.Kind, b.Kind), cmp.Compare(a.Name, b.Name))
	})
	return out
}

// coverage returns the field and load inputs no registry entry declares,
// and the registry entries no check reads, both sorted and unique.
func coverage(checks []Check, reg Registry) ([]string, []string) {
	undeclared := map[string]bool{}
	read := map[string]bool{}
	for _, c := range checks {
		for _, in := range c.Inputs {
			if in.Kind != KindField && in.Kind != KindLoad {
				continue
			}
			if in.DeclaredBy == "" {
				undeclared[in.Name] = true
			} else {
				read[in.DeclaredBy] = true
			}
		}
	}
	var unread []string
	for _, name := range reg.names() {
		if !read[name] {
			unread = append(unread, name)
		}
	}
	return slices.Sorted(maps.Keys(undeclared)), unread
}
