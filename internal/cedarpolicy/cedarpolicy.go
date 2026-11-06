// Package cedarpolicy renders a policy.Model in the Cedar policy language
// (cedar-policy.com): the one place that knows the format. Not CEDAR, Khan's
// analytics event pipeline. Pinned to the language as
// github.com/cedar-policy/cedar-go v1.9.0 parses and validates it: Export
// parses every policy and the schema with it and validates each policy
// against the schema (strict), so it never writes text cedar-go rejects.
//
// Mapping, one namespace per service ("Districts"):
//   - an action per operation, Districts::Action::"User.emails", applying to
//     the principal types User, Service (a service-admin request), Push (a
//     Pub/Sub push whose token validated) and Anonymous, the resource type
//     Target, and the context RequestContext;
//   - a permit per exported operation, @id("allow:<action>");
//   - a forbid per guard, @id(rule id) and @source(citation), whose `when`
//     holds exactly when the guard rejects;
//   - an alias operation (its handler only delegates) shares its target's
//     policies: `action in [target, alias]`;
//   - an entity type per item kind a pinned visibility filter decides,
//     "<Kind>Item" (ItemType: a kind may be named like a fixed type, as
//     User), with a Target's attributes, and its action "view:<Kind>":
//     the decision on one item of a filtered result.
//
// A policy whose condition errors is skipped, so a forbid on a missing
// attribute would allow: the schema makes every attribute the policies read
// required, and a check of a User attribute first tests `principal is User`.
//
// Leaves (templates):
//   - login: principal is User && principal.loggedIn; strict adds
//     !principal.phantom;
//   - service-admin: principal is Service; push: principal is Push; task,
//     Okta lifecycle: context.isTask, context.isOktaLifecycle;
//   - relation: resource.relations.contains("<name>");
//   - self: principal is User && resource.subject == principal; coach:
//     principal is User && resource.coaches.contains(principal);
//   - capability, by matcher (each follows the matcher's code in
//     pkg/khan/acl): acl.AnyScope principal in CapAny::"<c>";
//     acl.GlobalScope principal in CapGlobal::"<c>"; acl.UserScope
//     principal in CapUserAll::"<c>" || (principal in CapNwea::"<c>" &&
//     resource.kmapDistrictOwned); acl.CountryScope (principal is User &&
//     principal.countryGrants.contains({capability, country:
//     resource.country})); a service checker's user_all and user_nwea
//     principal in CapUserAll::"<c>", CapNwea::"<c>". The language has no quantifier over a grant
//     list, so the loader flattens grants into these groups.
//
// Loader contract (codetospec writes none): User entities by KAID, with
// loggedIn, phantom and countryGrants, member of CapAny::"<c>" for every
// grant of c, of CapGlobal::"<c>" for a global one, CapUserAll::"<c>" for
// userKind "all", CapNwea::"<c>" for "nwea-map"; a role's capabilities at
// its grant's scope; a site admin (GCP IAM) in CapAny and CapGlobal of
// every capability. A Target per request: subject (the User a check is
// about, taken from the request where the policies' @subject annotation
// says: "arg:<param>[.field]", "parent[:field]"), coaches,
// kmapDistrictOwned, country, relations (the names of the
// relations, registry entries a checkers file pins, the caller holds to
// the request's resource). Expiry and similar are the
// loader's to apply before building entities: the language has no now().
package cedarpolicy

import (
	"fmt"
	"slices"
	"strings"
	"unicode"

	"github.com/StevenACoffman/codetospec/internal/policy"
	"github.com/cedar-policy/cedar-go"
	xast "github.com/cedar-policy/cedar-go/x/exp/ast"
	"github.com/cedar-policy/cedar-go/x/exp/schema"
	"github.com/cedar-policy/cedar-go/x/exp/schema/validate"
)

// Spec is an export: files by path, and what went in.
type Spec struct {
	Files      map[string][]byte
	Namespace  string
	Operations int
	Denies     int
	Skipped    int
	Items      int // item kinds with a view action
}

// Export renders m for service: <service>.cedar (the policies) and
// <service>.cedarschema (the schema), both parsed and the policies
// validated against the schema before they're returned.
//
// Ensures: deterministic; an error when cedar-go rejects either file.
func Export(m *policy.Model, service string) (Spec, error) {
	ns := Namespace(service)
	spec := Spec{
		Files: map[string][]byte{}, Namespace: ns, Skipped: len(m.Skipped), Items: len(m.Items),
	}
	actions := make([]string, 0, len(m.Operations))
	ops := slices.Clone(m.Operations)
	kinds := make([]string, 0, len(m.Items))
	for i := range m.Items {
		view := m.Items[i].Operation
		view.Action = ViewAction(m.Items[i].Kind)
		ops = append(ops, view)
		kinds = append(kinds, m.Items[i].Kind)
	}
	var b strings.Builder
	for i := range ops {
		op := &ops[i]
		if i < len(m.Operations) {
			actions = append(actions, op.Actions()...)
		}
		scope := actionScope(ns, op.Actions())
		subject := ""
		if op.Subject != "" {
			subject = fmt.Sprintf("@subject(%q)\n", op.Subject)
		}
		fmt.Fprintf(
			&b,
			"@id(%q)\n%spermit (principal, %s, resource);\n\n",
			"allow:"+op.Action,
			subject,
			scope,
		)
		for _, d := range op.Denies {
			fmt.Fprintf(
				&b,
				"@id(%q)\n@source(%q)\n%sforbid (principal, %s, resource)\nwhen { %s };\n\n",
				d.RuleID,
				d.Source.File()+":"+d.Source.Lines,
				subject,
				scope,
				Expr(ns, d.When),
			)
			spec.Denies++
		}
	}
	spec.Operations = len(m.Operations)
	policies := []byte(b.String())
	schemaText := []byte(Schema(ns, actions, kinds))
	if err := check(schemaText, policies); err != nil {
		return Spec{}, err
	}
	spec.Files[service+".cedar"] = policies
	spec.Files[service+".cedarschema"] = schemaText
	return spec, nil
}

// Namespace names a service's namespace, always an identifier: "ai-guide"
// → "AiGuide"; a name that would start with a digit or be empty is
// prefixed with "Service" ("001" → "Service001").
func Namespace(service string) string {
	var b strings.Builder
	upper := true
	for _, r := range service {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			upper = true
			continue
		}
		if upper {
			r = unicode.ToUpper(r)
			upper = false
		}
		b.WriteRune(r)
	}
	name := b.String()
	if name == "" || unicode.IsDigit(rune(name[0])) {
		name = "Service" + name
	}
	return name
}

// ViewAction is the action deciding one item of kind: "view:UserDistrictInfo".
func ViewAction(kind string) string {
	return policy.ViewAction + ":" + kind
}

// ItemType is the entity type of an item of kind: "UserItem". No fixed
// type's name ends in "Item", so none collides with it.
func ItemType(kind string) string {
	return kind + "Item"
}

// targetAttrs are the attributes of a Target, and of an item, which the
// same templates read.
const targetAttrs = "{subject: User, coaches: Set<User>, kmapDistrictOwned: Bool, country: String, " +
	"relations: Set<String>}"

// Schema is the namespace's schema in the Cedar schema format: actions
// apply to a Target, and an item kind's view action (ViewAction) to an
// entity of that kind's ItemType.
func Schema(ns string, actions, kinds []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "namespace %s {\n", ns)
	b.WriteString(
		"  entity CapAny;\n  entity CapGlobal;\n  entity CapUserAll;\n  entity CapNwea;\n",
	)
	b.WriteString("  type CountryGrant = {capability: String, country: String};\n")
	b.WriteString("  entity User in [CapAny, CapGlobal, CapUserAll, CapNwea] " +
		"{loggedIn: Bool, phantom: Bool, countryGrants: Set<CountryGrant>};\n")
	b.WriteString(
		"  entity Service in [CapAny, CapGlobal, CapUserAll, CapNwea];\n  entity Push;\n  entity Anonymous;\n",
	)
	fmt.Fprintf(&b, "  entity Target %s;\n", targetAttrs)
	for _, k := range kinds {
		fmt.Fprintf(&b, "  entity %s %s;\n", ItemType(k), targetAttrs)
	}
	b.WriteString("  type RequestContext = {isTask: Bool, isOktaLifecycle: Bool};\n")
	for _, a := range slices.Compact(slices.Sorted(slices.Values(actions))) {
		fmt.Fprintf(
			&b,
			"  action %q appliesTo {principal: [User, Service, Push, Anonymous], resource: [Target], "+
				"context: RequestContext};\n",
			a,
		)
	}
	for _, k := range kinds {
		fmt.Fprintf(
			&b,
			"  action %q appliesTo {principal: [User, Service, Push, Anonymous], resource: [%s], "+
				"context: RequestContext};\n",
			ViewAction(k), ItemType(k),
		)
	}
	b.WriteString("}\n")
	return b.String()
}

// Expr renders a guard's expression in the namespace ns.
func Expr(ns string, e policy.Expr) string {
	switch e.Op {
	case policy.OpAll, policy.OpAny:
		join := " && "
		if e.Op == policy.OpAny {
			join = " || "
		}
		parts := make([]string, len(e.Args))
		for i, a := range e.Args {
			parts[i] = Expr(ns, a)
		}
		return "(" + strings.Join(parts, join) + ")"
	case policy.OpNot:
		return "!(" + Expr(ns, e.Args[0]) + ")"
	case policy.OpCheck:
		return leafExpr(ns, e.Check)
	}
	return "false"
}

// leafExpr renders one check from its pinned template.
func leafExpr(ns string, l *policy.Leaf) string {
	user := "principal is " + ns + "::User"
	switch l.Kind {
	case policy.LeafLogin:
		return "(" + user + " && principal.loggedIn)"
	case policy.LeafLoginStrict:
		return "(" + user + " && principal.loggedIn && !principal.phantom)"
	case policy.LeafServiceAdmin:
		return "principal is " + ns + "::Service"
	case policy.LeafPush:
		return "principal is " + ns + "::Push"
	case policy.LeafRelation:
		return fmt.Sprintf("resource.relations.contains(%q)", l.Relation)
	case policy.LeafTask:
		return "context.isTask"
	case policy.LeafOkta:
		return "context.isOktaLifecycle"
	case policy.LeafSelf:
		return "(" + user + " && resource.subject == principal)"
	case policy.LeafCoach:
		return "(" + user + " && resource.coaches.contains(principal))"
	case policy.LeafCapability:
		return capabilityExpr(ns, l)
	}
	return "false"
}

// capabilityExpr renders a capability check by its matcher's template.
func capabilityExpr(ns string, l *policy.Leaf) string {
	group := func(t string) string { return fmt.Sprintf("principal in %s::%s::%q", ns, t, l.Capability) }
	switch l.Scope {
	case policy.ScopeGlobal:
		return group("CapGlobal")
	case policy.ScopeUser:
		return "(" + group(
			"CapUserAll",
		) + " || (" + group(
			"CapNwea",
		) + " && resource.kmapDistrictOwned))"
	case policy.ScopeCountry:
		return fmt.Sprintf("(principal is %s::User && principal.countryGrants.contains("+
			"{capability: %q, country: resource.country}))", ns, l.Capability)
	case policy.ScopeUserAll:
		return group("CapUserAll")
	case policy.ScopeUserNwea:
		return group("CapNwea")
	}
	return group("CapAny")
}

func actionUID(ns, action string) string {
	return fmt.Sprintf("%s::Action::%q", ns, action)
}

// actionScope is a policy's action scope over actions: equality for one,
// membership for several (an operation and its aliases).
func actionScope(ns string, actions []string) string {
	if len(actions) == 1 {
		return "action == " + actionUID(ns, actions[0])
	}
	uids := make([]string, len(actions))
	for i, a := range actions {
		uids[i] = actionUID(ns, a)
	}
	return "action in [" + strings.Join(uids, ", ") + "]"
}

// check parses the schema and the policies with cedar-go and validates
// every policy against the schema.
func check(schemaText, policies []byte) error {
	var s schema.Schema
	if err := s.UnmarshalCedar(schemaText); err != nil {
		return fmt.Errorf("cedarpolicy: schema: %w", err)
	}
	resolved, err := s.Resolve()
	if err != nil {
		return fmt.Errorf("cedarpolicy: resolve schema: %w", err)
	}
	ps, err := cedar.NewPolicySetFromBytes("policies.cedar", policies)
	if err != nil {
		return fmt.Errorf("cedarpolicy: policies: %w", err)
	}
	v := validate.New(resolved, validate.WithStrict())
	for id, p := range ps.All() {
		if err = v.Policy(string(id), (*xast.Policy)(p.AST())); err != nil {
			return fmt.Errorf("cedarpolicy: policy %s: %w", id, err)
		}
	}
	return nil
}
