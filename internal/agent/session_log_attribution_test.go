package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/debuglog"
)

// TestTokenUsageDebugLogRoutesThroughEmitter pins the DebugLog contract: the
// caller supplies the emitter (the owning client's emitDebug), so TOKENS rows
// carry that client's session id instead of the process-global fallback that
// leaked them into every session's Logs tab.
func TestTokenUsageDebugLogRoutesThroughEmitter(t *testing.T) {
	u := &TokenUsage{
		PromptTokens:     int64Ptr(1234),
		CompletionTokens: int64Ptr(56),
		CacheReadTokens:  int64Ptr(78),
	}
	var gotKind, gotMsg string
	u.DebugLog("mock-model", func(kind, msg string) {
		gotKind, gotMsg = kind, msg
	})
	if gotKind != "TOKENS" {
		t.Fatalf("emit kind = %q, want TOKENS", gotKind)
	}
	for _, want := range []string{"model=mock-model", "input=1234", "cache_read=78", "output=56"} {
		if !strings.Contains(gotMsg, want) {
			t.Errorf("emitted %q, want it to contain %q", gotMsg, want)
		}
	}
}

// TestTokenUsageDebugLogNilEmitterFallsBack confirms a nil emitter does not
// panic and still reaches the process-global sink. The package-level emitDebug
// only appends to debuglog when a DebugAppend sink is installed (the server
// wires one); otherwise it writes to stderr, so the test installs a sink to
// observe the untagged fallback deterministically.
func TestTokenUsageDebugLogNilEmitterFallsBack(t *testing.T) {
	prev := DebugAppend
	DebugAppend = func(kind, msg string) {
		debuglog.Log.Append(debuglog.Entry{Kind: debuglog.EntryKind(kind), Message: msg})
	}
	defer func() { DebugAppend = prev }()
	debuglog.Log.Clear()
	u := &TokenUsage{PromptTokens: int64Ptr(1), CompletionTokens: int64Ptr(2)}
	u.DebugLog("legacy-model", nil)
	var found bool
	for _, e := range debuglog.Log.Snapshot() {
		if e.Kind == debuglog.EntryKind("TOKENS") && strings.Contains(e.Message, "legacy-model") {
			found = true
			if e.SessionID != "" {
				t.Errorf("fallback entry SessionID = %q, want empty (process-global)", e.SessionID)
			}
		}
	}
	if !found {
		t.Fatal("nil-emitter DebugLog did not reach the process-global sink")
	}
}

// TestTaskSubagentLogsInheritParentSession is the regression guard for the
// Logs-tab clutter: a `task` sub-agent must inherit the parent chat's session
// id so its own debug entries are attributed to that chat instead of being
// emitted untagged (and therefore shown in every open session's Logs tab).
//
// Before the fix subagent.go built the child via NewAgent without
// SetSessionID, so every entry it emitted was process-global.
func TestTaskSubagentLogsInheritParentSession(t *testing.T) {
	// Mirror the server's sink (handler.go wires DebugAppend to debuglog), so
	// an untagged entry is captured here exactly as it would leak into every
	// Logs tab in production.
	prevSink := DebugAppend
	DebugAppend = func(kind, msg string) {
		debuglog.Log.Append(debuglog.Entry{Kind: debuglog.EntryKind(kind), Message: msg})
	}
	defer func() { DebugAppend = prevSink }()
	debuglog.Log.Clear()
	workDir := t.TempDir()
	bashCall := ToolCall{ID: "call-bash", Type: "function"}
	bashCall.Function.Name = "bash"
	bashCall.Function.Arguments = `{"command":"echo tagged > ./out.txt"}`
	client := &scriptedSubagentClient{responses: []*Message{
		{Role: "assistant", ToolCalls: []ToolCall{bashCall}},
		{Role: "assistant", Content: "done"},
	}}
	parent := NewAgent(client, nil, nil, nil)
	parent.SetWorkDir(workDir)
	parent.SetSessionID("ses_tasklogs")
	parent.Permissions().SetRule("task", PermissionAllow)
	parent.Permissions().SetRule("bash", PermissionAllow)
	parent.SetSubAgentPermAsker(func(req PermissionRequest) PermissionResponse {
		return PermissionResponse{Level: PermissionAllow}
	})

	reg := NewAgentRegistry()
	reg.defs = append(reg.defs, AgentDefinition{
		Name:        "sheller",
		Description: "test",
		Mode:        AgentModeSubagent,
		Tools:       []string{"bash"},
		Source:      "test",
	})
	if _, err := (TaskTool{mainAgent: parent, registry: reg}).Execute(
		json.RawMessage(`{"prompt":"run","agent":"sheller"}`)); err != nil {
		t.Fatalf("task execute: %v", err)
	}

	// The parent never runs Step here, so every "exposing <n> tools" entry was
	// emitted by the sub-agent; all must carry the inherited session id.
	var sawSubagentEntry bool
	for _, e := range debuglog.Log.Snapshot() {
		if strings.Contains(e.Message, "exposing") {
			sawSubagentEntry = true
			if e.SessionID != "ses_tasklogs" {
				t.Errorf("sub-agent entry %q SessionID = %q, want ses_tasklogs", e.Message, e.SessionID)
			}
		}
	}
	if !sawSubagentEntry {
		t.Fatal("no sub-agent tool-exposure entry observed; test setup did not run the child")
	}
}
