//go:build !windows

package server

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/remote"
	"github.com/u007/ocode/internal/termtabs"
)

const (
	pulseRemoteTestHost = "dev@box"
	pulseRemoteTestPath = "/home/me/app"
	pulseRemoteTestTerm = "rt-1"
	// pulseRemoteTestHome is the fake host's home: a saved "~" project expands to
	// it, as the real host's POST /api/projects does on its own machine.
	pulseRemoteTestHome = "/home/me"
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

	// firstFrame, when set, replaces the attach frame the terminal websocket sends first.
	firstFrame string
	// liveGrow makes the live list grow forever: every page is full and total is
	// always one more than the rows served so far. liveHits counts its requests.
	liveGrow bool
	liveHits int
	// pulseDelay makes each GET /api/pulse wait before it answers. pulsePages > 0
	// serves that many pages, linked by cursor.
	pulseDelay time.Duration
	pulsePages int
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
		// Like the real host: "~" expands on the host's own home, and the stored
		// path is echoed back.
		stored := body.Path
		if stored == "~" || strings.HasPrefix(stored, "~/") {
			stored = filepath.Join(pulseRemoteTestHome, stored[1:])
		}
		f.mu.Lock()
		f.registered = append(f.registered, body.Path)
		f.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "path": stored})
	}))
	mux.HandleFunc("GET /api/pulse/terminals", auth(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		grow := f.liveGrow
		if grow {
			f.liveHits++
		}
		f.mu.Unlock()
		if grow {
			offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
			limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
			rows := make([]pulseTerminalRow, 0, limit)
			for i := range limit {
				rows = append(rows, pulseTerminalRow{ID: fmt.Sprintf("g-%d", offset+i), Project: pulseRemoteTestPath})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"terminals": rows, "total": offset + limit + 1})
			return
		}
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
		delay, pages := f.pulseDelay, f.pulsePages
		f.mu.Unlock()
		if delay > 0 {
			select {
			case <-time.After(delay):
			case <-r.Context().Done():
				return
			}
		}
		page := 0
		if c := r.URL.Query().Get("cursor"); c != "" {
			page, _ = strconv.Atoi(c)
		}
		f.mu.Lock()
		rows := append([]PulseRow{}, f.boardRows...)
		f.mu.Unlock()
		resp := PulsePageResponse{Items: rows}
		if pages > 0 && page+1 < pages {
			next := strconv.Itoa(page + 1)
			resp.NextCursor = &next
		}
		_ = json.NewEncoder(w).Encode(resp)
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
		first := f.firstFrame
		f.mu.Unlock()
		if first == "" {
			first = `{"type":"attach","resumed":false}`
		}
		_ = conn.WriteMessage(websocket.TextMessage, []byte(first))
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
	var ws remoteHostWorkspace
	if f != nil {
		ws = f.ws
	}
	h := newTestProxyHandler(t, pulseRemoteTestHost, pulseRemoteTestPath, ws)
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
	h := pulseTestHandler(t)
	for _, host := range []string{"dev@box", "dev@box:2222", "wsl:Ubuntu"} {
		if err := h.projects.AddRemote(host, "/srv/x"); err != nil {
			t.Fatal(err)
		}
	}
	cases := []struct{ key, host, path string }{
		{"/home/me/app", "", "/home/me/app"},
		{"dev@box::/home/me/app", "dev@box", "/home/me/app"},
		{"wsl:Ubuntu::/home/x", "wsl:Ubuntu", "/home/x"},
		{"dev@box:2222::~/app", "dev@box:2222", "~/app"},
		// A local path that contains "::" is not split on a prefix that is no saved host.
		{"/work/a::b", "", "/work/a::b"},
		{"unsaved@host::/x", "", "unsaved@host::/x"},
	}
	for _, c := range cases {
		host, path := h.pulseSplitTabKey(c.key)
		if host != c.host || path != c.path {
			t.Fatalf("%q -> (%q, %q), want (%q, %q)", c.key, host, path, c.host, c.path)
		}
	}
}

func TestPulseRemoteSavedPathMatchesTildeProjectsOnlyByTheHostsExpansion(t *testing.T) {
	f := newPulseRemoteFake(t, "", 80, 24)
	h := newTestProxyHandler(t, pulseRemoteTestHost, "~/app", f.ws)
	if _, ok := h.pulseRemoteSavedPath(pulseRemoteTestHost, pulseRemoteTestPath); ok {
		t.Fatal("before the host has reported the expansion, a ~ project must match nothing")
	}
	if err := h.pulseRemoteRegister(f.ws, pulseRemoteTestHost, "~/app"); err != nil {
		t.Fatal(err)
	}
	if got, ok := h.pulseRemoteSavedPath(pulseRemoteTestHost, pulseRemoteTestPath); !ok || got != "~/app" {
		t.Fatalf("the host's expansion must map to its saved ~ project, got %q %v", got, ok)
	}
	// A suffix is not an expansion: each of these ends in /app, and none is the host's /home/me/app.
	for _, path := range []string{"/var/www/app", "/home/me/x/app", "/home/me/other"} {
		if _, ok := h.pulseRemoteSavedPath(pulseRemoteTestHost, path); ok {
			t.Fatalf("%q must not match the saved ~/app", path)
		}
	}
	if _, ok := h.pulseRemoteSavedPath("else@box", pulseRemoteTestPath); ok {
		t.Fatal("a different host must not match")
	}
}

func TestTildeProjectTerminalReadsThroughTheHostsExpansion(t *testing.T) {
	f := newPulseRemoteFake(t, "one\r\ntwo\r\nthree\r\n", 80, 24)
	h := newTestProxyHandler(t, pulseRemoteTestHost, "~/app", f.ws)
	h.terminalLoopback = true
	store, err := termtabs.NewStoreAt(filepath.Join(t.TempDir(), "term.json"))
	if err != nil {
		t.Fatal(err)
	}
	h.termTabsStore = store
	// The tab is keyed by the saved path. The host's own path (/home/me/app) is
	// what its history endpoint must receive, and the read fails with a 404 if not.
	if err := store.Set(pulseRemoteTestHost+"::~/app", termtabs.ProjectTerminals{
		Terminals: []termtabs.Terminal{{ID: pulseRemoteTestTerm, Title: "claude"}},
	}); err != nil {
		t.Fatal(err)
	}
	injectProxy(t, h.remoteHosts, pulseRemoteTestHost, f.ws)

	var got struct {
		Project string   `json:"project_path"`
		Lines   []string `json:"lines"`
	}
	out := mustPulseTool(t, h, "terminal_read", fmt.Sprintf(`{"terminal_id":%q,"host":%q,"lines":2}`, pulseRemoteTestTerm, pulseRemoteTestHost))
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if strings.Join(got.Lines, "|") != "two|three" {
		t.Fatalf("tilde project read = %+v", got)
	}
	f.mu.Lock()
	registered := append([]string(nil), f.registered...)
	f.mu.Unlock()
	if len(registered) != 1 || registered[0] != "~/app" {
		t.Fatalf("the saved path, not the host path, is what gets registered: %v", registered)
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

func TestHostConnectConnectsASavedHostWithoutBlocking(t *testing.T) {
	f := newPulseRemoteFake(t, "", 80, 24)
	h := pulseRemoteTestHandler(t, nil) // saved remote project, nothing connected
	var connects atomic.Int32
	release := make(chan struct{})
	h.remoteHosts = newTestRegistry(func(remote.Target, string) (remoteHostWorkspace, error) {
		connects.Add(1)
		<-release
		return f.ws, nil
	})
	old := remoteConnectWaitBound
	remoteConnectWaitBound = 50 * time.Millisecond
	t.Cleanup(func() { remoteConnectWaitBound = old })

	if pt := pulseToolByName(t, h, "host_connect"); pt.ask {
		t.Fatal("host_connect is allowed without a per-call approval; it must carry no ask")
	}
	call := func() pulseHostConnectResult {
		t.Helper()
		var r pulseHostConnectResult
		if err := json.Unmarshal([]byte(mustPulseTool(t, h, "host_connect", fmt.Sprintf(`{"host":%q}`, pulseRemoteTestHost))), &r); err != nil {
			t.Fatal(err)
		}
		return r
	}

	// A connect that outlasts the wait bound is reported as in progress, not awaited.
	started := time.Now()
	if r := call(); r.State != "connecting" {
		t.Fatalf("slow connect = %+v, want connecting", r)
	}
	if time.Since(started) > 2*time.Second {
		t.Fatal("host_connect must not wait for a whole connect")
	}
	// Calling again must not start a second connect.
	if r := call(); r.State != "connecting" {
		t.Fatalf("second call = %+v, want connecting", r)
	}
	if n := connects.Load(); n != 1 {
		t.Fatalf("one connect attempt expected, got %d", n)
	}
	close(release)
	deadline := time.Now().Add(5 * time.Second)
	for !h.pulseRemoteHostConnected(pulseRemoteTestHost) {
		if time.Now().After(deadline) {
			t.Fatal("the background connect never completed")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if r := call(); r.State != "connected" {
		t.Fatalf("connected host = %+v", r)
	}
	if n := connects.Load(); n != 1 {
		t.Fatalf("a connected host must never be reconnected, connects = %d", n)
	}
}

func TestHostConnectLeavesAFailedHostAloneAndRefusesUnsavedHosts(t *testing.T) {
	h := pulseRemoteTestHandler(t, nil)
	var connects atomic.Int32
	h.remoteHosts = newTestRegistry(func(remote.Target, string) (remoteHostWorkspace, error) {
		connects.Add(1)
		return nil, errors.New("ssh: connection refused")
	})
	args := fmt.Sprintf(`{"host":%q}`, pulseRemoteTestHost)
	var first, second pulseHostConnectResult
	if err := json.Unmarshal([]byte(mustPulseTool(t, h, "host_connect", args)), &first); err != nil {
		t.Fatal(err)
	}
	if first.State != "failed" || !strings.Contains(first.Error, "connection refused") || first.RetryAfterSeconds == 0 {
		t.Fatalf("first attempt = %+v", first)
	}
	if err := json.Unmarshal([]byte(mustPulseTool(t, h, "host_connect", args)), &second); err != nil {
		t.Fatal(err)
	}
	if second.State != "failed" || second.RetryAfterSeconds == 0 || !strings.Contains(second.Error, "connection refused") {
		t.Fatalf("a repeat inside the cooldown must report the last failure: %+v", second)
	}
	if n := connects.Load(); n != 1 {
		t.Fatalf("a failed host must not be retried inside the cooldown, connects = %d", n)
	}

	if _, err := runPulseTool(t, h, "host_connect", `{"host":"evil@elsewhere"}`); err == nil {
		t.Fatal("a host that is not the host of a saved project must be refused")
	}
	if _, err := runPulseTool(t, h, "host_connect", `{}`); err == nil {
		t.Fatal("host is required")
	}
	if n := connects.Load(); n != 1 {
		t.Fatalf("refused calls must not dial, connects = %d", n)
	}
}

func TestTerminalNewDiscardsTheShellWhenItsAttachFails(t *testing.T) {
	f := newPulseRemoteFake(t, "", 80, 24)
	f.firstFrame = `{"type":"error","error":"no shell"}`
	h := pulseRemoteTestHandler(t, f)
	_, err := runPulseTool(t, h, "terminal_new", fmt.Sprintf(`{"host":%q,"project_path":%q}`, pulseRemoteTestHost, pulseRemoteTestPath))
	if err == nil {
		t.Fatal("a bad first frame must fail terminal_new")
	}
	f.mu.Lock()
	attached := append([]string(nil), f.attached...)
	killed := append([]string(nil), f.killed...)
	f.mu.Unlock()
	// The host spawned the shell before the websocket upgrade, so the failed attach
	// must not leave it detached for 24 h with no tab.
	if len(attached) != 1 || len(killed) != 1 || killed[0] != attached[0] {
		t.Fatalf("the shell the host spawned must be discarded: attached %v, killed %v", attached, killed)
	}
	// The fixture's own tab stays; the failed attach must add none.
	if tabs := h.termTabsStore.Get(pulseRemoteTestHost + "::" + pulseRemoteTestPath); len(tabs.Terminals) != 1 || tabs.Terminals[0].ID != pulseRemoteTestTerm {
		t.Fatalf("a failed terminal_new must not add a tab: %+v", tabs.Terminals)
	}
}

func TestPulseBoardConcurrentColdReadsShareOneHostRequest(t *testing.T) {
	f := newPulseRemoteFake(t, "", 80, 24)
	// The delay keeps the first fetch in flight while the other callers arrive.
	f.pulseDelay = 300 * time.Millisecond
	f.boardRows = []PulseRow{{SessionID: "rs-1", ProjectPath: pulseRemoteTestPath, Status: PulseStatusRunning, UpdatedAt: time.Now()}}
	h := pulseRemoteTestHandler(t, f)
	const callers = 8
	var wg sync.WaitGroup
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := runPulseTool(t, h, "pulse_board", `{}`); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	f.mu.Lock()
	hits := f.pulseHits
	f.mu.Unlock()
	if hits != 1 {
		t.Fatalf("%d concurrent board reads on a cold cache must share one /api/pulse request, got %d", callers, hits)
	}
}

func TestPulseBoardFetchSpendsOneDeadlineAcrossPages(t *testing.T) {
	f := newPulseRemoteFake(t, "", 80, 24)
	// Three pages of 1.2 s each fit each page's own 3 s limit, but not the 3 s the
	// whole board read is allowed.
	f.pulseDelay = 1200 * time.Millisecond
	f.pulsePages = 3
	h := pulseRemoteTestHandler(t, f)
	started := time.Now()
	if _, err := h.pulseRemoteHostRows(pulseRemoteTestHost, "live"); err == nil {
		t.Fatal("three 1.2 s pages cannot finish inside one 3 s board deadline")
	}
	if elapsed := time.Since(started); elapsed > pulseRemoteBoardTimeout+time.Second {
		t.Fatalf("the board read ran %v, past its %v deadline", elapsed, pulseRemoteBoardTimeout)
	}
}

func TestPulseBoardFailureIsNotServedAfterReconnect(t *testing.T) {
	f := newPulseRemoteFake(t, "", 80, 24)
	h := pulseRemoteTestHandler(t, f)
	f.srv.Close() // the connection's host is down
	if _, err := h.pulseRemoteHostRows(pulseRemoteTestHost, "live"); err == nil {
		t.Fatal("a down host must fail the board read")
	}
	// The operator reconnects: the registry now holds a new workspace for the host.
	back := newPulseRemoteFake(t, "", 80, 24)
	injectProxy(t, h.remoteHosts, pulseRemoteTestHost, back.ws)
	if _, err := h.pulseRemoteHostRows(pulseRemoteTestHost, "live"); err != nil {
		t.Fatalf("a failure cached on the old connection must not be served after reconnect: %v", err)
	}
	back.mu.Lock()
	hits := back.pulseHits
	back.mu.Unlock()
	if hits != 1 {
		t.Fatalf("the reconnected host must be asked once, got %d", hits)
	}
}

func TestPulseRemoteSessionHostNamesOnlyFreshConnectedBoards(t *testing.T) {
	f := newPulseRemoteFake(t, "", 80, 24)
	h := pulseRemoteTestHandler(t, f)
	key := pulseRemoteTestHost + "\x00live"
	put := func(e pulseRemoteBoardEntry) {
		h.pulseRemoteBoard.mu.Lock()
		defer h.pulseRemoteBoard.mu.Unlock()
		if h.pulseRemoteBoard.m == nil {
			h.pulseRemoteBoard.m = make(map[string]pulseRemoteBoardEntry)
		}
		h.pulseRemoteBoard.m[key] = e
	}
	rows := []PulseRow{{SessionID: "rs-1"}}
	now := time.Now()

	put(pulseRemoteBoardEntry{rows: rows, at: now.Add(-pulseRemoteSessionHostMaxAge - time.Minute), ws: f.ws})
	if got := h.pulseRemoteSessionHost("rs-1"); got != "" {
		t.Fatalf("a board older than %v must not name its host, got %q", pulseRemoteSessionHostMaxAge, got)
	}
	put(pulseRemoteBoardEntry{rows: rows, err: errors.New("timed out"), at: now, ws: f.ws})
	if got := h.pulseRemoteSessionHost("rs-1"); got != "" {
		t.Fatalf("a failed board must not name its host, got %q", got)
	}
	put(pulseRemoteBoardEntry{rows: rows, at: now, ws: f.ws})
	if got := h.pulseRemoteSessionHost("rs-1"); got != pulseRemoteTestHost {
		t.Fatalf("a fresh, successful board on a connected host must name it, got %q", got)
	}
	h.remoteHosts.drop(pulseRemoteTestHost, 0)
	if got := h.pulseRemoteSessionHost("rs-1"); got != "" {
		t.Fatalf("a host that is no longer connected must not be named, got %q", got)
	}
}

func TestPulseRemoteLiveErrorsInsteadOfTruncating(t *testing.T) {
	f := newPulseRemoteFake(t, "", 80, 24)
	f.liveGrow = true
	h := pulseRemoteTestHandler(t, f)
	rows, err := h.pulseRemoteLive(pulseRemoteTestHost)
	if err == nil || !strings.Contains(err.Error(), "reports more than") {
		t.Fatalf("a host that never stops growing must be an error, got %d rows, %v", len(rows), err)
	}
	f.mu.Lock()
	hits := f.liveHits
	f.mu.Unlock()
	if hits != pulseRemoteLiveMaxPages {
		t.Fatalf("the live list must stop at the page cap (%d requests), got %d", pulseRemoteLiveMaxPages, hits)
	}
}

func TestHostConnectReportsTheBackgroundFailure(t *testing.T) {
	h := pulseRemoteTestHandler(t, nil)
	release := make(chan struct{})
	h.remoteHosts = newTestRegistry(func(remote.Target, string) (remoteHostWorkspace, error) {
		<-release
		return nil, errors.New("ssh: host key mismatch")
	})
	old := remoteConnectWaitBound
	remoteConnectWaitBound = 50 * time.Millisecond
	t.Cleanup(func() { remoteConnectWaitBound = old })

	args := fmt.Sprintf(`{"host":%q}`, pulseRemoteTestHost)
	var first pulseHostConnectResult
	if err := json.Unmarshal([]byte(mustPulseTool(t, h, "host_connect", args)), &first); err != nil {
		t.Fatal(err)
	}
	if first.State != "connecting" {
		t.Fatalf("first call = %+v, want connecting", first)
	}
	close(release)
	deadline := time.Now().Add(5 * time.Second)
	for h.remoteHosts.lastFailure(pulseRemoteTestHost, 0) == nil {
		if time.Now().After(deadline) {
			t.Fatal("the background connect never failed")
		}
		time.Sleep(10 * time.Millisecond)
	}
	var second pulseHostConnectResult
	if err := json.Unmarshal([]byte(mustPulseTool(t, h, "host_connect", args)), &second); err != nil {
		t.Fatal(err)
	}
	if second.State != "failed" || !strings.Contains(second.Error, "host key mismatch") {
		t.Fatalf("a repeat call after the background failure must report its real error, got %+v", second)
	}
}

func TestPulseRemoteTildeRegistrationIsTheOnlyLinkToTheHostPath(t *testing.T) {
	f := newPulseRemoteFake(t, "", 80, 24)
	h := newTestProxyHandler(t, pulseRemoteTestHost, "~/app", f.ws)
	if err := h.pulseRemoteRegister(f.ws, pulseRemoteTestHost, "~/app"); err != nil {
		t.Fatal(err)
	}
	if got, ok := h.pulseRemoteHostPath(pulseRemoteTestHost, "~/app"); !ok || got != pulseRemoteTestPath {
		t.Fatalf("host path of ~/app = %q %v, want %q", got, ok, pulseRemoteTestPath)
	}
	if got, ok := h.pulseRemoteHostPath(pulseRemoteTestHost, "/srv/x"); !ok || got != "/srv/x" {
		t.Fatalf("an absolute saved path is its own host path, got %q %v", got, ok)
	}
}
