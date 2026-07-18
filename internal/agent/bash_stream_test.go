package agent

import (
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/u007/ocode/internal/tool"
)

// TestExecuteToolCallStreamsBashOutput exercises the full agent dispatch path
// for a streaming tool. With OnToolOutput wired, a bash tool call must emit
// incremental chunks live AND return the canonical complete result. This is
// the exact code path the TUI uses (Agent.OnToolOutput -> deltaCh), so it is a
// faithful headless equivalent of the live TUI stream that would otherwise
// require a real terminal (e.g. Termux).
func TestExecuteToolCallStreamsBashOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses bash -c")
	}
	var mu sync.Mutex
	var chunks []string
	a := &Agent{
		tools: map[string]tool.Tool{"bash": tool.BashTool{}},
	}
	a.OnToolOutput = func(toolCallID, chunk string) {
		mu.Lock()
		chunks = append(chunks, chunk)
		mu.Unlock()
	}

	cmd := `printf 'alpha\n'; sleep 0.05; printf 'beta\n'; sleep 0.05; printf 'gamma\n'`
	args, _ := json.Marshal(map[string]interface{}{"command": cmd})

	res, err := a.executeToolCall("bash", json.RawMessage(args), nil, "call-xyz")
	if err != nil {
		t.Fatalf("executeToolCall returned error: %v", err)
	}

	mu.Lock()
	joined := strings.Join(chunks, "")
	mu.Unlock()

	if joined == "" {
		t.Fatalf("expected streamed chunks via OnToolOutput, got none")
	}
	for _, want := range []string{"alpha", "beta", "gamma"} {
		if !strings.Contains(joined, want) {
			t.Errorf("streamed output missing %q; got %q", want, joined)
		}
		if !strings.Contains(res, want) {
			t.Errorf("canonical result missing %q; got %q", want, res)
		}
	}
}

// TestExecuteToolCallNoStreamWhenCallbackUnset confirms that without an
// OnToolOutput sink the agent falls back to the synchronous Execute and still
// returns the full result (no streaming path, e.g. headless subagents).
func TestExecuteToolCallNoStreamWhenCallbackUnset(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses bash -c")
	}
	a := &Agent{
		tools: map[string]tool.Tool{"bash": tool.BashTool{}},
	}
	// OnToolOutput intentionally left nil.

	cmd := `printf 'hello\n'`
	args, _ := json.Marshal(map[string]interface{}{"command": cmd})

	res, err := a.executeToolCall("bash", json.RawMessage(args), nil, "call-abc")
	if err != nil {
		t.Fatalf("executeToolCall returned error: %v", err)
	}
	if !strings.Contains(res, "hello") {
		t.Fatalf("expected hello in result, got %q", res)
	}
}

type delayedStreamingTool struct {
	started chan struct{}
	release <-chan struct{}
}

func (delayedStreamingTool) Name() string        { return "delayed_stream" }
func (delayedStreamingTool) Description() string { return "test streaming tool" }
func (delayedStreamingTool) Definition() map[string]interface{} {
	return map[string]interface{}{"name": "delayed_stream", "parameters": map[string]interface{}{"type": "object"}}
}
func (delayedStreamingTool) Execute(json.RawMessage) (string, error) { return "", nil }
func (delayedStreamingTool) Parallel() bool                          { return false }

func (t delayedStreamingTool) ExecuteStream(_ json.RawMessage, emit func(chunk string)) (string, error) {
	if t.started != nil {
		close(t.started)
	}
	<-t.release
	emit("chunk-from-stream")
	return "chunk-from-stream", nil
}

// TestExecuteToolCallStreamingCallbackSnapshotSurvivesNilFlip reproduces the
// race where the agent's OnToolOutput callback is cleared while a streaming
// tool is still running. executeToolCall must snapshot the callback before it
// hands control to the tool so the live stream keeps working and does not panic.
func TestExecuteToolCallStreamingCallbackSnapshotSurvivesNilFlip(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	got := make(chan string, 1)
	a := &Agent{
		tools: map[string]tool.Tool{"delayed_stream": delayedStreamingTool{started: started, release: release}},
	}
	a.OnToolOutput = func(_ string, chunk string) {
		got <- chunk
	}

	done := make(chan error, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				done <- fmt.Errorf("panic: %v", r)
			}
		}()
		args, _ := json.Marshal(map[string]interface{}{})
		_, err := a.executeToolCall("delayed_stream", json.RawMessage(args), nil, "call-snapshot")
		done <- err
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for streaming tool to start")
	}

	// Flip the field to nil after the tool has been selected but before it emits.
	// Without a local snapshot in executeToolCall, the callback invocation races
	// with this nil assignment and panics.
	a.OnToolOutput = nil
	close(release)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("executeToolCall returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for executeToolCall")
	}

	select {
	case chunk := <-got:
		if chunk != "chunk-from-stream" {
			t.Fatalf("unexpected streamed chunk %q", chunk)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for streamed callback")
	}
}
