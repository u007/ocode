package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/projects"
)

func testProjectHandler(t *testing.T) *Handler {
	t.Helper()
	h := testHandlerWithConfig(t)
	store, err := projects.NewStoreAt(t.TempDir() + "/projects.json")
	if err != nil {
		t.Fatalf("projects.NewStoreAt: %v", err)
	}
	h.projects = store
	return h
}

func postJSON(t *testing.T, h *Handler, fn func(http.ResponseWriter, *http.Request), body string) *httptest.ResponseRecorder {
	t.Helper()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/projects/x", strings.NewReader(body))
	fn(rr, req)
	return rr
}

func projectByRef(t *testing.T, h *Handler, host, path string) projects.Project {
	t.Helper()
	for _, p := range h.projects.List() {
		if p.Host == host && p.Path == path {
			return p
		}
	}
	t.Fatalf("project host=%q path=%q not found in %+v", host, path, h.projects.List())
	return projects.Project{}
}

// Rename with host scopes to the remote entry and leaves a same-path
// local entry untouched.
func TestHandleRenameProjectRemoteScoped(t *testing.T) {
	h := testProjectHandler(t)
	if err := h.projects.Add("/home/user/app"); err != nil {
		t.Fatal(err)
	}
	if err := h.projects.AddRemote("devbox", "/home/user/app"); err != nil {
		t.Fatal(err)
	}

	rr := postJSON(t, h, h.HandleRenameProject, `{"path":"/home/user/app","host":"devbox","name":"renamed-ssh"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", rr.Code, rr.Body.String())
	}
	if got := projectByRef(t, h, "devbox", "/home/user/app"); got.Name != "renamed-ssh" {
		t.Fatalf("remote name = %q, want renamed-ssh", got.Name)
	}
	if got := projectByRef(t, h, "", "/home/user/app"); got.Name == "renamed-ssh" {
		t.Fatalf("local entry was renamed: %+v", got)
	}
}

// Rename a WSL entry by verbatim path.
func TestHandleRenameProjectWSL(t *testing.T) {
	h := testProjectHandler(t)
	if err := h.projects.AddRemote("wsl:Ubuntu", `C:\Users\james\app`); err != nil {
		t.Fatal(err)
	}

	rr := postJSON(t, h, h.HandleRenameProject, `{"path":"C:\\Users\\james\\app","host":"wsl:Ubuntu","name":"wsl-app"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", rr.Code, rr.Body.String())
	}
	got := projectByRef(t, h, "wsl:Ubuntu", `C:\Users\james\app`)
	if got.Name != "wsl-app" {
		t.Fatalf("WSL name = %q, want wsl-app", got.Name)
	}
}

// Legacy rename without host still 404s for a remote-only path string
// (never silently renames the remote entry).
func TestHandleRenameProjectLegacyStillLocalOnly(t *testing.T) {
	h := testProjectHandler(t)
	if err := h.projects.AddRemote("devbox", "/home/user/app"); err != nil {
		t.Fatal(err)
	}

	rr := postJSON(t, h, h.HandleRenameProject, `{"path":"/home/user/app","name":"nope"}`)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rr.Code)
	}
}

// SetGroup with host scopes to the remote entry.
func TestHandleSetProjectGroupRemoteScoped(t *testing.T) {
	h := testProjectHandler(t)
	if err := h.projects.Add("/home/user/app"); err != nil {
		t.Fatal(err)
	}
	if err := h.projects.AddRemote("devbox", "/home/user/app"); err != nil {
		t.Fatal(err)
	}

	rr := postJSON(t, h, h.HandleSetProjectGroup, `{"path":"/home/user/app","host":"devbox","group":"g"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", rr.Code, rr.Body.String())
	}
	if got := projectByRef(t, h, "devbox", "/home/user/app"); got.Group != "g" {
		t.Fatalf("remote group = %q, want g", got.Group)
	}
	if got := projectByRef(t, h, "", "/home/user/app"); got.Group != "" {
		t.Fatalf("local entry was grouped: %+v", got)
	}
}

// Reorder with scoped refs orders same-path entries per host.
func TestHandleReorderProjectsScopedRefs(t *testing.T) {
	h := testProjectHandler(t)
	if err := h.projects.Add("/home/user/local"); err != nil {
		t.Fatal(err)
	}
	if err := h.projects.AddRemote("devbox", "/home/user/app"); err != nil {
		t.Fatal(err)
	}
	if err := h.projects.AddRemote("wsl:Ubuntu", "/home/user/app"); err != nil {
		t.Fatal(err)
	}

	body := `{"projects":[{"path":"/home/user/app","host":"wsl:Ubuntu"},{"path":"/home/user/local"},{"path":"/home/user/app","host":"devbox"}]}`
	rr := postJSON(t, h, h.HandleReorderProjects, body)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", rr.Code, rr.Body.String())
	}
	orders := map[string]int{}
	for _, p := range h.projects.List() {
		orders[p.Host+"\x00"+p.Path] = p.Order
	}
	if orders["wsl:Ubuntu\x00/home/user/app"] != 1 ||
		orders["\x00/home/user/local"] != 2 ||
		orders["devbox\x00/home/user/app"] != 3 {
		t.Fatalf("orders = %+v, want wsl=1 local=2 devbox=3", orders)
	}
}

// Legacy reorder payload still works for local entries.
func TestHandleReorderProjectsLegacyPaths(t *testing.T) {
	h := testProjectHandler(t)
	if err := h.projects.Add("/a"); err != nil {
		t.Fatal(err)
	}
	if err := h.projects.Add("/b"); err != nil {
		t.Fatal(err)
	}

	rr := postJSON(t, h, h.HandleReorderProjects, `{"paths":["/b","/a"]}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", rr.Code, rr.Body.String())
	}
	list := h.projects.List()
	if len(list) != 2 || list[0].Path != "/b" || list[1].Path != "/a" {
		raw, _ := json.Marshal(list)
		t.Fatalf("order = %s, want [/b /a]", raw)
	}
}
