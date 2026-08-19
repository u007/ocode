package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync/atomic"
	"time"

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
- Start by issuing a single 'doc_search' with a broad query and 'get_top: 3' so the top matches return their full bodies inline — this avoids a separate 'doc_get' round-trip. Answer and cite document paths directly from the returned content.
- Only call 'doc_get' when you need a document beyond the top-N (use 'page'/'get_top' to retrieve more), must verify an exact claim against the verbatim text, or are about to write/update the bundle.
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
		Tools:        []string{"read", "glob", "grep", "list", "lsp", "bash", "webfetch", "websearch", "skill", "load_skill"},
	},
	{
		Name:         "scout",
		Description:  "External docs, dependency research",
		SystemPrompt: scoutSubAgentPrompt,
		Tools:        []string{"repo_clone", "repo_overview", "glob", "grep", "list", "read", "webfetch", "websearch", "skill", "load_skill"},
	},
	{
		Name:         "context",
		Description:  "knowledge curator and retriever for the project's OKF docs/ bundle — answers why/decision/playbook questions from curated docs, cites doc paths, sole automated writer of the bundle",
		SystemPrompt: contextSubAgentPrompt,
		Tools:        []string{"grep", "glob", "read", "list", "skill", "load_skill"},
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

	// contract is the resolved output contract for this dispatch
	// (call-supplied expected_output, else the agent definition's
	// expected_output frontmatter, else "" = no verification). Set on
	// the same short-lived per-call copy as groupBus/agentID; the
	// shared instance in a.tools["task"] leaves it empty.
	contract string
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
				"resume_task_id": map[string]interface{}{
					"type":        "string",
					"description": "Resume a previously cancelled or completed task instead of starting a new one. Set to the task_id of a run whose state is cancelled or done; prompt becomes the follow-up instruction and the sub-agent continues with its full prior conversation history.",
				},
				"expected_output": map[string]interface{}{
					"type":        "string",
					"description": "Optional output contract: a short natural-language description of the shape or content the result must have (e.g. \"the full list of affected files, one path per line\"). When set, the sub-agent's final result is verified against it before being returned, and retried once in place if it does not match. If omitted, the agent's own expected_output frontmatter (if any) applies; with neither, no verification is performed.",
				},
				"id": map[string]interface{}{
					"type":        "string",
					"description": "Optional caller-chosen label for this dispatch, unique within the parallel batch. Combined with depends_on this batch becomes a DAG: a node that names another node's id in depends_on will not start until the named predecessor completes successfully, and the predecessor's final result is prepended to the child's context. Omit both fields to take the legacy flat parallel fan-out path.",
				},
				"depends_on": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "string"},
					"description": "Optional list of ids (set on sibling task calls in the same parallel batch) that must complete successfully before this dispatch starts. Only honoured inside the parallel partition; an unknown id, duplicate id, or cycle is a hard error and no node in the affected component runs. Concurrency is bounded by the shared agent limiter — dependents acquire their slot after every predecessor resolves (they never block on predecessors inside the child, which would deadlock the limiter).",
				},
			},
			"required": []string{"prompt"},
		},
	}
}

type taskToolParams struct {
	Prompt          string   `json:"prompt"`
	Agent           string   `json:"agent"`
	SubagentType    string   `json:"subagent_type"`
	Context         string   `json:"context"`
	Description     string   `json:"description"`
	RunInBackground bool     `json:"run_in_background"`
	Background      bool     `json:"background"`
	SharedNotes     bool     `json:"shared_notes"`
	ResumeTaskID    string   `json:"resume_task_id"`
	ExpectedOutput  string   `json:"expected_output"`
	DAGID           string   `json:"id"`
	DAGDeps         []string `json:"depends_on"`
}

func (t TaskTool) Execute(args json.RawMessage) (string, error) {
	var params taskToolParams
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

	if params.ResumeTaskID != "" {
		return t.executeResume(params)
	}

	spec := t.findAgent(params.Agent)
	var fallbackWarning string
	if spec == nil {
		if params.Agent != "" {
			fallbackWarning = fmt.Sprintf("⚠ Agent %q not found; fell back to built-in general agent.\n\n", params.Agent)
			t.mainAgent.emitDebug("TASK", fmt.Sprintf("agent %q not found, falling back to general", params.Agent))
		}
		defaultSpec := t.findAgent("general")
		if defaultSpec == nil {
			return "", fmt.Errorf("no agent available")
		}
		spec = defaultSpec
	}

	// Resolve the output contract: call-supplied expected_output wins,
	// otherwise the agent definition's expected_output frontmatter. Empty
	// from both sources means no verification — the unchanged, zero-cost
	// path.
	t.contract = resolveContract(params.ExpectedOutput, spec.ExpectedOutput)

	// Self-dispatch guard: an agent may never dispatch its own type (e.g.
	// explore spawning explore, review-changes spawning review-changes).
	// Unlike the re-dispatch guard below (which only trips after repeated
	// identical launches), this has no natural termination condition — each
	// spawned instance could itself dispatch another, recursing without
	// bound — so it is refused outright, even on the first attempt.
	if t.mainAgent != nil && t.mainAgent.spec != nil && spec.Name == t.mainAgent.spec.Name {
		return fmt.Sprintf("Error: agent %q cannot dispatch another instance of itself — this can recurse indefinitely. Do the work directly or dispatch a different agent type.", spec.Name), nil
	}

	// Hidden agents (e.g. the orchestrator pipeline's internal
	// orchestrator-planner/-explorer/-developer/-validator subagents) are
	// scoped to their own plugin: only a caller loaded from the same
	// directory as the target may dispatch it. This stops the main LLM, or
	// any agent outside that plugin, from invoking an internal-only agent
	// directly via the task tool.
	if spec.Hidden {
		if !t.callerCanDispatch(spec) {
			return "", fmt.Errorf("agent %q is internal-only and cannot be dispatched from here", spec.Name)
		}
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
			t.mainAgent.emitDebug("KNOWLEDGE", fmt.Sprintf("context agent dispatched without doc tools: %v", err))
		}
	}

	subAgent := NewAgent(t.mainAgent.client, tools, t.mainAgent.config, t.mainAgent.lspMgr)
	// Draw concurrency slots from the same pool as the dispatcher instead of
	// NewAgent's fresh per-agent default. Without this, max_concurrent_agents
	// only caps direct dispatches at each nesting level independently, and
	// the true number of agents active at once can multiply with nesting
	// depth (e.g. 2 top-level subagents each dispatching 2 more = 4 active).
	// Must happen before subAgent's Step loop can call Acquire, i.e. before
	// this function returns control to subAgent.
	if t.runs != nil && subAgent.runs != nil {
		subAgent.runs.ShareLimiterFrom(t.runs)
	}
	// Share the parent's snapshot store so file writes flow to the same
	// store that the TUI sidebar reads from. Without this, every sub-agent
	// creates its own isolated store (via NewAgent → NewStore), and the
	// sidebar — which reads from the main agent's store — would never
	// reflect sub-agent file changes.
	subAgent.snapshotStore = t.mainAgent.snapshotStore

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
			t.mainAgent.emitDebug("NOTEBUS", fmt.Sprintf("agent %s status=%s err=%v", agentID, status, err))
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
		subAgent.OnMessage = func(msg Message) {
			run.appendTranscript(msg)
			// Forward to the parent session's own OnMessage/OnDelta (wired by the
			// TUI/server to publish "thinking"/"text" for the live chat stream).
			// Without this, sub-agent runs only ever reached run.appendTranscript
			// (the separate runs/task panel) and never appeared in the main chat.
			if t.mainAgent != nil && t.mainAgent.OnMessage != nil {
				t.mainAgent.OnMessage(msg)
			}
		}
		subAgent.OnDelta = func(kind, text string) {
			if t.mainAgent != nil && t.mainAgent.OnDelta != nil {
				t.mainAgent.OnDelta(kind, text)
			}
		}
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

		return t.runBackgroundDispatch(spec.Name, subAgent, run, subAgentMsgs, "started"), nil
	}

	// Synchronous mode.
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

	result, err := t.runSyncDispatch(spec.Name, subAgent, run, subAgentMsgs)
	if err != nil {
		return "", err
	}
	return fallbackWarning + result, nil
}

// resolveContract picks the effective output contract for a dispatch:
// call-supplied expected_output wins over the agent definition's
// expected_output frontmatter. Empty from both sources means no
// verification — the unchanged, zero-cost path.
func resolveContract(callValue, defValue string) string {
	if callValue != "" {
		return callValue
	}
	return defValue
}

// resumeEligibleRun looks up taskID and validates it can be resumed: it must
// exist, be owned by the calling agent (same comparison as
// AgentRunRegistry.CancelOwned), and be in RunCancelled or RunDone. Returns
// the run and its subagent-type name (run.Name) on success.
func (t TaskTool) resumeEligibleRun(taskID string) (*AgentRun, string, error) {
	if t.runs == nil {
		return nil, "", fmt.Errorf("no agent run registry")
	}
	run, ok := t.runs.Get(taskID)
	if !ok {
		return nil, "", fmt.Errorf("unknown task: %s", taskID)
	}
	dispatcher := ""
	if t.mainAgent != nil && t.mainAgent.spec != nil {
		dispatcher = t.mainAgent.spec.Name
	}
	if run.Dispatcher != dispatcher {
		return nil, "", fmt.Errorf("task %s is not owned by %q", taskID, dispatcher)
	}
	status := run.statusValue()
	if status != RunCancelled && status != RunDone {
		return nil, "", fmt.Errorf("task %s cannot be resumed from state %q (only cancelled or done tasks can be resumed)", taskID, status)
	}
	if run.Sub == nil {
		return nil, "", fmt.Errorf("task %s has no resumable sub-agent state", taskID)
	}
	return run, run.Name, nil
}

// executeResume continues a cancelled-or-completed subagent run with a new
// prompt. It reuses the original *Agent (run.Sub) and its full prior
// transcript instead of constructing a fresh subagent, so tools, permissions,
// and conversation history all carry over unchanged.
func (t TaskTool) executeResume(params taskToolParams) (string, error) {
	run, specName, err := t.resumeEligibleRun(params.ResumeTaskID)
	if err != nil {
		return "", err
	}
	subAgent := run.Sub

	// Resolve the output contract for the resumed dispatch the same way a
	// fresh dispatch does: call-supplied expected_output, else the agent
	// definition's expected_output frontmatter, else none.
	defContract := ""
	if def := t.findAgent(specName); def != nil {
		defContract = def.ExpectedOutput
	}
	t.contract = resolveContract(params.ExpectedOutput, defContract)

	// Re-dispatch guard: refuse repeated identical resumes without any
	// intervening user input, same protection fresh dispatch has (see
	// subagentDispatchLimit).
	if t.mainAgent != nil {
		if count := t.mainAgent.NoteSubagentDispatch(specName); count > subagentDispatchLimit {
			return fmt.Sprintf("Error: refusing to resume subagent %q — it has been launched %d times in a row without any new user input. This usually means the conversation is in a feedback loop. Wait for the user to provide new direction before retrying.", specName, count), nil
		}
	}

	// Status can flip to terminal (finishOK/finishErr/tryFinishCancelled)
	// before the dispatch goroutine's deferred shutdownTransient() has
	// actually run — resumeEligibleRun above only checked Status, so wait for
	// that teardown to genuinely finish before touching subAgent again.
	// Otherwise RearmMaintenance below could race a still-running
	// shutdownTransient over the same maintenance-worker fields. Must happen
	// BEFORE beginResume, which replaces teardownDone for the new cycle.
	run.awaitTeardown(10 * time.Second)

	if !run.beginResume() {
		return fmt.Sprintf("Error: task %s changed state and can no longer be resumed (currently %s).", params.ResumeTaskID, run.statusValue()), nil
	}

	// Build the follow-up message(s) the same way a fresh dispatch builds its
	// initial ones (context -> optional system message, prompt -> user
	// message), then append them onto the run's existing transcript — which
	// already holds the full prior conversation, seeded at the original
	// dispatch and streamed via subAgent.OnMessage as it ran. The resulting
	// TranscriptPublic() is exactly the message slice Step needs, since
	// Agent.Step is stateless and retains no history of its own between
	// calls.
	var resumeMsgs []Message
	if params.Context != "" {
		resumeMsgs = append(resumeMsgs, Message{
			Role:    "system",
			Content: "Background Context: " + params.Context,
		})
	}
	resumeMsgs = append(resumeMsgs, Message{Role: "user", Content: params.Prompt})
	for _, msg := range resumeMsgs {
		run.appendTranscript(msg)
	}
	messages := run.TranscriptPublic()

	// Undo shutdownTransient(): reopen stopCh and restart the maintenance
	// workers it tore down when this run first went terminal.
	subAgent.RearmMaintenance()

	if params.RunInBackground {
		return t.runBackgroundDispatch(specName, subAgent, run, messages, "resumed"), nil
	}
	return t.runSyncDispatch(specName, subAgent, run, messages)
}

// runBackgroundDispatch launches subAgent asynchronously against messages,
// advancing run through the queued/running/terminal lifecycle exactly like a
// fresh background task dispatch. Shared by fresh dispatch and task resume;
// verb only affects the human-readable "state" line in the returned summary
// ("started" for a fresh dispatch, "resumed" for a resume).
func (t TaskTool) runBackgroundDispatch(specName string, subAgent *Agent, run *AgentRun, messages []Message, verb string) string {
	// Only surface a Queued state when a concurrency limit is actually
	// configured — otherwise Acquire below is a no-op and the run goes
	// straight to Running, same as before this limiter existed.
	limited := t.runs.MaxConcurrent() > 0
	if limited {
		run.markQueued()
	}
	var stopCh <-chan struct{}
	if t.mainAgent != nil {
		stopCh = t.mainAgent.StopCh()
	}
	runs := t.runs

	go func() {
		// Tear down the transient sub-agent's goroutines once this background
		// run reaches a terminal state. The AgentRun record (transcript +
		// result + run.Sub for ModelLabel) is retained so status/resume still
		// work, but the two maintenance-worker goroutines no longer leak. This
		// mirrors the synchronous path's shutdownTransient; it must only run
		// AFTER completion — never while the agent is still running, or it
		// would Cancel() and abort the run. run.markTeardownDone() runs after
		// shutdownTransient() (not concurrently with it) since both are in the
		// same deferred closure — a resume waiting on awaitTeardown only
		// unblocks once teardown has actually finished, closing the race where
		// Status already looks terminal (set above via finishOK/finishErr,
		// which return before this deferred func runs) but the maintenance
		// goroutines RearmMaintenance would reinitialize are still live.
		defer func() {
			subAgent.shutdownTransient()
			run.markTeardownDone()
		}()

		// Wait for a concurrency slot (no-op when unlimited). If the session
		// is cancelled while this dispatch is still queued, stopCh closes and
		// we bail without ever starting the sub-agent.
		release, aerr := runs.AcquireForRun(run, stopCh)
		if aerr != nil {
			run.tryFinishCancelled()
			runs.notifyDone(run)
			return
		}
		// Own the slot via subAgent (not a bare defer) so that if subAgent
		// itself dispatches further nested "task" calls, it can temporarily
		// hand this slot back to the shared pool instead of self-deadlocking
		// (see Agent.pauseOwnSlotForNestedCall).
		subAgent.setOwnSlot(release)
		defer subAgent.releaseOwnSlot()
		// task_cancel or session shutdown may have cancelled this specific run
		// while it was queued (CancelOwned marks Queued runs Cancelled too);
		// honor that instead of starting work the caller already gave up on.
		// Re-check the stop channel after Acquire as well: cancellation can race
		// with the final slot admission, including when the limit is unlimited.
		if run.statusValue() == RunCancelled || isClosed(stopCh) {
			run.tryFinishCancelled()
			runs.notifyDone(run)
			return
		}
		if !run.beginExecution() {
			runs.notifyDone(run)
			return
		}

		result, resp, err := t.executeSubAgentWithTranscript(specName, subAgent, messages)
		if err != nil {
			run.finishErr(err.Error())
			runs.notifyDone(run)
			return
		}
		// Output-contract verification + single in-place retry, inside the
		// dispatch goroutine after the child ran and before the terminal
		// status is published. The deferred shutdownTransient (above) only
		// fires on goroutine exit, so the child is still live here; a polled
		// "done" therefore means "done and checked". The verdict is recorded
		// on the run before finishOK so agent_status/task_status surface it.
		result, _, _ = t.verifyAndRetryContract(specName, subAgent, messages, resp, result, run)
		run.finishOK(result)
		runs.notifyDone(run)
	}()

	state := "running"
	if limited {
		state = "queued"
	}
	return fmt.Sprintf("task_id: %s (agent: %s)\nstate: %s\n\n<task_result>\nBackground task %s. Poll with task_status or agent_status.\n</task_result>", run.ID, specName, state, verb)
}

// runSyncDispatch blocks until subAgent finishes executing messages, updating
// run (if non-nil) through the queued/running/terminal lifecycle exactly like
// a fresh synchronous task dispatch. Shared by fresh dispatch and task
// resume.
func (t TaskTool) runSyncDispatch(specName string, subAgent *Agent, run *AgentRun, messages []Message) (string, error) {
	// t.mainAgent is about to block on subAgent's full execution, so it isn't
	// doing concurrent work itself for the duration — hand its own concurrency
	// slot (if it holds one) back to the shared pool for the duration of this
	// call, otherwise a low max_concurrent_agents limit would deadlock against
	// t.mainAgent's own blocked ancestors on any synchronous nested dispatch.
	resumeMainSlot := t.mainAgent.pauseOwnSlotForNestedCall()
	defer resumeMainSlot()

	if run != nil {
		if t.runs.MaxConcurrent() > 0 {
			run.markQueued()
		}
		var stopCh <-chan struct{}
		if t.mainAgent != nil {
			stopCh = t.mainAgent.StopCh()
		}
		release, aerr := t.runs.AcquireForRun(run, stopCh)
		if aerr != nil {
			run.tryFinishCancelled()
			return "", aerr
		}
		if run.statusValue() == RunCancelled || isClosed(stopCh) {
			release()
			run.tryFinishCancelled()
			return "", fmt.Errorf("task cancelled while queued")
		}
		if !run.beginExecution() {
			release()
			return "", fmt.Errorf("task cancelled while queued")
		}
		subAgent.setOwnSlot(release)
		defer subAgent.releaseOwnSlot()
	}
	// Each synchronous sub-agent dispatch builds (or, for a resume, re-arms) two
	// maintenance-worker goroutines (memory + doc) that would otherwise leak
	// forever — memoryMaintCh is never closed by the caller and the sync path
	// never calls Shutdown. Tear the transient agent down once this call
	// returns so we don't accumulate one leaked Agent + goroutines per
	// knowledge_lookup / synchronous task. The sub-agent shares the parent's
	// snapshotStore, so shutdownTransient stops only the goroutines/loop and
	// must NOT Reset the shared store. Also signal run.markTeardownDone (when
	// run is tracked) so a later resume's awaitTeardown sees this cycle's
	// teardown as finished — see runBackgroundDispatch for why that matters.
	defer func() {
		subAgent.shutdownTransient()
		if run != nil {
			run.markTeardownDone()
		}
	}()
	result, resp, err := t.executeSubAgentWithTranscript(specName, subAgent, messages)
	if err != nil {
		if run != nil {
			run.finishErr(err.Error())
			t.runs.notifyDone(run)
		}
		return "", err
	}

	// Output-contract verification + single in-place retry. Must happen
	// HERE — inside runSyncDispatch, before the deferred shutdownTransient
	// fires — so the retry can step the still-live child. The retry is NOT
	// routed back through TaskTool.Execute (which would rebuild the child
	// and trip the re-dispatch guard), and the public resume_task_id path
	// is unusable mid-dispatch (resumeEligibleRun requires a terminal run).
	// resp is reassigned by the retry so the persisted child session below
	// reflects the final transcript.
	result, resp, _ = t.verifyAndRetryContract(specName, subAgent, messages, resp, result, run)

	sessionID := childSessionID("parent", specName)
	metadata := childSessionMetadata("parent", specName)
	if t.persistChildSess != nil {
		if err := t.persistChildSess(sessionID, fmt.Sprintf("Child: %s", specName), resp, metadata); err != nil {
			t.mainAgent.emitDebug("SESSION", fmt.Sprintf("failed to persist child session: %v", err))
		}
	}

	if run != nil {
		run.finishOK(result)
		t.runs.notifyDone(run)
	}
	if sessionID != "" {
		result += fmt.Sprintf("\n\n(Child session: %s)", sessionID)
	}
	return result, nil
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

// callerCanDispatch reports whether the agent that owns this TaskTool
// instance (t.mainAgent) is allowed to dispatch the given hidden target.
// A hidden agent may only be dispatched by a caller defined in the same
// source directory (i.e. the same markdown-agent plugin) — this keeps
// pipeline-internal agents like orchestrator-developer reachable only from
// their own orchestrator.md, not from the main LLM or unrelated agents.
func (t TaskTool) callerCanDispatch(target *AgentDefinition) bool {
	if t.registry == nil || t.mainAgent == nil || t.mainAgent.spec == nil {
		return false
	}
	callerDef := t.registry.Get(t.mainAgent.spec.Name)
	if callerDef == nil {
		return false
	}
	return filepath.Dir(callerDef.Source) == filepath.Dir(target.Source)
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

// mainAgentOnlyTools lists tools that only the main agent may invoke. The
// store-load-bearing rule: subagents must never write to the todo file —
// only the main agent owns the plan. Subagents may still call todoread.
var mainAgentOnlyTools = map[string]bool{
	"todowrite":   true,
	"todo_update": true,
}

func (t TaskTool) getToolsForDef(spec *AgentDefinition) []tool.Tool {
	if len(spec.Tools) == 0 {
		return filterMainOnlyTools(t.mainAgent.GetTools())
	}
	allTools := filterMainOnlyTools(t.mainAgent.GetTools())
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

func filterMainOnlyTools(tools []tool.Tool) []tool.Tool {
	result := make([]tool.Tool, 0, len(tools))
	for _, t := range tools {
		if mainAgentOnlyTools[t.Name()] {
			continue
		}
		result = append(result, t)
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
	// Output-contract verdict, surfaced for any run that carried one. A
	// finished-but-contract-failed run is "done" with a caveat — the parent
	// must not read done as correct.
	if checked, satisfied, deficiency := run.ContractVerdict(); checked {
		if satisfied {
			b.WriteString("\nContract: satisfied")
		} else {
			b.WriteString("\nContract: NOT satisfied")
			if strings.TrimSpace(deficiency) != "" {
				b.WriteString(" — " + strings.TrimSpace(deficiency))
			}
		}
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
		text := run.Result
		if checked, satisfied, deficiency := run.ContractVerdict(); checked && !satisfied {
			text = "Contract NOT satisfied" + contractDeficiencySuffix(deficiency) + "\n\n" + text
		}
		return formatTaskStatus(taskID, "completed", text)
	case RunFailed:
		return formatTaskStatus(taskID, "error", run.Err)
	default:
		return formatTaskStatus(taskID, string(status), "")
	}
}

func contractDeficiencySuffix(deficiency string) string {
	if strings.TrimSpace(deficiency) == "" {
		return ""
	}
	return " — " + strings.TrimSpace(deficiency)
}

func formatTaskStatus(taskID, state, text string) string {
	tag := "task_result"
	if state == "error" || state == "failed" || state == "cancelled" {
		tag = "task_error"
	}
	return fmt.Sprintf("task_id: %s\nstate: %s\n\n<%s>\n%s\n</%s>", taskID, state, tag, text, tag)
}
