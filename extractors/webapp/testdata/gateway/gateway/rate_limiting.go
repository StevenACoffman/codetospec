package gateway

// RateLimitExceeded is an authorization error held in a variable.
var RateLimitExceeded = errors.Unauthorized("Rate limit exceeded")

type _rateLimit struct {
	maxIPCount   int
	ipWindow     time.Duration
	maxKaidCount int
	kaidWindow   time.Duration
}

var _safelistedKAIDsForOperation = map[string][]string{
	"testQuery": {
		"kaid_111",
		"kaid_222", // a tester
	},
}

var _rateLimits = map[string]_rateLimit{
	"testQuery": {
		maxIPCount: 10, ipWindow: time.Minute,
		maxKaidCount: 3, kaidWindow: time.Minute,
	},
	"createProgram": {
		maxIPCount: 300, ipWindow: time.Hour,
		maxKaidCount: 25, kaidWindow: 2 * time.Hour,
	},
	"ipOnly": {maxIPCount: 50, ipWindow: 10 * time.Minute},
}

// RateLimit reports whether a request is over its operation's limits.
func RateLimit(ctx context.Context, operationName string, kaid string) bool {
	rules := _rateLimits[operationName]

	if rules.maxIPCount > 0 {
		if ratelimit.ByIP(
			ctx, "graphql_gateway_"+operationName,
			ratelimit.MaxRequestsPerWindow(rules.maxIPCount),
			ratelimit.LimitWindow(rules.ipWindow),
		) {
			return true
		}
	}

	if kaid != "" && rules.maxKaidCount > 0 {
		if slices.Contains(_safelistedKAIDsForOperation[operationName], kaid) {
			return false
		}
		if ratelimit.ByAccount(
			ctx, "graphql_gateway_"+operationName, kaid,
			ratelimit.MaxRequestsPerWindow(rules.maxKaidCount),
			ratelimit.LimitWindow(rules.kaidWindow),
		) {
			return true
		}
	}
	return false
}
