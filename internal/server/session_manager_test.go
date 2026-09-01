package server

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/session"
)

// TestAppendLiveFrameBuffersDuringActiveTurn reproduces the reload-loses-
// streaming-text bug at the SessionManager layer: while a turn is active,
// text/thinking/tool_* frames accumulate and come back from State() in order;
// a non-streaming event type (e.g. "messages") is ignored; and the buffer
// clears on both the turn-start and turn-end transitions, mirroring
// broadcastEvent's real call pattern (see handler.go's broadcastEvent, which
// calls appendLiveFrame after every publish).
func TestAppendLiveFrameBuffersDuringActiveTurn(t *testing.T) {
	mgr := NewSessionManager(time.Minute, func() []string { return nil }, nil)
	id := session.NewSessionID()
	mgr.Register(id, t.TempDir())

	// No active turn yet: frames are dropped.
	mgr.appendLiveFrame(id, "text", map[string]string{"delta": "dropped"}, 1)
	state, ok := mgr.State(id)
	if !ok {
		t.Fatal("session not found")
	}
	if len(state.LiveFrames) != 0 {
		t.Fatalf("frame buffered with no active turn: %+v", state.LiveFrames)
	}

	mgr.setTurnActive(id, true)
	mgr.appendLiveFrame(id, "text", map[string]string{"delta": "hel"}, 2)
	mgr.appendLiveFrame(id, "text", map[string]string{"delta": "lo"}, 3)
	mgr.appendLiveFrame(id, "tool_start", map[string]string{"tool": "bash"}, 4)
	// Not in liveFrameEvents — must not be buffered.
	mgr.appendLiveFrame(id, "messages", []string{"final"}, 5)

	state, ok = mgr.State(id)
	if !ok {
		t.Fatal("session not found")
	}
	if len(state.LiveFrames) != 3 {
		t.Fatalf("LiveFrames = %d frames, want 3: %+v", len(state.LiveFrames), state.LiveFrames)
	}
	wantEvents := []string{"text", "text", "tool_start"}
	wantSeqs := []uint64{2, 3, 4}
	for i, f := range state.LiveFrames {
		if f.Event != wantEvents[i] || f.Seq != wantSeqs[i] {
			t.Fatalf("frame %d = {%q, seq %d}, want {%q, seq %d}", i, f.Event, f.Seq, wantEvents[i], wantSeqs[i])
		}
	}

	// Turn ends: the authoritative `messages` broadcast supersedes the buffer.
	mgr.setTurnActive(id, false)
	state, ok = mgr.State(id)
	if !ok {
		t.Fatal("session not found")
	}
	if len(state.LiveFrames) != 0 {
		t.Fatalf("LiveFrames not cleared after turn end: %+v", state.LiveFrames)
	}
}

// TestAppendLiveFrameClearedWithTurnActive locks down the invariant the fix
// relies on: liveFrames and turnActive are only ever mutated together, under
// the same lock (setTurnActive), so State() can never observe a stale buffer
// alongside turn_active: false — which would replay already-superseded
// streaming text after the authoritative `messages` broadcast already landed.
func TestAppendLiveFrameClearedWithTurnActive(t *testing.T) {
	mgr := NewSessionManager(time.Minute, func() []string { return nil }, nil)
	id := session.NewSessionID()
	mgr.Register(id, t.TempDir())

	for i := 0; i < 5; i++ {
		mgr.setTurnActive(id, true)
		mgr.appendLiveFrame(id, "text", map[string]string{"delta": "x"}, uint64(i))
		state, ok := mgr.State(id)
		if !ok || len(state.LiveFrames) != 1 {
			t.Fatalf("iteration %d: expected 1 buffered frame while active, got %+v", i, state.LiveFrames)
		}
		mgr.setTurnActive(id, false)
		state, ok = mgr.State(id)
		if !ok {
			t.Fatal("session not found")
		}
		if state.TurnActive {
			t.Fatalf("iteration %d: TurnActive still true after setTurnActive(false)", i)
		}
		if len(state.LiveFrames) != 0 {
			t.Fatalf("iteration %d: TurnActive false but LiveFrames non-empty: %+v", i, state.LiveFrames)
		}
	}
}

// TestAppendLiveFrameByteCapTruncatesHead ensures a very long streaming reply
// (or runaway tool_output) cannot grow a session's buffer unbounded — older
// frames drop first once liveFramesByteCap is exceeded.
func TestAppendLiveFrameByteCapTruncatesHead(t *testing.T) {
	mgr := NewSessionManager(time.Minute, func() []string { return nil }, nil)
	id := session.NewSessionID()
	mgr.Register(id, t.TempDir())
	mgr.setTurnActive(id, true)

	chunk := make([]byte, 1024)
	for i := range chunk {
		chunk[i] = 'x'
	}
	// Comfortably exceed liveFramesByteCap (256KB) so head frames are evicted.
	const frames = 300
	for i := 0; i < frames; i++ {
		mgr.appendLiveFrame(id, "text", map[string]string{"delta": string(chunk)}, uint64(i))
	}

	state, ok := mgr.State(id)
	if !ok {
		t.Fatal("session not found")
	}
	if len(state.LiveFrames) == 0 || len(state.LiveFrames) >= frames {
		t.Fatalf("expected head-truncation, got %d/%d frames", len(state.LiveFrames), frames)
	}
	// The oldest surviving frame must not be seq 0 — that's the head-truncation
	// contract (drop oldest first), and the newest frame must be the last one
	// appended (nothing dropped from the tail).
	if state.LiveFrames[0].Seq == 0 {
		t.Fatalf("head frame (seq 0) was not truncated")
	}
	if last := state.LiveFrames[len(state.LiveFrames)-1].Seq; last != frames-1 {
		t.Fatalf("tail frame seq = %d, want %d", last, frames-1)
	}
}

// saveSessionToDir persists a session into the storage dir for wd, so tests
// can plant sessions under arbitrary project roots without touching the
// process-global workdir (the override is reset on test cleanup).
func saveSessionToDir(t *testing.T, wd, id string) {
	t.Helper()
	session.SetWorkDir(wd)
	t.Cleanup(func() { session.SetWorkDir("") })
	if err := session.Save(id, "project session", []agent.Message{{Role: "user", Content: "hello"}}, nil); err != nil {
		t.Fatalf("save session %s under %s: %v", id, wd, err)
	}
}

// removeSessionFileForDir deletes the on-disk session file for id under wd's
// storage dir (used to prove Resolve's cache answers without the disk).
func removeSessionFileForDir(t *testing.T, wd, id string) error {
	t.Helper()
	dir, err := session.GetStorageDirForPath(wd)
	if err != nil {
		return err
	}
	for _, suffix := range []string{".sqlite", ".ojsonl", ".json"} {
		if err := os.Remove(filepath.Join(dir, id+suffix)); err == nil {
			return nil
		}
	}
	// Also clean up the sqlite index row if present — not required for
	// Resolve (which scans the directory), but keeps the temp dir tidy.
	return fmt.Errorf("no session file found for %s under %s", id, wd)
}

// TestSessionManagerResolveAcrossProjects is the core cross-project test:
// two project roots each hold a session on disk; Resolve finds each session's
// correct root; unknown IDs error; a second Resolve of the same ID hits the
// cache (the file is gone by then, so only the cache could answer).
func TestSessionManagerResolveAcrossProjects(t *testing.T) {
	rootA := t.TempDir()
	rootB := t.TempDir()
	idA := session.NewSessionID()
	idB := session.NewSessionID()
	saveSessionToDir(t, rootA, idA)
	saveSessionToDir(t, rootB, idB)

	mgr := NewSessionManager(30*time.Minute, func() []string { return []string{rootA, rootB} }, nil)

	eA, err := mgr.Resolve(idA)
	if err != nil {
		t.Fatalf("resolve %s: %v", idA, err)
	}
	if eA.ProjectRoot != rootA {
		t.Fatalf("resolve %s: root = %q, want %q", idA, eA.ProjectRoot, rootA)
	}
	eB, err := mgr.Resolve(idB)
	if err != nil {
		t.Fatalf("resolve %s: %v", idB, err)
	}
	if eB.ProjectRoot != rootB {
		t.Fatalf("resolve %s: root = %q, want %q", idB, eB.ProjectRoot, rootB)
	}

	if _, err := mgr.Resolve("ses_missing"); err == nil {
		t.Fatal("expected error for unknown session id")
	}

	// Cache hit: the file is removed, so only the cached mapping can answer.
	if err := removeSessionFileForDir(t, rootA, idA); err != nil {
		t.Fatal(err)
	}
	if e, err := mgr.Resolve(idA); err != nil || e.ProjectRoot != rootA {
		t.Fatalf("cached resolve: entry=%+v err=%v, want root %q", e, err, rootA)
	}
}

// TestSessionManagerRegisterBindsExplicitRoot verifies explicit registration
// and that it wins over an earlier implicit resolution.
func TestSessionManagerRegisterBindsExplicitRoot(t *testing.T) {
	rootA := t.TempDir()
	rootB := t.TempDir()
	id := session.NewSessionID()
	saveSessionToDir(t, rootA, id)

	mgr := NewSessionManager(30*time.Minute, func() []string { return []string{rootA, rootB} }, nil)

	// Implicit resolution finds rootA (only place the file exists).
	e, err := mgr.Resolve(id)
	if err != nil {
		t.Fatal(err)
	}
	if e.ProjectRoot != rootA {
		t.Fatalf("implicit root = %q, want %q", e.ProjectRoot, rootA)
	}

	// Explicit Register to rootB (client-supplied project_path) wins.
	mgr.Register(id, rootB)
	if got := mgr.Lookup(id).ProjectRoot; got != rootB {
		t.Fatalf("after register: root = %q, want %q", got, rootB)
	}
}

// TestEvictIdleReleasesStaleAgents covers Task 4: an entry with a built agent
// idle past the threshold is released (agent nil afterwards, entry still
// resolvable); an entry with an active turn is not evicted.
func TestEvictIdleReleasesStaleAgents(t *testing.T) {
	mgr := NewSessionManager(time.Minute, func() []string { return nil }, nil)

	staleID := session.NewSessionID()
	staleEntry := mgr.Register(staleID, t.TempDir())
	mgr.setAgent(staleID, &agentSession{})
	staleEntry.lastActivity = time.Now().Add(-2 * time.Minute)

	activeID := session.NewSessionID()
	mgr.Register(activeID, t.TempDir())
	mgr.setAgent(activeID, &agentSession{})
	mgr.setTurnActive(activeID, true)
	mgr.Lookup(activeID).lastActivity = time.Now().Add(-2 * time.Minute)

	evicted := mgr.EvictIdle()
	if len(evicted) != 1 || evicted[0] != staleID {
		t.Fatalf("evicted = %v, want [%s]", evicted, staleID)
	}

	if got := mgr.Lookup(staleID); got == nil || got.agent != nil {
		t.Fatalf("stale entry should survive eviction without an agent, got %+v", got)
	}
	// Entry remains resolvable even though the agent is gone.
	if e, err := mgr.Resolve(staleID); err != nil || e == nil {
		t.Fatalf("evicted entry must stay resolvable: %v", err)
	}
	if got := mgr.Lookup(activeID); got == nil || got.agent == nil {
		t.Fatalf("active-turn entry must keep its agent, got %+v", got)
	}
}

// TestEvictIdleNotifiesHandler verifies the onEvict callback fires for
// evicted sessions (used to drop the h.agents mirror entry).
func TestEvictIdleNotifiesHandler(t *testing.T) {
	var evicted []string
	mgr := NewSessionManager(time.Minute, func() []string { return nil }, func(id string) {
		evicted = append(evicted, id)
	})
	id := session.NewSessionID()
	mgr.Register(id, t.TempDir())
	mgr.setAgent(id, &agentSession{})
	mgr.Lookup(id).lastActivity = time.Now().Add(-2 * time.Minute)

	mgr.EvictIdle()
	if len(evicted) != 1 || evicted[0] != id {
		t.Fatalf("onEvict got %v, want [%s]", evicted, id)
	}
}

// TestReleaseAgentReleasesImmediately verifies ReleaseAgent tears down a
// resident agent on demand (not gated on idle time), keeps the registry
// entry, and notifies the handler via onEvict.
func TestReleaseAgentReleasesImmediately(t *testing.T) {
	var evicted []string
	mgr := NewSessionManager(30*time.Minute, func() []string { return nil }, func(id string) {
		evicted = append(evicted, id)
	})
	id := session.NewSessionID()
	entry := mgr.Register(id, t.TempDir())
	mgr.setAgent(id, &agentSession{})

	if released := mgr.ReleaseAgent(id); !released {
		t.Fatal("ReleaseAgent should report release of a resident agent")
	}
	if entry.agent != nil {
		t.Fatal("ReleaseAgent left the resident agent attached")
	}
	if len(evicted) != 1 || evicted[0] != id {
		t.Fatalf("onEvict got %v, want [%s]", evicted, id)
	}
	// A second release on the now-agentless entry is a no-op.
	if released := mgr.ReleaseAgent(id); released {
		t.Fatal("ReleaseAgent should no-op for an agentless entry")
	}
}

// TestReleaseAgentSkipsActiveTurn verifies ReleaseAgent does not tear down an
// agent while its turn is running (the cancel handler aborts the turn; idle
// eviction reclaims the agent afterwards).
func TestReleaseAgentSkipsActiveTurn(t *testing.T) {
	mgr := NewSessionManager(30*time.Minute, func() []string { return nil }, nil)
	id := session.NewSessionID()
	entry := mgr.Register(id, t.TempDir())
	mgr.setAgent(id, &agentSession{})
	mgr.setTurnActive(id, true)

	if released := mgr.ReleaseAgent(id); released {
		t.Fatal("ReleaseAgent must not release an agent with an active turn")
	}
	if entry.agent == nil {
		t.Fatal("active-turn entry lost its agent")
	}
	mgr.setTurnActive(id, false)
	if released := mgr.ReleaseAgent(id); !released {
		t.Fatal("ReleaseAgent should release after the turn unwinds")
	}
	if entry.agent != nil {
		t.Fatal("agent still attached after post-turn release")
	}
}
