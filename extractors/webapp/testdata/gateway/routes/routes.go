package routes

import (
	"net/http"

	"github.com/Khan/webapp/pkg/web/serve"
)

// AllRoutes registers the gateway's handlers the ways webapp writes them.
func (rts *Routes) AllRoutes() []serve.HandlerOption {
	return []serve.HandlerOption{
		serve.HandleFunc("/graphql/", rts.HandleGraphQL),
		serve.HandleFunc("/tasks/graphql/", rts.HandleGraphQL),
		serve.HandleFunc("/settings", rts.HandleSettings),
		serve.HandleFunc("/pubsub/notify", func(w http.ResponseWriter, r *http.Request) {
			handleNotify(w, r)
		}),
		serve.HandleFunc("/tasks/cleanup", serve.CreateTaskHandler(handleCleanup)),
		serve.HandleFunc("/pubsub/push", handlePush),
		serve.HandleFunc("/pubsub/raw", handleRaw),
		serve.HandleFunc("/pubsub/token", handleTokenPush),
		serve.HandleFunc("/tasks/archive", serve.CreateTaskHandler(handleArchive)),
		serve.HandleFunc("/logged", serve.WithLogging(handleRaw)),
		serve.HandleFunc("/dev", handleDev),
	}
}

// DevRoutes registers the playground in dev only, and a debug page
// elsewhere.
func DevRoutes(routes []serve.HandlerOption) []serve.HandlerOption {
	if os.Getenv("KA_IS_DEV_SERVER") == "1" {
		routes = append(routes, serve.HandleFunc("/", playground.Handler("GraphQL playground", "/graphql/playground")))
	} else {
		routes = append(routes, serve.HandleFunc("/debug", handleRaw))
	}
	return routes
}
