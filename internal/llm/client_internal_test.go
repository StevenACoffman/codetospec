package llm

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/StevenACoffman/toerr/errors/errcode"
)

const okReply = `{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`

// testClient is a client of url whose waits are milliseconds and whose
// jitter is none.
func testClient(t *testing.T, url string, logger *slog.Logger) *Client {
	t.Helper()
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(t.Output(), nil))
	}
	c := NewClient(url, "", "model", 16, logger)
	c.backoffs = []time.Duration{time.Millisecond, time.Millisecond}
	c.jitter = func(d time.Duration) time.Duration { return d }
	c.pause = 30 * time.Millisecond
	c.maxPause = time.Hour
	return c
}

// signalHandler closes seen once a record with message msg is handled.
type signalHandler struct {
	slog.Handler
	msg  string
	once sync.Once
	seen chan struct{}
}

//nolint:gocritic // hugeParam: slog.Handler's signature takes the record by value
func (h *signalHandler) Handle(ctx context.Context, r slog.Record) error {
	if r.Message == h.msg {
		h.once.Do(func() { close(h.seen) })
	}
	return h.Handler.Handle(ctx, r)
}

func TestChatPausesEveryCallWhenThrottled(t *testing.T) {
	var (
		mu        sync.Mutex
		throttled time.Time
		arrivals  []time.Time
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if throttled.IsZero() {
			throttled = time.Now()
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		arrivals = append(arrivals, time.Now())
		_, _ = w.Write([]byte(okReply))
	}))
	defer srv.Close()
	h := &signalHandler{
		Handler: slog.NewTextHandler(t.Output(), nil),
		msg:     "llm endpoint throttled, pausing every call",
		seen:    make(chan struct{}),
	}
	c := testClient(t, srv.URL, slog.New(h))

	first := make(chan error, 1)
	go func() {
		_, _, err := c.Chat(t.Context(), nil)
		first <- err
	}()
	<-h.seen // the gate is closed: a call made now must wait it out
	if _, _, err := c.Chat(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(arrivals) != 2 {
		t.Fatalf("%d calls reached the endpoint after the throttle, want 2", len(arrivals))
	}
	for _, at := range arrivals {
		if waited := at.Sub(throttled); waited < c.pause {
			t.Errorf(
				"a call reached the endpoint %v after the throttle, want at least %v",
				waited,
				c.pause,
			)
		}
	}
}

func TestChatWaitsTheDelayTheEndpointNames(t *testing.T) {
	cases := map[string]struct {
		header   http.Header
		body     string
		maxPause time.Duration
	}{
		"gemini retry info": {
			body:     `{"error":{"details":[{"retryDelay":"0.02s"}]}}`,
			maxPause: time.Hour,
		},
		"capped retry-after": {
			header:   http.Header{headerRetryAfter: {"3600"}},
			maxPause: 20 * time.Millisecond,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var calls atomic.Int32
			srv := httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					if calls.Add(1) == 1 {
						for k, v := range tc.header {
							w.Header()[k] = v
						}
						w.WriteHeader(http.StatusTooManyRequests)
						_, _ = w.Write([]byte(tc.body))
						return
					}
					_, _ = w.Write([]byte(okReply))
				}),
			)
			defer srv.Close()
			c := testClient(t, srv.URL, nil)
			c.pause = time.Hour // the default would outlast the test
			c.maxPause = tc.maxPause
			reply, _, err := c.Chat(t.Context(), nil)
			if err != nil || reply != "ok" {
				t.Fatalf("Chat = %q, %v; want ok", reply, err)
			}
		})
	}
}

func TestChatFailures(t *testing.T) {
	cases := map[string]struct {
		status       int
		wantRequests int32
		wantCode     errcode.StatusCode
		wantText     string
	}{
		"throttled past the budget": {
			http.StatusTooManyRequests,
			3,
			errcode.StatusResourceExhausted,
			"still throttled after 2 pauses",
		},
		"unavailable past the retries": {
			http.StatusInternalServerError, 3, errcode.StatusUnavailable, "500",
		},
		"bad request, not retried": {
			http.StatusBadRequest, 1, errcode.StatusUnknown, "400",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var calls atomic.Int32
			srv := httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					calls.Add(1)
					w.WriteHeader(tc.status)
				}),
			)
			defer srv.Close()
			c := testClient(t, srv.URL, nil)
			c.pause = time.Millisecond
			c.maxThrottleWaits = 2
			_, _, err := c.Chat(t.Context(), nil)
			if err == nil {
				t.Fatal("Chat succeeded")
			}
			if got := errcode.Status(err); got != tc.wantCode {
				t.Errorf("status = %s, want %s", got, tc.wantCode)
			}
			if !strings.Contains(err.Error(), tc.wantText) {
				t.Errorf("error %q lacks %q", err, tc.wantText)
			}
			if got := calls.Load(); got != tc.wantRequests {
				t.Errorf("%d requests, want %d", got, tc.wantRequests)
			}
		})
	}
}

func TestChatTimeoutIsDeadlineExceeded(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		<-release // answer only once the client gave up
	}))
	defer srv.Close()
	defer close(release)
	c := testClient(t, srv.URL, nil)
	c.httpClient.Timeout = 10 * time.Millisecond
	c.backoffs = nil
	_, _, err := c.Chat(t.Context(), nil)
	if got := errcode.Status(err); got != errcode.StatusDeadlineExceeded {
		t.Errorf("status = %s (%v), want %s", got, err, errcode.StatusDeadlineExceeded)
	}
}
