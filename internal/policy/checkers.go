package policy

import (
	"errors"
	"fmt"
	"maps"
	"slices"

	"gopkg.in/yaml.v3"
)

// Checkers pins what checks the extractor doesn't know mean, by check
// name as check_args names it ("actorHasGlobalPermission",
// "permissions.CanViewCDI"). A person writes it, reading each check's
// code; codetospec never does. A pinned meaning applies only to a call
// the extractor gave none.
type Checkers map[string]Checker

// Checker is one pinned meaning, one of two shapes:
//   - a check: Means a leaf kind; Scope for a capability (ScopeAny…),
//     whose capability is Capability (its Go name), else the call's
//     argument; Relation for a relation, the name of the relation the
//     caller must hold to the request's resource (a registry entry: the
//     PIP serves it);
//   - Any or All of checkers, as the checker's code combines them (each
//     part's capability named: a part has no call argument of its own).
//
// Item, on a top-level checker, says the check is a visibility filter over
// items of that kind: its meaning says when one item is visible (a
// relation is then to the item, a check about a user about the item's
// own, SubjectItem).
//
// Unpinnable, on an entry alone, states why the check can't be pinned (a
// person's reason: cross_service, data_predicate…), Note saying more: its
// guards are skipped with that reason.
type Checker struct {
	Means      string `yaml:"means,omitempty"`
	Scope      string `yaml:"scope,omitempty"`
	Relation   string `yaml:"relation,omitempty"`
	Capability string `yaml:"capability,omitempty"`
	// SubjectArg is which of the call's arguments (from 0, the context
	// included) a self, coach or user-scope check is about. On a compound
	// checker, it is every such part's that names none.
	SubjectArg *int `yaml:"subject_arg,omitempty"`
	// CapabilityArg is which of the call's arguments names a capability
	// check's capability, for a part: it has no call argument of its own.
	CapabilityArg *int      `yaml:"capability_arg,omitempty"`
	Item          string    `yaml:"item,omitempty"`
	Any           []Checker `yaml:"any,omitempty"`
	All           []Checker `yaml:"all,omitempty"`
	Unpinnable    string    `yaml:"unpinnable,omitempty"`
	Note          string    `yaml:"note,omitempty"`
	// Of and Attribute name an attribute check's holder (Holder*) and
	// attribute; Not negates a checker.
	Of        string   `yaml:"of,omitempty"`
	Attribute string   `yaml:"attribute,omitempty"`
	Not       *Checker `yaml:"not,omitempty"`
	// Digest is the digest of the checker's definition (its access_check
	// fact's source, as drift hashes a span) when the entry was written:
	// a different current digest makes the pin stale (StalePins).
	Digest string `yaml:"digest,omitempty"`
}

// pinnable are the meanings a checker may state.
func pinnable() []string {
	return []string{
		MeansLogin, MeansLoginStrict, MeansCapability, MeansServiceAdmin,
		MeansTask, MeansOkta, MeansPush, MeansRelation, MeansSelf, MeansCoach,
		MeansAttribute,
	}
}

// withSubjectArg returns a copy of c with a compound checker's
// subject_arg passed to each part about a user that names none, and taken
// off the checker itself; c is not modified.
func (c *Checker) withSubjectArg() Checker {
	out := *c
	if c.SubjectArg == nil || c.Any == nil && c.All == nil {
		return out
	}
	pass := func(parts []Checker) []Checker {
		passed := make([]Checker, len(parts))
		for i := range parts {
			p := parts[i]
			if p.SubjectArg == nil && (p.Any != nil || p.All != nil || p.needsSubject()) {
				p.SubjectArg = c.SubjectArg
			}
			passed[i] = p.withSubjectArg()
		}
		return passed
	}
	if c.Any != nil {
		out.Any = pass(c.Any)
	}
	if c.All != nil {
		out.All = pass(c.All)
	}
	out.SubjectArg = nil
	return out
}

// needsSubject reports whether c, check-shaped, is about a user: a self or
// coach check, a capability on a user scope.
func (c *Checker) needsSubject() bool {
	return c.Means == MeansSelf || c.Means == MeansCoach ||
		c.Means == MeansCapability && c.Scope == ScopeUser
}

// subjectIn is the user a pinned check is about and where it comes from:
// the call argument SubjectArg names, else operand (the extractor's) and
// the call's binding.
func (c *Checker) subjectIn(call *CheckCall, operand string) (string, string) {
	if c.SubjectArg == nil {
		return operand, call.SubjectFrom
	}
	if i := *c.SubjectArg; i >= 0 && i < len(call.Args) {
		return call.Args[i].Text, call.Args[i].From
	}
	return "", ""
}

// ParseCheckers reads a checkers file: a YAML map from check name to
// checker.
//
// Ensures: an error naming the first bad entry (sorted by name): two
// shapes or none, an empty any or all, an unknown meaning, a capability
// without a pinned scope (or, in a part, without its name), a relation
// without its name.
func ParseCheckers(data []byte) (Checkers, error) {
	var cs Checkers
	if err := yaml.Unmarshal(data, &cs); err != nil {
		return nil, fmt.Errorf("policy: checkers: %w", err)
	}
	for _, name := range slices.Sorted(maps.Keys(cs)) {
		entry := cs[name]
		c := entry.withSubjectArg()
		if err := c.check(false, c.Item != ""); err != nil {
			return nil, fmt.Errorf("policy: checker %q: %w", name, err)
		}
		cs[name] = c
	}
	return cs, nil
}

// check validates c, a part of a compound checker when part, of an item
// filter when item: there, a check about a user is about the item's own
// user (SubjectItem), so it names no subject_arg.
func (c *Checker) check(part, item bool) error {
	shapes := 0
	for _, set := range []bool{c.Means != "", c.Any != nil, c.All != nil, c.Not != nil, c.Unpinnable != ""} {
		if set {
			shapes++
		}
	}
	switch {
	case part && (c.Item != "" || c.Unpinnable != ""):
		return errors.New("item and unpinnable name a whole entry, not a part")
	case shapes != 1:
		return errors.New("states one of means, any, all, not or unpinnable")
	case c.Unpinnable != "":
		return nil
	case c.Not != nil:
		if err := c.Not.check(true, item); err != nil {
			return fmt.Errorf("not: %w", err)
		}
		return nil
	case c.Any != nil || c.All != nil:
		return c.checkParts(item)
	}
	return c.checkLeaf(part, item)
}

// checkParts validates a compound checker's parts, naming the first bad
// one by its position.
func (c *Checker) checkParts(item bool) error {
	parts := append(slices.Clone(c.Any), c.All...)
	if len(parts) == 0 {
		return errors.New("an empty any or all")
	}
	for i := range parts {
		if err := parts[i].check(true, item); err != nil {
			return fmt.Errorf("part %d: %w", i+1, err)
		}
	}
	return nil
}

// checkLeaf validates a check-shaped checker (see check).
func (c *Checker) checkLeaf(part, item bool) error {
	switch {
	case !slices.Contains(pinnable(), c.Means):
		return fmt.Errorf("means %q is not one of %v", c.Means, pinnable())
	case c.Means == MeansCapability && !slices.Contains(pinnedScopes(), c.Scope):
		return fmt.Errorf("scope %q is not one of %v", c.Scope, pinnedScopes())
	case c.Capability != "" && c.CapabilityArg != nil:
		return errors.New("names its capability twice (capability, capability_arg)")
	case c.Means == MeansCapability && part && c.Capability == "" && c.CapabilityArg == nil:
		return errors.New("a capability part needs its capability or capability_arg")
	case c.Means == MeansRelation && c.Relation == "":
		return errors.New("a relation needs its name")
	case c.Means == MeansAttribute &&
		(!slices.Contains([]string{HolderCaller, HolderResource, HolderContext}, c.Of) || c.Attribute == ""):
		return errors.New(
			"an attribute needs its holder (of: caller, resource or context) and its name",
		)
	case c.Means != MeansAttribute && (c.Of != "" || c.Attribute != ""):
		return errors.New("of and attribute name an attribute check only")
	case item && c.SubjectArg != nil:
		return errors.New("an item filter's check is about the item's user: no subject_arg")
	case !item && (c.Means == MeansSelf || c.Means == MeansCoach) && c.SubjectArg == nil:
		return errors.New("a self or coach check needs its subject_arg")
	case c.SubjectArg != nil && !c.needsSubject() && c.Means != MeansRelation:
		return errors.New(
			"subject_arg names the user of a self, coach, user-scope or relation check only",
		)
	}
	return nil
}

// StalePins lists, sorted, the checkers whose recorded Digest differs from
// their definition's current digest in digests (by check name): pins
// written for code that has changed since. A pin with no digest, or whose
// checker's digest is unknown, is never stale.
func StalePins(cs Checkers, digests map[string]string) []string {
	var stale []string
	for name, c := range cs {
		if current, known := digests[name]; known && c.Digest != "" && c.Digest != current {
			stale = append(stale, name)
		}
	}
	slices.Sort(stale)
	return stale
}

// MergeCheckers merges checkers files, by path, into one set: shared
// pins (webapp's pkg/) and a service's own combine.
//
// Ensures: an error naming the check and the two files (sorted) when two
// files define one name.
func MergeCheckers(files map[string]Checkers) (Checkers, error) {
	merged := Checkers{}
	from := map[string]string{} // check name → file
	for _, path := range slices.Sorted(maps.Keys(files)) {
		for _, name := range slices.Sorted(maps.Keys(files[path])) {
			if first, dup := from[name]; dup {
				return nil, fmt.Errorf("policy: checker %q is in both %s and %s", name, first, path)
			}
			from[name] = path
			merged[name] = files[path][name]
		}
	}
	return merged, nil
}

// Undeclared lists the relations cs names, in parts too, that no entry of
// reg declares, sorted and unique: the loader contract would ask for what
// the PIP can't serve.
func (cs Checkers) Undeclared(reg Registry) []string {
	var out []string
	var walk func(c *Checker)
	walk = func(c *Checker) {
		if c.Means == MeansRelation && !slices.Contains(reg.names(), c.Relation) &&
			!slices.Contains(out, c.Relation) {
			out = append(out, c.Relation)
		}
		for _, p := range append(slices.Clone(c.Any), c.All...) {
			walk(&p)
		}
		if c.Not != nil {
			walk(c.Not)
		}
	}
	for _, c := range cs {
		walk(&c)
	}
	slices.Sort(out)
	return out
}
