package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/u007/ocode/internal/knowledge"
)

const (
	docMaintChannelCap      = 64
	docMaintTimeoutSeconds  = 120
	docMaintContextMessages = 8
)

// DocMaintenanceRequest is enqueued after a job completes so the agent can
// decide whether the OKF knowledge bundle should be updated.
type DocMaintenanceRequest struct {
	WorkDir        string
	RecentMessages []Message
	Forced         bool   // true when triggered by /docs update
	Focus          string // optional focus topic for forced runs
}

type docMaintTriageAction struct {
	Action string `json:"action"` // "create", "update", "deprecate"
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

type docMaintTriageResult struct {
	Decision string                 `json:"decision"` // "noop" or "actions"
	Actions  []docMaintTriageAction `json:"actions"`
}

// QueueDocMaintenance schedules a maintenance pass after a completed job.
// Non-blocking; drops when the buffer is full, when shutting down, or when
// the channel is closed, and logs a debug message.
//
// Thread-safe against concurrent docMaintShutdown (C4: OCSEC:31f59a:2).
func (a *Agent) QueueDocMaintenance(req DocMaintenanceRequest) {
	if a == nil || a.docMaintCh == nil || !a.DocPromptEnabled() {
		return
	}
	if strings.TrimSpace(req.WorkDir) == "" {
		req.WorkDir, _ = os.Getwd()
	}

	// Check the bundle marker is present.
	if _, ok := knowledge.DetectBundle(req.WorkDir); !ok {
		return
	}

	a.docMaintMu.Lock()
	defer a.docMaintMu.Unlock()

	if a.docMaintClosing {
		emitDebug("DOCMAINT", "shutting down, dropped maintenance request")
		return
	}

	select {
	case a.docMaintCh <- req:
	default:
		emitDebug("DOCMAINT", "doc maintenance channel full, dropped request")
	}
}

func (a *Agent) docMaintenanceWorker() {
	defer close(a.docMaintDone)
	for req := range a.docMaintCh {
		// Drop queued items after shutdown has been initiated. The channel close
		// drains remaining items, but we must not process them (C5).
		a.docMaintMu.Lock()
		closing := a.docMaintClosing
		a.docMaintMu.Unlock()
		if closing {
			emitDebug("DOCMAINT", "shutting down, dropping queued maintenance request")
			continue
		}
		a.runDocMaintenance(req)
	}
}

func (a *Agent) runDocMaintenance(req DocMaintenanceRequest) {
	if a == nil || !a.DocPromptEnabled() {
		return
	}
	if strings.TrimSpace(req.WorkDir) == "" {
		req.WorkDir, _ = os.Getwd()
	}

	// Check bundle marker is still present.
	bundle, ok := knowledge.DetectBundle(req.WorkDir)
	if !ok {
		emitDebug("DOCMAINT", "no bundle marker found, skipping")
		return
	}

	client := a.docMaintenanceClient()
	if client == nil {
		emitDebug("DOCMAINT", "skipped: no small-model client available")
		return
	}

	// Read the current index for the triage prompt.
	indexPath := bundle.Root + "/index.md"
	indexContent := ""
	if data, err := os.ReadFile(indexPath); err == nil {
		indexContent = string(data)
	}

	// Pass 1: Triage — small model decides whether to act.
	prompt := a.buildDocMaintTriagePrompt(req, indexContent)
	ctx, cancel := context.WithTimeout(context.Background(), docMaintTimeoutSeconds*time.Second)
	defer cancel()

	respCh := make(chan struct {
		msg *Message
		err error
	}, 1)
	go func() {
		msg, err := client.Chat([]Message{{Role: "system", Content: prompt}}, nil)
		respCh <- struct {
			msg *Message
			err error
		}{msg: msg, err: err}
	}()

	var decision docMaintTriageResult
	select {
	case <-ctx.Done():
		emitDebug("DOCMAINT", fmt.Sprintf("triage timed out: %v", ctx.Err()))
		return
	case out := <-respCh:
		if out.err != nil {
			emitDebug("DOCMAINT", fmt.Sprintf("triage failed: %v", out.err))
			return
		}
		if out.msg == nil {
			emitDebug("DOCMAINT", "triage returned empty response")
			return
		}
		a.RecordSideUsageFromMessage(out.msg)
		// Small models routinely fence JSON responses with ```json / ```,
		// so strip those before unmarshalling (M4).
		clean := stripJSONFences(out.msg.Content)
		if err := json.Unmarshal([]byte(clean), &decision); err != nil {
			emitDebug("DOCMAINT", fmt.Sprintf("triage parse error: %v — raw: %s", err, out.msg.Content))
			return
		}
	}

	if decision.Decision == "noop" || len(decision.Actions) == 0 {
		emitDebug("DOCMAINT", "triage decided noop")
		return
	}

	// Pass 2: Execute — dispatch the context agent with the triage plan.
	emitDebug("DOCMAINT", fmt.Sprintf("executing %d triage actions", len(decision.Actions)))
	spec := FindSubAgentSpec("context")
	if spec == nil {
		emitDebug("DOCMAINT", "context subagent not found")
		return
	}

	// Build action list for the context agent.
	var actionLines []string
	for _, act := range decision.Actions {
		actionLines = append(actionLines, fmt.Sprintf("- **%s** `%s`: %s", act.Action, act.Path, act.Reason))
	}
	actionText := strings.Join(actionLines, "\n")

	// Dispatch the context agent via the task tool.
	taskTool, ok := a.GetTool("task")
	if !ok {
		emitDebug("DOCMAINT", "task tool not available for execution")
		return
	}
	task, ok := taskTool.(*TaskTool)
	if !ok {
		emitDebug("DOCMAINT", "task tool has unexpected type")
		return
	}

	execPrompt := fmt.Sprintf(`Execute the following doc maintenance actions on the OKF knowledge bundle. 

Triage plan:
%s

For each action:
1. Check existing docs with doc_search/doc_get before making changes.
2. For "create": use doc_write to create the new doc.
3. For "update": use doc_write to update the existing doc, preserving any content.
4. For "deprecate": use doc_deprecate with the given reason.
5. Never delete. Deprecate instead.
6. After all actions, summarise what was done.`, actionText)

	result, err := task.ExecuteRaw("context", execPrompt, false)
	if err != nil {
		emitDebug("DOCMAINT", fmt.Sprintf("execution failed: %v", err))
		return
	}
	emitDebug("DOCMAINT", fmt.Sprintf("execution complete: %s", result[:min(len(result), 200)]))
}

func (a *Agent) docMaintenanceClient() LLMClient {
	if a == nil {
		return nil
	}
	if a.config == nil {
		return a.client
	}
	if !a.config.Ocode.SmallModelEnabled {
		return a.client
	}
	model := strings.TrimSpace(a.config.Ocode.SmallModel)
	if model == "" {
		model = ResolveSmallModel(a.config)
	}
	if model == "" {
		return a.client
	}
	if client := newClientFn(a.config, model); client != nil {
		return client
	}
	return a.client
}

func (a *Agent) buildDocMaintTriagePrompt(req DocMaintenanceRequest, indexContent string) string {
	var b strings.Builder
	b.WriteString("You are a triage system for the project's OKF (Open Knowledge Format) knowledge bundle.\n")
	b.WriteString("Analyze the recent conversation and decide whether any durable knowledge (decisions, gotchas, playbooks, schema/architecture changes) was produced that should be written to the bundle.\n\n")

	if req.Forced {
		b.WriteString("This is a forced maintenance pass triggered by /docs update.\n")
		if req.Focus != "" {
			b.WriteString(fmt.Sprintf("Focus topic: %s\n", req.Focus))
		}
		b.WriteString("Review the bundle for staleness. Look for docs that contradict current code, duplicate content, or are no longer accurate.\n\n")
	} else {
		b.WriteString(`Gate rules (strict):
- Only durable knowledge triggers actions: decisions made, gotchas discovered, playbooks executed, schema/architecture changes.
- Q&A, routine edits, and chat-only discussion are NOOPs.
- If in doubt, choose noop.
`)
	}

	b.WriteString("\nCurrent bundle index:\n")
	b.WriteString(strings.TrimSpace(indexContent))
	b.WriteString("\n\n")

	b.WriteString("Recent conversation:\n")
	for i, msg := range req.RecentMessages {
		if i >= docMaintContextMessages {
			break
		}
		role := msg.Role
		content := msg.Content
		if len(content) > 2000 {
			content = content[:2000] + "...\n[truncated]"
		}
		b.WriteString(fmt.Sprintf("\n--- %s ---\n%s", role, content))
	}

	b.WriteString(`

Respond with ONLY valid JSON, one of these two shapes:

{"decision":"noop"}

{"decision":"actions","actions":[{"action":"create|update|deprecate","path":"bundle-relative-path.md","reason":"why this action is needed"}]}

The "path" must be bundle-relative (e.g. "decisions/api-design.md"). Never suggest deleting. Never include paths outside docs/.
`)
	return b.String()
}

// docMaintShutdown signals the worker to stop, closes the channel, and waits
// for the worker to exit. It is called during Agent.Shutdown().
//
// Thread-safe: uses docMaintMu to coordinate with QueueDocMaintenance (C4).
// After return, no maintenance request will be processed and no writes will
// land (C5).
func (a *Agent) docMaintShutdown() {
	if a.docMaintCh == nil {
		return
	}
	a.docMaintShutdownOnce.Do(func() {
		// Set the closing flag and close the channel under the mutex so
		// concurrent QueueDocMaintenance callers see a consistent state
		// and do not send on a closed channel.
		a.docMaintMu.Lock()
		a.docMaintClosing = true
		close(a.docMaintCh)
		remaining := len(a.docMaintCh)
		a.docMaintMu.Unlock()

		if remaining > 0 {
			emitDebug("DOCMAINT", fmt.Sprintf("shutdown: %d pending maintenance requests will be dropped", remaining))
		}

		// Wait for the worker to finish its in-flight item (if any) and exit.
		// After this point, no doc writes can land. Use a bounded wait — the
		// worker cannot block agent teardown forever.
		select {
		case <-a.docMaintDone:
		case <-time.After(5 * time.Second):
			emitDebug("DOCMAINT", "shutdown: worker did not exit within 5s, proceeding")
		}
	})
}

// stripJSONFences removes markdown code fence markers (```json / ```) that
// small models routinely wrap around JSON responses (M4).
func stripJSONFences(raw string) string {
	s := strings.TrimSpace(raw)
	// Remove leading ```json or ```
	if strings.HasPrefix(s, "```json") {
		s = strings.TrimPrefix(s, "```json")
	} else if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```")
	}
	// Remove trailing ```
	if strings.HasSuffix(s, "```") {
		s = strings.TrimSuffix(s, "```")
	}
	return strings.TrimSpace(s)
}
