// Package llm provides a minimal OpenAI-compatible chat completions client
// and the repair loop shared by the map and reduce phases.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/StevenACoffman/toerr/errors/errcode"
)

// Message is one chat message.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Usage reports token consumption for one call.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

// Add accumulates another usage into u.
func (u *Usage) Add(other Usage) {
	u.PromptTokens += other.PromptTokens
	u.CompletionTokens += other.CompletionTokens
}

// Chatter is the minimal chat interface consumed by the pipeline; *Client
// implements it and tests substitute a local mock.
type Chatter interface {
	Chat(ctx context.Context, msgs []Message) (string, Usage, error)
}

// Client talks to any OpenAI-compatible chat completions endpoint.
type Client struct {
	baseURL    string
	apiKey     string
	model      string
	maxTokens  int
	httpClient *http.Client
	logger     *slog.Logger
	// gate pauses every call while the endpoint throttles.
	gate gate
	// backoffs are the waits before each retry of a transient failure.
	backoffs []time.Duration
	// jitter spreads a backoff, so workers that failed together don't
	// retry together.
	jitter func(time.Duration) time.Duration
	// pause is the wait after a throttled reply that names none, maxPause
	// caps one that does, and maxThrottleWaits is how many pauses one call
	// waits through before it fails.
	pause, maxPause  time.Duration
	maxThrottleWaits int
}

// Request settings and limits.
const (
	// requestTimeout bounds one completion: a long reduce answer takes
	// minutes on a slow endpoint.
	requestTimeout = 180 * time.Second
	// temperature keeps answers close to deterministic, so cached runs
	// rarely disagree with fresh ones.
	temperature = 0.1
	// maxResponseBytes caps a response read into memory.
	maxResponseBytes = 32 << 20
	// errorExcerptLen is how much of an error response an error quotes.
	errorExcerptLen = 200
	// throttlePause is the wait after a throttled reply that names none:
	// Gemini's per-minute quotas clear within one, and retrying sooner
	// keeps the throttle going.
	throttlePause = time.Minute
	// maxThrottlePause caps a wait an endpoint names.
	maxThrottlePause = 5 * time.Minute
	// maxThrottleWaits is how many pauses one call waits through: a quota
	// that stays exhausted (a daily one) still ends the call, and the run
	// resumes from the caches.
	maxThrottleWaits = 10
)

// NewClient builds a client for one endpoint/model pair; logger receives
// each throttle pause and each retry.
func NewClient(baseURL, apiKey, model string, maxTokens int, logger *slog.Logger) *Client {
	return &Client{
		baseURL:          strings.TrimRight(baseURL, "/"),
		apiKey:           apiKey,
		model:            model,
		maxTokens:        maxTokens,
		httpClient:       &http.Client{Timeout: requestTimeout},
		logger:           logger,
		backoffs:         []time.Duration{2 * time.Second, 8 * time.Second, 20 * time.Second},
		jitter:           equalJitter,
		pause:            throttlePause,
		maxPause:         maxThrottlePause,
		maxThrottleWaits: maxThrottleWaits,
	}
}

// equalJitter keeps half of d and draws the other half at random.
func equalJitter(d time.Duration) time.Duration {
	return d/2 + rand.N(d/2+1) //nolint:gosec // jitter spreads retries; it guards no secret
}

type chatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature float64   `json:"temperature"`
	MaxTokens   int       `json:"max_tokens"`
}

type chatResponse struct {
	Choices []struct {
		Message Message `json:"message"`
	} `json:"choices"`
	Usage Usage `json:"usage"`
}

// reply is one call's outcome: a success's content and usage, or the wait
// a failed reply names (zero when none).
type reply struct {
	content    string
	usage      Usage
	retryAfter time.Duration
}

// Chat posts the messages and returns the assistant reply content and token
// usage. Each failure carries an errcode status, which decides what
// follows:
//   - resource exhausted (429, or 5xx naming a wait): every call of the
//     client pauses for the wait the reply names, else throttlePause, then
//     retries, up to maxThrottleWaits pauses per call;
//   - unavailable or deadline exceeded (network errors, timeouts, other
//     5xx): this call alone retries after each backoff, jittered;
//   - anything else is returned as is.
func (c *Client) Chat(ctx context.Context, msgs []Message) (string, Usage, error) {
	body, err := json.Marshal(chatRequest{
		Model:       c.model,
		Messages:    msgs,
		Temperature: temperature,
		MaxTokens:   c.maxTokens,
	})
	if err != nil {
		return "", Usage{}, fmt.Errorf("encode chat request: %w", err)
	}

	var b budget
	for {
		if err = c.gate.wait(ctx); err != nil {
			return "", Usage{}, err
		}
		r, callErr := c.call(ctx, body)
		if callErr == nil {
			return r.content, r.usage, nil
		}
		if err = c.waitToRetry(ctx, &b, r.retryAfter, callErr); err != nil {
			return "", Usage{}, err
		}
	}
}

// budget counts one call's retries and throttle pauses.
type budget struct{ retries, pauses int }

// waitToRetry waits out a failed call when its status allows another try
// within b, and returns the error to give up with otherwise.
func (c *Client) waitToRetry(
	ctx context.Context,
	b *budget,
	retryAfter time.Duration,
	err error,
) error {
	code := errcode.Status(err)
	if code == errcode.StatusResourceExhausted {
		return c.waitOutThrottle(ctx, b, retryAfter, err)
	}
	if code == errcode.StatusUnavailable || code == errcode.StatusDeadlineExceeded {
		return c.backOff(ctx, b, err)
	}
	return err
}

// waitOutThrottle pauses every call of the client for the wait the
// throttled reply named, else c.pause; the next gate.wait holds this call.
func (c *Client) waitOutThrottle(
	ctx context.Context,
	b *budget,
	retryAfter time.Duration,
	err error,
) error {
	if b.pauses == c.maxThrottleWaits {
		return fmt.Errorf("still throttled after %d pauses: %w", b.pauses, err)
	}
	b.pauses++
	pause := c.pause
	if retryAfter > 0 {
		pause = min(retryAfter, c.maxPause)
	}
	if c.gate.pause(pause) {
		c.logger.WarnContext(ctx, "llm endpoint throttled, pausing every call",
			"pause", pause, "pauses", b.pauses, "err", err)
	}
	return nil
}

// backOff waits this call's next backoff, jittered.
func (c *Client) backOff(ctx context.Context, b *budget, err error) error {
	if b.retries == len(c.backoffs) {
		return err
	}
	wait := c.jitter(c.backoffs[b.retries])
	b.retries++
	c.logger.WarnContext(ctx, "llm call failed, retrying",
		"wait", wait, "retry", b.retries, "err", err)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(wait):
		return nil
	}
}

func (c *Client) call(ctx context.Context, body []byte) (reply, error) {
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.baseURL+"/chat/completions",
		bytes.NewReader(body),
	)
	if err != nil {
		return reply{}, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return reply{}, ctx.Err()
		}
		code := errcode.StatusUnavailable
		if netErr, ok := errors.AsType[net.Error](err); ok && netErr.Timeout() {
			code = errcode.StatusDeadlineExceeded
		}
		return reply{}, errcode.Link(code, "", fmt.Errorf("chat request: %w", err))
	}
	defer func() {
		// The body is read in full or abandoned with an error already
		// returned: a failed close has nothing left to report.
		_ = resp.Body.Close()
	}()

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return reply{}, errcode.Link(errcode.StatusUnavailable, "",
			fmt.Errorf("read chat response: %w", err))
	}
	if resp.StatusCode != http.StatusOK {
		code, retryAfter := classify(resp.StatusCode, resp.Header, data, time.Now())
		return reply{retryAfter: retryAfter}, errcode.Link(code, "", fmt.Errorf(
			"chat endpoint returned %s: %s",
			resp.Status,
			truncate(data, errorExcerptLen),
		))
	}

	var parsed chatResponse
	if err = json.Unmarshal(data, &parsed); err != nil {
		return reply{}, fmt.Errorf("decode chat response: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return reply{}, errors.New("chat response has no choices")
	}
	return reply{content: parsed.Choices[0].Message.Content, usage: parsed.Usage}, nil
}

func truncate(data []byte, n int) string {
	s := strings.TrimSpace(string(data))
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}

// StripFences trims whitespace and surrounding markdown code fences from an
// LLM reply, before JSON unmarshalling.
func StripFences(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[i+1:]
	} else {
		return ""
	}
	if j := strings.LastIndex(s, "```"); j >= 0 {
		s = s[:j]
	}
	return strings.TrimSpace(s)
}

// ChatJSON drives a chat conversation until validate accepts the assistant
// reply, allowing up to maxRepairs correction rounds. Each rejection is fed
// back to the model verbatim. onUsage is invoked after every call.
func ChatJSON[T any](
	ctx context.Context,
	chat Chatter,
	msgs []Message,
	maxRepairs int,
	onUsage func(Usage),
	validate func(reply string) (T, error),
) (T, error) {
	var zero T
	conversation := append([]Message(nil), msgs...)
	var lastErr error
	for round := 0; round <= maxRepairs; round++ {
		reply, usage, err := chat.Chat(ctx, conversation)
		if onUsage != nil {
			onUsage(usage)
		}
		if err != nil {
			return zero, err
		}
		result, validationErr := validate(reply)
		if validationErr == nil {
			return result, nil
		}
		lastErr = validationErr
		conversation = append(
			conversation,
			Message{Role: "assistant", Content: reply},
			Message{
				Role: "user",
				Content: fmt.Sprintf(
					"output rejected: %v; resend the full corrected JSON only",
					validationErr,
				),
			},
		)
	}
	return zero, fmt.Errorf("rejected after %d corrections: %w", maxRepairs, lastErr)
}
