//go:build !windows

package server

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// wantSignalTests skips the signal-delivery tests unless explicitly opted in.
// The terminal path signals a process group rooted at a session-leader child
// (pty.Start sets Setsid), exactly the shape that some sandboxed exec runners
// intercept so kill() reports success yet the child survives. On a normal
// desktop host SIGTERM/SIGKILL to such a group works, so developers can run:
//
//	OCODE_TEST_TERMINAL_SIGNALS=1 go test ./internal/server/ -run TerminateProcessTree
func wantSignalTests(t *testing.T) {
	t.Helper()
	if os.Getenv("OCODE_TEST_TERMINAL_SIGNALS") == "" {
		t.Skip("set OCODE_TEST_TERMINAL_SIGNALS=1 to run (needs signal delivery to session-leader children, unavailable in some sandboxes)")
	}
}

// TestTerminateProcessTreeSigtermExits verifies that a process group which
// honors SIGTERM is reaped quickly (well within the grace window) and is fully
// gone afterwards.
func TestTerminateProcessTreeSigtermExits(t *testing.T) {
	wantSignalTests(t)
	// Shell that exits cleanly on SIGTERM (deterministic, unlike `sleep`).
	cmd := exec.Command("sh", "-c", "trap 'exit 0' TERM; while true; do sleep 0.5; done")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start shell: %v", err)
	}
	pid := cmd.Process.Pid

	start := time.Now()
	terminateProcessTree(pid, 2*time.Second)
	elapsed := time.Since(start)

	if _, err := cmd.Process.Wait(); err != nil {
		t.Logf("shell wait result: %v", err)
	}
	if elapsed > 1500*time.Millisecond {
		t.Fatalf("terminate returned too late: %s (expected < 1.5s)", elapsed)
	}
	if err := syscall.Kill(-pid, 0); err != syscall.ESRCH {
		t.Fatalf("process group %d still alive after terminate (err=%v)", pid, err)
	}
}

// TestTerminateProcessTreeSigkillEscalation verifies that a process group which
// ignores SIGTERM is force-killed after the grace window elapses.
func TestTerminateProcessTreeSigkillEscalation(t *testing.T) {
	wantSignalTests(t)
	// bash that ignores TERM and loops forever.
	cmd := exec.Command("bash", "-c", "trap '' TERM; while true; do sleep 0.2; done")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		t.Skipf("bash unavailable, skipping: %v", err)
	}
	pid := cmd.Process.Pid

	start := time.Now()
	terminateProcessTree(pid, 600*time.Millisecond)
	elapsed := time.Since(start)

	if _, err := cmd.Process.Wait(); err != nil {
		t.Logf("bash wait result: %v", err)
	}
	if elapsed < 500*time.Millisecond {
		t.Fatalf("terminate escalated too early: %s", elapsed)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("terminate took too long: %s", elapsed)
	}
	if err := syscall.Kill(-pid, 0); err != syscall.ESRCH {
		t.Fatalf("process group %d still alive after SIGKILL (err=%v)", pid, err)
	}
}

// TestTerminateProcessTreeDeadPID verifies the already-gone path returns
// immediately without hanging or panicking.
func TestTerminateProcessTreeDeadPID(t *testing.T) {
	start := time.Now()
	terminateProcessTree(9_999_999, 2*time.Second) // no such process group
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("terminate on dead pid took too long: %s", elapsed)
	}
}

func TestTerminalGraceDuration(t *testing.T) {
	// No deadline -> max grace.
	if g := terminalGraceDuration(context.Background()); g != 2*time.Second {
		t.Fatalf("no-deadline grace = %s, want 2s", g)
	}
	// Tight deadline -> half the remaining budget (with timing slack).
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	g := terminalGraceDuration(ctx)
	if g < 40*time.Millisecond || g > 60*time.Millisecond {
		t.Fatalf("tight-deadline grace = %s, want ~50ms", g)
	}
}

// TestTerminalShutdownCapturesSigtermMarker verifies the desktop-quit contract:
// a command trapping SIGTERM emits a final marker, and that marker is present
// in the persisted history after shutdownTerminals returns. The read loop must
// drain the final pty bytes into history before exit() closes the log.
//
// NOTE: this test requires real pty + signal delivery and is skipped unless
// OCODE_TEST_TERMINAL_SIGNALS=1 is set (see wantSignalTests): some sandboxed
// exec runners intercept kill() so the child survives, and pty.Start itself
// can fail with "operation not permitted" there.
func TestTerminalShutdownCapturesSigtermMarker(t *testing.T) {
	wantSignalTests(t)
	t.Setenv("SHELL", "/bin/sh")
	h, _, wsURL := terminalTestHandler(t)

	conn, _ := dialTerminal(t, wsURL, "term-shutdown-marker")
	// Run a foreground command that traps SIGTERM, prints a final marker to
	// the pty, and only then exits — the exact shape shutdown must preserve.
	if err := conn.WriteMessage(websocket.BinaryMessage, []byte("trap 'echo SHUTDOWN-MARKER; exit 0' TERM; echo READY; while true; do sleep 0.2; done\n")); err != nil {
		t.Fatalf("write trap command: %v", err)
	}
	readUntil(t, conn, "READY\r\n")
	sess := h.terminalSessions.lookup("term-shutdown-marker")
	if sess == nil {
		t.Fatal("session disappeared before shutdown")
	}
	project := sess.project
	if err := conn.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	waitFor(t, "detach", func() bool { return !sess.attached() })

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	h.shutdownTerminals(ctx)

	select {
	case <-sess.done:
	case <-time.After(10 * time.Second):
		t.Fatal("session did not complete shutdown")
	}
	data, _, err := readTerminalHistoryRange(project, "term-shutdown-marker", 0, terminalHistoryMaxPage)
	if err != nil {
		t.Fatalf("read persisted history: %v", err)
	}
	if !strings.Contains(string(data), "SHUTDOWN-MARKER") {
		t.Fatalf("persisted history missing SIGTERM marker, got %q", data)
	}
}

// TestTerminalShutdownAnonymousCapturesOutput verifies anonymous shells (no
// terminal_id, no disk history) still get a graceful SIGTERM and are reaped
// by shutdown: anonymous sessions live in the table so shutdown can reach
// them, but lookup deliberately hides them.
func TestTerminalShutdownAnonymousCapturesOutput(t *testing.T) {
	wantSignalTests(t)
	t.Setenv("SHELL", "/bin/sh")
	h, _, wsURL := terminalTestHandler(t)

	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	if resp != nil {
		resp.Body.Close()
	}
	t.Cleanup(func() { conn.Close() })
	if _, _, err := conn.ReadMessage(); err != nil {
		t.Fatalf("read attach: %v", err)
	}
	if n := h.terminalSessions.count(); n != 1 {
		t.Fatalf("expected exactly 1 anonymous session, got %d", n)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	h.shutdownTerminals(ctx)
	waitFor(t, "anonymous shutdown reap", func() bool { return h.terminalSessions.count() == 0 })
}

// TestTerminalShutdownConcurrent verifies simultaneous terminals shut down
// concurrently: N terminals each ignoring the first SIGTERM wave would take
// N×grace sequentially, but must complete within a single shared budget.
func TestTerminalShutdownConcurrent(t *testing.T) {
	wantSignalTests(t)
	t.Setenv("SHELL", "/bin/sh")
	h, _, wsURL := terminalTestHandler(t)

	const n = 3
	for i := 0; i < n; i++ {
		id := "term-shutdown-conc-" + strconv.Itoa(i)
		conn, _ := dialTerminal(t, wsURL, id)
		if err := conn.WriteMessage(websocket.BinaryMessage, []byte("trap 'exit 0' TERM; echo READY-"+id+"; while true; do sleep 0.2; done\n")); err != nil {
			t.Fatalf("write trap command: %v", err)
		}
		readUntil(t, conn, "READY-"+id+"\r\n")
		if err := conn.Close(); err != nil {
			t.Fatalf("close: %v", err)
		}
	}
	waitFor(t, "all detached", func() bool {
		for i := 0; i < n; i++ {
			if s := h.terminalSessions.lookup("term-shutdown-conc-" + strconv.Itoa(i)); s == nil || s.attached() {
				return false
			}
		}
		return true
	})

	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	h.shutdownTerminals(ctx)
	elapsed := time.Since(start)
	// Sequential 2s graces would take ~6s; concurrent shutdown stays near one.
	if elapsed > 4*time.Second {
		t.Fatalf("concurrent shutdown took %s, want well under sequential %s", elapsed, 3*terminalKillGrace)
	}
	waitFor(t, "all reaped", func() bool { return h.terminalSessions.count() == 0 })
}

// TestTerminalShutdownRefusesNewTerminals verifies the creation/shutdown race
// guard: once Shutdown begins (shutdownStarted set, as shutdownAgentSessions
// does first), new terminal creations and reattaches are refused with 503 so
// shutdown joins a bounded session set.
func TestTerminalShutdownRefusesNewTerminals(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	h, srv, _ := terminalTestHandler(t)

	h.shutdownMu.Lock()
	h.shutdownStarted = true
	h.shutdownMu.Unlock()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/terminal/ws"
	_, resp, err := websocket.DefaultDialer.Dial(wsURL+"?terminal_id=term-shutdown-race", nil)
	if err == nil {
		t.Fatal("expected dial to be refused during shutdown")
	}
	if resp == nil || resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %v", resp)
	}
}

// TestTerminalSealRefusesReserveAndPut is the deterministic (no pty, no
// signals) form of the shutdown/creation race: once the table is sealed,
// reserve() refuses with (nil, false, nil) and put() reports false without
// storing, so a pty.Start that raced the seal cannot escape termination.
func TestTerminalSealRefusesReserveAndPut(t *testing.T) {
	tab := newTerminalSessionTable()
	live := tab.sealForShutdown()
	if len(live) != 0 {
		t.Fatalf("fresh seal = %d live; want 0", len(live))
	}
	if existing, created, done := tab.reserve("term-sealed"); existing != nil || created || done != nil {
		t.Fatalf("sealed reserve = (%v, %v, %v); want (nil, false, nil)", existing, created, done)
	}
	sess := &terminalSession{id: "anon-sealed", done: make(chan struct{})}
	if tab.put(sess.id, sess) {
		t.Fatal("sealed put reported success; want refusal")
	}
	if n := tab.count(); n != 0 {
		t.Fatalf("sealed put stored %d session(s); want 0", n)
	}
}

// TestTerminalSealedCompleteCreateRefusesRacingPublish verifies the owner-side
// teardown contract: a reservation won before the seal whose pty.Start
// finishes after the seal is refused by completeCreate (stores nothing,
// reports false), so shutdown's sealed set stays exact and the owner — not
// shutdown — reaps the raced shell. This is the deterministic core of the
// pty.Start-vs-seal race, and it holds even when the shutdown context is
// already cancelled (no second snapshot wait involved).
func TestTerminalSealedCompleteCreateRefusesRacingPublish(t *testing.T) {
	tab := newTerminalSessionTable()
	_, created, done := tab.reserve("term-racing")
	if !created || done == nil {
		t.Fatalf("pre-seal reserve = created=%v done=%v; want (true, non-nil)", created, done)
	}
	live := tab.sealForShutdown()
	if len(live) != 0 {
		t.Fatalf("seal = %d live; want 0", len(live))
	}
	// The winner's pty.Start finished after the seal: completeCreate refuses,
	// stores nothing, but still wakes waiters so they observe a retryable
	// spawn failure instead of hanging.
	winner := &terminalSession{id: "term-racing", resumable: true, done: make(chan struct{})}
	if accepted := tab.completeCreate("term-racing", winner); accepted {
		t.Fatal("sealed completeCreate reported accepted; want refusal")
	}
	<-done
	if n := tab.count(); n != 0 {
		t.Fatalf("sealed completeCreate stored %d session(s); want 0", n)
	}
	if got := tab.lookup("term-racing"); got != nil {
		t.Fatalf("sealed lookup after refused publish = %v; want nil", got)
	}
}

// TestTerminalSealedRacingPublishTerminatedByOwner verifies the full race
// under a cancelled shutdown context: reserve, seal, cancel ctx, complete the
// raced spawn, and confirm the owner-side kill path reaps the shell and it is
// never registered. Uses a real pty shell; skipped without
// OCODE_TEST_TERMINAL_SIGNALS=1 like the other signal tests.
func TestTerminalSealedRacingPublishTerminatedByOwner(t *testing.T) {
	wantSignalTests(t)
	t.Setenv("SHELL", "/bin/sh")
	h := NewHandler()
	h.workDir = t.TempDir()
	h.SetTerminalAccessPolicy(false, true)

	_, created, done := h.terminalSessions.reserve("term-race-cancelled-ctx")
	if !created {
		t.Fatal("pre-seal reserve must succeed")
	}
	// Seal while pty.Start is "blocked", then cancel the shutdown context
	// before the spawn completes — the exact interleaving the old
	// wait-for-reservation loop got wrong.
	live := h.terminalSessions.sealForShutdown()
	if len(live) != 0 {
		t.Fatalf("seal = %d live; want 0", len(live))
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // shutdown deadline already expired when the spawn lands

	sess, err := h.startTerminalShell("term-race-cancelled-ctx", h.workDir, terminalShellCommand("/bin/sh"))
	if err != nil {
		t.Fatalf("spawn raced shell: %v", err)
	}
	if accepted := h.terminalSessions.completeCreate("term-race-cancelled-ctx", sess); accepted {
		t.Fatal("sealed completeCreate reported accepted; want refusal")
	}
	<-done
	// Owner-side teardown, exactly as HandleTerminalWS does on refusal.
	sess.kill()
	select {
	case <-sess.done:
	case <-time.After(10 * time.Second):
		t.Fatal("raced shell was not reaped by owner-side kill")
	}
	if n := h.terminalSessions.count(); n != 0 {
		t.Fatalf("table count = %d; want 0 (raced shell never registered)", n)
	}
	_ = ctx
}

// TestTerminalSealedLookupHidesLiveSession verifies reattach is refused after
// the seal: lookup reports nil even for a live session, so no new socket can
// resurrect a shell shutdown is about to terminate.
func TestTerminalSealedLookupHidesLiveSession(t *testing.T) {
	tab := newTerminalSessionTable()
	live := &terminalSession{id: "term-live", resumable: true, done: make(chan struct{})}
	if !tab.put("term-live", live) {
		t.Fatal("pre-seal put refused; want success")
	}
	if got := tab.lookup("term-live"); got != live {
		t.Fatalf("pre-seal lookup = %v; want live session", got)
	}
	tab.sealForShutdown()
	if got := tab.lookup("term-live"); got != nil {
		t.Fatalf("sealed lookup = %v; want nil", got)
	}
}

// TestTerminalAnonymousCleanupRemovesRegistryEntry verifies anonymous session
// teardown removes both the session table entry and the processes registry
// entry, so the Processes tab never shows a stale row for an exited shell.
func TestTerminalAnonymousCleanupRemovesRegistryEntry(t *testing.T) {
	h := NewHandler()
	sess := &terminalSession{id: "anon-cleanup", done: make(chan struct{})}
	h.terminalSessions.put(sess.id, sess)
	h.terminalProcs.register(sess.id, terminalProcEntry{Project: "/proj", PID: 1234})
	h.terminalExited(sess)
	if got := h.terminalSessions.count(); got != 0 {
		t.Fatalf("session table count = %d; want 0", got)
	}
	if entries := h.terminalProcs.snapshot(); len(entries) != 0 {
		t.Fatalf("proc registry = %v; want empty", entries)
	}
}
