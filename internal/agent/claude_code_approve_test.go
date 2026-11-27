package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/tool"
)

func TestClaudeCodeToolMapping(t *testing.T) {
	cases := []struct {
		name     string
		tool     string
		input    string
		wantName string
		wantArgs string // compared as canonical JSON
		mapped   bool
		wantErr  bool
	}{
		{"bash", "Bash", `{"command":"ls -la"}`, "bash", `{"command":"ls -la"}`, true, false},
		{"write", "Write", `{"file_path":"/p/a.txt","content":"hi"}`, "write", `{"content":"hi","path":"/p/a.txt"}`, true, false},
		{"edit", "Edit", `{"file_path":"/p/a.go","old_string":"x","new_string":"y"}`, "edit", `{"path":"/p/a.go","replace":"y","search":"x"}`, true, false},
		{
			"multiedit", "MultiEdit",
			`{"file_path":"/p/a.go","edits":[{"old_string":"a","new_string":"b"},{"old_string":"c","new_string":"d","replace_all":true}]}`,
			"multiedit",
			`{"edits":[{"oldString":"a","newString":"b"},{"oldString":"c","newString":"d","replaceAll":true}],"file_path":"/p/a.go"}`,
			true, false,
		},
		{"notebook", "NotebookEdit", `{"notebook_path":"/p/n.ipynb","new_source":"x","cell_id":"c1","edit_mode":"replace"}`, "write", `{"content":"x","path":"/p/n.ipynb"}`, true, false},
		{"webfetch", "WebFetch", `{"url":"https://example.com/a"}`, "webfetch", `{"url":"https://example.com/a"}`, true, false},
		{"unknown websearch", "WebSearch", `{"query":"go"}`, "", "", false, false},
		{"unknown mcp", "mcp__x__y", `{"k":1}`, "", "", false, false},
		{"bash wrong shape", "Bash", `{"command":5}`, "", "", true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			name, args, mapped, err := mapClaudeCodeTool(tc.tool, json.RawMessage(tc.input))
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if mapped != tc.mapped {
				t.Fatalf("mapped = %v, want %v", mapped, tc.mapped)
			}
			if name != tc.wantName {
				t.Fatalf("name = %q, want %q", name, tc.wantName)
			}
			if tc.wantArgs == "" {
				return
			}
			var got, want any
			if err := json.Unmarshal(args, &got); err != nil {
				t.Fatalf("args not JSON: %v", err)
			}
			if err := json.Unmarshal([]byte(tc.wantArgs), &want); err != nil {
				t.Fatalf("bad want JSON: %v", err)
			}
			gb, _ := json.Marshal(got)
			wb, _ := json.Marshal(want)
			if string(gb) != string(wb) {
				t.Fatalf("args = %s, want %s", gb, wb)
			}
		})
	}
}

// claudeCodeAskRecorder is a stub bridgePermAsker that records every request
// and answers with a fixed response.
type claudeCodeAskRecorder struct {
	calls []PermissionRequest
	resp  PermissionResponse
}

func (r *claudeCodeAskRecorder) ask(_ context.Context, req PermissionRequest) PermissionResponse {
	r.calls = append(r.calls, req)
	return r.resp
}

// claudeCodeTestAgent builds an agent with the real bash tool and a private
// HOME, and a work dir that contains the target file so the bash path scope
// does not add an unrelated ask. It returns the agent and the target path.
func claudeCodeTestAgent(t *testing.T) (*Agent, string) {
	t.Helper()
	dir := t.TempDir()
	setHomeTree(t, t.TempDir())
	a := newTestAgent(&scriptedSideQueryClient{}, []tool.Tool{tool.BashTool{}}, nil, nil)
	a.SetWorkDir(dir)
	return a, filepath.Join(dir, "x")
}

func bashInput(t *testing.T, command string) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(map[string]string{"command": command})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func assertNotExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err == nil {
		t.Fatalf("%s exists: the decide-only path executed the tool", path)
	}
}

func TestClaudeCodeAllowedBashIsDecidedNotExecuted(t *testing.T) {
	a, target := claudeCodeTestAgent(t)
	a.permissions.SetRule("bash", PermissionAllow)

	allowed, msg := a.decideClaudeCodeToolUse(context.Background(), "Bash", bashInput(t, "touch "+target), "toolu_1")
	if !allowed {
		t.Fatalf("allowed = false, message = %q", msg)
	}
	assertNotExists(t, target)
}

func TestClaudeCodeDenyRuleReturnsMessage(t *testing.T) {
	a, target := claudeCodeTestAgent(t)
	// A /ban-style prefix rule is a pipeline Deny. (A tool-level Deny would be
	// caught earlier by isToolAllowed and never reach the decide path.)
	a.permissions.SetBashPrefixRule("cp", PermissionDeny)

	allowed, msg := a.decideClaudeCodeToolUse(context.Background(), "Bash", bashInput(t, "cp /etc/hosts "+target), "toolu_2")
	if allowed {
		t.Fatal("allowed = true for a deny rule")
	}
	if !strings.Contains(msg, "permission rules") {
		t.Fatalf("message = %q, want the permission-rule denial", msg)
	}
	assertNotExists(t, target)
}

func TestClaudeCodeAskUsesBridgeAsker(t *testing.T) {
	cases := []struct {
		name        string
		resp        PermissionResponse
		wantAllowed bool
	}{
		{"allow", PermissionResponse{Level: PermissionAllow}, true},
		{"deny", PermissionResponse{Level: PermissionDeny}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, target := claudeCodeTestAgent(t)
			// cp is not auto-allowed (a copy from outside the work dir asks), so it
			// reaches the human asker. The copy must never run.
			rec := &claudeCodeAskRecorder{resp: tc.resp}
			a.SetBridgePermAsker(rec.ask)

			allowed, msg := a.decideClaudeCodeToolUse(context.Background(), "Bash", bashInput(t, "cp /etc/hosts "+target), "toolu_3")
			if allowed != tc.wantAllowed {
				t.Fatalf("allowed = %v (message %q), want %v", allowed, msg, tc.wantAllowed)
			}
			if len(rec.calls) != 1 {
				t.Fatalf("asker called %d times, want 1", len(rec.calls))
			}
			if got := rec.calls[0].AgentName; got != claudeCodeAskLabel {
				t.Fatalf("AgentName = %q, want %q", got, claudeCodeAskLabel)
			}
			if !tc.wantAllowed && msg == "" {
				t.Fatal("deny message is empty")
			}
			assertNotExists(t, target)
		})
	}
}

func TestClaudeCodeAskWithoutAskerHasNoSurface(t *testing.T) {
	a, target := claudeCodeTestAgent(t)
	// The main agent's own OnPermissionAsk and the sub-agent asker must not
	// answer a Claude Code approval when no bridge asker is installed.
	hostAsked := false
	a.OnPermissionAsk = func(PermissionRequest) PermissionResponse {
		hostAsked = true
		return PermissionResponse{Level: PermissionAllow}
	}
	subAsked := false
	a.SetSubAgentPermAsker(func(PermissionRequest) PermissionResponse {
		subAsked = true
		return PermissionResponse{Level: PermissionAllow}
	})

	allowed, msg := a.decideClaudeCodeToolUse(context.Background(), "Bash", bashInput(t, "cp /etc/hosts "+target), "toolu_4")
	if allowed {
		t.Fatal("allowed = true with no approval surface")
	}
	if msg != claudeCodeNoSurfaceMessage {
		t.Fatalf("message = %q, want %q", msg, claudeCodeNoSurfaceMessage)
	}
	if hostAsked || subAsked {
		t.Fatalf("another asker answered the approval (OnPermissionAsk=%t, subAgent=%t)", hostAsked, subAsked)
	}
	assertNotExists(t, target)
}

// TestClaudeCodeAskCancelledByContextDenies checks that an ask parked on the
// bridge asker ends as a deny as soon as the request context is cancelled, so
// a CLI that disconnects cannot leave a permission dialog waiting.
func TestClaudeCodeAskCancelledByContextDenies(t *testing.T) {
	a, target := claudeCodeTestAgent(t)
	parked := make(chan struct{})
	a.SetBridgePermAsker(func(ctx context.Context, _ PermissionRequest) PermissionResponse {
		close(parked)
		<-ctx.Done()
		return PermissionResponse{Level: PermissionDeny}
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type result struct {
		allowed bool
		msg     string
	}
	done := make(chan result, 1)
	go func() {
		allowed, msg := a.decideClaudeCodeToolUse(ctx, "Bash", bashInput(t, "cp /etc/hosts "+target), "toolu_8")
		done <- result{allowed, msg}
	}()

	select {
	case <-parked:
	case <-time.After(5 * time.Second):
		t.Fatal("bridge asker was never called")
	}
	cancel()

	select {
	case got := <-done:
		if got.allowed {
			t.Fatal("allowed = true after the request was cancelled")
		}
		if got.msg == "" {
			t.Fatal("deny message is empty")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("decideClaudeCodeToolUse did not return after cancellation")
	}
	assertNotExists(t, target)
}

// TestClaudeCodeUnmappedAskCancelledByContextDenies is the same guarantee for
// a Claude Code tool with no ocode counterpart.
func TestClaudeCodeUnmappedAskCancelledByContextDenies(t *testing.T) {
	a, _ := claudeCodeTestAgent(t)
	parked := make(chan struct{})
	a.SetBridgePermAsker(func(ctx context.Context, _ PermissionRequest) PermissionResponse {
		close(parked)
		<-ctx.Done()
		return PermissionResponse{Level: PermissionDeny}
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan bool, 1)
	go func() {
		allowed, _ := a.decideClaudeCodeToolUse(ctx, "WebSearch", json.RawMessage(`{"query":"go"}`), "toolu_9")
		done <- allowed
	}()
	<-parked
	cancel()

	select {
	case allowed := <-done:
		if allowed {
			t.Fatal("allowed = true after the request was cancelled")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("unmapped approval did not return after cancellation")
	}
}

// TestClaudeCodeElevationIsRefusedBeforeAsk checks that a sudo/su bash command
// is refused without showing any dialog: the bridge must not collect a root
// password.
func TestClaudeCodeElevationIsRefusedBeforeAsk(t *testing.T) {
	a, target := claudeCodeTestAgent(t)
	// An Allow rule would let a plain command through; elevation must still stop.
	a.permissions.SetRule("bash", PermissionAllow)
	rec := &claudeCodeAskRecorder{resp: PermissionResponse{Level: PermissionAllow}}
	a.SetBridgePermAsker(rec.ask)

	allowed, msg := a.decideClaudeCodeToolUse(context.Background(), "Bash", bashInput(t, "sudo ls "+target), "toolu_10")
	if allowed {
		t.Fatal("allowed = true for sudo")
	}
	if msg != claudeCodeElevationMessage {
		t.Fatalf("message = %q, want %q", msg, claudeCodeElevationMessage)
	}
	if len(rec.calls) != 0 {
		t.Fatalf("asker called %d times for an elevation command, want 0", len(rec.calls))
	}
}

func TestClaudeCodeUnknownToolNeverRunsJudge(t *testing.T) {
	a, _ := claudeCodeTestAgent(t)
	// An Allow rule for the tool would let the pipeline pass it without asking.
	// The unmapped path must ignore rules and ask the human directly.
	a.permissions.SetRule("mcp__x__y", PermissionAllow)
	judged := false
	a.OnPermissionCheck = func(string, string, bool) { judged = true }
	rec := &claudeCodeAskRecorder{resp: PermissionResponse{Level: PermissionAllow}}
	a.SetBridgePermAsker(rec.ask)

	allowed, msg := a.decideClaudeCodeToolUse(context.Background(), "mcp__x__y", json.RawMessage(`{"k":1}`), "toolu_5")
	if !allowed {
		t.Fatalf("allowed = false, message %q", msg)
	}
	if judged {
		t.Fatal("auto-permission judge ran for an unmapped tool")
	}
	if len(rec.calls) != 1 {
		t.Fatalf("asker called %d times, want 1", len(rec.calls))
	}
	req := rec.calls[0]
	if req.ToolName != "claude-code:mcp__x__y" {
		t.Fatalf("ToolName = %q", req.ToolName)
	}
	if req.AgentName != claudeCodeAskLabel {
		t.Fatalf("AgentName = %q", req.AgentName)
	}
	if string(req.Args) != `{"k":1}` {
		t.Fatalf("Args = %s", req.Args)
	}
}

func TestClaudeCodeUnknownToolWithoutAskerIsDenied(t *testing.T) {
	a, _ := claudeCodeTestAgent(t)
	allowed, msg := a.decideClaudeCodeToolUse(context.Background(), "WebSearch", json.RawMessage(`{"query":"go"}`), "toolu_6")
	if allowed {
		t.Fatal("allowed = true with no approval surface")
	}
	if msg != claudeCodeNoSurfaceMessage {
		t.Fatalf("message = %q", msg)
	}
}

func TestClaudeCodeMalformedInputIsDenied(t *testing.T) {
	a, _ := claudeCodeTestAgent(t)
	cases := []struct {
		name, tool, input, want string
	}{
		{"truncated json", "Bash", `{"command":`, "not valid JSON"},
		{"wrong shape", "Bash", `{"command":5}`, "does not match the expected shape"},
		{"unmapped truncated", "mcp__x__y", `{"k":`, "not valid JSON"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			allowed, msg := a.decideClaudeCodeToolUse(context.Background(), tc.tool, json.RawMessage(tc.input), "toolu_7")
			if allowed {
				t.Fatal("allowed = true for malformed input")
			}
			if !strings.Contains(msg, tc.want) {
				t.Fatalf("message = %q, want it to contain %q", msg, tc.want)
			}
		})
	}
}

// claudeCodeWriteTestAgent builds an agent with the real write tool, a private
// HOME and its own work dir, and a permissive Allow rule for write. Only the
// path confinement and the pre-hooks can stop a Claude Code Write.
func claudeCodeWriteTestAgent(t *testing.T) *Agent {
	t.Helper()
	setHomeTree(t, t.TempDir())
	a := newTestAgent(&scriptedSideQueryClient{}, []tool.Tool{tool.WriteTool{}}, nil, nil)
	a.SetWorkDir(t.TempDir())
	a.permissions.SetRule("write", PermissionAllow)
	return a
}

func writeInput(t *testing.T, filePath, content string) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(map[string]string{"file_path": filePath, "content": content})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// TestClaudeCodeWriteRelativeEscapeIsDenied checks that the decide-only path
// refuses a write whose relative path escapes the work dir, even under an Allow
// rule. Absolute paths are not covered here: runToolCallWithContext takes a
// temporary allowed-root lease for every absolute write before the decision, so
// the confinement check cannot see them (see the report on that lease).
func TestClaudeCodeWriteRelativeEscapeIsDenied(t *testing.T) {
	a := claudeCodeWriteTestAgent(t)
	escape := "../../../../../../../../../../etc/ocode-test-nope.txt"

	allowed, msg := a.decideClaudeCodeToolUse(context.Background(), "Write", writeInput(t, escape, "x"), "toolu_w1")
	if allowed {
		t.Fatalf("allowed = true for a relative write escaping the work dir (message %q)", msg)
	}
	if msg == "" {
		t.Fatal("deny message is empty")
	}
	assertNotExists(t, "/etc/ocode-test-nope.txt")
}

// TestClaudeCodeWriteInsideWorkDirIsAllowed is the counterpart: a write inside
// the work dir with an Allow rule is approved, and still not executed.
func TestClaudeCodeWriteInsideWorkDirIsAllowed(t *testing.T) {
	a := claudeCodeWriteTestAgent(t)
	target := filepath.Join(a.WorkDir(), "ok.txt")

	allowed, msg := a.decideClaudeCodeToolUse(context.Background(), "Write", writeInput(t, target, "x"), "toolu_w2")
	if !allowed {
		t.Fatalf("allowed = false for a write inside the work dir, message %q", msg)
	}
	assertNotExists(t, target)
}

// TestClaudeCodePreHookBlocksDecision checks that a configured pre-hook runs
// for a Claude Code tool call and its block reaches the bridge as a denial,
// even under an Allow rule.
func TestClaudeCodePreHookBlocksDecision(t *testing.T) {
	a, target := claudeCodeTestAgent(t)
	a.permissions.SetRule("bash", PermissionAllow)
	a.config = &config.Config{Hooks: map[string]config.HookConfig{
		"bash": {Pre: []string{"echo policy-says-no >&2; exit 1"}},
	}}

	allowed, msg := a.decideClaudeCodeToolUse(context.Background(), "Bash", bashInput(t, "touch "+target), "toolu_h1")
	if allowed {
		t.Fatalf("allowed = true although a pre-hook blocked the call (message %q)", msg)
	}
	if !strings.Contains(msg, "pre-hook blocked") || !strings.Contains(msg, "policy-says-no") {
		t.Fatalf("message = %q, want the pre-hook block text", msg)
	}
	assertNotExists(t, target)
}
