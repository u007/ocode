package desktop

import "testing"

func TestQuitGuardBlockClearRoundTrip(t *testing.T) {
	g := NewQuitGuard()
	if blocked, reason := g.Blocked(); blocked || reason != "" {
		t.Fatalf("new guard should be unblocked, got blocked=%v reason=%q", blocked, reason)
	}

	g.Block("editor draft could not be saved")
	blocked, reason := g.Blocked()
	if !blocked {
		t.Fatal("expected blocked after Block")
	}
	if reason != "editor draft could not be saved" {
		t.Fatalf("unexpected reason %q", reason)
	}

	g.Clear()
	if blocked, reason := g.Blocked(); blocked || reason != "" {
		t.Fatalf("expected unblocked after Clear, got blocked=%v reason=%q", blocked, reason)
	}
}

func TestQuitGuardHandleRawMessage(t *testing.T) {
	g := NewQuitGuard()

	if handled := g.HandleRawMessage("unrelated:message"); handled {
		t.Fatal("unrelated message should not be handled")
	}
	if g.HandleRawMessage("ocode:quit-guard:blocked") != true {
		t.Fatal("expected blocked message handled")
	}
	if blocked, _ := g.Blocked(); !blocked {
		t.Fatal("expected blocked after bare blocked message")
	}

	if g.HandleRawMessage(quitGuardReasonPrefix+"quota exceeded for /a/b.txt") != true {
		t.Fatal("expected reason-prefixed message handled")
	}
	blocked, reason := g.Blocked()
	if !blocked || reason != "quota exceeded for /a/b.txt" {
		t.Fatalf("expected blocked with reason, got blocked=%v reason=%q", blocked, reason)
	}

	if g.HandleRawMessage("ocode:quit-guard:clear") != true {
		t.Fatal("expected clear message handled")
	}
	if blocked, _ := g.Blocked(); blocked {
		t.Fatal("expected unblocked after clear message")
	}
}

func TestQuitGuardReasonWithColons(t *testing.T) {
	// Only the known prefix is trimmed; the reason may itself contain colons
	// (Windows paths, "Error: ...") and must survive intact.
	g := NewQuitGuard()
	g.HandleRawMessage(quitGuardReasonPrefix + `C:\Users\me\a.txt: quota exceeded`)
	blocked, reason := g.Blocked()
	if !blocked {
		t.Fatal("expected blocked")
	}
	if reason != `C:\Users\me\a.txt: quota exceeded` {
		t.Fatalf("reason mangled: %q", reason)
	}
}
