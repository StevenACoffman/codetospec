package gqlclient

import "github.com/Khan/webapp/pkg/web"

// NewClient reads the secrets newClient sends.
func NewClient(ctx context.Context, secretsClient secrets.Client) http.Header {
	gatewaySecret, err := secretsClient.Get(ctx, web.GatewayToBackendSecret)
	if err != nil {
		return nil
	}
	return newClient(gatewaySecret)
}

func newClient(gatewaySecret string) http.Header {
	userHeaders := http.Header{}
	userHeaders.Set("X-Ka-Gateway-Secret", gatewaySecret)
	return userHeaders
}
