package agent

import (
	"strings"
	"testing"
	"time"
)

func TestWaitToolPlainDuration(t *testing.T) {
	wt := WaitTool{}
	start := time.Now()
	out, err := wt.Execute([]byte(`{"seconds":1}`))
	if err != nil {
		t.Fatalf("Execute err: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 900*time.Millisecond {
		t.Fatalf("wait returned too early: %v", elapsed)
	}
	if !strings.Contains(out, "1") {
		t.Fatalf("output: %q", out)
	}
}

func TestWaitToolClampsToMax(t *testing.T) {
	d, clamped := resolveWaitDuration(0, 9999)
	if !clamped {
		t.Fatal("expected clamp flag")
	}
	if d != waitCeiling {
		t.Fatalf("duration = %v, want ceiling %v", d, waitCeiling)
	}
}

func TestWaitToolJoinShortCircuits(t *testing.T) {
	runs := NewAgentRunRegistry()
	run := runs.New("explore")
	run.finishOK("already done")
	wt := WaitTool{runs: runs}
	start := time.Now()
	out, err := wt.Execute([]byte(`{"minutes":10,"for":"` + run.ID + `"}`))
	if err != nil {
		t.Fatalf("Execute err: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("join did not short-circuit: waited %v", elapsed)
	}
	if !strings.Contains(out, "already done") {
		t.Fatalf("output: %q", out)
	}
}

func TestWaitToolJoinUnknownID(t *testing.T) {
	wt := WaitTool{runs: NewAgentRunRegistry()}
	out, _ := wt.Execute([]byte(`{"seconds":1,"for":"agent-999"}`))
	if !strings.Contains(out, "unknown") {
		t.Fatalf("expected unknown-id error, got %q", out)
	}
}
