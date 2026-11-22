package messaging

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// telegramStub mimics the Telegram Bot API getMe endpoint.
func telegramStub(t *testing.T, valid bool) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/bot") {
			http.Error(w, "unexpected path", http.StatusNotFound)
			return
		}
		if !valid {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"ok":          false,
				"error_code":  401,
				"description": "Unauthorized",
			})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"ok": true,
			"result": map[string]interface{}{
				"id":         123456789,
				"is_bot":     true,
				"first_name": "Test Bot",
				"username":   "test_bot",
			},
		})
	}))
}

type capturingClient struct {
	req *http.Request
	http.Client
}

func (c *capturingClient) Do(req *http.Request) (*http.Response, error) {
	c.req = req
	return c.Client.Do(req)
}

func TestValidateBotTokenWithClient(t *testing.T) {
	// valid token → nil error
	svr := telegramStub(t, true)
	defer svr.Close()
	// Rebuild the endpoint with the stub host: /bot<token>/getMe
	endpoint := svr.URL + "/bot%s/%s"
	if err := ValidateBotTokenWithClient("123456:ABC", endpoint, &http.Client{Timeout: 5 * time.Second}); err != nil {
		t.Fatalf("valid token: %v", err)
	}

	// invalid token → error, and the token is never leaked into the message
	svr2 := telegramStub(t, false)
	defer svr2.Close()
	endpoint2 := svr2.URL + "/bot%s/%s"
	err2 := ValidateBotTokenWithClient("bad:TOKEN", endpoint2, &http.Client{Timeout: 5 * time.Second})
	if err2 == nil {
		t.Fatal("invalid token: expected error, got nil")
	}
	if strings.Contains(err2.Error(), "bad:TOKEN") {
		t.Fatalf("token leaked into error: %q", err2.Error())
	}

	// capture the request path to confirm getMe is used
	captured := &capturingClient{Client: http.Client{Timeout: 5 * time.Second}}
	svr3 := telegramStub(t, true)
	defer svr3.Close()
	endpoint3 := svr3.URL + "/bot%s/%s"
	if err := ValidateBotTokenWithClient("123456:ABC", endpoint3, captured); err != nil {
		t.Fatalf("capture: %v", err)
	}
	if captured.req == nil {
		t.Fatal("no request captured")
	}
	if !strings.HasSuffix(captured.req.URL.Path, "/getMe") {
		t.Fatalf("expected getMe, got %s", captured.req.URL.Path)
	}

	// empty token → NewBotAPI still calls getMe; telegram rejects empty token path
	svr4 := telegramStub(t, false)
	defer svr4.Close()
	endpoint4 := svr4.URL + "/bot%s/%s"
	if err := ValidateBotTokenWithClient("", endpoint4, &http.Client{Timeout: 5 * time.Second}); err == nil {
		t.Fatal("empty token: expected error, got nil")
	}
}
