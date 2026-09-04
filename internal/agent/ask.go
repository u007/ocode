package agent

import (
	"errors"
	"fmt"
	"os"
	"runtime/debug"
	"strings"
	"sync/atomic"

	"github.com/u007/ocode/internal/changes"
	"github.com/u007/ocode/internal/tool"
)

// btwAgentCounter namespaces each side-query agent's process IDs in the shared
// supervisor so a /btw loop's bash processes cannot collide with the main
// agent's or another side-query's identically-numbered processes.
var btwAgentCounter atomic.Uint64

// AskLoopOptions configures the independent side-query agent loop behind
// AskLoopAsync (the /btw popup).
type AskLoopOptions struct {
	// Tools exposed to the side-query agent. When nil, the side-query agent
	// gets the receiver's current tool set (a.GetTools).
	Tools []tool.Tool
	// ExcludedTools lists tool names to REMOVE from the side-query agent's
	// final tool map after construction. NewAgent unconditionally registers
	// the dispatch family (task, task_status, agent_status, task_cancel,
	// wait) plus advisor/knowledge_lookup regardless of the Tools slice, so a
	// caller that wants a lean side query must name them here. Applied after
	// NewAgent, so the model never sees them and cannot dispatch sub-agents
	// or mutate session-owned state from inside the popup loop.
	ExcludedTools []string
	// MaxSteps caps the agentic loop. 0 = default cap (8).
	MaxSteps int
	// Client, when set, is used directly as the side-query client (test seam
	// / advanced callers). When nil, a FRESH client is built from the main
	// agent's model id — the side query never shares the main agent's client,
	// so its OnDelta/OnUsage can never race the main turn's installed
	// callbacks. A nil result from the fresh-client build is surfaced as a
	// startup error, never a deferred connection failure.
	Client LLMClient
	// OnMessage, if set, is invoked for each assistant/tool message produced
	// during the loop (live tool activity for the popup). Runs on the loop
	// goroutine — keep it fast and non-blocking.
	OnMessage func(Message)
	// OnDelta, if set, streams text tokens (kind "text"/"reasoning") from the
	// side-query's own client. Safe to set because the child owns a fresh
	// client — its deltas can never leak into the main transcript.
	OnDelta func(kind, text string)
}

// AskLoopAsync runs an INDEPENDENT agent loop (a full Step with tool calls) on
// a dedicated child agent with its OWN fresh client, so the side query can
// never race the main agent's OnDelta/OnUsage on a shared client. The child:
//
//   - shares the parent's snapshot store and changes registry, so side-query
//     file edits and bash mutations land in the Changes tab and stay undoable
//   - shares the parent's PermissionManager, but PermissionAsk decisions are
//     denied non-blockingly (the popup cannot show dialogs, and a sentinel
//     would end the loop early with a half answer)
//   - inherits redaction (registry, enabled state, tier-1 net hook re-applied
//     onto its own client, tier-2 scanner)
//   - runs with discovery skipped and a step cap
//
// The final concatenated assistant content is delivered via onResult; the
// returned cancel func stops the loop and kills only the child's own
// processes (never the shared supervisor's).
func (a *Agent) AskLoopAsync(messages []Message, opts AskLoopOptions, onResult func(content string, err error)) (cancel func()) {
	if onResult == nil {
		return func() {}
	}
	if a == nil {
		onResult("", errors.New("no agent available"))
		return func() {}
	}

	child, err := a.newSideQueryAgent(opts)
	if err != nil {
		a.emitDebug("SIDEQUERY", "AskLoopAsync: "+err.Error())
		onResult("", err)
		return func() {}
	}

	cancel = func() {
		child.Cancel()
		// Kill only the child's own processes — never KillAll(), which would
		// terminate every process on the shared supervisor.
		for _, p := range child.procs.Snapshot() {
			_, _ = child.procs.Kill(p.ID)
		}
	}

	go func() {
		defer func() {
			if r := recover(); r != nil {
				stack := string(debug.Stack())
				a.emitDebug("SIDEQUERY", fmt.Sprintf("side-query loop panic: %v\n%s", r, stack))
				onResult("", fmt.Errorf("side-query loop panic: %v", r))
			}
		}()
		// Stop the maintenance workers (memory/doc) started by NewAgent; the
		// SHARED snapshot store is deliberately NOT reset (never Shutdown()).
		defer child.shutdownTransient()

		resp, err := child.Step(messages)
		if err != nil {
			onResult("", err)
			return
		}
		// Record usage through the MAIN agent so side-query spend lands in the
		// same usage ledger as everything else.
		a.RecordSideUsageFromMessages(resp)
		var b strings.Builder
		for _, m := range resp {
			if m.Role == "assistant" && m.Content != "" {
				b.WriteString(m.Content)
			}
		}
		onResult(b.String(), nil)
	}()
	return cancel
}

// newSideQueryAgent builds the dedicated child agent behind AskLoopAsync: a
// fresh client (never the main agent's — OnDelta/OnUsage isolation), the
// caller's tool set minus ExcludedTools (enforced on the FINAL map, since
// NewAgent unconditionally registers the dispatch family), shared
// snapshot/changes/permission state, redaction inheritance, and a namespaced
// supervisor. Split out of AskLoopAsync so tests can inspect the child's tool
// map directly. The returned child must be shut down with shutdownTransient
// (never Shutdown — the snapshot store is shared).
func (a *Agent) newSideQueryAgent(opts AskLoopOptions) (*Agent, error) {
	if a == nil {
		return nil, errors.New("no agent available")
	}

	// The child gets its OWN client. Derive the full model id from the main
	// agent's current client (provider + "/" + model round-trips through
	// NewClient; local-model ports and redaction are re-resolved/applied
	// below). Explicit opts.Client bypasses the build (test seam).
	client := opts.Client
	if client == nil {
		modelID := ""
		if a.client != nil {
			p := a.client.GetProvider()
			m := a.client.GetModel()
			if p != "" && m != "" {
				modelID = p + "/" + m
			} else if m != "" {
				modelID = m
			}
		}
		if modelID == "" && a.config != nil {
			modelID = a.config.Model
		}
		if modelID != "" && a.config != nil {
			client = NewClient(a.config, modelID)
		}
		if client == nil {
			return nil, fmt.Errorf("could not build a side-query client for %q", modelID)
		}
	}

	tools := opts.Tools
	if tools == nil {
		tools = a.GetTools()
	}
	maxSteps := opts.MaxSteps
	if maxSteps <= 0 {
		maxSteps = 8
	}

	child := NewAgent(client, tools, a.config, a.lspMgr)
	child.SetMaxSteps(maxSteps)
	child.skipDiscovery = true
	// Side-query agents belong to the same logical session. Share the root
	// advisor guard so a /btw query cannot run an advisor concurrently with the
	// main agent or a task child.
	child.SetParentAdvisorInFlight(a.advisorGuard())
	// Enforce the caller's exclusion list on the FINAL tool map: NewAgent
	// unconditionally registers wait/task/task_status/agent_status/task_cancel
	// (+ advisor/knowledge_lookup) regardless of the Tools slice, so a side
	// query that must not dispatch sub-agents or block on session state has to
	// delete them here — the slice filter alone is not enough.
	for _, name := range opts.ExcludedTools {
		delete(child.tools, name)
	}
	// Match the main agent's project root so the environment prompt and
	// path-scoped permission checks agree with the active /cd target. Direct
	// field assignment (not SetWorkDir) so the SHARED snapshot store's base
	// dir is not re-pointed.
	if a.workDir != "" {
		child.workDir = a.workDir
	}
	if a.sessionID != "" {
		child.SetSessionID(a.sessionID)
	} else if sessionID := a.OpenCodeSessionID(); sessionID != "" {
		child.SetOpenCodeSessionID(sessionID)
	}
	// Share session-scoped state so side-query mutations are tracked and
	// undoable in the Changes tab (same contract as subagents).
	if a.snapshotStore != nil {
		child.snapshotStore = a.snapshotStore
	}
	if a.changes != nil {
		child.changes = a.changes
		// Rebuild the child's bash tool so its stat-bash recorder reports into
		// the SHARED changes registry (the one the Changes tab reads), not the
		// throwaway registry NewAgent created for the child.
		workDir := a.workDir
		if workDir == "" {
			workDir, _ = os.Getwd()
		}
		if bt, ok := child.tools["bash"].(*tool.BashTool); ok {
			bt.Recorder = changes.NewStatBashRecorder(workDir, a.changes)
			child.tools["bash"] = bt
		}
	}
	// Share permissions; ASK decisions resolve to a non-blocking deny so the
	// popup loop never waits on a dialog it cannot show. Auto-permission (LLM
	// judge) still runs first when enabled.
	if a.permissions != nil {
		child.permissions = a.permissions
	}
	child.OnPermissionAsk = func(req PermissionRequest) PermissionResponse {
		return PermissionResponse{Level: PermissionDeny}
	}
	// Redaction inheritance: registry, enabled state, tier-1 net hook
	// (re-applied onto the child's OWN client), tier-2 scanner.
	child.redactionRegistry = a.redactionRegistry
	child.redactionEnabled = a.redactionEnabled
	child.redactionScanner = a.redactionScanner
	if a.redactionHook != nil {
		child.SetRedactionHook(a.redactionHook)
	}
	// Track child bash processes under the shared supervisor so they are
	// cleaned up on session teardown, namespaced so IDs cannot collide.
	if sup := a.Supervisor(); sup != nil {
		child.SetSupervisor(sup)
		child.SetSupervisorIDPrefix(fmt.Sprintf("btw-%d-", btwAgentCounter.Add(1)))
	}
	child.OnMessage = opts.OnMessage
	child.OnDelta = opts.OnDelta
	return child, nil
}
