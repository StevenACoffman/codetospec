package llm

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/StevenACoffman/toerr/errors/errcode"
)

func TestStatusCode(t *testing.T) {
	cases := map[int]errcode.StatusCode{
		http.StatusTooManyRequests:     errcode.StatusResourceExhausted,
		http.StatusRequestTimeout:      errcode.StatusDeadlineExceeded,
		http.StatusGatewayTimeout:      errcode.StatusDeadlineExceeded,
		http.StatusInternalServerError: errcode.StatusUnavailable,
		http.StatusServiceUnavailable:  errcode.StatusUnavailable,
		http.StatusBadRequest:          errcode.StatusUnknown,
		http.StatusUnauthorized:        errcode.StatusUnknown,
	}
	for status, want := range cases {
		if got := statusCode(status); got != want {
			t.Errorf("statusCode(%d) = %s, want %s", status, got, want)
		}
	}
}

func TestClassify(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	named := http.Header{headerRetryAfter: {"30"}}
	cases := map[string]struct {
		status    int
		header    http.Header
		wantCode  errcode.StatusCode
		wantDelay time.Duration
	}{
		"429 naming no wait": {http.StatusTooManyRequests, nil, errcode.StatusResourceExhausted, 0},
		"429 naming a wait": {
			http.StatusTooManyRequests,
			named,
			errcode.StatusResourceExhausted,
			30 * time.Second,
		},
		"503 naming a wait": {
			http.StatusServiceUnavailable,
			named,
			errcode.StatusResourceExhausted,
			30 * time.Second,
		},
		"503 naming no wait": {http.StatusServiceUnavailable, nil, errcode.StatusUnavailable, 0},
		"400 naming a wait": {
			http.StatusBadRequest,
			named,
			errcode.StatusUnknown,
			30 * time.Second,
		},
		"504 naming no wait": {http.StatusGatewayTimeout, nil, errcode.StatusDeadlineExceeded, 0},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			code, delay := classify(c.status, c.header, nil, now)
			if code != c.wantCode || delay != c.wantDelay {
				t.Errorf("classify = %s, %v; want %s, %v", code, delay, c.wantCode, c.wantDelay)
			}
		})
	}
}

func TestThrottleDelay(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	gemini := `[{"error":{"code":429,"status":"RESOURCE_EXHAUSTED","details":[` +
		`{"@type":"type.googleapis.com/google.rpc.QuotaFailure"},` +
		`{"@type":"type.googleapis.com/google.rpc.RetryInfo","retryDelay":"37s"}]}}]`
	cases := map[string]struct {
		header http.Header
		body   string
		want   time.Duration
		named  bool
	}{
		"retry-after seconds": {
			header: http.Header{headerRetryAfter: {"12"}},
			want:   12 * time.Second, named: true,
		},
		"retry-after date": {
			header: http.Header{
				headerRetryAfter: {now.Add(90 * time.Second).Format(http.TimeFormat)},
			},
			want:  90 * time.Second,
			named: true,
		},
		"retry-after date already past": {
			header: http.Header{headerRetryAfter: {now.Add(-time.Minute).Format(http.TimeFormat)}},
			want:   0, named: true,
		},
		"header wins over body": {
			header: http.Header{headerRetryAfter: {"5"}}, body: gemini,
			want: 5 * time.Second, named: true,
		},
		"gemini retry info in a list": {body: gemini, want: 37 * time.Second, named: true},
		"gemini retry info, fractional": {
			body:  `{"error":{"details":[{"retryDelay":"1.5s"}]}}`,
			want:  1500 * time.Millisecond,
			named: true,
		},
		"longest of two delays": {
			body:  `{"a":{"retryDelay":"3s"},"b":{"retryDelay":"9s"}}`,
			want:  9 * time.Second,
			named: true,
		},
		"unreadable header, no body": {header: http.Header{headerRetryAfter: {"soon"}}},
		"body names no delay":        {body: `{"error":{"message":"quota"}}`},
		"bad delay":                  {body: `{"retryDelay":"later"}`},
		"not json":                   {body: "<html>busy</html>"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got, named := throttleDelay(c.header, []byte(c.body), now)
			if got != c.want || named != c.named {
				t.Errorf("throttleDelay = %v, %v; want %v, %v", got, named, c.want, c.named)
			}
		})
	}
}

func TestGatePauseKeepsTheLongerPause(t *testing.T) {
	var g gate
	if !g.pause(time.Hour) {
		t.Fatal("a first pause did not move the end")
	}
	if g.pause(time.Millisecond) {
		t.Error("a shorter pause moved the end")
	}
}

func TestGateWaitEndsWithContext(t *testing.T) {
	var g gate
	g.pause(time.Hour)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := g.wait(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("wait = %v, want context.Canceled", err)
	}
}

func TestGateWaitBlocksForThePause(t *testing.T) {
	var g gate
	const pause = 20 * time.Millisecond
	start := time.Now()
	g.pause(pause)
	if err := g.wait(t.Context()); err != nil {
		t.Fatal(err)
	}
	if waited := time.Since(start); waited < pause {
		t.Errorf("waited %v, want at least %v", waited, pause)
	}
}
