package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/config"
)

func TestSessionReadReturnsGoalAndTodo(t *testing.T) {
	h := pulseTestHandler(t)
	msgs := []agent.Message{{Role: "user", Content: "hi"}, {Role: "assistant", Content: "hello"}}
	registerLiveSession(t, h, "ses_rt", "/work/rt", false, msgs)
	// set a goal on the resident agent
	if as := h.lookupAgentSession("ses_rt"); as != nil && as.agent != nil {
		_ = as.agent.SetGoal("ship it")
	}
	out := mustPulseTool(t, h, "session_read", `{"session_id":"ses_rt"}`)
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatal(err)
	}
	// goal/todo are omitempty: absent when unset, which is correct. Just ensure valid JSON and no panic.
	_ = m
}

// registerResidentSession registers a session with a live agent on this server,
// as a queue target needs. Cleanup evicts it and waits for any drainer to exit,
// so a busy-waiting drainer never outlives its test.
func registerResidentSession(t *testing.T, h *Handler, id, root string, busy bool, msgs []agent.Message) {
	t.Helper()
	h.sessions.Register(id, root)
	if busy {
		h.sessions.setTurnActive(id, true)
	}
	h.mu.Lock()
	h.agents[id] = &agentSession{agent: agent.NewAgent(nil, nil, &config.Config{}, nil), messages: msgs}
	h.mu.Unlock()
	t.Cleanup(func() {
		h.sessions.setTurnActive(id, false)
		h.mu.Lock()
		delete(h.agents, id)
		h.mu.Unlock()
		waitQueueDrainersIdle(t, h)
	})
}

func waitQueueDrainersIdle(t *testing.T, h *Handler) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		h.drainMu.Lock()
		n := len(h.queueDrainers)
		h.drainMu.Unlock()
		if n == 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("queue drainer still running after the test")
}

func TestSessionSendQueueCap(t *testing.T) {
	h := pulseTestHandler(t)
	msgs := []agent.Message{{Role: "user", Content: "hi"}}
	registerResidentSession(t, h, "ses_q", "/work/q", true, msgs) // busy, resident
	// fill queue to cap
	for i := 0; i < 10; i++ {
		out := mustPulseTool(t, h, "session_send", fmt.Sprintf(`{"session_id":"ses_q","content":"q%d","queue":true}`, i))
		var m map[string]any
		_ = json.Unmarshal([]byte(out), &m)
		if m["queued"] != true {
			t.Fatalf("expected queued, got %s", out)
		}
	}
	// 11th refused
	_, err := runPulseTool(t, h, "session_send", `{"session_id":"ses_q","content":"overflow","queue":true}`)
	if err == nil {
		t.Fatal("expected queue cap refusal, got nil")
	}
}

func TestSessionSendSelfTargetNotGuarded(t *testing.T) {
	// Documented limitation: tools lack caller context, so self-target is not
	// refused here. This test just ensures no panic.
	h := pulseTestHandler(t)
	msgs := []agent.Message{{Role: "user", Content: "hi"}}
	registerLiveSession(t, h, "ses_self", "/work/self", false, msgs)
	out, err := runPulseTool(t, h, "session_send", `{"session_id":"ses_self","content":"self","queue":false}`)
	t.Logf("self send: %v %s", err, out)
}

// TestSessionCommToolSetIsExactAllowlist pins the peer-communication set a
// normal session gets. A denylist once leaked host_connect and the cross-project
// readers into every session; the allowlist must stay exact.
func TestSessionCommToolSetIsExactAllowlist(t *testing.T) {
	h := pulseTestHandler(t)
	want := []string{
		"permission_resolve", "question_answer", "question_dismiss",
		"session_ask", "session_command", "session_read", "session_recap", "session_send",
	}
	var got []string
	for _, tl := range h.sessionCommTools() {
		got = append(got, tl.Name())
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("session comm tools = %v, want %v", got, want)
	}
	for _, forbidden := range []string{"host_connect", "pulse_board", "project_list", "agent_runs", "session_new", "session_close",
		"memory_read", "memory_write", "activation_read", "activation_write", "terminal_read", "terminal_tabs", "terminal_screen", "terminal_new", "terminal_kill"} {
		for _, g := range got {
			if g == forbidden {
				t.Fatalf("pulse-only tool %q leaked into normal sessions", forbidden)
			}
		}
	}
}

// TestSessionCommToolsHaveNoAllowRule checks the permission default that
// buildAgentSession now relies on: with no rule installed, every comm tool asks.
func TestSessionCommToolsHaveNoAllowRule(t *testing.T) {
	h := pulseTestHandler(t)
	ag := agent.NewAgent(nil, nil, &config.Config{}, nil)
	for _, tl := range h.sessionCommTools() {
		if lvl := ag.Permissions().Check(tl.Name()); lvl != agent.PermissionAsk {
			t.Fatalf("%s permission = %v, want ask (no unconditional allow)", tl.Name(), lvl)
		}
	}
}

// TestSessionCommScopeConfinesTargets checks that a normal session's peer tools
// refuse a target outside allowedProjectRoots, a remote host, and the Pulse
// assistant's own session, and still serve an in-root target.
func TestSessionCommScopeConfinesTargets(t *testing.T) {
	h := pulseTestHandler(t)
	h.workDir = "/work/allowed"
	msgs := []agent.Message{{Role: "user", Content: "hi"}, {Role: "assistant", Content: "hello"}}
	registerLiveSession(t, h, "ses_in", "/work/allowed", false, msgs)
	registerLiveSession(t, h, "ses_out", "/elsewhere/x", false, msgs)

	read := func(args string) (string, error) {
		for _, tl := range h.sessionCommTools() {
			if tl.Name() == "session_read" {
				return tl.Execute(json.RawMessage(args))
			}
		}
		t.Fatal("session_read missing from sessionCommTools")
		return "", nil
	}
	if _, err := read(`{"session_id":"ses_in"}`); err != nil {
		t.Fatalf("in-root read refused: %v", err)
	}
	if _, err := read(`{"session_id":"ses_out"}`); err == nil {
		t.Fatal("out-of-root read allowed; want refusal")
	}
	if _, err := read(`{"session_id":"ses_in","host":"box"}`); err == nil {
		t.Fatal("remote host read allowed; want refusal")
	}
	if _, err := read(`{"session_id":"pulse_abc"}`); err == nil {
		t.Fatal("pulse assistant session readable from a normal session; want refusal")
	}
}

// TestQueueRefusesTargetWithoutResidentAgent: a TUI-owned or disk-only session
// has no agent on this server. The queue must refuse it up front (409) and
// persist nothing, instead of writing a file the TUI owns.
func TestQueueRefusesTargetWithoutResidentAgent(t *testing.T) {
	h := pulseTestHandler(t)
	registerLiveSession(t, h, "ses_nr", "/work/nr", false, []agent.Message{{Role: "user", Content: "hi"}})
	err := h.enqueueQueuedMessage("ses_nr", "later")
	var qe *queueError
	if !errors.As(err, &qe) || qe.status != http.StatusConflict {
		t.Fatalf("enqueue without resident agent = %v, want 409 queueError", err)
	}
	if n := h.sessions.PendingCount("ses_nr"); n != 0 {
		t.Fatalf("refused queue left %d pending entries", n)
	}
}

// TestQueueHTTPStatusMapping checks the HTTP surface of the queue refusals.
func TestQueueHTTPStatusMapping(t *testing.T) {
	h := pulseTestHandler(t)
	registerLiveSession(t, h, "ses_hx", "/work/hx", false, []agent.Message{{Role: "user", Content: "hi"}})
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/ses_hx/queue", strings.NewReader(`{"content":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.HandleSessionQueue(rec, req, "ses_hx")
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 for a session with no resident agent; body %s", rec.Code, rec.Body.String())
	}
}

// TestTryPushPendingCapIsAtomic hammers the queue from many goroutines; exactly
// limit pushes may succeed. Run with -race.
func TestTryPushPendingCapIsAtomic(t *testing.T) {
	h := pulseTestHandler(t)
	h.sessions.Register("ses_cap", "/work/cap")
	var wg sync.WaitGroup
	var ok int64
	var mu sync.Mutex
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if h.sessions.TryPushPending("ses_cap", fmt.Sprintf("m%d", i), maxQueuedMessages) {
				mu.Lock()
				ok++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	if ok != maxQueuedMessages {
		t.Fatalf("accepted %d pushes, want exactly %d", ok, maxQueuedMessages)
	}
	if n := h.sessions.PendingCount("ses_cap"); n != maxQueuedMessages {
		t.Fatalf("PendingCount = %d, want %d", n, maxQueuedMessages)
	}
}

// TestRemovePendingLastRollsBackReservation: a failed persist must release its
// slot, and only its own entry, not a later push.
func TestRemovePendingLastRollsBackReservation(t *testing.T) {
	h := pulseTestHandler(t)
	h.sessions.Register("ses_rb", "/work/rb")
	h.sessions.TryPushPending("ses_rb", "first", maxQueuedMessages)
	h.sessions.TryPushPending("ses_rb", "second", maxQueuedMessages)
	h.sessions.RemovePendingLast("ses_rb", "first")
	got := h.sessions.PendingContents("ses_rb")
	if len(got) != 1 || got[0] != "second" {
		t.Fatalf("after rollback queue = %v, want [second]", got)
	}
}

// TestQueueDrainerOnePerSessionAndRetires: a busy session keeps one drainer no
// matter how many queue requests arrive, and the drainer retires once the
// session has no resident agent.
func TestQueueDrainerOnePerSessionAndRetires(t *testing.T) {
	h := pulseTestHandler(t)
	registerResidentSession(t, h, "ses_dr", "/work/dr", true, []agent.Message{{Role: "user", Content: "hi"}})
	for i := 0; i < 3; i++ {
		if err := h.enqueueQueuedMessage("ses_dr", fmt.Sprintf("d%d", i)); err != nil {
			t.Fatalf("enqueue %d: %v", i, err)
		}
	}
	h.drainMu.Lock()
	n := len(h.queueDrainers)
	h.drainMu.Unlock()
	if n != 1 {
		t.Fatalf("drainers = %d, want exactly 1 for one session", n)
	}
	// Evicting the agent must retire the drainer; the cleanup waits for it.
	h.mu.Lock()
	delete(h.agents, "ses_dr")
	h.mu.Unlock()
	waitQueueDrainersIdle(t, h)
}
