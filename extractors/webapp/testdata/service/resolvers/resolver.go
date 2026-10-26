package resolvers

type Resolver struct{}

// The factories gqlgen requires: the method name is the schema type.
func (r *Resolver) AIProfile() generated.AIProfileResolver { return &aiProfileResolver{r} }

func (r *Resolver) Entity() generated.EntityResolver { return &entityResolver{r} }

func (r *Resolver) Mutation() generated.MutationResolver { return &mutationResolver{r} }

// An alias factory (progress has them): its method name is no schema type,
// its result type is.
func (r *Resolver) InvalidateCache() generated.MutationResolver { return &mutationResolver{r} }

// No factory: it takes parameters and returns no resolver (content-editing's
// GetServiceSpecificKAContext); its struct's methods are not resolvers.
func (r *Resolver) ServiceContext(ctx context.Context) *serviceContext {
	return &serviceContext{ctx: ctx}
}

func (k *serviceContext) Repository() repository {
	if !acl.IsLoggedIn(k.ctx) {
		return nil
	}
	return k.repository
}
