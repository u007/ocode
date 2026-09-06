package server

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/u007/ocode/internal/version"
)

func TestHandleHealth(t *testing.T) {
	s := New("127.0.0.1:0", "user", "pass", nil) // even an authenticated server...
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/health", nil) // ...answers /api/health with no credentials
	s.mux.ServeHTTP(w, r)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var body struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON body: %v", err)
	}
	if body.Version != version.Version {
		t.Errorf("version = %q, want %q", body.Version, version.Version)
	}
}
