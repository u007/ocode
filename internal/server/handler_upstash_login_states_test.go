package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/u007/ocode/internal/boxsession"
	"github.com/u007/ocode/internal/config"
)

const loginStateTestCookieSecret = "s3cret-cookie-value"
const loginStateTestStorageSecret = "t0ken-storage-value"

// loginStateTestServer wires the login-state routes on a mux at a fresh home.
// The one project root the server allows is returned. The store is a temp dir.
func loginStateTestServer(t *testing.T) (*http.ServeMux, string, *boxsession.LoginStateStore) {
	t.Helper()
	setHomeTree(t, t.TempDir())

	store := boxsession.NewLoginStateStore(filepath.Join(t.TempDir(), "login-states"))
	prev := upstashLoginStoreFor
	upstashLoginStoreFor = func() (*boxsession.LoginStateStore, error) { return store, nil }
	t.Cleanup(func() { upstashLoginStoreFor = prev })

	project := t.TempDir()
	h := NewHandler()
	h.workDir = project

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/upstash/login-states", h.HandleUpstashLoginStatesList)
	mux.HandleFunc("DELETE /api/upstash/login-states/{name}", h.HandleUpstashLoginStateDelete)
	mux.HandleFunc("GET /api/upstash/login-state", h.HandleUpstashLoginStateGet)
	mux.HandleFunc("PUT /api/upstash/login-state", h.HandleUpstashLoginStateSet)
	return mux, project, store
}

func loginStateSave(t *testing.T, store *boxsession.LoginStateStore, name string) {
	t.Helper()
	st := boxsession.LoginState{
		Name:    name,
		SavedAt: time.Now(),
		Cookies: []json.RawMessage{json.RawMessage(`{"name":"sid","value":"` + loginStateTestCookieSecret + `"}`)},
		LocalStorage: map[string]map[string]string{
			"https://example.com": {"token": loginStateTestStorageSecret},
		},
	}
	if err := store.Save(st); err != nil {
		t.Fatalf("save %q: %v", name, err)
	}
}

func loginStateAssignment(t *testing.T, mux http.Handler, project string) string {
	t.Helper()
	code, raw := upstashDo(t, mux, http.MethodGet, "/api/upstash/login-state?project="+project, nil)
	if code != http.StatusOK {
		t.Fatalf("GET login-state: %d %s", code, raw)
	}
	return upstashDecode(t, raw)["name"].(string)
}

func TestLoginStateAssignSetGetClear(t *testing.T) {
	mux, project, _ := loginStateTestServer(t)

	if got := loginStateAssignment(t, mux, project); got != "" {
		t.Fatalf("unassigned project name = %q, want empty", got)
	}
	code, raw := upstashDo(t, mux, http.MethodPut, "/api/upstash/login-state",
		map[string]any{"project": project, "name": "work"})
	if code != http.StatusOK {
		t.Fatalf("PUT: %d %s", code, raw)
	}
	if got := upstashDecode(t, raw)["name"]; got != "work" {
		t.Fatalf("PUT echoed name = %v, want work", got)
	}
	if got := loginStateAssignment(t, mux, project); got != "work" {
		t.Fatalf("GET after PUT = %q, want work", got)
	}

	cfg, err := config.LoadUpstashBoxConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LoginStateFor(project) != "work" {
		t.Fatalf("persisted assignment = %v, want work", cfg.LoginStates)
	}

	code, raw = upstashDo(t, mux, http.MethodPut, "/api/upstash/login-state",
		map[string]any{"project": project, "name": "none"})
	if code != http.StatusOK || upstashDecode(t, raw)["name"] != "" {
		t.Fatalf("PUT none = %d %s, want 200 with empty name", code, raw)
	}
	if got := loginStateAssignment(t, mux, project); got != "" {
		t.Fatalf("GET after none = %q, want empty", got)
	}

	code, raw = upstashDo(t, mux, http.MethodPut, "/api/upstash/login-state",
		map[string]any{"project": project, "name": "work"})
	if code != http.StatusOK {
		t.Fatalf("re-assign: %d %s", code, raw)
	}
	code, _ = upstashDo(t, mux, http.MethodPut, "/api/upstash/login-state",
		map[string]any{"project": project, "name": ""})
	if code != http.StatusOK || loginStateAssignment(t, mux, project) != "" {
		t.Fatalf("PUT empty name did not clear the assignment (status %d)", code)
	}
}

// A name that is not a saved state yet is accepted: it is created on first save.
func TestLoginStateAssignUnsavedNameAccepted(t *testing.T) {
	mux, project, store := loginStateTestServer(t)
	code, raw := upstashDo(t, mux, http.MethodPut, "/api/upstash/login-state",
		map[string]any{"project": project, "name": "not-yet-saved"})
	if code != http.StatusOK {
		t.Fatalf("PUT unsaved name: %d %s", code, raw)
	}
	if _, ok, err := store.Load("not-yet-saved"); err != nil || ok {
		t.Fatalf("assigning created a state file: ok %v err %v", ok, err)
	}
}

func TestLoginStateProjectGuard(t *testing.T) {
	mux, project, _ := loginStateTestServer(t)
	foreign := filepath.Join(t.TempDir(), "elsewhere")

	if code, _ := upstashDo(t, mux, http.MethodGet, "/api/upstash/login-state", nil); code != http.StatusBadRequest {
		t.Fatalf("GET without project = %d, want 400", code)
	}
	if code, _ := upstashDo(t, mux, http.MethodGet, "/api/upstash/login-state?project="+foreign, nil); code != http.StatusForbidden {
		t.Fatalf("GET foreign project = %d, want 403", code)
	}
	if code, _ := upstashDo(t, mux, http.MethodPut, "/api/upstash/login-state",
		map[string]any{"name": "work"}); code != http.StatusBadRequest {
		t.Fatalf("PUT without project = %d, want 400", code)
	}
	if code, _ := upstashDo(t, mux, http.MethodPut, "/api/upstash/login-state",
		map[string]any{"project": foreign, "name": "work"}); code != http.StatusForbidden {
		t.Fatalf("PUT foreign project = %d, want 403", code)
	}
	cfg, err := config.LoadUpstashBoxConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.LoginStates) != 0 {
		t.Fatalf("refused PUT stored an assignment: %v", cfg.LoginStates)
	}
	if got := loginStateAssignment(t, mux, project); got != "" {
		t.Fatalf("allowed project gained an assignment from a refused PUT: %q", got)
	}
}

func TestLoginStateNameRules(t *testing.T) {
	mux, project, _ := loginStateTestServer(t)
	for _, name := range []string{"Bad Name", "../escape", "a/b", "x.lock", "-lead", strings.Repeat("a", 41)} {
		code, raw := upstashDo(t, mux, http.MethodPut, "/api/upstash/login-state",
			map[string]any{"project": project, "name": name})
		if code != http.StatusBadRequest {
			t.Errorf("PUT name %q = %d %s, want 400", name, code, raw)
		}
	}
	cfg, err := config.LoadUpstashBoxConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.LoginStates) != 0 {
		t.Fatalf("refused names were stored: %v", cfg.LoginStates)
	}
}

// The list carries counts and holders only. Cookie and storage values never
// appear in the response.
func TestLoginStateListCountsOnly(t *testing.T) {
	mux, _, store := loginStateTestServer(t)

	code, raw := upstashDo(t, mux, http.MethodGet, "/api/upstash/login-states", nil)
	if code != http.StatusOK || !strings.Contains(string(raw), `"login_states":[]`) {
		t.Fatalf("empty list = %d %s, want an empty array, not null", code, raw)
	}

	loginStateSave(t, store, "work")
	release, err := store.Acquire("work", "sess-live")
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	code, raw = upstashDo(t, mux, http.MethodGet, "/api/upstash/login-states", nil)
	if code != http.StatusOK {
		t.Fatalf("list: %d %s", code, raw)
	}
	if strings.Contains(string(raw), loginStateTestCookieSecret) || strings.Contains(string(raw), loginStateTestStorageSecret) {
		t.Fatalf("list response leaked stored values: %s", raw)
	}
	var out struct {
		LoginStates []struct {
			Name    string `json:"name"`
			SavedAt string `json:"saved_at"`
			Cookies int    `json:"cookies"`
			Origins int    `json:"origins"`
			Bytes   int64  `json:"bytes"`
			InUseBy string `json:"in_use_by"`
		} `json:"login_states"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.LoginStates) != 1 {
		t.Fatalf("list = %s, want one state", raw)
	}
	got := out.LoginStates[0]
	if got.Name != "work" || got.Cookies != 1 || got.Origins != 1 || got.Bytes <= 0 || got.SavedAt == "" {
		t.Fatalf("entry = %+v", got)
	}
	if got.InUseBy != "sess-live" {
		t.Fatalf("in_use_by = %q, want sess-live", got.InUseBy)
	}
}

func TestLoginStateDeleteRules(t *testing.T) {
	mux, project, store := loginStateTestServer(t)

	for _, name := range []string{"none", "Bad", "a.b"} {
		if code, _ := upstashDo(t, mux, http.MethodDelete, "/api/upstash/login-states/"+name, nil); code != http.StatusBadRequest {
			t.Errorf("DELETE %q = %d, want 400", name, code)
		}
	}
	if code, _ := upstashDo(t, mux, http.MethodDelete, "/api/upstash/login-states/gone", nil); code != http.StatusNoContent {
		t.Fatalf("DELETE of a missing state = %d, want 204", code)
	}

	loginStateSave(t, store, "work")
	if code, _ := upstashDo(t, mux, http.MethodPut, "/api/upstash/login-state",
		map[string]any{"project": project, "name": "work"}); code != http.StatusOK {
		t.Fatalf("assign: %d", code)
	}
	// A second project's assignment of the same state must also be cleared.
	// The config layer holds it; the server only allows the one root.
	if err := config.MutateUpstashBox(func(c *config.UpstashBoxConfig) error {
		return c.SetLoginState("/some/other/project", "work")
	}); err != nil {
		t.Fatal(err)
	}

	if code, raw := upstashDo(t, mux, http.MethodDelete, "/api/upstash/login-states/work", nil); code != http.StatusNoContent {
		t.Fatalf("DELETE: %d %s", code, raw)
	}
	if _, ok, err := store.Load("work"); err != nil || ok {
		t.Fatalf("state after delete: ok %v err %v", ok, err)
	}
	cfg, err := config.LoadUpstashBoxConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.LoginStates) != 0 {
		t.Fatalf("assignments survived the delete: %v", cfg.LoginStates)
	}
	if got := loginStateAssignment(t, mux, project); got != "" {
		t.Fatalf("project still assigned %q after delete", got)
	}
}

func TestLoginStateDeleteInUseIs409WithHolder(t *testing.T) {
	mux, project, store := loginStateTestServer(t)
	loginStateSave(t, store, "busy")
	if code, _ := upstashDo(t, mux, http.MethodPut, "/api/upstash/login-state",
		map[string]any{"project": project, "name": "busy"}); code != http.StatusOK {
		t.Fatalf("assign: %d", code)
	}
	release, err := store.Acquire("busy", "sess-held")
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	code, raw := upstashDo(t, mux, http.MethodDelete, "/api/upstash/login-states/busy", nil)
	if code != http.StatusConflict {
		t.Fatalf("DELETE in use = %d %s, want 409", code, raw)
	}
	body := upstashDecode(t, raw)
	if body["in_use_by"] != "sess-held" {
		t.Fatalf("409 body = %s, want in_use_by sess-held", raw)
	}
	if _, ok, err := store.Load("busy"); err != nil || !ok {
		t.Fatalf("a refused delete removed the state: ok %v err %v", ok, err)
	}
	if got := loginStateAssignment(t, mux, project); got != "busy" {
		t.Fatalf("a refused delete cleared the assignment: %q", got)
	}
}

// TestLoginStateListReportsCorruptFileForDeletion: one unreadable state file does not
// fail the list. It is listed with corrupt true and zero counts, and DELETE removes it.
func TestLoginStateListReportsCorruptFileForDeletion(t *testing.T) {
	mux, _, _ := loginStateTestServer(t)
	dir := filepath.Join(t.TempDir(), "login-states")
	store := boxsession.NewLoginStateStore(dir)
	upstashLoginStoreFor = func() (*boxsession.LoginStateStore, error) { return store, nil }
	loginStateSave(t, store, "good")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bad.json"), []byte(`{"cookies": [`+loginStateTestCookieSecret), 0o600); err != nil {
		t.Fatal(err)
	}

	code, raw := upstashDo(t, mux, http.MethodGet, "/api/upstash/login-states", nil)
	if code != http.StatusOK {
		t.Fatalf("list with one corrupt file = %d %s, want 200", code, raw)
	}
	if strings.Contains(string(raw), loginStateTestCookieSecret) {
		t.Fatalf("list response echoed the corrupt file's bytes: %s", raw)
	}
	var out struct {
		LoginStates []struct {
			Name    string `json:"name"`
			Cookies int    `json:"cookies"`
			Corrupt bool   `json:"corrupt"`
		} `json:"login_states"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	byName := map[string]int{}
	for _, v := range out.LoginStates {
		byName[v.Name] = v.Cookies
		if v.Name == "good" && v.Corrupt {
			t.Fatal("a readable state is marked corrupt")
		}
		if v.Name == "bad" && (!v.Corrupt || v.Cookies != 0) {
			t.Fatalf("the corrupt file's entry = %+v, want corrupt with zero counts", v)
		}
	}
	if _, ok := byName["bad"]; !ok || byName["good"] != 1 {
		t.Fatalf("list = %s, want good (1 cookie) and bad (corrupt)", raw)
	}

	code, raw = upstashDo(t, mux, http.MethodDelete, "/api/upstash/login-states/bad", nil)
	if code != http.StatusNoContent {
		t.Fatalf("delete of the corrupt file = %d %s, want 204", code, raw)
	}
	if _, err := os.Stat(filepath.Join(dir, "bad.json")); !os.IsNotExist(err) {
		t.Fatalf("the corrupt file still exists after delete (stat err %v)", err)
	}
}
