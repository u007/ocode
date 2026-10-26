package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/browsertabs"
)

// The handler tests call the handlers directly, which cannot catch a missing
// or shadowed route. A missing route falls through to the SPA and answers 404,
// the same failure that made the terminal tab list invisible to a second
// browser. So drive the real mux.
func TestBrowserTabsRoutesAreRegistered(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	srv := New("127.0.0.1:0", "", "", nil)
	store, err := browsertabs.NewStoreAt(filepath.Join(t.TempDir(), "browser-tabs.json"))
	if err != nil {
		t.Fatalf("browsertabs.NewStoreAt: %v", err)
	}
	srv.handler.browserTabsStore = store
	h := srv.serveHandler()

	body := `{"projects":{"/proj":{"tabs":[{"id":"b1","title":"One"}],"active_id":"b1","surfaces":{}}}}`
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/browser-tabs", strings.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT /api/browser-tabs = %d body=%s (404 means the route is missing)", rec.Code, rec.Body.String())
	}
	if got := store.All()["/proj"]; len(got.Tabs) != 1 || got.Tabs[0].ID != "b1" {
		t.Fatalf("store after router PUT = %+v", got)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/browser-tabs", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/browser-tabs = %d body=%s", rec.Code, rec.Body.String())
	}
	var got struct {
		Projects map[string]browsertabs.ProjectBrowsers `json:"projects"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Projects["/proj"].Tabs) != 1 {
		t.Fatalf("router GET returned %+v", got)
	}
}
