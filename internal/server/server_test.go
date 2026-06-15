package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestHandlerChatNoContent(t *testing.T) {
	h := NewHandler()
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/chat", bytes.NewReader([]byte(`{}`)))
	h.HandleChat(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandlerChatInvalidJSON(t *testing.T) {
	h := NewHandler()
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/chat", bytes.NewReader([]byte(`invalid`)))
	h.HandleChat(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestAuthMiddleware(t *testing.T) {
	s := New("localhost:0", "user", "pass", nil)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/sessions", nil)
	s.mux.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}

	w = httptest.NewRecorder()
	r = httptest.NewRequest("GET", "/api/sessions", nil)
	r.SetBasicAuth("user", "pass")
	s.mux.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestRealIPIgnoresForwardedHeaders(t *testing.T) {
	r := httptest.NewRequest("GET", "/api/sessions", nil)
	r.RemoteAddr = "203.0.113.10:4321"
	r.Header.Set("X-Real-IP", "198.51.100.1")
	r.Header.Set("X-Forwarded-For", "198.51.100.2, 198.51.100.3")

	if got := realIP(r); got != "203.0.113.10" {
		t.Fatalf("expected remote addr IP, got %q", got)
	}
}

func TestAuthMiddlewareRateLimitUsesRemoteAddr(t *testing.T) {
	s := New("localhost:0", "user", "pass", nil)

	for i := 0; i < 5; i++ {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/api/sessions", nil)
		r.RemoteAddr = "203.0.113.10:4321"
		r.Header.Set("X-Forwarded-For", fmt.Sprintf("198.51.100.%d", i))
		s.mux.ServeHTTP(w, r)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: expected 401, got %d", i+1, w.Code)
		}
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/sessions", nil)
	r.RemoteAddr = "203.0.113.10:4321"
	r.Header.Set("X-Forwarded-For", "198.51.100.250")
	s.mux.ServeHTTP(w, r)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 after repeated failures from same remote addr, got %d", w.Code)
	}
}

func TestNoAuthWhenEmpty(t *testing.T) {
	s := New("localhost:0", "", "", nil)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/sessions", nil)
	s.mux.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestListenFallsForwardWhenPortBusy(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()

	_, busyPortStr, err := net.SplitHostPort(busy.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	busyPort, err := strconv.Atoi(busyPortStr)
	if err != nil {
		t.Fatal(err)
	}

	s := New("127.0.0.1:"+busyPortStr, "", "", nil)
	ln, err := s.Listen()
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer ln.Close()

	if strings.HasSuffix(s.Addr(), ":"+busyPortStr) {
		t.Fatalf("expected Listen to fall forward from busy port %d, got %s", busyPort, s.Addr())
	}
}

func TestChatStreamNoMessage(t *testing.T) {
	h := NewHandler()
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/chat/stream", nil)
	h.HandleChatStream(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestListModels(t *testing.T) {
	h := NewHandler()
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/models", nil)
	h.HandleListModels(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var models []ModelInfo
	if err := json.Unmarshal(w.Body.Bytes(), &models); err != nil {
		t.Errorf("invalid JSON: %v", err)
	}
}
