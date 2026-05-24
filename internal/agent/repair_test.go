package agent

import "testing"

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
