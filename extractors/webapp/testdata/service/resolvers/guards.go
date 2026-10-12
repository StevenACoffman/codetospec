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
