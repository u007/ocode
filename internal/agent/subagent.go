package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime/debug"
	"strings"
	"sync/atomic"

	"github.com/u007/ocode/internal/notebus"
	"github.com/u007/ocode/internal/tool"
)

// subAgentSupervisorCounter assigns each subagent a unique namespace prefix
// for IDs registered in the shared parent supervisor, preventing collisions
// between independently-counted "proc-N" sequences across sibling subagents.
var subAgentSupervisorCounter atomic.Uint64

type SubAgentSpec struct {
	Name         string
	Description  string
	SystemPrompt string
	Tools        []string
}

const generalSubAgentPrompt = "You are a general-purpose sub-agent. Complete the task efficiently and return the final result. " +
	"Use a User Expectation Checklist for multi-step work, validate each checklist item with the strongest practical check available, and iterate if validation fails. " +
	"Be concise in your output and include checklist status, validation performed, and remaining gaps."

const exploreSubAgentPrompt = `You are explore, a read-only codebase navigator. Your job is to locate code and answer "where/how/what" questions about THIS repository — never modify it.

Approach:
- Start broad with glob to map the area (e.g. "src/**/*.tsx", "**/auth*.go"), then narrow with grep for symbols, strings, or callsites.
- Use list to understand directory structure when paths aren't given.
- Use read for known files; prefer reading the smallest relevant excerpt over whole files.
- Use lsp for symbol definitions, references, and type info when grep alone is ambiguous (overloads, generics, re-exports).
- Use read-only bash sparingly — only when it materially improves discovery (e.g. git log/blame, jq on a JSON manifest). Never run commands that touch the network, install, or write files.

Thoroughness levels the caller may specify:
- quick: one targeted lookup, single best match.
- medium: a handful of related queries to triangulate.
- very thorough: cover multiple naming conventions, plural/singular, common synonyms, and adjacent layers.

Output:
- Be concise. Lead with the answer.
- Cite file:line for every claim that names a symbol or path.
- Address each of the caller's user expectations explicitly — one bullet per expectation.
- End with a "remaining unknowns" section listing anything you could not verify within scope.
- Do not propose fixes or write design discussion; you are a research agent.`

const contextSubAgentPrompt = `You are context, the knowledge curator for this project's OKF (Open Knowledge Format) bundle under docs/. Your job is to answer "why/what did we decide/do we have a playbook for X" questions from curated docs, and to be the sole automated writer of the bundle.

Approach:
- Start by checking the bundle index (doc_search with a broad query) to find relevant documents before reading code.
- Verify doc claims against code before answering or writing — use grep/glob/read to cross-reference.
- Write only through the doc tools (doc_write, doc_deprecate). Never edit docs/ files directly.
- Prefer updating an existing document over creating a near-duplicate.
- Deprecate rather than delete — set status=deprecated with a reason.
- When the knowledge system is not initialized, say so and suggest /docs init.

Output:
- Lead with the answer, citing document paths.
- When writing, summarise what changed and why in one paragraph.`

const scoutSubAgentPrompt = `You are scout, a read-only research agent for code OUTSIDE this workspace — external libraries, dependency source, vendor docs, and reference repositories.

Use the right source for the question:
- repo_clone + repo_overview when the question is about a specific library's source (architecture, API surface, internal behavior). Prefer cloning over reading a published doc when behavior may have changed.
- webfetch for a known URL (release notes, RFCs, API reference pages).
- websearch when you need to discover the right URL first.
- glob/grep/list/read against cloned external repos for the same reasons as explore.

Discipline:
- Do not modify the user's workspace. All writes must go to the scout cache / clone area provided by repo_clone.
- Distinguish verified source-of-truth (code, official docs, RFCs) from inference, third-party blogs, or LLM-generated guides. Cite the strongest source available.
- Quote short, relevant excerpts with their URL or repo-relative path. Avoid pasting long unrelated context.
- Note version/tag/commit when behavior is version-dependent.

Output:
- Lead with the answer.
- Cite source URLs and repo paths for every claim.
- Address each of the caller's user expectations explicitly.
- End with a "remaining unknowns" section: what you could not verify, what would require running code, what version constraints you assumed.`

var DefaultSubAgents = []SubAgentSpec{
	{
		Name:         "general",
		Description:  "Multi-step tasks, parallel work",
		SystemPrompt: generalSubAgentPrompt,
	},
	{
		Name:         "explore",
		Description:  "Fast read-only codebase exploration",
		SystemPrompt: exploreSubAgentPrompt,
		Tools:        []string{"read", "glob", "grep", "list", "lsp", "bash", "webfetch", "websearch"},
	},
	{
		Name:         "scout",
		Description:  "External docs, dependency research",
		SystemPrompt: scoutSubAgentPrompt,
		Tools:        []string{"repo_clone", "repo_overview", "glob", "grep", "list", "read", "webfetch", "websearch"},
	},
	{
		Name:         "context",
		Description:  "knowledge curator and retriever for the project's OKF docs/ bundle — answers why/decision/playbook questions from curated docs, cites doc paths, sole automated writer of the bundle",
		SystemPrompt: contextSubAgentPrompt,
		Tools:        []string{"grep", "glob", "read", "list"},
	},
}

// enumNames returns visible names for the JSON Schema enum, falling back to
// the full list (which may include hidden agents like "title"/"compaction")
// only if no visible subagents are registered — otherwise we'd ship an empty
// enum, which most schema validators reject.
func enumNames(visible, all []string) []string {
	if len(visible) > 0 {
		return visible
	}
	return all
}

func FindSubAgentSpec(name string) *SubAgentSpec {
	for i := range DefaultSubAgents {
		if DefaultSubAgents[i].Name == name {
			return &DefaultSubAgents[i]
		}
	}
	return nil
}

type TaskTool struct {
	mainAgent        *Agent
	registry         *AgentRegistry
	runs             *AgentRunRegistry
	persistChildSess func(sessionID, title string, messages []Message, metadata map[string]any) error

	// Per-call bus + agent id. These are only ever set on a
	// short-lived COPY of the task tool, created per dispatch in
	// executeToolCall from a taskBinding; the shared instance in
	// a.tools["task"] always leaves them nil. A non-nil groupBus
	// means this call is part of a group and the child agent
	// should be wired onto the bus.
	groupBus *notebus.Bus
	agentID  string

	// Per-group completion tracker. Set on the same per-call copy
	// alongside groupBus/agentID; nil for solo / sequential calls.
	// The tracker is the reconcile (Part 05) input — it lists which
	// agents completed, which failed, and which partitions they
	// owned.
	groupTracker *groupTracker
}

func (t TaskTool) Name() string        { return "task" }
func (t TaskTool) Description() string { return "Delegate a task to a specialized sub-agent" }
func (t TaskTool) Parallel() bool      { return true }
func (t TaskTool) Definition() map[string]interface{} {
	subAgents := t.registrySubAgents()
	subAgentNames := make([]string, 0)
	subAgentDescs := make([]string, 0)
	visibleAgentNames := make([]string, 0)
	for _, sa := range subAgents {
		subAgentNames = append(subAgentNames, sa.Name)
		if !sa.Hidden {
			visibleAgentNames = append(visibleAgentNames, sa.Name)
			subAgentDescs = append(subAgentDescs, fmt.Sprintf("%s: %s", sa.Name, sa.Description))
		}
	}

	return map[string]interface{}{
		"name":        "task",
		"description": "Spawn a sub-agent with a specific scope to handle a task autonomously. Available agents: " + strings.Join(subAgentDescs, ", "),
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"prompt": map[string]interface{}{
					"type":        "string",
					"description": "The specific task or instructions for the sub-agent.",
				},
				"agent": map[string]interface{}{
					"type":        "string",
					"description": "The sub-agent type to use. Options: " + strings.Join(visibleAgentNames, ", "),
					"enum":        enumNames(visibleAgentNames, subAgentNames),
				},
				"subagent_type": map[string]interface{}{
					"type":        "string",
					"description": "OpenCode-compatible alias for agent. Options: " + strings.Join(visibleAgentNames, ", "),
					"enum":        enumNames(visibleAgentNames, subAgentNames),
				},
				"context": map[string]interface{}{
					"type":        "string",
					"description": "Additional background context relevant to the task.",
				},
				"description": map[string]interface{}{
					"type":        "string",
					"description": "OpenCode-compatible short description of the task.",
				},
				"run_in_background": map[string]interface{}{
					"type":        "boolean",
					"description": "If true, run the subagent in the background and return immediately with the run ID. Poll with agent_status or task_status.",
				},
				"background": map[string]interface{}{
					"type":        "boolean",
					"description": "OpenCode-compatible alias for run_in_background.",
				},
				"shared_notes": map[string]interface{}{
					"type":        "boolean",
					"description": "When true and the parallel batch contains 2+ subagent calls with this flag, the agent will share a notes bus across the group. Has no effect on a single (non-grouped) call.",
				},
			},
			"required": []string{"prompt"},
		},
	}
}

func (t TaskTool) Execute(args json.RawMessage) (string, error) {
	var params struct {
		Prompt          string `json:"prompt"`
		Agent           string `json:"agent"`
		SubagentType    string `json:"subagent_type"`
		Context         string `json:"context"`
		Description     string `json:"description"`
		RunInBackground bool   `json:"run_in_background"`
		Background      bool   `json:"background"`
		SharedNotes     bool   `json:"shared_notes"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	if params.Prompt == "" {
		return "", fmt.Errorf("prompt is required")
	}
	if params.Agent == "" {
		params.Agent = params.SubagentType
	}
	if params.Background {
		params.RunInBackground = true
	}

	spec := t.findAgent(params.Agent)
	var fallbackWarning string
	if spec == nil {
		if params.Agent != "" {
			fallbackWarning = fmt.Sprintf("⚠ Agent %q not found; fell back to built-in general agent.\n\n", params.Agent)
			emitDebug("TASK", fmt.Sprintf("agent %q not found, falling back to general", params.Agent))
		}
		defaultSpec := t.findAgent("general")
		if defaultSpec == nil {
			return "", fmt.Errorf("no agent available")
		}
		spec = defaultSpec
	}

	// Re-dispatch guard: refuse repeated identical subagent launches without
	// any intervening user input. Without this, a small model that interprets
	// every job-completion notification as a fresh request will loop forever
	// re-launching the same subagent.
	if t.mainAgent != nil {
		if count := t.mainAgent.NoteSubagentDispatch(spec.Name); count > subagentDispatchLimit {
			return fmt.Sprintf("Error: refusing to dispatch subagent %q — it has been launched %d times in a row without any new user input. This usually means the conversation is in a feedback loop. Wait for the user to provide new direction before retrying.", spec.Name, count), nil
		}
	}

	tools := t.getToolsForDef(spec)

	// Track doc tool names injected for the context subagent so we can
	// extend the spec's allowlist. Without this, isToolAllowed rejects
	// the injected tools because the context spec only lists grep/glob/
	// read/list (C1: OCSEC:31f59a:1).
	var injectedDocToolNames []string

	// Inject doc tools for the context subagent at dispatch time so the
	// main agent never gains write access to the bundle. If the bundle is
	// absent, dispatch without doc tools (the agent prompt tells it to say
	// the knowledge system is not initialized).
	if spec.Name == "context" {
		wd := t.mainAgent.workDir
		if wd == "" {
			wd, _ = os.Getwd()
		}
		if docTools, err := newDocTools(wd); err == nil {
			for _, dt := range docTools {
				tools = append(tools, dt)
				injectedDocToolNames = append(injectedDocToolNames, dt.Name())
			}
		} else {
			emitDebug("KNOWLEDGE", fmt.Sprintf("context agent dispatched without doc tools: %v", err))
		}
	}

	subAgent := NewAgent(t.mainAgent.client, tools, t.mainAgent.config, t.mainAgent.lspMgr)
	// Wire the sub-agent's advisor gate to the parent's atomic flag so
	// mid-run toggles propagate immediately (reactive, not a snapshot).
	subAgent.SetParentAdvisorEnabled(&t.mainAgent.advisorEnabled)
	// If this call is part of a notes group, hand the bus and the
	// per-call agent id to the child. Disabled/single calls leave
	// groupBus nil and the child runs without a bus — same as
	// before. The bus is set BEFORE the spec/permissions block so
	// the bus owns the child from the moment it exists.
	if t.groupBus != nil && t.agentID != "" {
		subAgent.SetNoteBus(t.groupBus, t.agentID)
		// Propagate completion status to the bus for
		// reconcile. If the parallel block attached a
		// tracker, also record into it (so the post-
		// teardown reconcile hand-off can surface
		// unreviewed partitions). The local "logged" flag
		// is for the debug log; the tracker records in
		// addition.
		subAgent.SetNoteBusCompletion(func(agentID, status string, err error) {
			emitDebug("NOTEBUS", fmt.Sprintf("agent %s status=%s err=%v", agentID, status, err))
			if t.groupTracker != nil {
				t.groupTracker.Record(agentID, status, err)
			}
		})
	}
	// Subagents do not inherit the parent's mode prompt — they have their own
	// system prompt. SetSpec installs the spec AND runs applySpecModel so any
	// Model / Temperature / TopP overrides on the registry definition actually
	// reach the subagent's client. Building the spec literal and assigning to
	// subAgent.spec directly would bypass applySpecModel and silently lose
	// those fields.
	subSpec := AgentSpec{
		Name:         spec.Name,
		Description:  spec.Description,
		SystemPrompt: spec.SystemPrompt,
		Tools:        spec.Tools,
		DeniedTools:  spec.DeniedTools,
		MaxSteps:     spec.MaxSteps,
		Model:        spec.Model,
		Color:        spec.Color,
		Temperature:  spec.Temperature,
		TopP:         spec.TopP,
	}
	// Extend the spec's allowlist with any doc tools injected for the
	// context agent (C1). Without this, isToolAllowed blocks them even
	// though they exist in the tools array — the spec's Tools field is
	// the source of truth for the allowlist filter.
	if len(injectedDocToolNames) > 0 {
		subSpec.Tools = append(spec.Tools, injectedDocToolNames...)
	}
	// Inject the small model for lightweight agents (explore, general, compaction)
	// when no explicit model override is present on the spec.
	injectSmallModelIfEligible(subAgent, &subSpec, t.mainAgent.config)
	subAgent.SetSpec(&subSpec)

	// Discovery: the sub-agent gets its OWN fresh sticky set (it does not inherit
	// the parent's). NewAgent drops MCP markers, so re-mark them from the parent
	// here; the sub-agent's Step() then ranks against params.Prompt itself.
	subAgent.markMCPFrom(t.mainAgent)

	// Inherit the shared session supervisor so subagent processes are tracked
	// under the same lifecycle owner as the main agent. Namespace this subagent's
	// supervisor IDs so its "proc-N" counter cannot collide with the parent's
	// or another subagent's identically-numbered process.
	subAgent.SetSupervisor(t.mainAgent.Supervisor())
	subAgent.SetSupervisorIDPrefix(fmt.Sprintf("sub-%d-", subAgentSupervisorCounter.Add(1)))

	// Propagate the permission-ask callback so sub-agent tool calls that need a
	// decision bubble up to the main TUI. Set before the spec-permissions block
	// so it applies whether or not the sub-agent gets its own PermissionManager.
	subAgent.OnPermissionAsk = t.mainAgent.subAgentPermAsker
	subAgent.OnPermissionGrant = t.mainAgent.OnPermissionGrant
	subAgent.SetSubAgentPermAsker(t.mainAgent.subAgentPermAsker)

	// Subagents share the parent (main thread) PermissionManager directly so
	// every grant — whether seeded at startup or accumulated mid-session via
	// "always allow" — is honored without an extra inheritance step. If the
	// agent spec carries its own permissions, layer them onto the shared
	// manager so they extend the main thread's allow-set ("additions to its
	// own allowed"); there is no separate subagent-scoped PermissionManager.
	if parentPerms := t.mainAgent.Permissions(); parentPerms != nil {
		subAgent.permissions = parentPerms
	}
	if subAgent.permissions == nil {
		subAgent.permissions = NewPermissionManager()
	}
	if len(spec.Permissions) > 0 {
		applyAgentPermissionsWithDiags(subAgent.permissions, spec.Permissions)
	}

	// spec.SystemPrompt is delivered via the prompt assembler (BasePromptMessages
	// picks it up from subAgent.spec). We only inject background context here as
	// a marker-less extra system message; the assembler will preserve it.
	var subAgentMsgs []Message
	if params.Context != "" {
		subAgentMsgs = append(subAgentMsgs, Message{
			Role:    "system",
			Content: "Background Context: " + params.Context,
		})
	}
	subAgentMsgs = append(subAgentMsgs, Message{Role: "user", Content: params.Prompt})

	attachRunTranscript := func(run *AgentRun) {
		if run == nil {
			return
		}
		// Seed the transcript with the prompt so the TUI drill-in has useful
		// context immediately, then stream every sub-agent assistant/tool message
		// into the run as Step progresses. The parent agent only sees the final
		// task tool result, so without this hook the live agent strip stays empty.
		for _, msg := range subAgentMsgs {
			run.appendTranscript(msg)
		}
		subAgent.OnMessage = func(msg Message) { run.appendTranscript(msg) }
		subAgent.OnUsage = func(in, out int64) { run.AddUsage(in, out) }
	}

	// Background mode
	if params.RunInBackground && t.runs != nil {
		run := t.runs.New(spec.Name)
		run.Background = true
		run.Procs = subAgent.Procs()
		run.Sub = subAgent
		run.Cancel = subAgent.Cancel
		if t.mainAgent != nil && t.mainAgent.spec != nil {
			run.Dispatcher = t.mainAgent.spec.Name
		}
		attachRunTranscript(run)

		go func() {
			result, err := t.executeSubAgent(spec.Name, subAgent, subAgentMsgs)
			if err != nil {
				run.finishErr(err.Error())
				t.runs.notifyDone(run)
				return
			}
			run.finishOK(result)
			t.runs.notifyDone(run)
		}()

		return fmt.Sprintf("task_id: %s (agent: %s)\nstate: running\n\n<task_result>\nBackground task started. Poll with task_status or agent_status.\n</task_result>", run.ID, spec.Name), nil
	}

	// Synchronous mode
	var run *AgentRun
	if t.runs != nil {
		run = t.runs.New(spec.Name)
		run.Procs = subAgent.Procs()
		run.Sub = subAgent
		run.Cancel = subAgent.Cancel
		if t.mainAgent != nil && t.mainAgent.spec != nil {
			run.Dispatcher = t.mainAgent.spec.Name
		}
		attachRunTranscript(run)
	}
	result, resp, err := t.executeSubAgentWithTranscript(spec.Name, subAgent, subAgentMsgs)
	if err != nil {
		if run != nil {
			run.finishErr(err.Error())
			t.runs.notifyDone(run)
		}
		return "", err
	}

	sessionID := childSessionID("parent", spec.Name)
	metadata := childSessionMetadata("parent", spec.Name)
	if t.persistChildSess != nil {
		if err := t.persistChildSess(sessionID, fmt.Sprintf("Child: %s", spec.Name), resp, metadata); err != nil {
			emitDebug("SESSION", fmt.Sprintf("failed to persist child session: %v", err))
		}
	}

	if run != nil {
		run.finishOK(result)
		t.runs.notifyDone(run)
	}
	if sessionID != "" {
		result += fmt.Sprintf("\n\n(Child session: %s)", sessionID)
	}
	return fallbackWarning + result, nil
}

func (t TaskTool) executeSubAgent(name string, subAgent *Agent, messages []Message) (string, error) {
	result, _, err := t.executeSubAgentWithTranscript(name, subAgent, messages)
	return result, err
}

func (t TaskTool) executeSubAgentWithTranscript(name string, subAgent *Agent, messages []Message) (result string, resp []Message, err error) {
	// Fire the bus completion callback exactly once, with the
	// final status. We always run (defer) so panic / cancellation
	// / error paths still report. The callback is a no-op when
	// the agent is not in a group.
	defer func() {
		if subAgent.noteBus != nil {
			status := "completed"
			if err != nil {
				status = "failed"
			}
			subAgent.noteBus.ReportCompletion(subAgent.noteAgentID, status, err)
		}
		if cb := subAgent.noteBusCompletion; cb != nil {
			status := "completed"
			if err != nil {
				status = "failed"
			}
			cb(subAgent.noteAgentID, status, err)
		}
	}()
	if t.mainAgent.activity != nil {
		t.mainAgent.activity.agentStarted(name)
		defer t.mainAgent.activity.agentDone(name)
	}
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("subagent %s stopped unexpectedly: %v\n%s", name, r, strings.TrimSpace(string(debug.Stack())))
			resp = nil
			result = ""
		}
	}()
	resp, err = subAgent.Step(messages)
	if err != nil {
		return "", nil, err
	}
	if t.mainAgent != nil {
		t.mainAgent.RecordSideUsageFromMessages(resp)
	}
	var b strings.Builder
	for _, m := range resp {
		if m.Role == "assistant" && m.Content != "" {
			b.WriteString(m.Content)
		}
	}
	return b.String(), resp, nil
}

func (t TaskTool) registrySubAgents() []AgentDefinition {
	if t.registry != nil {
		return t.registry.SubAgents()
	}
	var result []AgentDefinition
	for _, sa := range DefaultSubAgents {
		result = append(result, AgentDefinition{
			Name:         sa.Name,
			Description:  sa.Description,
			SystemPrompt: sa.SystemPrompt,
			Tools:        sa.Tools,
			Mode:         AgentModeSubagent,
			Source:       "builtin",
		})
	}
	return result
}

// ExecuteRaw dispatches a subagent by name synchronously with the given prompt.
// Used by the KnowledgeLookupTool for synchronous knowledge lookups.
func (t TaskTool) ExecuteRaw(agentName, prompt string, background bool) (string, error) {
	args, _ := json.Marshal(map[string]interface{}{
		"agent":             agentName,
		"prompt":            prompt,
		"run_in_background": background,
	})
	return t.Execute(args)
}

func (t TaskTool) findAgent(name string) *AgentDefinition {
	if t.registry != nil {
		return t.registry.Get(name)
	}
	spec := FindSubAgentSpec(name)
	if spec != nil {
		return &AgentDefinition{
			Name:         spec.Name,
			Description:  spec.Description,
			SystemPrompt: spec.SystemPrompt,
			Tools:        spec.Tools,
			Mode:         AgentModeSubagent,
			Source:       "builtin",
		}
	}
	return nil
}

func (t TaskTool) getToolsForDef(spec *AgentDefinition) []tool.Tool {
	if len(spec.Tools) == 0 {
		return t.mainAgent.GetTools()
	}
	allTools := t.mainAgent.GetTools()
	result := make([]tool.Tool, 0, len(spec.Tools))
	for _, mainTool := range allTools {
		for _, allowed := range spec.Tools {
			if mainTool.Name() == allowed {
				result = append(result, mainTool)
				break
			}
		}
	}
	return result
}

// AgentStatusTool returns the status of a background agent run.
type AgentStatusTool struct {
	runs *AgentRunRegistry
}

func (t AgentStatusTool) Name() string        { return "agent_status" }
func (t AgentStatusTool) Description() string { return "Check the status of a background agent run" }
func (t AgentStatusTool) Parallel() bool      { return true }
func (t AgentStatusTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "agent_status",
		"description": "Check the status and latest output of a background agent run.",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"id": map[string]interface{}{
					"type":        "string",
					"description": "The agent run id to check.",
				},
			},
			"required": []string{"id"},
		},
	}
}

func (t AgentStatusTool) Execute(args json.RawMessage) (string, error) {
	var params struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}
	if t.runs == nil {
		return "", fmt.Errorf("no agent run registry")
	}
	run, ok := t.runs.Get(params.ID)
	if !ok {
		return fmt.Sprintf("Error: unknown agent run %s", params.ID), nil
	}
	status := run.statusValue()
	var b strings.Builder
	b.WriteString(fmt.Sprintf("[agent run %s status=%s", params.ID, status))
	if status != RunRunning {
		b.WriteString(fmt.Sprintf(" agent=%s", run.Name))
	}
	b.WriteString("]")
	if status == RunRunning {
		lines := run.LastLines(5)
		if len(lines) > 0 {
			b.WriteString("\nLatest output:\n")
			for _, ln := range lines {
				b.WriteString("  ")
				b.WriteString(ln)
				b.WriteByte('\n')
			}
		} else {
			b.WriteString("\n(no output yet)")
		}
	}
	if status == RunDone {
		b.WriteString("\nResult: ")
		b.WriteString(run.Result)
	}
	if status == RunFailed {
		b.WriteString("\nError: ")
		b.WriteString(run.Err)
	}
	return b.String(), nil
}

// TaskStatusTool returns the status of a background agent run using the
// opencode-compatible task_status tool name.
type TaskStatusTool struct {
	runs *AgentRunRegistry
}

func (t TaskStatusTool) Name() string        { return "task_status" }
func (t TaskStatusTool) Description() string { return "Poll the status of a background subagent task" }
func (t TaskStatusTool) Parallel() bool      { return true }
func (t TaskStatusTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "task_status",
		"description": "Poll the status of a background subagent task launched with the task tool. Use this for tasks started with task(run_in_background=true). Returns the current task_id, state, and task_result/task_error blocks immediately; call again to keep polling.",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"task_id": map[string]interface{}{
					"type":        "string",
					"description": "The task_id returned by the task tool",
				},
			},
			"required": []string{"task_id"},
		},
	}
}

func (t TaskStatusTool) Execute(args json.RawMessage) (string, error) {
	var params struct {
		TaskID string `json:"task_id"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}
	if params.TaskID == "" {
		return "", fmt.Errorf("task_id is required")
	}
	if t.runs == nil {
		return "", fmt.Errorf("no agent run registry")
	}

	run, ok := t.runs.Get(params.TaskID)
	if !ok {
		return formatTaskStatus(params.TaskID, "error", fmt.Sprintf("unknown task %s", params.TaskID)), nil
	}
	return formatTaskRunStatus(params.TaskID, run), nil
}

func formatTaskRunStatus(taskID string, run *AgentRun) string {
	status := run.statusValue()
	switch status {
	case RunRunning:
		lines := run.LastLines(5)
		text := "Task is still running."
		if len(lines) > 0 {
			text = strings.Join(lines, "\n")
		}
		return formatTaskStatus(taskID, "running", text)
	case RunDone:
		return formatTaskStatus(taskID, "completed", run.Result)
	case RunFailed:
		return formatTaskStatus(taskID, "error", run.Err)
	default:
		return formatTaskStatus(taskID, string(status), "")
	}
}

func formatTaskStatus(taskID, state, text string) string {
	tag := "task_result"
	if state == "error" || state == "failed" || state == "cancelled" {
		tag = "task_error"
	}
	return fmt.Sprintf("task_id: %s\nstate: %s\n\n<%s>\n%s\n</%s>", taskID, state, tag, text, tag)
}
