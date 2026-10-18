package auth

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func probeServer(t *testing.T, modelsJSON string) (*httptest.Server, *[]string) {
	t.Helper()
	var mu sync.Mutex
	var completionModels []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/models":
			_, _ = w.Write([]byte(modelsJSON))
		case "/chat/completions":
			body, _ := io.ReadAll(r.Body)
			mu.Lock()
			completionModels = append(completionModels, string(body))
			mu.Unlock()
			if strings.Contains(string(body), "embed") {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &completionModels
}

func TestProbeOpenAIInferenceSkipsNonChatModels(t *testing.T) {
	srv, bodies := probeServer(t, `{"data":[{"id":"text-embedding-3"},{"id":"gpt-chat"}]}`)
	if err := probeOpenAIInference(context.Background(), srv.URL, "k", false); err != nil {
		t.Fatalf("valid key rejected: %v", err)
	}
	if len(*bodies) != 1 || !strings.Contains((*bodies)[0], "gpt-chat") {
		t.Fatalf("completion sent to %v, want gpt-chat", *bodies)
	}
}

func TestProbeOpenAIInferenceFailsWhenNoChatModelListed(t *testing.T) {
	for _, body := range []string{`{"data":[]}`, `{"data":[{"id":"text-embedding-3"}]}`, `not json`} {
		srv, _ := probeServer(t, body)
		if err := probeOpenAIInference(context.Background(), srv.URL, "k", false); err == nil {
			t.Fatalf("models %q: want error, got nil (unverified credential must not read as ok)", body)
		}
	}
}
