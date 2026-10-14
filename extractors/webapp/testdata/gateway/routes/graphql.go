package routes

// HandleGraphQL serves /graphql/ and /tasks/graphql/.
func (rts *Routes) HandleGraphQL(w http.ResponseWriter, r *http.Request) {
	ctx := kacontext.Upgrade(r.Context())
	isDev := os.Getenv("KA_IS_DEV_SERVER") != "0"
	isMutation := r.Method == http.MethodPost

	if gateway.RateLimit(ctx, operationName(r), r.Header.Get("X-Ka-Kaid")) {
		writeError(ctx, w, errors.Wrap(gateway.RateLimitExceeded))
		return
	}

	if isMutation && !isDev {
		err := validateIsTrustedClient(ctx, r.Header, "X-Ka-Task-Secret", useTaskSecret)
		err2 := validateIsTrustedClient(ctx, r.Header, "X-Ka-Service-Admin-Query", useServiceAdminSecret)
		if err != nil && err2 != nil {
			err = gateway.ValidateCSRF(r.Header.Get("Origin"), r.Header.Get("X-Ka-Fkey"))
			if err != nil {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
		}
	}
	gatewaySecret, err := ctx.Secrets().Get(ctx, graphqlGateway.GatewayToBackendSecret)
	if err != nil {
		return
	}
	secretsHook := func(backendRequest *http.Request) error {
		backendRequest.Header.Add("X-Ka-Gateway-Secret", gatewaySecret)
		return nil
	}
	_ = secretsHook
	w.WriteHeader(http.StatusOK)
}
