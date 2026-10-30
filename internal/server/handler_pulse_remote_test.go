//go:build !windows

package server

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/u007/ocode/internal/termtabs"
)

const (
	pulseRemoteTestHost = "dev@box"
	pulseRemoteTestPath = "/home/me/app"
	pulseRemoteTestTerm = "rt-1"
)

// pulseRemoteFake is a remote host's own ocode server, reduced to the three
// endpoints the Pulse tools call: project registration, the live terminal list
// and the history log.
type pulseRemoteFake struct {
	srv *httptest.Server
	ws  *fakeTestWorkspace

	mu         sync.Mutex
	log        []byte
	cols, rows uint16
	registered []string
	badAuth    int
	histories  int
}

func newPulseRemoteFake(t *testing.T, log string, cols, rows uint16) *pulseRemoteFake {
	t.Helper()
	f := &pulseRemoteFake{log: []byte(log), cols: cols, rows: rows}
	mux := http.NewServeMux()
	auth := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer tok-remote" {
				f.mu.Lock()
				f.badAuth++
				f.mu.Unlock()
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}
			h(w, r)
		}
	}
	mux.HandleFunc("POST /api/projects", auth(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Path string `json:"path"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.registered = append(f.registered, body.Path)
		f.mu.Unlock()
		fmt.Fprint(w, `{"ok":true}`)
	}))
	mux.HandleFunc("GET /api/pulse/terminals", auth(func(w http.ResponseWriter, r *http.Request) {
		row := pulseTerminalRow{ID: pulseRemoteTestTerm, Project: pulseRemoteTestPath, PID: 7, Command: "claude", Running: true, Cols: f.cols, Rows: f.rows}
		_ = json.NewEncoder(w).Encode(map[string]any{"terminals": []pulseTerminalRow{row}, "total": 1})
	}))
	mux.HandleFunc("GET /api/terminal/{id}/history", auth(func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("id") != pulseRemoteTestTerm || r.URL.Query().Get("project_path") != pulseRemoteTestPath {
			http.Error(w, `{"error":"no terminal history"}`, http.StatusNotFound)
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		f.histories++
		offset, _ := strconv.ParseInt(r.URL.Query().Get("offset"), 10, 64)
		limit, _ := strconv.ParseInt(r.URL.Query().Get("limit"), 10, 64)
		end := int64(len(f.log))
		if offset > end {
			http.Error(w, `{"error":"offset is past the history snapshot"}`, http.StatusRequestedRangeNotSatisfiable)
			return
		}
		stop := min(offset+limit, end)
		_ = json.NewEncoder(w).Encode(terminalHistoryResponse{
			ID: pulseRemoteTestTerm, Offset: offset, NextOffset: stop, SnapshotEnd: end,
			EOF: stop >= end, Data: base64.StdEncoding.EncodeToString(f.log[offset:stop]), State: "active",
		})
	}))
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	f.ws = &fakeTestWorkspace{apiURL: f.srv.URL, token: "tok-remote"}
	return f
}

// pulseRemoteTestHandler is a handler with one local project, one remote project
// on pulseRemoteTestHost, a terminal-tab store, and the fake host connected.
func pulseRemoteTestHandler(t *testing.T, f *pulseRemoteFake) *Handler {
	t.Helper()
	h := newTestProxyHandler(t, pulseRemoteTestHost, pulseRemoteTestPath, f.ws)
	localRoot := t.TempDir()
	if err := h.projects.Add(localRoot); err != nil {
		t.Fatalf("add local project: %v", err)
	}
	h.terminalLoopback = true
	store, err := termtabs.NewStoreAt(filepath.Join(t.TempDir(), "term.json"))
	if err != nil {
		t.Fatal(err)
	}
	h.termTabsStore = store
	if err := store.Set(pulseRemoteTestHost+"::"+pulseRemoteTestPath, termtabs.ProjectTerminals{
		Terminals: []termtabs.Terminal{{ID: pulseRemoteTestTerm, Title: "claude"}},
	}); err != nil {
		t.Fatal(err)
	}
	if f != nil {
		injectProxy(t, h.remoteHosts, pulseRemoteTestHost, f.ws)
	}
	return h
}

func TestProjectListIncludesRemoteProjectsAndConnectionState(t *testing.T) {
	f := newPulseRemoteFake(t, "", 80, 24)
	h := pulseRemoteTestHandler(t, f)
	if err := h.projects.AddRemote("other@host", "/srv/x"); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Projects []pulseProjectEntry `json:"projects"`
	}
	if err := json.Unmarshal([]byte(mustPulseTool(t, h, "project_list", `{}`)), &got); err != nil {
		t.Fatal(err)
	}
	byHost := map[string]pulseProjectEntry{}
	local := 0
	for _, p := range got.Projects {
		if p.Host == "" {
			local++
			if p.Connected != nil {
				t.Fatalf("local project must not carry a connection state: %+v", p)
			}
			continue
		}
		byHost[p.Host] = p
	}
	if local != 1 || len(got.Projects) != 3 {
		t.Fatalf("want 1 local + 2 remote projects, got %+v", got.Projects)
	}
	if c := byHost[pulseRemoteTestHost].Connected; c == nil || !*c {
		t.Fatalf("%s is connected: %+v", pulseRemoteTestHost, byHost[pulseRemoteTestHost])
	}
	if c := byHost["other@host"].Connected; c == nil || *c {
		t.Fatalf("other@host is not connected: %+v", byHost["other@host"])
	}
	if byHost[pulseRemoteTestHost].Kind != "ssh" || byHost[pulseRemoteTestHost].Path != pulseRemoteTestPath {
		t.Fatalf("remote entry = %+v", byHost[pulseRemoteTestHost])
	}
}

func TestTerminalTabsJoinsRemoteLiveStateAndReportsUnreachableHosts(t *testing.T) {
	f := newPulseRemoteFake(t, "", 80, 24)
	h := pulseRemoteTestHandler(t, f)
	// A second remote host that is saved but not connected.
	if err := h.projects.AddRemote("other@host", "/srv/x"); err != nil {
		t.Fatal(err)
	}
	if err := h.termTabsStore.Set("other@host::/srv/x", termtabs.ProjectTerminals{Terminals: []termtabs.Terminal{{ID: "ot-1", Title: "idle"}}}); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Projects []pulseTabsProject `json:"projects"`
	}
	if err := json.Unmarshal([]byte(mustPulseTool(t, h, "terminal_tabs", `{}`)), &got); err != nil {
		t.Fatal(err)
	}
	by := map[string]pulseTabsProject{}
	for _, p := range got.Projects {
		by[p.Host] = p
	}
	live := by[pulseRemoteTestHost]
	if live.Project != pulseRemoteTestPath || len(live.Terminals) != 1 {
		t.Fatalf("connected remote group = %+v", live)
	}
	if term := live.Terminals[0]; !term.Live || !term.Running || term.Command != "claude" {
		t.Fatalf("remote live state not joined: %+v", term)
	}
	if live.RemoteError != "" {
		t.Fatalf("connected host reported an error: %q", live.RemoteError)
	}
	down := by["other@host"]
	if down.RemoteError == "" || !strings.Contains(down.RemoteError, "not connected") {
		t.Fatalf("unconnected host must say why it has no live state: %+v", down)
	}
	if len(down.Terminals) != 1 || down.Terminals[0].Live {
		t.Fatalf("unconnected host's tab must list without live state: %+v", down.Terminals)
	}
	if f.badAuth != 0 {
		t.Fatalf("remote calls must carry the workspace bearer token (%d refused)", f.badAuth)
	}
}

func TestTerminalReadReadsARemoteTerminalThroughItsHost(t *testing.T) {
	f := newPulseRemoteFake(t, "one\r\ntwo\r\nthree\r\n", 80, 24)
	h := pulseRemoteTestHandler(t, f)
	var got struct {
		Host  string   `json:"host"`
		Lines []string `json:"lines"`
	}
	out := mustPulseTool(t, h, "terminal_read", fmt.Sprintf(`{"terminal_id":%q,"host":%q,"lines":2}`, pulseRemoteTestTerm, pulseRemoteTestHost))
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if got.Host != pulseRemoteTestHost || strings.Join(got.Lines, "|") != "two|three" {
		t.Fatalf("remote read = %+v", got)
	}
	f.mu.Lock()
	registered := append([]string(nil), f.registered...)
	f.mu.Unlock()
	if len(registered) != 1 || registered[0] != pulseRemoteTestPath {
		t.Fatalf("the project must be registered on the host before its log is read, got %v", registered)
	}
}

func TestTerminalScreenRendersARemoteTerminalAtItsPtySize(t *testing.T) {
	f := newPulseRemoteFake(t, "\x1b[2J\x1b[Hhello\r\nworld", 20, 4)
	h := pulseRemoteTestHandler(t, f)
	var got struct {
		Cols  int      `json:"cols"`
		Rows  int      `json:"rows"`
		Lines []string `json:"lines"`
	}
	out := mustPulseTool(t, h, "terminal_screen", fmt.Sprintf(`{"terminal_id":%q,"host":%q}`, pulseRemoteTestTerm, pulseRemoteTestHost))
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if got.Cols != 20 || got.Rows != 4 || strings.Join(got.Lines, "|") != "hello|world" {
		t.Fatalf("remote screen = %+v", got)
	}
}

func TestRemoteTerminalToolsRefuseWhatTheyMustNot(t *testing.T) {
	f := newPulseRemoteFake(t, "x\r\n", 80, 24)
	h := pulseRemoteTestHandler(t, f)
	args := func(host string) string {
		return fmt.Sprintf(`{"terminal_id":%q,"host":%q}`, pulseRemoteTestTerm, host)
	}
	for _, tool := range []string{"terminal_read", "terminal_screen"} {
		// A host that is not the host of any saved project is never contacted.
		if _, err := runPulseTool(t, h, tool, args("evil@elsewhere")); err == nil {
			t.Fatalf("%s: an unsaved host must be refused", tool)
		}
		// The local access gate applies to remote output too: it reaches the model.
		h.terminalLoopback = false
		if _, err := runPulseTool(t, h, tool, args(pulseRemoteTestHost)); err == nil || !strings.Contains(err.Error(), "terminal access") {
			t.Fatalf("%s: without terminal access the read must be refused, got %v", tool, err)
		}
		h.terminalLoopback = true
	}
	// A saved host with no live connection is reported, not dialed.
	if err := h.projects.AddRemote("other@host", "/srv/x"); err != nil {
		t.Fatal(err)
	}
	if err := h.termTabsStore.Set("other@host::/srv/x", termtabs.ProjectTerminals{Terminals: []termtabs.Terminal{{ID: "ot-1"}}}); err != nil {
		t.Fatal(err)
	}
	_, err := runPulseTool(t, h, "terminal_read", `{"terminal_id":"ot-1","host":"other@host"}`)
	if err == nil || !strings.Contains(err.Error(), "not connected") {
		t.Fatalf("an unconnected host must be reported as such, got %v", err)
	}
	// A terminal id the host does not own is not found, not read from another project.
	if _, err := runPulseTool(t, h, "terminal_read", fmt.Sprintf(`{"terminal_id":"nope","host":%q}`, pulseRemoteTestHost)); err == nil {
		t.Fatal("an unknown terminal id must be an error")
	}
}

func TestPulseRemoteTabKey(t *testing.T) {
	cases := []struct{ key, host, path string }{
		{"/home/me/app", "", "/home/me/app"},
		{"dev@box::/home/me/app", "dev@box", "/home/me/app"},
		{"wsl:Ubuntu::/home/x", "wsl:Ubuntu", "/home/x"},
		{"dev@box:2222::~/app", "dev@box:2222", "~/app"},
	}
	for _, c := range cases {
		host, path := pulseRemoteTabKey(c.key)
		if host != c.host || path != c.path {
			t.Fatalf("%q -> (%q, %q), want (%q, %q)", c.key, host, path, c.host, c.path)
		}
	}
}

func TestPulseRemoteSavedPathMatchesTildeProjects(t *testing.T) {
	h := pulseTestHandler(t)
	if err := h.projects.AddRemote("dev@box", "~/app"); err != nil {
		t.Fatal(err)
	}
	if got, ok := h.pulseRemoteSavedPath("dev@box", "/home/me/app"); !ok || got != "~/app" {
		t.Fatalf("expanded path must map to its saved ~ project, got %q %v", got, ok)
	}
	if _, ok := h.pulseRemoteSavedPath("dev@box", "/home/me/other"); ok {
		t.Fatal("a path outside the saved projects must not match")
	}
	if _, ok := h.pulseRemoteSavedPath("else@box", "/home/me/app"); ok {
		t.Fatal("a different host must not match")
	}
}
