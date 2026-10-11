package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/config"
)

// The handler tests call HandleGetQuickActionsConfig/HandleSetQuickActionsConfig
// directly. That cannot catch a missing or shadowed ROUTE — the failure mode
// that matters here is a 404 from the SPA fallback, which keeps every handler
// test green while the settings form silently fails to save. So drive the real
// mux, exactly like handler_terminal_tabs_routes_test.go.
func TestQuickActionsRoutesAreRegistered(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	srv := New("127.0.0.1:0", "", "", nil)
	h := srv.serveHandler()

	// GET first. The JSON content type is the load-bearing assertion: an
	// unregistered /api/* path falls through to the SPA handler and answers
	// 200 with an HTML body, which the settings form would try to parse as
	// config. The seed count here is a smoke check only -- an absent key is also
	// seeded by the config loader (ocodeconfig.go), so it does not by itself
	// prove the handler's own seed branch; TestHandleGetQuickActionsSeedsWhenAbsent
	// pins that.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, quickActionsPath, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET = %d body=%s (a 404 here means the route is missing, not that the handler is wrong)",
			rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("GET Content-Type = %q, want JSON; an HTML body means the SPA fallback answered", ct)
	}
	var seeded config.QuickActionsConfig
	if err := json.Unmarshal(rec.Body.Bytes(), &seeded); err != nil {
		t.Fatalf("decode GET: %v body=%s", err, rec.Body.String())
	}
	if len(seeded.Chips) != len(config.SeedQuickActions().Chips) {
		t.Fatalf("GET over the mux seeded %d chips, want %d: %+v",
			len(seeded.Chips), len(config.SeedQuickActions().Chips), seeded.Chips)
	}

	// PUT through the router.
	body := `{"chips":[{"id":"a","label":"A","icon":"zap","message":"m","mode":"send"}]}`
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, quickActionsPath, strings.NewReader(body))
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT = %d body=%s (a 404 here means the route is missing)", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, quickActionsPath, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp config.QuickActionsConfig
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v body=%s", err, rec.Body.String())
	}
	if len(resp.Chips) != 1 || resp.Chips[0].ID != "a" {
		t.Fatalf("GET over the mux did not round-trip the saved chip: %+v", resp.Chips)
	}
}

// A validation rejection must surface as a 400 JSON error over the wire, not as
// a 200 or an SPA HTML body the settings form would try to parse as config.
func TestQuickActionsRouteRejectsOverCapOverTheMux(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	srv := New("127.0.0.1:0", "", "", nil)
	h := srv.serveHandler()

	chips := make([]string, 0, config.QuickActionsMaxChips+1)
	for i := 0; i <= config.QuickActionsMaxChips; i++ {
		chips = append(chips, `{"id":"c`+itoaTest(i)+`","label":"C","icon":"zap","message":"m","mode":"send"}`)
	}
	body := `{"chips":[` + strings.Join(chips, ",") + `]}`

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, quickActionsPath, strings.NewReader(body)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("PUT over cap = %d, want 400 body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "at most 20") {
		t.Fatalf("error body %q does not explain the cap", rec.Body.String())
	}
}
