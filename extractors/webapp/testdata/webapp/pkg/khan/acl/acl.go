package acl

// ManyCoachedByActor reports, per coachee, whether the actor coaches them:
// a filter (a collection, no error that denies).
//
//ka:permission-check
func ManyCoachedByActor(ctx context.Context, coacheeKaids []string) map[string]bool {
	return nil
}

// TODO is a "permission check" that is a no-op: it marks that this resolver
// does not have a permission check, but should.
//
//ka:permission-check // resolvers can use this as their permission check
func TODO() {}
