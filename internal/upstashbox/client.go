// Package upstashbox is an HTTP client for the Upstash Box REST API.
//
// Every call takes the caller's context. A context without a deadline gets a
// default per-call timeout. Errors and String methods never carry the API key
// or a token-bearing URL; see errors.go and cdp.go. The package does not log,
// print or start goroutines: it returns errors to the caller.
package upstashbox

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// defaultCallTimeout bounds a call whose context has no deadline.
	defaultCallTimeout = 30 * time.Second

	// connectCallTimeout bounds the CDP connect call, since Chromium starts on first use.
	// verified against @upstash/box 0.8.1 source on 2026-11-21
	connectCallTimeout = 60 * time.Second

	// createWaitBudget bounds how long Create polls a box that is still "creating".
	// verified against @upstash/box 0.8.1 source on 2026-11-21
	createWaitBudget = 300 * time.Second

	// pollInitialDelay and pollMaxDelay bound the increasing wait between polls.
	pollInitialDelay = 500 * time.Millisecond
	pollMaxDelay     = 5 * time.Second
)

// Client talks to the Upstash Box API. It is safe for concurrent use.
type Client struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
	timeout    time.Duration

	// Create polling. Fields rather than constants so tests can shorten them.
	pollInitial time.Duration
	pollMax     time.Duration
	createWait  time.Duration
}

// Option configures a Client in New.
type Option func(*Client) error

// WithBaseURL overrides the API origin. The URL must be absolute http or https
// with a host, and must not carry userinfo, a query or a fragment. One trailing
// slash is removed.
func WithBaseURL(raw string) Option {
	return func(c *Client) error {
		u, err := url.Parse(raw)
		// The parse error echoes raw, so it is deliberately not returned.
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return errors.New("upstashbox: base URL must be an absolute http or https URL")
		}
		if u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return errors.New("upstashbox: base URL must not carry userinfo, a query or a fragment")
		}
		c.baseURL = strings.TrimSuffix(u.String(), "/")
		return nil
	}
}

// WithHTTPClient replaces the transport used for requests. Tests inject a stub
// here. The client sets no Timeout of its own: deadlines come from the context.
// The client works on a shallow copy of hc with redirects refused, so the
// caller's *http.Client is not changed.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) error {
		if hc == nil {
			return errors.New("upstashbox: HTTP client must not be nil")
		}
		cp := *hc
		cp.CheckRedirect = refuseRedirect
		c.httpClient = &cp
		return nil
	}
}

// refuseRedirect stops every redirect. Go's client would copy the
// X-Box-Api-Key header to a redirect target on another host, so a 3xx response
// is returned as an *APIError instead.
func refuseRedirect(*http.Request, []*http.Request) error {
	return http.ErrUseLastResponse
}

// New returns a client for the given API key.
func New(apiKey string, opts ...Option) (*Client, error) {
	key := strings.TrimSpace(apiKey)
	if key == "" {
		return nil, errors.New("upstashbox: API key is required")
	}
	c := &Client{
		apiKey:      key,
		baseURL:     defaultBaseURL,
		httpClient:  &http.Client{CheckRedirect: refuseRedirect},
		timeout:     defaultCallTimeout,
		pollInitial: pollInitialDelay,
		pollMax:     pollMaxDelay,
		createWait:  createWaitBudget,
	}
	for _, opt := range opts {
		if err := opt(c); err != nil {
			return nil, err
		}
	}
	return c, nil
}

// callContext returns ctx unchanged when it has a deadline, otherwise a child
// context bounded by timeout.
func callContext(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, timeout)
}

// do performs one API call with the client's default timeout.
func (c *Client) do(ctx context.Context, method, path string, query url.Values, body, out any) error {
	return c.doWithin(ctx, c.timeout, method, path, query, body, out)
}

// doWithin performs one API call. A non-nil body is sent as JSON. A non-nil
// out receives the decoded JSON response, and a 2xx response with an empty
// body is then an error. With out nil, any 2xx response is success.
func (c *Client) doWithin(ctx context.Context, timeout time.Duration, method, path string, query url.Values, body, out any) error {
	ctx, cancel := callContext(ctx, timeout)
	defer cancel()

	target := c.baseURL + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}

	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("upstashbox: encode %s %s request: %s", method, path, c.redact(err.Error()))
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		// NewRequest's error embeds the full target URL, so only the path is reported.
		return fmt.Errorf("upstashbox: build %s %s request: invalid request", method, path)
	}
	req.Header.Set(headerAPIKey, c.apiKey)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return c.transportError(method, path, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return c.transportError(method, path, err)
	}
	if len(data) > maxResponseBytes {
		return fmt.Errorf("upstashbox: %s %s: response exceeds %d bytes", method, path, maxResponseBytes)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return c.apiError(method, path, resp.StatusCode, data)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("upstashbox: %s %s: unexpected response body: %s", method, path, c.redact(err.Error()))
	}
	return nil
}

// netError is a failed round trip that produced no HTTP response. The create
// poll retries it.
type netError struct {
	msg string
}

func (e *netError) Error() string { return e.msg }

// transportError converts a failed round trip into an error that carries the
// method and path only. The *url.Error wrapper is dropped because its message
// holds the full URL, which can include a token. Context errors are wrapped
// as the sentinel itself so errors.Is keeps working.
func (c *Client) transportError(method, path string, err error) error {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return fmt.Errorf("upstashbox: %s %s: %w", method, path, context.DeadlineExceeded)
	case errors.Is(err, context.Canceled):
		return fmt.Errorf("upstashbox: %s %s: %w", method, path, context.Canceled)
	}
	cause := err
	var ue *url.Error
	if errors.As(err, &ue) && ue.Err != nil {
		cause = ue.Err
	}
	return &netError{msg: fmt.Sprintf("upstashbox: %s %s: request failed: %s", method, path, c.redact(cause.Error()))}
}
