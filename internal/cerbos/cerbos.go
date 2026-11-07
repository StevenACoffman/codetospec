// Package cerbos renders a policy.Model as Cerbos policies: the one place
// that knows the format. Pinned to Cerbos's api.cerbos.dev/v1 resource
// policy and test suite schemas at github.com/cerbos/cerbos 6340b18
// (testdata/SCHEMAS.txt).
//
// Mapping:
//   - one resource policy per service, resource kind = the service name;
//     one action per operation ("User.emails");
//   - one resource policy per item kind a pinned visibility filter
//     decides, resource kind "<service>.<Kind>", action "view": the
//     decision on one item of a filtered result;
//   - an EFFECT_ALLOW rule over the exported operations for the principal
//     kinds the loader sets as roles (anonymous, push, service, user), and a
//     separate one each for open (acl.OpenAccess) and unchecked (acl.TODO)
//     operations, so they stay visible; an operation whose guards pass
//     only some kinds (policy.Operation.Principals: a push handler) is
//     allowed for those roles alone, one rule "allow-<roles>" per set;
//   - an alias operation (its handler only delegates) shares its
//     target's rules: each lists both actions;
//   - one EFFECT_DENY rule per guard, named after its rule id, with
//     roles ["*"] so it binds every role (a DENY wins only within a role),
//     whose CEL condition holds exactly when the guard rejects.
//
// Loader contract, what the PDP's caller must supply (codetospec doesn't
// write a loader):
//   - P.id the caller's KAID; P.roles one of "user", "service" (a
//     service-admin request), "push" (a Pub/Sub push whose token
//     validated), "anonymous";
//   - P.attr.loggedIn, P.attr.phantom (bool);
//   - P.attr.grants, the caller's grants as a list of {capability (stored
//     name), global (bool: every scope key at its global value), userKind,
//     country, locale, explicit (bool: granted to the caller, not implied
//     by a role such as admin's), contentKinds (the content kinds a
//     content grant covers, empty for others); a grant on every locale has
//     locale "*", one on every content kind "*" among its contentKinds}, a role's capabilities
//     expanded at the
//     role grant's scope, and every capability at global scope for a site
//     admin (GCP IAM);
//   - P.attr.attributes, the names of the caller's boolean attributes a
//     checkers file pins (internal_user, has_any_permission…);
//   - R.attr.subject, the user a self or user-scope check is about, taken
//     from the request where the policy's "subject:<action>" annotation
//     says ("arg:<param>[.field]", "parent[:field]");
//     R.attr.subject2, R.attr.coaches2 and R.attr.subjectRelations2,
//     the same of the second user of an operation whose checks are about
//     two (annotation "subject2:<action>"), always set (a placeholder
//     user when there is none);
//     R.attr.coaches, that user's coaches' KAIDs; R.attr.kmapDistrictOwned
//     (bool, webapp's nwea-map scope test); R.attr.country;
//     R.attr.isTask, R.attr.isOktaLifecycle (bool, the request's kind);
//     R.attr.relations, the names of the relations (registry entries a
//     checkers file pins) the caller holds to the request's resource;
//     R.attr.subjectRelations, those the caller holds to R.attr.subject;
//     R.attr.attributes, the resource's (or item's) boolean attributes;
//     R.attr.requestAttributes, the request's (a feature flag on for the
//     caller…); R.attr.locale, the request's locale.
//
// Scope matchers by identity (each template follows the matcher's code in
// pkg/khan/acl): acl.AnyScope any grant of the capability; acl.GlobalScope
// a global one; acl.UserScope userKind "all", or "nwea-map" on
// KMAP-district-owned data; acl.CountryScope the resource's country; a
// service checker's user_all and user_nwea userKind "all" and "nwea-map",
// whatever the data; locale a grant for the request's locale; explicit
// any grant the caller holds itself, not through a role's implicit ones;
// content a grant for the request's locale covering the pinned content
// kind. A locale or content grant on every locale ("*") matches any
// request's, and one on every content kind any kind, as webapp's admin
// service decides.
package cerbos

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/StevenACoffman/codetospec/internal/policy"
	"gopkg.in/yaml.v3"
)

// APIVersion is the only api version Cerbos accepts.
const APIVersion = "api.cerbos.dev/v1"

// subjectAnnotation prefixes the policy annotation naming, per action,
// where the PEP takes R.attr.subject from (policy.Operation.Subject):
// "subject:User.emails": "parent:Kaid".
const subjectAnnotation = "subject:"

// subject2Annotation is the same for R.attr.subject2, the second user of
// an operation whose checks are about two (policy.Operation.Subject2).
const subject2Annotation = "subject2:"

// Effects of a rule, and of a test's expectation.
const (
	effectAllow = "EFFECT_ALLOW"
	effectDeny  = "EFFECT_DENY"
)

// Principal kinds, the roles the loader sets.
const (
	RoleUser      = "user"
	RoleService   = "service"
	RolePush      = "push"
	RoleAnonymous = "anonymous"
)

// Spec is an export: files by path, and what went in.
type Spec struct {
	Files      map[string][]byte
	Operations int // exported operations, open and unchecked included
	Items      int // item kinds with a view policy
	Denies     int
	Skipped    int // guards left out (see policy.Model.Skipped)
}

// Export renders m for service as resource policies, the service's and
// one per item kind (ItemResource), and the test suite that pins their
// decisions (see TestSuite).
//
// Requires: service names a resource kind ([^!*?[]{}]+).
// Ensures: deterministic; m unmodified.
func Export(m *policy.Model, service string) (Spec, error) {
	spec := Spec{
		Files: map[string][]byte{}, Skipped: len(m.Skipped),
		Operations: len(m.Operations), Items: len(m.Items),
	}
	resources := []resourceOps{{service, m.Operations}}
	for i := range m.Items {
		resources = append(resources, resourceOps{
			ItemResource(service, m.Items[i].Kind), []policy.Operation{m.Items[i].Operation},
		})
	}
	for _, r := range resources {
		doc, denies := policyFor(r.resource, r.ops)
		data, err := yaml.Marshal(&doc)
		if err != nil {
			return Spec{}, fmt.Errorf("cerbos: encode policy: %w", err)
		}
		spec.Files["resource_policies/"+r.resource+".yaml"] = data
		spec.Denies += denies
	}
	suite, err := TestSuite(m, service)
	if err != nil {
		return Spec{}, err
	}
	spec.Files["tests/"+service+"_test.yaml"] = suite
	return spec, nil
}

// ItemResource is the resource kind of a service's items of kind:
// "districts.UserDistrictInfo".
func ItemResource(service, kind string) string {
	return service + "." + kind
}

// resourceOps is a resource kind and the operations its policy decides.
type resourceOps struct {
	resource string
	ops      []policy.Operation
}

// policyFor is the resource policy deciding ops on resource, and its
// number of deny rules.
func policyFor(resource string, ops []policy.Operation) (policyDoc, int) {
	doc := policyDoc{APIVersion: APIVersion, ResourcePolicy: resourcePolicy{
		Resource: resource, Version: "default",
	}}
	var allowed, open, unchecked []string
	byRoles := map[string][]string{} // restricted operations' actions, by role set
	denies := 0
	for i := range ops {
		op := &ops[i]
		switch {
		case op.Open:
			open = append(open, op.Actions()...)
		case op.Unchecked:
			unchecked = append(unchecked, op.Actions()...)
		case op.Principals != nil:
			key := strings.Join(op.Principals, "-")
			byRoles[key] = append(byRoles[key], op.Actions()...)
		default:
			allowed = append(allowed, op.Actions()...)
		}
		doc.annotateSubjects(op)
		for _, d := range op.Denies {
			doc.ResourcePolicy.Rules = append(doc.ResourcePolicy.Rules, rule{
				Name:      d.RuleID,
				Actions:   op.Actions(),
				Effect:    effectDeny,
				Roles:     []string{"*"},
				Condition: &ruleCondition{Match: match{Expr: Expr(d.When)}},
			})
			denies++
		}
	}
	allRoles := []string{RoleAnonymous, RolePush, RoleService, RoleUser}
	allows := []rule{}
	for _, a := range []struct {
		name    string
		actions []string
	}{{"allow-guarded", allowed}, {"open-access", open}, {"unchecked", unchecked}} {
		if len(a.actions) > 0 {
			allows = append(
				allows,
				rule{Name: a.name, Actions: a.actions, Effect: effectAllow, Roles: allRoles},
			)
		}
	}
	for _, key := range slices.Sorted(maps.Keys(byRoles)) {
		// Allowed for these principal kinds alone: no request of another
		// kind passes the operation's guards.
		allows = append(allows, rule{
			Name: "allow-" + key, Actions: byRoles[key], Effect: effectAllow,
			Roles: strings.Split(key, "-"),
		})
	}
	doc.ResourcePolicy.Rules = append(allows, doc.ResourcePolicy.Rules...)
	return doc, denies
}

// annotateSubjects records, per action of op, where the PEP takes the
// users op's checks are about from (subject, and subject2 for a second).
func (doc *policyDoc) annotateSubjects(op *policy.Operation) {
	if op.Subject == "" {
		return
	}
	if doc.Metadata == nil {
		doc.Metadata = &metadata{Annotations: map[string]string{}}
	}
	for _, a := range op.Actions() {
		doc.Metadata.Annotations[subjectAnnotation+a] = op.Subject
		if op.Subject2 != "" {
			doc.Metadata.Annotations[subject2Annotation+a] = op.Subject2
		}
	}
}

// Expr renders a guard's expression as CEL.
func Expr(e policy.Expr) string {
	switch e.Op {
	case policy.OpAll, policy.OpAny:
		join := " && "
		if e.Op == policy.OpAny {
			join = " || "
		}
		parts := make([]string, len(e.Args))
		for i, a := range e.Args {
			parts[i] = Expr(a)
		}
		return "(" + strings.Join(parts, join) + ")"
	case policy.OpNot:
		return "!(" + Expr(e.Args[0]) + ")"
	case policy.OpCheck:
		return leafExpr(e.Check)
	}
	return "false"
}

// leafExpr renders one check from its pinned template.
func leafExpr(l *policy.Leaf) string {
	switch l.Kind {
	case policy.LeafLogin:
		return "P.attr.loggedIn == true"
	case policy.LeafLoginStrict:
		return "(P.attr.loggedIn == true && P.attr.phantom == false)"
	case policy.LeafServiceAdmin:
		return `"` + RoleService + `" in P.roles`
	case policy.LeafPush:
		return `"` + RolePush + `" in P.roles`
	case policy.LeafRelation:
		if l.Subject != "" {
			return fmt.Sprintf("%q in R.attr.subjectRelations%s", l.Relation, slot(l))
		}
		return fmt.Sprintf("%q in R.attr.relations", l.Relation)
	case policy.LeafAttribute:
		return fmt.Sprintf("%q in %s", l.Attribute, attributesOf(l.Holder))
	case policy.LeafTask:
		return "R.attr.isTask == true"
	case policy.LeafOkta:
		return "R.attr.isOktaLifecycle == true"
	case policy.LeafSelf:
		return "R.attr.subject" + slot(l) + " == P.id"
	case policy.LeafCoach:
		return "P.id in R.attr.coaches" + slot(l)
	case policy.LeafCapability:
		return fmt.Sprintf(
			"P.attr.grants.exists(g, g.capability == %q && %s)",
			l.Capability,
			scopeTest(l),
		)
	}
	return "false"
}

// slot is the suffix of the resource attributes about check l's user:
// "2" for an operation's second user, else "".
func slot(l *policy.Leaf) string {
	if l.Second {
		return "2"
	}
	return ""
}

// scopeTest is the CEL test of a grant g against capability check l's
// pinned matcher.
func scopeTest(l *policy.Leaf) string {
	switch l.Scope {
	case policy.ScopeGlobal:
		return "g.global == true"
	case policy.ScopeUser:
		return `(g.userKind == "all" || (g.userKind == "nwea-map" && R.attr.kmapDistrictOwned == true))`
	case policy.ScopeCountry:
		return "g.country == R.attr.country"
	case policy.ScopeUserAll:
		return `g.userKind == "all"`
	case policy.ScopeUserNwea:
		return `g.userKind == "nwea-map"`
	case policy.ScopeLocale:
		return localeTest
	case policy.ScopeExplicit:
		return "g.explicit == true"
	case policy.ScopeContent:
		return fmt.Sprintf("%s && (%q in g.contentKinds || %q in g.contentKinds)",
			localeTest, l.ContentKind, policy.EveryValue)
	}
	return "true" // any scope
}

// localeTest is the CEL test of a grant g for the request's locale, or
// for every locale (policy.EveryValue).
const localeTest = `(g.locale == R.attr.locale || g.locale == "` + policy.EveryValue + `")`

// attributesOf is the CEL list of an attribute holder's attribute names.
func attributesOf(holder string) string {
	switch holder {
	case policy.HolderCaller:
		return "P.attr.attributes"
	case policy.HolderContext:
		return "R.attr.requestAttributes"
	}
	return "R.attr.attributes"
}

// The Cerbos policy document, as much of it as the export writes.
type (
	policyDoc struct {
		APIVersion     string         `yaml:"apiVersion"`
		Metadata       *metadata      `yaml:"metadata,omitempty"`
		ResourcePolicy resourcePolicy `yaml:"resourcePolicy"`
	}
	metadata struct {
		Annotations map[string]string `yaml:"annotations"`
	}
	resourcePolicy struct {
		Resource string `yaml:"resource"`
		Version  string `yaml:"version"`
		Rules    []rule `yaml:"rules"`
	}
	rule struct {
		Name      string         `yaml:"name"`
		Actions   []string       `yaml:"actions"`
		Effect    string         `yaml:"effect"`
		Roles     []string       `yaml:"roles"`
		Condition *ruleCondition `yaml:"condition,omitempty"`
	}
	ruleCondition struct {
		Match match `yaml:"match"`
	}
	match struct {
		Expr string `yaml:"expr"`
	}
)
