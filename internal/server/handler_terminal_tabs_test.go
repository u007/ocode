package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/termtabs"
)

func testTerminalTabsHandler(t *testing.T) *Handler {
	t.Helper()
	h := testHandlerWithConfig(t)
	store, err := termtabs.NewStoreAt(filepath.Join(t.TempDir(), "terminals.json"))
	if err != nil {
		t.Fatalf("termtabs.NewStoreAt: %v", err)
	}
	h.termTabsStore = store
	return h
}

// The reported bug, at the HTTP layer: a terminal PUT by one client must be
// visible to a GET from another, and the write must fan a
// terminal_tabs_changed envelope so other windows refetch instead of polling.
func TestHandleTerminalTabsBulkRoundTripAndPublishes(t *testing.T) {
	h := testTerminalTabsHandler(t)
	sub := h.bus.Subscribe(nil)
	defer h.bus.Unsubscribe(sub)

	body := `{"projects":{"/proj-a":{"terminals":[{"id":"t1","title":"One","renamed":true},{"id":"t2","title":"Two","osc_title":"npm run dev"}]}}}`
	rr := httptest.NewRecorder()
	h.HandleSetTerminalTabs(rr, httptest.NewRequest(http.MethodPut, "/api/terminal-tabs", strings.NewReader(body)))
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT status=%d body=%s", rr.Code, rr.Body.String())
	}

	select {
	case env := <-sub:
		if env.Event != "terminal_tabs_changed" {
			t.Fatalf("event=%q want terminal_tabs_changed", env.Event)
		}
	default:
		t.Fatal("no terminal_tabs_changed event published")
	}

	rr = httptest.NewRecorder()
	h.HandleGetTerminalTabs(rr, httptest.NewRequest(http.MethodGet, "/api/terminal-tabs", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET status=%d body=%s", rr.Code, rr.Body.String())
	}
	var got struct {
		Projects map[string]termtabs.ProjectTerminals `json:"projects"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode GET: %v", err)
	}
	terms := got.Projects["/proj-a"].Terminals
	if len(terms) != 2 {
		t.Fatalf("got %d terminals, want 2: %+v", len(terms), terms)
	}
	if terms[0].ID != "t1" || !terms[0].Renamed || terms[0].Title != "One" {
		t.Fatalf("first terminal lost metadata: %+v", terms[0])
	}
	if terms[1].OSCTitle != "npm run dev" {
		t.Fatalf("OSC title lost: %+v", terms[1])
	}
}

// A remote project's key is `<host>::<path>`. It must survive the HTTP layer
// byte-for-byte — a rewritten key would silently split one project's list in
// two, so the second client would see half the terminals.
func TestHandleTerminalTabsPreservesRemoteHostQualifiedKey(t *testing.T) {
	h := testTerminalTabsHandler(t)

	key := "james@217.216.72.49::/home/james/www/aimsai2"
	body, err := json.Marshal(map[string]any{
		"projects": map[string]any{
			key: map[string]any{"terminals": []map[string]any{{"id": "t1", "title": "One"}}},
		},
	})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	rr := httptest.NewRecorder()
	h.HandleSetTerminalTabs(rr, httptest.NewRequest(http.MethodPut, "/api/terminal-tabs", strings.NewReader(string(body))))
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT status=%d body=%s", rr.Code, rr.Body.String())
	}

	rr = httptest.NewRecorder()
	h.HandleGetTerminalTabs(rr, httptest.NewRequest(http.MethodGet, "/api/terminal-tabs", nil))
	var got struct {
		Projects map[string]termtabs.ProjectTerminals `json:"projects"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode GET: %v", err)
	}
	if len(got.Projects) != 1 {
		t.Fatalf("stored %d keys, want 1: %+v", len(got.Projects), got.Projects)
	}
	if _, ok := got.Projects[key]; !ok {
		t.Fatalf("key was rewritten; got %+v", got.Projects)
	}
}

// A partial PUT must not delete projects it did not mention — this is what
// stops a second browser from wiping the desktop app's terminals.
func TestHandleTerminalTabsPartialPutPreservesOtherProjects(t *testing.T) {
	h := testTerminalTabsHandler(t)

	seed := `{"projects":{"/a":{"terminals":[{"id":"ta","title":"A"}]},"/b":{"terminals":[{"id":"tb","title":"B"}]}}}`
	rr := httptest.NewRecorder()
	h.HandleSetTerminalTabs(rr, httptest.NewRequest(http.MethodPut, "/api/terminal-tabs", strings.NewReader(seed)))
	if rr.Code != http.StatusOK {
		t.Fatalf("seed PUT status=%d body=%s", rr.Code, rr.Body.String())
	}

	// A client that only knows /a writes only /a.
	partial := `{"projects":{"/a":{"terminals":[{"id":"ta","title":"A"},{"id":"ta2","title":"A2"}]}}}`
	rr = httptest.NewRecorder()
	h.HandleSetTerminalTabs(rr, httptest.NewRequest(http.MethodPut, "/api/terminal-tabs", strings.NewReader(partial)))
	if rr.Code != http.StatusOK {
		t.Fatalf("partial PUT status=%d body=%s", rr.Code, rr.Body.String())
	}

	if got := h.termTabsStore.Get("/b").Terminals; len(got) != 1 || got[0].ID != "tb" {
		t.Fatalf("/b = %+v, want it preserved", got)
	}
	if got := h.termTabsStore.Get("/a").Terminals; len(got) != 2 {
		t.Fatalf("/a = %+v, want 2 terminals", got)
	}
}

// Closing the project's last terminal sends an explicit empty list, which the
// store treats as a delete. If the empty list were dropped instead, the closed
// tab would reappear on the next restore.
func TestHandleTerminalTabsEmptyListDeletesTheProject(t *testing.T) {
	h := testTerminalTabsHandler(t)

	seed := `{"projects":{"/a":{"terminals":[{"id":"ta","title":"A"}]}}}`
	rr := httptest.NewRecorder()
	h.HandleSetTerminalTabs(rr, httptest.NewRequest(http.MethodPut, "/api/terminal-tabs", strings.NewReader(seed)))
	if rr.Code != http.StatusOK {
		t.Fatalf("seed PUT status=%d", rr.Code)
	}

	clear := `{"projects":{"/a":{"terminals":[]}}}`
	rr = httptest.NewRecorder()
	h.HandleSetTerminalTabs(rr, httptest.NewRequest(http.MethodPut, "/api/terminal-tabs", strings.NewReader(clear)))
	if rr.Code != http.StatusOK {
		t.Fatalf("clear PUT status=%d body=%s", rr.Code, rr.Body.String())
	}
	if got := h.termTabsStore.Get("/a").Terminals; len(got) != 0 {
		t.Fatalf("/a = %+v, want deleted", got)
	}
}

// An id-less terminal is unattachable — it can never open a shell — so it is
// dropped at the boundary rather than persisted and rendered.
func TestHandleTerminalTabsDropsIDLessTerminals(t *testing.T) {
	h := testTerminalTabsHandler(t)

	body := `{"projects":{"/a":{"terminals":[{"id":"","title":"ghost"},{"id":"real","title":"Real"}]}}}`
	rr := httptest.NewRecorder()
	h.HandleSetTerminalTabs(rr, httptest.NewRequest(http.MethodPut, "/api/terminal-tabs", strings.NewReader(body)))
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT status=%d body=%s", rr.Code, rr.Body.String())
	}
	got := h.termTabsStore.Get("/a").Terminals
	if len(got) != 1 || got[0].ID != "real" {
		t.Fatalf("stored %+v, want only the id-bearing terminal", got)
	}
}

func TestHandleSetTerminalTabsRejectsBadRequests(t *testing.T) {
	h := testTerminalTabsHandler(t)

	rr := httptest.NewRecorder()
	h.HandleSetTerminalTabs(rr, httptest.NewRequest(http.MethodPut, "/api/terminal-tabs", strings.NewReader("{not json")))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("malformed body status=%d, want 400", rr.Code)
	}

	rr = httptest.NewRecorder()
	h.HandleSetTerminalTabs(rr, httptest.NewRequest(http.MethodPut, "/api/terminal-tabs", strings.NewReader(`{}`)))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("empty body status=%d, want 400", rr.Code)
	}
}

// With no store (unresolvable data dir) the GET must still answer with a valid
// empty shape rather than panicking, so the client renders an empty strip
// instead of breaking on `undefined`.
func TestHandleGetTerminalTabsWithoutStore(t *testing.T) {
	h := testHandlerWithConfig(t)
	h.termTabsStore = nil

	rr := httptest.NewRecorder()
	h.HandleGetTerminalTabs(rr, httptest.NewRequest(http.MethodGet, "/api/terminal-tabs", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", rr.Code)
	}
	var got struct {
		Projects map[string]termtabs.ProjectTerminals `json:"projects"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Projects == nil {
		t.Fatal("projects must be an empty object, not null — the client indexes it directly")
	}
}

// The single-key form used for a focused-project fetch.
func TestHandleGetTerminalTabsByKey(t *testing.T) {
	h := testTerminalTabsHandler(t)

	seed := `{"projects":{"k1":{"terminals":[{"id":"t1","title":"One"}]}}}`
	rr := httptest.NewRecorder()
	h.HandleSetTerminalTabs(rr, httptest.NewRequest(http.MethodPut, "/api/terminal-tabs", strings.NewReader(seed)))
	if rr.Code != http.StatusOK {
		t.Fatalf("seed PUT status=%d", rr.Code)
	}

	rr = httptest.NewRecorder()
	h.HandleGetTerminalTabs(rr, httptest.NewRequest(http.MethodGet, "/api/terminal-tabs?key=k1", nil))
	var got termtabs.ProjectTerminals
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Terminals) != 1 || got.Terminals[0].ID != "t1" {
		t.Fatalf("keyed GET returned %+v", got)
	}
}
