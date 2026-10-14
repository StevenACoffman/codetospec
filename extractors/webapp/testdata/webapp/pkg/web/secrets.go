package web

var (
	GatewayToBackendSecret  = secrets.GSM("backend_to_gateway_secret", 1)
	ServiceAdminQuerySecret = secrets.GSM("service_admin_query_secret", 1)
)
