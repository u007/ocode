package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/projects"
	"github.com/u007/ocode/internal/session"
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

func TestHandleUpdateRemoteProjectSSH(t *testing.T) {
	h := testProjectHandler(t)
	if err := h.projects.AddRemote("alice@old.example", "/srv/app"); err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/api/projects/remote", strings.NewReader(`{"old_host":"alice@old.example","old_path":"/srv/app","kind":"ssh","user":"bob","host":"new.example","port":2222,"path":"/srv/new"}`))
	h.HandleUpdateRemoteProject(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	got := projectByRef(t, h, "bob@new.example", "/srv/new")
	if got.RemotePort != 2222 || got.RemoteUser != "bob" {
		t.Fatalf("updated = %+v", got)
	}
}

func TestHandleUpdateRemoteProjectRejectsInvalidWSLPort(t *testing.T) {
	h := testProjectHandler(t)
	if err := h.projects.AddRemote("wsl:Ubuntu", "/home/app"); err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/api/projects/remote", strings.NewReader(`{"old_host":"wsl:Ubuntu","old_path":"/home/app","kind":"wsl","distro":"Debian","port":22,"path":"/home/app"}`))
	h.HandleUpdateRemoteProject(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}
	if _, ok := func() (projects.Project, bool) {
		for _, p := range h.projects.List() {
			if p.Host == "wsl:Ubuntu" {
				return p, true
			}
		}
		return projects.Project{}, false
	}(); !ok {
		t.Fatal("original project was changed")
	}
}

// TestProjectHostFor locks the environment-prompt remote-project wiring:
// projectHostFor must resolve a registered remote (SSH/WSL) project's host so
// buildAgentSession can stamp it on the agent, and return "" for local
// projects, unknown roots, and the empty root. Without this the <env> block
// described a remote project root using the local machine's config/session/
// skill paths with no indication they belong to different machines.
func TestProjectHostFor(t *testing.T) {
	h := testProjectHandler(t)
	if err := h.projects.Add("/home/user/local"); err != nil {
		t.Fatal(err)
	}
	if err := h.projects.AddRemote("devbox", "/home/user/app"); err != nil {
		t.Fatal(err)
	}
	if err := h.projects.AddRemote("wsl:Ubuntu", `C:\Users\james\win`); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		root string
		want string
	}{
		{"local project", "/home/user/local", ""},
		{"remote ssh", "/home/user/app", "devbox"},
		{"remote wsl", `C:\Users\james\win`, "wsl:Ubuntu"},
		{"unknown root", "/nope", ""},
		{"empty root", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := h.projectHostFor(tc.root); got != tc.want {
				t.Fatalf("projectHostFor(%q) = %q, want %q", tc.root, got, tc.want)
			}
		})
	}
}

// HandleAddProject expands ~ in the LOCAL branch so the project is stored
// with the resolved absolute path.
func TestHandleAddProjectExpandHome(t *testing.T) {
	h := testProjectHandler(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	// Create the target directory so any future existence check would pass.
	if err := os.MkdirAll(filepath.Join(home, "webapp"), 0o755); err != nil {
		t.Fatal(err)
	}

	rr := postJSON(t, h, h.HandleAddProject, `{"path":"~/webapp"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", rr.Code, rr.Body.String())
	}

	// The stored path must be expanded, not literal ~.
	want := filepath.Join(home, "webapp")
	got := projectByRef(t, h, "", want)
	if got.Path != want {
		t.Errorf("stored path = %q, want %q", got.Path, want)
	}
	if got.Name != "webapp" {
		t.Errorf("stored name = %q, want webapp", got.Name)
	}
}

// HandleAddProject does NOT expand ~ when a host is present (R3).
func TestHandleAddProjectRemoteTildeNotExpanded(t *testing.T) {
	h := testProjectHandler(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	rr := postJSON(t, h, h.HandleAddProject, `{"host":"devbox","path":"~/webapp"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", rr.Code, rr.Body.String())
	}
	// Remote path must be stored VERBATIM — no expansion.
	got := projectByRef(t, h, "devbox", "~/webapp")
	if got.Path != "~/webapp" {
		t.Errorf("stored path = %q, want ~/webapp", got.Path)
	}
}

// A tilde-keyed remote project must still list its sessions on the host.
//
// The desktop registers the project remotely VERBATIM ("~/www/aimsai2"), so the
// request it later proxies to the host's `ocode serve --remote` carries the
// tilde form. That host saved the project through the LOCAL branch of
// HandleAddProject, which expands "~" (TestHandleAddProjectExpandHome), so its
// registry holds "/home/james/www/aimsai2". An exact-match gate therefore 404s
// every tilde-keyed remote project and the session list never loads.
//
// Reproduces the live failure: GET
// /api/remote/james@217.216.72.49/api/projects/sessions?path=~/www/aimsai2
// returned {"error":"project not found in saved list"} while the host reported
// the project as /home/james/www/aimsai2.
func TestHandleListProjectSessionsResolvesTildeProject(t *testing.T) {
	h := testProjectHandler(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	// The host's registry holds the expanded form, exactly as HandleAddProject
	// would have written it.
	want := filepath.Join(home, "www", "aimsai2")
	if err := h.projects.Add(want); err != nil {
		t.Fatal(err)
	}

	// A real session on disk under the EXPANDED path. Sessions were created with
	// that cwd, so this is the only spelling ListRefsForDir can find them by.
	id := session.NewSessionID()
	if err := session.SaveForDir(want, id, "Real remote chat", []agent.Message{
		{Role: "user", Content: "hi"},
	}, nil); err != nil {
		t.Fatalf("save: %v", err)
	}

	// The proxied request carries the verbatim tilde form the desktop registered.
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/projects/sessions?path="+url.QueryEscape("~/www/aimsai2"), nil)
	h.HandleListProjectSessions(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200 (a tilde-keyed remote project must not 404)", rr.Code, rr.Body.String())
	}
	var got []SessionInfo
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v (body %s)", err, rr.Body.String())
	}
	if got == nil {
		t.Fatal("body decoded to nil, want an array so an empty project reads as \"No sessions yet\"")
	}

	// The session must actually be LISTED, not merely 200-with-nothing. A fix
	// that resolved the path only to satisfy the registry gate — then scanned the
	// raw "~/..." string — would pass a status-only assertion and still leave the
	// user staring at an empty list, which is the same bug in a new disguise.
	found := false
	for _, s := range got {
		if s.ID == id {
			found = true
			if s.Title != "Real remote chat" {
				t.Errorf("title = %q, want %q", s.Title, "Real remote chat")
			}
		}
	}
	if !found {
		t.Errorf("session %s not listed; got %+v — the RESOLVED path must be the one scanned", id, got)
	}
}

// The tilde fallback must NOT widen the accepted set: an unsaved path is still
// rejected, and a "~user" form (which ExpandHome leaves alone) is not guessed at.
func TestHandleListProjectSessionsTildeFallbackStillRequiresSavedProject(t *testing.T) {
	h := testProjectHandler(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	for _, tc := range []struct{ name, query string }{
		{"unsaved tilde path", "~/www/never-added"},
		{"tilde-user form is not expanded", "~bob/www/app"},
		{"unsaved absolute path", "/definitely/not/a/saved/project"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/api/projects/sessions?path="+url.QueryEscape(tc.query), nil)
			h.HandleListProjectSessions(rr, req)
			if rr.Code != http.StatusNotFound {
				t.Errorf("status = %d, body = %s, want 404 — the fallback must not accept unsaved roots", rr.Code, rr.Body.String())
			}
		})
	}
}

// Duplicating a local project as remote creates a new (host, path) entry that

// Duplicating a local project as remote creates a new (host, path) entry that
// inherits the display name and group, leaving the local source untouched.
func TestHandleDuplicateProjectAsRemoteInheritsNameAndGroup(t *testing.T) {
	h := testProjectHandler(t)
	if err := h.projects.Add("/home/user/app"); err != nil {
		t.Fatal(err)
	}
	if err := h.projects.RenameRef(projects.ProjectRef{Path: "/home/user/app"}, "My App"); err != nil {
		t.Fatal(err)
	}
	if err := h.projects.SetGroupRef(projects.ProjectRef{Path: "/home/user/app"}, "work"); err != nil {
		t.Fatal(err)
	}

	rr := postJSON(t, h, h.HandleDuplicateProjectAsRemote, `{"host":"devbox","path":"/home/user/app","name":"My App","group":"work"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", rr.Code, rr.Body.String())
	}
	var created projects.Project
	if err := json.Unmarshal(rr.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode response: %v (body %s)", err, rr.Body.String())
	}
	if created.Host != "devbox" || created.Path != "/home/user/app" {
		t.Fatalf("created identity = %q:%q, want devbox:/home/user/app", created.Host, created.Path)
	}
	if created.Name != "My App" || created.Group != "work" {
		t.Fatalf("created name/group = %q/%q, want My App/work", created.Name, created.Group)
	}

	got := projectByRef(t, h, "devbox", "/home/user/app")
	if got.Name != "My App" || got.Group != "work" {
		t.Fatalf("stored duplicate = %+v, want name My App in group work", got)
	}
	if local := projectByRef(t, h, "", "/home/user/app"); local.Host != "" {
		t.Fatalf("local source changed: %+v", local)
	}
}

// Duplicating onto a target that is already saved is a 409 conflict rather
// than an upsert (that is HandleAddProject's behavior).
func TestHandleDuplicateProjectAsRemoteConflict(t *testing.T) {
	h := testProjectHandler(t)
	if err := h.projects.AddRemote("devbox", "/home/user/app"); err != nil {
		t.Fatal(err)
	}

	rr := postJSON(t, h, h.HandleDuplicateProjectAsRemote, `{"host":"devbox","path":"/home/user/app"}`)
	if rr.Code != http.StatusConflict {
		t.Fatalf("status = %d, body = %s, want 409", rr.Code, rr.Body.String())
	}
	if got := len(h.projects.List()); got != 1 {
		t.Fatalf("list length = %d, want 1 (no entry added on conflict)", got)
	}
}

// Missing host/path and malformed targets are rejected before any write.
func TestHandleDuplicateProjectAsRemoteValidation(t *testing.T) {
	h := testProjectHandler(t)

	cases := []struct {
		name string
		body string
		want int
	}{
		{"missing host", `{"path":"/p"}`, http.StatusBadRequest},
		{"missing path", `{"host":"devbox"}`, http.StatusBadRequest},
		{"invalid target", `{"host":"bad/host","path":"/p"}`, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rr := postJSON(t, h, h.HandleDuplicateProjectAsRemote, tc.body)
			if rr.Code != tc.want {
				t.Fatalf("status = %d, body = %s, want %d", rr.Code, rr.Body.String(), tc.want)
			}
		})
	}
	if got := len(h.projects.List()); got != 0 {
		t.Fatalf("list length = %d, want 0 (no entry added on validation failure)", got)
	}
}

// The duplicate endpoint must resolve through the server mux, not only the
// handler method: a handler that exists but was never registered in
// registerRoutes would 404/405 the client (the models-favorite routes had
// exactly that bug).
func TestDuplicateProjectAsRemoteRouteRegistered(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	s := New("localhost:0", "", "", nil)
	store, err := projects.NewStoreAt(t.TempDir() + "/projects.json")
	if err != nil {
		t.Fatalf("projects.NewStoreAt: %v", err)
	}
	s.handler.projects = store

	w := httptest.NewRecorder()
	r := httptest.NewRequest(
		http.MethodPost,
		"/api/projects/duplicate",
		strings.NewReader(`{"host":"devbox","path":"/home/user/app","name":"My App","group":"work"}`),
	)
	s.mux.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("POST /api/projects/duplicate status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	if got := len(store.List()); got != 1 {
		t.Fatalf("list length = %d, want 1 (route did not reach the handler)", got)
	}
}
