//go:build !windows

package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/tool"
)

// elevationE2EClient is an LLM stand-in: its first Chat returns one `bash
// sudo whoami` tool call (the "LLM-style" request), then a terminal reply. Step
// must stop at the round that raised the human ask, so the second reply is
// never consumed in this test.
type elevationE2EClient struct {
	calls int
}

func (c *elevationE2EClient) Chat([]Message, []map[string]interface{}) (*Message, error) {
	c.calls++
	if c.calls == 1 {
		var tc ToolCall
		tc.ID, tc.Type = "call-elev", "function"
		tc.Function.Name = "bash"
		tc.Function.Arguments = `{"command":"sudo whoami"}`
		return &Message{Role: "assistant", ToolCalls: []ToolCall{tc}}, nil
	}
	return &Message{Role: "assistant", Content: "done"}, nil
}

func (c *elevationE2EClient) GetProvider() string { return "mock" }
func (c *elevationE2EClient) GetModel() string    { return "mock-model" }

// writeFakeSudo puts a fake `sudo` on PATH that reads one password line from
// its tty, validates it, and prints a unique marker plus its argv verbatim.
// It mirrors the tool-level fake but uses a distinct marker so the agent-level
// chain is proven independently.
func writeFakeSudo(t *testing.T, dir, secret, marker string) {
	t.Helper()
	body := fmt.Sprintf(`#!/bin/sh
IFS= read -r pw
if [ "$pw" = '%s' ]; then
  printf '%s\n'
  for a in "$@"; do printf 'ARG=[%%s]\n' "$a"; done
  exit 0
fi
printf '%s_BAD\n'
exit 1
`, secret, marker, marker)
	if err := os.WriteFile(filepath.Join(dir, "sudo"), []byte(body), 0o755); err != nil {
		t.Fatalf("write fake sudo: %v", err)
	}
}

func mustMarshalElevation(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

// TestElevationEndToEnd drives the whole chain at the agent layer: an
// LLM-style `bash sudo whoami` tool call -> a human-only elevation ask carrying
// NeedsPassword -> the secret stored on the owning agent (as the resolve
// endpoint does) -> PTY re-execution against a fake sudo -> a tool result that
// carries the success marker and NO trace of the secret. It then proves the
// secret is absent from the session transcript AND from the JSON-serialized
// messages that become SSE frames on the wire.
func TestElevationEndToEnd_AskStorePTYNoSecretLeak(t *testing.T) {
	setHomeTree(t, t.TempDir())

	const (
		secret = "e2eElevS3cr3t9f2a"
		marker = "E2E_ELEV_MARKER"
	)
	dir := t.TempDir()
	writeFakeSudo(t, dir, secret, marker)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	client := &elevationE2EClient{}
	a := newTestAgent(client, []tool.Tool{tool.BashTool{}}, nil, nil)

	// OnMessage is the exact channel the hosts turn into SSE frames. Capture it
	// so the live-stream serialization can be checked for leaks.
	var emitted []Message
	a.OnMessage = func(m Message) { emitted = append(emitted, m) }

	newMsgs, err := a.Step([]Message{{Role: "user", Content: "run sudo whoami"}})
	if err != nil {
		t.Fatalf("Step: %v", err)
	}

	// The round must pause on a permission ask, not run the command.
	askIdx := -1
	for i, m := range newMsgs {
		if m.Role == "tool" && strings.HasPrefix(m.Content, tool.SentinelPermissionAsk) {
			askIdx = i
			break
		}
	}
	if askIdx < 0 {
		t.Fatalf("no permission ask emitted; messages = %+v", newMsgs)
	}
	ask := newMsgs[askIdx]
	if ask.ToolID != "call-elev" {
		t.Fatalf("ask tool id = %q, want call-elev", ask.ToolID)
	}
	if strings.Contains(ask.Content, marker) {
		t.Fatal("elevated command ran before the human ask was resolved")
	}
	var req PermissionRequest
	payload := strings.TrimPrefix(ask.Content, tool.SentinelPermissionAsk)
	if err := json.Unmarshal([]byte(payload), &req); err != nil {
		t.Fatalf("unmarshal ask payload: %v", err)
	}
	if !req.NeedsPassword {
		t.Fatal("ask NeedsPassword = false, want true on Unix")
	}
	if req.ElevationCommand != "sudo whoami" {
		t.Fatalf("ask ElevationCommand = %q, want %q", req.ElevationCommand, "sudo whoami")
	}
	if strings.Contains(ask.Content, secret) {
		t.Fatal("secret present in the ask sentinel (it must never be in a transcript)")
	}

	// Human approval path: the masked password is stored on the owning agent,
	// keyed by the tool-call id, then the approved call is re-executed.
	a.StoreElevationSecret(ask.ToolID, []byte(secret))
	result, err := a.HandleApprovedToolCall("bash", req.Args, ask.ToolID)
	if err != nil {
		t.Fatalf("HandleApprovedToolCall: %v", err)
	}
	if !strings.Contains(result, marker) {
		t.Fatalf("tool result missing success marker %q: %q", marker, result)
	}
	if !strings.Contains(result, "ARG=[whoami]") {
		t.Fatalf("tool result lost the original command argv: %q", result)
	}
	if strings.Contains(result, secret) {
		t.Fatalf("secret leaked into tool result: %q", result)
	}
	// The secret is single-use: re-execution must have consumed it.
	if got := a.elevation.Take(ask.ToolID); got != nil {
		t.Fatalf("secret survived re-execution: %q", got)
	}

	// Reconstruct the resolved transcript exactly as the host does: replace the
	// ask sentinel in place with the approved tool result.
	transcript := append([]Message(nil), newMsgs...)
	transcript[askIdx].Content = result

	for i, m := range transcript {
		if strings.Contains(m.Content, secret) {
			t.Fatalf("transcript message %d contains the secret", i)
		}
	}
	// Messages are marshalled to JSON verbatim into SSE `data:` frames, so this
	// is byte-identical to the wire representation of the session transcript.
	if data := mustMarshalElevation(t, transcript); strings.Contains(string(data), secret) {
		t.Fatal("session transcript JSON (SSE wire form) contains the secret")
	}
	for i, m := range emitted {
		if strings.Contains(m.Content, secret) {
			t.Fatalf("emitted message %d contains the secret", i)
		}
	}
	if data := mustMarshalElevation(t, emitted); strings.Contains(string(data), secret) {
		t.Fatal("OnMessage stream JSON (SSE wire form) contains the secret")
	}
	if data := mustMarshalElevation(t, transcript[askIdx]); strings.Contains(string(data), secret) {
		t.Fatal("resolved tool-result JSON contains the secret")
	}
}

// TestElevationSecretClearanceOnResetPaths maps each reset path in the plan to
// the agent-level hook that zeroes the store, and proves a stored secret does
// not survive it. /new, /clear and idle eviction all retire the agent through
// Shutdown(); /reset-id keeps the agent alive and rekeys it in place.
func TestElevationSecretClearanceOnResetPaths(t *testing.T) {
	const secret = "reset-path-secret"
	for _, tc := range []struct {
		name string
		act  func(*Agent)
	}{
		{"new_or_clear_or_eviction_Shutdown", func(a *Agent) { a.Shutdown() }},
		{"reset_id_server_RekeySession", func(a *Agent) { a.RekeySession("ses_old", "ses_new") }},
		{"reset_id_tui_RekeyOpenCodeSession", func(a *Agent) { a.RekeyOpenCodeSession("ses_old", "ses_new") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := NewAgent(nil, nil, nil, nil)
			a.elevation.Store("call-1", []byte(secret))
			tc.act(a)
			if got := a.elevation.Take("call-1"); got != nil {
				t.Fatalf("secret survived reset: %q", got)
			}
		})
	}
	// A freshly built agent (the destination of /new and /clear) starts empty.
	fresh := NewAgent(nil, nil, nil, nil)
	if got := fresh.elevation.Take("call-1"); got != nil {
		t.Fatalf("fresh agent has a stored secret: %q", got)
	}
}

// TestElevationAskDefeatsAutoPermissionJudge proves a non-interactive auto
// grant cannot satisfy a secret ask. With auto-permission enabled and a judge
// that would allow anything, a leading sudo still reaches the human ask tier;
// with no asker installed it stays a pending human ask rather than executing.
// This is the nearest real guard for the headless "refuse elevation" rule:
// without a masked prompt there is no way to supply the password, and the tool
// fails closed rather than running unprivileged.
func TestElevationAskDefeatsAutoPermissionJudge(t *testing.T) {
	cfg := &config.Config{}
	cfg.Ocode.Permissions.Auto = &config.AutoPermissionConfig{Enabled: true, Model: "mock/model"}
	a := NewAgent(nil, nil, cfg, nil)
	a.Permissions().SetAutoPermissionEnabled(true)
	a.AddTools([]tool.Tool{tool.BashTool{}})

	prevClientFn := newClientFn
	t.Cleanup(func() { newClientFn = prevClientFn })
	newClientFn = func(_ *config.Config, _ string) LLMClient {
		return &MockClient{Response: &Message{Role: "assistant", Content: "ALLOW: run it"}}
	}

	// No OnPermissionAsk callback models the headless / no-asker case. For an
	// ordinary ask, auto-permission would consult the judge and execute without
	// a human; an elevation ask must instead remain a pending human ask.
	res, err := a.HandleToolCall("bash", json.RawMessage(`{"command":"sudo whoami"}`))
	if err != nil {
		t.Fatalf("HandleToolCall: %v", err)
	}
	if !strings.HasPrefix(res, tool.SentinelPermissionAsk) {
		t.Fatalf("auto-permission satisfied an elevation ask without a human; result = %q", res)
	}
	var req PermissionRequest
	payload := strings.TrimPrefix(res, tool.SentinelPermissionAsk)
	if err := json.Unmarshal([]byte(payload), &req); err != nil {
		t.Fatalf("unmarshal ask payload: %v", err)
	}
	if !req.NeedsPassword || req.ElevationCommand != "sudo whoami" {
		t.Fatalf("ask is not an elevation ask: %+v", req)
	}
}
