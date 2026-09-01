package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/session"
)

// cancelRequest hits HandleCancelSession for id and returns the recorder.
func cancelRequest(h *Handler, id string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.HandleCancelSession(rec, httptest.NewRequest("POST", "/api/sessions/"+id+"/cancel", nil), id)
	return rec
}

// TestCancelIdleSessionIsNoOp verifies that cancelling a session with no
// in-flight work does not record a pendingCancel flag. A stale flag would
// poison the NEXT turn: executeTurnJob consumes pendingCancel at start and
// publishes a cancelled turn_error without running the user's message.
func TestCancelIdleSessionIsNoOp(t *testing.T) {
	h := NewHandler()
	proj := t.TempDir()
	id := session.NewSessionID()
	h.sessions.Register(id, proj)

	rec := cancelRequest(h, id)
	if rec.Code != http.StatusOK {
		t.Fatalf("cancel status %d, want 200", rec.Code)
	}
	h.cancelMu.Lock()
	_, poisoned := h.pendingCancel[id]
	h.cancelMu.Unlock()
	if poisoned {
		t.Fatal("idle cancel recorded a pendingCancel flag — would poison the next turn")
	}

	// The next real turn must run normally (no swallowed cancellation).
	as := newTestSession(h, id, &instantClient{})
	content, err := h.runTurn(id, as, "hi", turnOptions{})
	if err != nil {
		t.Fatalf("runTurn after idle cancel: %v", err)
	}
	if content == "" {
		t.Fatal("turn produced no output after idle cancel")
	}
}

// TestCancelClearsStalePendingFlag verifies that a stale pendingCancel left by
// an in-flight cancel is cleared when the session later goes idle, so a
// subsequent fire-and-forget cancel (e.g. closing a session tab) cannot
// deliver a poisoned flag to the next turn.
func TestCancelClearsStalePendingFlag(t *testing.T) {
	h := NewHandler()
	proj := t.TempDir()
	id := session.NewSessionID()
	h.sessions.Register(id, proj)

	// Simulate the stale state: a flag left by an earlier cancel (e.g. the
	// cancel raced the end of a turn and the job exited before consuming).
	h.cancelMu.Lock()
	if h.pendingCancel == nil {
		h.pendingCancel = make(map[string]bool)
	}
	h.pendingCancel[id] = true
	h.cancelMu.Unlock()

	rec := cancelRequest(h, id)
	if rec.Code != http.StatusOK {
		t.Fatalf("cancel status %d, want 200", rec.Code)
	}
	h.cancelMu.Lock()
	_, still := h.pendingCancel[id]
	h.cancelMu.Unlock()
	if still {
		t.Fatal("idle cancel did not clear the stale pendingCancel flag")
	}

	// The next turn must not be swallowed.
	as := newTestSession(h, id, &instantClient{})
	content, err := h.runTurn(id, as, "hi", turnOptions{})
	if err != nil {
		t.Fatalf("runTurn after stale-flag cleanup: %v", err)
	}
	if content == "" {
		t.Fatal("turn produced no output after stale-flag cleanup")
	}
}

// TestCancelActiveTurnRecordsFlagAndCancelsAgent verifies that cancelling a
// session with a running turn records the flag and cancels the agent.
func TestCancelActiveTurnRecordsFlagAndCancelsAgent(t *testing.T) {
	h := NewHandler()
	proj := t.TempDir()
	id := session.NewSessionID()
	h.sessions.Register(id, proj)

	blocking := newBlockingClient()
	as := newTestSession(h, id, blocking)

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = h.runTurn(id, as, "slow", turnOptions{}) //nolint:errcheck
	}()
	select {
	case <-blocking.started:
	case <-time.After(3 * time.Second):
		t.Fatal("turn never started")
	}

	rec := cancelRequest(h, id)
	if rec.Code != http.StatusOK {
		t.Fatalf("cancel status %d, want 200", rec.Code)
	}
	if !as.agent.Cancelled() {
		t.Fatal("agent was not cancelled by HandleCancelSession")
	}

	close(blocking.release)
	<-done
}

// TestCancelDuringBootstrapHonored verifies that a cancel arriving while the
// agent is being built (bootstrap phase, agent not registered yet) is held in
// pendingCancel and observed by executeTurnJob — the classic web/desktop Stop
// during a slow first-turn bootstrap.
func TestCancelDuringBootstrapHonored(t *testing.T) {
	h := NewHandler()
	proj := t.TempDir()
	id := session.NewSessionID()
	h.sessions.Register(id, proj)
	// Stall the bootstrap at the MCP stage so the agent is never registered.
	h.mcpCache = &mcpCache{ready: make(chan struct{})}

	job, err := h.dispatchTurn(id, "opencode-go/deepseek-v4-flash", "hi", turnOptions{})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	// Wait for the persistAck so the message is durable and the job is past
	// the point of no return.
	select {
	case <-job.persistAck:
	case <-time.After(3 * time.Second):
		t.Fatal("persistAck never arrived")
	}

	// Flip the MCP cache ready so bootstrap can proceed, then cancel in the
	// window where the agent is being built. The pendingCancel flag must be
	// recorded because a job is in flight (turnInFlight counter).
	close(h.mcpCache.ready)
	rec := cancelRequest(h, id)
	if rec.Code != http.StatusOK {
		t.Fatalf("cancel status %d, want 200", rec.Code)
	}
	h.cancelMu.Lock()
	got := h.pendingCancel[id]
	h.cancelMu.Unlock()
	if !got {
		t.Fatal("cancel during in-flight bootstrap did not record pendingCancel")
	}
}

// TestCancelDispatchRaceWindowRecordsFlag verifies that a cancel arriving
// after dispatchTurn but before the job goroutine's first pendingCancel check
// is still honored: the turnInFlight counter marks the session active, so the
// cancel records the flag that the job then consumes.
func TestCancelDispatchRaceWindowRecordsFlag(t *testing.T) {
	h := NewHandler()
	proj := t.TempDir()
	id := session.NewSessionID()
	h.sessions.Register(id, proj)

	// Register the job exactly like dispatchTurn does (counter increment
	// happens before the goroutine starts; this test calls the primitive
	// directly, skipping the goroutine, to model the dispatch window).
	job := &turnJob{content: "hi", persistAck: make(chan struct{})}
	h.cancelMu.Lock()
	if h.turnInFlight == nil {
		h.turnInFlight = make(map[string]int)
	}
	h.turnInFlight[id]++
	h.cancelMu.Unlock()

	rec := cancelRequest(h, id)
	if rec.Code != http.StatusOK {
		t.Fatalf("cancel status %d, want 200", rec.Code)
	}
	h.cancelMu.Lock()
	got := h.pendingCancel[id]
	h.cancelMu.Unlock()
	if !got {
		t.Fatal("cancel during dispatch window did not record pendingCancel")
	}

	// Release the counter as the job goroutine would.
	h.cancelMu.Lock()
	delete(h.turnInFlight, id)
	h.cancelMu.Unlock()
	_ = job
}

// TestCancelActiveTurnCancelsSubagents verifies that Stop terminates
// background sub-agent runs for the session, mirroring TUI Escape (the
// pendingCancel flag plus Runs().CancelAll()).
func TestCancelActiveTurnCancelsSubagents(t *testing.T) {
	h := NewHandler()
	proj := t.TempDir()
	id := session.NewSessionID()
	h.sessions.Register(id, proj)

	blocking := newBlockingClient()
	as := newTestSession(h, id, blocking)

	// Register a fake active run in the agent's run registry so CancelAll
	// has something to terminate.
	run := as.agent.Runs().New("sub")
	cancelled := make(chan struct{})
	run.Cancel = func() { close(cancelled) }

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = h.runTurn(id, as, "slow", turnOptions{}) //nolint:errcheck
	}()
	select {
	case <-blocking.started:
	case <-time.After(3 * time.Second):
		t.Fatal("turn never started")
	}

	rec := cancelRequest(h, id)
	if rec.Code != http.StatusOK {
		t.Fatalf("cancel status %d, want 200", rec.Code)
	}
	select {
	case <-cancelled:
		// sub-agent cancel function invoked
	case <-time.After(3 * time.Second):
		t.Fatal("sub-agent run was not cancelled by HandleCancelSession")
	}

	close(blocking.release)
	<-done
	_ = run
}

// TestCancelTwiceIdempotent verifies repeated cancels are safe.
func TestCancelTwiceIdempotent(t *testing.T) {
	h := NewHandler()
	proj := t.TempDir()
	id := session.NewSessionID()
	h.sessions.Register(id, proj)

	rec := cancelRequest(h, id)
	if rec.Code != http.StatusOK {
		t.Fatalf("first cancel status %d", rec.Code)
	}
	rec = cancelRequest(h, id)
	if rec.Code != http.StatusOK {
		t.Fatalf("second cancel status %d", rec.Code)
	}
}

// compile-time check that agent import is used (blockingClient implements
// agent.LLMClient).
var _ agent.LLMClient = (*instantClient)(nil)

// closeRequest hits HandleCloseSession for id and returns the recorder.
func closeRequest(h *Handler, id string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.HandleCloseSession(rec, httptest.NewRequest("POST", "/api/sessions/"+id+"/close", nil), id)
	return rec
}

// TestCloseSessionReleasesResidentAgent verifies the stop/close distinction:
// closing a session releases the resident agent (backend resources freed),
// while the registry entry survives for reopen.
func TestCloseSessionReleasesResidentAgent(t *testing.T) {
	h := NewHandler()
	proj := t.TempDir()
	id := session.NewSessionID()
	h.sessions.Register(id, proj)
	// Mirror the real registration flow: both h.agents and the manager entry
	// must see the agent, or ReleaseAgent (which reads the entry) no-ops.
	as := newTestSession(h, id, &instantClient{})
	h.sessions.setAgent(id, as)

	if h.lookupAgentSession(id) == nil {
		t.Fatal("precondition: resident agent should exist")
	}

	rec := closeRequest(h, id)
	if rec.Code != http.StatusOK {
		t.Fatalf("close status %d, want 200", rec.Code)
	}
	if h.lookupAgentSession(id) != nil {
		t.Fatal("close did not release the resident agent")
	}
	// The registry entry must survive (session reopenable).
	if h.sessions.Lookup(id) == nil {
		t.Fatal("close removed the registry entry — session should stay reopenable")
	}
}

// TestCloseSessionActiveTurnSkipsRelease verifies a session with a running
// turn is not released mid-turn, but the close-pending marker is drained the
// moment the turn unwinds — the agent is torn down without waiting for the
// idle-eviction loop.
func TestCloseSessionActiveTurnSkipsRelease(t *testing.T) {
	h := NewHandler()
	proj := t.TempDir()
	id := session.NewSessionID()
	h.sessions.Register(id, proj)

	blocking := newBlockingClient()
	as := newTestSession(h, id, blocking)
	h.sessions.setAgent(id, as)

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = h.runTurn(id, as, "slow", turnOptions{}) //nolint:errcheck
	}()
	select {
	case <-blocking.started:
	case <-time.After(3 * time.Second):
		t.Fatal("turn never started")
	}

	rec := closeRequest(h, id)
	if rec.Code != http.StatusOK {
		t.Fatalf("close status %d, want 200", rec.Code)
	}
	if !as.agent.Cancelled() {
		t.Fatal("close did not cancel the in-flight turn")
	}
	// Mid-turn the agent must still be resident (the close marked it
	// close-pending instead of tearing down mid-Step).
	if h.lookupAgentSession(id) == nil {
		t.Fatal("agent was released while the turn was mid-flight")
	}

	// Turn unwinds — the drain must release the agent automatically.
	close(blocking.release)
	<-done
	h.drainPendingClose(id)
	if h.lookupAgentSession(id) != nil {
		t.Fatal("close-pending drain did not drop the resident agent after turn unwind")
	}
}

// TestCloseDuringBootstrapReleasesNewlyRegisteredAgent verifies issue #2: a
// close arriving while the agent is mid-bootstrap (not yet registered) marks
// the session close-pending; when the bootstrap completes and registers the
// agent, the turn job's drain releases it instead of stranding it until idle
// eviction.
func TestCloseDuringBootstrapReleasesNewlyRegisteredAgent(t *testing.T) {
	h := NewHandler()
	proj := t.TempDir()
	id := session.NewSessionID()
	h.sessions.Register(id, proj)
	// Stall the bootstrap at the MCP stage so the agent is never registered.
	h.mcpCache = &mcpCache{ready: make(chan struct{})}

	job, err := h.dispatchTurn(id, "opencode-go/deepseek-v4-flash", "hi", turnOptions{})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	select {
	case <-job.persistAck:
	case <-time.After(3 * time.Second):
		t.Fatal("persistAck never arrived")
	}

	// Close while the agent is still being built (no resident agent yet).
	rec := closeRequest(h, id)
	if rec.Code != http.StatusOK {
		t.Fatalf("close status %d, want 200", rec.Code)
	}
	h.cancelMu.Lock()
	pending := h.closePending[id]
	h.cancelMu.Unlock()
	if !pending {
		t.Fatal("close during bootstrap should mark the session close-pending")
	}

	// Let bootstrap finish; the job registers the agent, observes the pending
	// cancel, and the deferred drain must release it. Wait until the job
	// goroutine has fully exited (turnInFlight back to 0) — that is the point
	// where the deferred drain has run.
	close(h.mcpCache.ready)
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		h.cancelMu.Lock()
		inflight := h.turnInFlight[id]
		h.cancelMu.Unlock()
		if inflight == 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	h.cancelMu.Lock()
	inflight := h.turnInFlight[id]
	h.cancelMu.Unlock()
	if inflight != 0 {
		t.Fatalf("turn job did not finish (turnInFlight=%d)", inflight)
	}
	if as := h.lookupAgentSession(id); as != nil {
		t.Fatal("bootstrap-registered agent was not released by the close-pending drain")
	}
	h.cancelMu.Lock()
	still := h.closePending[id]
	h.cancelMu.Unlock()
	if still {
		t.Fatal("close-pending marker not cleared after the agent was released")
	}
}

// TestCancelIdleWithPendingMessagesDoesNotPoisonRetry verifies the advisor's
// pending-but-idle case: a failed bootstrap can leave persisted-but-unturned
// messages (PendingCount > 0) while the session is idle (no active turn, no
// dispatched job). A cancel arriving then must NOT record pendingCancel —
// executeTurnJob would consume it at the next turn's start and swallow the
// user's retry message. The flag is only meaningful when a live job or active
// turn can observe and consume it.
func TestCancelIdleWithPendingMessagesDoesNotPoisonRetry(t *testing.T) {
	h := NewHandler()
	proj := t.TempDir()
	id := session.NewSessionID()
	h.sessions.Register(id, proj)
	// Simulate a failed bootstrap: a pending message with no resident agent
	// and no turn in flight.
	h.sessions.PushPending(id, "retry me")

	rec := cancelRequest(h, id)
	if rec.Code != http.StatusOK {
		t.Fatalf("cancel status %d, want 200", rec.Code)
	}
	h.cancelMu.Lock()
	_, poisoned := h.pendingCancel[id]
	h.cancelMu.Unlock()
	if poisoned {
		t.Fatal("cancel on idle-with-pending session recorded pendingCancel — would swallow the retry")
	}

	// The retry must run normally: register an instant agent and turn the
	// still-pending message via executeTurnJob's normal path.
	as := newTestSession(h, id, &instantClient{})
	h.sessions.setAgent(id, as)
	job := &turnJob{content: "retry me", persistAck: make(chan struct{})}
	h.executeTurnJob(id, job)
	if job.err != nil {
		t.Fatalf("retry turn error: %v", job.err)
	}
	if h.sessions.PendingCount(id) != 0 {
		t.Fatalf("pending count after retry = %d, want 0", h.sessions.PendingCount(id))
	}
}

// TestCloseRacesTurnStartNoDeadlock hammers ReleaseAgent concurrently with
// turn starts to prove the lock order (m.mu then as.mu in EvictIdle's pattern)
// never deadlocks. Run with -race to catch the inversion the varsion that
// held m.mu across as.mu would exhibit.
func TestCloseRacesTurnStartNoDeadlock(t *testing.T) {
	h := NewHandler()
	proj := t.TempDir()
	id := session.NewSessionID()
	h.sessions.Register(id, proj)
	as := newTestSession(h, id, &instantClient{})
	h.sessions.setAgent(id, as)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 50; i++ {
			h.sessions.setTurnActive(id, true)
			// Simulate a tight turn: flip active, let the release race it.
			h.sessions.setTurnActive(id, false)
			// Re-register the agent each iteration since a close may have
			// released it.
			h.mu.Lock()
			h.agents[id] = as
			h.mu.Unlock()
			h.sessions.setAgent(id, as)
		}
	}()

	closed := make(chan struct{})
	go func() {
		defer close(closed)
		for i := 0; i < 50; i++ {
			h.sessions.ReleaseAgent(id)
		}
	}()

	// If the lock order were inverted (m.mu held while taking as.mu), the two
	// goroutines would deadlock and this timeout would fire. The runTurn path
	// holds as.mu then takes m.mu via setTurnActive; ReleaseAgent must never
	// hold m.mu while taking as.mu.
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("turn-start goroutine deadlocked against ReleaseAgent")
	}
	select {
	case <-closed:
	case <-time.After(10 * time.Second):
		t.Fatal("ReleaseAgent goroutine deadlocked against turn-start")
	}
}
