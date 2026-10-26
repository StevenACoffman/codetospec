package pubsub

// DecodeAndValidatePushMsg checks a push message's signature: an
// authenticator, though it fails with InvalidInput.
func DecodeAndValidatePushMsg(ctx context.Context, r *http.Request) ([]byte, error) {
	var msg PushMessage
	if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
		return nil, errors.InvalidInput("Could not decode body", err)
	}
	expected, err := computeSignature(ctx, msg.Data)
	if err != nil || msg.Signature != expected {
		return nil, errors.InvalidInput("Invalid pubsub message signature")
	}
	return msg.Data, nil
}

// Decode reads no secret: not an authenticator.
func Decode(r *http.Request) ([]byte, error) {
	return io.ReadAll(r.Body)
}
