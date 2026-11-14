package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/u007/ocode/internal/plugins"
	"github.com/u007/ocode/internal/tool"
)

// Claude Code PostToolUse hooks for the tools an agent runs. The hooks come
// from Claude Code-format plugins (internal/plugins/claudehooks.go). Each
// matching hook gets the call as JSON on stdin and may replace the result, or
// add to it, through its reply on stdout.

// claudeToolNames maps ocode built-in tool names to the Claude Code tool names
// that hook matchers are written against. Tools not listed keep their ocode
// name, and MCP tools are named by pluginHookToolName.
var claudeToolNames = map[string]string{
	"bash":      "Bash",
	"read":      "Read",
	"write":     "Write",
	"edit":      "Edit",
	"multiedit": "MultiEdit",
	"grep":      "Grep",
	"glob":      "Glob",
	"webfetch":  "WebFetch",
	"websearch": "WebSearch",
	"task":      "Task",
}

// claudeToolName returns the Claude Code name of an ocode built-in tool, or the
// ocode name when there is no mapping.
func claudeToolName(ocodeName string) string {
	if n, ok := claudeToolNames[ocodeName]; ok {
		return n
	}
	return ocodeName
}

// pluginHookToolName returns the name a hook matcher sees for the tool name.
// An MCP tool is mcp__<server>__<tool>, derived the same way as the MCP app
// lookup (the ocode name is <server>_<tool>).
func (a *Agent) pluginHookToolName(name string) string {
	a.toolsMu.RLock()
	server, ok := a.mcpServers[name]
	a.toolsMu.RUnlock()
	if ok && server != "" {
		return "mcp__" + server + "__" + strings.TrimPrefix(name, server+"_")
	}
	return claudeToolName(name)
}

// isSentinelResult reports whether a tool result is an unanswered ask (a
// permission or question prompt). Those results are control messages, not tool
// output, so no hook sees them.
func isSentinelResult(result string) bool {
	return tool.UnansweredAsk(result) || strings.HasPrefix(result, tool.SentinelQuestionPrompt)
}

// runPluginPostToolHooks runs the PostToolUse hooks that match the successful
// call name and returns the (possibly replaced or extended) result. argsStr is
// the redacted argument string the command hooks also receive. Without a
// matching hook it returns result unchanged and runs no subprocess. Hooks run
// in plugin order, each seeing the result the previous one left.
func (a *Agent) runPluginPostToolHooks(ctx context.Context, name, argsStr, result string) string {
	if isSentinelResult(result) {
		return result
	}
	root := a.pluginHookRoot()
	all := a.pluginHooksFor("PostToolUse")
	if len(all) == 0 {
		return result
	}
	claudeName := a.pluginHookToolName(name)
	var matched []plugins.PluginHook
	for _, h := range all {
		if plugins.MatchesHook(h.Matcher, claudeName) {
			matched = append(matched, h)
		}
	}
	if len(matched) == 0 {
		return result
	}
	if ctx == nil {
		ctx = context.Background()
	}

	// tool_input is the argument object; anything that is not valid JSON goes
	// through as a string rather than being dropped.
	var toolInput any = argsStr
	if json.Valid([]byte(argsStr)) {
		toolInput = json.RawMessage(argsStr)
	}
	sessionID := a.sessionIDValue()

	for _, h := range matched {
		payload := map[string]any{
			"hook_event_name": "PostToolUse",
			"tool_name":       claudeName,
			"tool_input":      toolInput,
			"tool_response":   result,
			"cwd":             root,
		}
		if sessionID != "" {
			payload["session_id"] = sessionID
		}
		body, err := json.Marshal(payload)
		if err != nil {
			a.emitDebug("HOOK", fmt.Sprintf("plugin %s PostToolUse hook for %s not run: %v", h.Plugin, claudeName, err))
			continue
		}
		res := plugins.RunClaudeHook(ctx, h, root, body)
		if res.Err != nil || res.ExitCode != 0 {
			a.emitDebug("HOOK", fmt.Sprintf("plugin %s PostToolUse hook for %s failed (exit %d: %v); result unchanged", h.Plugin, claudeName, res.ExitCode, res.Err))
			continue
		}
		var note string
		result, note = applyPostToolHookReply(result, h.Plugin, res.Stdout)
		if note != "" {
			a.emitDebug("HOOK", note)
		}
	}
	return result
}

// postToolReply is the JSON a PostToolUse hook prints on stdout. Both the
// hookSpecificOutput form and the top-level updatedToolOutput spelling are read.
type postToolReply struct {
	Decision           string          `json:"decision"`
	Reason             string          `json:"reason"`
	UpdatedToolOutput  json.RawMessage `json:"updatedToolOutput"`
	HookSpecificOutput struct {
		AdditionalContext string          `json:"additionalContext"`
		UpdatedToolOutput json.RawMessage `json:"updatedToolOutput"`
	} `json:"hookSpecificOutput"`
}

// applyPostToolHookReply applies one successful hook's stdout to result. The
// order is fixed: updatedToolOutput replaces the result, a block reason is
// appended, then additionalContext is appended, each under a
// "[plugin <name> PostToolUse]" header after a blank line. Empty stdout is no
// change and no note. Non-JSON or unparseable stdout is no change and returns a
// note for the debug log that names the plugin.
func applyPostToolHookReply(result, pluginName, stdout string) (string, string) {
	out := strings.TrimSpace(stdout)
	if out == "" {
		return result, ""
	}
	if !strings.HasPrefix(out, "{") {
		return result, fmt.Sprintf("plugin %s PostToolUse hook printed non-JSON output; result unchanged", pluginName)
	}
	var reply postToolReply
	if err := json.Unmarshal([]byte(out), &reply); err != nil {
		return result, fmt.Sprintf("plugin %s PostToolUse hook reply is not a valid hook object (%v); result unchanged", pluginName, err)
	}

	raw := reply.HookSpecificOutput.UpdatedToolOutput
	if !isJSONValue(raw) {
		raw = reply.UpdatedToolOutput
	}
	if isJSONValue(raw) {
		result = updatedToolOutputText(raw)
	}
	if reply.Decision == "block" {
		if r := strings.TrimSpace(reply.Reason); r != "" {
			result = appendPluginHookText(result, pluginName, r)
		}
	}
	if c := strings.TrimSpace(reply.HookSpecificOutput.AdditionalContext); c != "" {
		result = appendPluginHookText(result, pluginName, c)
	}
	return result, ""
}

// isJSONValue reports whether raw holds a JSON value other than null or nothing.
func isJSONValue(raw json.RawMessage) bool {
	return len(raw) > 0 && string(raw) != "null"
}

// updatedToolOutputText turns a replacement value into the new result: a JSON
// string is used as is, any other value as its compact JSON encoding.
func updatedToolOutputText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return string(raw)
	}
	return buf.String()
}

// appendPluginHookText appends text under a one-line header naming the plugin.
func appendPluginHookText(result, pluginName, text string) string {
	return result + "\n\n[plugin " + pluginName + " PostToolUse]\n" + text
}

// pluginHookState is the agent's plugin hook memo. mu guards every field and is
// never held while a hook subprocess runs.
type pluginHookState struct {
	mu sync.Mutex
	// memo maps "<project root>\x00<event>" to the hooks that event resolved to.
	memo map[string][]plugins.PluginHook
	// promptRun is the UserPromptSubmit run for the human prompt this agent last
	// ran the hooks for (see injectPluginPromptTail).
	promptRun *userPromptHookRun
}

// userPromptHookRun records one UserPromptSubmit run: the prompt it ran for,
// the prompt's identity, and the user-role text it produced ("" when no plugin
// printed anything).
type userPromptHookRun struct {
	content string
	userSeq int
	// ord is the prompt's ordinal among the transcript's human messages.
	ord  int
	text string
}

// pluginHookRoot is the project root plugin hooks run in: the agent's WorkDir,
// else the process cwd.
func (a *Agent) pluginHookRoot() string {
	root := a.WorkDir()
	if root == "" {
		if cwd, err := os.Getwd(); err == nil {
			root = cwd
		}
	}
	return root
}

// pluginEnabledMap is the plugin enable state from this agent's config.
func (a *Agent) pluginEnabledMap() map[string]bool {
	enabled := make(map[string]bool)
	if a.config != nil {
		for pname, p := range a.config.Plugins {
			enabled[pname] = p.Enabled
		}
	}
	return enabled
}

// pluginHooksFor returns the Claude Code command hooks declared for event by
// the plugins this agent loads in its project root. The result is memoised per
// (root, event) for the life of the agent, so the plugin directories are read
// once, and the hook set is fixed for the session like SessionStart output.
// Matchers are returned unevaluated; apply plugins.MatchesHook.
func (a *Agent) pluginHooksFor(event string) []plugins.PluginHook {
	key := a.pluginHookRoot() + "\x00" + event
	a.pluginHooks.mu.Lock()
	hooks, ok := a.pluginHooks.memo[key]
	a.pluginHooks.mu.Unlock()
	if ok {
		return hooks
	}
	// The disk read runs outside the lock. A concurrent first call may read too;
	// the first stored result wins so every caller sees one list.
	hooks = plugins.ClaudeHooksFor(a.pluginEnabledMap(), a.pluginHookRoot(), event)
	a.pluginHooks.mu.Lock()
	defer a.pluginHooks.mu.Unlock()
	if prev, ok := a.pluginHooks.memo[key]; ok {
		return prev
	}
	if a.pluginHooks.memo == nil {
		a.pluginHooks.memo = make(map[string][]plugins.PluginHook)
	}
	a.pluginHooks.memo[key] = hooks
	return hooks
}

// pluginContextOutput is the text one plugin's context hook printed.
type pluginContextOutput struct {
	Plugin string
	Text   string
}

// runPluginContextHooks runs hooks in order for a context event (UserPromptSubmit
// or SubagentStart). Each gets payload as JSON on stdin, with hook_event_name,
// cwd and session_id added, and the non-empty text it printed is returned. A
// failing hook adds nothing and logs one debug line. The caller formats the
// output. No lock is held while a hook runs.
func (a *Agent) runPluginContextHooks(ctx context.Context, event string, hooks []plugins.PluginHook, root string, payload map[string]any) []pluginContextOutput {
	if ctx == nil {
		ctx = context.Background()
	}
	payload["hook_event_name"] = event
	payload["cwd"] = root
	if sid := a.sessionIDValue(); sid != "" {
		payload["session_id"] = sid
	}
	body, err := json.Marshal(payload)
	if err != nil {
		a.emitDebug("HOOK", fmt.Sprintf("%s hooks not run: %v", event, err))
		return nil
	}
	var outs []pluginContextOutput
	for _, h := range hooks {
		res := plugins.RunClaudeHook(ctx, h, root, body)
		if res.Err != nil || res.ExitCode != 0 {
			a.emitDebug("HOOK", fmt.Sprintf("plugin %s %s hook failed (exit %d: %v); nothing added", h.Plugin, event, res.ExitCode, res.Err))
			continue
		}
		text, note := parsePluginContextReply(event, h.Plugin, res.Stdout)
		if note != "" {
			a.emitDebug("HOOK", note)
		}
		if text != "" {
			outs = append(outs, pluginContextOutput{Plugin: h.Plugin, Text: text})
		}
	}
	return outs
}

// parsePluginContextReply reads a context hook's stdout. Plain non-JSON text is
// the context itself, as Claude Code treats UserPromptSubmit and SubagentStart
// stdout. A JSON object contributes hookSpecificOutput.additionalContext. A
// "block" decision is not supported: it yields a note and no text.
func parsePluginContextReply(event, plugin, stdout string) (string, string) {
	out := strings.TrimSpace(stdout)
	if out == "" {
		return "", ""
	}
	if !strings.HasPrefix(out, "{") {
		return out, ""
	}
	var reply struct {
		Decision           string `json:"decision"`
		HookSpecificOutput struct {
			AdditionalContext string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(out), &reply); err != nil {
		return "", fmt.Sprintf("plugin %s %s hook reply is not a valid hook object (%v); nothing added", plugin, event, err)
	}
	if reply.Decision == "block" {
		return "", fmt.Sprintf("plugin %s asked to block the %s event; ocode does not support blocking, nothing added", plugin, event)
	}
	return strings.TrimSpace(reply.HookSpecificOutput.AdditionalContext), ""
}

// pluginPromptMarker opens the user-role block that carries UserPromptSubmit
// hook output. The block is request-time only and is never persisted.
const pluginPromptMarker = "[ocode:plugin-hook]"

// stepLimitNoticePrefix opens the user-role notice Step appends when it hits the
// step limit (agent.go). It is not a human message.
const stepLimitNoticePrefix = "You have reached the maximum number of steps ("

// isHumanPromptMessage reports whether m is a message a person typed. Synthetic
// user-role messages all start with an [ocode:...] marker (goal resume, notices,
// tail injections), auto-continue resumes are recognised by their fixed text,
// and the step-limit notice is written by the loop. Tool results and assistant
// output are not user-role.
func isHumanPromptMessage(m Message) bool {
	if m.Role != "user" {
		return false
	}
	content := strings.TrimSpace(m.Content)
	if strings.HasPrefix(content, "[ocode:") || strings.HasPrefix(content, stepLimitNoticePrefix) {
		return false
	}
	return !isAutoContinueResumeText(m.Content)
}

// humanPromptOrdinal counts the human messages in base up to and including
// index upto.
func humanPromptOrdinal(base []Message, upto int) int {
	n := 0
	for i := 0; i <= upto && i < len(base); i++ {
		if isHumanPromptMessage(base[i]) {
			n++
		}
	}
	return n
}

// hasCompactionSummary reports whether base carries a compaction summary, which
// means older messages were removed and the transcript was rewritten.
func hasCompactionSummary(base []Message) bool {
	for _, m := range base {
		if strings.HasPrefix(m.Content, compactionSummaryMarker) {
			return true
		}
	}
	return false
}

// reusablePromptHookText returns the text the UserPromptSubmit hooks last
// produced, when that run was for this same human prompt. A message with a
// UserSeq (server sessions) is identified by it alone. Otherwise the prompt must
// have the same content at the same ordinal among the human messages. A shorter
// transcript with the same content counts as the same prompt only when a
// compaction summary is present, because compaction removes older messages and
// shifts the ordinal. A new message with the same text, with no compaction,
// still gets a new ordinal and runs again.
func (a *Agent) reusablePromptHookText(human Message, ord int, base []Message) (string, bool) {
	a.pluginHooks.mu.Lock()
	run := a.pluginHooks.promptRun
	a.pluginHooks.mu.Unlock()
	if run == nil {
		return "", false
	}
	if human.UserSeq > 0 {
		if run.userSeq == human.UserSeq {
			return run.text, true
		}
		return "", false
	}
	if run.userSeq != 0 || run.content != human.Content {
		return "", false
	}
	if run.ord == ord || (ord < run.ord && hasCompactionSummary(base)) {
		return run.text, true
	}
	return "", false
}

// formatPromptHookBlock renders UserPromptSubmit output: the marker line, then
// one "--- Plugin: <name> (UserPromptSubmit) ---" block per plugin with output.
func formatPromptHookBlock(outs []pluginContextOutput) string {
	if len(outs) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(pluginPromptMarker + "\n")
	for _, o := range outs {
		fmt.Fprintf(&b, "\n--- Plugin: %s (UserPromptSubmit) ---\n%s\n", o.Plugin, o.Text)
	}
	return strings.TrimRight(b.String(), "\n")
}

// injectPluginPromptTail returns base plus, when the UserPromptSubmit hooks
// printed anything for the current human prompt, one user-role message with that
// output at the tail. It is user-role like the other tail injectors (see
// injectTodoTail): a system-role block would be hoisted into the cached system
// prompt. base is not mutated, and nothing is written to the transcript.
//
// The hooks run once per human prompt, not once per request. The prompt is the
// last user-role message, ignoring the selection block. It counts only when it is
// a genuine human message; a synthetic last message (goal resume, auto-continue,
// an [ocode:...] notice) runs nothing and injects nothing. Sub-agents and helper
// agents (advisor, ask side-queries) never run the hooks.
func (a *Agent) injectPluginPromptTail(ctx context.Context, base []Message) []Message {
	if a.isSubAgent || a.helperAgent {
		return base
	}
	hooks := a.pluginHooksFor("UserPromptSubmit")
	if len(hooks) == 0 {
		return base
	}
	anchor := -1
	for i := len(base) - 1; i >= 0; i-- {
		if base[i].Role != "user" || strings.HasPrefix(base[i].Content, promptSelectionMarker) {
			continue
		}
		anchor = i
		break
	}
	if anchor < 0 || !isHumanPromptMessage(base[anchor]) {
		return base
	}
	human := base[anchor]
	ord := humanPromptOrdinal(base, anchor)
	text, ok := a.reusablePromptHookText(human, ord, base)
	if !ok {
		outs := a.runPluginContextHooks(ctx, "UserPromptSubmit", hooks, a.pluginHookRoot(), map[string]any{"prompt": human.Content})
		text = formatPromptHookBlock(outs)
		a.pluginHooks.mu.Lock()
		a.pluginHooks.promptRun = &userPromptHookRun{content: human.Content, userSeq: human.UserSeq, ord: ord, text: text}
		a.pluginHooks.mu.Unlock()
	}
	if text == "" {
		return base
	}
	out := make([]Message, 0, len(base)+1)
	out = append(out, base...)
	return append(out, Message{Role: "user", Content: text})
}

// subagentStartMessage runs this agent's SubagentStart hooks whose matcher
// accepts agentType (the child's agent type name) and returns their output as one
// system-role message for the child's initial messages. The caller sets it once at
// child creation. It never changes, so system role is safe. A resumed child reuses
// its transcript and does not reach this path.
func (a *Agent) subagentStartMessage(ctx context.Context, agentType string) (Message, bool) {
	var hooks []plugins.PluginHook
	for _, h := range a.pluginHooksFor("SubagentStart") {
		if plugins.MatchesHook(h.Matcher, agentType) {
			hooks = append(hooks, h)
		}
	}
	if len(hooks) == 0 {
		return Message{}, false
	}
	outs := a.runPluginContextHooks(ctx, "SubagentStart", hooks, a.pluginHookRoot(), map[string]any{"agent_type": agentType})
	if len(outs) == 0 {
		return Message{}, false
	}
	var b strings.Builder
	b.WriteString("Plugin context (SubagentStart):")
	for _, o := range outs {
		fmt.Fprintf(&b, "\n--- Plugin: %s ---\n%s\n", o.Plugin, o.Text)
	}
	return Message{Role: "system", Content: strings.TrimRight(b.String(), "\n")}, true
}
