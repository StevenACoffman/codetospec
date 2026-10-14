package gateway

// Mutations exempt from CSRF protection, each with its reason.
var _csrfExempt = map[string]bool{
	// AuthCookieMutation is used by the frontend and mobile.
	"AuthCookieMutation": true,
	// userDeletionMutation only runs as a service-admin query.
	"userDeletionMutation": true,
}

func _isApp(origin string) bool {
	return strings.HasPrefix(origin, "app:org.khanacademy.")
}

func _isExempted(operationName string) bool {
	return _csrfExempt[operationName]
}

func _validateCSRF(headerValue string) error {
	if headerValue == "" {
		return errors.Unauthorized("csrf failed: no x-ka-fkey header")
	}
	return nil
}

// ValidateCSRF is an allow-list ending in another checker's error.
func ValidateCSRF(operationName, originHeaderValue, xKAFKeyHeaderValue string) error {
	if _isApp(originHeaderValue) {
		return nil
	}
	validateErr := _validateCSRF(xKAFKeyHeaderValue)
	if validateErr == nil {
		return nil
	}
	if _isExempted(operationName) {
		return nil
	}
	return validateErr
}
