package tool

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestProcessRingBufferTruncates(t *testing.T) {
	p := &Process{ID: "proc-1", Command: "x", Status: ProcRunning}
	// Write more than the cap.
	big := strings.Repeat("a", procBufferCap+5000)
	p.appendOutput([]byte(big))
	text, _ := p.readSince()
	if len(text) > procBufferCap+64 {
		t.Fatalf("buffer not capped: got %d bytes", len(text))
	}
	if !strings.Contains(text, "truncated") {
		t.Fatalf("expected truncation marker, got prefix %q", text[:40])
	}
}

func TestProcessReadSinceIsIncremental(t *testing.T) {
	p := &Process{ID: "proc-1", Command: "x", Status: ProcRunning}
	p.appendOutput([]byte("hello "))
	first, _ := p.readSince()
	if first != "hello " {
		t.Fatalf("first read = %q", first)
	}
	p.appendOutput([]byte("world"))
	second, _ := p.readSince()
	if second != "world" {
		t.Fatalf("second read = %q, want incremental %q", second, "world")
	}
}

func TestProcessRegistryRunAndCapture(t *testing.T) {
	r := NewProcessRegistry()
	p := r.StartBackground("echo hello-bg")
	if p.ID == "" {
		t.Fatal("expected non-empty process id")
	}
	// Poll until the process exits (cap the wait).
	for i := 0; i < 200; i++ {
		if st, _ := p.snapshotStatus(); st != ProcRunning {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	out, _, code, err := r.Output(p.ID)
	if err != nil {
		t.Fatalf("Output err: %v", err)
	}
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(out, "hello-bg") {
		t.Fatalf("output missing echo: %q", out)
	}
}

func TestProcessRegistryUnknownID(t *testing.T) {
	r := NewProcessRegistry()
	if _, _, _, err := r.Output("proc-999"); err == nil {
		t.Fatal("expected error for unknown id")
	}
	if _, err := r.Kill("proc-999"); err == nil {
		t.Fatal("expected error killing unknown id")
	}
}

func TestBashToolBackgroundReturnsID(t *testing.T) {
	r := NewProcessRegistry()
	bt := &BashTool{Procs: r}
	out, err := bt.Execute(json.RawMessage(`{"command":"echo hi","run_in_background":true}`))
	if err != nil {
		t.Fatalf("Execute err: %v", err)
	}
	if !strings.Contains(out, "proc-1") {
		t.Fatalf("expected process id in output, got %q", out)
	}
	if len(r.Snapshot()) != 1 {
		t.Fatalf("expected 1 registered process, got %d", len(r.Snapshot()))
	}
}

func TestBashToolForegroundUnchanged(t *testing.T) {
	bt := &BashTool{}
	out, err := bt.Execute(json.RawMessage(`{"command":"echo sync-ok"}`))
	if err != nil {
		t.Fatalf("Execute err: %v", err)
	}
	if !strings.Contains(out, "sync-ok") {
		t.Fatalf("foreground output wrong: %q", out)
	}
}

func TestBashOutputTool(t *testing.T) {
	r := NewProcessRegistry()
	p := r.StartBackground("echo poll-me")
	for i := 0; i < 200; i++ {
		if st, _ := p.snapshotStatus(); st != ProcRunning {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	bo := BashOutputTool{Procs: r}
	out, err := bo.Execute(json.RawMessage(`{"id":"` + p.ID + `"}`))
	if err != nil {
		t.Fatalf("Execute err: %v", err)
	}
	if !strings.Contains(out, "poll-me") {
		t.Fatalf("bash_output missing process text: %q", out)
	}
}

func TestKillShellTool(t *testing.T) {
	r := NewProcessRegistry()
	p := r.StartBackground("sleep 30")
	ks := KillShellTool{Procs: r}
	out, err := ks.Execute(json.RawMessage(`{"id":"` + p.ID + `"}`))
	if err != nil {
		t.Fatalf("Execute err: %v", err)
	}
	if !strings.Contains(out, "killed") {
		t.Fatalf("expected kill confirmation, got %q", out)
	}
}
