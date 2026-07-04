package ocr

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestOpenAICompatExecuteStripsLMStudioPrefix(t *testing.T) {
	var gotModel string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		var payload struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		gotModel = payload.Model
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	t.Cleanup(srv.Close)

	img := t.TempDir() + "/sample.png"
	if err := os.WriteFile(img, []byte("fake image"), 0o600); err != nil {
		t.Fatal(err)
	}

	text, err := (&openaiCompatBackend{}).Execute(context.Background(), img, BackendConfig{BaseURL: srv.URL, Model: "lmstudio/deepseek-ocr"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if text != "ok" {
		t.Fatalf("expected response text %q, got %q", "ok", text)
	}
	if gotModel != "deepseek-ocr" {
		t.Fatalf("expected stripped model %q, got %q", "deepseek-ocr", gotModel)
	}
}
