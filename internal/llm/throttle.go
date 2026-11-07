package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/StevenACoffman/toerr/errors/errcode"
)

// headerRetryAfter is the HTTP header naming how long to wait.
const headerRetryAfter = "Retry-After"

// statusCode classifies an endpoint's HTTP status by what a retry can do:
// resource exhausted (throttled: wait, then retry), deadline exceeded or
// unavailable (transient: back off, then retry), unknown (not retried).
func statusCode(httpStatus int) errcode.StatusCode {
	switch {
	case httpStatus == http.StatusTooManyRequests:
		return errcode.StatusResourceExhausted
	case httpStatus == http.StatusRequestTimeout, httpStatus == http.StatusGatewayTimeout:
		return errcode.StatusDeadlineExceeded
	case httpStatus >= http.StatusInternalServerError:
		return errcode.StatusUnavailable
	default:
		return errcode.StatusUnknown
	}
}

// classify is what a failed reply asks of its caller: its status code and
// the wait it names (zero when none). An unavailable endpoint that names a
// wait is throttling too: it asked to be left alone for that long.
func classify(
	httpStatus int,
	header http.Header,
	body []byte,
	now time.Time,
) (errcode.StatusCode, time.Duration) {
	code := statusCode(httpStatus)
	delay, named := throttleDelay(header, body, now)
	if named && code == errcode.StatusUnavailable {
		code = errcode.StatusResourceExhausted
	}
	return code, delay
}

// throttleDelay is the wait an endpoint's reply names, from the Retry-After
// header (seconds or an HTTP date, read against now) or else a Gemini
// RetryInfo detail's retryDelay in the body; false when it names none.
func throttleDelay(header http.Header, body []byte, now time.Time) (time.Duration, bool) {
	if v := header.Get(headerRetryAfter); v != "" {
		if secs, err := strconv.Atoi(v); err == nil && secs >= 0 {
			return time.Duration(secs) * time.Second, true
		}
		if at, err := http.ParseTime(v); err == nil {
			return max(at.Sub(now), 0), true
		}
	}
	var doc any
	if json.Unmarshal(body, &doc) != nil {
		return 0, false
	}
	return retryDelay(doc)
}

// retryDelay is the longest "retryDelay" duration in a decoded JSON
// document (Gemini nests it in error.details, inside a list or not); the
// longest, so map order never changes the answer.
func retryDelay(doc any) (time.Duration, bool) {
	var longest time.Duration
	found := false
	visit := func(d time.Duration, ok bool) {
		if ok && (!found || d > longest) {
			longest, found = d, true
		}
	}
	switch v := doc.(type) {
	case map[string]any:
		if s, ok := v["retryDelay"].(string); ok {
			d, err := time.ParseDuration(s)
			visit(d, err == nil && d >= 0)
		}
		for _, child := range v {
			visit(retryDelay(child))
		}
	case []any:
		for _, child := range v {
			visit(retryDelay(child))
		}
	}
	return longest, found
}

// gate holds every call of one client while the endpoint throttles. The
// quota is the endpoint's, shared by every worker, so one throttled reply
// pauses them all and they resume together: a worker retrying on its own
// keeps the throttle going for the others. The zero value is open.
type gate struct {
	mu    sync.Mutex
	until time.Time // monotonic: read only through time.Until
}

// pause closes the gate for d from now, unless it already stays closed
// longer; it reports whether the end moved.
func (g *gate) pause(d time.Duration) bool {
	end := time.Now().Add(d)
	g.mu.Lock()
	defer g.mu.Unlock()
	if !end.After(g.until) {
		return false
	}
	g.until = end
	return true
}

// wait blocks until the gate is open, including any pause added while it
// waited, or ctx ends.
func (g *gate) wait(ctx context.Context) error {
	for {
		g.mu.Lock()
		d := time.Until(g.until)
		g.mu.Unlock()
		if d <= 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(d):
		}
	}
}
