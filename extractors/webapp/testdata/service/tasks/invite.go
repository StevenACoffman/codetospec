package tasks

func accept(ctx context.Context, client graphql.Client) error {
	_, err := genqlient.Users_AcceptInvitation(ctx, client, "token")
	if err != nil {
		return err
	}
	_, _ = genqlient.Users_Mixed(ctx, client, "kaid")
	_, _ = genqlient.Users_Own(ctx, client, "kaid") // own service: no dependency
	return nil
}
