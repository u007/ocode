package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/browse"
)

func TestBrowseConfigEndpoint(t *testing.T) {
	s := New("127.0.0.1:0", "", "", nil) // no-auth loopback form
	s.EnableBrowse("http://127.0.0.1:54321", browse.New("", nil))
	r := httptest.NewRequest("GET", "/api/browse/config", nil)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d", w.Code)
	}
	var body struct {
		BaseURL string `json:"base_url"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.BaseURL != "http://127.0.0.1:54321" {
		t.Fatalf("base_url = %q", body.BaseURL)
	}
}

// TestBrowseGrantEndpointRoundTrip proves the main server mints grants on the
// browse server it was wired with: the returned grant must redeem (302 +
// session cookie) on that browse server's handler.
func TestBrowseGrantEndpointRoundTrip(t *testing.T) {
	bs := browse.New("apitoken", nil)
	s := New("127.0.0.1:0", "", "", nil)
	s.EnableBrowse("http://127.0.0.1:9999", bs)

	r := httptest.NewRequest("POST", "/api/browse/grant",
		strings.NewReader(`{"state_key":"tab:abc"}`))
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("grant: got %d want 200", w.Code)
	}
	var resp struct {
		Grant string `json:"grant"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode grant: %v", err)
	}
	if resp.Grant == "" {
		t.Fatal("empty grant")
	}

	nav := httptest.NewRequest("GET", "/b/tab:abc/https/example.com/?__grant="+resp.Grant, nil)
	nw := httptest.NewRecorder()
	bs.Handler().ServeHTTP(nw, nav)
	if nw.Code != http.StatusFound {
		t.Fatalf("grant redeem on browse origin: got %d want 302", nw.Code)
	}
}

func TestBrowseGrantRejectsEmptyStateKey(t *testing.T) {
	s := New("127.0.0.1:0", "", "", nil)
	s.EnableBrowse("http://127.0.0.1:9999", browse.New("", nil))
	r := httptest.NewRequest("POST", "/api/browse/grant", strings.NewReader(`{"state_key":""}`))
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("empty state_key: got %d want 400", w.Code)
	}
}

func TestBrowseGrantRejectsBadJSON(t *testing.T) {
	s := New("127.0.0.1:0", "", "", nil)
	s.EnableBrowse("http://127.0.0.1:9999", browse.New("", nil))
	r := httptest.NewRequest("POST", "/api/browse/grant", strings.NewReader(`{oops`))
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad JSON: got %d want 400", w.Code)
	}
}
