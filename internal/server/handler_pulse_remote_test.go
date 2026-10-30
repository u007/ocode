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
	"time"

	"github.com/gorilla/websocket"
	"github.com/u007/ocode/internal/agent"
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

	// board and session endpoints
	pulseHits int
	boardRows []PulseRow
	messages  []agent.Message
	asks      *PendingAsks
	killed    []string
	attached  []string // terminal ids created by a websocket attach
	attachDir []string // project_path of each attach
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
	mux.HandleFunc("GET /api/pulse", auth(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.pulseHits++
		rows := append([]PulseRow{}, f.boardRows...)
		f.mu.Unlock()
		_ = json.NewEncoder(w).Encode(PulsePageResponse{Items: rows})
	}))
	mux.HandleFunc("GET /api/sessions/{id}", auth(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		msgs := append([]agent.Message{}, f.messages...)
		f.mu.Unlock()
		total := len(msgs)
		if n, _ := strconv.Atoi(r.URL.Query().Get("limit")); n > 0 && n < total {
			msgs = msgs[total-n:]
		}
		_ = json.NewEncoder(w).Encode(SessionDetail{SessionInfo: SessionInfo{ID: r.PathValue("id"), Title: "remote title"}, Messages: msgs, Total: total})
	}))
	mux.HandleFunc("GET /api/sessions/{id}/recap", auth(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"recap": "remote recap for " + r.PathValue("id")})
	}))
	mux.HandleFunc("GET /api/sessions/{id}/state", auth(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		asks := f.asks
		f.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"pending_asks": asks})
	}))
	mux.HandleFunc("DELETE /api/terminal/{id}", auth(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.killed = append(f.killed, r.PathValue("id"))
		f.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	upgrader := websocket.Upgrader{}
	mux.HandleFunc("GET /api/terminal/ws", auth(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		f.mu.Lock()
		f.attached = append(f.attached, r.URL.Query().Get("terminal_id"))
		f.attachDir = append(f.attachDir, r.URL.Query().Get("project_path"))
		f.mu.Unlock()
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"attach","resumed":false}`))
		_, _, _ = conn.ReadMessage() // wait for the client's close
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

func TestPulseBoardMergesRemoteSessionsAndNamesUnreachableHosts(t *testing.T) {
	f := newPulseRemoteFake(t, "", 80, 24)
	f.boardRows = []PulseRow{{
		SessionID: "rs-1", ProjectPath: pulseRemoteTestPath, Title: "remote work", Status: PulseStatusNeedsPermission,
		PendingAsk: &PulseAsk{Kind: PulseAskKindPermission, Summary: "rm -rf build"}, UpdatedAt: time.Now(),
	}}
	h := pulseRemoteTestHandler(t, f)
	registerLiveSession(t, h, "local-1", t.TempDir(), true, nil)
	if err := h.projects.AddRemote("other@host", "/srv/x"); err != nil {
		t.Fatal(err)
	}

	var got struct {
		Items []PulseRow      `json:"items"`
		Hosts []pulseHostNote `json:"hosts_unavailable"`
	}
	if err := json.Unmarshal([]byte(mustPulseTool(t, h, "pulse_board", `{}`)), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != 2 || got.Items[0].SessionID != "rs-1" || got.Items[0].Host != pulseRemoteTestHost {
		t.Fatalf("a remote needs-you row must be tagged with its host and sort ahead of a running local one: %+v", got.Items)
	}
	if got.Items[1].SessionID != "local-1" || got.Items[1].Host != "" {
		t.Fatalf("local row = %+v", got.Items[1])
	}
	if len(got.Hosts) != 1 || got.Hosts[0].Host != "other@host" || !strings.Contains(got.Hosts[0].Reason, "not connected") {
		t.Fatalf("an unconnected host must be named with its reason, got %+v", got.Hosts)
	}

	snap := h.pulseBoardSnapshot()
	if !strings.Contains(snap, "app@"+pulseRemoteTestHost+" | rs-1 | remote work") {
		t.Fatalf("snapshot must show the remote project as name@host:\n%s", snap)
	}
	if !strings.Contains(snap, "Remote hosts not shown:\nother@host: not connected") {
		t.Fatalf("snapshot must say which host is missing:\n%s", snap)
	}
}

func TestPulseBoardCachesRemoteRowsAndRemembersFailures(t *testing.T) {
	f := newPulseRemoteFake(t, "", 80, 24)
	h := pulseRemoteTestHandler(t, f)
	for range 3 {
		mustPulseTool(t, h, "pulse_board", `{}`)
	}
	f.mu.Lock()
	hits := f.pulseHits
	f.mu.Unlock()
	if hits != 1 {
		t.Fatalf("three board reads inside the TTL must cost one remote request, got %d", hits)
	}

	// A host that errors is left alone for the failure window, not re-asked per turn.
	f.srv.Close()
	h.pulseRemoteBoard = pulseRemoteBoardCache{}
	first := mustPulseTool(t, h, "pulse_board", `{}`)
	if !strings.Contains(first, "hosts_unavailable") {
		t.Fatalf("a failing host must be reported: %s", first)
	}
	h.pulseRemoteBoard.mu.Lock()
	e := h.pulseRemoteBoard.m[pulseRemoteTestHost+"\x00live"]
	h.pulseRemoteBoard.m[pulseRemoteTestHost+"\x00live"] = pulseRemoteBoardEntry{err: e.err, at: e.at}
	h.pulseRemoteBoard.mu.Unlock()
	if e.err == nil {
		t.Fatal("the failure must be cached")
	}
}

func TestSessionToolsReadARemoteSessionThroughItsHost(t *testing.T) {
	f := newPulseRemoteFake(t, "", 80, 24)
	for i := 0; i < 5; i++ {
		f.messages = append(f.messages, agent.Message{Role: "user", Content: fmt.Sprintf("msg %d needle%d", i, i%2)})
	}
	f.asks = &PendingAsks{Permissions: []PermissionEvent{{RequestID: "req-1", Tool: "bash", Scope: "bash", Summary: "run secret-command"}}}
	f.boardRows = []PulseRow{{SessionID: "rs-1", ProjectPath: pulseRemoteTestPath, Status: PulseStatusRunning, UpdatedAt: time.Now()}}
	h := pulseRemoteTestHandler(t, f)
	mustPulseTool(t, h, "pulse_board", `{}`) // fills the row cache that supplies project_path

	var read struct {
		Host        string `json:"host"`
		ProjectPath string `json:"project_path"`
		Title       string `json:"title"`
		Messages    []struct {
			Index   int    `json:"index"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	args := func(extra string) string {
		return fmt.Sprintf(`{"session_id":"rs-1","host":%q%s}`, pulseRemoteTestHost, extra)
	}
	if err := json.Unmarshal([]byte(mustPulseTool(t, h, "session_read", args(`,"last":2`))), &read); err != nil {
		t.Fatal(err)
	}
	if read.Host != pulseRemoteTestHost || read.Title != "remote title" || read.ProjectPath != pulseRemoteTestPath {
		t.Fatalf("remote read header = %+v", read)
	}
	if len(read.Messages) != 2 || read.Messages[0].Index != 3 || read.Messages[1].Index != 4 {
		t.Fatalf("indexes must be absolute within the host's transcript, got %+v", read.Messages)
	}
	if err := json.Unmarshal([]byte(mustPulseTool(t, h, "session_read", args(`,"search":"needle0"`))), &read); err != nil {
		t.Fatal(err)
	}
	if len(read.Messages) != 3 || read.Messages[0].Index != 0 || read.Messages[2].Index != 4 {
		t.Fatalf("search must filter the whole transcript with absolute indexes, got %+v", read.Messages)
	}

	if out := mustPulseTool(t, h, "session_recap", args("")); !strings.Contains(out, "remote recap for rs-1") {
		t.Fatalf("remote recap = %s", out)
	}

	var ask pulseAskResult
	if err := json.Unmarshal([]byte(mustPulseTool(t, h, "session_ask", args(""))), &ask); err != nil {
		t.Fatal(err)
	}
	if len(ask.Permissions) != 1 || ask.Permissions[0].RequestID != "req-1" || ask.Permissions[0].Summary != "run secret-command" {
		t.Fatalf("remote ask = %+v", ask)
	}
	h.terminalLoopback = false
	var gated pulseAskResult
	if err := json.Unmarshal([]byte(mustPulseTool(t, h, "session_ask", args(""))), &gated); err != nil {
		t.Fatal(err)
	}
	if len(gated.Permissions) != 1 || gated.Permissions[0].Summary != "" || gated.Permissions[0].RequestID != "req-1" {
		t.Fatalf("without terminal access a remote ask must drop command text like a local one: %+v", gated.Permissions)
	}

	if _, err := runPulseTool(t, h, "session_read", `{"session_id":"rs-1","host":"evil@elsewhere"}`); err == nil {
		t.Fatal("a host that is not a saved project's host must be refused")
	}
	// Acting on a remote session says why it cannot, instead of "not found".
	_, err := runPulseTool(t, h, "session_send", `{"session_id":"rs-1","content":"hi"}`)
	if err == nil || !strings.Contains(err.Error(), "remote host "+pulseRemoteTestHost) {
		t.Fatalf("a write to a remote session must name the host, got %v", err)
	}
}

func TestTerminalNewStartsAShellOnARemoteHost(t *testing.T) {
	f := newPulseRemoteFake(t, "", 80, 24)
	h := pulseRemoteTestHandler(t, f)
	out := mustPulseTool(t, h, "terminal_new", fmt.Sprintf(`{"host":%q,"project_path":%q,"title":"dev"}`, pulseRemoteTestHost, pulseRemoteTestPath))
	var got struct {
		TerminalID  string `json:"terminal_id"`
		Host        string `json:"host"`
		ProjectPath string `json:"project_path"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	attached, dirs, registered := append([]string(nil), f.attached...), append([]string(nil), f.attachDir...), append([]string(nil), f.registered...)
	f.mu.Unlock()
	if len(attached) != 1 || attached[0] != got.TerminalID || dirs[0] != pulseRemoteTestPath {
		t.Fatalf("the host must see one attach for the new id in the project: %v %v (result %+v)", attached, dirs, got)
	}
	if len(registered) != 1 {
		t.Fatalf("the project must be registered on the host first: %v", registered)
	}
	tabs := h.termTabsStore.Get(pulseRemoteTestHost + "::" + pulseRemoteTestPath)
	found := false
	for _, tab := range tabs.Terminals {
		if tab.ID == got.TerminalID && tab.Title == "dev" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the new terminal must get a shared tab under host::path: %+v", tabs.Terminals)
	}

	if _, err := runPulseTool(t, h, "terminal_new", fmt.Sprintf(`{"host":%q,"project_path":"/somewhere/else"}`, pulseRemoteTestHost)); err == nil {
		t.Fatal("a path that is not a saved project of the host must be refused")
	}
	if _, err := runPulseTool(t, h, "terminal_new", fmt.Sprintf(`{"host":"evil@elsewhere","project_path":%q}`, pulseRemoteTestPath)); err == nil {
		t.Fatal("an unsaved host must be refused")
	}
	h.terminalLoopback = false
	if _, err := runPulseTool(t, h, "terminal_new", fmt.Sprintf(`{"host":%q,"project_path":%q}`, pulseRemoteTestHost, pulseRemoteTestPath)); err == nil {
		t.Fatal("without terminal access no shell may be started")
	}
	f.mu.Lock()
	n := len(f.attached)
	f.mu.Unlock()
	if n != 1 {
		t.Fatalf("refused calls must not reach the host, attaches = %d", n)
	}
}

func TestTerminalKillKillsARemoteTerminalAndDropsItsTab(t *testing.T) {
	f := newPulseRemoteFake(t, "", 80, 24)
	h := pulseRemoteTestHandler(t, f)
	out := mustPulseTool(t, h, "terminal_kill", fmt.Sprintf(`{"terminal_id":%q,"host":%q}`, pulseRemoteTestTerm, pulseRemoteTestHost))
	if !strings.Contains(out, `"killed":true`) {
		t.Fatalf("kill result = %s", out)
	}
	f.mu.Lock()
	killed := append([]string(nil), f.killed...)
	f.mu.Unlock()
	if len(killed) != 1 || killed[0] != pulseRemoteTestTerm {
		t.Fatalf("the host must receive the kill: %v", killed)
	}
	if tabs := h.termTabsStore.Get(pulseRemoteTestHost + "::" + pulseRemoteTestPath); len(tabs.Terminals) != 0 {
		t.Fatalf("the killed terminal's tab must be dropped: %+v", tabs.Terminals)
	}

	// An id the host does not report live is never sent to its kill endpoint.
	if _, err := runPulseTool(t, h, "terminal_kill", fmt.Sprintf(`{"terminal_id":"ghost","host":%q}`, pulseRemoteTestHost)); err == nil {
		t.Fatal("a terminal that is not live on the host must be refused")
	}
	if _, err := runPulseTool(t, h, "terminal_kill", fmt.Sprintf(`{"terminal_id":%q,"host":"evil@elsewhere"}`, pulseRemoteTestTerm)); err == nil {
		t.Fatal("an unsaved host must be refused")
	}
	f.mu.Lock()
	n := len(f.killed)
	f.mu.Unlock()
	if n != 1 {
		t.Fatalf("refused kills must not reach the host, kills = %d", n)
	}
}
