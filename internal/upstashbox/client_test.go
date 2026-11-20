package upstashbox

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// testKey and testToken are distinctive strings so a leak is easy to spot in
// any error message.
const (
	testKey   = "key-TESTSECRET-abc123"
	testToken = "tok-TESTSECRET-xyz789"
)

// roundTripFunc stands in for the network in a test.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// newStubClient returns a client whose transport is rt.
func newStubClient(t *testing.T, rt roundTripFunc) *Client {
	t.Helper()
	c, err := New(testKey, WithHTTPClient(&http.Client{Transport: rt}))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

// newServerClient returns a client pointed at an httptest server running h.
func newServerClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c, err := New(testKey, WithBaseURL(srv.URL))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

// jsonResponse builds a canned response for a stub transport.
func jsonResponse(req *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
}

// expectRequest reports a mismatch between the request and what the test expects.
// The API key travels only in X-Box-Api-Key; no Authorization header is sent.
func expectRequest(t *testing.T, r *http.Request, method, path string) {
	t.Helper()
	if r.Method != method {
		t.Errorf("method = %s, want %s", r.Method, method)
	}
	if r.URL.Path != path {
		t.Errorf("path = %s, want %s", r.URL.Path, path)
	}
	if got := r.Header.Get(headerAPIKey); got != testKey {
		t.Errorf("%s = %q, want the API key", headerAPIKey, got)
	}
	if got := r.Header.Get("Authorization"); got != "" {
		t.Errorf("Authorization = %q, want none", got)
	}
}

func TestDefaultBaseURLAndTrailingSlash(t *testing.T) {
	c, err := New(testKey)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c.baseURL != "https://us-east-1.box.upstash.com" {
		t.Errorf("default baseURL = %q", c.baseURL)
	}
	c, err = New(testKey, WithBaseURL("https://box.example.test//"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c.baseURL != "https://box.example.test/" {
		t.Errorf("baseURL after WithBaseURL with two slashes = %q, want exactly one trimmed", c.baseURL)
	}
	c, err = New(testKey, WithBaseURL("https://box.example.test/"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c.baseURL != "https://box.example.test" {
		t.Errorf("baseURL = %q, want the trailing slash trimmed", c.baseURL)
	}
}

// failIfCalled fails the test if the client sends any request.
func failIfCalled(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func TestNewRequiresAPIKey(t *testing.T) {
	if _, err := New("   "); err == nil {
		t.Fatal("New with a blank key: want error")
	}
}

func TestWithBaseURLRejectsUnsafeURLs(t *testing.T) {
	for _, raw := range []string{
		"",
		"ftp://example.test",
		"not a url",
		"https://user:abc@example.test",
		"https://example.test/?token=abc",
		"https://example.test/#abc",
	} {
		_, err := New(testKey, WithBaseURL(raw))
		if err == nil {
			t.Errorf("WithBaseURL(%q): want error", raw)
			continue
		}
		if strings.Contains(err.Error(), "abc") {
			t.Errorf("WithBaseURL(%q) error echoes input: %v", raw, err)
		}
	}
}

func TestDefaultTimeoutAppliedWithoutDeadline(t *testing.T) {
	var deadline time.Time
	var hasDeadline bool
	c := newStubClient(t, func(r *http.Request) (*http.Response, error) {
		deadline, hasDeadline = r.Context().Deadline()
		return jsonResponse(r, http.StatusOK, `[]`), nil
	})
	if _, err := c.List(context.Background(), ""); err != nil {
		t.Fatalf("List: %v", err)
	}
	if !hasDeadline {
		t.Fatal("request has no deadline; want the default per-call timeout")
	}
	if left := time.Until(deadline); left <= 29*time.Second || left > defaultCallTimeout {
		t.Errorf("deadline is %v away, want about %v", left, defaultCallTimeout)
	}
}

func TestCallerDeadlineIsKept(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	want, _ := ctx.Deadline()
	var got time.Time
	c := newStubClient(t, func(r *http.Request) (*http.Response, error) {
		got, _ = r.Context().Deadline()
		return jsonResponse(r, http.StatusOK, `[]`), nil
	})
	if _, err := c.List(ctx, ""); err != nil {
		t.Fatalf("List: %v", err)
	}
	if !got.Equal(want) {
		t.Errorf("deadline = %v, want the caller's %v", got, want)
	}
}

func TestContextCancellationStopsCall(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	c := newStubClient(t, func(r *http.Request) (*http.Response, error) {
		cancel()
		<-r.Context().Done()
		return nil, r.Context().Err()
	})
	_, err := c.Get(ctx, "box-1")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Get after cancel: err = %v, want context.Canceled", err)
	}
}

func TestInternalTimeoutBoundsCall(t *testing.T) {
	c := newStubClient(t, func(r *http.Request) (*http.Response, error) {
		<-r.Context().Done()
		return nil, r.Context().Err()
	})
	c.timeout = 20 * time.Millisecond
	_, err := c.Get(context.Background(), "box-1")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Get with a short default timeout: err = %v, want context.DeadlineExceeded", err)
	}
}

// TestTransportFailureRedactsSecrets forces a transport failure whose
// *url.Error carries the token-bearing URL, and whose message holds the key
// both as the API key value and as the X-Box-Api-Key header value the request
// sent. It checks that none of those reach err.Error().
func TestTransportFailureRedactsSecrets(t *testing.T) {
	c := newStubClient(t, func(r *http.Request) (*http.Response, error) {
		headerValue := r.Header.Get(headerAPIKey)
		return nil, &url.Error{
			Op:  "Post",
			URL: "https://box-1-9223.preview.box.upstash.com/?token=" + testToken,
			Err: fmt.Errorf("tls failed for key %s header %s", testKey, headerValue),
		}
	})
	_, err := c.CDPURL(context.Background(), "box-1")
	if err == nil {
		t.Fatal("CDPURL: want transport error")
	}
	msg := err.Error()
	for _, secret := range []string{testKey, testToken, "https://", "box-1-9223.preview"} {
		if strings.Contains(msg, secret) {
			t.Errorf("error leaks %q: %s", secret, msg)
		}
	}
	if !strings.Contains(msg, "POST /v2/box/box-1/browser/connect") {
		t.Errorf("error should keep method and path: %s", msg)
	}
}

// TestClosedServerFailureRedactsURL uses a real transport failure: the server
// is closed before the request, so the dial fails.
func TestClosedServerFailureRedactsURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	base := srv.URL
	srv.Close()
	c, err := New(testKey, WithBaseURL(base))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = c.Get(context.Background(), "box-1")
	if err == nil {
		t.Fatal("Get against a closed server: want error")
	}
	msg := err.Error()
	if strings.Contains(msg, testKey) || strings.Contains(msg, "://") || strings.Contains(msg, base) {
		t.Errorf("error leaks URL or key: %s", msg)
	}
}

func TestAPIErrorRedactsEchoedSecrets(t *testing.T) {
	c := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
		// The server echoes the key from the header, the key itself, and a URL with the token.
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"error":"rejected `+r.Header.Get(headerAPIKey)+` key `+testKey+` via https://evil.example/?token=`+testToken+`"}`)
	})
	_, err := c.List(context.Background(), "")
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("List: err = %v, want *APIError", err)
	}
	if apiErr.StatusCode != http.StatusInternalServerError {
		t.Errorf("StatusCode = %d, want 500", apiErr.StatusCode)
	}
	msg := err.Error()
	if strings.Contains(msg, testKey) || strings.Contains(msg, testToken) || strings.Contains(msg, "evil.example") {
		t.Errorf("APIError leaks a secret or URL: %s", msg)
	}
	if !strings.Contains(msg, "[REDACTED]") {
		t.Errorf("APIError should show redaction marker: %s", msg)
	}
}

// TestErrorBodiesWithoutErrorFieldUseStatusMessage covers a non-JSON body, JSON
// with no error field, and JSON with an empty error string.
func TestErrorBodiesWithoutErrorFieldUseStatusMessage(t *testing.T) {
	for name, body := range map[string]string{
		"non-JSON":       "<html>upstream down</html>",
		"no error field": `{"message":"nope"}`,
		"empty error":    `{"error":""}`,
	} {
		t.Run(name, func(t *testing.T) {
			c := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusBadGateway)
				_, _ = io.WriteString(w, body)
			})
			_, err := c.Get(context.Background(), "box-1")
			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("Get: err = %v, want *APIError", err)
			}
			if want := "request failed with status 502"; apiErr.Message != want {
				t.Errorf("Message = %q, want %q", apiErr.Message, want)
			}
		})
	}
}

// TestRedirectNeverFollowedWithKey: the origin answers 302 to a second server.
// The second server must receive no request, for both the default client and an
// injected one. The 302 comes back as an *APIError.
func TestRedirectNeverFollowedWithKey(t *testing.T) {
	makers := map[string]func(origin string) (*Client, error){
		"default client": func(origin string) (*Client, error) {
			return New(testKey, WithBaseURL(origin))
		},
		"injected client": func(origin string) (*Client, error) {
			return New(testKey, WithBaseURL(origin), WithHTTPClient(&http.Client{}))
		},
	}
	for name, mk := range makers {
		t.Run(name, func(t *testing.T) {
			var hits atomic.Int32
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
			}))
			t.Cleanup(target.Close)
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, target.URL+"/landing", http.StatusFound)
			}))
			t.Cleanup(origin.Close)

			c, err := mk(origin.URL)
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			_, err = c.Get(context.Background(), "box-1")
			var apiErr *APIError
			if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusFound {
				t.Fatalf("Get: err = %v, want *APIError with status 302", err)
			}
			if n := hits.Load(); n != 0 {
				t.Errorf("redirect target received %d request(s), want 0", n)
			}
		})
	}
}

// TestWithHTTPClientLeavesCallerClientUnchanged: the caller's *http.Client keeps
// its own CheckRedirect. The copy the client uses is the one that refuses.
func TestWithHTTPClientLeavesCallerClientUnchanged(t *testing.T) {
	hc := &http.Client{}
	c, err := New(testKey, WithHTTPClient(hc))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if hc.CheckRedirect != nil {
		t.Error("caller's http.Client.CheckRedirect was modified")
	}
	if c.httpClient == hc || c.httpClient.CheckRedirect == nil {
		t.Error("client should use a copy with redirects refused")
	}
}

func TestUnexpectedResponseBodyIsError(t *testing.T) {
	c := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "not json")
	})
	if _, err := c.Get(context.Background(), "box-1"); err == nil {
		t.Fatal("Get with a non-JSON 200 body: want error")
	}
}
