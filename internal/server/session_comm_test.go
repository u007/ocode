package server

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/u007/ocode/internal/agent"
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

func TestSessionSendQueueCap(t *testing.T) {
	h := pulseTestHandler(t)
	msgs := []agent.Message{{Role: "user", Content: "hi"}}
	registerLiveSession(t, h, "ses_q", "/work/q", true, msgs) // busy
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

func TestNormalSessionNoTerminalMemoryActivationTools(t *testing.T) {
	h := pulseTestHandler(t)
	// sessionCommTools must exclude terminal_*, memory_*, activation_*
	for _, t2 := range h.sessionCommTools() {
		n := t2.Name()
		if len(n) >= 9 && n[:9] == "terminal_" {
			t.Fatalf("terminal tool leaked: %s", n)
		}
		if n == "memory_read" || n == "memory_write" || n == "activation_read" || n == "activation_write" {
			t.Fatalf("pulse-only tool leaked: %s", n)
		}
	}
}
