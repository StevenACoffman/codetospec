package serve

import "github.com/Khan/webapp/pkg/web"

// HandleGraphQL serves the backend GraphQL API behind the gateway secret.
func HandleGraphQL(schema graphql.ExecutableSchema) HandlerOption {
	return Handle("/backend-graphql/", ValidateGraphqlSecretMiddleware(newServer(schema)))
}

// ValidateGraphqlSecretMiddleware rejects requests without the gateway secret.
func ValidateGraphqlSecretMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := kacontext.Upgrade(r.Context())
		actual := r.Header.Get("X-Ka-Gateway-Secret")
		if actual == "" {
			http.Error(w, "Missing graphql-validation header", http.StatusUnauthorized)
			return
		}
		expected, err := ctx.Secrets().Get(ctx, web.GatewayToBackendSecret)
		if err != nil || actual != expected {
			http.Error(w, "Invalid value for X-Ka-Gateway-Secret", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Main serves the options, deriving each request's data.
func Main(ctx context.Context, options ...HandlerOption) {
	_, _ = web.NewRequestData(ctx, nil)
}
