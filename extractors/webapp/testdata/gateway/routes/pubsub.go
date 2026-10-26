package routes

import (
	"net/http"

	"github.com/Khan/webapp/pkg/gcloud/pubsub"
)

// handlePush authenticates the push by its signature; handleRaw does not.
func handlePush(w http.ResponseWriter, r *http.Request) {
	ctx := kacontext.Upgrade(r.Context())
	data, err := pubsub.DecodeAndValidatePushMsg(ctx, r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	_ = data
	w.WriteHeader(http.StatusOK)
}

func handleRaw(w http.ResponseWriter, r *http.Request) {
	data, err := pubsub.Decode(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	_ = data
}

var _pushToken = "not-a-real-token"

const jsonContentType = "application/json"

// handleTokenPush checks a shared token; the content type is not a credential.
func handleTokenPush(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("token") != _pushToken {
		http.Error(w, "Bad token", http.StatusBadRequest)
		return
	}
	if r.Header.Get("Content-Type") != jsonContentType {
		http.Error(w, "Bad content type", http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusOK)
}
