package routes

// HandleSettings needs the settings secret.
func (rts *Routes) HandleSettings(w http.ResponseWriter, r *http.Request) {
	ctx := kacontext.Upgrade(r.Context())
	err := validateIsTrustedClient(ctx, r.Header, "X-Ka-Secret-Key", useSettingsSecret)
	if err != nil {
		writeError(ctx, w, errors.Unauthorized("invalid secret-key"))
		return
	}
	w.WriteHeader(http.StatusOK)
}

// handleNotify logs a denial-named error but answers 200: no rule.
func handleNotify(w http.ResponseWriter, r *http.Request) {
	ctx := kacontext.Upgrade(r.Context())
	if expected := isExpected(ctx); expected {
		ctx.Log().Warn(errors.NotAllowed("expected failure"))
		w.WriteHeader(http.StatusOK)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// handleCleanup is a task handler: only tasks may run it.
func handleCleanup(ctx context.Context) error {
	if !ctx.RequestIsTask() {
		return errors.Unauthorized("not a task")
	}
	return nil
}

// handleArchive checks nothing itself: its wrapper does.
func handleArchive(r *http.Request) error {
	return archive(r.Context())
}

// _ensureLoggedIn denies on a check: callers' errors deny.
func _ensureLoggedIn(ctx context.Context) (string, error) {
	if !acl.IsLoggedIn(ctx) {
		return "", errors.Unauthorized()
	}
	return "ok", nil
}

// _requireName builds an authorization error on no check: not a denier.
func _requireName(name string) error {
	if name == "" {
		return errors.Unauthorized("no name")
	}
	return nil
}

func handleDev(w http.ResponseWriter, r *http.Request) {
	ctx := kacontext.Upgrade(r.Context())
	if _, err := _ensureLoggedIn(ctx); err != nil {
		writeError(ctx, w, err)
		return
	}
	if err := _requireName(r.FormValue("name")); err != nil {
		writeError(ctx, w, err)
		return
	}
	w.WriteHeader(http.StatusOK)
}
