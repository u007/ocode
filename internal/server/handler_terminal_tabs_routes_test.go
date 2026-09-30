package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/termtabs"
)

// The handler tests call HandleGetTerminalTabs/HandleSetTerminalTabs directly.
// That cannot catch a missing or shadowed ROUTE — the failure mode that matters
// here is a 404 from the SPA fallback, which is exactly how the "second browser
// sees no terminal" bug presented. So drive the real mux.
func newTerminalTabsRouter(t *testing.T) (*Server, *termtabs.Store) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	srv := New("127.0.0.1:0", "", "", nil)
	store, err := termtabs.NewStoreAt(t.TempDir() + "/terminals.json")
	if err != nil {
		t.Fatalf("termtabs.NewStoreAt: %v", err)
	}
	srv.handler.termTabsStore = store
	return srv, store
}

func TestTerminalTabsRoutesAreRegistered(t *testing.T) {
	srv, store := newTerminalTabsRouter(t)
	h := srv.serveHandler()

	// PUT through the router.
	body := `{"projects":{"/proj":{"terminals":[{"id":"t1","title":"One"}]}}}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/terminal-tabs", strings.NewReader(body))
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT /api/terminal-tabs = %d body=%s (a 404 here means the route is missing)", rec.Code, rec.Body.String())
	}
	if got := store.Get("/proj").Terminals; len(got) != 1 || got[0].ID != "t1" {
		t.Fatalf("store after router PUT = %+v", got)
	}

	// GET through the router.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/terminal-tabs", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/terminal-tabs = %d body=%s", rec.Code, rec.Body.String())
	}
	var got struct {
		Projects map[string]termtabs.ProjectTerminals `json:"projects"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Projects["/proj"].Terminals) != 1 {
		t.Fatalf("router GET returned %+v", got)
	}
}

// /api/terminal-tabs must not be swallowed by the terminal routes' wildcards
// (`/api/terminal/{id}` DELETE, `/api/terminal/{id}/history` GET), nor vice
// versa: a terminal id is never literally "tabs" here, but a mis-registered
// pattern would silently route a real terminal id to the tab list.
func TestTerminalTabsRouteDoesNotShadowTerminalIDRoutes(t *testing.T) {
	srv, _ := newTerminalTabsRouter(t)
	h := srv.serveHandler()

	// DELETE /api/terminal/term-1 must reach the kill handler (405/403/404 are
	// all fine — a 200 from the tab-list handler would mean it was shadowed).
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/terminal/term-1?project_path=/proj", nil))
	if rec.Code == http.StatusOK {
		t.Fatalf("DELETE /api/terminal/term-1 was routed to the tab list: %s", rec.Body.String())
	}

	// GET /api/terminal-tabs must reach the tab list, not the history handler.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/terminal-tabs", nil))
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("GET /api/terminal-tabs Content-Type = %q, want JSON (got body %s)", ct, rec.Body.String())
	}
}
