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
// relation is then to the item), about no user.
type Checker struct {
	Means      string `yaml:"means,omitempty"`
	Scope      string `yaml:"scope,omitempty"`
	Relation   string `yaml:"relation,omitempty"`
	Capability string `yaml:"capability,omitempty"`
	// SubjectArg is which of the call's arguments (from 0, the context
	// included) a self, coach or user-scope check is about.
	SubjectArg *int      `yaml:"subject_arg,omitempty"`
	Item       string    `yaml:"item,omitempty"`
	Any        []Checker `yaml:"any,omitempty"`
	All        []Checker `yaml:"all,omitempty"`
}

// pinnable are the meanings a checker may state.
func pinnable() []string {
	return []string{
		MeansLogin, MeansLoginStrict, MeansCapability, MeansServiceAdmin,
		MeansTask, MeansOkta, MeansPush, MeansRelation, MeansSelf, MeansCoach,
	}
}

// aboutUser reports whether c or a part of it is about a user: a self or
// coach check, a capability on a user scope.
func (c *Checker) aboutUser() bool {
	if c.Means == MeansSelf || c.Means == MeansCoach ||
		(c.Means == MeansCapability && c.Scope == ScopeUser) {
		return true
	}
	return slices.ContainsFunc(append(slices.Clone(c.Any), c.All...), func(p Checker) bool {
		return p.aboutUser()
	})
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
		c := cs[name]
		err := c.check(false)
		if err == nil && c.Item != "" && c.aboutUser() {
			err = errors.New("an item filter's meaning is about the item, not a user")
		}
		if err != nil {
			return nil, fmt.Errorf("policy: checker %q: %w", name, err)
		}
	}
	return cs, nil
}

// check validates c, a part of a compound checker when part.
func (c *Checker) check(part bool) error {
	shapes := 0
	for _, set := range []bool{c.Means != "", c.Any != nil, c.All != nil} {
		if set {
			shapes++
		}
	}
	switch {
	case part && c.Item != "":
		return errors.New("item names a filter at the top level only")
	case shapes != 1:
		return errors.New("states one of means, any or all")
	case c.Any != nil || c.All != nil:
		parts := append(slices.Clone(c.Any), c.All...)
		if len(parts) == 0 {
			return errors.New("an empty any or all")
		}
		for i := range parts {
			if err := parts[i].check(true); err != nil {
				return err
			}
		}
		return nil
	case !slices.Contains(pinnable(), c.Means):
		return fmt.Errorf("means %q is not one of %v", c.Means, pinnable())
	case c.Means == MeansCapability && !slices.Contains(pinnedScopes(), c.Scope):
		return fmt.Errorf("scope %q is not one of %v", c.Scope, pinnedScopes())
	case c.Means == MeansCapability && part && c.Capability == "":
		return errors.New("a capability part needs its capability")
	case c.Means == MeansRelation && c.Relation == "":
		return errors.New("a relation needs its name")
	case (c.Means == MeansSelf || c.Means == MeansCoach) && c.SubjectArg == nil:
		return errors.New("a self or coach check needs its subject_arg")
	case c.SubjectArg != nil && c.Means != MeansSelf && c.Means != MeansCoach &&
		(c.Means != MeansCapability || c.Scope != ScopeUser):
		return errors.New("subject_arg names the user of a self, coach or user-scope check only")
	}
	return nil
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
	}
	for _, c := range cs {
		walk(&c)
	}
	slices.Sort(out)
	return out
}
