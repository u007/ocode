package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	s := New("localhost:0", "user", "pass")

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

func TestNoAuthWhenEmpty(t *testing.T) {
	s := New("localhost:0", "", "")

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/sessions", nil)
	s.mux.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
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
