package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLoginPollsUntilApprovedAndSavesToken(t *testing.T) {
	withTempHome(t)

	var pollCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/ocode/device/start" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"deviceCode": "dev-123",
				"userCode":   "ABCD-EFGH-IJKL",
				"verifyUrl":  "http://example.com/device",
				"expiresIn":  600,
			})
			return
		}
		if r.URL.Path == "/api/ocode/device/token" {
			pollCount++
			if pollCount < 3 {
				json.NewEncoder(w).Encode(map[string]interface{}{
					"status": "pending",
				})
				return
			}
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "approved",
				"token":  "sync-token-abc",
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	c := &Client{
		BaseURL:      srv.URL,
		HTTPClient:   srv.Client(),
		pollInterval: 0, // immediate
	}

	var printed string
	err := c.Login(context.Background(), func(url string) error { return nil }, func(format string, args ...interface{}) {
		printed += fmt.Sprintf(format, args...)
	})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if pollCount < 3 {
		t.Errorf("expected at least 3 polls before approval, got %d", pollCount)
	}

	token, ok, err := LoadToken()
	if err != nil {
		t.Fatalf("LoadToken: %v", err)
	}
	if !ok {
		t.Fatal("expected token to be saved")
	}
	if token != "sync-token-abc" {
		t.Fatalf("expected 'sync-token-abc', got %q", token)
	}
}

func TestLoginReturnsErrorOnExpired(t *testing.T) {
	withTempHome(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/ocode/device/start" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"deviceCode": "dev-expired",
				"userCode":   "EXPI-RED-CODE",
				"verifyUrl":  "http://example.com/device",
				"expiresIn":  0, // already expired
			})
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "expired",
		})
	}))
	defer srv.Close()

	c := &Client{
		BaseURL:      srv.URL,
		HTTPClient:   srv.Client(),
		pollInterval: 0,
	}

	err := c.Login(context.Background(), func(url string) error { return nil }, func(format string, args ...interface{}) {})
	if err == nil {
		t.Fatal("expected error for expired code")
	}
}
