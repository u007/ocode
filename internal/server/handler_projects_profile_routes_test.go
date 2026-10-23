package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/projects"
)

// The handler-level tests in handler_remote_profile_test.go call ProfileFor on
// the store directly. Driving the real mux is what catches a missing or
// shadowed route, plus the body/validation contract the sidebar depends on.
func newProjectsProfileRouter(t *testing.T) (*Server, *projects.Store) {
	t.Helper()
	setHomeTree(t, t.TempDir())
	srv := New("127.0.0.1:0", "", "", nil)
	store, err := projects.NewStoreAt(filepath.Join(t.TempDir(), "projects.json"))
	if err != nil {
		t.Fatalf("projects.NewStoreAt: %v", err)
	}
	srv.handler.projects = store
	return srv, store
}

func postProjectProfile(t *testing.T, h http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/projects/profile", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rec, req)
	return rec
}

func TestProjectProfileRouteBindAndValidate(t *testing.T) {
	srv, store := newProjectsProfileRouter(t)
	if err := store.Add("/home/user/local"); err != nil {
		t.Fatal(err)
	}
	if err := store.AddRemote("devbox", "/home/user/remote"); err != nil {
		t.Fatal(err)
	}
	if err := config.SaveProfile("work", config.ProfileDelta{}); err != nil {
		t.Fatalf("SaveProfile: %v", err)
	}
	h := srv.serveHandler()

	// Bind a local project. A 404 here means the route is missing/shadowed.
	rec := postProjectProfile(t, h, `{"path":"/home/user/local","profile":"work"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("bind local = %d body=%s", rec.Code, rec.Body.String())
	}
	if got := store.ProfileFor(projects.ProjectRef{Path: "/home/user/local"}); got != "work" {
		t.Fatalf("local binding = %q, want work", got)
	}

	// Bind a remote project by host: the same path on another host must not
	// inherit it.
	rec = postProjectProfile(t, h, `{"path":"/home/user/remote","host":"devbox","profile":"work"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("bind remote = %d body=%s", rec.Code, rec.Body.String())
	}
	if got := store.ProfileFor(projects.ProjectRef{Host: "devbox", Path: "/home/user/remote"}); got != "work" {
		t.Fatalf("remote binding = %q, want work", got)
	}

	// An unknown profile is rejected before it can be stored.
	rec = postProjectProfile(t, h, `{"path":"/home/user/local","profile":"ghost"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown profile = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
	if got := store.ProfileFor(projects.ProjectRef{Path: "/home/user/local"}); got != "work" {
		t.Fatalf("binding changed by a rejected request: %q", got)
	}

	// Missing path / unknown project.
	if rec = postProjectProfile(t, h, `{"path":"","profile":"work"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("empty path = %d, want 400", rec.Code)
	}
	if rec = postProjectProfile(t, h, `{"path":"/nope","profile":"work"}`); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown project = %d, want 404", rec.Code)
	}

	// Clearing the binding is the empty profile, and it must publish so other
	// windows drop the tag.
	ch := srv.handler.bus.Subscribe(nil)
	defer srv.handler.bus.Unsubscribe(ch)
	rec = postProjectProfile(t, h, `{"path":"/home/user/local","profile":""}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("clear = %d body=%s", rec.Code, rec.Body.String())
	}
	if got := store.ProfileFor(projects.ProjectRef{Path: "/home/user/local"}); got != "" {
		t.Fatalf("binding after clear = %q, want empty", got)
	}

	var found bool
	for len(ch) > 0 {
		env := <-ch
		if env.Event != "profile.changed" {
			continue
		}
		var data map[string]string
		raw, err := json.Marshal(env.Data)
		if err != nil {
			t.Fatalf("marshal event data: %v", err)
		}
		if err := json.Unmarshal(raw, &data); err != nil {
			t.Fatalf("unmarshal event data: %v", err)
		}
		if data["action"] == "project_bound" && data["path"] == "/home/user/local" {
			found = true
		}
	}
	if !found {
		t.Fatal("no profile.changed/project_bound event published for the clear")
	}
}
