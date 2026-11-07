package policy

import (
	"cmp"
	"slices"
	"strings"

	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/graph"
)

// JoinTarget is another service's graph a cross-service check may call
// into: Service its slug as dependency facts name it (the graph
// directory's name), Rules its rule nodes, Checkers its pins.
type JoinTarget struct {
	Service  string
	Rules    []graph.Node
	Checkers Checkers
}

// Joined is one check pinned from another service, or why it isn't: Via
// the operation it calls ("<service>:<operation>"), Answer the check that
// operation answers there; Reason empty when the check took that check's
// pin.
type Joined struct {
	Check  string `json:"check"`
	Via    string `json:"via"`
	Answer string `json:"answer,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// Reasons a cross-service check takes no pin.
const (
	JoinSeveral    = "calls several operations of other services"
	JoinNoTarget   = "no --join graph of the service"
	JoinNoAnswer   = "the operation answers no check"
	JoinAnswers    = "the operation answers several checks"
	JoinNotPinned  = "the answered check isn't pinned there"
	JoinUnpinnable = "the answered check is unpinnable there"
	JoinSubject    = "the answered check is about a user the call's arguments don't map"
)

// Join pins each check facts name that cs doesn't, whose code (within
// MaxDepth calls) calls one operation of another service (a dependency
// fact) answering one check there (graph.ExtraAnswers on its guards):
// that check's pin in its target, when it is about no user (which of the
// call's arguments is the user isn't known). It returns the pins and, per
// such check, what was joined or why not, sorted by check.
//
// Ensures: cs and targets unmodified; a joined pin notes its origin.
func Join(facts []extract.Fact, cs Checkers, targets []JoinTarget) (Checkers, []Joined) {
	idx := indexFacts(facts)
	var deps []extract.Fact
	for _, f := range facts {
		if f.Kind == extract.KindDependency {
			deps = append(deps, f)
		}
	}
	byService := map[string]*JoinTarget{}
	for i := range targets {
		byService[targets[i].Service] = &targets[i]
	}
	pins := Checkers{}
	var report []Joined
	for _, f := range idx.checks {
		name := f.Attrs[extract.AttrName]
		if _, pinned := cs[name]; pinned || f.Source.Path == "" || f.Source.Root != "" {
			continue
		}
		vias := operationsCalled(idx.reached(f.Source), deps)
		if len(vias) == 0 {
			continue
		}
		j := Joined{Check: name, Via: strings.Join(vias, ", ")}
		if len(vias) > 1 {
			j.Reason = JoinSeveral
		} else {
			service, op, _ := strings.Cut(vias[0], ":")
			var pin Checker
			if pin, j.Answer, j.Reason = byService[service].pinOf(op); j.Reason == "" {
				pin.Note = "joined from " + j.Via + " (" + j.Answer + ")"
				pin.Digest = ""
				pins[name] = pin
			}
		}
		report = append(report, j)
	}
	slices.SortFunc(report, func(a, b Joined) int { return cmp.Compare(a.Check, b.Check) })
	return pins, report
}

// operationsCalled lists, sorted, the other services' operations the
// code at spans calls ("<service>:<operation>"), from dependency facts.
func operationsCalled(spans []extract.Ref, deps []extract.Fact) []string {
	var vias []string
	for _, d := range deps {
		via := d.Attrs[extract.AttrService] + ":" + d.Attrs[extract.AttrOperation]
		if coveredBy(d.Source, spans) && !slices.Contains(vias, via) {
			vias = append(vias, via)
		}
	}
	slices.Sort(vias)
	return vias
}

// pinOf is the pin of the one check operation op answers in t, that check,
// and why there's none to take ("" when there is).
func (t *JoinTarget) pinOf(op string) (Checker, string, string) {
	if t == nil {
		return Checker{}, "", JoinNoTarget
	}
	var answers []string
	for i := range t.Rules {
		n := &t.Rules[i]
		if n.Extra[graph.ExtraOperation] != op || n.Extra[graph.ExtraAnswers] == "" {
			continue
		}
		for _, a := range strings.Split(n.Extra[graph.ExtraAnswers], ",") {
			if !slices.Contains(answers, a) {
				answers = append(answers, a)
			}
		}
	}
	switch len(answers) {
	case 0:
		return Checker{}, "", JoinNoAnswer
	case 1:
	default:
		return Checker{}, "", JoinAnswers
	}
	pin, ok := t.Checkers[answers[0]]
	switch {
	case !ok:
		return Checker{}, answers[0], JoinNotPinned
	case pin.Unpinnable != "":
		return Checker{}, answers[0], JoinUnpinnable
	case aboutUser(&pin):
		return Checker{}, answers[0], JoinSubject
	}
	return pin, answers[0], ""
}

// aboutUser reports whether a part of c is about a user (a self, coach or
// user-scope check, anything naming a subject_arg).
func aboutUser(c *Checker) bool {
	if c.needsSubject() || c.SubjectArg != nil {
		return true
	}
	parts := append(slices.Clone(c.Any), c.All...)
	if c.Not != nil {
		parts = append(parts, *c.Not)
	}
	for i := range c.Cases {
		parts = append(parts, c.Cases[i].Checker)
	}
	return slices.ContainsFunc(parts, func(p Checker) bool { return aboutUser(&p) })
}
