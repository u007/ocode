package tui

import (
	"testing"
)

func TestJoinDelayedChatInputs(t *testing.T) {
	got := joinDelayedChatInputs([]string{"first", "second", "third"})
	if want := "first\n\nsecond\n\nthird"; got != want {
		t.Fatalf("joined input = %q, want %q", got, want)
	}
}

func TestDelayedChatInputGenerationRejectsStaleTimer(t *testing.T) {
	var m model
	first := m.queueDelayedChatInput("first")
	second := m.queueDelayedChatInput("second")

	if got := first().(delayedChatInputMsg); got.generation == m.delayedChatGeneration {
		t.Fatal("stale timer still matched the current generation")
	}
	if got := second().(delayedChatInputMsg); got.generation != m.delayedChatGeneration {
		t.Fatalf("current timer generation = %d, want %d", got.generation, m.delayedChatGeneration)
	}
	if got := m.takeDelayedChatInput(); got != "first\n\nsecond" {
		t.Fatalf("consolidated input = %q", got)
	}
	if m.hasDelayedChatInput() {
		t.Fatal("pending input remained after taking the batch")
	}
}

func TestInvalidateDelayedChatInputDropsPendingMessages(t *testing.T) {
	var m model
	m.queueDelayedChatInput("discard me")
	m.invalidateDelayedChatInput()

	if m.hasDelayedChatInput() {
		t.Fatal("invalidated input remained pending")
	}
}
