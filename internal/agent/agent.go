package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/jamesmercstudio/ocode/internal/config"
	"github.com/jamesmercstudio/ocode/internal/hooks"
	"github.com/jamesmercstudio/ocode/internal/mcp"
	"github.com/jamesmercstudio/ocode/internal/tool"
)

var DebugAppend func(kind, msg string)

func emitDebug(kind, msg string) {
	if DebugAppend != nil {
		DebugAppend(kind, msg)
	}
}

// JobEvent describes a background job (process or agent run) that finished.
type JobEvent struct {
	Kind       string // "process" or "agent"
	ID         string
	Name       string // process command, or agent name
	Status     string // exited/killed/done/failed
	Result     string // output tail or result text
	Background bool   // for Kind=="agent": true if run_in_background; false means the parent already consumed the result via the task tool's return value
	ToolCallID string // for Kind=="agent": id of the task tool_call that spawned this run, when known
}

type Agent struct {
	client      LLMClient
	tools       map[string]tool.Tool
	mcpTools    map[string]struct{}
	mcpErrors   []string
	config      *config.Config
	mode        Mode
	spec        *AgentSpec
	permissions *PermissionManager
	activity    *ActivityTracker
	procs       *tool.ProcessRegistry
	runs        *AgentRunRegistry
	stopCh      chan struct{}
	stopMu      sync.Mutex
	jobEvents   chan JobEvent
	// OnMessage, if set, is invoked for each message produced during Step
	// (assistant replies and tool results) as soon as they are generated,
	// enabling live UI updates between iterations of the tool-call loop.
	OnMessage func(Message)
	// OnCompactStart, if set, is invoked when async compaction begins (so the
	// UI can show a spinner). It runs from the goroutine that called
	// MaybeCompactAsync — keep handlers fast and thread-safe.
	OnCompactStart func()
	// OnCompact, if set, is invoked when async compaction finishes. The
	// callback receives a CompactResult describing whether compaction
	// occurred, the splice indices, and the summary message to insert.
	OnCompact func(CompactResult)
	// OnPermissionAsk, if set, is invoked synchronously when a tool call
	// requires a permission decision. It blocks until the user (via the TUI)
	// responds, returning the permission response. When set, HandleToolCall acts
	// on the returned level directly instead of emitting the PERMISSION_ASK:
	// sentinel string. This is set ONLY on sub-agents — the main agent leaves
	// it nil and keeps the pause-and-resume sentinel flow handled by the TUI.
	// Sub-agents run in their own goroutines and never reach the TUI's
	// tool-result handling, so the sentinel path is invisible to them.
	OnPermissionAsk func(PermissionRequest) PermissionResponse
	// subAgentPermAsker is the permission-ask callback the TUI installs on the
	// main agent. It is not used by the main agent itself; it is copied onto
	// each sub-agent's OnPermissionAsk so sub-agent asks reach the TUI.
	subAgentPermAsker func(PermissionRequest) PermissionResponse
	// maxSteps limits the number of agentic iterations. 0 = unlimited.
	maxSteps int
	// compactMu serialises async compaction passes so a slow summary call
	// can't fire OnCompact twice for overlapping snapshots.
	compactMu sync.Mutex
	// subagentDispatchGuard tracks consecutive identical task-tool dispatches
	// since the last user input, to break runaway loops where a small model
	// keeps re-launching the same subagent in response to its own completion
	// notifications.
	subagentDispatchMu    sync.Mutex
	subagentDispatchLast  string
	subagentDispatchCount int
}

const subagentDispatchLimit = 3

// NoteSubagentDispatch increments the consecutive-dispatch counter for the
// given subagent name and returns the new count. Callers use this to refuse
// runaway loops (see TaskTool.Execute).
func (a *Agent) NoteSubagentDispatch(name string) int {
	a.subagentDispatchMu.Lock()
	defer a.subagentDispatchMu.Unlock()
	if a.subagentDispatchLast == name {
		a.subagentDispatchCount++
	} else {
		a.subagentDispatchLast = name
		a.subagentDispatchCount = 1
	}
	return a.subagentDispatchCount
}

// ResetSubagentDispatch clears the consecutive-dispatch counter. The TUI
// calls this whenever the user sends a new message, so legitimate repeated
// dispatches across turns are allowed.
func (a *Agent) ResetSubagentDispatch() {
	a.subagentDispatchMu.Lock()
	a.subagentDispatchLast = ""
	a.subagentDispatchCount = 0
	a.subagentDispatchMu.Unlock()
}

func NewAgent(client LLMClient, tools []tool.Tool, cfg *config.Config) *Agent {
	toolMap := make(map[string]tool.Tool)
	for _, t := range tools {
		toolMap[t.Name()] = t
	}
	a := &Agent{
		client:      client,
		tools:       toolMap,
		mcpTools:    make(map[string]struct{}),
		config:      cfg,
		mode:        ModeBuild,
		permissions: NewPermissionManager(),
		activity:    newActivityTracker(),
	}
	a.procs = tool.NewProcessRegistry()
	a.runs = NewAgentRunRegistry()
	a.stopCh = make(chan struct{})
	a.jobEvents = make(chan JobEvent, 32)
	a.procs.SetOnDone(func(p *tool.Process) {
		text, status, code, _ := a.procs.Output(p.ID)
		a.emitJob(JobEvent{
			Kind:   "process",
			ID:     p.ID,
			Name:   p.Command,
			Status: string(status),
			Result: fmt.Sprintf("exit %d\n%s", code, text),
		})
	})
	a.runs.SetOnDone(func(r *AgentRun) {
		result := r.Result
		status := "done"
		if r.statusValue() == RunFailed {
			result = r.Err
			status = "failed"
		}
		a.emitJob(JobEvent{
			Kind:       "agent",
			ID:         r.ID,
			Name:       r.Name,
			Status:     status,
			Result:     result,
			Background: r.Background,
			ToolCallID: r.ToolCallID,
		})
	})
	a.tools["bash"] = &tool.BashTool{Procs: a.procs}
	a.tools["bash_output"] = tool.BashOutputTool{Procs: a.procs}
	a.tools["kill_shell"] = tool.KillShellTool{Procs: a.procs}
	// "agent" tool retired in favor of "task". AgentTool the type is kept
	// only so existing transcripts/back-compat permission entries still
	// resolve. It is no longer registered on new agents.
	a.tools["task"] = TaskTool{mainAgent: a, registry: DefaultAgentRegistry, runs: a.runs}
	a.tools["agent_status"] = AgentStatusTool{runs: a.runs}
	a.tools["task_status"] = TaskStatusTool{runs: a.runs}
	a.tools["wait"] = WaitTool{procs: a.procs, runs: a.runs, stopCh: a.stopCh}
	if cfg != nil {
		a.permissions.LoadFromConfig(cfg.Permission)
		a.permissions.LoadFromOcode(cfg.Ocode.Permissions)
	}
	// Set workDir for path-scoped permission checks
	if wd, err := os.Getwd(); err == nil {
		a.permissions.SetWorkDir(wd)
	}
	return a
}

func (a *Agent) SetChildSessionPersistence(persist func(sessionID, title string, messages []Message, metadata map[string]any) error) {
	if taskTool, ok := a.tools["task"].(TaskTool); ok {
		taskTool.persistChildSess = persist
		a.tools["task"] = taskTool
	}
}

// AgentTool is a legacy compatibility shim. The "agent" tool is no longer
// registered (the runtime exposes "task" instead), but the type is kept so
// older session transcripts and user-supplied permission entries naming
// "agent" still resolve. Removal target: drop once stored transcripts older
// than the deprecation date no longer need to round-trip — track in TODO.md.
type AgentTool struct {
	mainAgent *Agent
}

func (t AgentTool) Name() string        { return "agent" }
func (t AgentTool) Description() string { return "Compatibility alias for the task sub-agent tool" }
func (t AgentTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "agent",
		"description": "Compatibility alias for task. Spawn a registry-backed sub-agent with a specific scope to handle a task autonomously.",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"prompt": map[string]interface{}{
					"type":        "string",
					"description": "The specific task or instructions for the sub-agent.",
				},
				"agent": map[string]interface{}{
					"type":        "string",
					"description": "Optional sub-agent name. Defaults to general.",
				},
				"subagent_type": map[string]interface{}{
					"type":        "string",
					"description": "OpenCode-compatible alias for agent.",
				},
				"context": map[string]interface{}{
					"type":        "string",
					"description": "Additional background context relevant to the task.",
				},
				"description": map[string]interface{}{
					"type":        "string",
					"description": "Short description of the task.",
				},
				"run_in_background": map[string]interface{}{
					"type":        "boolean",
					"description": "If true, run the subagent in the background and return immediately with the run ID.",
				},
				"background": map[string]interface{}{
					"type":        "boolean",
					"description": "OpenCode-compatible alias for run_in_background.",
				},
			},
			"required": []string{"prompt"},
		},
	}
}

func (t AgentTool) Parallel() bool { return true }

func (t AgentTool) Execute(args json.RawMessage) (string, error) {
	if t.mainAgent == nil {
		return "", fmt.Errorf("no main agent configured")
	}
	if task, ok := t.mainAgent.tools["task"].(TaskTool); ok {
		return task.Execute(args)
	}
	task := TaskTool{mainAgent: t.mainAgent, registry: DefaultAgentRegistry, runs: t.mainAgent.runs}
	return task.Execute(args)
}

func (a *Agent) Step(messages []Message) ([]Message, error) {
	if a.client == nil {
		return []Message{{Role: "assistant", Content: "(no llm client configured)"}}, nil
	}

	messages = a.PrepareMessages(messages, "")
	toolDefs := a.GetToolDefinitions()
	var newMsgs []Message

	// Recover orphaned tool calls from prior sessions: find the last assistant
	// message that has ToolCalls, then check which call IDs have no following
	// tool-result message. Re-execute those calls now (the prior execution is
	// guaranteed gone — we're in a new Step invocation).
	messages = a.recoverOrphanedToolCalls(messages)

	for i := 0; ; i++ {
		if a.cancelled() {
			return newMsgs, nil
		}
		limit := a.maxSteps
		if limit <= 0 {
			limit = 100
		}
		if i >= limit {
			summarizeMsg := Message{
				Role:    "user",
				Content: "You have reached the maximum number of steps (" + strconv.Itoa(limit) + "). Stop using tools and respond with a summary of your work and any remaining tasks.",
			}
			newMsgs = append(newMsgs, summarizeMsg)
			messages = append(messages, summarizeMsg)
			resp, err := a.client.Chat(messages, toolDefs)
			if err != nil {
				return nil, err
			}
			if a.cancelled() {
				return newMsgs, nil
			}
			newMsgs = append(newMsgs, *resp)
			if a.OnMessage != nil {
				a.OnMessage(*resp)
			}
			return newMsgs, nil
		}
		emitDebug("LLM", fmt.Sprintf("→ %s/%s [%d msgs]", a.client.GetProvider(), a.client.GetModel(), len(messages)))
		a.activity.setLLMRunning(true)
		resp, err := a.client.Chat(messages, toolDefs)
		a.activity.setLLMRunning(false)
		if err != nil {
			emitDebug("ERROR", fmt.Sprintf("LLM error: %v", err))
			return nil, err
		}
		if a.cancelled() {
			return newMsgs, nil
		}
		if resp.Usage != nil {
			in, out := int64(0), int64(0)
			if resp.Usage.PromptTokens != nil {
				in = *resp.Usage.PromptTokens
			}
			if resp.Usage.CompletionTokens != nil {
				out = *resp.Usage.CompletionTokens
			}
			emitDebug("LLM", fmt.Sprintf("← tokens in=%d out=%d", in, out))
			a.warnIfNearWindow(in)
		}

		newMsgs = append(newMsgs, *resp)
		messages = append(messages, *resp)
		if a.OnMessage != nil {
			a.OnMessage(*resp)
		}

		if len(resp.ToolCalls) == 0 {
			break
		}

		type tcResult struct {
			idx int
			msg Message
		}

		var parallelTCs, sequentialTCs []int
		for j, tc := range resp.ToolCalls {
			t, ok := a.tools[tc.Function.Name]
			if ok && t.Parallel() {
				parallelTCs = append(parallelTCs, j)
			} else {
				sequentialTCs = append(sequentialTCs, j)
			}
		}

		results := make([]Message, len(resp.ToolCalls))

		if len(parallelTCs) > 0 {
			var wg sync.WaitGroup
			for _, i := range parallelTCs {
				wg.Add(1)
				go func(idx int, tc ToolCall) {
					defer wg.Done()
					if a.cancelled() {
						return
					}
					a.activity.toolStarted(tc.Function.Name)
					result, err := a.HandleToolCall(tc.Function.Name, json.RawMessage(tc.Function.Arguments))
					a.activity.toolDone(tc.Function.Name)
					if err != nil {
						result = fmt.Sprintf("Error: %v", err)
					}
					result = TruncateToolResult(tc.ID, result)
					results[idx] = Message{Role: "tool", ToolID: tc.ID, Content: result}
				}(i, resp.ToolCalls[i])
			}
			wg.Wait()
		}

		for _, i := range sequentialTCs {
			if a.cancelled() {
				return newMsgs, nil
			}
			tc := resp.ToolCalls[i]
			a.activity.toolStarted(tc.Function.Name)
			result, err := a.HandleToolCall(tc.Function.Name, json.RawMessage(tc.Function.Arguments))
			a.activity.toolDone(tc.Function.Name)
			if err != nil {
				result = fmt.Sprintf("Error: %v", err)
			}
			result = TruncateToolResult(tc.ID, result)
			results[i] = Message{Role: "tool", ToolID: tc.ID, Content: result}
		}
		if a.cancelled() {
			return newMsgs, nil
		}

		pauseAfterResults := false
		for _, toolMsg := range results {
			newMsgs = append(newMsgs, toolMsg)
			messages = append(messages, toolMsg)
			if a.OnMessage != nil {
				a.OnMessage(toolMsg)
			}
			if strings.Contains(toolMsg.Content, tool.SentinelWaitingForUser) || strings.HasPrefix(toolMsg.Content, tool.SentinelPermissionAsk) {
				pauseAfterResults = true
			}
		}
		if pauseAfterResults {
			return newMsgs, nil
		}
	}

	return newMsgs, nil
}

// warnIfNearWindow emits a debug warning when the most recent prompt token
// count is close to the active model's window. Mid-loop compaction is unsafe
// (would split open tool-call pairs), so the actual compaction is deferred to
// the post-Step trigger fired by the TUI on streamDoneMsg. This warning lets
// users see why they may be approaching a hard context-length error.
func (a *Agent) warnIfNearWindow(promptTokens int64) {
	rt := a.resolveCompactRuntime()
	if !rt.Enabled || rt.WindowTokens <= 0 {
		return
	}
	limit := int64(float64(rt.WindowTokens) * rt.TokenThreshold)
	if promptTokens >= limit {
		emitDebug("COMPACT", fmt.Sprintf("warning: prompt tokens=%d ≥ threshold=%d (window=%d); compaction will run after this Step", promptTokens, limit, rt.WindowTokens))
	}
}

// resolveCompactRuntime materialises the compaction knobs for the current
// agent + active model. Returns Enabled=false when compaction is disabled.
func (a *Agent) resolveCompactRuntime() compactRuntime {
	rt := compactRuntime{Enabled: false}
	if a.config == nil {
		return rt
	}
	c := a.config.Ocode.Compact
	if !c.Enabled {
		return rt
	}
	rt.Enabled = true
	rt.TokenThreshold = c.TokenThreshold
	if rt.TokenThreshold <= 0 || rt.TokenThreshold > 1 {
		rt.TokenThreshold = 0.85
	}
	rt.KeepRecentTurns = c.KeepRecentTurns
	if rt.KeepRecentTurns <= 0 {
		rt.KeepRecentTurns = 3
	}
	rt.MinMessages = c.MinMessages
	if rt.MinMessages <= 0 {
		rt.MinMessages = 8
	}
	rt.SummaryTimeoutSeconds = c.SummaryTimeoutSeconds
	if rt.SummaryTimeoutSeconds <= 0 {
		rt.SummaryTimeoutSeconds = 30
	}
	rt.SummaryMaxRetries = c.SummaryMaxRetries
	if rt.SummaryMaxRetries < 0 {
		rt.SummaryMaxRetries = 0
	}
	rt.MaxSummaryInputTokens = c.MaxSummaryInputTokens
	if rt.MaxSummaryInputTokens <= 0 {
		rt.MaxSummaryInputTokens = 50000
	}
	if a.client != nil {
		model := a.client.GetModel()
		if a.client.GetProvider() != "" {
			model = a.client.GetProvider() + "/" + model
		}
		rt.WindowTokens = int(ModelWindow(model))
	}
	return rt
}

// MaybeCompactAsync runs compaction in a goroutine. It first checks the token
// threshold; if exceeded, it picks a tool-pair-safe cut, runs the summary
// client with a timeout + retry loop, and fires OnCompact with the result.
//
// Returns true iff a compaction goroutine was actually started — false when
// disabled, below threshold, or another compaction is already in flight. The
// boolean lets callers (TUI) avoid stomping per-trigger state with snapshots
// from a deferred call that never produced a result.
//
// The provided messages slice is read-only; the caller is responsible for
// splicing its own copy when OnCompact fires.
func (a *Agent) MaybeCompactAsync(messages []Message) bool {
	rt := a.resolveCompactRuntime()
	if !rt.Enabled {
		return false
	}
	need, used := shouldCompact(messages, rt)
	if !need {
		return false
	}
	if !a.compactMu.TryLock() {
		emitDebug("COMPACT", "skipped: another compaction in flight")
		return false
	}
	snapshot := make([]Message, len(messages))
	copy(snapshot, messages)
	emitDebug("COMPACT", fmt.Sprintf("triggered: ~%d tokens used, window=%d, threshold=%.2f", used, rt.WindowTokens, rt.TokenThreshold))
	if a.OnCompactStart != nil {
		a.OnCompactStart()
	}
	go func() {
		defer a.compactMu.Unlock()
		result := a.runCompact(snapshot, rt)
		if a.OnCompact != nil {
			a.OnCompact(result)
		}
	}()
	return true
}

func (a *Agent) runCompact(messages []Message, rt compactRuntime) CompactResult {
	res := CompactResult{OriginalLen: len(messages)}

	prefixEnd := findPrefixEnd(messages)
	tailStart := findTurnBoundary(messages, rt.KeepRecentTurns)
	if tailStart < prefixEnd {
		tailStart = prefixEnd
	}
	tailStart = safeCut(messages, tailStart)
	if tailStart <= prefixEnd {
		// Nothing meaningful to summarize between prefix and tail.
		emitDebug("COMPACT", "skipped: no compactible middle after safe-cut")
		return res
	}

	middle := messages[prefixEnd:tailStart]
	if len(middle) == 0 {
		return res
	}

	prompt, dropped := buildSummaryPrompt(middle, rt.MaxSummaryInputTokens)
	if dropped > 0 {
		emitDebug("COMPACT", fmt.Sprintf("dropped %d middle msgs from summary input (size cap)", dropped))
	}

	ctx, cancel := contextWithTimeout(rt.SummaryTimeoutSeconds)
	defer cancel()

	client := a.compactSummaryClient()
	summaryText, err := runSummary(ctx, client, prompt, rt.SummaryMaxRetries)
	if err != nil {
		emitDebug("COMPACT", fmt.Sprintf("summary failed: %v", err))
		res.Err = err
		return res
	}

	summaryMsg := Message{
		Role: "system",
		Content: fmt.Sprintf(
			"[Compacted summary of %d earlier messages]\n\n%s",
			len(middle), strings.TrimSpace(summaryText),
		),
	}
	res.OK = true
	res.ReplaceFrom = prefixEnd
	res.ReplaceTo = tailStart
	res.Summary = summaryMsg
	emitDebug("COMPACT", fmt.Sprintf("done: replaced [%d:%d] (%d msgs) with summary", prefixEnd, tailStart, len(middle)))
	return res
}

func (a *Agent) compactSummaryClient() LLMClient {
	if a.config == nil {
		return a.client
	}

	compact := a.config.Ocode.Compact
	if compact.SummaryProvider == "" && compact.SummaryModel == "" {
		return a.client
	}

	provider := compact.SummaryProvider
	if provider == "" {
		provider = a.client.GetProvider()
	}

	model := compact.SummaryModel
	if model == "" {
		model = a.client.GetModel()
	}
	if model == "" {
		return a.client
	}

	targetModel := model
	if provider != "" {
		targetModel = provider + ":" + model
	}

	if client := NewClient(a.config, targetModel); client != nil {
		return client
	}
	return a.client
}

func (a *Agent) Mode() Mode {
	if a.mode == "" {
		return ModeBuild
	}
	return a.mode
}

func (a *Agent) SetMode(m Mode) {
	if !m.Valid() {
		return
	}
	a.mode = m
}

func (a *Agent) HandleToolCall(name string, args json.RawMessage) (string, error) {
	if deny, ok := gateToolCall(a.Mode(), name, args); !ok {
		return deny, nil
	}

	if a.permissions != nil {
		decision := a.permissions.Decide(name, args)
		if decision.Level == PermissionDeny {
			return fmt.Sprintf("denied: tool %q is not permitted by permission rules", name), nil
		}
		if decision.Level == PermissionAsk {
			// When a permission callback is wired (sub-agents), ask the user
			// synchronously and act on the answer. The PERMISSION_ASK: sentinel
			// path is used only when no callback is set (the main agent's flow,
			// where the TUI handles the sentinel in appendAgentMessage).
			if a.OnPermissionAsk != nil {
				req := PermissionRequest{ToolName: name, Args: args, Scope: PermissionScopeTool, Rule: "tool." + name}
				if decision.Request != nil {
					req = *decision.Request
				}
				resp := a.OnPermissionAsk(req)
				if resp.Level == PermissionAllow {
					a.applyPermissionResponse(req, resp)
					return a.executeToolCall(name, args)
				}
				return fmt.Sprintf("denied: tool %q denied by user", name), nil
			}
			payload, err := json.Marshal(decision.Request)
			if err != nil {
				return "", fmt.Errorf("marshal permission request: %w", err)
			}
			return tool.SentinelPermissionAsk + string(payload), nil
		}
	}

	return a.executeToolCall(name, args)
}

func (a *Agent) HandleApprovedToolCall(name string, args json.RawMessage) (string, error) {
	return a.executeToolCall(name, args)
}

func (a *Agent) executeToolCall(name string, args json.RawMessage) (string, error) {
	emitDebug("TOOL", fmt.Sprintf("→ %s %s", name, truncateDebugArgs(args, 120)))
	if !a.isToolAllowed(name) {
		return fmt.Sprintf("denied: tool %q is not allowed for this agent", name), nil
	}

	t, ok := a.tools[name]
	if !ok {
		return "", fmt.Errorf("tool %s not found", name)
	}

	if name == "glob" || name == "grep" || name == "list" {
		var params map[string]interface{}
		if err := json.Unmarshal(args, &params); err == nil && a.config != nil && len(a.config.Watcher.Ignore) > 0 {
			params["ignore"] = a.config.Watcher.Ignore
			if merged, err := json.Marshal(params); err == nil {
				args = merged
			}
		}
	}

	var hooksCfg map[string]config.HookConfig
	if a.config != nil {
		hooksCfg = a.config.Hooks
	}

	argsStr := string(args)
	if err := hooks.RunPreHook(name, argsStr, hooksCfg); err != nil {
		return fmt.Sprintf("pre-hook blocked: %v", err), nil
	}

	result, err := t.Execute(args)

	if hooksCfg != nil {
		resultStr := ""
		if err == nil {
			resultStr = result
		}
		_ = hooks.RunPostHook(name, argsStr, resultStr, hooksCfg)
	}

	if err != nil {
		emitDebug("ERROR", fmt.Sprintf("tool %s: %v", name, err))
	} else {
		emitDebug("TOOL", fmt.Sprintf("← %s (ok)", name))
	}
	return result, err
}

func (a *Agent) GetToolDefinitions() []map[string]interface{} {
	defs := make([]map[string]interface{}, 0, len(a.tools))
	for _, t := range a.tools {
		if !a.isToolAllowed(t.Name()) {
			continue
		}
		defs = append(defs, t.Definition())
	}
	return defs
}

func (a *Agent) GetProvider() string {
	return a.client.GetProvider()
}

func (a *Agent) Client() LLMClient {
	return a.client
}

func (a *Agent) GetTools() []tool.Tool {
	tools := make([]tool.Tool, 0, len(a.tools))
	for _, t := range a.tools {
		if !a.isToolAllowed(t.Name()) {
			continue
		}
		tools = append(tools, t)
	}
	return tools
}

func (a *Agent) isToolAllowed(name string) bool {
	if a.spec != nil && len(a.spec.Tools) > 0 {
		found := false
		for _, allowed := range a.spec.Tools {
			if allowed == name {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if a.spec != nil {
		for _, denied := range a.spec.DeniedTools {
			if denied == name {
				return false
			}
		}
	}
	if a.permissions != nil {
		level := a.permissions.Check(name)
		if level == PermissionDeny {
			return false
		}
	}
	return true
}

func (a *Agent) SetSpec(spec *AgentSpec) {
	a.spec = spec
	if spec != nil && spec.Mode.Valid() {
		a.mode = spec.Mode
	}
	if spec != nil && spec.MaxSteps > 0 {
		a.maxSteps = spec.MaxSteps
	}
	a.applySpecModel(spec)
}

// applySpecModel swaps the active LLM client when the spec declares a Model
// override. Empty Model leaves the current client untouched (inherit). A
// failed NewClient call is logged and the previous client is kept so a typo
// in an agent file can't strand the session without an LLM.
//
// It also propagates spec.Temperature / spec.TopP onto the resulting client
// (or the existing client when no Model swap happens) for the providers that
// support those sampling params.
func (a *Agent) applySpecModel(spec *AgentSpec) {
	if spec == nil {
		return
	}
	if strings.TrimSpace(spec.Model) != "" {
		if a.config == nil {
			emitDebug("AGENT", fmt.Sprintf("spec %q requested model %q but agent has no config; keeping current client", spec.Name, spec.Model))
		} else if client := NewClient(a.config, spec.Model); client != nil {
			emitDebug("AGENT", fmt.Sprintf("spec %q: switching client to %s", spec.Name, spec.Model))
			a.client = client
		} else {
			emitDebug("AGENT", fmt.Sprintf("spec %q model %q: NewClient returned nil; keeping current client", spec.Name, spec.Model))
		}
	}
	if gc, ok := a.client.(*GenericClient); ok {
		gc.Temperature = spec.Temperature
		gc.TopP = spec.TopP
		if spec.Temperature != nil || spec.TopP != nil {
			emitDebug("AGENT", fmt.Sprintf("spec %q: sampling params temperature=%v top_p=%v", spec.Name, spec.Temperature, spec.TopP))
		}
	}
}

func (a *Agent) Spec() *AgentSpec {
	return a.spec
}

func (a *Agent) Activity() *ActivityTracker {
	return a.activity
}

// Procs returns this agent's background-process registry.
func (a *Agent) Procs() *tool.ProcessRegistry { return a.procs }

// Supervisor returns the process supervisor attached to this agent (may be nil).
func (a *Agent) Supervisor() *tool.ProcessSupervisor {
	if a.procs == nil {
		return nil
	}
	return a.procs.Supervisor()
}

// SetSupervisor attaches a shared session-scoped process supervisor to this
// agent and its process registry. Subagents should inherit the same supervisor.
func (a *Agent) SetSupervisor(sup *tool.ProcessSupervisor) {
	if a.procs != nil {
		a.procs.SetSupervisor(sup)
	}
}

// Runs returns the registry of async subagent runs.
func (a *Agent) Runs() *AgentRunRegistry { return a.runs }

// JobEvents is the channel the TUI reads background-job completions from.
func (a *Agent) JobEvents() chan JobEvent { return a.jobEvents }

// emitJob delivers a completion event, dropping it only if the buffer is full.
func (a *Agent) emitJob(ev JobEvent) {
	select {
	case a.jobEvents <- ev:
	default:
		emitDebug("JOB", "job event buffer full, dropped "+ev.ID)
	}
}

// Shutdown cancels the agent and async subagent runs. Shared process teardown
// is owned by the session supervisor, not the agent registries.
func (a *Agent) Shutdown() {
	a.Cancel()
	if a.runs != nil {
		a.runs.CancelAll()
	}
}

// Cancel signals the agent's Step loop to stop before the next LLM call.
// Best-effort: an in-flight HTTP call is not interrupted.
func (a *Agent) Cancel() {
	a.stopMu.Lock()
	defer a.stopMu.Unlock()
	select {
	case <-a.stopCh:
		// already closed
	default:
		close(a.stopCh)
	}
}

// Done returns a channel closed when the agent is cancelled. Callers blocking
// on agent-scoped work (e.g. a permission-ask callback) can select on it to
// unblock cleanly on Shutdown/Cancel instead of leaking a goroutine.
func (a *Agent) Done() <-chan struct{} { return a.stopCh }

// cancelled reports whether Cancel has been called.
func (a *Agent) cancelled() bool {
	select {
	case <-a.stopCh:
		return true
	default:
		return false
	}
}

func (a *Agent) Permissions() *PermissionManager {
	return a.permissions
}

// SetSubAgentPermAsker installs the callback that sub-agents spawned by this
// agent will use to surface permission asks to the TUI. The main agent itself
// does not use this callback — its permission asks flow through the
// PERMISSION_ASK: sentinel handled by the TUI's message pipeline.
func (a *Agent) SetSubAgentPermAsker(f func(PermissionRequest) PermissionResponse) {
	a.subAgentPermAsker = f
}

func (a *Agent) applyPermissionResponse(req PermissionRequest, resp PermissionResponse) {
	if a.permissions == nil || resp.Level != PermissionAllow {
		return
	}
	if resp.PersistTool {
		a.permissions.SetRule(req.ToolName, PermissionAllow)
		return
	}
	if !resp.PersistRule {
		return
	}
	if req.ToolName == "webfetch" && strings.HasPrefix(req.Rule, "webfetch.domain.") {
		a.permissions.SetWebfetchDomain(strings.TrimPrefix(req.Rule, "webfetch.domain."), PermissionAllow)
		return
	}
	if req.Scope == PermissionScopeBashPrefix && req.Prefix != "" {
		a.permissions.SetBashPrefixRule(req.Prefix, PermissionAllow)
		return
	}
	a.permissions.SetRule(req.ToolName, PermissionAllow)
}

func (a *Agent) AddTools(tools []tool.Tool) {
	for _, t := range tools {
		a.tools[t.Name()] = t
	}
}

func (a *Agent) addMCPTools(tools []tool.Tool) {
	for _, t := range tools {
		a.tools[t.Name()] = t
		a.mcpTools[t.Name()] = struct{}{}
	}
}

func (a *Agent) MCPToolNames() []string {
	names := make([]string, 0, len(a.mcpTools))
	for name := range a.mcpTools {
		names = append(names, name)
	}
	return names
}

func (a *Agent) RestoreMCPToolNames(names []string) {
	for _, name := range names {
		a.mcpTools[name] = struct{}{}
	}
}

func (a *Agent) MCPToolCount() int {
	return len(a.mcpTools)
}

func (a *Agent) MCPErrors() []string {
	return a.mcpErrors
}

func (a *Agent) LoadExternalTools(cfg *config.Config) {
	custom := tool.LoadCustomTools()
	a.AddTools(custom)

	if cfg != nil {
		for name, mcpCfg := range cfg.MCP {
			if !mcpCfg.Enabled {
				continue
			}
			var client *mcp.MCPClient
			var err error
			if mcpCfg.Type == "local" {
				client, err = mcp.NewLocalClient(name, mcpCfg)
			} else if mcpCfg.Type == "remote" {
				client, err = mcp.NewRemoteClient(name, mcpCfg)
			}

			if err != nil {
				a.mcpErrors = append(a.mcpErrors, fmt.Sprintf("%s: %v", name, err))
				continue
			}

			if client != nil {
				mcpTools, err := client.ListTools()
				if err != nil {
					a.mcpErrors = append(a.mcpErrors, fmt.Sprintf("%s: %v", name, err))
					continue
				}
				var tools []tool.Tool
				for _, mt := range mcpTools {
					tools = append(tools, mt)
				}
				a.addMCPTools(tools)
			}
		}
	}
}

// recoverOrphanedToolCalls iterates over all assistant messages with ToolCalls,
// identifies any call IDs that have no following tool-result message, and
// re-executes them. This handles sessions that were persisted mid-tool-execution
// or messages that were compacted, where prior execution results may have been
// removed but the tool_call declarations remain.
func (a *Agent) recoverOrphanedToolCalls(messages []Message) []Message {
	// Build a set of all tool result IDs present in the conversation.
	resultIDs := make(map[string]bool)
	for _, msg := range messages {
		if msg.Role == "tool" && msg.ToolID != "" {
			resultIDs[msg.ToolID] = true
		}
	}

	// Collect all orphaned tool calls across ALL assistant messages.
	var orphans []ToolCall
	orphanIDs := make(map[string]bool) // dedup: avoid re-executing the same call ID twice
	for _, msg := range messages {
		if msg.Role == "assistant" {
			for _, tc := range msg.ToolCalls {
				if tc.ID != "" && !resultIDs[tc.ID] && !orphanIDs[tc.ID] {
					orphans = append(orphans, tc)
					orphanIDs[tc.ID] = true
				}
			}
		}
	}
	if len(orphans) == 0 {
		return messages
	}

	emitDebug("RECOVER", fmt.Sprintf("re-executing %d orphaned tool call(s) across history", len(orphans)))

	// Re-execute each orphan and append its result.
	for _, tc := range orphans {
		result, err := a.HandleToolCall(tc.Function.Name, json.RawMessage(tc.Function.Arguments))
		if err != nil {
			// ORPHAN_TOOL_ERROR: prefix is internal-only, used by TUI to render a visible warning.
			// Do not use this prefix in tool outputs; it has special semantic meaning for session recovery.
			result = fmt.Sprintf("ORPHAN_TOOL_ERROR:%s:%v\n%s", tc.Function.Name, err, result)
		}
		result = TruncateToolResult(tc.ID, result)
		messages = append(messages, Message{Role: "tool", ToolID: tc.ID, Content: result})
	}
	return messages
}

func truncateDebugArgs(args json.RawMessage, max int) string {
	s := string(args)
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}
