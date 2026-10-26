package pubsub

func computeSignature(ctx context.Context, data []byte) (string, error) {
	secret, err := ctx.Secrets().Get(ctx, gcloud.PubsubSecretKey)
	if err != nil {
		return "", err
	}
	return sign(data, secret), nil
}
