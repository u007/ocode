package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/hooks"
	"github.com/u007/ocode/internal/lsp"
	"github.com/u007/ocode/internal/mcp"
	"github.com/u007/ocode/internal/redact"
	"github.com/u007/ocode/internal/tool"
)

var DebugAppend func(kind, msg string)

func emitDebug(kind, msg string) {
	if DebugAppend != nil {
		DebugAppend(kind, msg)
		return
	}
	// No sink registered (headless modes: run/serve/acp). The TUI sets
	// DebugAppend before its alt-screen starts, so this stderr fallback never
	// corrupts the rendered frame; it only fires when there is no TUI to capture
	// the message, where stderr is the correct destination.
	fmt.Fprintf(os.Stderr, "[%s] %s\n", kind, msg)
}

// DebugAppendf is a fmt.Sprintf shortcut for callers outside this package
// that want to emit a debug log without importing fmt twice.
func DebugAppendf(kind, format string, args ...interface{}) {
	emitDebug(kind, fmt.Sprintf(format, args...))
}

// getClientAPIKey extracts the API key from an LLMClient via type assertion.
// Returns "(unknown)" if the client type doesn't expose the key.
func getClientAPIKey(c LLMClient) string {
	if gc, ok := c.(*GenericClient); ok {
		return gc.APIKey
	}
	return "(unknown)"
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

// RecapResult carries an async recap response plus a generation tag so callers
// can ignore stale results after a session reset.
type RecapResult struct {
	Gen  uint64
	Text string
}

type Agent struct {
	client    LLMClient
	tools     map[string]tool.Tool
	mcpTools  map[string]struct{}
	mcpErrors []string
	config    *config.Config
	mode      Mode
	spec      *AgentSpec
	// lspMgr, when non-nil, is the project-wide LSP manager. The agent
	// loop reads its diagnostic store on every Step to build a
	// transient system-message fragment (see injectLSPDiagnostics) — it
	// is never persisted to message history. nil disables the inject.
	lspMgr      *lsp.Manager
	permissions *PermissionManager
	activity    *ActivityTracker
	procs       *tool.ProcessRegistry
	runs        *AgentRunRegistry
	stopCh      chan struct{}
	stopMu      sync.Mutex
	// redactionRegistry, when non-nil, is used to resolve OCSEC tokens in
	// tool arguments back to original values before tool execution.
	redactionRegistry *redact.Registry
	// advisorEnabled is a runtime gate for the "advisor" tool. It is seeded
	// from cfg.Ocode.Advisor.Enabled at construction and can be flipped at
	// runtime (e.g. from the web sidebar) WITHOUT persisting to config.
	advisorEnabled atomic.Bool
	// parentAdvisorEnabled, when non-nil, makes the advisor gate reactive:
	// isToolAllowed dereferences this pointer instead of reading the agent's
	// own advisorEnabled. Sub-agents set this to point at the parent agent's
	// advisorEnabled field so mid-run toggles propagate immediately.
	parentAdvisorEnabled *atomic.Bool
	jobEvents            chan JobEvent
	retryEvents          chan *RetryStatusEvent
	// OnMessage, if set, is invoked for each message produced during Step
	// (assistant replies and tool results) as soon as they are generated,
	// enabling live UI updates between iterations of the tool-call loop.
	OnMessage func(Message)
	// OnDelta, if set, is invoked from inside Chat for each streamed reasoning
	// or text token (kind ∈ {"reasoning","text"}). The callback fires on the
	// HTTP goroutine — keep handlers fast and non-blocking (push to a buffered
	// channel). Subagents do not inherit OnDelta, preventing pollution of the
	// parent TUI.
	OnDelta func(kind, text string)
	// OnUsage, if set, is invoked when the provider streams token usage
	// information during a Chat call. The callback fires on the HTTP goroutine
	// — keep handlers fast and non-blocking. Not all providers support streaming
	// usage; currently Anthropic (via message_delta) and OpenAI/Copilot (final
	// chunk) deliver it. Subagents do not inherit OnUsage.
	OnUsage func(inputTokens, outputTokens int64)
	// OnCompactStart, if set, is invoked when async compaction begins (so the
	// UI can show a spinner). It runs from the goroutine that called
	// MaybeCompactAsync — keep handlers fast and thread-safe.
	OnCompactStart func()
	// OnCompact, if set, is invoked when async compaction finishes. The
	// callback receives a CompactResult describing whether compaction
	// occurred, the splice indices, and the summary message to insert.
	OnCompact func(CompactResult)
	// OnRecap, if set, is invoked when async recap finishes. The callback
	// receives the recap result produced by the small model.
	OnRecap func(RecapResult)
	// OnPermissionAsk, if set, is invoked synchronously when a tool call
	// requires a permission decision. It blocks until the user (via the TUI)
	// responds, returning the permission response. When set, HandleToolCall acts
	// on the returned level directly instead of emitting the PERMISSION_ASK:
	// sentinel string. This is set ONLY on sub-agents — the main agent leaves
	// it nil and keeps the pause-and-resume sentinel flow handled by the TUI.
	// Sub-agents run in their own goroutines and never reach the TUI's
	// tool-result handling, so the sentinel path is invisible to them.
	OnPermissionAsk func(PermissionRequest) PermissionResponse
	// OnPermissionGrant, if set, is invoked when the permission verifier derives
	// a durable auto-grant that should be persisted by the session layer. The
	// callback owns persistence; the agent keeps only the in-memory matcher in
	// sync after the callback succeeds.
	OnPermissionGrant func(config.AutoGrant) error
	// subAgentPermAsker is the permission-ask callback the TUI installs on the
	// main agent. It is not used by the main agent itself; it is copied onto
	// each sub-agent's OnPermissionAsk so sub-agent asks reach the TUI.
	subAgentPermAsker func(PermissionRequest) PermissionResponse
	// maxSteps limits the number of agentic iterations. 0 = unlimited.
	maxSteps int
	// compactMu serialises async compaction passes so a slow summary call
	// can't fire OnCompact twice for overlapping snapshots.
	compactMu sync.Mutex
	// recapMu serialises async recap passes.
	recapMu sync.Mutex
	// subagentDispatchGuard tracks consecutive identical task-tool dispatches
	// since the last user input, to break runaway loops where a small model
	// keeps re-launching the same subagent in response to its own completion
	// notifications.
	subagentDispatchMu    sync.Mutex
	subagentDispatchLast  string
	subagentDispatchCount int
	preloadedContextMu    sync.RWMutex
	preloadedContext      string // set by askAgent to avoid duplicate LoadContext calls
	preloadedModelContext string // cached result of LoadModelContext, set once lazily
	// pipeline, if set, runs in-process hook callbacks for tool calls and chat
	// requests. Field is named "pipeline" (not "hooks") because the hooks package
	// is already imported under that name.
	pipeline *hooks.Pipeline
}

// SetHooks wires an in-process hook pipeline into this agent.
func (a *Agent) SetHooks(p *hooks.Pipeline) { a.pipeline = p }

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

// chatWithDelta proxies client.Chat, attaching the agent's OnDelta callback
// to the underlying *GenericClient for the duration of the call. The field is
// always cleared on return so subagents that share the same client (see
// subagent.go) never inherit a stale callback.
// stopCh, when provided, is used to derive a cancellable context so that
// Escape / Cancel interrupts in-flight HTTP requests immediately.
func (a *Agent) chatWithDelta(stopCh <-chan struct{}, messages []Message, toolDefs []map[string]interface{}) (*Message, error) {
	gc, ok := a.client.(*GenericClient)
	if ok {
		if a.OnDelta != nil {
			gc.SetOnDelta(a.OnDelta)
			defer gc.SetOnDelta(nil)
		}
		if a.OnUsage != nil {
			gc.SetOnUsage(a.OnUsage)
			defer gc.SetOnUsage(nil)
		}
		if a.retryEvents != nil {
			origNotifier := gc.RetryNotifier
			// specName is safe to access even when a.spec is nil (initial agent
			// created via NewAgent without SetSpec). Guard to avoid panic when 429
			// retries fire before a spec is assigned.
			specName := ""
			if a.spec != nil {
				specName = a.spec.Name
			}
			gc.RetryNotifier = func(attempt, maxRetries int, delay time.Duration, err error) {
				a.EmitRetryStatus(&RetryStatusEvent{
					ID:         specName,
					Name:       gc.Model,
					RetryCount: attempt + 1,
					MaxRetries: maxRetries + 1,
					LastError:  err.Error(),
					RetryDelay: delay,
					RetryingAt: time.Now(),
					Kind:       "llm",
				})
				emitDebug("retry", fmt.Sprintf("llm %s — retry %d/%d in %v: %v",
					gc.Model, attempt+1, maxRetries+1, delay, err))
			}
			defer func() { gc.RetryNotifier = origNotifier }()
		}
		origTemp, origTopP := gc.Temperature, gc.TopP
		if a.pipeline != nil {
			cp := a.pipeline.RunChatParams(gc.Model, hooks.ChatParams{
				Temperature: gc.Temperature,
				TopP:        gc.TopP,
			})
			gc.Temperature = cp.Temperature
			gc.TopP = cp.TopP
		}
		defer func() {
			gc.Temperature = origTemp
			gc.TopP = origTopP
		}()
		ctx, ctxCancel := stopChContext(stopCh)
		defer ctxCancel()
		return gc.ChatWithContext(ctx, messages, toolDefs)
	}
	return a.client.Chat(messages, toolDefs)
}

// SetPreloadedContext stores a pre-computed context string so
// BasePromptMessages skips the filesystem read on the next call.
// Cleared automatically after use in Step / buildAgentMessagesSnapshot.
func (a *Agent) SetPreloadedContext(ctx string) {
	a.preloadedContextMu.Lock()
	a.preloadedContext = ctx
	a.preloadedContextMu.Unlock()
}

// getPreloadedContext returns the cached context under read lock.
func (a *Agent) getPreloadedContext() string {
	a.preloadedContextMu.RLock()
	defer a.preloadedContextMu.RUnlock()
	return a.preloadedContext
}

// NewAgent constructs an agent. lspMgr is optional: when non-nil the
// agent loop auto-injects a transient system-message fragment with the
// current LSP diagnostics on every Step (see injectLSPDiagnostics). The
// fragment is rebuilt every turn from the manager's DiagnosticStore and
// is never persisted to message history. Pass nil to disable the
// auto-inject (e.g. for tests with no LSP setup).
func NewAgent(client LLMClient, tools []tool.Tool, cfg *config.Config, lspMgr *lsp.Manager) *Agent {
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
		lspMgr:      lspMgr,
		permissions: NewPermissionManager(),
		activity:    newActivityTracker(),
	}
	a.procs = tool.NewProcessRegistry()
	a.runs = NewAgentRunRegistry()
	a.stopCh = make(chan struct{})
	a.jobEvents = make(chan JobEvent, 32)
	a.retryEvents = make(chan *RetryStatusEvent, 32)
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
	a.tools["wait"] = WaitTool{procs: a.procs, runs: a.runs, agent: a}
	// "agent" tool retired in favor of "task". AgentTool the type is kept
	// only so existing transcripts/back-compat permission entries still
	// resolve. It is no longer registered on new agents.
	// Always register the advisor tool; whether it is exposed to the model is
	// gated at runtime by advisorEnabled (seeded from config, default enabled,
	// flippable from the web sidebar without touching config).
	a.tools["advisor"] = AdvisorTool{cfg: cfg, mainAgent: a}
	a.advisorEnabled.Store(cfg == nil || cfg.Ocode.Advisor.Enabled)
	a.tools["task"] = TaskTool{mainAgent: a, registry: DefaultAgentRegistry, runs: a.runs}
	a.tools["agent_status"] = AgentStatusTool{runs: a.runs}
	a.tools["task_status"] = TaskStatusTool{runs: a.runs}
	if cfg != nil {
		a.permissions.LoadFromConfig(cfg.Permission)
		a.permissions.LoadFromOcode(cfg.Ocode.Permissions)
	}
	// Set workDir for path-scoped permission checks
	if wd, err := os.Getwd(); err == nil {
		a.permissions.SetWorkDir(wd)
		// Pass workDir to the advisor tool so Claude Code CLI resolves paths
		// against the project root, not the process launch directory.
		if at, ok := a.tools["advisor"].(AdvisorTool); ok {
			at.workDir = wd
			a.tools["advisor"] = at
		}
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

	// Capture the stop channel once at the start of this invocation.
	// Using a local snapshot means a caller can call ResetCancellation() to
	// unblock the next Step() without affecting this one — old goroutines still
	// check the channel that was live when they started.
	stopCh := a.StopCh()
	isCancelled := func() bool {
		select {
		case <-stopCh:
			return true
		default:
			return false
		}
	}

	preLen := len(messages)
	messages = a.PrepareMessages(messages, "")
	messages = a.injectLSPDiagnostics(messages)
	toolDefs := a.GetToolDefinitions()
	var newMsgs []Message
	emitDebug("AGENT", fmt.Sprintf("Step: %d msgs (after prompt prep, was %d) with %d tools", len(messages), preLen, len(toolDefs)))

	// Recover orphaned tool calls from prior sessions: find the last assistant
	// message that has ToolCalls, then check which call IDs have no following
	// tool-result message. Re-execute those calls now (the prior execution is
	// guaranteed gone — we're in a new Step invocation).
	messages = a.recoverOrphanedToolCalls(messages)

	for i := 0; ; i++ {
		if isCancelled() {
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
			resp, err := a.chatWithDelta(stopCh, messages, toolDefs)
			if err != nil {
				return nil, err
			}
			if isCancelled() {
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
		resp, err := a.chatWithDelta(stopCh, messages, toolDefs)
		a.activity.setLLMRunning(false)
		if err != nil {
			if isCancelled() || errors.Is(err, context.Canceled) {
				// Expected cancellation (user interrupt / superseded request) —
				// benign, not an error condition. Log quietly per logging rules.
				emitDebug("LLM", fmt.Sprintf("request cancelled: provider=%s model=%q",
					a.client.GetProvider(), a.client.GetModel()))
			} else {
				emitDebug("ERROR", fmt.Sprintf("LLM error: provider=%s model=%q apiKey=%s error: %v",
					a.client.GetProvider(), a.client.GetModel(), maskKey(getClientAPIKey(a.client)), err))
			}
			return nil, err
		}
		if isCancelled() {
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
			// Best-effort cancellation check before delivering the
			// response. Cancel() can still race in after this check and
			// before OnMessage returns; OnMessage must not block on a
			// receiver that goes away on cancel.
			if isCancelled() {
				return newMsgs, nil
			}
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
				go func(idx int, tc ToolCall, cancelled func() bool) {
					defer wg.Done()
					if cancelled() {
						return
					}
					a.activity.toolStarted(tc.Function.Name)
					result, err := a.HandleToolCall(tc.Function.Name, json.RawMessage(tc.Function.Arguments))
					a.activity.toolDone(tc.Function.Name)
					var notice string
					if err != nil {
						var ne *tool.NoticedError
						if errors.As(err, &ne) {
							notice = ne.Notice
						}
						result = fmt.Sprintf("Error: %v", err)
					}
					result = TruncateToolResult(tc.ID, result)
					results[idx] = Message{Role: "tool", ToolID: tc.ID, Content: result, Notice: notice}
				}(i, resp.ToolCalls[i], isCancelled)
			}
			wg.Wait()
		}

		for _, i := range sequentialTCs {
			if isCancelled() {
				return newMsgs, nil
			}
			tc := resp.ToolCalls[i]
			a.activity.toolStarted(tc.Function.Name)
			result, err := a.HandleToolCall(tc.Function.Name, json.RawMessage(tc.Function.Arguments))
			a.activity.toolDone(tc.Function.Name)
			var notice string
			if err != nil {
				var ne *tool.NoticedError
				if errors.As(err, &ne) {
					notice = ne.Notice
				}
				result = fmt.Sprintf("Error: %v", err)
			}
			result = TruncateToolResult(tc.ID, result)
			results[i] = Message{Role: "tool", ToolID: tc.ID, Content: result, Notice: notice}
		}
		if isCancelled() {
			return newMsgs, nil
		}

		pauseAfterResults := false
		for _, toolMsg := range results {
			newMsgs = append(newMsgs, toolMsg)
			messages = append(messages, toolMsg)
			if a.OnMessage != nil {
				// Best-effort cancellation check; see note above OnMessage
				// call for the assistant response. Residual race window
				// is accepted — OnMessage must tolerate post-cancel sends.
				if isCancelled() {
					return newMsgs, nil
				}
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

const lspDiagnosticsAutoInjectLimit = 50

// injectLSPDiagnostics prepends a transient system message with the current
// project LSP diagnostics. The message is rebuilt on every Step and is never
// persisted to history.
func (a *Agent) injectLSPDiagnostics(messages []Message) []Message {
	if a == nil || a.lspMgr == nil {
		return messages
	}
	store := a.lspMgr.Diagnostics()
	if store == nil || store.IsEmpty() {
		return messages
	}
	rendered := tool.RenderDiagnosticsPage(store.All(), 0, lspDiagnosticsAutoInjectLimit)
	if strings.TrimSpace(rendered) == "" {
		return messages
	}
	msg := Message{Role: "system", Content: rendered}
	insertAt := 0
	for insertAt < len(messages) && messages[insertAt].Role == "system" {
		insertAt++
	}
	out := make([]Message, 0, len(messages)+1)
	out = append(out, messages[:insertAt]...)
	out = append(out, msg)
	out = append(out, messages[insertAt:]...)
	return out
}

// warnIfNearWindow emits a debug warning when the most recent prompt token
// count is close to the active model's window. Mid-loop compaction is unsafe
// (would split open tool-call pairs), so the actual compaction is deferred to
// the post-Step trigger fired by the TUI on streamDoneMsg. This warning lets
// users see why they may be approaching a hard context-length error.
func (a *Agent) warnIfNearWindow(promptTokens int64) {
	rt := a.resolveCompactRuntime(false)
	if !rt.Enabled || rt.WindowTokens <= 0 {
		return
	}
	limit := int64(float64(rt.WindowTokens) * rt.TokenThreshold)
	if promptTokens >= limit {
		emitDebug("COMPACT", fmt.Sprintf("warning: prompt tokens=%d ≥ threshold=%d (window=%d); compaction will run after this Step", promptTokens, limit, rt.WindowTokens))
	}
}

// resolveCompactRuntime materialises the compaction knobs for the current
// agent + active model. When force is false, it returns Enabled=false when
// compaction is disabled. Manual /compact uses force=true so it can still run
// even if automatic compaction is switched off.
func (a *Agent) resolveCompactRuntime(force bool) compactRuntime {
	rt := compactRuntime{Enabled: false}
	if a.config == nil {
		return rt
	}
	c := a.config.Ocode.Compact
	if !c.Enabled && !force {
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
		rt.SummaryTimeoutSeconds = 90
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

// MaybeCompactAsync runs compaction in a goroutine when the current context
// usage is above the configured threshold.
//
// Returns true iff a compaction goroutine was actually started — false when
// disabled, below threshold, or another compaction is already in flight. The
// boolean lets callers (TUI) avoid stomping per-trigger state with snapshots
// from a deferred call that never produced a result.
//
// The provided messages slice is read-only; the caller is responsible for
// splicing its own copy when OnCompact fires.
func (a *Agent) MaybeCompactAsync(messages []Message) bool {
	rt := a.resolveCompactRuntime(false)
	if !rt.Enabled {
		return false
	}
	need, used := shouldCompact(messages, rt)
	if !need {
		return false
	}
	return a.startCompactAsync(messages, rt, "", fmt.Sprintf("triggered: ~%d tokens used, window=%d, threshold=%.2f", used, rt.WindowTokens, rt.TokenThreshold))
}

// CompactAsync runs a manual compaction in a goroutine, bypassing the token
// threshold check so /compact always attempts a summary when the feature is
// available. focus is optional user guidance ("/compact <focus>") passed to
// the summary prompt.
func (a *Agent) CompactAsync(messages []Message, focus string) bool {
	rt := a.resolveCompactRuntime(true)
	if !rt.Enabled {
		return false
	}
	return a.startCompactAsync(messages, rt, focus, fmt.Sprintf("manual compaction requested: messages=%d window=%d", len(messages), rt.WindowTokens))
}

func (a *Agent) startCompactAsync(messages []Message, rt compactRuntime, focus, note string) bool {
	if !a.compactMu.TryLock() {
		emitDebug("COMPACT", "skipped: another compaction in flight")
		return false
	}
	snapshot := make([]Message, len(messages))
	copy(snapshot, messages)
	emitDebug("COMPACT", note)
	if a.OnCompactStart != nil {
		a.OnCompactStart()
	}
	go func() {
		defer a.compactMu.Unlock()
		result := a.runCompact(snapshot, rt, focus)
		if a.OnCompact != nil {
			a.OnCompact(result)
		}
	}()
	return true
}

// RecapAsync generates a conversation recap using the small model in a
// goroutine. Returns false if a recap is already in flight.
// instruction is optional additional guidance appended to the recap prompt.
func (a *Agent) RecapAsync(messages []Message, gen uint64, instruction string) bool {
	if !a.recapMu.TryLock() {
		return false
	}
	snapshot := make([]Message, len(messages))
	copy(snapshot, messages)
	go func() {
		defer a.recapMu.Unlock()
		text := a.runRecap(snapshot, instruction)
		if a.OnRecap != nil {
			a.OnRecap(RecapResult{Gen: gen, Text: text})
		}
	}()
	return true
}

// Compact runs context compaction synchronously and returns the result.
// Returns (result, false) if compaction is disabled in config.
func (a *Agent) Compact(messages []Message) (CompactResult, bool) {
	rt := a.resolveCompactRuntime(true)
	if !rt.Enabled {
		return CompactResult{}, false
	}
	return a.runCompact(messages, rt, ""), true
}

// Recap generates a conversation recap synchronously using the small model.
// instruction is optional additional guidance appended to the recap prompt.
func (a *Agent) Recap(messages []Message, instruction string) string {
	return a.runRecap(messages, instruction)
}

func (a *Agent) runRecap(messages []Message, instruction string) string {
	client := a.recapClient()
	if client == nil {
		return "Recap unavailable: no LLM client."
	}

	var b strings.Builder
	b.WriteString("You are a conversation recap assistant. Summarize the following conversation in caveman style — short, punchy, no fluff.\n\n")
	b.WriteString("Cover these sections:\n")
	b.WriteString("1. WHAT USER WANT — what was asked\n")
	b.WriteString("2. WHAT FIND — what was found or discovered\n")
	b.WriteString("3. DECISION — what decisions were made\n")
	b.WriteString("4. DO — what was updated and tested\n\n")
	b.WriteString("Format: use headers and bullet points. Be terse. No filler.\n\n")
	if instruction != "" {
		fmt.Fprintf(&b, "Additional focus: %s\n\n", instruction)
	}
	b.WriteString("CONVERSATION:\n")

	for _, msg := range messages {
		role := "user"
		if msg.Role == "assistant" {
			role = "assistant"
		}
		content := msg.Content
		if len(content) > 2000 {
			content = content[:2000] + "... (truncated)"
		}
		fmt.Fprintf(&b, "[%s] %s\n\n", role, content)
	}

	prompt := b.String()

	ctx, cancel := contextWithTimeout(60)
	defer cancel()

	done := make(chan struct {
		content string
		err     error
	}, 1)
	go func() {
		resp, err := client.Chat([]Message{{Role: "user", Content: prompt}}, nil)
		if err != nil {
			done <- struct {
				content string
				err     error
			}{"", err}
			return
		}
		done <- struct {
			content string
			err     error
		}{resp.Content, nil}
	}()
	select {
	case <-ctx.Done():
		return "Recap timed out."
	case r := <-done:
		if r.err != nil {
			return fmt.Sprintf("Recap failed: %v", r.err)
		}
		if strings.TrimSpace(r.content) == "" {
			return "Recap returned empty."
		}
		return r.content
	}
}

func (a *Agent) recapClient() LLMClient {
	if a.config == nil {
		return a.client
	}
	small := strings.TrimSpace(a.config.Ocode.SmallModel)
	if small == "" {
		return a.client
	}
	if client := NewClient(a.config, small); client != nil {
		return client
	}
	return a.client
}

func (a *Agent) runCompact(messages []Message, rt compactRuntime, focus string) CompactResult {
	res := CompactResult{OriginalLen: len(messages)}

	prefixEnd := findPrefixEnd(messages)

	// Anchored multi-compaction: if a previous summary exists in the message
	// stream, treat its position as the new prefix boundary so we only
	// summarise content created since it. The previous summary itself is
	// passed to the model as <previous-summary> and overwritten in place when
	// the splice runs.
	prevSummary, prevSummaryIdx := findPreviousSummary(messages)
	middleStart := prefixEnd
	replaceFrom := prefixEnd
	if prevSummaryIdx >= prefixEnd {
		middleStart = prevSummaryIdx + 1
		replaceFrom = prevSummaryIdx
	}

	tailStart := findTurnBoundary(messages, rt.KeepRecentTurns)
	if tailStart < middleStart {
		tailStart = middleStart
	}
	tailStart = safeCut(messages, tailStart)
	if tailStart <= middleStart {
		// When a previous summary exists but there is no new compactible
		// middle after it, fall back to re-compacting the summary itself.
		// This lets manual /compact always produce a result (the user
		// explicitly asked for it), and the anchored re-compaction
		// re-summarises the previous summary into a fresh one.
		if prevSummaryIdx >= prefixEnd {
			emitDebug("COMPACT", fmt.Sprintf("manual compact: no new content after summary at %d; re-compacting summary itself", prevSummaryIdx))
			middleStart = prevSummaryIdx
			tailStart = prevSummaryIdx + 1
			prevSummary = "" // don't pass the old summary as <previous-summary> since we're re-compacting it
		} else if rt.KeepRecentTurns > 1 {
			// Sessions with few user turns (e.g. one long agentic run) exhaust
			// the KeepRecentTurns budget before reaching the prefix end, leaving
			// no compactible middle. Retry with KeepRecentTurns=1 so we keep
			// only the last user turn and summarise everything before it.
			tailStart = findTurnBoundary(messages, 1)
			if tailStart < middleStart {
				tailStart = middleStart
			}
			tailStart = safeCut(messages, tailStart)
			if tailStart <= middleStart {
				emitDebug("COMPACT", "skipped: no compactible middle even with KeepRecentTurns=1")
				return res
			}
			emitDebug("COMPACT", fmt.Sprintf("retried with KeepRecentTurns=1: tailStart=%d middleStart=%d", tailStart, middleStart))
		} else {
			emitDebug("COMPACT", "skipped: no compactible middle after safe-cut")
			return res
		}
	}

	middle := messages[middleStart:tailStart]
	if len(middle) == 0 {
		return res
	}

	// Prune oversized tool results in the middle slice before summarising.
	// Keeps signal density high without losing tool-call structure.
	pruned := pruneToolResults(middle, compactPruneToolMaxChars)

	client := a.compactSummaryClient()

	// Use an inactivity-based timeout: the timer resets each time the LLM
	// streams data (onDelta fires). This prevents spurious timeouts while
	// the model is actively generating a summary. One inactivity context
	// covers the whole pass — each streamed chunk extends the deadline, so
	// multi-batch summarisation does not starve later batches.
	var ctx context.Context
	var cancel context.CancelFunc
	if rt.SummaryTimeoutSeconds > 0 {
		var reset func()
		ctx, cancel, reset = inactivityContext(rt.SummaryTimeoutSeconds)
		// Wire the reset into the streaming delta callback so each chunk
		// received from the LLM extends the deadline.
		if gc, ok := client.(*GenericClient); ok {
			gc.SetOnDelta(func(kind, text string) { reset() })
			defer gc.SetOnDelta(nil)
		}
	} else {
		ctx, cancel = contextWithTimeout(rt.SummaryTimeoutSeconds)
	}
	defer cancel()

	// Chunked anchored summarisation: when the middle exceeds the per-call
	// input budget, split it into consecutive batches and summarise each in
	// order, feeding the running summary forward as the anchor. Nothing is
	// dropped unsummarised — the old behaviour silently discarded the oldest
	// middle messages once the prompt was over budget.
	batches := chunkMiddleByBudget(pruned, rt.MaxSummaryInputTokens)
	running := prevSummary
	totalDropped := 0
	for bi, batch := range batches {
		prompt, dropped := buildSummaryPrompt(batch, rt.MaxSummaryInputTokens, running, focus)
		if dropped > 0 {
			totalDropped += dropped
			emitDebug("COMPACT", fmt.Sprintf("batch %d/%d: dropped %d msgs from summary input (size cap)", bi+1, len(batches), dropped))
		}
		summaryText, err := runSummary(ctx, client, prompt, rt.SummaryMaxRetries)
		if err != nil {
			emitDebug("COMPACT", fmt.Sprintf("summary failed on batch %d/%d: %v", bi+1, len(batches), err))
			res.Err = err
			return res
		}
		running = summaryText
	}
	summaryText := running

	if len(batches) > 1 || totalDropped > 0 {
		var notes []string
		if len(batches) > 1 {
			notes = append(notes, fmt.Sprintf("summarised in %d batches", len(batches)))
		}
		if totalDropped > 0 {
			notes = append(notes, fmt.Sprintf("%d messages dropped for size", totalDropped))
		}
		res.Note = strings.Join(notes, ", ")
	}

	header := fmt.Sprintf("Compacted summary covering %d messages", len(middle))
	if prevSummaryIdx >= 0 {
		header = "Compacted anchored summary (updated)"
	}
	summaryMsg := Message{
		Role: "system",
		Content: fmt.Sprintf(
			"%s\n%s\n\n%s",
			compactionSummaryMarker, header, strings.TrimSpace(summaryText),
		),
	}
	res.OK = true
	res.ReplaceFrom = replaceFrom
	res.ReplaceTo = tailStart
	res.Summary = summaryMsg
	emitDebug("COMPACT", fmt.Sprintf("done: replaced [%d:%d] (%d msgs) with anchored=%v summary", replaceFrom, tailStart, tailStart-replaceFrom, prevSummaryIdx >= 0))
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

// SetRedactionRegistry sets the registry for resolving OCSEC tokens in tool args.
func (a *Agent) SetRedactionRegistry(reg *redact.Registry) {
	a.redactionRegistry = reg
}

func (a *Agent) HandleToolCall(name string, args json.RawMessage) (string, error) {
	if deny, ok := gateToolCall(a.Mode(), name, args); !ok {
		return deny, nil
	}

	if a.permissions != nil {
		autoEnabled := a.permissions.AutoPermissionEnabled()
		decision := a.permissions.Decide(name, args)
		emitDebug("PERMISSION", a.permissionDecisionTrace(name, args, decision, autoEnabled))
		if decision.Level == PermissionDeny {
			return fmt.Sprintf("denied: tool %q is not permitted by permission rules. This call is blocked by policy — do not retry the same call; choose a different approach or ask the user.", name), nil
		}
		if decision.Level == PermissionAsk {
			if autoEnabled {
				// Build the permission request so we can check for harmful ops.
				req := PermissionRequest{ToolName: name, Args: args, Scope: PermissionScopeTool, Rule: "tool." + name}
				if decision.Request != nil {
					req = *decision.Request
				}
				if IsHarmfulRequest(req) && !a.autoPermissionAllowsDestructive() {
					emitDebug("PERMISSION", fmt.Sprintf("tier=auto_fallback_harmful tool=%s command=%s", name, req.Command))
					// Fall through to human ask unless destructive auto-permission is enabled.
				} else {
					// Consult the LLM permission model (interpreter executions
					// take the structured effect-verification path; everything
					// else the plain ALLOW/DENY path).
					allowed, reason, summary := a.consultPermissionModel(name, args, &req)
					if allowed {
						emitDebug("PERMISSION", fmt.Sprintf("tier=auto_llm_allow tool=%s model=%s reason=%s", name, a.autoPermissionModelDisplayName(), reason))
						return a.executeToolCall(name, args)
					}
					emitDebug("PERMISSION", fmt.Sprintf("tier=auto_llm_deny tool=%s model=%s reason=%s", name, a.autoPermissionModelDisplayName(), reason))
					// LLM denied — fall through to human ask.
					if decision.Request == nil && name == "bash" {
						// Reuse the bash builder so the deny dialog shows the
						// command/prefix, not a thinner args-only summary.
						req = *bashPermissionRequest(args, bashCommand(args), "")
					}
					req.DenyReason = reason
					req.Summary = summary
					decision.Request = &req
				}
			}
			emitDebug("PERMISSION", fmt.Sprintf("tier=human_ask tool=%s request=%s callback=%t", name, permissionRequestSummary(decision.Request), a.OnPermissionAsk != nil))
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
				return fmt.Sprintf("denied: tool %q denied by user. Do not retry the same call; ask the user how they'd like to proceed or take a different approach.", name), nil
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

func (a *Agent) autoPermissionModelName() string {
	if a == nil || a.config == nil {
		return "unavailable"
	}
	if auto := a.config.Ocode.Permissions.Auto; auto != nil {
		if model := strings.TrimSpace(auto.Model); model != "" {
			return model
		}
	}
	if model := ResolveSmallModel(a.config); model != "" {
		return model
	}
	return "unavailable"
}

func (a *Agent) autoPermissionModelDisplayName() string {
	if a == nil || a.config == nil {
		return "unavailable"
	}
	if auto := a.config.Ocode.Permissions.Auto; auto != nil {
		if model := strings.TrimSpace(auto.Model); model != "" {
			return model
		}
	}
	if model := ResolveSmallModel(a.config); model != "" {
		return model + " (resolved small model)"
	}
	return "unavailable"
}

func (a *Agent) autoPermissionAllowsDestructive() bool {
	return a != nil && a.config != nil && a.config.Ocode.Permissions.Auto != nil && a.config.Ocode.Permissions.Auto.AllowDestructive
}

func (a *Agent) permissionDecisionTrace(name string, args json.RawMessage, decision PermissionDecision, autoEnabled bool) string {
	parts := []string{
		"tier=static_decide",
		"tool=" + name,
		"level=" + string(decision.Level),
		"auto_enabled=" + strconv.FormatBool(autoEnabled),
		"mode=" + string(a.permissions.Mode()),
	}
	if decision.Request != nil {
		parts = append(parts,
			"scope="+string(decision.Request.Scope),
			"rule="+decision.Request.Rule,
			"request="+permissionRequestSummary(decision.Request),
		)
	} else {
		parts = append(parts, "request=none")
	}
	if decision.Level == PermissionAsk && autoEnabled {
		parts = append(parts, "auto_model="+a.autoPermissionModelDisplayName())
	}
	return strings.Join(parts, " ")
}

func permissionRequestSummary(req *PermissionRequest) string {
	if req == nil {
		return "none"
	}
	data, err := json.Marshal(req)
	if err != nil {
		return fmt.Sprintf("marshal_error:%v", err)
	}
	return truncateDebugArgs(data, 240)
}

// consultPermissionModel routes a permission decision to the right model path:
// a bash command that classifies as interpreter execution goes through the
// structured effect-verification path; every other tool/command uses the plain
// ALLOW/DENY path.
func (a *Agent) consultPermissionModel(name string, args json.RawMessage, req *PermissionRequest) (bool, string, string) {
	if name == "bash" {
		var p struct {
			Command string `json:"command"`
		}
		if err := json.Unmarshal(args, &p); err == nil && p.Command != "" {
			if ie, ok := classifyInterpreterExecution(p.Command); ok {
				return a.askPermissionModelInterpreter(p.Command, ie)
			}
		}
	}
	allowed, reason := a.askPermissionModel(name, args, req)
	return allowed, reason, ""
}

// askPermissionModel sends a permission request to the configured LLM model
// and returns (allowed bool, reason string). The model can only approve or
// ask (deny falls through to human). Returns (true, "") on approval,
// (false, reason) on denial/ask, and (false, error) on LLM failure.
//
// The LLM is given a read_file tool so it can explore the codebase before
// deciding. The tool call loop is capped at maxToolCalls to prevent abuse.
func (a *Agent) askPermissionModel(toolName string, args json.RawMessage, req *PermissionRequest) (bool, string) {
	modelName := a.autoPermissionModelName()
	modelLabel := a.autoPermissionModelDisplayName()
	if modelName == "unavailable" {
		return false, "no permission model configured"
	}

	client := newClientFn(a.config, modelName)
	if client == nil {
		emitDebug("PERMISSION", fmt.Sprintf("tier=auto_llm_fail tool=%s model=%s error=client_creation_failed", toolName, modelLabel))
		return false, "could not create LLM client"
	}
	pinDeterministicSampling(client)

	// Gather context limits from config.
	maxCtxBytes := 2048
	maxSources := 3
	maxLinesPerSource := 40
	if a.config != nil && a.config.Ocode.Permissions.Auto != nil {
		if a.config.Ocode.Permissions.Auto.MaxContextBytes > 0 {
			maxCtxBytes = a.config.Ocode.Permissions.Auto.MaxContextBytes
		}
		if a.config.Ocode.Permissions.Auto.MaxContextSources > 0 {
			maxSources = a.config.Ocode.Permissions.Auto.MaxContextSources
		}
		if a.config.Ocode.Permissions.Auto.MaxContextLinesPerSource > 0 {
			maxLinesPerSource = a.config.Ocode.Permissions.Auto.MaxContextLinesPerSource
		}
	}

	// Build initial context snapshot.
	context := a.buildPermissionContext(toolName, args, maxCtxBytes, maxSources, maxLinesPerSource)

	// Build the prompt.
	toolArgs := string(args)
	if len(toolArgs) > 500 {
		toolArgs = toolArgs[:500] + "...(truncated)"
	}

	rule := "tool." + toolName
	scope := "tool"
	if req != nil {
		rule = req.Rule
		scope = string(req.Scope)
	}

	prompt := fmt.Sprintf(`You are a permission gatekeeper for an AI coding assistant.
A tool call is requesting permission. Decide whether to ALLOW or DENY it.

Tool: %s
Arguments: %s
Rule: %s
Scope: %s

Project context:
%s

You have a read_file tool to explore the codebase before deciding. Use it to:
- Read the target file being written/edited/deleted
- Check project configuration files
- Understand what a bash command would operate on

If a target file does not exist yet, that is normal for a command that creates it
(e.g. a heredoc/redirect that writes a new path). Do NOT treat a missing file as a
reason to refuse — decide from the command and arguments.

After gathering enough context, end your response with a verdict line that is
EXACTLY one of the following — the line must START with the verdict word, with no
reasoning before it on that line:
ALLOW: <brief reason>
DENY: <brief reason>
Do not bury the words ALLOW or DENY inside a sentence (e.g. "I would ALLOW this");
the final line must begin with the bare verdict word followed by a colon.
Keep your reply short. Examples of correctly formatted final lines:
ALLOW: writes a test file inside the project directory
ALLOW: read-only listing of project files
DENY: deletes files outside the working directory
These are format examples only — decide from THIS request's tool and arguments.`, toolName, toolArgs, rule, scope, context)

	// Apply custom prompt from config if set.
	if a.config != nil && a.config.Ocode.Permissions.Auto != nil && a.config.Ocode.Permissions.Auto.Prompt != "" {
		prompt = a.config.Ocode.Permissions.Auto.Prompt + "\n\n" + prompt
	}

	tools := []map[string]interface{}{permissionReadFileTool()}
	messages := []Message{{Role: "user", Content: prompt}}

	finalText, gotFinal, failReason := runPermissionModelLoop(a.StopCh(), client, messages, tools, modelLabel, toolName)
	if !gotFinal {
		return false, failReason
	}

	decided, allow, reason := parsePermissionVerdict(finalText)
	if !decided {
		// One strict-format reprompt before giving up: weak judge models often
		// reach a correct decision but wrap it in prose the parser rejects. A
		// single cheap retry demanding the bare verdict line recovers most of
		// those instead of falling through to a needless human prompt. Tools are
		// withheld so the model must answer, and the prior tool exchanges are
		// not replayed — the model's own final text carries its conclusion, and
		// the retry only asks it to restate that in the required format.
		emitDebug("PERMISSION", fmt.Sprintf("tier=auto_llm_reprompt tool=%s response=%s", toolName, truncateDebugArgs([]byte(finalText), 100)))
		messages = append(messages,
			Message{Role: "assistant", Content: finalText},
			Message{Role: "user", Content: "Your previous reply did not contain a parseable verdict. Reply with EXACTLY one line and nothing else:\nALLOW: <brief reason>\nor\nDENY: <brief reason>"})
		// A failed retry is logged inside runPermissionModelLoop; the original
		// ambiguous text then falls through to the human prompt below.
		if retryText, gotRetry, retryErr := runPermissionModelLoop(a.StopCh(), client, messages, nil, modelLabel, toolName); gotRetry {
			finalText = retryText
			decided, allow, reason = parsePermissionVerdict(retryText)
		} else if retryErr != "" {
			// Surface the transport error so the user knows the retry failed
			// rather than just seeing "ambiguous LLM response".
			emitDebug("PERMISSION", fmt.Sprintf("tier=auto_llm_reprompt_error tool=%s err=%s", toolName, retryErr))
			return false, fmt.Sprintf("permission judge retry failed: %s", retryErr)
		}
	}
	if decided {
		// A model ALLOW never overrides Go's deterministic guardrails — confidence
		// alone never auto-approves (mirrors the interpreter effect verifier).
		if allow {
			if ok, vreason := a.verifyAutoGrant(toolName, args, req); !ok {
				emitDebug("PERMISSION", fmt.Sprintf("tier=auto_llm_guardrail_reject tool=%s reason=%s", toolName, vreason))
				return false, vreason
			}
		}
		return allow, reason
	}

	emitDebug("PERMISSION", fmt.Sprintf("tier=auto_llm_ambiguous tool=%s response=%s", toolName, truncateDebugArgs([]byte(finalText), 100)))
	return false, "ambiguous LLM response: " + finalText
}

// pinDeterministicSampling forces greedy decoding on a freshly created
// permission-judge client. Verdicts must be reproducible; small/local models
// (which are common as the auto-permission judge) often default to temperature
// 1.0, which makes their already-shaky verdict formatting flake run-to-run.
// Safe to mutate: the judge client is created per permission request, never
// shared. applyGenerationParams skips reasoning models that reject sampling
// tunables, so pinning here is a no-op for those.
func pinDeterministicSampling(client LLMClient) {
	if gc, ok := client.(*GenericClient); ok {
		gc.Temperature = floatPtr(0)
	}
}

// pathConfinedAutoTools are the path-scoped tools whose auto-grant target is a
// concrete file path that must stay inside the allowed roots and clear of
// sensitive paths. Pattern/dir tools (glob, list, grep, repo_overview) are
// excluded — their "path" is a glob/directory that does not resolve cleanly and
// would over-reject to human Ask.
var pathConfinedAutoTools = map[string]bool{
	"read": true, "write": true, "edit": true, "delete": true,
	"multiedit": true, "multi_file_edit": true, "replace_lines": true,
	"apply_patch": true, "format": true, "lsp": true,
}

// verifyAutoGrant re-checks a model ALLOW verdict against Go's deterministic
// guardrails before the plain ALLOW/DENY path auto-grants. It returns ok=false
// (with a human-readable reason) to defer to human Ask. This is intentionally
// narrow: for arbitrary bash it can only reject hard-blocked commands (harmful
// bash is already gated upstream by IsHarmfulRequest before the model is
// consulted), and it confines path-scoped file tools to the allowed roots —
// it does NOT make the plain bash path as scrutinised as the interpreter path.
func (a *Agent) verifyAutoGrant(toolName string, args json.RawMessage, req *PermissionRequest) (bool, string) {
	pm := a.permissions
	if pm == nil {
		return true, ""
	}
	if toolName == "bash" {
		cmd := ""
		if req != nil {
			cmd = req.Command
		}
		if cmd == "" {
			cmd = bashCommand(args)
		}
		if isHardBlockedCommand(cmd) {
			return false, "hard-blocked command cannot be auto-granted"
		}
		// A command the static decider flagged as targeting an out-of-workspace
		// path must never be auto-granted on the model's word alone — scope
		// expansion requires explicit human approval (mirrors the path-confined
		// file-tool check below).
		if req != nil && req.OutOfScopePath != "" {
			return false, "command targets path outside allowed roots: " + req.OutOfScopePath
		}
		return true, ""
	}
	if pathConfinedAutoTools[toolName] {
		if path := extractPathFromArgs(toolName, args); path != "" {
			if !pm.IsPathWithinAllowedRoots(path) {
				return false, "target path outside allowed roots: " + path
			}
			if isSensitivePath(path) {
				return false, "target touches sensitive path: " + path
			}
		}
	}
	return true, ""
}

// parsePermissionVerdict extracts an ALLOW/DENY decision from the permission
// model's final message. The prompt asks for a bare "ALLOW: <reason>" /
// "DENY: <reason>", but weaker models prepend reasoning or wrap the verdict in
// markdown (e.g. "**ALLOW: ...**"), so a strict whole-string prefix match misses
// real verdicts and falls through to a needless human prompt. We scan lines from
// last to first (the final stated verdict wins, matching final-answer
// convention and failing safe to the earlier line only when the last is not a
// verdict), strip leading markdown/quote decoration, and accept a line only when
// the verdict word is the whole line or is immediately followed by ':'. The
// colon requirement prevents prose like "ALLOW only if trusted" from flipping a
// decision. decided=false means no verdict was found (caller treats as ambiguous
// → deny).
func parsePermissionVerdict(text string) (decided, allow bool, reason string) {
	lines := strings.Split(text, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := stripVerdictLabel(strings.TrimLeft(strings.TrimSpace(lines[i]), " \t>*#-`\"'"))
		upper := strings.ToUpper(line)
		if rest, ok := strings.CutPrefix(upper, "ALLOW"); ok && verdictBoundary(rest) {
			return true, true, cleanVerdictReason(line[len("ALLOW"):])
		}
		if rest, ok := strings.CutPrefix(upper, "DENY"); ok && verdictBoundary(rest) {
			return true, false, cleanVerdictReason(line[len("DENY"):])
		}
	}
	// Fallback: weak models sometimes bury the verdict mid-sentence on the final
	// line (e.g. "Based on the above, I would DENY this operation because:").
	// The boundary-anchored loop above intentionally rejects that to stop prose
	// like "ALLOW only if trusted" from flipping a decision, so only as a last
	// resort do we scan the final non-empty line for a standalone verdict word.
	// This is a SECURITY-SENSITIVE recovery path that infers intent from prose, so
	// it is DENY-ONLY: a buried DENY fails closed (we honour it), but a buried
	// ALLOW is NOT auto-granted — it returns decided=false so the caller defers to
	// a human prompt. That keeps the recovery benefit for denials without ever
	// auto-allowing on ambiguous prose (e.g. "I would ALLOW this, but only after
	// the user confirms"), whose conditionals the negation set cannot fully cover.
	// Even the buried DENY is honoured only when DENY appears alone and the line
	// carries no negation token ("I would NOT DENY", "cannot DENY") that inverts it.
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		upper := strings.ToUpper(line)
		hasAllow := containsWord(upper, "ALLOW")
		hasDeny := containsWord(upper, "DENY")
		if hasDeny && !hasAllow && !hasNegation(upper) {
			return true, false, ""
		}
		break
	}
	return false, false, ""
}

// hasNegation reports whether an upper-cased line contains a token that could
// invert a buried verdict, so the prose-inference fallback can fail closed
// rather than read "I would NOT ALLOW this" as an allow.
func hasNegation(upper string) bool {
	if strings.Contains(upper, "N'T") || strings.Contains(upper, "ONLY IF") {
		return true
	}
	for _, w := range []string{"NOT", "NEVER", "CANNOT", "REFUSE", "UNLESS", "OTHERWISE", "DECLINE", "AVOID", "PROHIBIT"} {
		if containsWord(upper, w) {
			return true
		}
	}
	return false
}

// containsWord reports whether word appears in s as a standalone token (bounded
// by non-letters), so "ALLOW" matches "I would ALLOW this" but not "ALLOWED".
func containsWord(s, word string) bool {
	for i := 0; i+len(word) <= len(s); i++ {
		if s[i:i+len(word)] != word {
			continue
		}
		if i > 0 && isAsciiLetter(s[i-1]) {
			continue
		}
		if j := i + len(word); j < len(s) && isAsciiLetter(s[j]) {
			continue
		}
		return true
	}
	return false
}

func isAsciiLetter(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// stripVerdictLabel removes a leading prose label some models prepend to their
// verdict (e.g. "Answer: ALLOW: ...", "Verdict: DENY") so the verdict word lands
// at the start of the line for the prefix match. Only a single known label is
// stripped, and any leading markdown/quote chars exposed after it are re-trimmed.
func stripVerdictLabel(line string) string {
	for _, label := range []string{"answer:", "verdict:", "decision:", "final answer:", "final verdict:"} {
		if len(line) >= len(label) && strings.EqualFold(line[:len(label)], label) {
			return strings.TrimLeft(strings.TrimSpace(line[len(label):]), " \t>*#-`\"'")
		}
	}
	return line
}

// verdictBoundary reports whether the text following a verdict word marks it as a
// standalone verdict: the word is the entire line, or it is immediately followed
// by a ':' (the instructed "ALLOW: <reason>" form). This rejects longer words
// like "ALLOWED"/"ALLOWING" and loose prose like "ALLOW only if ...".
func verdictBoundary(rest string) bool {
	// Tolerate a markdown-emphasis run that closes between the verdict word and
	// its colon (e.g. "**ALLOW**:" → rest is "**: ..."). Leading-junk trimming in
	// the caller only strips the opening "**"; the closing run lands here.
	r := strings.TrimLeft(strings.TrimSpace(rest), "*`_")
	r = strings.TrimSpace(r)
	return r == "" || strings.HasPrefix(r, ":")
}

// cleanVerdictReason trims the verdict word's trailing reason of its leading
// ": " separator and any wrapping markdown/quote characters.
func cleanVerdictReason(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimLeft(s, ": *`_")
	s = strings.TrimRight(s, " *`\"'")
	return strings.TrimSpace(s)
}

// permissionReadFileTool returns the read_file tool definition given to the
// permission model so it can inspect the codebase before deciding.
func permissionReadFileTool() map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        "read_file",
			"description": "Read the contents of a file, or list a directory's entries if the path is a directory. Use this to explore the codebase before making a permission decision.",
			"parameters": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Path to the file to read (relative to working directory or absolute)",
					},
					"start_line": map[string]interface{}{
						"type":        "integer",
						"description": "1-based line to start reading from (default: 1)",
					},
					"end_line": map[string]interface{}{
						"type":        "integer",
						"description": "1-based last line to read inclusive (default: start_line + 49)",
					},
				},
				"required": []string{"path"},
			},
		},
	}
}

// runPermissionModelLoop drives the read_file tool-call loop (capped at
// maxToolCalls) and returns the model's final, no-tool-call message text.
// gotFinal is false on transport error, nil response, or budget exhaustion, in
// which case failReason carries the explanation. Shared by the plain ALLOW/DENY
// path and the interpreter structured-effects path.
func runPermissionModelLoop(stopCh <-chan struct{}, client LLMClient, messages []Message, tools []map[string]interface{}, modelLabel, toolName string) (finalText string, gotFinal bool, failReason string) {
	const maxToolCalls = 15
	// Derive a context cancelled when the agent is stopped (Esc) so an in-flight
	// permission-model request is aborted instead of blocking the agent for up to
	// maxToolCalls × the client timeout — which the user experiences as an
	// un-cancellable freeze.
	ctx, cancel := stopChContext(stopCh)
	defer cancel()
	// ctxChatter is the subset of clients that honour context cancellation. The
	// production *GenericClient implements it; test fakes implement only Chat and
	// take the plain (uncancellable) call. This is a capability check, not error
	// suppression.
	type ctxChatter interface {
		ChatWithContext(ctx context.Context, messages []Message, tools []map[string]interface{}) (*Message, error)
	}
	for i := 0; i < maxToolCalls; i++ {
		select {
		case <-stopCh:
			emitDebug("PERMISSION", fmt.Sprintf("tier=auto_llm_cancelled tool=%s model=%s attempt=%d", toolName, modelLabel, i))
			return "", false, "cancelled"
		default:
		}
		emitDebug("PERMISSION", fmt.Sprintf("tier=auto_llm_call tool=%s model=%s attempt=%d", toolName, modelLabel, i))

		// On the final attempt, withhold the read_file tool so the model is
		// forced to emit a verdict instead of wasting the turn on another read.
		callTools := tools
		if i == maxToolCalls-1 {
			callTools = nil
		}
		var (
			resp *Message
			err  error
		)
		if cc, ok := client.(ctxChatter); ok {
			resp, err = cc.ChatWithContext(ctx, messages, callTools)
		} else {
			resp, err = client.Chat(messages, callTools)
		}
		if err != nil {
			emitDebug("PERMISSION", fmt.Sprintf("tier=auto_llm_error tool=%s model=%s error=%v", toolName, modelLabel, err))
			return "", false, fmt.Sprintf("LLM error: %v", err)
		}
		if resp == nil {
			return "", false, "nil response from LLM"
		}

		if len(resp.ToolCalls) > 0 {
			messages = append(messages, *resp)
			for _, tc := range resp.ToolCalls {
				emitDebug("PERMISSION", fmt.Sprintf("tier=auto_llm_tool_call tool=%s model=%s function=%s args=%s", toolName, modelLabel, tc.Function.Name, truncateDebugArgs([]byte(tc.Function.Arguments), 200)))

				var toolResult string
				if tc.Function.Name == "read_file" {
					var params struct {
						Path      string `json:"path"`
						StartLine int    `json:"start_line"`
						EndLine   int    `json:"end_line"`
					}
					if err := json.Unmarshal([]byte(tc.Function.Arguments), &params); err != nil {
						toolResult = fmt.Sprintf("error: invalid arguments: %v", err)
					} else {
						toolResult = executePermissionReadFile(params.Path, params.StartLine, params.EndLine)
					}
				} else {
					toolResult = fmt.Sprintf("error: unknown tool %q", tc.Function.Name)
				}

				if len(toolResult) > 4000 {
					toolResult = toolResult[:4000] + "\n...(truncated)"
				}
				emitDebug("PERMISSION", fmt.Sprintf("tier=auto_llm_tool_result tool=%s model=%s function=%s result=%s", toolName, modelLabel, tc.Function.Name, truncateDebugArgs([]byte(toolResult), 200)))
				messages = append(messages, Message{Role: "tool", ToolID: tc.ID, Content: toolResult})
			}
			continue
		}

		emitDebug("PERMISSION", fmt.Sprintf("tier=auto_llm_response tool=%s model=%s response=%s", toolName, modelLabel, truncateDebugArgs([]byte(resp.Content), 200)))
		return resp.Content, true, ""
	}

	emitDebug("PERMISSION", fmt.Sprintf("tier=auto_llm_budget_exhausted tool=%s model=%s", toolName, modelLabel))
	return "", false, "LLM exhausted tool call budget without decision"
}

// executePermissionReadFile reads a file for the permission model LLM.
// It resolves the path, reads the requested lines, and returns the content.
func executePermissionReadFile(path string, startLine, endLine int) string {
	// Resolve relative paths against working directory.
	if !filepath.IsAbs(path) {
		if wd, err := os.Getwd(); err == nil {
			path = filepath.Join(wd, path)
		}
	}

	// A directory target is normal when the request is itself a directory
	// operation (list/glob/grep/repo_overview). os.ReadFile would return an
	// opaque "is a directory" error that weaker models misread as a validation
	// failure and refuse on. Return a directory listing instead so the model
	// gets usable context, and state plainly that this is the expected shape.
	if info, statErr := os.Stat(path); statErr == nil && info.IsDir() {
		return permissionListDir(path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		// A missing target is normal when the command/tool is creating the file
		// (e.g. `cat > new.go <<EOF`, write to a new path). Signal this explicitly
		// so the model does not treat "file not found" as a reason to refuse.
		if os.IsNotExist(err) {
			return fmt.Sprintf("%s does not exist yet — this is expected when the operation creates the file. Decide based on the command/arguments, not the missing file.", path)
		}
		return fmt.Sprintf("error reading %s: %v", path, err)
	}

	lines := strings.Split(string(data), "\n")
	total := len(lines)

	if startLine <= 0 {
		startLine = 1
	}
	if endLine <= 0 {
		endLine = startLine + 49
	}
	if startLine > total {
		return fmt.Sprintf("(file has %d lines, start_line=%d is out of range)", total, startLine)
	}
	if endLine > total {
		endLine = total
	}

	var sb strings.Builder
	for i := startLine; i <= endLine; i++ {
		fmt.Fprintf(&sb, "%d\t%s\n", i, lines[i-1])
	}
	if endLine < total {
		fmt.Fprintf(&sb, "... (%d more lines)\n", total-endLine)
	}
	return sb.String()
}

// permissionListDir returns a sorted directory listing for the permission model
// when read_file is pointed at a directory. Listing a directory is the expected
// operation for list/glob/grep/repo_overview tools, so this returns the entries
// (capped) plus an explicit note that a directory target is not a reason to
// refuse — the model should decide from the path and tool, not from "it's a dir".
func permissionListDir(path string) string {
	entries, err := os.ReadDir(path)
	if err != nil {
		return fmt.Sprintf("error listing directory %s: %v", path, err)
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			name += "/"
		}
		names = append(names, name)
	}
	sort.Strings(names)

	const maxEntries = 100
	truncated := 0
	if len(names) > maxEntries {
		truncated = len(names) - maxEntries
		names = names[:maxEntries]
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "%s is a directory — listing a directory is the expected target for list/glob/grep operations, not a reason to refuse. Decide from the path and tool.\n\nEntries (%d):\n", path, len(entries))
	for _, n := range names {
		sb.WriteString("  " + n + "\n")
	}
	if truncated > 0 {
		fmt.Fprintf(&sb, "  ... (%d more)\n", truncated)
	}
	return sb.String()
}

// buildPermissionContext gathers relevant code context for the permission model
// to make informed decisions. It reads target files, identifies the project type,
// and explains bash commands -- all within the configured byte/source/line limits.
func (a *Agent) buildPermissionContext(toolName string, args json.RawMessage, maxCtxBytes, maxSources, maxLinesPerSource int) string {
	var b strings.Builder
	usedBytes := 0
	sourcesAdded := 0

	addSection := func(label, content string) bool {
		if sourcesAdded >= maxSources || usedBytes+len(content)+len(label)+4 > maxCtxBytes {
			return false
		}
		b.WriteString(label + "\n")
		b.WriteString(content + "\n\n")
		usedBytes += len(label) + len(content) + 4
		sourcesAdded++
		return true
	}

	// 1. Working directory and project type.
	if wd, err := os.Getwd(); err == nil {
		addSection("Working directory:", wd)
	}

	// Allowed filesystem roots — the authoritative scope boundary. Anything
	// outside these roots is out-of-scope and must not be auto-allowed.
	if a.permissions != nil {
		if roots := a.permissions.AllowedRoots(); len(roots) > 0 {
			addSection("Allowed filesystem roots (anything outside is OUT OF SCOPE):", strings.Join(roots, "\n"))
		}
	}

	projectType := detectProjectType()
	if projectType != "" {
		addSection("Project type:", projectType)
	}

	// 2. File context for file-based tools.
	if pathScopedTools[toolName] {
		var params struct {
			Path     string `json:"path"`
			FilePath string `json:"file_path"`
			Pattern  string `json:"pattern"`
			URL      string `json:"url"`
		}
		if err := json.Unmarshal(args, &params); err == nil {
			targetPath := params.Path
			if targetPath == "" {
				targetPath = params.FilePath
			}
			if targetPath != "" {
				if content, totalLines, err := readFileSnippet(targetPath, maxLinesPerSource); err == nil {
					label := fmt.Sprintf("Target file: %s (%d lines total, showing first %d):", targetPath, totalLines, maxLinesPerSource)
					addSection(label, content)
				} else if !os.IsNotExist(err) {
					addSection(fmt.Sprintf("Target file: %s", targetPath), fmt.Sprintf("(unreadable: %v)", err))
				} else {
					addSection(fmt.Sprintf("Target file: %s", targetPath), "(file does not exist yet -- new file creation)")
				}
				if dir := filepath.Dir(targetPath); dir != "." {
					if listing, err := listDirectory(dir, 15); err == nil {
						addSection(fmt.Sprintf("Directory %s:", dir), listing)
					}
				}
			}
		}
	}

	// 3. Bash command context.
	if toolName == "bash" {
		var params struct {
			Command string `json:"command"`
		}
		if err := json.Unmarshal(args, &params); err == nil && params.Command != "" {
			explanation := explainBashCommand(params.Command)
			addSection("Command analysis:", explanation)
			for _, file := range extractFilesFromCommand(params.Command) {
				if sourcesAdded >= maxSources {
					break
				}
				if content, totalLines, err := readFileSnippet(file, maxLinesPerSource); err == nil {
					addSection(fmt.Sprintf("Referenced file: %s (%d lines):", file, totalLines), content)
				}
			}
		}
	}

	// 4. Webfetch context.
	if toolName == "webfetch" {
		var params struct {
			URL string `json:"url"`
		}
		if err := json.Unmarshal(args, &params); err == nil && params.URL != "" {
			if parsed, err := url.Parse(params.URL); err == nil {
				addSection("Fetch target:", fmt.Sprintf("Domain: %s\nPath: %s", parsed.Hostname(), parsed.Path))
			}
		}
	}

	if b.Len() == 0 {
		return "(no context available)"
	}
	return b.String()
}

func detectProjectType() string {
	for _, manifest := range []struct {
		file string
		kind string
	}{
		{"go.mod", "Go module"},
		{"package.json", "Node.js project"},
		{"Cargo.toml", "Rust project"},
		{"pyproject.toml", "Python project"},
		{"requirements.txt", "Python project"},
		{"Gemfile", "Ruby project"},
		{"pom.xml", "Java/Maven project"},
		{"build.gradle", "Java/Gradle project"},
		{"pubspec.yaml", "Dart/Flutter project"},
	} {
		if data, err := os.ReadFile(manifest.file); err == nil {
			lines := strings.SplitN(string(data), "\n", 3)
			if len(lines) > 0 {
				return fmt.Sprintf("%s (%s)", manifest.kind, strings.TrimSpace(lines[0]))
			}
			return manifest.kind
		}
	}
	return ""
}

func readFileSnippet(path string, maxLines int) (string, int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", 0, err
	}
	lines := strings.Split(string(data), "\n")
	total := len(lines)
	if total > maxLines {
		lines = lines[:maxLines]
	}
	return strings.Join(lines, "\n"), total, nil
}

func listDirectory(dir string, maxEntries int) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	count := 0
	for _, e := range entries {
		if count >= maxEntries {
			fmt.Fprintf(&b, "... (%d more entries)\n", len(entries)-maxEntries)
			break
		}
		prefix := "  "
		if e.IsDir() {
			prefix = "d "
		}
		fmt.Fprintf(&b, "%s%s\n", prefix, e.Name())
		count++
	}
	return b.String(), nil
}

func explainBashCommand(command string) string {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return "(empty command)"
	}
	prefix := fields[0]
	explanations := map[string]string{
		"ls":     "List directory contents",
		"cat":    "Display file contents",
		"grep":   "Search for patterns in files",
		"find":   "Find files matching criteria",
		"head":   "Show first lines of a file",
		"tail":   "Show last lines of a file",
		"wc":     "Count lines/words/characters",
		"diff":   "Compare files line by line",
		"sort":   "Sort lines of text",
		"sed":    "Stream editor (text transformation)",
		"jq":     "JSON processor",
		"curl":   "HTTP client (can upload data!)",
		"wget":   "HTTP downloader (can upload data!)",
		"git":    "Version control",
		"go":     "Go toolchain",
		"cargo":  "Rust toolchain",
		"npm":    "Node.js package manager",
		"npx":    "Node.js package runner (execute packages without installing)",
		"pnpm":   "Node.js package manager",
		"yarn":   "Node.js package manager",
		"bun":    "JavaScript runtime & package manager",
		"docker": "Container management",
		"make":   "Build automation",
		"rm":     "Delete files (DESTRUCTIVE!)",
		"sudo":   "Execute as root (DANGEROUS!)",
		"echo":   "Print text to stdout",
		"mkdir":  "Create directories",
	}
	if explanation, ok := explanations[prefix]; ok {
		if prefix == "git" && len(fields) >= 2 {
			sub := fields[1]
			gitSubs := map[string]string{
				"status":   "Show working tree status (read-only)",
				"diff":     "Show changes (read-only)",
				"log":      "Show commit history (read-only)",
				"show":     "Show commit details (read-only)",
				"blame":    "Show line-by-line annotations (read-only)",
				"branch":   "List/create branches",
				"checkout": "Switch branches or restore files (can discard changes!)",
				"reset":    "Reset HEAD (DESTRUCTIVE!)",
				"revert":   "Undo commits (rewrites history!)",
				"clean":    "Remove untracked files (DESTRUCTIVE!)",
				"stash":    "Stash/unstash changes",
				"push":     "Push to remote",
				"pull":     "Pull from remote",
				"merge":    "Merge branches",
				"add":      "Stage files",
				"commit":   "Create a commit",
			}
			if desc, ok := gitSubs[sub]; ok {
				return fmt.Sprintf("git %s: %s", sub, desc)
			}
		}
		return explanation
	}
	return fmt.Sprintf("Execute '%s' (unknown command)", prefix)
}

func extractFilesFromCommand(command string) []string {
	fields := strings.Fields(command)
	var files []string
	for _, f := range fields {
		if strings.HasPrefix(f, "-") || f == "|" || f == ">" || f == ">>" || f == "<" || f == "&&" || f == ";" || f == "||" {
			continue
		}
		if f == "git" || f == "go" || f == "cargo" || f == "npm" || f == "npx" || f == "pnpm" || f == "yarn" || f == "bun" || f == "docker" || f == "make" || f == "curl" || f == "wget" || f == "echo" || f == "cd" || f == "pwd" {
			continue
		}
		if strings.Contains(f, ".") || strings.Contains(f, "/") {
			if !strings.HasPrefix(f, "http://") && !strings.HasPrefix(f, "https://") {
				files = append(files, f)
			}
		}
	}
	return files
}

func (a *Agent) HandleApprovedToolCall(name string, args json.RawMessage) (string, error) {
	return a.executeToolCall(name, args)
}

func (a *Agent) executeToolCall(name string, args json.RawMessage) (string, error) {
	emitDebug("TOOL", fmt.Sprintf("→ %s %s", name, truncateDebugArgs(args, 120)))
	if !a.isToolAllowed(name) {
		return fmt.Sprintf("denied: tool %q is not allowed for this agent. Do not retry; use a different tool or approach.", name), nil
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

	// Resolve any OCSEC tokens in tool arguments back to original values
	// This allows tools to work with the real secrets after LLM processing
	if a.redactionRegistry != nil {
		resolved := a.redactionRegistry.Resolve(string(args))
		args = json.RawMessage(resolved)
	}

	if a.pipeline != nil {
		args = a.pipeline.RunToolBefore(name, args)
	}

	result, err := t.Execute(args)

	if hooksCfg != nil {
		resultStr := ""
		if err == nil {
			resultStr = result
		}
		_ = hooks.RunPostHook(name, argsStr, resultStr, hooksCfg)
	}

	if a.pipeline != nil && err == nil {
		result = a.pipeline.RunToolAfter(name, result)
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
	if a.client == nil {
		return ""
	}
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
	if name == "advisor" {
		// Prefer the parent's reactive pointer so mid-run toggles propagate
		// immediately to sub-agents. Fall back to the agent's own static flag.
		enabled := a.advisorEnabled.Load()
		if a.parentAdvisorEnabled != nil {
			enabled = a.parentAdvisorEnabled.Load()
		}
		if !enabled {
			return false
		}
	}
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

// SetAdvisorEnabled toggles the advisor tool for this agent at runtime. It does
// not persist to config — the change lives only for the agent's lifetime.
func (a *Agent) SetAdvisorEnabled(enabled bool) { a.advisorEnabled.Store(enabled) }

// SetParentAdvisorEnabled wires this agent's advisor gate to the parent's
// atomic flag. When set, isToolAllowed and AdvisorEnabled dereference the
// parent pointer so mid-run toggles propagate immediately.
func (a *Agent) SetParentAdvisorEnabled(parent *atomic.Bool) {
	a.parentAdvisorEnabled = parent
}

// AdvisorEnabled reports whether the advisor tool is currently exposed.
// When a parent pointer is set, it reads from the parent (reactive);
// otherwise it reads the agent's own static flag.
func (a *Agent) AdvisorEnabled() bool {
	if a.parentAdvisorEnabled != nil {
		return a.parentAdvisorEnabled.Load()
	}
	return a.advisorEnabled.Load()
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

// SetMaxSteps overrides the runtime step limit. 0 or negative means unlimited
// (the step loop applies a default cap of 100).
func (a *Agent) SetMaxSteps(n int) {
	a.maxSteps = n
}

// GetMaxSteps returns the current runtime step limit. 0 means unlimited
// (default cap of 100 applies in the step loop).
func (a *Agent) GetMaxSteps() int {
	return a.maxSteps
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
			a.preloadedModelContext = "" // model may have changed; reload model-specific context lazily
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

// ProcCounter returns the high-water mark of background-process IDs issued by
// this agent's registry.
func (a *Agent) ProcCounter() int {
	if a.procs == nil {
		return 0
	}
	return a.procs.Counter()
}

// SeedProcCounter raises this agent's process-ID counter to at least n so newly
// started background processes continue past n rather than restarting at proc-1.
func (a *Agent) SeedProcCounter(n int) {
	if a.procs != nil {
		a.procs.SeedCounter(n)
	}
}

// SetSupervisorIDPrefix namespaces this agent's process IDs in the shared
// supervisor so subagents that inherit the parent supervisor do not collide
// on their independently-counted "proc-N" identifiers.
func (a *Agent) SetSupervisorIDPrefix(prefix string) {
	if a.procs != nil {
		a.procs.SetSupervisorIDPrefix(prefix)
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
// In-flight HTTP calls are interrupted via context cancellation in chatWithDelta.
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

// ResetCancellation creates a fresh stop channel so subsequent Step calls are
// not immediately short-circuited by a prior Cancel. Call this before spawning
// a new request goroutine when the previous stream was cancelled (e.g. Escape).
func (a *Agent) ResetCancellation() {
	a.stopMu.Lock()
	defer a.stopMu.Unlock()
	select {
	case <-a.stopCh:
		// Was cancelled — replace with a fresh, open channel.
		a.stopCh = make(chan struct{})
	default:
		// Not cancelled; nothing to do.
	}
}

// StopCh returns the current stop channel. Callers that need to check
// cancellation for the lifetime of a single operation should capture this
// once at the start of the operation so that a later ResetCancellation call
// does not affect their check.
func (a *Agent) StopCh() <-chan struct{} {
	a.stopMu.Lock()
	ch := a.stopCh
	a.stopMu.Unlock()
	return ch
}

// Done returns a channel closed when the agent is cancelled. Callers blocking
// on agent-scoped work (e.g. a permission-ask callback) can select on it to
// unblock cleanly on Shutdown/Cancel instead of leaking a goroutine.
func (a *Agent) Done() <-chan struct{} { return a.StopCh() }

// cancelled reports whether Cancel has been called on the current stop channel.
func (a *Agent) cancelled() bool {
	select {
	case <-a.StopCh():
		return true
	default:
		return false
	}
}

// stopChContext derives a context.Context that is cancelled when ch is closed.
// The caller must call the returned CancelFunc to release resources.
func stopChContext(ch <-chan struct{}) (context.Context, context.CancelFunc) {
	select {
	case <-ch:
		// Channel already closed — return a pre-cancelled context.
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		return ctx, cancel
	default:
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		defer cancel()
		select {
		case <-ch:
		case <-ctx.Done():
		}
	}()
	return ctx, cancel
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
		a.permissions.SetUserConfirmedRule(req.ToolName, PermissionAllow)
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
	a.permissions.SetUserConfirmedRule(req.ToolName, PermissionAllow)
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

// recoverOrphanedToolCalls re-executes tool calls whose results are missing.
//
// Only the LAST assistant message's orphans are re-executed — these represent
// a session persisted mid-tool-execution where the most recent turn had calls
// whose results were never saved. Historical orphans (earlier turns) are left
// for repairToolCallSequence to synthesise inert stubs, avoiding dangerous
// re-execution of side-effectful operations from prior turns.
//
// Re-executed results are inserted right after the assistant message, not at
// the end of the message list. The old behaviour appended everything at the
// end, placing tool results AFTER the new user message and confusing the LLM
// into responding with "done".
func (a *Agent) recoverOrphanedToolCalls(messages []Message) []Message {
	// Find the LAST assistant message with orphaned tool calls.
	candidateIdx := -1
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "assistant" && len(messages[i].ToolCalls) > 0 {
			candidateIdx = i
			break
		}
	}
	if candidateIdx < 0 {
		return messages
	}

	// Build existing result IDs.
	resultIDs := make(map[string]bool)
	for _, msg := range messages {
		if msg.Role == "tool" && msg.ToolID != "" {
			resultIDs[msg.ToolID] = true
		}
	}

	// Check if this assistant has orphaned calls.
	var orphans []ToolCall
	seen := make(map[string]bool)
	for _, tc := range messages[candidateIdx].ToolCalls {
		if tc.ID != "" && !resultIDs[tc.ID] && !seen[tc.ID] {
			orphans = append(orphans, tc)
			seen[tc.ID] = true
		}
	}
	if len(orphans) == 0 {
		return messages
	}

	emitDebug("RECOVER", fmt.Sprintf("re-executing %d orphaned tool call(s) from assistant at index %d", len(orphans), candidateIdx))

	// Re-execute each orphan and insert results right after the assistant.
	insertAt := candidateIdx + 1
	for insertAt < len(messages) && messages[insertAt].Role == "tool" {
		insertAt++ // skip existing tool results for other calls in this batch
	}

	out := make([]Message, 0, len(messages)+len(orphans))
	out = append(out, messages[:insertAt]...)

	for _, tc := range orphans {
		result, err := a.HandleToolCall(tc.Function.Name, json.RawMessage(tc.Function.Arguments))
		if err != nil {
			result = fmt.Sprintf("ORPHAN_TOOL_ERROR:%s:%v\n%s", tc.Function.Name, err, result)
		}
		result = TruncateToolResult(tc.ID, result)
		out = append(out, Message{Role: "tool", ToolID: tc.ID, Content: result})
	}

	out = append(out, messages[insertAt:]...)
	return out
}

func truncateDebugArgs(args json.RawMessage, max int) string {
	s := string(args)
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}
