package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"

	"github.com/u007/ocode/internal/changes"
	"github.com/u007/ocode/internal/crashguard"
)

// ClaudeCodeSessionMetadataKey is the session-metadata key holding the Claude
// Code CLI session id. It is persisted with the ocode transcript because
// ocode sends only the latest user turn on each call; losing the id silently
// loses the conversation inside Claude Code.
const ClaudeCodeSessionMetadataKey = "claude_code_session_id"

// ClaudeCodeModelAliases are the model aliases the installed CLI accepts.
// Shared by the TUI advisor section and the main-model picker so the two lists
// cannot drift.
var ClaudeCodeModelAliases = []string{
	"claude-sonnet-4-6",
	"claude-sonnet-5",
	"claude-sonnet-5-5",
	"claude-opus-4-8",
	"claude-opus-4-7",
	"claude-opus-5",
	"claude-opus-5-5",
	"claude-haiku-4-5",
	"claude-haiku-5-5",
	"claude-fable-5",
}

// ClaudeCodeAvailable reports whether the installed CLI can be resolved.
func ClaudeCodeAvailable() bool {
	_, err := exec.LookPath(claudeCLIPath())
	return err == nil
}

// ClaudeCodeMainModelIDs returns the selectable main-model ids when the CLI is
// installed. Empty when it is absent, so a picker never offers a model that
// cannot run.
func ClaudeCodeMainModelIDs() []string {
	if !ClaudeCodeAvailable() {
		return nil
	}
	ids := make([]string, 0, len(ClaudeCodeModelAliases))
	for _, model := range ClaudeCodeModelAliases {
		ids = append(ids, "claude-code/"+model)
	}
	return ids
}

// ClaudeCodeSessionStore persists and restores the Claude Code CLI session id
// for one ocode session. The session package registers the implementation
// through SetClaudeCodeSessionStore, keeping internal/agent free of a session
// import cycle (session already imports agent).
type ClaudeCodeSessionStore struct {
	Load func(projectRoot, sessionID string) (string, error)
	Save func(projectRoot, sessionID, claudeSessionID string) error
}

var claudeCodeSessionStore atomic.Pointer[ClaudeCodeSessionStore]

// SetClaudeCodeSessionStore installs the process-wide session-id store. A nil
// store disables persistence (useful in package tests).
func SetClaudeCodeSessionStore(store *ClaudeCodeSessionStore) {
	claudeCodeSessionStore.Store(store)
}

func currentClaudeCodeSessionStore() *ClaudeCodeSessionStore {
	return claudeCodeSessionStore.Load()
}

// chatClaudeCode runs the Claude Code CLI (claude -p) in stream-json mode and
// streams assistant deltas through c.OnDelta. The Claude Code session id is
// captured from the init event and reused on later turns via --resume, so the
// conversation retains its own history inside Claude Code (ocode only forwards
// the latest user turn).
func (c *GenericClient) chatClaudeCode(ctx context.Context, messages []Message, tools []map[string]interface{}) (*Message, error) {
	// Refuse a remote project's path: WorkDir names a directory on the SSH/WSL
	// host, while the claude binary this process would start runs locally.
	if c.ProjectHost != "" {
		return nil, fmt.Errorf("claude-code: remote project %q is not supported; run the chat on its host", c.ProjectHost)
	}
	// A mode callback is mandatory: a background/side client that never received
	// one must not fall through to a permissive default.
	if c.PermissionModeFn == nil {
		return nil, fmt.Errorf("claude-code: permission mode is unavailable; refusing to run")
	}
	claudeMode, err := claudeCodePermissionMode(c.PermissionModeFn())
	if err != nil {
		return nil, err
	}

	c.hydrateClaudeSessionID()
	if c.claudeSessionIDValue() == "" && hasPriorClaudeConversation(messages) {
		return nil, fmt.Errorf("claude-code: the stored Claude session id is missing, so earlier turns cannot be resumed; start a new chat")
	}

	// Concatenate every user message since the last assistant message so a
	// synthetic tail block ([ocode:todo], [ocode:discovery], ...) cannot be
	// mistaken for the human prompt.
	var userParts []string
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "assistant" {
			break
		}
		if messages[i].Role == "user" && strings.TrimSpace(messages[i].Content) != "" {
			userParts = append([]string{messages[i].Content}, userParts...)
		}
	}
	lastUser := strings.TrimSpace(strings.Join(userParts, "\n\n"))
	if lastUser == "" {
		return nil, fmt.Errorf("claude-code: no user message to send")
	}

	claudePath := c.claudeCLIPathOverride
	if claudePath == "" {
		claudePath = claudeCLIPath()
	}
	if _, err := exec.LookPath(claudePath); err != nil {
		return nil, fmt.Errorf("claude-code: `claude` was not found on PATH; install Claude Code and run `claude` to sign in: %w", err)
	}

	// The permission flags depend on the mode. Manual mode sends every prompt
	// to ocode through the approval bridge for the life of this run. Bypass
	// mode has no prompts to answer, so inside -p they are denied outright.
	// One run state per chat call: the approval bridge reads the transcript and
	// the judge-answer cap from it, and its notice lines join the tool rows below.
	turn := newClaudeCodeTurn(messages)
	var permissionArgs []string
	switch claudeMode {
	case "manual":
		if c.ClaudeCodeApproveFn == nil {
			return nil, fmt.Errorf("claude-code: no approval bridge is bound to this client; refusing to run in normal mode")
		}
		bridge, err := startClaudeCodeBridge(claudeCodeApproveForTurn(c.ClaudeCodeApproveFn, turn))
		if err != nil {
			return nil, fmt.Errorf("claude-code: start approval bridge: %w", err)
		}
		defer func() {
			if err := bridge.Close(); err != nil {
				emitDebug("CLAUDE-CODE", fmt.Sprintf("close approval bridge: %v", err))
			}
		}()
		mcpConfig, err := writeClaudeCodeMCPConfig(bridge.URL, bridge.Token)
		if err != nil {
			return nil, fmt.Errorf("claude-code: %w", err)
		}
		defer func() {
			if err := os.Remove(mcpConfig); err != nil {
				emitDebug("CLAUDE-CODE", fmt.Sprintf("remove mcp config: %v", err))
			}
		}()
		permissionArgs = []string{
			"--permission-prompts", "host",
			"--mcp-config", mcpConfig,
			"--permission-prompt-tool", claudeCodeApprovalTool,
			// The model must not call the prompt tool itself; the CLI still uses it
			// as --permission-prompt-tool.
			"--disallowedTools", claudeCodeApprovalTool,
		}
	case "bypassPermissions":
		permissionArgs = []string{"--permission-prompts", "none"}
	default:
		return nil, fmt.Errorf("claude-code: unsupported CLI permission mode %q", claudeMode)
	}

	args := []string{
		"-p",
		"--verbose",
		"--output-format", "stream-json",
		"--input-format", "text",
		"--model", c.Model,
		"--permission-mode", claudeMode,
	}
	args = append(args, permissionArgs...)
	if sid := c.claudeSessionIDValue(); sid != "" {
		args = append(args, "--resume", sid)
	}
	// Do NOT forward ocode's system messages: they describe ocode tools that
	// this path never exposes, and they duplicate the CLAUDE.md that the CLI
	// already loads from cmd.Dir. ocode's tool definitions are omitted on
	// purpose — Claude Code owns its own tool set.
	args = append(args, "--", lastUser)

	// Reuse the bash pre/post stat hook: Claude Code writes files inside its own
	// process, so this is the only place its changes can enter ocode's change
	// tracking. The hook advances the advisor write watermark without creating
	// undo snapshots (there is no pre-image to restore).
	// shortcut: the stat diff cannot distinguish Claude Code writes from a
	// concurrent user/formatter/other-session write in WorkDir; detect by diff,
	// upgrade when the CLI exposes a tool-call file list.
	var recorder changes.BashRecorder
	var baseline changes.BashBaseline
	exitCode := 0
	if c.Changes != nil && c.WorkDir != "" {
		recorder = changes.NewStatBashRecorder(c.WorkDir, c.Changes,
			changes.WithWriteHook(func(touches []changes.BashTouch) {
				if c.SnapshotStore == nil {
					return
				}
				for _, touch := range touches {
					c.SnapshotStore.RecordExternalWrite(touch.Path)
				}
			}))
		baseline = recorder.Pre()
		defer func() { recorder.Post(baseline, "claude-code", exitCode) }()
	}

	// No shared process-spawn helper exists for a streamed CLI: this path needs
	// a stdout pipe, its own process group, and a login-shell environment.
	cmd := exec.CommandContext(ctx, claudePath, args...)
	if c.WorkDir != "" {
		cmd.Dir = c.WorkDir
	}
	cmd.Env = withLoginShellPath(cleanEnvForTerminal())
	// Start in its own process group where the platform supports it so Stop can
	// kill the whole tree.
	configureClaudeCodeCommand(cmd)

	devNull, err := os.Open(os.DevNull)
	if err != nil {
		return nil, fmt.Errorf("claude-code: open /dev/null: %w", err)
	}
	cmd.Stdin = devNull
	defer devNull.Close()

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("claude-code: stdout pipe: %w", err)
	}
	var stderrBuf strings.Builder
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("claude-code: start: %w", err)
	}
	done := make(chan struct{})
	crashguard.Go(func() {
		select {
		case <-ctx.Done():
			killClaudeCodeProcess(cmd)
		case <-done:
		}
	})

	var resultText string
	var resultIsError bool
	var sessionID string
	var usage *TokenUsage
	var toolNotices []string
	toolNames := map[string]string{}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024*4)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var ev map[string]any
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			emitDebug("CLAUDE-CODE", fmt.Sprintf("json parse error: %v (line=%s)", err, truncateCC(line, 200)))
			continue
		}
		// A judge answer lands between its tool_use row and its tool_result row,
		// so moving the queued lines here keeps the transcript in stream order.
		toolNotices = c.appendClaudeJudgeNotices(toolNotices, turn)
		typ, _ := ev["type"].(string)
		switch typ {
		case "system":
			if sub, _ := ev["subtype"].(string); sub == "init" {
				if sid, ok := ev["session_id"].(string); ok && sid != "" {
					sessionID = sid
					c.persistClaudeSessionID(sid)
				}
			}
		case "assistant":
			msg, _ := ev["message"].(map[string]any)
			content, _ := msg["content"].([]any)
			for _, cb := range content {
				b, _ := cb.(map[string]any)
				switch b["type"] {
				case "text":
					if t, ok := b["text"].(string); ok && t != "" {
						if fn := c.onDelta(); fn != nil {
							fn("text", t)
						}
					}
				case "tool_use":
					name, _ := b["name"].(string)
					id, _ := b["id"].(string)
					if id != "" && name != "" {
						toolNames[id] = name
					}
					if name != "" {
						input, _ := b["input"].(map[string]any)
						line := c.redactClaudeNotice("→ " + name + claudeToolInputSummary(input))
						toolNotices = appendClaudeToolNotice(toolNotices, line)
					}
				}
			}
		case "user":
			// Tool results are Claude Code's internal execution detail. They are
			// surfaced as display-only transcript notices; they must never become
			// ocode ToolCalls, which would be executed a second time by Step.
			msg, _ := ev["message"].(map[string]any)
			content, _ := msg["content"].([]any)
			for _, cb := range content {
				b, _ := cb.(map[string]any)
				if b["type"] != "tool_result" {
					continue
				}
				id, _ := b["tool_use_id"].(string)
				name := toolNames[id]
				if name == "" {
					name = "tool"
				}
				status := "ok"
				if isErr, _ := b["is_error"].(bool); isErr {
					status = "error"
				}
				toolNotices = appendClaudeToolNotice(toolNotices, "← "+name+" "+status)
			}
		case "result":
			if r, ok := ev["result"].(string); ok {
				resultText = r
			}
			resultIsError, _ = ev["is_error"].(bool)
			usage = claudeCodeUsage(ev["usage"])
			if sid, ok := ev["session_id"].(string); ok && sessionID == "" {
				sessionID = sid
				c.persistClaudeSessionID(sid)
			}
		}
	}
	if scErr := scanner.Err(); scErr != nil {
		emitDebug("CLAUDE-CODE", fmt.Sprintf("scanner error: %v", scErr))
	}
	waitErr := cmd.Wait()
	close(done)
	devNull.Close()
	if waitErr != nil || resultIsError {
		exitCode = 1
		// The CLI reports most failures (e.g. "Not logged in") as an is_error
		// result on stdout with an empty stderr, so both are surfaced.
		detail := strings.TrimSpace(strings.TrimSpace(stderrBuf.String()) + "\n" + strings.TrimSpace(resultText))
		if claudeCodeAuthError(detail) {
			return nil, fmt.Errorf("claude-code: not authenticated; run `claude` and sign in again: %s", detail)
		}
		if waitErr != nil {
			return nil, fmt.Errorf("claude-code: %w\n%s", waitErr, detail)
		}
		return nil, fmt.Errorf("claude-code: %s", detail)
	}
	if strings.TrimSpace(resultText) == "" {
		exitCode = 1
		return nil, fmt.Errorf("claude-code: empty result")
	}
	// The final result is the transcript value; live text already went through
	// OnDelta, so it must not be streamed a second time here.
	modelID := "claude-code/" + c.Model
	if usage != nil {
		usage.DebugLog(modelID, c.emitDebug)
	}
	toolNotices = c.appendClaudeJudgeNotices(toolNotices, turn)
	msg := &Message{Role: "assistant", Content: resultText, Model: modelID, Usage: usage}
	if len(toolNotices) > 0 {
		// Notice is UI-only, persisted with the transcript, and never sent to a
		// model. It gives the web/TUI a tool-activity row without creating
		// executable tool calls.
		msg.Notice = "Claude Code tools:\n" + strings.Join(toolNotices, "\n")
	}
	return msg, nil
}

// appendClaudeToolNotice bounds the display-only activity row. A long agent run
// can emit hundreds of tool events; the transcript should stay readable.
func appendClaudeToolNotice(list []string, line string) []string {
	const maxClaudeToolNotices = 40
	if len(list) >= maxClaudeToolNotices {
		return list
	}
	return append(list, truncateCC(strings.TrimSpace(line), 400))
}

// appendClaudeJudgeNotices moves the auto-continue judge's answer lines for this
// run into the tool-activity rows. Each line is redacted like the tool rows.
func (c *GenericClient) appendClaudeJudgeNotices(list []string, turn *claudeCodeTurn) []string {
	if turn == nil {
		return list
	}
	for _, line := range turn.takeJudgeNotices() {
		list = appendClaudeToolNotice(list, c.redactClaudeNotice(line))
	}
	return list
}

// redactClaudeNotice keeps tool input summaries (commands, URLs, paths) from
// persisting obvious secrets. The notice is display-only, but it is persisted
// with the transcript.
func (c *GenericClient) redactClaudeNotice(text string) string {
	if c == nil || c.Redaction == nil || c.Redaction.Registry == nil {
		return text
	}
	return redactText(text, c.Redaction.Registry)
}

func claudeToolInputSummary(input map[string]any) string {
	if len(input) == 0 {
		return ""
	}
	keys := []string{"command", "file_path", "path", "pattern", "query", "url", "description"}
	var parts []string
	for _, key := range keys {
		value, ok := input[key].(string)
		if !ok || strings.TrimSpace(value) == "" {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s=%q", key, truncateCC(value, 120)))
	}
	if len(parts) == 0 {
		return ""
	}
	return " " + strings.Join(parts, " ")
}

func claudeCodeUsage(raw any) *TokenUsage {
	if raw == nil {
		return nil
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	usage, err := parseAnthropicUsage(encoded)
	if err != nil {
		return nil
	}
	return usage
}

// claudeCodePermissionMode maps ocode's permission mode to the CLI's mode.
// yolo lets Claude Code edit and run any command (bypassPermissions, with
// --permission-prompts none). Normal mode is always "manual": every tool call
// is asked through the approval bridge, where ocode's own permission pipeline
// and auto-permission judge decide. Claude's own "auto" classifier is never
// used.
func claudeCodePermissionMode(mode PermissionMode) (string, error) {
	switch mode {
	case PermissionModeLocked, PermissionModeSandbox:
		return "", fmt.Errorf("claude-code: refused in %s permission mode", mode)
	case PermissionModeYOLO:
		return "bypassPermissions", nil
	case PermissionModeNormal:
		return "manual", nil
	default:
		return "", fmt.Errorf("claude-code: unsupported permission mode %q", mode)
	}
}

// hasPriorClaudeConversation reports whether earlier turns exist. Synthetic
// user-role tail blocks ([ocode:todo], [ocode:discovery], ...) are not human
// turns: counting them made a brand-new chat look like a resumed one.
func hasPriorClaudeConversation(messages []Message) bool {
	humanCount := 0
	for _, m := range messages {
		switch m.Role {
		case "assistant":
			if strings.TrimSpace(m.Content) != "" {
				return true
			}
		case "user":
			if strings.TrimSpace(m.Content) == "" || !isHumanPromptMessage(m) {
				continue
			}
			humanCount++
			if humanCount > 1 {
				return true
			}
		}
	}
	return false
}

func claudeCodeAuthError(stderr string) bool {
	lower := strings.ToLower(stderr)
	for _, needle := range []string{
		"invalid api key",
		"not logged in",
		"please run /login",
		"oauth token has expired",
		"authentication_error",
		"unauthorized",
		"401",
	} {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	return false
}

func (c *GenericClient) claudeConversationID() string {
	c.sessionMu.RLock()
	defer c.sessionMu.RUnlock()
	if c.opencodeSessionIDOverride != "" {
		return c.opencodeSessionIDOverride
	}
	return c.sessionID
}

func (c *GenericClient) claudeSessionIDValue() string {
	c.sessionMu.RLock()
	defer c.sessionMu.RUnlock()
	return c.claudeSessionID
}

func (c *GenericClient) setClaudeSessionID(id string) {
	c.sessionMu.Lock()
	c.claudeSessionID = id
	c.sessionMu.Unlock()
}

func (c *GenericClient) hydrateClaudeSessionID() {
	if c.claudeSessionIDValue() != "" {
		return
	}
	store := currentClaudeCodeSessionStore()
	if store == nil || store.Load == nil {
		return
	}
	sessionID := c.claudeConversationID()
	if sessionID == "" || c.WorkDir == "" {
		return
	}
	id, err := store.Load(c.WorkDir, sessionID)
	if err != nil {
		emitDebug("CLAUDE-CODE", fmt.Sprintf("load session id for %s: %v", sessionID, err))
		return
	}
	if id != "" {
		c.setClaudeSessionID(id)
	}
}

func (c *GenericClient) persistClaudeSessionID(id string) {
	if id == "" {
		return
	}
	c.setClaudeSessionID(id)
	store := currentClaudeCodeSessionStore()
	if store == nil || store.Save == nil {
		return
	}
	sessionID := c.claudeConversationID()
	if sessionID == "" || c.WorkDir == "" {
		return
	}
	if err := store.Save(c.WorkDir, sessionID, id); err != nil {
		emitDebug("CLAUDE-CODE", fmt.Sprintf("save session id for %s: %v", sessionID, err))
	}
}

func truncateCC(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
