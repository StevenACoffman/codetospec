package permissions

// An access predicate: guards testing it are rules.
func UserCanViewEmails(ctx context.Context, kaid string) bool {
	return acl.IsCurrentUser(ctx, kaid)
}

// A loader, not a predicate: no guard tests it.
func LoadVisibleEmails(ctx context.Context, kaid string) ([]string, error) {
	return nil, nil
}

// Unexported: resolvers cannot call it.
func canSee(ctx context.Context) bool {
	return true
}
