package tui

import (
	"strings"
	"testing"
)

func TestMemoryMaintenanceContextKeepsRecentNonTransientMessages(t *testing.T) {
	m := model{
		messages: []message{
			{role: roleAssistant, text: "ignore transient", transient: true},
			{role: roleThinking, text: "think hard"},
			{role: roleUser, text: "user one"},
			{role: roleAssistant, text: "assistant one"},
			{role: roleUser, text: "user two"},
			{role: roleAssistant, text: "assistant two"},
			{role: roleUser, text: "user three"},
			{role: roleAssistant, text: "assistant three"},
			{role: roleUser, text: "user four"},
			{role: roleAssistant, text: "assistant four"},
			{role: roleUser, text: "user five"},
		},
	}

	got := m.memoryMaintenanceContext()
	if len(got) != 8 {
		t.Fatalf("expected 8 messages, got %d", len(got))
	}
	if got[0].Content != "assistant one" || got[len(got)-1].Content != "user five" {
		t.Fatalf("unexpected context order: %#v", got)
	}
	for _, msg := range got {
		if strings.TrimSpace(msg.Content) == "" {
			t.Fatal("context should not include empty messages")
		}
		if msg.Role != "user" && msg.Role != "assistant" {
			t.Fatalf("unexpected role %q in context", msg.Role)
		}
	}
}
