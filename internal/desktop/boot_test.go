package desktop

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStartServerServesAuthedAPI(t *testing.T) {
	t.Setenv("OPENCODE_CONFIG_DIR", t.TempDir()) // keep the sticky-port file out of the real config dir
	h, err := StartServer(nil, t.TempDir())      // nil webFS: API still works, SPA 404s
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

func TestPortStickinessRoundTrip(t *testing.T) {
	t.Setenv("OPENCODE_CONFIG_DIR", t.TempDir())
	if p := loadSavedPort(); p != 0 {
		t.Fatalf("expected no saved port on first run, got %d", p)
	}
	saveBoundPort("127.0.0.1:45678")
	if p := loadSavedPort(); p != 45678 {
		t.Fatalf("expected saved port 45678, got %d", p)
	}
}

func TestSaveDebugHandleWritesURLAndToken(t *testing.T) {
	cfgDir := t.TempDir()
	t.Setenv("OPENCODE_CONFIG_DIR", cfgDir)

	saveDebugHandle("http://127.0.0.1:45678", "deadbeef")

	data, err := os.ReadFile(filepath.Join(cfgDir, "desktop-debug-handle"))
	if err != nil {
		t.Fatalf("read debug handle file: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "url=http://127.0.0.1:45678") {
		t.Errorf("expected url in content, got %q", content)
	}
	if !strings.Contains(content, "token=deadbeef") {
		t.Errorf("expected token in content, got %q", content)
	}
}

func TestStartServerRejectsUnauthed(t *testing.T) {
	t.Setenv("OPENCODE_CONFIG_DIR", t.TempDir()) // keep the sticky-port file out of the real config dir
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
