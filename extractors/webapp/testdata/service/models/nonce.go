package models

var UserNonceKind = datastore.RegisterKind("UserNonce", new(UserNonce))

var OrphanKind = datastore.RegisterKind("Orphan", new(Missing))

// UserNonce is a typed, per-user nonce.
type UserNonce struct {
	datastore.BaseModel

	Value     string    `datastore:"value,noindex"`
	Kind      string    `datastore:",noindex"` // what the nonce is for
	// Expires is when the nonce
	// stops working.
	Expires   time.Time `datastore:"expires"`
	Secret    string    `datastore:"-"`
	internal  int
	A, B      int64
}

var SessionKind = datastore.RegisterKind("LoginSession", new(Session))

// Session is stored under another kind name.
type Session struct {
	Started time.Time `datastore:"started"`
}
