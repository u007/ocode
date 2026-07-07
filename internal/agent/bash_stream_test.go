package agent

import (
	"encoding/json"
	"runtime"
	"strings"
	"sync"
	"testing"

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
