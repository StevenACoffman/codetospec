package web

const serviceAdminQueryHeaderName = "X-Ka-Service-Admin-Query"

func getIsServiceAdminQueryFromRequest(ctx context.Context, r *http.Request) (bool, error) {
	secretFromHeader := r.Header.Get(serviceAdminQueryHeaderName)
	if secretFromHeader == "" {
		return false, nil
	}
	err := secrets.Validate(ctx, ServiceAdminQuerySecret, secretFromHeader)
	return err == nil, err
}

// NewRequestData reaches the service-admin check.
func NewRequestData(ctx context.Context, r *http.Request) (bool, error) {
	return getIsServiceAdminQueryFromRequest(ctx, r)
}
