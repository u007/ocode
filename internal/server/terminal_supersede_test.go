//go:build !windows

package server

import (
	"encoding/json"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// A second client attaching to the same terminal id takes the single
// attachment slot, and the server closes the socket it displaces. The close is
// clean (1000), byte-identical to a shell that exited, so without an explicit
// reason frame the displaced client renders "[terminal session ended]" for a
// shell that is still running — and its auto-reconnect (backoff timer +
// onWake) re-attaches, displacing the new client, which displaces it again.
//
// This pins the reason frame on the wire AND that it survives the handoff: the
// frame must arrive before the close, and the surviving socket must still own
// the same shell.
func TestTerminalSupersededSocketReceivesDetachFrame(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	requirePTY(t)
	h, _, wsURL := terminalTestHandler(t)

	first, resumed := dialTerminal(t, wsURL, "term-supersede")
	if resumed {
		t.Fatal("fresh terminal must not report resumed=true")
	}
	if err := first.WriteMessage(websocket.BinaryMessage, []byte("echo before-takeover\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	readUntil(t, first, "before-takeover\r\n")
	pidBefore := h.terminalProcs.snapshot()["term-supersede"].PID
	if pidBefore <= 0 {
		t.Fatal("terminal was not registered")
	}

	// Second client takes the slot.
	second, resumed := dialTerminal(t, wsURL, "term-supersede")
	if !resumed {
		t.Fatal("reattach by the second client must report resumed=true")
	}

	// The displaced socket must be told WHY before it goes away. Binary frames
	// are pty output that may still be buffered from before the takeover, so
	// skip them and assert on the first TEXT frame — the detach reason.
	deadline := time.Now().Add(10 * time.Second)
	var mt int
	var data []byte
	for {
		if err := first.SetReadDeadline(deadline); err != nil {
			t.Fatalf("SetReadDeadline: %v", err)
		}
		var err error
		mt, data, err = first.ReadMessage()
		if err != nil {
			t.Fatalf("displaced socket got no detach frame before close: %v", err)
		}
		if mt == websocket.TextMessage {
			break
		}
	}
	if mt != websocket.TextMessage {
		t.Fatalf("detach frame must be text, got type %d: %q", mt, data)
	}
	var msg terminalDetachMsg
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Fatalf("decode detach frame %q: %v", data, err)
	}
	if msg.Type != "detached" {
		t.Fatalf("detach frame type = %q, want \"detached\" (frame %q)", msg.Type, data)
	}
	if msg.Reason != terminalDetachReasonSuperseded {
		t.Fatalf("detach reason = %q, want %q (frame %q)", msg.Reason, terminalDetachReasonSuperseded, data)
	}

	// The shell survives the handoff and the surviving socket owns it.
	if pidAfter := h.terminalProcs.snapshot()["term-supersede"].PID; pidAfter != pidBefore {
		t.Fatalf("takeover spawned a new shell: pid %d -> %d", pidBefore, pidAfter)
	}
	if err := second.WriteMessage(websocket.BinaryMessage, []byte("echo after-takeover\n")); err != nil {
		t.Fatalf("write on surviving socket: %v", err)
	}
	readUntil(t, second, "after-takeover\r\n")
}

// The displaced socket must be told WHY and then actually closed, so the
// client stops writing to a pty it no longer owns. Loosely checking "eventually
// a read error" would pass even with no frame at all, so this asserts the exact
// sequence: detach frame, then close.
func TestTerminalSupersededSocketIsClosedAfterDetachFrame(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	requirePTY(t)
	_, _, wsURL := terminalTestHandler(t)

	first, _ := dialTerminal(t, wsURL, "term-supersede-close")
	dialTerminal(t, wsURL, "term-supersede-close")

	// Phase 1: a detach frame must arrive.
	deadline := time.Now().Add(10 * time.Second)
	sawFrame := false
	for !sawFrame {
		if err := first.SetReadDeadline(deadline); err != nil {
			t.Fatalf("SetReadDeadline: %v", err)
		}
		mt, data, err := first.ReadMessage()
		if err != nil {
			t.Fatalf("socket closed without a detach frame: %v", err)
		}
		if mt != websocket.TextMessage {
			continue // buffered pty output
		}
		var msg terminalDetachMsg
		if err := json.Unmarshal(data, &msg); err != nil {
			t.Fatalf("decode detach frame %q: %v", data, err)
		}
		if msg.Type != "detached" || msg.Reason != terminalDetachReasonSuperseded {
			t.Fatalf("unexpected control frame %q, want a superseded detach", data)
		}
		sawFrame = true
	}

	// Phase 2: the socket must then close.
	for {
		if err := first.SetReadDeadline(deadline); err != nil {
			t.Fatalf("SetReadDeadline: %v", err)
		}
		if _, _, err := first.ReadMessage(); err != nil {
			return
		}
	}
}

// Several clients racing to attach the same terminal id must converge on
// exactly one attachment: each loser sees a detach frame and a close, and the
// shell is never respawned. Run with -race — this is the path where a
// double-close or a lost detach frame would let two clients think they own the
// pty.
func TestTerminalConcurrentAttachLeavesSingleOwner(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	requirePTY(t)
	h, _, wsURL := terminalTestHandler(t)

	const clients = 6
	var wg sync.WaitGroup
	conns := make([]*websocket.Conn, clients)
	resumed := make([]bool, clients)
	for i := 0; i < clients; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			conn, res := dialTerminal(t, wsURL, "term-race")
			conns[i] = conn
			resumed[i] = res
		}(i)
	}
	wg.Wait()

	// Exactly one shell for one terminal id, no matter how many clients raced.
	if procs := h.terminalProcs.snapshot(); len(procs) != 1 {
		t.Fatalf("terminal registry has %d entries, want 1: %v", len(procs), procs)
	}
	pid := h.terminalProcs.snapshot()["term-race"].PID
	if pid <= 0 {
		t.Fatal("terminal was not registered")
	}

	// Exactly one socket may still be attached. Poll for convergence: a loser's
	// close lands asynchronously after its own attach already returned, so the
	// invariant settles rather than holding at the instant the goroutines join.
	waitFor(t, "single attached socket", func() bool {
		sess := h.terminalSessions.lookup("term-race")
		return sess != nil && sess.attached()
	})

	// Drain every socket to a terminal state. A single ReadMessage is not
	// enough: a displaced socket can still hold buffered pty output, so its
	// first read succeeds and only a later one reports the close. A read
	// deadline (the survivor simply has nothing to say) is NOT a close and is
	// reported separately so it cannot masquerade as one.
	const drainWait = 3 * time.Second
	closed, timedOut := 0, 0
	for i, c := range conns {
		if c == nil {
			t.Fatalf("client %d never connected", i)
		}
		deadline := time.Now().Add(drainWait)
		for {
			if err := c.SetReadDeadline(deadline); err != nil {
				t.Fatalf("SetReadDeadline: %v", err)
			}
			_, _, err := c.ReadMessage()
			if err == nil {
				continue // buffered output; keep draining
			}
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				timedOut++
				break
			}
			closed++
			break
		}
	}
	if timedOut != 1 {
		t.Fatalf("%d sockets still streaming after the race, want exactly 1 survivor (closed=%d)", timedOut, closed)
	}
	if closed != clients-1 {
		t.Fatalf("%d displaced sockets closed, want %d", closed, clients-1)
	}

	// The shell survived every handoff.
	if after := h.terminalProcs.snapshot()["term-race"].PID; after != pid {
		t.Fatalf("concurrent attach respawned the shell: pid %d -> %d", pid, after)
	}

	// Exactly one client saw a fresh shell; the rest resumed the same one.
	fresh := 0
	for _, r := range resumed {
		if !r {
			fresh++
		}
	}
	if fresh != 1 {
		t.Fatalf("%d clients saw a fresh shell, want exactly 1", fresh)
	}
}
