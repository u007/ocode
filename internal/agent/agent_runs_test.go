package agent

import (
	"strings"
	"testing"
)

func TestAgentRunRegistryLifecycle(t *testing.T) {
	r := NewAgentRunRegistry()
	run := r.New("explore")
	if run.ID == "" || run.Name != "explore" {
		t.Fatalf("bad run: %+v", run)
	}
	if run.statusValue() != RunRunning {
		t.Fatalf("new run status = %s, want running", run.statusValue())
	}
	run.appendTranscript(Message{Role: "assistant", Content: "line one\nline two"})
	run.finishOK("final result")
	if run.statusValue() != RunDone {
		t.Fatalf("status = %s, want done", run.statusValue())
	}
	got, ok := r.Get(run.ID)
	if !ok {
		t.Fatal("run not found by ID")
	}
	if got.Result != "final result" {
		t.Fatalf("result = %q", got.Result)
	}
}

func TestAgentRunTranscriptCap(t *testing.T) {
	run := &AgentRun{ID: "agent-1", Name: "x", Status: RunRunning}
	for i := 0; i < transcriptCap+50; i++ {
		run.appendTranscript(Message{Role: "assistant", Content: "msg"})
	}
	if n := len(run.transcriptCopy()); n > transcriptCap {
		t.Fatalf("transcript not capped: %d", n)
	}
}

func TestAgentRunLastLines(t *testing.T) {
	run := &AgentRun{ID: "agent-1", Name: "x", Status: RunRunning}
	run.appendTranscript(Message{Role: "assistant", Content: "alpha\nbeta\ngamma"})
	lines := run.LastLines(2)
	if len(lines) != 2 || lines[0] != "beta" || lines[1] != "gamma" {
		t.Fatalf("LastLines = %v", lines)
	}
}

func TestAgentRunRegistryUnknown(t *testing.T) {
	r := NewAgentRunRegistry()
	if _, ok := r.Get("agent-999"); ok {
		t.Fatal("expected miss for unknown run")
	}
	_ = strings.TrimSpace("") // keep strings import used
}

func TestAgentRunSnapshot(t *testing.T) {
	r := NewAgentRunRegistry()
	r.New("explore")
	r.New("scout")
	snap := r.Snapshot()
	if len(snap) != 2 {
		t.Fatalf("snapshot len = %d, want 2", len(snap))
	}
}

func TestAgentRunRunningCount(t *testing.T) {
	r := NewAgentRunRegistry()
	r1 := r.New("explore")
	r2 := r.New("scout")
	r2.finishOK("done")
	if c := r.RunningCount(); c != 1 {
		t.Fatalf("running count = %d, want 1", c)
	}
	_ = r1
}

func TestAgentRunRegistryCancelAll(t *testing.T) {
	r := NewAgentRunRegistry()
	run := r.New("explore")
	cancelled := false
	run.Sub = NewAgent(nil, nil, nil)
	run.Cancel = func() { cancelled = true }
	r.CancelAll()
	if !cancelled {
		t.Fatal("CancelAll did not invoke run cancel")
	}
}

func TestAgentRun_ModelLabel(t *testing.T) {
	t.Run("returns provider/model when subagent has both", func(t *testing.T) {
		run := &AgentRun{
			Sub: &Agent{client: &GenericClient{Provider: "opencode-go", Model: "deepseek-v4-flash"}},
		}
		got := run.ModelLabel()
		if got != "opencode-go/deepseek-v4-flash" {
			t.Fatalf("ModelLabel = %q, want %q", got, "opencode-go/deepseek-v4-flash")
		}
	})

	t.Run("returns model only when no provider", func(t *testing.T) {
		run := &AgentRun{
			Sub: &Agent{client: &GenericClient{Provider: "", Model: "gpt-4o"}},
		}
		got := run.ModelLabel()
		if got != "gpt-4o" {
			t.Fatalf("ModelLabel = %q, want %q", got, "gpt-4o")
		}
	})

	t.Run("returns empty string when Sub is nil", func(t *testing.T) {
		run := &AgentRun{Sub: nil}
		if got := run.ModelLabel(); got != "" {
			t.Fatalf("ModelLabel = %q, want empty", got)
		}
	})
}

func TestAgentRunTerminalStateIsStickyAfterCancel(t *testing.T) {
	r := NewAgentRunRegistry()
	run := r.New("explore")
	r.CancelAll()

	run.finishOK("late success")
	if run.statusValue() != RunFailed {
		t.Fatalf("status after late finishOK = %s, want %s", run.statusValue(), RunFailed)
	}
	if run.Result != "" {
		t.Fatalf("late finishOK should not set result, got %q", run.Result)
	}
}
