package tool

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/u007/ocode/internal/auth"
	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/ocr"
)

func setupOcrAuthTestEnv(t *testing.T) {
	t.Helper()

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
}

func TestOcrToolUsesSavedLmStudioToken(t *testing.T) {
	setupOcrAuthTestEnv(t)

	if err := auth.Set("lmstudio", auth.Credential{Kind: auth.KindAPIKey, Key: "saved-token"}); err != nil {
		t.Fatalf("set auth: %v", err)
	}
	t.Cleanup(func() { _ = auth.Remove("lmstudio") })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/chat" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer saved-token" {
			t.Fatalf("Authorization header = %q, want %q", got, "Bearer saved-token")
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Fatalf("Content-Type header = %q, want %q", got, "application/json")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"output":[{"type":"message","content":"ok"}]}`))
	}))
	t.Cleanup(srv.Close)

	img := filepath.Join(t.TempDir(), "sample.png")
	if err := os.WriteFile(img, []byte("fake image"), 0o600); err != nil {
		t.Fatal(err)
	}

	tool := &OcrTool{Config: &config.Config{Ocode: config.OcodeConfig{Ocr: ocr.OcrConfig{
		Enabled: true,
		Backend: "lmstudio",
		OpenAI: ocr.OpenAICfg{
			BaseURL: srv.URL,
			Model:   "deepseek-ocr",
		},
	}}}}

	args, err := json.Marshal(map[string]string{"image_path": img})
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}

	got, err := tool.Execute(args)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got != "ok" {
		t.Fatalf("output = %q, want %q", got, "ok")
	}
}

func TestOcrToolPrefersExplicitApiKeyOverSavedToken(t *testing.T) {
	setupOcrAuthTestEnv(t)

	if err := auth.Set("lmstudio", auth.Credential{Kind: auth.KindAPIKey, Key: "saved-token"}); err != nil {
		t.Fatalf("set auth: %v", err)
	}
	t.Cleanup(func() { _ = auth.Remove("lmstudio") })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/chat" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer config-token" {
			t.Fatalf("Authorization header = %q, want %q", got, "Bearer config-token")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"output":[{"type":"message","content":"ok"}]}`))
	}))
	t.Cleanup(srv.Close)

	img := filepath.Join(t.TempDir(), "sample.png")
	if err := os.WriteFile(img, []byte("fake image"), 0o600); err != nil {
		t.Fatal(err)
	}

	tool := &OcrTool{Config: &config.Config{Ocode: config.OcodeConfig{Ocr: ocr.OcrConfig{
		Enabled: true,
		Backend: "lmstudio",
		OpenAI: ocr.OpenAICfg{
			BaseURL: srv.URL,
			Model:   "deepseek-ocr",
			APIKey:  "config-token",
		},
	}}}}

	args, err := json.Marshal(map[string]string{"image_path": img})
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}

	got, err := tool.Execute(args)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got != "ok" {
		t.Fatalf("output = %q, want %q", got, "ok")
	}
}
