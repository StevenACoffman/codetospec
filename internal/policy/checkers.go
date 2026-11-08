package policy

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
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
	Means      string `json:"means,omitempty"      yaml:"means,omitempty"`
	Scope      string `json:"scope,omitempty"      yaml:"scope,omitempty"`
	Relation   string `json:"relation,omitempty"   yaml:"relation,omitempty"`
	Capability string `json:"capability,omitempty" yaml:"capability,omitempty"`
	// SubjectArg is which of the call's arguments (from 0, the context
	// included) a self, coach or user-scope check is about. On a compound
	// checker, it is every such part's that names none.
	SubjectArg *int `json:"subject_arg,omitempty" yaml:"subject_arg,omitempty"`
	// CapabilityArg is which of the call's arguments names a capability
	// check's capability, for a part: it has no call argument of its own.
	CapabilityArg *int      `json:"capability_arg,omitempty" yaml:"capability_arg,omitempty"`
	Item          string    `json:"item,omitempty"           yaml:"item,omitempty"`
	Any           []Checker `json:"any,omitempty"            yaml:"any,omitempty"`
	All           []Checker `json:"all,omitempty"            yaml:"all,omitempty"`
	Unpinnable    string    `json:"unpinnable,omitempty"     yaml:"unpinnable,omitempty"`
	Note          string    `json:"note,omitempty"           yaml:"note,omitempty"`
	// Of and Attribute name an attribute check's holder (Holder*) and
	// attribute; Not negates a checker.
	// ContentKind or ContentKindArg (which call argument, a string
	// literal, names it) is a content-scope capability's content kind.
	ContentKind    string   `json:"content_kind,omitempty"     yaml:"content_kind,omitempty"`
	ContentKindArg *int     `json:"content_kind_arg,omitempty" yaml:"content_kind_arg,omitempty"`
	Of             string   `json:"of,omitempty"               yaml:"of,omitempty"`
	Attribute      string   `json:"attribute,omitempty"        yaml:"attribute,omitempty"`
	Not            *Checker `json:"not,omitempty"              yaml:"not,omitempty"`
	// Digest is the digest of the checker's definition (its access_check
	// fact's source, as drift hashes a span) when the entry was written:
	// a different current digest makes the pin stale (StalePins).
	Digest string `json:"digest,omitempty" yaml:"digest,omitempty"`
	// Cases, on an entry, pin a checker whose meaning depends on its call's
	// arguments: the first case whose WhenArg the call's arguments match
	// is the call's meaning (For); a call matching none has none.
	Cases []ArgCase `json:"cases,omitempty" yaml:"cases,omitempty"`
	// Call, on an item entry, is the filter's call-level meaning: when a
	// guard tests the filter itself for one call (its failure, an empty
	// result), not one item.
	Call *Checker `json:"call,omitempty" yaml:"call,omitempty"`
	// Approx, on an entry, says the pin is stricter than the code: it
	// leaves out an allowing path the policy model can't state, so the
	// export denies some requests the code allows, never the reverse.
	Approx bool `json:"approx,omitempty" yaml:"approx,omitempty"`
	// Bugs, on an entry, are the defects its author found in the
	// checker's code while pinning it: questions for review beside the
	// rules (Definition.Questions), whatever the pin.
	Bugs []CheckerBug `json:"bugs,omitempty" yaml:"bugs,omitempty"`
}

// CheckerBug is one defect a pin's author found in a checker's code: Kind
// (Bug*) and Detail, what and where, in the author's words.
type CheckerBug struct {
	Kind   string `json:"kind"   yaml:"kind"`
	Detail string `json:"detail" yaml:"detail"`
}

// Kinds of a CheckerBug: the checker's doc comment says what its code
// doesn't do; a branch of its code can never run.
const (
	BugCommentMismatch = "checker_comment_mismatch"
	BugDeadBranch      = "dead_branch"
)

// ArgCase is one case of a checker keyed on argument values: WhenArg maps
// argument indexes (from 0, the context included, as SubjectArg) to the
// argument's text as written (`"admin"`, models.RoleAdmin), or CallerArg
// for an argument that is the caller's kaid, and Checker is the meaning
// when every one matches.
type ArgCase struct {
	WhenArg map[string]string `json:"when_arg" yaml:"when_arg"`
	Checker
}

// For is what c pins for call: c itself, or for an entry with cases, the
// first case whose WhenArg call's arguments match (with c's Item); false
// when none matches.
func (c *Checker) For(call *CheckCall) (Checker, bool) {
	if c.Cases == nil {
		return *c, true
	}
	for i := range c.Cases {
		if c.Cases[i].matches(call) {
			out := c.Cases[i].Checker
			out.Item, out.Call = c.Item, c.Call
			return out, true
		}
	}
	return Checker{}, false
}

// FromCaller is the origin (CallArg.From, CheckCall.SubjectFrom) of an
// operand that is the request user's own kaid: the caller. CallerArg, as a
// WhenArg value, matches an argument of that origin, whatever its text.
const (
	FromCaller = "caller"
	CallerArg  = "@caller"
)

// atCall is what c means where a guard tests it for the call: its Call
// for an item filter that states one, else c itself.
func (c *Checker) atCall() Checker {
	if c.Item != "" && c.Call != nil {
		return *c.Call
	}
	return *c
}

// matches reports whether call's arguments are k's WhenArg: each argument
// as written, or for CallerArg, bound to the caller.
func (k *ArgCase) matches(call *CheckCall) bool {
	for index, text := range k.WhenArg {
		i, err := strconv.Atoi(index)
		if err != nil || i < 0 || i >= len(call.Args) {
			return false
		}
		arg := call.Args[i]
		if text == CallerArg && arg.From != FromCaller || text != CallerArg && arg.Text != text {
			return false
		}
	}
	return true
}

// key is k's WhenArg as one canonical text, sorted by index:
// "2=\"admin\"".
func (k *ArgCase) key() string {
	parts := make([]string, 0, len(k.WhenArg))
	for _, index := range slices.Sorted(maps.Keys(k.WhenArg)) {
		parts = append(parts, index+"="+k.WhenArg[index])
	}
	return strings.Join(parts, ", ")
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
	if c.Call != nil {
		call := c.Call.withSubjectArg()
		out.Call = &call
	}
	if c.Cases != nil {
		out.Cases = make([]ArgCase, len(c.Cases))
		for i, k := range c.Cases {
			if k.SubjectArg == nil {
				k.SubjectArg = c.SubjectArg
			}
			out.Cases[i] = ArgCase{WhenArg: k.WhenArg, Checker: k.withSubjectArg()}
		}
		out.SubjectArg = nil
		return out
	}
	if c.SubjectArg == nil || c.Any == nil && c.All == nil && c.Not == nil {
		return out
	}
	passOne := func(p Checker) Checker {
		if p.SubjectArg == nil &&
			(p.Any != nil || p.All != nil || p.Not != nil || p.needsSubject()) {
			p.SubjectArg = c.SubjectArg
		}
		return p.withSubjectArg()
	}
	pass := func(parts []Checker) []Checker {
		passed := make([]Checker, len(parts))
		for i := range parts {
			passed[i] = passOne(parts[i])
		}
		return passed
	}
	if c.Not != nil {
		not := passOne(*c.Not)
		out.Not = &not
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
	normalized, err := validated(SchemaCheckers, data)
	if err != nil {
		return nil, fmt.Errorf("policy: checkers: %w", err)
	}
	var cs Checkers
	if err = json.Unmarshal(normalized, &cs); err != nil {
		return nil, fmt.Errorf("policy: checkers: %w", err)
	}
	for _, name := range slices.Sorted(maps.Keys(cs)) {
		entry := cs[name]
		c := entry.withSubjectArg()
		if err = c.check(false, c.Item != ""); err != nil {
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
	for _, set := range []bool{
		c.Means != "", c.Any != nil, c.All != nil, c.Not != nil, c.Unpinnable != "", c.Cases != nil,
	} {
		if set {
			shapes++
		}
	}
	if err := c.checkCall(part); err != nil {
		return err
	}
	switch {
	case part && (c.Item != "" || c.Unpinnable != "" || c.Cases != nil):
		return errors.New("item, unpinnable, cases and call name a whole entry, not a part")
	case shapes != 1:
		return errors.New("states one of means, any, all, not, cases or unpinnable")
	case c.Unpinnable != "":
		return nil
	case c.Cases != nil:
		return c.checkCases(item)
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

// checkCall validates c's call-level meaning: on an item entry only, and
// a checker of its own (not an item: about the call's arguments, so a
// self or coach check names its subject_arg).
func (c *Checker) checkCall(part bool) error {
	switch {
	case c.Call == nil:
		return nil
	case part:
		return errors.New("item, unpinnable, cases and call name a whole entry, not a part")
	case c.Item == "":
		return errors.New("call states an item filter's call-level meaning: it needs item")
	}
	if err := c.Call.check(true, false); err != nil {
		return fmt.Errorf("call: %w", err)
	}
	return nil
}

// checkCases validates an entry's cases, naming the first bad one by its
// position: each keyed on at least one argument index, a checker itself.
func (c *Checker) checkCases(item bool) error {
	for i := range c.Cases {
		k := &c.Cases[i]
		for index := range k.WhenArg {
			if n, err := strconv.Atoi(index); err != nil || n < 0 {
				return fmt.Errorf("case %d: when_arg index %q is not an argument index", i+1, index)
			}
		}
		if len(k.WhenArg) == 0 {
			return fmt.Errorf("case %d: needs when_arg", i+1)
		}
		if err := k.check(false, item); err != nil {
			return fmt.Errorf("case %d: %w", i+1, err)
		}
	}
	return nil
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
	if !slices.Contains(pinnable(), c.Means) {
		return fmt.Errorf("means %q is not one of %v", c.Means, pinnable())
	}
	if err := c.checkCapability(part); err != nil {
		return err
	}
	switch {
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

// checkCapability validates a capability check's operands: a pinned
// scope, its capability named once (by name or argument, a part by one of
// them), a content scope's kind named once and only there.
func (c *Checker) checkCapability(part bool) error {
	switch {
	case c.Means == MeansCapability && !slices.Contains(pinnedScopes(), c.Scope):
		return fmt.Errorf("scope %q is not one of %v", c.Scope, pinnedScopes())
	case c.Capability != "" && c.CapabilityArg != nil:
		return errors.New("names its capability twice (capability, capability_arg)")
	case c.Scope == ScopeContent && (c.ContentKind == "") == (c.ContentKindArg == nil):
		return errors.New("a content scope needs its content_kind or content_kind_arg, one of them")
	case c.Scope != ScopeContent && (c.ContentKind != "" || c.ContentKindArg != nil):
		return errors.New("content_kind and content_kind_arg name a content scope's kind only")
	case c.Means == MeansCapability && part && c.Capability == "" && c.CapabilityArg == nil:
		return errors.New("a capability part needs its capability or capability_arg")
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

// Undeclared lists the relations and attributes cs names, in parts too,
// that no entry of reg declares, sorted and unique: the loader contract
// would ask for what the PIP can't serve, and a business statement would
// have no phrase for them.
func (cs Checkers) Undeclared(reg Registry) []string {
	var out []string
	var walk func(c *Checker)
	walk = func(c *Checker) {
		term := c.Relation
		if c.Means == MeansAttribute {
			term = c.Attribute
		}
		if (c.Means == MeansRelation || c.Means == MeansAttribute) &&
			!slices.Contains(reg.names(), term) && !slices.Contains(out, term) {
			out = append(out, term)
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
