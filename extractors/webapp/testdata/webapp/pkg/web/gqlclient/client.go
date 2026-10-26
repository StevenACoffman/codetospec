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

// Sender sends with the gateway secret; its method is reached through it.
type Sender struct{ headers http.Header }

// NewSender builds a Sender.
func NewSender() *Sender { return &Sender{headers: http.Header{}} }

func (s *Sender) Send(ctx context.Context) error {
	if s.headers == nil {
		return errors.InvalidInput("no headers")
	}
	return nil
}

// unreached is called by nothing the service reaches.
func unreached() int { return 1 }
