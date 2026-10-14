package resolvers

// aiProfileResolver resolves AIProfile, not "AiProfile" (see the factory).
func (r *aiProfileResolver) Summary(ctx context.Context, p *AIProfile) (*string, error) {
	if !acl.IsLoggedIn(ctx) {
		return nil, errors.Unauthorized()
	}
	return p.Summary, nil
}

// Entity finders: the type comes from the method name, longest @key type
// first ("RowByTeacher", not "Row").
func (r *entityResolver) FindManyUserByKaids(ctx context.Context, kaids []string) ([]*User, error) {
	if !acl.IsLoggedIn(ctx) {
		return nil, errors.Unauthorized()
	}
	return nil, nil
}

func (r *entityResolver) FindManyRowByTeacherByTeacherIDAndKaLocales(ctx context.Context, reps []*RowByTeacherKey) ([]*RowByTeacher, error) {
	acl.OpenAccess()
	return nil, nil
}

func (r *entityResolver) FindAIProfileByID(ctx context.Context, id string) (*AIProfile, error) {
	acl.TODO()
	return nil, nil
}

// A private helper holds the check; gqlgen never calls it.
func (e *mutationResolver) BlockEmail(ctx context.Context, email string) (*bool, error) {
	if err := e._blockEmail(ctx, email); err != nil {
		return nil, err
	}
	return nil, nil
}

func (e *mutationResolver) _blockEmail(ctx context.Context, email string) error {
	if !acl.ActorHasPermission(ctx, capabilities.CanManageBlockList, acl.GlobalScope) {
		return errors.Unauthorized()
	}
	return nil
}

// _unused has no caller: no operation, and no schema warning.
func (e *mutationResolver) _unused(ctx context.Context) error {
	if !acl.IsLoggedIn(ctx) {
		return errors.Unauthorized()
	}
	return nil
}

// An alias finder: checked as the finder it calls.
func (r *entityResolver) FindUserByKaid(ctx context.Context, kaid string) (*User, error) {
	users, err := r.FindManyUserByKaids(ctx, []string{kaid})
	if err != nil || len(users) == 0 {
		return nil, err
	}
	return users[0], nil
}

// An alias of a finder with no rule: no delegation is claimed.
func (r *entityResolver) FindAIProfileByKey(ctx context.Context, id string) (*AIProfile, error) {
	return r.FindAIProfileByUnchecked(ctx, id)
}

func (r *entityResolver) FindAIProfileByUnchecked(ctx context.Context, id string) (*AIProfile, error) {
	return nil, nil
}

// An alias calling its target through a rebuilt receiver (assignments'
// devadmin mutations): checked as the target.
func (e *mutationResolver) DevadminAddAuthEmail(ctx context.Context, kaid string, authEmail string) (*graphql.AddAuthEmail, error) {
	return (&mutationResolver{e.Resolver}).AddAuthEmail(ctx, kaid, authEmail)
}
