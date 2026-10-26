package gateway

var _reqHeaderForwardingMap = _mapHeaders(
	append(gqlclient.ForwardedUserRequestHeaderNames,
		"Referer",
		"X-Ka-Service-Admin-Query",
	))

// ForwardRequestHeaders copies the allowed headers to a backend request.
func (hf *_headerForwarding) ForwardRequestHeaders(req *http.Request) {
	for name := range _reqHeaderForwardingMap {
		for _, value := range hf.reqHeader.Values(name) {
			req.Header.Add(name, value)
		}
	}
}
