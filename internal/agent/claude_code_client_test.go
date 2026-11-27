//go:build !windows

package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/changes"
	"github.com/u007/ocode/internal/snapshot"
)

// allowAllClaudeApprove is the approve callback for fake-CLI tests that run in
// normal mode but do not exercise the approval path itself.
func allowAllClaudeApprove(context.Context, string, json.RawMessage, string) (bool, json.RawMessage, string) {
	return true, nil, ""
}

// copyMCPConfigBody is the fake-CLI script fragment that copies the file given
// to --mcp-config into $FAKE_CLAUDE_MCP, so the test can read it after the run
// has removed the temp file.
const copyMCPConfigBody = `while [ "$#" -gt 0 ]; do
  if [ "$1" = "--mcp-config" ]; then cp "$2" "$FAKE_CLAUDE_MCP"; fi
  shift
done
`

func fakeClaude(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "claude")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$FAKE_CLAUDE_ARGS\"\n" + body
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func runFakeClaude(t *testing.T, c *GenericClient, messages []Message) (*Message, error) {
	t.Helper()
	return c.chatClaudeCode(context.Background(), messages, nil)
}

func readFakeArgs(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile(os.Getenv("FAKE_CLAUDE_ARGS"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

func argValue(args []string, flag string) string {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag {
			return args[i+1]
		}
	}
	return ""
}

func hasArg(args []string, flag string) bool {
	for _, arg := range args {
		if arg == flag {
			return true
		}
	}
	return false
}

func TestChatClaudeCodePermissionModes(t *testing.T) {
	cases := []struct {
		name       string
		mode       PermissionMode
		want       string
		wantPrompt string
	}{
		{"normal is manual", PermissionModeNormal, "manual", "host"},
		{"yolo bypasses permissions", PermissionModeYOLO, "bypassPermissions", "none"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			argsFile := filepath.Join(t.TempDir(), "args")
			t.Setenv("FAKE_CLAUDE_ARGS", argsFile)
			mcpCopy := filepath.Join(t.TempDir(), "mcp.json")
			t.Setenv("FAKE_CLAUDE_MCP", mcpCopy)
			path := fakeClaude(t, copyMCPConfigBody+"printf '%s\\n' '{\"type\":\"result\",\"result\":\"ok\"}'\n")
			mode := tc.mode
			c := &GenericClient{
				Provider:              "claude-code",
				Model:                 "sonnet",
				WorkDir:               t.TempDir(),
				claudeCLIPathOverride: path,
				PermissionModeFn:      func() PermissionMode { return mode },
				ClaudeCodeApproveFn:   allowAllClaudeApprove,
			}
			if _, err := runFakeClaude(t, c, []Message{{Role: "system", Content: "ocode tool rules"}, {Role: "user", Content: "do it"}}); err != nil {
				t.Fatal(err)
			}
			args := readFakeArgs(t)
			if got := argValue(args, "--permission-mode"); got != tc.want {
				t.Fatalf("permission-mode = %q, want %q (args=%v)", got, tc.want, args)
			}
			if hasArg(args, "--append-system-prompt") {
				t.Fatalf("system prompt forwarded: %v", args)
			}
			if got := argValue(args, "--permission-prompts"); got != tc.wantPrompt {
				t.Fatalf("permission-prompts = %q, want %q", got, tc.wantPrompt)
			}
			if got := argValue(args, "--model"); got != "sonnet" {
				t.Fatalf("model = %q", got)
			}
			if tc.want == "manual" {
				if got := argValue(args, "--permission-prompt-tool"); got != "mcp__ocode__approve" {
					t.Fatalf("permission-prompt-tool = %q, want mcp__ocode__approve", got)
				}
				if got := argValue(args, "--mcp-config"); got == "" {
					t.Fatalf("manual mode missing --mcp-config: %v", args)
				}
				// The model must not see the prompt tool as a callable tool.
				if got := argValue(args, "--disallowedTools"); got != "mcp__ocode__approve" {
					t.Fatalf("disallowedTools = %q, want mcp__ocode__approve (args=%v)", got, args)
				}
			} else {
				if hasArg(args, "--mcp-config") || hasArg(args, "--permission-prompt-tool") || hasArg(args, "--disallowedTools") {
					t.Fatalf("bypass mode must not start the approval bridge: %v", args)
				}
			}
		})
	}
}

// TestChatClaudeCodeManualModeWiresApprovalBridge checks the MCP config the CLI
// receives in normal mode: the loopback URL, a bearer token and the request
// timeout that keeps a human approval from being aborted after 60 seconds.
func TestChatClaudeCodeManualModeWiresApprovalBridge(t *testing.T) {
	argsFile := filepath.Join(t.TempDir(), "args")
	t.Setenv("FAKE_CLAUDE_ARGS", argsFile)
	mcpCopy := filepath.Join(t.TempDir(), "mcp.json")
	t.Setenv("FAKE_CLAUDE_MCP", mcpCopy)
	path := fakeClaude(t, copyMCPConfigBody+"printf '%s\\n' '{\"type\":\"result\",\"result\":\"ok\"}'\n")
	c := &GenericClient{
		Provider:              "claude-code",
		Model:                 "sonnet",
		WorkDir:               t.TempDir(),
		claudeCLIPathOverride: path,
		PermissionModeFn:      func() PermissionMode { return PermissionModeNormal },
		ClaudeCodeApproveFn:   allowAllClaudeApprove,
	}
	if _, err := runFakeClaude(t, c, []Message{{Role: "user", Content: "x"}}); err != nil {
		t.Fatal(err)
	}
	args := readFakeArgs(t)
	if got := argValue(args, "--permission-prompts"); got != "host" {
		t.Fatalf("permission-prompts = %q, want host", got)
	}
	if got := argValue(args, "--permission-prompt-tool"); got != "mcp__ocode__approve" {
		t.Fatalf("permission-prompt-tool = %q", got)
	}
	if got := argValue(args, "--disallowedTools"); got != "mcp__ocode__approve" {
		t.Fatalf("disallowedTools = %q, want mcp__ocode__approve", got)
	}
	data, err := os.ReadFile(mcpCopy)
	if err != nil {
		t.Fatalf("mcp config was not handed to the CLI: %v", err)
	}
	var cfg struct {
		MCPServers map[string]struct {
			Type    string            `json:"type"`
			URL     string            `json:"url"`
			Headers map[string]string `json:"headers"`
			Timeout int               `json:"timeout"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("mcp config is not JSON: %v\n%s", err, data)
	}
	server, ok := cfg.MCPServers["ocode"]
	if !ok {
		t.Fatalf("mcp config has no ocode server: %s", data)
	}
	if server.Type != "http" || !strings.HasPrefix(server.URL, "http://127.0.0.1:") || !strings.HasSuffix(server.URL, "/mcp") {
		t.Fatalf("server = %+v, want loopback http /mcp url", server)
	}
	if !regexp.MustCompile(`^Bearer [0-9a-f]{64}$`).MatchString(server.Headers["Authorization"]) {
		t.Fatalf("Authorization header = %q, want Bearer + 64 hex", server.Headers["Authorization"])
	}
	if server.Timeout != 1200000 {
		t.Fatalf("timeout = %d, want 1200000", server.Timeout)
	}
	mcpPath := argValue(args, "--mcp-config")
	if mcpPath == "" {
		t.Fatalf("manual mode missing --mcp-config: %v", args)
	}
	if _, err := os.Stat(mcpPath); !os.IsNotExist(err) {
		t.Fatalf("mcp config temp file %s was not removed after the run (stat err=%v)", mcpPath, err)
	}
}

// TestChatClaudeCodeNormalModeRequiresApproveFn checks normal mode refuses to
// start the CLI when no approval bridge is bound, rather than falling back to
// --permission-prompts none.
func TestChatClaudeCodeNormalModeRequiresApproveFn(t *testing.T) {
	argsFile := filepath.Join(t.TempDir(), "args")
	t.Setenv("FAKE_CLAUDE_ARGS", argsFile)
	path := fakeClaude(t, "printf '%s\\n' '{\"type\":\"result\",\"result\":\"ok\"}'\n")
	c := &GenericClient{
		Provider:              "claude-code",
		Model:                 "sonnet",
		WorkDir:               t.TempDir(),
		claudeCLIPathOverride: path,
		PermissionModeFn:      func() PermissionMode { return PermissionModeNormal },
	}
	_, err := runFakeClaude(t, c, []Message{{Role: "user", Content: "x"}})
	if err == nil || !strings.Contains(err.Error(), "no approval bridge") {
		t.Fatalf("error = %v, want missing approval bridge refusal", err)
	}
	if _, statErr := os.Stat(argsFile); statErr == nil {
		t.Fatal("the CLI must not start when no approval bridge is bound")
	}
}

func TestChatClaudeCodeRefusesUnsafeModesAndMissingHooks(t *testing.T) {
	for _, mode := range []PermissionMode{PermissionModeLocked, PermissionModeSandbox} {
		c := &GenericClient{
			Provider:         "claude-code",
			Model:            "sonnet",
			PermissionModeFn: func() PermissionMode { return mode },
		}
		if _, err := runFakeClaude(t, c, []Message{{Role: "user", Content: "x"}}); err == nil {
			t.Fatalf("mode %q: expected refusal", mode)
		}
	}
	c := &GenericClient{Provider: "claude-code", Model: "sonnet"}
	if _, err := runFakeClaude(t, c, []Message{{Role: "user", Content: "x"}}); err == nil || !strings.Contains(err.Error(), "permission mode is unavailable") {
		t.Fatalf("nil PermissionModeFn error = %v", err)
	}
}

func TestChatClaudeCodeRefusesRemoteProject(t *testing.T) {
	c := &GenericClient{
		Provider:         "claude-code",
		Model:            "sonnet",
		ProjectHost:      "me@host",
		PermissionModeFn: func() PermissionMode { return PermissionModeNormal },
	}
	if _, err := runFakeClaude(t, c, []Message{{Role: "user", Content: "x"}}); err == nil || !strings.Contains(err.Error(), "remote project") {
		t.Fatalf("remote error = %v", err)
	}
}

func TestChatClaudeCodeMissingCLI(t *testing.T) {
	c := &GenericClient{
		Provider:              "claude-code",
		Model:                 "sonnet",
		claudeCLIPathOverride: filepath.Join(t.TempDir(), "missing-claude"),
		PermissionModeFn:      func() PermissionMode { return PermissionModeNormal },
	}
	if _, err := runFakeClaude(t, c, []Message{{Role: "user", Content: "x"}}); err == nil || !strings.Contains(err.Error(), "not found on PATH") {
		t.Fatalf("missing CLI error = %v", err)
	}
}

func TestChatClaudeCodeAuthError(t *testing.T) {
	path := fakeClaude(t, "echo 'Invalid API key' >&2\nexit 1\n")
	c := &GenericClient{
		Provider:              "claude-code",
		Model:                 "sonnet",
		claudeCLIPathOverride: path,
		PermissionModeFn:      func() PermissionMode { return PermissionModeNormal },
		ClaudeCodeApproveFn:   allowAllClaudeApprove,
	}
	if _, err := runFakeClaude(t, c, []Message{{Role: "user", Content: "x"}}); err == nil || !strings.Contains(err.Error(), "not authenticated") {
		t.Fatalf("auth error = %v", err)
	}
}

func TestChatClaudeCodeStreamsOnceAndPersistsSessionID(t *testing.T) {
	argsFile := filepath.Join(t.TempDir(), "args")
	t.Setenv("FAKE_CLAUDE_ARGS", argsFile)
	path := fakeClaude(t, strings.Join([]string{
		"printf '%s\\n' '{\"type\":\"system\",\"subtype\":\"init\",\"session_id\":\"new-id\"}'",
		"printf '%s\\n' '{\"type\":\"assistant\",\"message\":{\"content\":[{\"type\":\"text\",\"text\":\"hello\"},{\"type\":\"tool_use\",\"id\":\"tool-1\",\"name\":\"Bash\",\"input\":{\"command\":\"echo hi\"}}]}}'",
		"printf '%s\\n' '{\"type\":\"user\",\"message\":{\"content\":[{\"type\":\"tool_result\",\"tool_use_id\":\"tool-1\",\"content\":\"hi\"}]}}'",
		"printf '%s\\n' '{\"type\":\"result\",\"result\":\"hello final\",\"session_id\":\"new-id\",\"total_cost_usd\":0.0125,\"usage\":{\"input_tokens\":10,\"output_tokens\":4,\"cache_read_input_tokens\":2,\"cache_creation_input_tokens\":1}}'",
	}, "\n")+"\n")
	var saved string
	SetClaudeCodeSessionStore(&ClaudeCodeSessionStore{
		Load: func(projectRoot, sessionID string) (string, error) { return "stored-id", nil },
		Save: func(projectRoot, sessionID, claudeSessionID string) error {
			saved = claudeSessionID
			return nil
		},
	})
	t.Cleanup(func() { SetClaudeCodeSessionStore(nil) })

	var deltas []string
	c := &GenericClient{
		Provider:              "claude-code",
		Model:                 "sonnet",
		WorkDir:               t.TempDir(),
		claudeCLIPathOverride: path,
		PermissionModeFn:      func() PermissionMode { return PermissionModeNormal },
		ClaudeCodeApproveFn:   allowAllClaudeApprove,
	}
	c.setSessionID("ses_test")
	c.SetOnDelta(func(kind, text string) { deltas = append(deltas, kind+":"+text) })

	msg, err := runFakeClaude(t, c, []Message{{Role: "user", Content: "x"}})
	if err != nil {
		t.Fatal(err)
	}
	if msg.Content != "hello final" {
		t.Fatalf("content = %q", msg.Content)
	}
	if len(deltas) != 1 || deltas[0] != "text:hello" {
		t.Fatalf("deltas = %v", deltas)
	}
	if len(msg.ToolCalls) != 0 {
		t.Fatalf("tool calls must stay display-only, got %#v", msg.ToolCalls)
	}
	for _, want := range []string{"→ Bash", "command=\"echo hi\"", "← Bash ok"} {
		if !strings.Contains(msg.Notice, want) {
			t.Fatalf("notice %q missing %q", msg.Notice, want)
		}
	}
	if msg.Usage == nil || msg.Usage.PromptTokens == nil || *msg.Usage.PromptTokens != 10 {
		t.Fatalf("prompt usage = %#v", msg.Usage)
	}
	if msg.Usage.CompletionTokens == nil || *msg.Usage.CompletionTokens != 4 {
		t.Fatalf("completion usage = %#v", msg.Usage)
	}
	if msg.Usage.CacheReadTokens == nil || *msg.Usage.CacheReadTokens != 2 {
		t.Fatalf("cache read usage = %#v", msg.Usage)
	}
	if msg.Usage.CacheWriteTokens == nil || *msg.Usage.CacheWriteTokens != 1 {
		t.Fatalf("cache write usage = %#v", msg.Usage)
	}
	if saved != "new-id" {
		t.Fatalf("saved session id = %q", saved)
	}
	args := readFakeArgs(t)
	if got := argValue(args, "--resume"); got != "stored-id" {
		t.Fatalf("resume = %q, want stored-id", got)
	}
}

func TestChatClaudeCodeSurfacesErrorResult(t *testing.T) {
	t.Setenv("FAKE_CLAUDE_ARGS", filepath.Join(t.TempDir(), "args"))
	path := fakeClaude(t, `printf '%s\n' '{"type":"result","is_error":true,"result":"Not logged in · Please run /login"}'
exit 1
`)
	c := &GenericClient{
		Provider:              "claude-code",
		Model:                 "sonnet",
		WorkDir:               t.TempDir(),
		claudeCLIPathOverride: path,
		PermissionModeFn:      func() PermissionMode { return PermissionModeNormal },
		ClaudeCodeApproveFn:   allowAllClaudeApprove,
	}
	_, err := runFakeClaude(t, c, []Message{{Role: "user", Content: "hi"}})
	if err == nil || !strings.Contains(err.Error(), "not authenticated") || !strings.Contains(err.Error(), "Not logged in") {
		t.Fatalf("error = %v, want the CLI's not-logged-in result surfaced", err)
	}
}

func TestChatClaudeCodeRefusesMissingSessionMidConversation(t *testing.T) {
	SetClaudeCodeSessionStore(&ClaudeCodeSessionStore{
		Load: func(projectRoot, sessionID string) (string, error) { return "", nil },
		Save: func(projectRoot, sessionID, claudeSessionID string) error { return nil },
	})
	t.Cleanup(func() { SetClaudeCodeSessionStore(nil) })
	c := &GenericClient{
		Provider:         "claude-code",
		Model:            "sonnet",
		WorkDir:          t.TempDir(),
		PermissionModeFn: func() PermissionMode { return PermissionModeNormal },
	}
	c.setSessionID("ses_test")
	_, err := runFakeClaude(t, c, []Message{
		{Role: "user", Content: "first"},
		{Role: "assistant", Content: "answer"},
		{Role: "user", Content: "second"},
	})
	if err == nil || !strings.Contains(err.Error(), "session id is missing") {
		t.Fatalf("error = %v", err)
	}
}

func TestHasPriorClaudeConversationIgnoresSyntheticTailBlocks(t *testing.T) {
	fresh := []Message{
		{Role: "user", Content: "haiku 5.5 missing"},
		{Role: "user", Content: "[ocode:todo] open items"},
		{Role: "user", Content: "[ocode:discovery] tools"},
	}
	if hasPriorClaudeConversation(fresh) {
		t.Fatal("a first turn with synthetic tail blocks must not count as a prior conversation")
	}
	if !hasPriorClaudeConversation([]Message{{Role: "user", Content: "first"}, {Role: "user", Content: "second"}}) {
		t.Fatal("two human prompts are a prior conversation")
	}
}

func TestChatClaudeCodeRecordsExternalWrites(t *testing.T) {
	work := t.TempDir()
	path := fakeClaude(t, strings.Join([]string{
		"echo changed > out.txt",
		"printf '%s\\n' '{\"type\":\"result\",\"result\":\"ok\"}'",
	}, "\n")+"\n")
	registry := changes.NewRegistry()
	store := snapshot.NewStore(snapshot.NewAgentID(), t.TempDir())
	if err := registry.AttachSnapshotStore("main", store); err != nil {
		t.Fatal(err)
	}
	c := &GenericClient{
		Provider:              "claude-code",
		Model:                 "sonnet",
		WorkDir:               work,
		Changes:               registry,
		SnapshotStore:         store,
		claudeCLIPathOverride: path,
		PermissionModeFn:      func() PermissionMode { return PermissionModeNormal },
		ClaudeCodeApproveFn:   allowAllClaudeApprove,
	}
	if _, err := runFakeClaude(t, c, []Message{{Role: "user", Content: "x"}}); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, change := range registry.List() {
		if strings.HasSuffix(change.OriginalPath, "out.txt") {
			found = true
			if change.Undoable {
				t.Fatal("external write must not be undoable")
			}
		}
	}
	if !found {
		t.Fatalf("Claude Code write missing from changes: %#v", registry.List())
	}
	if store.LastWriteSeq() == 0 {
		t.Fatal("external write did not advance the advisor watermark")
	}
}

func TestClaudeCodePermissionModeTable(t *testing.T) {
	if got, err := claudeCodePermissionMode(PermissionModeNormal); err != nil || got != "manual" {
		t.Fatalf("normal = %q, %v", got, err)
	}
	if got, err := claudeCodePermissionMode(PermissionModeYOLO); err != nil || got != "bypassPermissions" {
		t.Fatalf("yolo = %q, %v", got, err)
	}
	for _, mode := range []PermissionMode{PermissionModeLocked, PermissionModeSandbox} {
		if _, err := claudeCodePermissionMode(mode); err == nil {
			t.Fatalf("mode %q: expected error", mode)
		}
	}
}

func Example_claudeCodePermissionMode() {
	mode, _ := claudeCodePermissionMode(PermissionModeNormal)
	fmt.Println(mode)
	// Output: manual
}
