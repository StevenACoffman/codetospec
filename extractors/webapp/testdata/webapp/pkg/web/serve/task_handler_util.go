package serve

// CreateTaskHandler wraps a task body in a check that the request is a task.
func CreateTaskHandler(taskBody func(r *http.Request) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := kacontext.Upgrade(r.Context())
		if !ctx.RequestIsTask() {
			http.Error(w, "not a task", http.StatusUnauthorized)
			return
		}
		_ = taskBody(r)
	}
}

// WithLogging wraps a handler in no check.
func WithLogging(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h(w, r)
	}
}
