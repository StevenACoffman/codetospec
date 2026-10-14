package resolvers

func (e *mutationResolver) AddAuthEmail(ctx context.Context, kaid string, authEmail string) (*graphql.AddAuthEmail, error) {
	ktx := kacontext.Upgrade(ctx)

	if !ktx.RequestIsServiceAdminQuery() {
		return automap.AddAuthEmailErr(ktx, errors.Unauthorized())
	}
	return nil, nil
}

func (e *mutationResolver) AddCoachRole(ctx context.Context, kaid string) (*graphql.AddCoachRole, error) {
	ktx := kacontext.Upgrade(ctx)
	if !acl.IsCurrentUser(ktx, kaid) && !ktx.RequestIsServiceAdminQuery() {
		return nil, errors.Unauthorized()
	}
	return nil, nil
}

func (r *queryResolver) CheckUsername(ctx context.Context, username string) (*bool, error) {
	acl.OpenAccess() // anyone can check a username
	return nil, nil
}

func (r *queryResolver) DeviceAuthRequestInfo(ctx context.Context) (*string, error) {
	ktx := kacontext.Upgrade(ctx)
	allowed := acl.IsLoggedIn(ktx, acl.PhantomProhibited)
	if !allowed {
		return nil, errors.Unauthorized()
	}
	return nil, nil
}

func (r *userResolver) PendingEmailVerifications(ctx context.Context, user *User) ([]string, error) {
	ktx := kacontext.Upgrade(ctx)
	if !acl.IsCurrentUser(ktx, *user.Kaid) &&
		!acl.ActorHasPermission(ktx, capabilities.CanViewUserIdentity, acl.UserScope(*user.Kaid)) {
		return nil, nil
	}
	hasPermission, err := acl.ActorHasPermissionErr(ktx, capabilities.CanChangeUserData, acl.AnyScope)
	if err != nil {
		return nil, err
	}
	if !hasPermission || (customCheck(ktx) && acl.IsCoachedByActor(ktx, *user.Kaid)) {
		return nil, errors.NotAllowed("nope")
	}
	return nil, nil
}

func (r *userResolver) Emails(ctx context.Context, user *User) ([]string, error) {
	if !permissions.UserCanViewEmails(ctx, *user.Kaid) {
		return nil, nil
	}
	visible, err := permissions.LoadVisibleEmails(ctx, *user.Kaid)
	// Falls through when the check holds: not a guard.
	if permissions.UserCanViewEmails(ctx, "admin") {
		if err != nil {
			return nil, err
		}
		visible = nil
	}
	return visible, err
}

func (r *userResolver) Nickname(ctx context.Context, user *User) (*string, error) {
	_, err := permissions.LoadVisibleEmails(ctx, *user.Kaid)
	if err != nil { // plumbing: not a guard by itself
		return nil, err
	}
	canView, err := permissions.UserCanViewEmails(ctx, *user.Kaid), lookup(ctx)
	if err != nil {
		err = nil // reassigned in a nested block: err no longer holds lookup's error
	}
	if err != nil || !canView {
		return nil, errors.Unauthorized()
	}
	ok, err := acl.ActorHasPermissionErr(ctx, capabilities.CanViewUserIdentity, acl.AnyScope)
	if err != nil || !ok {
		return nil, errors.Unauthorized()
	}
	return nil, nil
}

func (r *userResolver) Bio(ctx context.Context, user *User) (*string, error) {
	ktx := kacontext.Upgrade(ctx)
	// Nested: one rule, "not service-admin AND not the current user".
	if !ktx.RequestIsServiceAdminQuery() {
		actor, err := ktx.RequestUser()
		if err != nil { // plumbing: no rule
			return nil, err
		}
		if !acl.IsCurrentUser(ktx, actor.Kaid) {
			return nil, nil
		}
	}
	// A guard under an if with an else: "user.Kaid != nil AND not logged in";
	// the else branch returns nothing without a check: no rule.
	if user.Kaid != nil {
		if !acl.IsLoggedIn(ktx) {
			return nil, errors.Unauthorized()
		}
	} else {
		return nil, nil
	}
	return nil, nil
}

func (r *userResolver) Partner(ctx context.Context, user *User) (*string, error) {
	ktx := kacontext.Upgrade(ctx)
	// Ends in a successful return: only the nested check is a guard.
	if user.Kaid != nil {
		actor, err := ktx.RequestUser()
		if err != nil {
			return nil, errors.Unauthorized()
		}
		return &actor.Kaid, nil
	}
	return nil, nil
}

func (r *userResolver) Notes(ctx context.Context, user *User) ([]string, error) {
	// A filter's error is a failure: no rule.
	notes, err := permissions.VisibleNotes(ctx)
	if err != nil {
		return nil, err
	}
	// A checker that denies through its error, in an if's init: one rule.
	if err := permissions.RequireEditor(ctx); err != nil {
		return nil, err
	}
	return notes, nil
}

func (r *userResolver) Drafts(ctx context.Context, user *User) ([]string, error) {
	_audit(ctx)
	// Denies through its error, though it returns a bool too: one rule.
	isAdmin, err := _canEditDrafts(ctx)
	if err != nil {
		return nil, err
	}
	_ = isAdmin
	return nil, nil
}

// _canEditDrafts reports whether the caller administers drafts, and denies
// callers who cannot edit them at all.
//
//ka:permission-check
func _canEditDrafts(ctx context.Context) (bool, error) {
	if !acl.IsLoggedIn(ctx) {
		return false, errors.Unauthorized("cannot edit drafts")
	}
	return acl.ActorHasPermission(ctx, capabilities.CanEditDrafts, acl.AnyScope), nil
}

func (r *mutationResolver) Signup(ctx context.Context, kaid string) (*string, error) {
	_audit(ctx)
	if ratelimit.ByIP(ctx, "Signup",
		ratelimit.MaxRequestsPerWindow(200),
		ratelimit.LimitWindow(600*time.Second),
	) {
		return nil, auth.TooManyRequests
	}
	if ratelimit.ByAccount(ctx, "SignupAccount", kaid,
		ratelimit.MaxRequestsPerWindow(5),
		ratelimit.LimitWindow(time.Minute*10),
	) {
		return nil, auth.TooManyRequests
	}
	return nil, nil
}

// Declared in schema.graphql, gqlgen's default file beside topical ones: the
// method takes its resolver file's domain, not "schema".
func (r *queryResolver) ServerTime(ctx context.Context) (*string, error) {
	acl.OpenAccess()
	return nil, nil
}

func (r *userResolver) Grants(ctx context.Context, user *User) ([]string, error) {
	ktx := kacontext.Upgrade(ctx)
	// A condition bound to a local reads as that condition; an empty slice
	// is no data.
	authorized := ktx.RequestIsServiceAdminQuery() || acl.IsCurrentUser(ktx, *user.Kaid)
	if !authorized {
		return []string{}, nil
	}
	return nil, nil
}

func (r *userResolver) Badges(ctx context.Context, user *User) ([]string, error) {
	ktx := kacontext.Upgrade(ctx)
	// Access through one check or another: ok is "logged in OR admin".
	ok := acl.IsLoggedIn(ktx)
	if !ok {
		ok = ktx.RequestIsServiceAdminQuery()
	}
	if !ok {
		return nil, errors.Unauthorized()
	}
	return nil, nil
}

func (r *queryResolver) Search(ctx context.Context, term string) ([]string, error) {
	ktx := kacontext.Upgrade(ctx)
	// A switch denying in its default: "not admin AND no permission".
	switch {
	case ktx.RequestIsServiceAdminQuery():
		term = ""
	case acl.ActorHasPermission(ktx, capabilities.CanViewUserIdentity, acl.AnyScope):
		term = "scoped"
	default:
		return []string{}, nil
	}
	return nil, nil
}

func (r *userResolver) Avatar(ctx context.Context, user *User) (*string, error) {
	// An else if after plumbing reads like the next statement.
	canEdit, err := acl.ActorHasPermissionErr(ctx, capabilities.CanEditDrafts, acl.AnyScope)
	if err != nil {
		return nil, err
	} else if !canEdit {
		return nil, errors.Unauthorized()
	}
	return nil, nil
}

func (r *mutationResolver) Merge(ctx context.Context, kaids []string) ([]error, error) {
	var results []error
	// A per-item rejection: the loop carries on.
	for _, kaid := range kaids {
		if !acl.IsCurrentUser(ctx, kaid) {
			results = append(results, errors.Unauthorized())
			continue
		}
	}
	return results, nil
}

func (r *userResolver) Peek(ctx context.Context, user *User, kaids []string) ([]string, error) {
	// A return inside a loop: one item denies the whole request.
	for _, kaid := range kaids {
		if !acl.IsCoachedByActor(ctx, kaid) {
			return nil, nil
		}
	}
	return kaids, nil
}

func (r *mutationResolver) Roster(ctx context.Context) (*string, error) {
	ktx := kacontext.Upgrade(ctx)
	// Three levels deep.
	if !ktx.RequestIsServiceAdminQuery() {
		if !acl.ActorHasPermission(ktx, capabilities.CanRosterDistrict, acl.AnyScope) {
			if !acl.ActorHasPermission(ktx, capabilities.CanAccessDevadmin, acl.AnyScope) {
				return nil, errors.Unauthorized()
			}
		}
	}
	return nil, nil
}

func (r *mutationResolver) OptOut(ctx context.Context, kaid *string) (*string, error) {
	// Guards in both branches: the else one holds under "kaid == nil" negated.
	if kaid == nil {
		if !acl.IsLoggedIn(ctx) {
			return nil, errors.Unauthorized()
		}
	} else {
		if !acl.IsCurrentUser(ctx, *kaid) {
			return nil, errors.Unauthorized()
		}
	}
	return nil, nil
}

func (r *mutationResolver) Cooldown(ctx context.Context, kaid string) (*string, error) {
	// A GraphQL error code denies as errors.Unauthorized() does.
	ok, err := cross_service.InKhanmigoDistrict(ctx, kaid)
	if err != nil || !ok {
		return Failure(graphql.CooldownErrorCodeUnauthorized, "not in a Khanmigo district")
	}
	return nil, nil
}

func (r *mutationResolver) Rename(ctx context.Context, name string) (*string, error) {
	// validateRename is unmarked, but denies through RequireEditor's error.
	if err := validateRename(ctx, name); err != nil {
		return nil, err
	}
	return nil, nil
}

func validateRename(ctx context.Context, name string) error {
	if name == "" {
		return errors.InvalidInput("empty name")
	}
	return permissions.RequireEditor(ctx)
}

func (r *mutationResolver) Login(ctx context.Context, token string) (*string, error) {
	// No guard shape: the rule names the check the resolver makes.
	kaid, err := permissions.LoginWithRateLimit(ctx, token)
	if err != nil {
		return nil, err
	}
	return &kaid, nil
}

func (r *queryResolver) Tools(ctx context.Context) ([]string, error) {
	ktx := kacontext.Upgrade(ctx)
	user, err := ktx.RequestUser()
	if err != nil {
		return nil, err
	}
	// kaid is the caller's own: IsCurrentUser on it always holds, so this
	// guard never denies a logged-in caller.
	kaid := user.Kaid
	authorized := ktx.RequestIsServiceAdminQuery() || acl.IsCurrentUser(ktx, kaid)
	if !authorized {
		return []string{}, nil
	}
	return nil, nil
}

func (r *userResolver) Coachees(ctx context.Context, user *User, kaids []string) ([]string, error) {
	// A filter the shared packages mark (testdata/webapp/pkg): visibility.
	coached := acl.ManyCoachedByActor(ctx, kaids)
	return keys(coached), nil
}

func (r *userResolver) Legacy(ctx context.Context, user *User) (*string, error) {
	acl.TODO() // ported without a permission check
	return nil, nil
}

// Called from two domains (auth-emails, user): no domain fact.
func _audit(ctx context.Context) {}

// Not a resolver: no guard facts, no domain fact.
func helper(ctx context.Context) bool {
	return acl.IsLoggedIn(ctx, acl.PhantomProhibited)
}

// A resolver whose field no schema declares: rule facts in the fallback
// domain, a warning, no domain fact.
func (r *queryResolver) Orphan(ctx context.Context) (bool, error) {
	if !acl.IsLoggedIn(ctx) {
		return false, errors.Unauthorized()
	}
	return true, nil
}

const maxAttempts = 3

const (
	tokenTTL  = 0x10
	unchecked = iota
)

// A helper predicate: one return of an access check.
func _isActor(ctx context.Context, kaid string) bool {
	return acl.IsCurrentUser(ctx, kaid)
}

func (r *queryResolver) RecoveryCodes(ctx context.Context, kaid string) ([]string, error) {
	if !_isActor(ctx, kaid) {
		return nil, nil
	}
	codes, err := loadCodes(ctx, kaid)
	if err != nil { // error plumbing: not a guard
		return nil, err
	}
	if codes == nil { // empty result, no authorization error: not a guard
		return nil, nil
	}
	return codes, nil
}

func (e *mutationResolver) EnableTotp(ctx context.Context, kaid string) (string, error) {
	underAgeGate, err := age.IsUnderAgeGateByKaid(ctx, kaid)
	if err != nil {
		return "", err
	}
	if underAgeGate {
		return "", errors.Unauthorized("under age gate")
	}
	return "secret", nil
}
