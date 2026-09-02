//go:build !windows

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// dialTerminal opens a terminal websocket for terminalID and consumes the
// leading attach control frame, returning the connection and whether the
// server reported a resumed shell.
func dialTerminal(t *testing.T, wsURL, terminalID string) (*websocket.Conn, bool) {
	t.Helper()
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL+"?terminal_id="+terminalID, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	if resp != nil {
		resp.Body.Close()
	}
	t.Cleanup(func() { conn.Close() })
	if err := conn.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	mt, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read attach frame: %v", err)
	}
	if mt != websocket.TextMessage {
		t.Fatalf("first frame must be the text attach control frame, got type %d: %q", mt, data)
	}
	var msg terminalAttachMsg
	if err := json.Unmarshal(data, &msg); err != nil || msg.Type != "attach" {
		t.Fatalf("unexpected attach frame %q: %v", data, err)
	}
	return conn, msg.Resumed
}

// readUntil drains binary frames until want appears (or the deadline hits).
func readUntil(t *testing.T, conn *websocket.Conn, want string) string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var seen strings.Builder
	for {
		if err := conn.SetReadDeadline(deadline); err != nil {
			t.Fatalf("SetReadDeadline: %v", err)
		}
		_, data, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read failed before seeing %q (got %q): %v", want, seen.String(), err)
		}
		seen.Write(data)
		if strings.Contains(seen.String(), want) {
			return seen.String()
		}
	}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// Closing the socket must detach — not kill — the shell, and a second dial
// with the same terminal_id must reattach to the same process and replay the
// output produced before (and during) the disconnect.
func TestTerminalWSReattachKeepsShell(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	h, _, wsURL := terminalTestHandler(t)

	conn, resumed := dialTerminal(t, wsURL, "term-reattach")
	if resumed {
		t.Fatal("fresh terminal must not report resumed=true")
	}
	if err := conn.WriteMessage(websocket.BinaryMessage, []byte("echo first-marker\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	readUntil(t, conn, "first-marker\r\n")
	pidBefore := h.terminalProcs.snapshot()["term-reattach"].PID
	if pidBefore <= 0 {
		t.Fatal("terminal was not registered")
	}
	// Queue output that lands while detached; the shell keeps running so it
	// must show up in the replay buffer.
	if err := conn.WriteMessage(websocket.BinaryMessage, []byte("sleep 0.3; echo while-detached\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	waitFor(t, "detach", func() bool { return !h.terminalSessions.lookup("term-reattach").attached() })

	conn2, resumed := dialTerminal(t, wsURL, "term-reattach")
	if !resumed {
		t.Fatal("reattach must report resumed=true")
	}
	replay := readUntil(t, conn2, "while-detached\r\n")
	if !strings.Contains(replay, "first-marker") {
		t.Fatalf("replay missing pre-detach output: %q", replay)
	}
	if pidAfter := h.terminalProcs.snapshot()["term-reattach"].PID; pidAfter != pidBefore {
		t.Fatalf("reattach spawned a new shell: pid %d -> %d", pidBefore, pidAfter)
	}
	if err := conn2.WriteMessage(websocket.BinaryMessage, []byte("echo second-marker\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	readUntil(t, conn2, "second-marker\r\n")
}

// A terminal_id already owned by another project must not be hijacked.
func TestTerminalWSReattachRejectsProjectMismatch(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	h, _, wsURL := terminalTestHandler(t)
	dialTerminal(t, wsURL, "term-mismatch")
	// Point the live session at a different root, then try to reattach from
	// the handler's workdir.
	h.terminalSessions.lookup("term-mismatch").project = t.TempDir()

	_, resp, err := websocket.DefaultDialer.Dial(wsURL+"?terminal_id=term-mismatch", nil)
	if err == nil {
		t.Fatal("expected dial to be rejected")
	}
	if resp == nil || resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %v", resp)
	}
}

// A detached shell that nobody reattaches to must be reaped once the TTL
// elapses, so closed browser tabs in web mode never leak shells forever.
func TestTerminalDetachTTLKillsShell(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	h, _, wsURL := terminalTestHandler(t)
	h.terminalSessions.detachTTL = 200 * time.Millisecond

	conn, _ := dialTerminal(t, wsURL, "term-ttl")
	readUntil(t, conn, "$") // wait for the prompt so the shell is fully up
	if err := conn.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	waitFor(t, "ttl reap", func() bool {
		return h.terminalSessions.lookup("term-ttl") == nil && len(h.terminalProcs.snapshot()) == 0
	})
}

// A socket without a terminal_id has nothing to reattach to, so its shell is
// killed on disconnect exactly as before.
func TestTerminalWSNoIDKillsOnClose(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	h, _, wsURL := terminalTestHandler(t)

	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	if resp != nil {
		resp.Body.Close()
	}
	// Read the attach frame so the handler has fully set up before we close.
	if _, _, err := conn.ReadMessage(); err != nil {
		t.Fatalf("read attach: %v", err)
	}
	conn.Close()
	waitFor(t, "anonymous shell reap", func() bool { return h.terminalSessions.count() == 0 })
}

// DELETE /api/terminal/{id} is the explicit close: it kills the shell right
// away and closes the attached socket.
func TestTerminalKillEndpoint(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	h, srv, wsURL := terminalTestHandler(t)

	conn, _ := dialTerminal(t, wsURL, "term-kill")
	readUntil(t, conn, "$")

	req, err := http.NewRequest(http.MethodDelete, srv.URL+"/api/terminal/term-kill", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}
	waitFor(t, "kill", func() bool {
		return h.terminalSessions.lookup("term-kill") == nil && len(h.terminalProcs.snapshot()) == 0
	})
	// The socket must be closed by the server once the shell is gone.
	if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			break
		}
	}

	req, _ = http.NewRequest(http.MethodDelete, srv.URL+"/api/terminal/term-kill", nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("delete again: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown terminal, got %d", resp.StatusCode)
	}
}

// Two sockets that race to open the same brand-new terminal_id must end up
// with exactly one shell: the reservation serializes creation, so the loser
// waits for the winner instead of spawning (and leaking) a second pty. The
// old code spawned before looking up, so a reload storm could orphan a shell
// per race. SHELL is pointed at a recorder script that logs exactly how many
// processes were spawned — deterministic, unlike scanning the process table.
func TestTerminalWSConcurrentSameIDSpawnsOnce(t *testing.T) {
	recorder := makeShellRecorder(t)
	t.Setenv("SHELL", recorder.script)
	t.Setenv("RECORDER_LOG", recorder.log)
	h, _, wsURL := terminalTestHandler(t)

	const id = "term-race"
	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			conn, resp, err := websocket.DefaultDialer.Dial(wsURL+"?terminal_id="+id, nil)
			if err != nil {
				errs <- err
				return
			}
			if resp != nil {
				resp.Body.Close()
			}
			conn.Close()
		}()
	}
	close(start) // release both dials simultaneously
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent dial failed: %v", err)
	}

	// Exactly one live shell: one session, one registered pid, exactly one
	// recorded spawn. All three must agree.
	if n := h.terminalSessions.count(); n != 1 {
		t.Fatalf("expected exactly 1 session, got %d", n)
	}
	procs := h.terminalProcs.snapshot()
	if len(procs) != 1 || procs[id].PID <= 0 {
		t.Fatalf("expected exactly one registered pid for %s, got %v", id, procs)
	}
	waitFor(t, "exactly one recorded shell spawn", func() bool {
		pids, err := recorder.readPIDs()
		if err != nil {
			return false
		}
		return len(pids) == 1 && pids[0] == procs[id].PID
	})
}

// shellRecorder is a SHELL stand-in that atomically logs the PID of every pty
// shell it spawns, then execs /bin/sh so the terminal behaves normally. The
// exec keeps the PID, so the logged PID is exactly the PID the terminal
// registers — a deterministic spawn counter.
type shellRecorder struct {
	script string
	log    string
}

func makeShellRecorder(t *testing.T) shellRecorder {
	t.Helper()
	dir := t.TempDir()
	r := shellRecorder{
		script: filepath.Join(dir, "sh-recorder"),
		log:    filepath.Join(dir, "spawned-pids"),
	}
	// `$$` is the script's PID; `exec /bin/sh "$@"` replaces the image with
	// the real shell but keeps the PID.
	content := "#!/bin/sh\nprintf '%s\\n' \"$$\" >> \"$RECORDER_LOG\"\nexec /bin/sh \"$@\"\n"
	if err := os.WriteFile(r.script, []byte(content), 0o755); err != nil {
		t.Fatalf("write recorder script: %v", err)
	}
	return r
}

func (r shellRecorder) readPIDs() ([]int32, error) {
	data, err := os.ReadFile(r.log)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var pids []int32
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		n, err := strconv.ParseInt(line, 10, 32)
		if err != nil {
			return nil, err
		}
		pids = append(pids, int32(n))
	}
	return pids, nil
}

// A session whose shell already exited — but whose teardown (terminalExited →
// remove) has not run yet — must not be handed out for reattach. reserve() must
// claim the slot fresh, and the identity-checked remove of the stale teardown
// must not clobber the new session.
func TestTerminalSessionTableReserveClaimsStaleEntry(t *testing.T) {
	tab := newTerminalSessionTable()
	stale := &terminalSession{id: "term-stale", resumable: true, exited: true}
	tab.put("term-stale", stale)

	existing, created, done := tab.reserve("term-stale")
	if created == false {
		t.Fatal("expected reserve to claim the stale slot for a fresh create")
	}
	if existing != nil {
		t.Fatalf("expected no existing session, got %v", existing)
	}
	if done == nil {
		t.Fatal("expected a done channel for the creator")
	}

	// The stale teardown may still call remove(id, stale) — identity check must
	// keep the new session.
	fresh := &terminalSession{id: "term-stale", resumable: true}
	tab.completeCreate("term-stale", fresh)
	tab.remove("term-stale", stale)
	if got := tab.lookup("term-stale"); got != fresh {
		t.Fatalf("stale remove clobbered the fresh session: got %v, want %v", got, fresh)
	}
}

// terminalTestHandler serves the terminal ws + kill routes on a mux so the
// DELETE path parameter resolves, and reaps every shell on cleanup.
func terminalTestHandler(t *testing.T) (*Handler, *httptest.Server, string) {
	t.Helper()
	h := NewHandler()
	h.workDir = t.TempDir()
	h.SetTerminalAccessPolicy(false, true)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/terminal/ws", h.HandleTerminalWS)
	mux.HandleFunc("DELETE /api/terminal/{id}", h.HandleTerminalKill)
	srv := httptest.NewServer(mux)
	t.Cleanup(func() {
		h.shutdownTerminals(t.Context())
		srv.Close()
	})
	return h, srv, "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/terminal/ws"
}
