package models

var UserNonceKind = datastore.RegisterKind("UserNonce", new(UserNonce))

var OrphanKind = datastore.RegisterKind("Orphan", new(Missing))

// UserNonce is a typed, per-user nonce.
type UserNonce struct {
	datastore.BaseModel

	Value     string    `datastore:"value,noindex"`
	Kind      string    `datastore:",noindex"`
	Expires   time.Time `datastore:"expires"`
	Secret    string    `datastore:"-"`
	internal  int
	A, B      int64
}
