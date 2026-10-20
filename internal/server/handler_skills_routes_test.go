package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/skill"
)

// The handler tests call HandleListManageableSkills / HandleSetSkillEnabled
// directly, which cannot catch a missing or shadowed ROUTE. That failure mode
// answers 200 with the SPA's HTML (the fallback), so the sidebar would parse a
// page as JSON and silently show nothing. Drive the real mux, like
// handler_quick_actions_routes_test.go.
func TestSkillRoutesAreRegistered(t *testing.T) {
	setHomeTree(t, t.TempDir())
	root := t.TempDir()
	writeServerSkill(t, root, "alpha")
	skill.InvalidateSkillCache()
	t.Cleanup(skill.InvalidateSkillCache)

	srv := New("127.0.0.1:0", "", "", nil)
	srv.handler.workDir = root
	h := srv.serveHandler()

	q := "?path=" + url.QueryEscape(root)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/skills/manage"+q, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/skills/manage = %d body=%s (404 means the route is missing)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("GET /api/skills/manage Content-Type = %q; an HTML body means the SPA fallback answered", ct)
	}
	var listed struct {
		Skills []skillManageEntry `json:"skills"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode manage listing: %v body=%s", err, rec.Body.String())
	}
	found := false
	for _, s := range listed.Skills {
		if s.Name == "alpha" {
			found = true
		}
	}
	if !found {
		t.Fatalf("manage listing missing the fixture skill: %+v", listed.Skills)
	}

	for _, action := range []string{"disable", "enable"} {
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/skills/alpha/"+action+q, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("PUT /api/skills/alpha/%s = %d body=%s (404 means the route is missing)", action, rec.Code, rec.Body.String())
		}
	}
}

// TestSkillRouteRejectsInvalidName proves the {name} path value is validated
// before it reaches the preference writer.
func TestSkillRouteRejectsInvalidName(t *testing.T) {
	setHomeTree(t, t.TempDir())
	root := t.TempDir()

	srv := New("127.0.0.1:0", "", "", nil)
	srv.handler.workDir = root
	h := srv.serveHandler()

	rec := httptest.NewRecorder()
	// %2E%2E is "..", which must be refused rather than resolved.
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/skills/%2E%2E/disable", nil))
	if rec.Code != http.StatusBadRequest && rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 400/404 (body %s)", rec.Code, rec.Body.String())
	}
}
