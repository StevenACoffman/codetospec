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

// RequireEditor denies through its error.
//
//ka:permission-check
func RequireEditor(ctx context.Context) error {
	if !acl.IsLoggedIn(ctx) {
		return errors.Unauthorized("editors only")
	}
	return nil
}

// VisibleNotes filters: it denies with an empty list, and its error is a
// failure, not a denial.
//
//ka:permission-check
func VisibleNotes(ctx context.Context) ([]string, error) {
	return nil, nil
}

// LoginWithRateLimit logs the caller in: a check by webapp's linter, but its
// error is no denial and it filters nothing.
//
//ka:permission-check
func LoginWithRateLimit(ctx context.Context, token string) (string, error) {
	return token, nil
}
