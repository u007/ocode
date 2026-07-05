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

func TestOpenAICompatExecuteUsesLMStudioNativeChatFormat(t *testing.T) {
	var gotPayload struct {
		Model        string `json:"model"`
		SystemPrompt string `json:"system_prompt"`
		Input        []struct {
			Type    string `json:"type"`
			Content string `json:"content"`
			DataURL string `json:"data_url"`
		} `json:"input"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/chat" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "" {
			t.Fatalf("Authorization header = %q, want empty", got)
		}
		var payload struct {
			Model        string `json:"model"`
			SystemPrompt string `json:"system_prompt"`
			Input        []struct {
				Type    string `json:"type"`
				Content string `json:"content"`
				DataURL string `json:"data_url"`
			} `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		gotPayload = payload
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"output":[{"type":"message","content":"ok"}]}`))
	}))
	t.Cleanup(srv.Close)

	img := t.TempDir() + "/sample.png"
	if err := os.WriteFile(img, []byte("fake image"), 0o600); err != nil {
		t.Fatal(err)
	}

	text, err := (&openaiCompatBackend{}).Execute(context.Background(), img, BackendConfig{BaseURL: srv.URL, Model: "deepseek-ocr", LMStudioNative: true})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if text != "ok" {
		t.Fatalf("expected response text %q, got %q", "ok", text)
	}
	// Some vision-model jinja templates (e.g. paddleocr-vl) fail to render a
	// string system message, so the instruction must ride in the text input
	// item and system_prompt must stay absent.
	if gotPayload.SystemPrompt != "" {
		t.Fatalf("expected no system_prompt, got %q", gotPayload.SystemPrompt)
	}
	if gotPayload.Model != "deepseek-ocr" {
		t.Fatalf("expected model %q, got %q", "deepseek-ocr", gotPayload.Model)
	}
	if len(gotPayload.Input) != 2 {
		t.Fatalf("expected 2 input items, got %d", len(gotPayload.Input))
	}
	if gotPayload.Input[0].Type != "text" || !strings.Contains(gotPayload.Input[0].Content, "OCR engine") {
		t.Fatalf("unexpected text input: %#v", gotPayload.Input[0])
	}
	if gotPayload.Input[1].Type != "image" || gotPayload.Input[1].DataURL == "" {
		t.Fatalf("unexpected image input: %#v", gotPayload.Input[1])
	}
}
