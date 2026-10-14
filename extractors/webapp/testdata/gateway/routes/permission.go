package routes

const _devadminQueryOperationName = "devadminQuery"

// _validatePermission is an ordered allow-list.
func _validatePermission(ctx context.Context, requestHeaders http.Header, operationName, queryName string, queryIsSafelisted, isDev bool) error {
	if isDev {
		return nil
	}
	if queryIsSafelisted && operationName == queryName {
		return nil
	}
	err := validateIsTrustedClient(ctx, requestHeaders, "X-Ka-Gateway-Secret", useGatewaySecret)
	if err == nil {
		return nil
	}
	if operationName == _devadminQueryOperationName &&
		acl.ActorHasPermission(ctx, capabilities.CanPerformDynamicGraphqlQueries, acl.AnyScope) {
		return nil
	}
	if queryIsSafelisted {
		oldMobileExceptions := map[string]string{
			"ContentProgressV1": "ContentProgressV1Query",
			"DomainsQueryV4":    "",
		}
		allowedOpnameInURL, ok := oldMobileExceptions[queryName]
		if ok && operationName == allowedOpnameInURL {
			return nil
		}
		return errors.Unauthorized("Wrong url-opname for query")
	}
	return errors.Unauthorized("Operation not found in safelist")
}

// _formatName returns only errors that are not denials: no policy.
func _formatName(name string) error {
	if name != "" {
		return nil
	}
	return errors.InvalidInput("no name")
}
