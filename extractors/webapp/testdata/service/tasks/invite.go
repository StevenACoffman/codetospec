package tasks

func accept(ctx context.Context, client graphql.Client) error {
	_, err := genqlient.Users_AcceptInvitation(ctx, ctx.GraphQL().AsServiceAdmin(), "token")
	if err != nil {
		return err
	}
	_, _ = genqlient.Users_Mixed(ctx, ctx.GraphQL().AsUser(), "kaid")
	_, _ = genqlient.Users_Own(ctx, client, "kaid") // own service: no dependency
	return nil
}
