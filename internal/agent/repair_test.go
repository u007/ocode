package agent

import (
	"strings"
	"testing"
)

func TestRepairToolCallSequence_InsertsStub(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", ToolCalls: []ToolCall{
			{ID: "a"}, {ID: "b"},
		}},
		{Role: "tool", ToolID: "a", Content: "ok"},
		{Role: "user", Content: "next"},
	}
	out := repairToolCallSequence(msgs)
	if len(out) != 5 {
		t.Fatalf("want 5 messages, got %d", len(out))
	}
	foundB := false
	for _, m := range out {
		if m.Role == "tool" && m.ToolID == "b" {
			foundB = true
		}
	}
	if !foundB {
		t.Fatalf("expected stub tool result for id=b, got %+v", out)
	}
}

func TestRepairToolCallSequence_NoOpWhenComplete(t *testing.T) {
	msgs := []Message{
		{Role: "assistant", ToolCalls: []ToolCall{{ID: "a"}}},
		{Role: "tool", ToolID: "a", Content: "ok"},
	}
	out := repairToolCallSequence(msgs)
	if len(out) != 2 {
		t.Fatalf("want 2 messages, got %d", len(out))
	}
}

func TestRepairToolCallSequence_ReordersInterleavedSystemBeforeToolResult(t *testing.T) {
	msgs := []Message{
		{Role: "assistant", ToolCalls: []ToolCall{{ID: "task-1"}}},
		{Role: "system", Content: "[Background agent agent-1 done]"},
		{Role: "tool", ToolID: "task-1", Content: "subagent result"},
		{Role: "user", Content: "continue"},
	}
	out := repairToolCallSequence(msgs)
	if len(out) != 4 {
		t.Fatalf("want 4 messages, got %d", len(out))
	}
	if out[0].Role != "assistant" {
		t.Fatalf("want assistant first, got %q", out[0].Role)
	}
	if out[1].Role != "tool" || out[1].ToolID != "task-1" {
		t.Fatalf("want tool result immediately after assistant, got role=%q tool=%q", out[1].Role, out[1].ToolID)
	}
	if out[2].Role != "system" {
		t.Fatalf("want background system note preserved after tool result, got %q", out[2].Role)
	}
}

func TestRepairToolCallSequence_DowngradesStrayToolResult(t *testing.T) {
	msgs := []Message{{Role: "tool", ToolID: "lost-call", Content: "orphan output"}}
	out := repairToolCallSequence(msgs)
	if len(out) != 1 {
		t.Fatalf("want 1 message, got %d", len(out))
	}
	if out[0].Role != "system" {
		t.Fatalf("want stray tool downgraded to system note, got %q", out[0].Role)
	}
	if out[0].Content == "orphan output" {
		t.Fatalf("want downgraded note content, got raw tool output only")
	}
}

func TestRepairToolCallSequence_DoesNotPullToolResultAcrossUserTurn(t *testing.T) {
	call := ToolCall{ID: "task-1"}
	call.Function.Name = "bash"
	msgs := []Message{
		{Role: "assistant", ToolCalls: []ToolCall{call}},
		{Role: "user", Content: "continue without waiting"},
		{Role: "tool", ToolID: "task-1", Content: "late result"},
	}
	out := repairToolCallSequence(msgs)
	if len(out) != 4 {
		t.Fatalf("want 4 messages, got %d", len(out))
	}
	if out[0].Role != "assistant" {
		t.Fatalf("want assistant first, got %q", out[0].Role)
	}
	if out[1].Role != "tool" || out[1].ToolID != "task-1" {
		t.Fatalf("want synthesized tool result immediately after assistant, got role=%q tool=%q", out[1].Role, out[1].ToolID)
	}
	if out[2].Role != "user" {
		t.Fatalf("want user turn preserved before late tool result note, got %q", out[2].Role)
	}
	if out[3].Role != "system" || !strings.Contains(out[3].Content, "late result") {
		t.Fatalf("want late tool result downgraded to system note, got %#v", out[3])
	}
}
