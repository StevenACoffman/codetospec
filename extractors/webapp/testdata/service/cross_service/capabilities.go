package cross_service

// ActorCanEditAnyContent's decision is admin's hasPermission for one named
// capability on the request's locale.
func ActorCanEditAnyContent(ctx gqlclient.KAContext) (bool, error) {
	resp, err := genqlient.Users_ActorCanEditAnyContent(ctx, ctx.GraphQL().AsUser())
	if err != nil {
		return false, err
	}
	return resp.User.CanEditContent, nil
}

// UserKAIDIfActorHasCapability returns nil when the caller lacks the
// capability its argument names, on the request's locale.
func UserKAIDIfActorHasCapability(ctx gqlclient.KAContext, capabilityName string) (*string, error) {
	resp, err := genqlient.Users_UserKAIDIfActorHasCapability(ctx, ctx.GraphQL().AsUser(), capabilityName)
	if err != nil {
		return nil, err
	}
	if !resp.User.HasPermission {
		return nil, nil
	}
	return &resp.User.Kaid, nil
}

// Unscoped returns two capabilities combined: not one decision.
func Unscoped(ctx gqlclient.KAContext, name string) (bool, error) {
	resp, err := genqlient.Users_Unscoped(ctx, ctx.GraphQL().AsUser(), name)
	if err != nil {
		return false, err
	}
	return resp.User.A && resp.User.B, nil
}

// ActorCanMapStandards passes a fixed capability as the query's variable.
func ActorCanMapStandards(ctx gqlclient.KAContext) (bool, error) {
	resp, err := genqlient.Users_ActorCanMapStandards(ctx, ctx.GraphQL().AsUser(), string(capabilities.CanMapStandards))
	if err != nil {
		return false, err
	}
	return resp.User.CanMapStandards, nil
}
