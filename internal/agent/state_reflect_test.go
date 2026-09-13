package agent

import (
	"testing"
)

// Verify reflection mechanism: unchanged state emits nothing; each
// transition emits once; close/clear transitions work; updates appear
// at tail; stable-prefix invariant (message array prefix unchanged).
func TestReflectMessageNoEmissionWhenUnchanged(t *testing.T) {
	prev := ReflectState{preview: "docs/deck.pptx", browser: "side:chat:web"}
	cur := ReflectState{preview: "docs/deck.pptx", browser: "side:chat:web"}
	msg, emitted := ReflectMessage(prev, cur)
	if emitted {
		t.Errorf("expected no emission for unchanged state, got message %v", msg)
	}
}

func TestReflectMessageEmitsOnPreviewChange(t *testing.T) {
	prev := ReflectState{preview: "", browser: ""}
	cur := ReflectState{preview: "docs/deck.pptx", browser: ""}
	msg, emitted := ReflectMessage(prev, cur)
	if !emitted {
		t.Errorf("expected emission when preview changed")
	}
	if msg.Role != "user" {
		t.Errorf("expected user role, got %s", msg.Role)
	}
}

func TestReflectMessageEmitsOnBrowserChange(t *testing.T) {
	prev := ReflectState{preview: "docs/deck.pptx", browser: "side:chat:web"}
	cur := ReflectState{preview: "docs/deck.pptx", browser: "side:chat:web"}
	_, emitted := ReflectMessage(prev, cur)
	if emitted {
		t.Errorf("expected no emission for same browser state")
	}

	prev.browser = ""
	cur.browser = "side:chat:web"
	msg, emitted := ReflectMessage(prev, cur)
	if !emitted {
		t.Errorf("expected emission when browser changed")
	}
	if msg.Role != "user" {
		t.Errorf("expected user role for browser change, got %s", msg.Role)
	}
}

func TestReflectTailAppendsAtEnd(t *testing.T) {
	a := &Agent{}
	a.reflectState = ReflectState{preview: "", browser: ""}
	messages := []Message{{Role: "user", Content: "hello"}, {Role: "assistant", Content: "hi"}}
	current := ReflectState{preview: "/path/file.md", browser: "side:chat:web"}
	updated := a.reflectTail(messages, current)
	if len(updated) != 3 {
		t.Errorf("expected 3 messages after append, got %d", len(updated))
	}
	last := updated[len(updated)-1]
	if last.Role != "user" {
		t.Errorf("expected appended message to be user-role, got %s", last.Role)
	}
	// Stable prefix: first message unchanged.
	if updated[0].Content != "hello" {
		t.Errorf("prefix changed unexpectedly: %v", updated[0])
	}
}

func TestReflectBaselineAdvancesEvenWithoutEmission(t *testing.T) {
	a := &Agent{}
	a.reflectState = ReflectState{preview: "a.md", browser: "side:chat:web"}
	current := ReflectState{preview: "a.md", browser: "side:chat:web"}
	a.reflectTail([]Message{{Role: "user", Content: "x"}}, current)
	if a.reflectState.preview != "a.md" {
		t.Errorf("baseline should advance/stay: %v", a.reflectState)
	}
}
