package resolvers

func (r *userResolver) Kaid(ctx context.Context, user *User) (*string, error) {
	return formatKaid(user), nil
}

// formatKaid is a helper: it takes the file's single domain.
func formatKaid(user *User) *string {
	return user.Kaid
}

// A field served by another resolver type's method (districts'
// studentListResolver calling classroomResolver): checked as that method.
func (r *userResolver) AiSummary(ctx context.Context, user *User) (*string, error) {
	return (&aiProfileResolver{r.Resolver}).Summary(ctx, nil)
}
