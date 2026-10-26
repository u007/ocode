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

func testBrowserTabsHandler(t *testing.T) *Handler {
	t.Helper()
	h := testHandlerWithConfig(t)
	store, err := browsertabs.NewStoreAt(filepath.Join(t.TempDir(), "browser-tabs.json"))
	if err != nil {
		t.Fatalf("browsertabs.NewStoreAt: %v", err)
	}
	h.browserTabsStore = store
	return h
}

func putBrowserTabs(t *testing.T, h *Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	rr := httptest.NewRecorder()
	h.HandleSetBrowserTabs(rr, httptest.NewRequest(http.MethodPut, "/api/browser-tabs", strings.NewReader(body)))
	return rr
}

func getBrowserTabs(t *testing.T, h *Handler) map[string]browsertabs.ProjectBrowsers {
	t.Helper()
	rr := httptest.NewRecorder()
	h.HandleGetBrowserTabs(rr, httptest.NewRequest(http.MethodGet, "/api/browser-tabs", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET status=%d body=%s", rr.Code, rr.Body.String())
	}
	var got struct {
		Projects map[string]browsertabs.ProjectBrowsers `json:"projects"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode GET: %v", err)
	}
	return got.Projects
}

const browserTabsOneProject = `{"projects":{"/proj-a":{
	"tabs":[{"id":"b1","title":"One"},{"id":"b2","title":"Mine","manual_title":"Mine"}],
	"active_id":"b2",
	"surfaces":{"b1":{"url":"https://a.example/","history":["https://a.example/"],"history_index":0,"user_mode":"chrome","page_title":"A"}}
}}}`

// The reported bug at the HTTP layer: a tab set by one client must be visible
// to another client's GET, and the write must publish browser_tabs_changed so
// other clients refetch instead of polling.
func TestHandleBrowserTabsRoundTripAndPublishes(t *testing.T) {
	h := testBrowserTabsHandler(t)
	sub := h.bus.Subscribe(nil)
	defer h.bus.Unsubscribe(sub)

	if rr := putBrowserTabs(t, h, browserTabsOneProject); rr.Code != http.StatusOK {
		t.Fatalf("PUT status=%d body=%s", rr.Code, rr.Body.String())
	}
	select {
	case env := <-sub:
		if env.Event != "browser_tabs_changed" {
			t.Fatalf("event=%q want browser_tabs_changed", env.Event)
		}
	default:
		t.Fatal("no browser_tabs_changed event published")
	}

	got := getBrowserTabs(t, h)["/proj-a"]
	if len(got.Tabs) != 2 || got.Tabs[1].ManualTitle != "Mine" {
		t.Fatalf("tabs lost in round trip: %+v", got.Tabs)
	}
	if got.ActiveID != "b2" {
		t.Fatalf("active id = %q, want b2", got.ActiveID)
	}
	if s := got.Surfaces["b1"]; s.URL != "https://a.example/" || s.UserMode != "chrome" || s.PageTitle != "A" {
		t.Fatalf("surface lost in round trip: %+v", s)
	}
}

// A PUT naming one project must leave another client's project untouched.
func TestHandleBrowserTabsPartialPutPreservesOtherProjects(t *testing.T) {
	h := testBrowserTabsHandler(t)
	if rr := putBrowserTabs(t, h, browserTabsOneProject); rr.Code != http.StatusOK {
		t.Fatalf("seed PUT status=%d body=%s", rr.Code, rr.Body.String())
	}
	if rr := putBrowserTabs(t, h, `{"projects":{"/proj-b":{"tabs":[{"id":"x1","title":"X"}],"active_id":"x1","surfaces":{}}}}`); rr.Code != http.StatusOK {
		t.Fatalf("second PUT status=%d body=%s", rr.Code, rr.Body.String())
	}
	all := getBrowserTabs(t, h)
	if _, ok := all["/proj-a"]; !ok {
		t.Fatalf("PUT for /proj-b dropped /proj-a: keys=%d", len(all))
	}
	if _, ok := all["/proj-b"]; !ok {
		t.Fatal("/proj-b missing after its own PUT")
	}
}

// A remote project's key is `<host>::<path>` and must survive the HTTP layer
// byte-for-byte, or one project's strip would split across two keys.
func TestHandleBrowserTabsPreservesRemoteHostQualifiedKey(t *testing.T) {
	h := testBrowserTabsHandler(t)
	key := "james@217.216.72.49::/home/james/www/aimsai2"
	body := `{"projects":{"` + key + `":{"tabs":[{"id":"b1","title":"One"}],"active_id":"b1","surfaces":{}}}}`
	if rr := putBrowserTabs(t, h, body); rr.Code != http.StatusOK {
		t.Fatalf("PUT status=%d body=%s", rr.Code, rr.Body.String())
	}
	all := getBrowserTabs(t, h)
	if len(all) != 1 {
		t.Fatalf("stored %d keys, want 1", len(all))
	}
	if _, ok := all[key]; !ok {
		t.Fatalf("key was rewritten; got keys=%v", all)
	}
}

// A strip with no navigation state yet (a tab opened but never navigated) must
// read back with an empty surfaces object, never null, or the client's
// `surfaces[id]` lookup would throw.
func TestHandleBrowserTabsWithoutSurfacesReadsBackAsEmptyObject(t *testing.T) {
	h := testBrowserTabsHandler(t)
	if rr := putBrowserTabs(t, h, `{"projects":{"/p":{"tabs":[{"id":"b1","title":"New tab"}]}}}`); rr.Code != http.StatusOK {
		t.Fatalf("PUT status=%d body=%s", rr.Code, rr.Body.String())
	}
	rr := httptest.NewRecorder()
	h.HandleGetBrowserTabs(rr, httptest.NewRequest(http.MethodGet, "/api/browser-tabs", nil))
	if !strings.Contains(rr.Body.String(), `"surfaces": {}`) && !strings.Contains(rr.Body.String(), `"surfaces":{}`) {
		t.Fatalf("GET body has no empty surfaces object: %s", rr.Body.String())
	}
}

// An empty strip is the explicit "last tab closed" signal and deletes the
// project for every client.
func TestHandleBrowserTabsEmptyStripDeletesProject(t *testing.T) {
	h := testBrowserTabsHandler(t)
	if rr := putBrowserTabs(t, h, browserTabsOneProject); rr.Code != http.StatusOK {
		t.Fatalf("seed PUT status=%d body=%s", rr.Code, rr.Body.String())
	}
	if rr := putBrowserTabs(t, h, `{"projects":{"/proj-a":{"tabs":[],"surfaces":{},"revision":1}}}`); rr.Code != http.StatusOK {
		t.Fatalf("clear PUT status=%d body=%s", rr.Code, rr.Body.String())
	}
	if _, ok := getBrowserTabs(t, h)["/proj-a"]; ok {
		t.Fatal("empty strip did not delete /proj-a")
	}
}

// A write built on a stale revision is refused with 409 and writes nothing.
// The body carries the current state so the caller can re-merge and retry
// without a second GET.
func TestHandleBrowserTabsStaleRevisionReturns409WithProjects(t *testing.T) {
	h := testBrowserTabsHandler(t)
	if rr := putBrowserTabs(t, h, browserTabsOneProject); rr.Code != http.StatusOK {
		t.Fatalf("seed PUT status=%d body=%s", rr.Code, rr.Body.String())
	}
	rr := putBrowserTabs(t, h, `{"projects":{"/proj-a":{"tabs":[{"id":"x1","title":"X"}],"surfaces":{},"revision":0}}}`)
	if rr.Code != http.StatusConflict {
		t.Fatalf("stale PUT status=%d, want 409 (body=%s)", rr.Code, rr.Body.String())
	}
	var body struct {
		Error    string                                 `json:"error"`
		Projects map[string]browsertabs.ProjectBrowsers `json:"projects"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode 409 body: %v", err)
	}
	if body.Error == "" {
		t.Fatalf("409 body has no error message: %s", rr.Body.String())
	}
	got := body.Projects["/proj-a"]
	if got.Revision != 1 || len(got.Tabs) != 2 || got.ActiveID != "b2" {
		t.Fatalf("409 projects[/proj-a] = %+v, want the stored state at revision 1", got)
	}
}

// Invalid state is refused whole. A javascript: URL would otherwise reach the
// iframe on every client, and a silently repaired value would fight the
// sender's copy.
func TestHandleBrowserTabsRejectsInvalidPayloadWithoutWriting(t *testing.T) {
	cases := map[string]string{
		"javascript url":  `{"projects":{"/p":{"tabs":[{"id":"b1","title":"X"}],"surfaces":{"b1":{"url":"javascript:alert(1)","history":["javascript:alert(1)"],"history_index":0}}}}}`,
		"active id ghost": `{"projects":{"/p":{"tabs":[{"id":"b1","title":"X"}],"active_id":"ghost","surfaces":{}}}}`,
		"blank key":       `{"projects":{"  ":{"tabs":[{"id":"b1","title":"X"}],"surfaces":{}}}}`,
		"no projects":     `{}`,
		"not json":        `{"projects":`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			h := testBrowserTabsHandler(t)
			rr := putBrowserTabs(t, h, body)
			if rr.Code != http.StatusBadRequest {
				t.Fatalf("PUT status=%d, want 400 (body=%s)", rr.Code, rr.Body.String())
			}
			if len(getBrowserTabs(t, h)) != 0 {
				t.Fatal("rejected payload was partially written")
			}
		})
	}
}

// A payload that mixes a valid project with an invalid one must write neither.
func TestHandleBrowserTabsInvalidEntryRejectsWholeRequest(t *testing.T) {
	h := testBrowserTabsHandler(t)
	body := `{"projects":{"/good":{"tabs":[{"id":"b1","title":"X"}],"surfaces":{}},"/bad":{"tabs":[{"id":"b1","title":"X"}],"active_id":"ghost","surfaces":{}}}}`
	if rr := putBrowserTabs(t, h, body); rr.Code != http.StatusBadRequest {
		t.Fatalf("PUT status=%d, want 400", rr.Code)
	}
	if len(getBrowserTabs(t, h)) != 0 {
		t.Fatal("valid entry was written alongside an invalid one")
	}
}

// With no store (its data dir could not be resolved) the PUT must fail loudly
// rather than report success for state that was never kept.
func TestHandleBrowserTabsPutWithoutStoreFails(t *testing.T) {
	h := testHandlerWithConfig(t)
	h.browserTabsStore = nil
	if rr := putBrowserTabs(t, h, browserTabsOneProject); rr.Code != http.StatusInternalServerError {
		t.Fatalf("PUT status=%d, want 500", rr.Code)
	}
}
