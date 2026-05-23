package agent

import (
	"encoding/json"
	"testing"
)

func TestGateToolCall_PlanModeAllowsDelegation(t *testing.T) {
	cases := []string{"task", "agent", "agent_status", "task_status"}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			reason, ok := gateToolCall(ModePlan, name, json.RawMessage(`{"prompt":"investigate auth flow"}`))
			if !ok {
				t.Fatalf("plan mode should allow %s for delegation; denied with: %s", name, reason)
			}
		})
	}
}

func TestGateToolCall_PlanModeStillBlocksMutations(t *testing.T) {
	denyCases := []struct {
		name string
		args string
	}{
		{"edit", `{"path":"src/main.go"}`},
		{"apply_patch", `{}`},
		{"delete", `{"path":"foo.txt"}`},
		{"write", `{"path":"src/main.go","content":"x"}`},
	}
	for _, c := range denyCases {
		t.Run(c.name, func(t *testing.T) {
			_, ok := gateToolCall(ModePlan, c.name, json.RawMessage(c.args))
			if ok {
				t.Fatalf("plan mode must not allow %s", c.name)
			}
		})
	}
}

func TestGateToolCall_PlanWriteAllowedForPlanPaths(t *testing.T) {
	_, ok := gateToolCall(ModePlan, "write", json.RawMessage(`{"path":".opencode/plans/2026-05-24.md","content":"plan"}`))
	if !ok {
		t.Fatal("plan mode must allow writes to .opencode/plans/*.md")
	}
}

func TestGateToolCall_PlanEnterExit(t *testing.T) {
	for _, name := range []string{"plan_enter", "plan_exit"} {
		t.Run(name, func(t *testing.T) {
			_, ok := gateToolCall(ModePlan, name, json.RawMessage(`{}`))
			if !ok {
				t.Fatalf("plan mode must allow %s", name)
			}
			// Non-plan modes (other than build, which short-circuits at the
			// top) must reject these tools.
			if _, ok := gateToolCall(ModeReview, name, json.RawMessage(`{}`)); ok {
				t.Fatalf("review mode must not allow %s", name)
			}
		})
	}
}
