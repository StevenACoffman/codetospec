package resolvers

// Test files are never analyzed.
func (r *queryResolver) CheckUsername(ctx context.Context) error {
	acl.OpenAccess()
	return nil
}
