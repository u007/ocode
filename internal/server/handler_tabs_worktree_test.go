package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/tabs"
)

// Legacy tabs written before worktree_path existed must still load and
// round-trip, with an empty worktree path.
func TestHandleTabsLegacyWithoutWorktreePath(t *testing.T) {
	h := testTabsHandler(t)
	body := `{"projects":{"/proj-a":{"tabs":[{"id":"s1","title":"One"}],"active":"s1"}}}`
	rr := httptest.NewRecorder()
	h.HandleSetTabs(rr, httptest.NewRequest(http.MethodPut, "/api/tabs", strings.NewReader(body)))
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT status=%d body=%s", rr.Code, rr.Body.String())
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
	tabsForProject := got.Projects["/proj-a"].Tabs
	if len(tabsForProject) != 1 {
		t.Fatalf("legacy project tabs = %+v, want one tab", got.Projects)
	}
	tab := tabsForProject[0]
	if tab.ID != "s1" || tab.WorktreePath != "" {
		t.Fatalf("legacy tab = %+v, want id s1 and empty worktree path", tab)
	}
}

// A tab's linked-worktree checkout must persist through the bulk and per-path
// API shapes.
func TestHandleTabsWorktreePathRoundTrip(t *testing.T) {
	h := testTabsHandler(t)
	body := `{"projects":{"/proj-a":{"tabs":[{"id":"s1","title":"One","worktree_path":"/proj-a/.worktrees/feat"}],"active":"s1"}}}`
	rr := httptest.NewRecorder()
	h.HandleSetTabs(rr, httptest.NewRequest(http.MethodPut, "/api/tabs", strings.NewReader(body)))
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT status=%d body=%s", rr.Code, rr.Body.String())
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
	tabsForProject := got.Projects["/proj-a"].Tabs
	if len(tabsForProject) != 1 {
		t.Fatalf("bulk project tabs = %+v, want one tab", got.Projects)
	}
	if tabsForProject[0].WorktreePath != "/proj-a/.worktrees/feat" {
		t.Fatalf("bulk round-trip worktree_path = %q", tabsForProject[0].WorktreePath)
	}

	// The per-project shape (single path write, then read) must carry it too.
	single := `{"path":"/proj-a","tabs":[{"id":"s1","title":"One","worktree_path":"/proj-a/.worktrees/feat"}],"active":"s1"}`
	rr = httptest.NewRecorder()
	h.HandleSetTabs(rr, httptest.NewRequest(http.MethodPut, "/api/tabs", strings.NewReader(single)))
	if rr.Code != http.StatusOK {
		t.Fatalf("single PUT status=%d body=%s", rr.Code, rr.Body.String())
	}
	rr = httptest.NewRecorder()
	h.HandleGetTabs(rr, httptest.NewRequest(http.MethodGet, "/api/tabs?path=%2Fproj-a", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("single GET status=%d body=%s", rr.Code, rr.Body.String())
	}
	var one tabs.ProjectTabs
	if err := json.Unmarshal(rr.Body.Bytes(), &one); err != nil {
		t.Fatalf("decode single: %v", err)
	}
	if len(one.Tabs) != 1 || one.Tabs[0].WorktreePath != "/proj-a/.worktrees/feat" {
		t.Fatalf("single round-trip = %+v", one)
	}
}
