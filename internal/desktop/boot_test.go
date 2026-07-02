package desktop

import (
	"net/http"
	"testing"
	"time"
)

func TestStartServerServesAuthedAPI(t *testing.T) {
	h, err := StartServer(nil, t.TempDir()) // nil webFS: API still works, SPA 404s
	if err != nil {
		t.Fatalf("StartServer: %v", err)
	}
	if h.Token == "" || len(h.Token) != 32 {
		t.Fatalf("expected 32-char hex token, got %q", h.Token)
	}
	if h.URL == "" {
		t.Fatal("expected non-empty URL")
	}

	client := &http.Client{Timeout: 2 * time.Second}

	// Authed request succeeds.
	req, _ := http.NewRequest("GET", h.URL+"/api/models", nil)
	req.Header.Set("Authorization", "Bearer "+h.Token)
	res, err := client.Do(req)
	if err != nil {
		t.Fatalf("authed request failed: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}
}

func TestStartServerRejectsUnauthed(t *testing.T) {
	h, err := StartServer(nil, t.TempDir())
	if err != nil {
		t.Fatalf("StartServer: %v", err)
	}

	client := &http.Client{Timeout: 2 * time.Second}

	// No auth header → 401
	req, _ := http.NewRequest("GET", h.URL+"/api/models", nil)
	res, err := client.Do(req)
	if err != nil {
		t.Fatalf("unauthed request failed: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", res.StatusCode)
	}

	// Wrong token → 401
	req, _ = http.NewRequest("GET", h.URL+"/api/models", nil)
	req.Header.Set("Authorization", "Bearer wrongtoken")
	res, err = client.Do(req)
	if err != nil {
		t.Fatalf("wrong-token request failed: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 for wrong token, got %d", res.StatusCode)
	}
}
