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
	// An else branch changes what the outer condition means: no rule.
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
