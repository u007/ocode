package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/session"
	"github.com/u007/ocode/internal/tool"
)

// errorClient fails every Chat call.
type errorClient struct{}

func (errorClient) Chat([]agent.Message, []map[string]interface{}) (*agent.Message, error) {
	return nil, errors.New("llm exploded")
}
func (errorClient) GetProvider() string { return "fake" }
func (errorClient) GetModel() string    { return "fake-model" }

// TestBootstrapEmitsStageEventsInOrder covers Part 03 Task 3: a bootstrap
// emits session_bootstrap stage events in order (model → tools → mcp → ready)
// on the bus, each tagged with the session and its project.
func TestBootstrapEmitsStageEventsInOrder(t *testing.T) {
	h := NewHandler()
	proj := t.TempDir()
	id := session.NewSessionID()
	h.sessions.Register(id, proj)
	// Completed MCP cache: the bootstrap should not stall.
	ready := make(chan struct{})
	close(ready)
	h.mcpCache = &mcpCache{ready: ready, tools: []tool.Tool{}, errs: nil}

	sub := h.bus.Subscribe(nil)
	defer h.bus.Unsubscribe(sub)

	as, stage, err := h.buildAgentSession(id, "opencode-go/deepseek-v4-flash", nil, proj)
	if err != nil {
		t.Fatalf("build: %v (stage %s)", err, stage)
	}
	if as.agent != nil {
		defer as.agent.Shutdown()
	}

	var stages []string
	var lastEnvelope Envelope
	deadline := time.After(3 * time.Second)
	for len(stages) < 4 {
		select {
		case env := <-sub:
			if env.Event != "session_bootstrap" {
				continue
			}
			lastEnvelope = env
			var data map[string]string
			if err := json.Unmarshal(mustMarshal(t, env.Data), &data); err != nil {
				t.Fatalf("decode bootstrap data: %v", err)
			}
			stages = append(stages, data["stage"])
		case <-deadline:
			t.Fatalf("timed out waiting for stage events, got %v", stages)
		}
	}
	want := []string{"model", "tools", "mcp", "ready"}
	if strings.Join(stages, ",") != strings.Join(want, ",") {
		t.Fatalf("stages = %v, want %v", stages, want)
	}
	if lastEnvelope.SessionID != id || lastEnvelope.Project != proj {
		t.Fatalf("envelope tags = %+v, want session %s project %s", lastEnvelope, id, proj)
	}
	if st, _ := h.sessions.State(id); st.BootstrapStage != "ready" {
		t.Fatalf("entry stage = %q, want ready", st.BootstrapStage)
	}
}

// TestBootstrapMCPTimeoutEmitsWarning covers the 30s-bounded MCP wait: a cache
// that never completes must not hang the bootstrap — after the (shortened)
// timeout a warning event fires and bootstrap proceeds to ready.
func TestBootstrapMCPTimeoutEmitsWarning(t *testing.T) {
	h := NewHandler()
	h.mcpBootstrapTimeout = 40 * time.Millisecond
	proj := t.TempDir()
	id := session.NewSessionID()
	h.sessions.Register(id, proj)
	h.mcpCache = &mcpCache{ready: make(chan struct{})} // never closes

	sub := h.bus.Subscribe(nil)
	defer h.bus.Unsubscribe(sub)

	as, stage, err := h.buildAgentSession(id, "opencode-go/deepseek-v4-flash", nil, proj)
	if err != nil {
		t.Fatalf("build: %v (stage %s)", err, stage)
	}
	defer as.agent.Shutdown()

	warned := false
	ready := false
	deadline := time.After(3 * time.Second)
	for !warned || !ready {
		select {
		case env := <-sub:
			if env.Event != "session_bootstrap" {
				continue
			}
			var data map[string]any
			if err := json.Unmarshal(mustMarshal(t, env.Data), &data); err != nil {
				t.Fatalf("decode bootstrap data: %v", err)
			}
			if data["stage"] == "mcp" && data["warning"] != nil {
				warned = true
			}
			if data["stage"] == "ready" {
				ready = true
			}
		case <-deadline:
			t.Fatalf("timed out: warned=%v ready=%v", warned, ready)
		}
	}
}

// TestBootstrapFailureEmitsTurnErrorWithStage covers the "a failing stage emits
// turn_error with that stage and the persisted message remains" requirement:
// an async chat whose agent can never build still 202s (message durable) and
// then publishes turn_error carrying the failing stage, leaving the message on
// disk.
func TestBootstrapFailureEmitsTurnErrorWithStage(t *testing.T) {
	h := NewHandler()
	proj := t.TempDir()
	h.projects = newTestProjectStore(t, proj)

	sub := h.bus.Subscribe(nil)
	defer h.bus.Unsubscribe(sub)

	rec := chatRequest(t, h, map[string]any{
		"content":      "hi",
		"project_path": proj,
		"async":        true,
		"model":        "local/definitely-not-running",
	})
	if rec.Code != 202 {
		t.Fatalf("chat status %d, want 202 (body %s)", rec.Code, rec.Body.String())
	}
	var resp ChatResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode chat response: %v", err)
	}

	var turnErr Envelope
	deadline := time.After(3 * time.Second)
	for {
		select {
		case env := <-sub:
			if env.Event == "turn_error" && env.SessionID == resp.SessionID {
				turnErr = env
				goto got
			}
		case <-deadline:
			t.Fatal("timed out waiting for turn_error")
		}
	}
got:
	var data map[string]any
	if err := json.Unmarshal(mustMarshal(t, turnErr.Data), &data); err != nil {
		t.Fatalf("decode turn_error: %v", err)
	}
	if data["stage"] != "model" {
		t.Fatalf("turn_error stage = %v, want model", data["stage"])
	}
	if data["error"] == nil || data["error"] == "" {
		t.Fatal("turn_error missing error message")
	}
	st, _ := h.sessions.State(resp.SessionID)
	if st.BootstrapStage != "model" || st.BootstrapError == "" {
		t.Fatalf("entry failed state = %+v, want stage model + error", st)
	}
	// The persisted message must remain on disk under the owning project.
	s, err := session.LoadForDir(proj, resp.SessionID)
	if err != nil {
		t.Fatalf("load persisted session after failure: %v", err)
	}
	if len(s.Messages) != 1 || s.Messages[0].Content != "hi" {
		t.Fatalf("persisted messages after failure = %+v, want [user: hi]", s.Messages)
	}
}

// TestAsync202BeforeBootstrapCompletes covers Part 03 Task 2's core property:
// the 202 returns before the agent build completes. The build is stalled in
// the MCP stage (never-completing cache, 30s timeout) — the 202 must still
// arrive promptly with the message durable.
func TestAsync202BeforeBootstrapCompletes(t *testing.T) {
	h := NewHandler()
	h.mcpCache = &mcpCache{ready: make(chan struct{})} // build will stall at mcp
	proj := t.TempDir()
	h.projects = newTestProjectStore(t, proj)

	start := time.Now()
	rec := chatRequest(t, h, map[string]any{
		"content":      "hi",
		"project_path": proj,
		"async":        true,
		"model":        "opencode-go/deepseek-v4-flash",
	})
	elapsed := time.Since(start)
	if rec.Code != 202 {
		t.Fatalf("chat status %d, want 202 (body %s)", rec.Code, rec.Body.String())
	}
	// The MCP wait alone is 30s; a 202 that waited for the build could not
	// have returned this fast.
	if elapsed > 5*time.Second {
		t.Fatalf("202 took %s — it waited for the bootstrap", elapsed)
	}
	var resp ChatResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode chat response: %v", err)
	}
	if _, err := session.LoadForDir(proj, resp.SessionID); err != nil {
		t.Fatalf("message not durable at 202 time: %v", err)
	}
}

// TestTurnLifecycleEventsAndHeartbeat covers Part 03 Task 4: a running turn
// emits turn_started, periodic turn_heartbeat, then turn_done; the registry
// entry is turn-active during the turn and idle after; an erroring turn emits
// turn_error and the heartbeat stops.
func TestTurnLifecycleEventsAndHeartbeat(t *testing.T) {
	h := NewHandler()
	h.turnHeartbeatInterval = 20 * time.Millisecond
	proj := t.TempDir()
	id := session.NewSessionID()
	h.sessions.Register(id, proj)

	blocking := newBlockingClient()
	newTestSession(h, id, blocking)

	sub := h.bus.Subscribe(nil)
	defer h.bus.Unsubscribe(sub)

	done := make(chan struct{})
	go func() {
		if _, err := h.runTurn(id, h.lookupAgentSession(id), "slow", turnOptions{}); err != nil {
			t.Errorf("runTurn: %v", err)
		}
		close(done)
	}()

	select {
	case <-blocking.started:
	case <-time.After(3 * time.Second):
		t.Fatal("turn never started")
	}
	if st, _ := h.sessions.State(id); !st.TurnActive {
		t.Fatal("entry should be turn-active while the turn runs")
	}

	var sawStarted, sawHeartbeat bool
	deadline := time.After(3 * time.Second)
	for !sawStarted || !sawHeartbeat {
		select {
		case env := <-sub:
			if env.SessionID != id {
				continue
			}
			switch env.Event {
			case "turn_started":
				sawStarted = true
			case "turn_heartbeat":
				sawHeartbeat = true
			}
		case <-deadline:
			t.Fatalf("timed out: started=%v heartbeat=%v", sawStarted, sawHeartbeat)
		}
	}

	close(blocking.release)
	<-done
	if !waitForEvent(t, sub, id, "turn_done") {
		t.Fatal("turn_done not observed")
	}
	if st, _ := h.sessions.State(id); st.TurnActive {
		t.Fatal("entry should be idle after the turn")
	}
	if st, _ := h.sessions.State(id); st.LastSeq == 0 {
		t.Fatal("reconcile last_seq should be non-zero after turn events")
	}
}

// TestTurnErrorLifecycleEvent covers the erroring-turn half of Task 4: the
// turn emits turn_error and the entry returns to idle.
func TestTurnErrorLifecycleEvent(t *testing.T) {
	h := NewHandler()
	h.turnHeartbeatInterval = time.Hour // no heartbeats in this test
	id := session.NewSessionID()
	h.sessions.Register(id, t.TempDir())
	newTestSession(h, id, errorClient{})

	sub := h.bus.Subscribe(nil)
	defer h.bus.Unsubscribe(sub)

	if _, err := h.runTurn(id, h.lookupAgentSession(id), "boom", turnOptions{}); err == nil {
		t.Fatal("expected runTurn error")
	}
	if !waitForEvent(t, sub, id, "turn_error") {
		t.Fatal("turn_error not observed")
	}
	if st, _ := h.sessions.State(id); st.TurnActive {
		t.Fatal("entry should be idle after a failed turn")
	}
}

// TestTurnEventsFlowWhenBridgeAttached covers Part 06 Task 3's
// bridge-suppression removal: turn lifecycle events flow on the bus even when
// an RC bridge is attached (previously runTurn suppressed headless events for
// every session the moment a bridge existed).
func TestTurnEventsFlowWhenBridgeAttached(t *testing.T) {
	h := NewHandler()
	id := session.NewSessionID()
	h.sessions.Register(id, t.TempDir())
	newTestSession(h, id, instantClient{})
	// Attach a bridge for a *different* session — the headless session's turn
	// must still publish lifecycle events.
	h.rc = &RCBridge{SessionID: "sess-other", RcCh: make(chan RCRequest, 1)}

	sub := h.bus.Subscribe(nil)
	defer h.bus.Unsubscribe(sub)

	if _, err := h.runTurn(id, h.lookupAgentSession(id), "hi", turnOptions{}); err != nil {
		t.Fatalf("runTurn: %v", err)
	}
	if !waitForEvent(t, sub, id, "turn_done") {
		t.Fatal("turn_done not observed with bridge attached")
	}
}

// waitForEvent drains the bus subscription until an envelope for session id
// with the given event name arrives (or a 3s timeout fires), skipping earlier
// events (turn_started, heartbeats) queued ahead of it.
func waitForEvent(t *testing.T, sub chan Envelope, id, event string) bool {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case env := <-sub:
			if env.SessionID == id && env.Event == event {
				return true
			}
		case <-deadline:
			return false
		}
	}
}

// waitForEnvelopeData waits for an event on sub for id and decodes its payload.
func waitForEnvelopeData(t *testing.T, sub chan Envelope, id, event string) (map[string]any, bool) {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case env := <-sub:
			if env.SessionID != id || env.Event != event {
				continue
			}
			var data map[string]any
			if err := json.Unmarshal(mustMarshal(t, env.Data), &data); err != nil {
				t.Fatalf("decode %s data: %v", event, err)
			}
			return data, true
		case <-deadline:
			return nil, false
		}
	}
}

// TestTurnStartedIncludesStartedAtAndSessionCreatedAt verifies the bus payload
// for turn_started carries the authoritative started-at timestamp (RFC3339Nano)
// plus the session's persisted CreatedAt when available — so the web's
// current-input and entire-session timers can be driven from a single event.
func TestTurnStartedIncludesStartedAtAndSessionCreatedAt(t *testing.T) {
	h := NewHandler()
	h.turnHeartbeatInterval = time.Hour // no heartbeats in this test
	proj := t.TempDir()
	id := session.NewSessionID()
	// Persist the session first so CreatedAt is populated, then register.
	saveSessionToDir(t, proj, id)
	h.sessions.Register(id, proj)
	newTestSession(h, id, instantClient{})

	sub := h.bus.Subscribe(nil)
	defer h.bus.Unsubscribe(sub)

	if _, err := h.runTurn(id, h.lookupAgentSession(id), "hi", turnOptions{}); err != nil {
		t.Fatalf("runTurn: %v", err)
	}

	data, ok := waitForEnvelopeData(t, sub, id, "turn_started")
	if !ok {
		t.Fatal("turn_started not observed")
	}
	started, ok := data["started_at"].(string)
	if !ok || started == "" {
		t.Fatalf("turn_started missing started_at: %+v", data)
	}
	if t1, err := time.Parse(time.RFC3339Nano, started); err != nil {
		t.Fatalf("started_at not RFC3339Nano: %v", err)
	} else if delta := time.Since(t1); delta < 0 || delta > 30*time.Second {
		t.Fatalf("started_at %s is not recent (delta %s)", started, delta)
	}
	if created, ok := data["session_created_at"].(string); !ok || created == "" {
		t.Fatalf("turn_started missing session_created_at: %+v", data)
	} else {
		eventCreated, err := time.Parse(time.RFC3339Nano, created)
		if err != nil {
			t.Fatalf("session_created_at not RFC3339Nano: %v", err)
		}
		// The persisted session was written just above — the event must carry
		// that same creation time (disk round-trip may shift sub-second
		// precision, so tolerance is a few seconds, not exact equality).
		s, err := session.LoadForDir(proj, id)
		if err != nil {
			t.Fatalf("reload persisted session: %v", err)
		}
		persistedCreated := s.CreatedAt.UTC()
		if eventCreated.Sub(persistedCreated) > 5*time.Second ||
			persistedCreated.Sub(eventCreated) > 5*time.Second {
			t.Fatalf("session_created_at %v != persisted CreatedAt %v", eventCreated, persistedCreated)
		}
	}
}

// TestTurnDoneIncludesTookMs verifies turn_done carries started_at, ended_at,
// and took_ms for a successful turn, so the web can show "✓ done at … · took …".
// The started_at must equal the one published on turn_started, ended_at must
// not be earlier, and took_ms must match the timestamp difference.
func TestTurnDoneIncludesTookMs(t *testing.T) {
	h := NewHandler()
	h.turnHeartbeatInterval = time.Hour
	proj := t.TempDir()
	id := session.NewSessionID()
	saveSessionToDir(t, proj, id)
	h.sessions.Register(id, proj)
	// Slow client so the took duration is measurable.
	slow := &slowClient{delay: 50 * time.Millisecond}
	newTestSession(h, id, slow)

	sub := h.bus.Subscribe(nil)
	defer h.bus.Unsubscribe(sub)

	if _, err := h.runTurn(id, h.lookupAgentSession(id), "hi", turnOptions{}); err != nil {
		t.Fatalf("runTurn: %v", err)
	}

	startedData, ok := waitForEnvelopeData(t, sub, id, "turn_started")
	if !ok {
		t.Fatal("turn_started not observed")
	}
	startedStr, ok := startedData["started_at"].(string)
	if !ok || startedStr == "" {
		t.Fatalf("turn_started missing started_at: %+v", startedData)
	}
	startedAt, err := time.Parse(time.RFC3339Nano, startedStr)
	if err != nil {
		t.Fatalf("turn_started started_at not RFC3339Nano: %v", err)
	}

	data, ok := waitForEnvelopeData(t, sub, id, "turn_done")
	if !ok {
		t.Fatal("turn_done not observed")
	}
	doneStarted, _ := data["started_at"].(string)
	endedStr, _ := data["ended_at"].(string)
	if doneStarted != startedStr {
		t.Fatalf("turn_done started_at = %q, want %q (must match turn_started)", doneStarted, startedStr)
	}
	if doneStarted == "" || endedStr == "" {
		t.Fatalf("turn_done missing started_at/ended_at: %+v", data)
	}
	endedAt, err := time.Parse(time.RFC3339Nano, endedStr)
	if err != nil {
		t.Fatalf("turn_done ended_at not RFC3339Nano: %v", err)
	}
	if endedAt.Before(startedAt) {
		t.Fatalf("ended_at %s before started_at %s", endedAt, startedAt)
	}
	tookMs, ok := data["took_ms"].(float64)
	if !ok {
		t.Fatalf("turn_done missing took_ms: %+v", data)
	}
	// took_ms is the millisecond-truncated timestamp span. RFC3339Nano
	// round-tripping may lose <1ms of precision, so allow a 1ms delta.
	spanMs := endedAt.Sub(startedAt).Milliseconds()
	if diff := int64(tookMs) - spanMs; diff < -1 || diff > 1 {
		t.Fatalf("took_ms=%v does not match timestamp span %dms", tookMs, spanMs)
	}
}

// TestTurnErrorIncludesTookMs verifies turn_error carries started_at/ended_at/
// took_ms too, so the web shows took for failed turns as well.
func TestTurnErrorIncludesTookMs(t *testing.T) {
	h := NewHandler()
	h.turnHeartbeatInterval = time.Hour
	proj := t.TempDir()
	id := session.NewSessionID()
	saveSessionToDir(t, proj, id)
	h.sessions.Register(id, proj)
	newTestSession(h, id, errorClient{})

	sub := h.bus.Subscribe(nil)
	defer h.bus.Unsubscribe(sub)

	if _, err := h.runTurn(id, h.lookupAgentSession(id), "boom", turnOptions{}); err == nil {
		t.Fatal("expected runTurn error")
	}

	data, ok := waitForEnvelopeData(t, sub, id, "turn_error")
	if !ok {
		t.Fatal("turn_error not observed")
	}
	startedStr, _ := data["started_at"].(string)
	endedStr, _ := data["ended_at"].(string)
	if startedStr == "" || endedStr == "" {
		t.Fatalf("turn_error missing started_at/ended_at: %+v", data)
	}
	startedAt, err := time.Parse(time.RFC3339Nano, startedStr)
	if err != nil {
		t.Fatalf("turn_error started_at not RFC3339Nano: %v", err)
	}
	endedAt, err := time.Parse(time.RFC3339Nano, endedStr)
	if err != nil {
		t.Fatalf("turn_error ended_at not RFC3339Nano: %v", err)
	}
	if endedAt.Before(startedAt) {
		t.Fatalf("ended_at %s before started_at %s", endedAt, startedAt)
	}
	if tookMs, ok := data["took_ms"].(float64); !ok {
		t.Fatalf("turn_error missing took_ms: %+v", data)
	} else {
		spanMs := endedAt.Sub(startedAt).Milliseconds()
		if diff := int64(tookMs) - spanMs; diff < -1 || diff > 1 {
			t.Fatalf("turn_error took_ms=%v does not match timestamp span %dms", tookMs, spanMs)
		}
	}
}

// TestSessionStatusIncludesTurnTiming verifies GET /api/sessions/:id/status
// returns TurnStartedAt/TurnElapsedMs/TurnEndedAt/TurnTookMs and
// SessionCreatedAt from authoritative state.
func TestSessionStatusIncludesTurnTiming(t *testing.T) {
	h := NewHandler()
	proj := t.TempDir()
	id := session.NewSessionID()
	saveSessionToDir(t, proj, id)
	h.sessions.Register(id, proj)

	// Pre-set a turn that has finished so the snapshot carries ended/took.
	// No duration lower bound is asserted: an immediate on→off can
	// legitimately measure 0ms. Only internal consistency is checked.
	h.sessions.setTurnActive(id, true)
	h.sessions.setTurnActive(id, false)

	rec := httptest.NewRecorder()
	h.HandleSessionStatus(rec, httptest.NewRequest("GET", "/api/sessions/"+id+"/status", nil), id)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var snap TUIStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &snap); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if snap.TurnStartedAt == "" || snap.TurnEndedAt == "" {
		t.Fatalf("status missing turn timing: %+v", snap)
	}
	if snap.SessionCreatedAt == "" {
		t.Fatalf("status missing session_created_at: %+v", snap)
	}
	startedAt, err := time.Parse(time.RFC3339Nano, snap.TurnStartedAt)
	if err != nil {
		t.Fatalf("turn_started_at not RFC3339Nano: %v", err)
	}
	endedAt, err := time.Parse(time.RFC3339Nano, snap.TurnEndedAt)
	if err != nil {
		t.Fatalf("turn_ended_at not RFC3339Nano: %v", err)
	}
	if endedAt.Before(startedAt) {
		t.Fatalf("turn_ended_at %s before turn_started_at %s", endedAt, startedAt)
	}
	// TurnTookMs must match the timestamp span (1ms tolerance for the
	// RFC3339Nano round-trip). The turn finished, so TurnElapsedMs stays 0
	// (omitempty) — only the terminal fields are asserted.
	spanMs := endedAt.Sub(startedAt).Milliseconds()
	if diff := snap.TurnTookMs - spanMs; diff < -1 || diff > 1 {
		t.Fatalf("status turn_took_ms=%d does not match span %dms", snap.TurnTookMs, spanMs)
	}
}

// TestTurnTimingForBridgedSessionDoesNotAutoStart ensures no turn timing is
// published for an idle bridged session — the bridged TUI owns the snapshot.
func TestTurnTimingForBridgedSessionDoesNotAutoStart(t *testing.T) {
	h := NewHandler()
	proj := t.TempDir()
	id := session.NewSessionID()
	saveSessionToDir(t, proj, id)
	h.sessions.Register(id, proj)
	// Attach a bridge for THIS session — headless turn events are routed
	// through publishBusEvent (no auto status push); see publishTurnStarted.
	h.rc = &RCBridge{SessionID: id, RcCh: make(chan RCRequest, 1)}
	newTestSession(h, id, instantClient{})

	rec := httptest.NewRecorder()
	h.HandleSessionStatus(rec, httptest.NewRequest("GET", "/api/sessions/"+id+"/status", nil), id)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}
	var snap TUIStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &snap); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// SessionCreatedAt is loaded from disk regardless of bridged state.
	if snap.SessionCreatedAt == "" {
		t.Fatalf("bridged status missing session_created_at: %+v", snap)
	}
	// No turn has run yet, so turn timing stays empty.
	if snap.TurnStartedAt != "" || snap.TurnEndedAt != "" {
		t.Fatalf("bridged status leaked turn timing before any turn: %+v", snap)
	}
}

// TestBridgedTurnTimingStillFlowsOnBus verifies that in bridged mode (TUI
// attached, /rc active) the turn lifecycle events still carry started_at /
// ended_at / took_ms on the unified bus, even though no headless status push
// fires (the TUI owns the status feed).
func TestBridgedTurnTimingStillFlowsOnBus(t *testing.T) {
	h := NewHandler()
	h.turnHeartbeatInterval = time.Hour
	proj := t.TempDir()
	id := session.NewSessionID()
	saveSessionToDir(t, proj, id)
	h.sessions.Register(id, proj)
	h.rc = &RCBridge{SessionID: id, RcCh: make(chan RCRequest, 1)}
	newTestSession(h, id, instantClient{})

	sub := h.bus.Subscribe(nil)
	defer h.bus.Unsubscribe(sub)

	if _, err := h.runTurn(id, h.lookupAgentSession(id), "hi", turnOptions{}); err != nil {
		t.Fatalf("runTurn: %v", err)
	}

	startedData, ok := waitForEnvelopeData(t, sub, id, "turn_started")
	if !ok {
		t.Fatal("turn_started not observed with bridge attached")
	}
	if startedAt, _ := startedData["started_at"].(string); startedAt == "" {
		t.Fatalf("bridged turn_started missing started_at: %+v", startedData)
	}

	doneData, ok := waitForEnvelopeData(t, sub, id, "turn_done")
	if !ok {
		t.Fatal("turn_done not observed with bridge attached")
	}
	if doneStarted, _ := doneData["started_at"].(string); doneStarted == "" {
		t.Fatalf("bridged turn_done missing started_at: %+v", doneData)
	}
	if ended, _ := doneData["ended_at"].(string); ended == "" {
		t.Fatalf("bridged turn_done missing ended_at: %+v", doneData)
	}
	if tookMs, ok := doneData["took_ms"].(float64); !ok {
		t.Fatalf("bridged turn_done missing took_ms: %+v", doneData)
	} else if int(tookMs) < 0 {
		t.Fatalf("bridged turn_done took_ms=%v, want >= 0", tookMs)
	}
}

// slowClient delays Chat by `delay` to give a measurable took_ms.
type slowClient struct{ delay time.Duration }

func (c *slowClient) Chat([]agent.Message, []map[string]interface{}) (*agent.Message, error) {
	time.Sleep(c.delay)
	return &agent.Message{Role: "assistant", Content: "ok"}, nil
}
func (c *slowClient) GetProvider() string { return "fake" }
func (c *slowClient) GetModel() string    { return "fake-model" }

// mustMarshal round-trips any data through JSON so envelope payloads can be
// inspected in a type-safe way.
func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal %T: %v", v, err)
	}
	return b
}
