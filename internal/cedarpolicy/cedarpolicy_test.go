package cedarpolicy_test

import (
	"bytes"
	"fmt"
	"os"
	"testing"

	"github.com/StevenACoffman/codetospec/internal/cedarpolicy"
	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/graph"
	"github.com/StevenACoffman/codetospec/internal/policy"
	"github.com/cedar-policy/cedar-go"
	"github.com/cedar-policy/cedar-go/types"
)

const (
	identityGoName = "CanViewUserIdentity"
	service        = "users"
	ns             = "Users"
	goldenFile     = "testdata/users.golden.cedar"
	capability     = "can_view_user_identity"
	actor          = "kaid_actor"
	userType       = "User"
	svcType        = "Service"
	pushType       = "Push"
)

// usersCheckers pins usersModel's checks the extractor doesn't know.
func usersCheckers() policy.Checkers {
	one := 1
	return policy.Checkers{
		// Every pinnable kind beyond the extractor's: attributes of the
		// caller, the resource and the request, a negation, the locale and
		// explicit scopes, a relation to an argument's user.
		"permissions.CanEditBio": {All: []policy.Checker{
			{Means: policy.MeansAttribute, Of: policy.HolderCaller, Attribute: "internal_user"},
			{
				Not: &policy.Checker{
					Means:     policy.MeansAttribute,
					Of:        policy.HolderResource,
					Attribute: "deleted",
				},
			},
			{Means: policy.MeansAttribute, Of: policy.HolderContext, Attribute: "bio_flag"},
			{Means: policy.MeansCapability, Capability: identityGoName, Scope: policy.ScopeLocale},
			{
				Means:      policy.MeansCapability,
				Capability: identityGoName,
				Scope:      policy.ScopeExplicit,
			},
			{Means: policy.MeansRelation, Relation: "managing_coach_of", SubjectArg: &one},
		}},
		"permissions.CanViewCDI": {Means: policy.MeansRelation, Relation: "teacher_of"},
		// districts' ActorCanViewUserDistrictData, as its code reads, plus a
		// content grant (the content scope).
		"permissions.ActorCanViewUserDistrictData": {Any: []policy.Checker{
			{Means: policy.MeansServiceAdmin},
			{
				Means:       policy.MeansCapability,
				Capability:  identityGoName,
				Scope:       policy.ScopeContent,
				ContentKind: "Video",
			},
			{
				Means:      policy.MeansCapability,
				Capability: identityGoName,
				Scope:      policy.ScopeUserAll,
			},
			{
				Means:      policy.MeansCapability,
				Capability: identityGoName,
				Scope:      policy.ScopeUserNwea,
			},
		}},
		// districts' VisibleAssignmentDistrictInfos-like filter; an item is
		// also visible to its own user.
		"permissions.VisibleThings": {Item: "Thing", Any: []policy.Checker{
			{Means: policy.MeansServiceAdmin},
			{Means: policy.MeansSelf},
			{All: []policy.Checker{
				{
					Means:      policy.MeansCapability,
					Capability: identityGoName,
					Scope:      policy.ScopeUserNwea,
				},
				{Means: policy.MeansRelation, Relation: "in_kmap_district"},
			}},
		}},
	}
}

// usersModel: User.emails denied unless the caller is the user or holds
// can_view_user_identity on a user scope; Query.time is open;
// Mutation.login denied unless logged in strictly or a service-admin call;
// Query.cdi denied unless the caller is a teacher of the resource (a
// pinned checker returning an error), Query.cdiAlias delegating to it;
// Mutation.notify denied unless a Pub/Sub push.
// notifyRateLimit is a rate-limit guard of Mutation.notify: no forbid, an
// @obligation on its permit.
func notifyRateLimit() graph.Node {
	return graph.Node{
		ID:      "rule.hook.ratelimit-notify",
		Type:    graph.TypeRule,
		Sources: []extract.Ref{{Path: "resolvers/u.go", Lines: "1-3"}},
		Extra: map[string]string{
			graph.ExtraExtractedBy: "webapp",
			graph.ExtraOperation:   "Mutation.notify",
			graph.ExtraChecks:      "ratelimit.ByIP",
			graph.ExtraCondition:   `{"var":"caller.overRateLimit.Notify","op":"equal","value":true}`,
			graph.ExtraCheckArgs: `[{"check":"ratelimit.ByIP","var":"caller.overRateLimit.Notify",` +
				`"rate":{"key":"Notify","count":"10","window":"60 * time.Second"}}]`,
		},
	}
}

func usersModel() policy.Model {
	facts := []extract.Fact{
		{
			Kind: extract.KindCapability,
			ID:   "capability.1",
			Attrs: map[string]string{
				extract.AttrName:  identityGoName,
				extract.AttrValue: capability,
			},
		},
	}
	node := func(id, op, checks, cond, args string) graph.Node {
		return graph.Node{
			ID:      id,
			Type:    graph.TypeRule,
			Sources: []extract.Ref{{Path: "resolvers/u.go", Lines: "1-3"}},
			Extra: map[string]string{
				graph.ExtraExtractedBy: "webapp",
				graph.ExtraOperation:   op,
				graph.ExtraChecks:      checks,
				graph.ExtraCondition:   cond,
				graph.ExtraCheckArgs:   args,
			},
		}
	}
	alias := node("rule.class.authz-cdi-alias", "Query.cdiAlias", "", "", "")
	alias.Extra[graph.ExtraDelegatesTo] = "classResolver.Cdi"
	alias.Extra[graph.ExtraDelegatesToOperation] = "Query.cdi"
	checkers := usersCheckers()
	things := node("rule.district.authz-things", "Query.things", "permissions.VisibleThings", "",
		`[{"check":"permissions.VisibleThings"}]`)
	things.Extra[graph.ExtraCheckScope] = "item"
	udi := node(
		"rule.district.authz-udi",
		"Query.udi",
		"permissions.ActorCanViewUserDistrictData",
		`{"not":{"var":"permissions.ActorCanViewUserDistrictData","op":"equal","value":true}}`,
		`[{"check":"permissions.ActorCanViewUserDistrictData","var":"permissions.ActorCanViewUserDistrictData"}]`,
	)
	bio := node("rule.user.authz-bio", "Mutation.editBio", "permissions.CanEditBio",
		`{"not":{"var":"permissions.CanEditBio","op":"equal","value":true}}`,
		`[{"check":"permissions.CanEditBio","var":"permissions.CanEditBio",`+
			`"args":[{"text":"ktx"},{"text":"kaid","from":"arg:kaid"}]}]`)
	return policy.BuildModel(facts, []graph.Node{
		alias,
		udi,
		things,
		bio,
		node(
			"rule.class.authz-cdi",
			"Query.cdi",
			"permissions.CanViewCDI",
			`{"var":"permissions.CanViewCDI.fails","op":"equal","value":true}`,
			`[{"check":"permissions.CanViewCDI","var":"permissions.CanViewCDI.fails","fails":true}]`,
		),
		node(
			"rule.hook.authz-notify",
			"Mutation.notify",
			"pubsub.DecodeAndValidatePushMsg",
			`{"var":"pubsub.DecodeAndValidatePushMsg.fails","op":"equal","value":true}`,
			`[{"check":"pubsub.DecodeAndValidatePushMsg","var":"pubsub.DecodeAndValidatePushMsg.fails",`+
				`"fails":true,"means":"push"}]`,
		),
		notifyRateLimit(),
		node(
			"rule.user.authz-emails",
			"User.emails",
			"acl.ActorHasPermission,acl.IsCurrentUser",
			`{"all":[{"not":{"var":"acl.IsCurrentUser","op":"equal","value":true}},`+
				`{"not":{"var":"caller.hasPermission.CanViewUserIdentity","op":"equal","value":true}}]}`,
			`[{"check":"acl.IsCurrentUser","var":"acl.IsCurrentUser","means":"self","subject":"user.Kaid","subject_from":"parent:Kaid"},`+
				`{"check":"acl.ActorHasPermission","var":"caller.hasPermission.CanViewUserIdentity","means":"capability","scope":"user","capability":"CanViewUserIdentity","matcher":"acl.UserScope","matcher_arg":"user.Kaid","subject_from":"parent:Kaid"}]`,
		),
		// Checks about two users: the second's self check names the same
		// variable as the first's.
		node(
			"rule.user.authz-transfer-from",
			"Mutation.transfer",
			"acl.IsCurrentUser",
			`{"not":{"var":"acl.IsCurrentUser","op":"equal","value":true}}`,
			`[{"check":"acl.IsCurrentUser","var":"acl.IsCurrentUser","means":"self","subject":"from","subject_from":"arg:from"}]`,
		),
		node(
			"rule.user.authz-transfer-to",
			"Mutation.transfer",
			"acl.IsCurrentUser,acl.IsCoach",
			`{"all":[{"not":{"var":"acl.IsCurrentUser","op":"equal","value":true}},`+
				`{"not":{"var":"acl.IsCoach","op":"equal","value":true}}]}`,
			`[{"check":"acl.IsCurrentUser","var":"acl.IsCurrentUser","means":"self","subject":"to","subject_from":"arg:to"},`+
				`{"check":"acl.IsCoach","var":"acl.IsCoach","means":"coach","subject":"to","subject_from":"arg:to"}]`,
		),
		node("rule.misc.authz-time", "Query.time", "open", "", ""),
		node(
			"rule.auth.authz-login",
			"Mutation.login",
			"acl.IsLoggedIn,method.RequestIsServiceAdminQuery",
			`{"all":[{"not":{"var":"caller.loggedInNonPhantom","op":"equal","value":true}},`+
				`{"not":{"var":"request.isServiceAdmin","op":"equal","value":true}}]}`,
			`[{"check":"acl.IsLoggedIn","var":"caller.loggedInNonPhantom","means":"login_strict"},`+
				`{"check":"method.RequestIsServiceAdminQuery","var":"request.isServiceAdmin","means":"service_admin"}]`,
		),
	}, checkers)
}

// TestExportGolden pins the policies (Export itself parses them and
// validates them against the schema); regenerate with UPDATE_GOLDEN=1 go
// test ./internal/cedarpolicy and review.
func TestExportGolden(t *testing.T) {
	m := usersModel()
	spec, err := cedarpolicy.Export(&m, service)
	if err != nil {
		t.Fatal(err)
	}
	got := spec.Files[service+".cedar"]
	if os.Getenv("UPDATE_GOLDEN") != "" {
		if writeErr := os.WriteFile(goldenFile, got, 0o600); writeErr != nil {
			t.Fatal(writeErr)
		}
	}
	want, err := os.ReadFile(goldenFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("policies differ from %s:\n%s", goldenFile, got)
	}
}

// TestAuthorizeAgreesWithConditions: for every case of every guarded
// operation that entities can realize, cedar.Authorize on the exported
// policies denies exactly when one of the rules' conditions holds. Whether
// a case is realized is asked of cedar-go too (each check as a one-leaf
// policy), so no Go copy of the templates decides anything.
func TestAuthorizeAgreesWithConditions(t *testing.T) {
	m := usersModel()
	spec, err := cedarpolicy.Export(&m, service)
	if err != nil {
		t.Fatal(err)
	}
	policies, err := cedar.NewPolicySetFromBytes("users.cedar", spec.Files[service+".cedar"])
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for i := range m.Operations {
		op := &m.Operations[i]
		for _, action := range op.Actions() {
			checked += agree(t, policies, op, action, "Target")
		}
	}
	for i := range m.Items {
		item := &m.Items[i]
		checked += agree(t, policies, &item.Operation, cedarpolicy.ViewAction(item.Kind),
			cedarpolicy.ItemType(item.Kind))
	}
	if checked == 0 {
		t.Fatal("no case checked")
	}
	t.Logf("%d cases checked", checked)
}

// TestItemKindNamedLikeAFixedType: an item kind may share its name with a
// type the schema always declares (ai-guide's filters are over User): the
// item's entity type stays distinct, so the export still validates.
func TestItemKindNamedLikeAFixedType(t *testing.T) {
	for _, kind := range []string{userType, "Target", svcType} {
		m := usersModel()
		m.Items[0].Kind = kind
		if _, err := cedarpolicy.Export(&m, service); err != nil {
			t.Errorf("item kind %s: %v", kind, err)
		}
	}
}

// agree checks that cedar.Authorize decides each realizable case of op, as
// action on a resource of resourceType, as the case says; it returns how
// many cases it checked.
func agree(
	t *testing.T,
	policies *cedar.PolicySet,
	op *policy.Operation,
	action, resourceType string,
) int {
	t.Helper()
	leaves := op.Leaves()
	checked := 0
	for _, c := range op.Cases() {
		entities, req := realize(t, action, resourceType, leaves, c.Given)
		if !realized(t, leaves, c.Given, entities, &req) {
			continue
		}
		decision, diag := cedar.Authorize(policies, entities, req)
		if got := decision == cedar.Deny; got != c.Denied {
			t.Errorf("%s %v: denied = %v, want %v (%+v)", action, c.Given, got, c.Denied, diag)
		}
		checked++
	}
	return checked
}

// world is what a case's checks set: the principal, its groups and
// attributes, the target's subject and coaches, the request's context.
type world struct {
	uid           func(typ, id string) types.EntityUID
	principalType string
	parents       []types.EntityUID
	attrs, ctx    types.RecordMap
	subject       types.EntityUID
	coaches       types.Set
	relations     []types.Value
	// The attribute names of each holder, and the subject's relations.
	callerAttrs, resourceAttrs, requestAttrs, subjectRelations []types.Value
	localeGrants, contentGrants                                []types.Value
	// The second user's, for an operation whose checks are about two.
	subject2          types.EntityUID
	coaches2          types.Set
	subjectRelations2 []types.Value
}

// apply makes one check hold or not.
func (w *world) apply(l *policy.Leaf, holds bool) {
	switch l.Kind {
	case policy.LeafServiceAdmin:
		if holds {
			w.principalType = svcType
		}
	case policy.LeafPush:
		if holds {
			w.principalType = pushType
		}
	case policy.LeafRelation, policy.LeafAttribute:
		if holds {
			w.addName(l)
		}
	case policy.LeafLogin, policy.LeafLoginStrict:
		if holds {
			w.attrs["loggedIn"] = types.True
		} else if l.Kind == policy.LeafLoginStrict {
			w.attrs["phantom"] = types.True
		}
	case policy.LeafTask:
		w.ctx["isTask"] = types.Boolean(holds)
	case policy.LeafOkta:
		w.ctx["isOktaLifecycle"] = types.Boolean(holds)
	case policy.LeafSelf:
		switch {
		case holds && l.Second:
			w.subject2 = w.uid("User", actor)
		case holds:
			w.subject = w.uid("User", actor)
		}
	case policy.LeafCoach:
		switch {
		case holds && l.Second:
			w.coaches2 = types.NewSet(w.uid("User", actor))
		case holds:
			w.coaches = types.NewSet(w.uid("User", actor))
		}
	case policy.LeafCapability:
		if holds {
			w.grant(l)
		}
	}
}

// localeKey is the grants' and the context's locale attribute.
const localeKey = "locale"

// grant gives the principal a grant satisfying capability check l: its
// groups, and a locale or content grant for those scopes.
func (w *world) grant(l *policy.Leaf) {
	w.parents = append(w.parents, w.capabilityGroups(l)...)
	switch l.Scope {
	case policy.ScopeLocale:
		// A grant on every locale, so the oracle covers that disjunct; the
		// Cerbos suite realizes the request's locale.
		w.localeGrants = append(w.localeGrants, types.NewRecord(types.RecordMap{
			"capability": types.String(l.Capability), localeKey: types.String(policy.EveryValue),
		}))
	case policy.ScopeContent:
		// The request's locale on every content kind, likewise.
		w.contentGrants = append(w.contentGrants, types.NewRecord(types.RecordMap{
			"capability": types.String(l.Capability), localeKey: types.String("en"),
			"contentKind": types.String(policy.EveryValue),
		}))
	}
}

// addName records the name a holding relation or attribute check needs
// in the list its template reads.
func (w *world) addName(l *policy.Leaf) {
	switch {
	case l.Kind == policy.LeafRelation && l.Subject != "" && l.Second:
		w.subjectRelations2 = append(w.subjectRelations2, types.String(l.Relation))
	case l.Kind == policy.LeafRelation && l.Subject != "":
		w.subjectRelations = append(w.subjectRelations, types.String(l.Relation))
	case l.Kind == policy.LeafRelation:
		w.relations = append(w.relations, types.String(l.Relation))
	case l.Holder == policy.HolderCaller:
		w.callerAttrs = append(w.callerAttrs, types.String(l.Attribute))
	case l.Holder == policy.HolderContext:
		w.requestAttrs = append(w.requestAttrs, types.String(l.Attribute))
	default:
		w.resourceAttrs = append(w.resourceAttrs, types.String(l.Attribute))
	}
}

// capabilityGroups are the groups the loader puts a holder of a grant
// satisfying l's matcher in.
func (w *world) capabilityGroups(l *policy.Leaf) []types.EntityUID {
	groups := []types.EntityUID{w.uid("CapAny", l.Capability)}
	switch l.Scope {
	case policy.ScopeGlobal:
		groups = append(groups, w.uid("CapGlobal", l.Capability))
	case policy.ScopeUser, policy.ScopeUserAll:
		groups = append(groups, w.uid("CapUserAll", l.Capability))
	case policy.ScopeUserNwea:
		groups = append(groups, w.uid("CapNwea", l.Capability))
	case policy.ScopeExplicit:
		groups = append(groups, w.uid("CapExplicit", l.Capability))
	}
	return groups
}

// realize builds entities and a request meant to make each check hold or
// not as given, under the loader contract.
func realize(
	t *testing.T,
	action, resourceType string,
	leaves []policy.Leaf,
	given map[string]bool,
) (types.EntityMap, cedar.Request) {
	t.Helper()
	uid := func(typ, id string) types.EntityUID {
		et, err := types.NewEntityType(ns + "::" + typ)
		if err != nil {
			t.Fatal(err)
		}
		return types.NewEntityUID(et, types.String(id))
	}
	w := world{
		uid:           uid,
		principalType: userType,
		attrs: types.RecordMap{
			"loggedIn":      types.False,
			"phantom":       types.False,
			"countryGrants": types.NewSet(),
		},
		ctx: types.RecordMap{
			"isTask": types.False, "isOktaLifecycle": types.False, localeKey: types.String("en"),
		},
		subject:  uid("User", "kaid_other"),
		coaches:  types.NewSet(),
		subject2: uid("User", "kaid_other"),
		coaches2: types.NewSet(),
	}
	for i := range leaves {
		w.apply(&leaves[i], given[leaves[i].Var])
	}
	w.attrs["attributes"] = types.NewSet(w.callerAttrs...)
	w.attrs["localeGrants"] = types.NewSet(w.localeGrants...)
	w.attrs["contentGrants"] = types.NewSet(w.contentGrants...)
	w.ctx["attributes"] = types.NewSet(w.requestAttrs...)
	principal := uid(w.principalType, actor)
	p := types.Entity{UID: principal, Parents: types.NewEntityUIDSet(w.parents...)}
	if w.principalType == userType {
		p.Attributes = types.NewRecord(w.attrs)
	}
	target := uid(resourceType, "t")
	entities := types.EntityMap{
		principal: p,
		target: {UID: target, Attributes: types.NewRecord(types.RecordMap{
			"subject":           w.subject,
			"coaches":           w.coaches,
			"kmapDistrictOwned": types.False,
			"country":           types.String("US"),
			"relations":         types.NewSet(w.relations...),
			"subjectRelations":  types.NewSet(w.subjectRelations...),
			"attributes":        types.NewSet(w.resourceAttrs...),
			"subject2":          w.subject2,
			"coaches2":          w.coaches2,
			"subjectRelations2": types.NewSet(w.subjectRelations2...),
		})},
	}
	return entities, cedar.Request{
		Principal: principal,
		Action:    uid("Action", action),
		Resource:  target,
		Context:   types.NewRecord(w.ctx),
	}
}

// realized asks cedar-go whether each check holds as given, by authorizing
// a policy permitting exactly when it holds.
func realized(
	t *testing.T,
	leaves []policy.Leaf,
	given map[string]bool,
	entities types.EntityMap,
	req *cedar.Request,
) bool {
	t.Helper()
	for _, l := range leaves {
		text := fmt.Sprintf("permit (principal, action, resource) when { %s };",
			cedarpolicy.Expr(ns, policy.Expr{Op: policy.OpCheck, Check: &l}))
		ps, err := cedar.NewPolicySetFromBytes("leaf.cedar", []byte(text))
		if err != nil {
			t.Fatalf("leaf %s: %v", text, err)
		}
		decision, _ := cedar.Authorize(ps, entities, *req)
		if (decision == cedar.Allow) != given[l.Var] {
			return false
		}
	}
	return true
}

// TestNamespace: a service's namespace is always an identifier.
func TestNamespace(t *testing.T) {
	for in, want := range map[string]string{
		"districts": "Districts", "ai-guide": "AiGuide", "graphql_gateway": "GraphqlGateway",
		"001": "Service001", "": "Service", "-": "Service",
	} {
		if got := cedarpolicy.Namespace(in); got != want {
			t.Errorf("Namespace(%q) = %q, want %q", in, got, want)
		}
	}
}
