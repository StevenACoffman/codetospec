package resolvers

func (r *userResolver) Kaid(ctx context.Context, user *User) (*string, error) {
	return formatKaid(user), nil
}

// formatKaid is a helper: it takes the file's single domain.
func formatKaid(user *User) *string {
	return user.Kaid
}
