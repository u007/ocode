package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/tabs"
)

func testTabsHandler(t *testing.T) *Handler {
	t.Helper()
	h := testHandlerWithConfig(t)
	store, err := tabs.NewStoreAt(filepath.Join(t.TempDir(), "tabs.json"))
	if err != nil {
		t.Fatalf("tabs.NewStoreAt: %v", err)
	}
	h.tabsStore = store
	return h
}

// A bulk PUT of every project's tabs (the shape the web store persists) must
// round-trip through a bulk GET with sub-tab and active preserved, and must
// fan a tabs_changed envelope onto the bus so other windows refetch.
func TestHandleTabsBulkRoundTrip(t *testing.T) {
	h := testTabsHandler(t)
	sub := h.bus.Subscribe(nil)
	defer h.bus.Unsubscribe(sub)

	body := `{"projects":{"/proj-a":{"tabs":[{"id":"s1","title":"One","sub_tab":"changes"},{"id":"s2","title":"Two"}],"active":"s2"}}}`
	rr := httptest.NewRecorder()
	h.HandleSetTabs(rr, httptest.NewRequest(http.MethodPut, "/api/tabs", strings.NewReader(body)))
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT status=%d body=%s", rr.Code, rr.Body.String())
	}

	select {
	case env := <-sub:
		if env.Event != "tabs_changed" {
			t.Fatalf("event=%q want tabs_changed", env.Event)
		}
	default:
		t.Fatal("no tabs_changed event published")
	}

	rr = httptest.NewRecorder()
	h.HandleGetTabs(rr, httptest.NewRequest(http.MethodGet, "/api/tabs", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET status=%d body=%s", rr.Code, rr.Body.String())
	}
	var got struct {
		Projects map[string]tabs.ProjectTabs `json:"projects"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	pt, ok := got.Projects["/proj-a"]
	if !ok {
		t.Fatalf("project missing from %+v", got.Projects)
	}
	if pt.Active != "s2" || len(pt.Tabs) != 2 || pt.Tabs[0].SubTab != "changes" || pt.Tabs[1].Title != "Two" {
		t.Fatalf("unexpected tabs: %+v", pt)
	}
}

// A bulk PUT is a full replacement: projects absent from the body are
// dropped, so closing every tab of a project in one window clears it for all.
func TestHandleTabsBulkReplacesAll(t *testing.T) {
	h := testTabsHandler(t)
	put := func(body string) {
		t.Helper()
		rr := httptest.NewRecorder()
		h.HandleSetTabs(rr, httptest.NewRequest(http.MethodPut, "/api/tabs", strings.NewReader(body)))
		if rr.Code != http.StatusOK {
			t.Fatalf("PUT status=%d body=%s", rr.Code, rr.Body.String())
		}
	}
	put(`{"projects":{"/a":{"tabs":[{"id":"s1","title":"x"}],"active":"s1"},"/b":{"tabs":[{"id":"s2","title":"y"}],"active":"s2"}}}`)
	put(`{"projects":{"/a":{"tabs":[{"id":"s1","title":"x"}],"active":"s1"}}}`)

	all := h.tabsStore.All()
	if _, ok := all["/b"]; ok {
		t.Fatalf("/b should have been dropped: %+v", all)
	}
	if len(all) != 1 {
		t.Fatalf("want 1 project, got %+v", all)
	}
}

// Tabs without an id are dropped from a bulk PUT (same rule as the per-path
// form), and a project left with no tabs is not stored at all.
func TestHandleTabsBulkDropsEmpty(t *testing.T) {
	h := testTabsHandler(t)
	rr := httptest.NewRecorder()
	body := `{"projects":{"/a":{"tabs":[{"id":"","title":"x"}],"active":""},"/b":{"tabs":[{"id":"s2","title":"y"},{"id":"","title":"z"}],"active":"s2"}}}`
	h.HandleSetTabs(rr, httptest.NewRequest(http.MethodPut, "/api/tabs", strings.NewReader(body)))
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT status=%d body=%s", rr.Code, rr.Body.String())
	}
	all := h.tabsStore.All()
	if _, ok := all["/a"]; ok {
		t.Fatalf("/a with no valid tabs should not be stored: %+v", all)
	}
	if len(all["/b"].Tabs) != 1 {
		t.Fatalf("/b should keep one tab: %+v", all["/b"])
	}
}
