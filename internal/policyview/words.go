package policyview

import (
	"cmp"
	"fmt"
	"strings"

	"github.com/StevenACoffman/codetospec/internal/cedarpolicy"
	"github.com/StevenACoffman/codetospec/internal/d2"
	"github.com/StevenACoffman/codetospec/internal/locale"
	"github.com/StevenACoffman/codetospec/internal/policy"
)

// phraser words decisions, callers and checks in one language.
type phraser struct {
	goNames map[string]string // capability stored name → Go name
	reg     policy.Registry
	t       *locale.Text
}

// cell is a decision, in words.
func (ph phraser) cell(d cedarpolicy.Decision) Cell {
	return Cell{Outcome: d.Outcome, Text: ph.decision(d)}
}

// decision is a decision in words.
func (ph phraser) decision(d cedarpolicy.Decision) string {
	switch d.Outcome {
	case cedarpolicy.Allowed:
		return ph.t.AccessAllowed
	case cedarpolicy.Denied:
		return ph.t.AccessDenied
	case cedarpolicy.Depends:
		facts := make([]string, len(d.Depends))
		for i := range d.Depends {
			facts[i] = ph.fact(&d.Depends[i])
		}
		return fmt.Sprintf(ph.t.AccessDependsFmt, strings.Join(facts, "; "))
	}
	return ph.t.AccessUndecided
}

// fact is a request fact in words.
func (ph phraser) fact(f *cedarpolicy.Fact) string {
	switch f.Kind {
	case cedarpolicy.FactKmap:
		return ph.t.FactKmap
	case cedarpolicy.FactCountry:
		return ph.t.FactCountry
	case cedarpolicy.FactLocale:
		return ph.t.FactLocale
	}
	return ph.leaf(f.Check)
}

// leaf is a check in words.
func (ph phraser) leaf(l *policy.Leaf) string {
	return policy.LeafPhrase(l, ph.goNames[l.Capability], ph.reg, ph.t)
}

// personas labels a column: each persona in words, a role's grant scopes
// gathered after its name.
func (ph phraser) personas(group []policy.Persona) string {
	// A role's personas are consecutive (Personas lists each role's
	// scopes together), so a role's label is written at its last one.
	var labels, scopes []string
	for i, p := range group {
		if p.Role == "" {
			labels = append(labels, ph.persona(&p))
			continue
		}
		scopes = append(scopes, ph.grantScope(p.Grants[0].Scope))
		if i+1 == len(group) || group[i+1].Role != p.Role {
			labels = append(
				labels,
				fmt.Sprintf(ph.t.PersonaRoleFmt, p.Role, strings.Join(scopes, ", ")),
			)
			scopes = nil
		}
	}
	return strings.Join(labels, " · ")
}

// persona is one persona that no role gives in words.
func (ph phraser) persona(p *policy.Persona) string {
	switch {
	case p.ID == policy.PrincipalAnonymous:
		return ph.t.PersonaAnonymous
	case p.ID == policy.PrincipalPush:
		return ph.t.PersonaPush
	case p.ID == policy.PrincipalService:
		return ph.t.PersonaService
	case p.ID == policy.PrincipalUser:
		return ph.t.PersonaUser
	case p.Phantom:
		return ph.t.PersonaPhantom
	case len(p.Grants) == 1 && p.Grants[0].Explicit:
		c := p.Grants[0].Capability
		return fmt.Sprintf(ph.t.PersonaHoldsFmt, policy.CapabilityWords(cmp.Or(ph.goNames[c], c)))
	}
	return ph.t.PersonaSiteAdmin
}

// grantScope is a grant scope in words.
func (ph phraser) grantScope(scope string) string {
	return map[string]string{
		policy.GrantGlobal: ph.t.GrantGlobal, policy.GrantUsersAll: ph.t.GrantUsersAll,
		policy.GrantUsersNwea: ph.t.GrantUsersNwea, policy.GrantCountry: ph.t.GrantCountry,
		policy.GrantLocale: ph.t.GrantLocale, policy.GrantContent: ph.t.GrantContent,
	}[scope]
}

// diagram lays out op's decision: from the request, each guard in rule
// order asks its checks (a guard denies when its expression holds), and
// the request reaches Allowed only past every guard.
func (ph phraser) diagram(op *policy.Operation, label string) *d2.Diagram {
	b := diagramBuilder{ph: ph}
	allowedLabel := ph.t.DiagramAllowed
	if len(op.Obligations) > 0 {
		allowedLabel = ph.t.DiagramAllowedLimits
	}
	allowed := b.node(allowedLabel, d2.ClassFocal)
	denied := b.node(ph.t.DiagramDenied, d2.ClassTerminal)
	next := allowed
	for i := len(op.Denies) - 1; i >= 0; i-- {
		next = b.expr(&op.Denies[i].When, denied, next)
	}
	start := b.node(fmt.Sprintf(ph.t.DiagramRequestFmt, label), d2.ClassTerminal)
	b.d.Edges = append(b.d.Edges, d2.Edge{From: start, To: next})
	// Nodes were made from the end back; draw them from the request on.
	for i, j := 0, len(b.d.Nodes)-1; i < j; i, j = i+1, j-1 {
		b.d.Nodes[i], b.d.Nodes[j] = b.d.Nodes[j], b.d.Nodes[i]
	}
	return &b.d
}

// diagramBuilder adds a decision diagram's nodes and edges.
type diagramBuilder struct {
	ph phraser
	d  d2.Diagram
}

// node adds a node, its id.
func (b *diagramBuilder) node(label, class string) string {
	id := fmt.Sprintf("n%d", len(b.d.Nodes))
	b.d.Nodes = append(b.d.Nodes, d2.Node{ID: id, Label: label, Class: class})
	return id
}

// expr adds the questions deciding e, its entry node's id: where e holds
// the flow goes on to onTrue, else to onFalse.
func (b *diagramBuilder) expr(e *policy.Expr, onTrue, onFalse string) string {
	switch e.Op {
	case policy.OpNot:
		return b.expr(&e.Args[0], onFalse, onTrue)
	case policy.OpAll:
		next := onTrue
		for i := len(e.Args) - 1; i >= 0; i-- {
			next = b.expr(&e.Args[i], next, onFalse)
		}
		return next
	case policy.OpAny:
		next := onFalse
		for i := len(e.Args) - 1; i >= 0; i-- {
			next = b.expr(&e.Args[i], onTrue, next)
		}
		return next
	}
	q := b.node(fmt.Sprintf(b.ph.t.DiagramQuestionFmt, b.ph.leaf(e.Check)), d2.ClassDecision)
	b.d.Edges = append(b.d.Edges,
		d2.Edge{From: q, To: onTrue, Label: b.ph.t.DiagramYes},
		d2.Edge{From: q, To: onFalse, Label: b.ph.t.DiagramNo})
	return q
}
