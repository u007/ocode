package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/u007/ocode/internal/memory"
)

const (
	memoryMaintenanceHardLimitBytes  = 32 * 1024
	memoryMaintenancePromptBodyBytes = 12 * 1024
	memoryMaintenancePromptContext   = 8
	memoryMaintenanceTimeoutSeconds  = 60
)

// MemoryMaintenanceRequest is enqueued after a job completes so the agent can
// decide whether any persistent memory scope should be updated.
type MemoryMaintenanceRequest struct {
	WorkDir        string
	Job            JobEvent
	RecentMessages []Message
}

type memoryMaintenanceDecision struct {
	Action string `json:"action"`
	Scope  string `json:"scope"`
	Body   string `json:"body"`
	Reason string `json:"reason"`
}

type memoryScopeState struct {
	Name    string
	Path    string
	Body    string
	Size    int
	OverCap bool
}

var memoryScopeOrder = []struct {
	Name string
	Key  string
}{
	{Name: "Project memory", Key: "project"},
	{Name: "User preferences", Key: "user"},
	{Name: "Global history", Key: "global"},
}

// QueueMemoryMaintenance schedules a maintenance pass after a completed job.
// It is intentionally non-blocking for the caller; the worker serialises writes.
func (a *Agent) QueueMemoryMaintenance(req MemoryMaintenanceRequest) {
	if a == nil || a.memoryMaintCh == nil || !a.MemoryEnabled() || strings.TrimSpace(req.WorkDir) == "" {
		return
	}
	select {
	case a.memoryMaintCh <- req:
	default:
		go func() { a.memoryMaintCh <- req }()
	}
}

func (a *Agent) memoryMaintenanceWorker() {
	defer close(a.memoryMaintDone)
	for req := range a.memoryMaintCh {
		a.runMemoryMaintenance(req)
	}
}

// memoryMaintShutdown closes the memory-maintenance channel so the worker
// goroutine exits, then waits (bounded) for it to finish. Idempotent.
// Mirrors docMaintShutdown. The channel is never sent to by a discarded
// sub-agent (QueueMemoryMaintenance has no callers on sub-agents), so closing
// it cannot race a late send.
func (a *Agent) memoryMaintShutdown() {
	if a.memoryMaintCh == nil {
		return
	}
	a.memoryMaintShutdownOnce.Do(func() {
		close(a.memoryMaintCh)
		select {
		case <-a.memoryMaintDone:
		case <-time.After(5 * time.Second):
			emitDebug("MEMORY", "shutdown: worker did not exit within 5s, proceeding")
		}
	})
}

func (a *Agent) runMemoryMaintenance(req MemoryMaintenanceRequest) {
	if a == nil || !a.MemoryEnabled() {
		return
	}
	client := a.memoryMaintenanceClient()
	if client == nil {
		emitDebug("MEMORY", "skipped: no small-model client available")
		return
	}
	if strings.TrimSpace(req.WorkDir) == "" {
		req.WorkDir, _ = os.Getwd()
	}
	paths, err := memory.ResolvePaths(req.WorkDir)
	if err != nil {
		emitDebug("MEMORY", fmt.Sprintf("resolve paths: %v", err))
		return
	}
	scopes, err := loadMemoryScopes(paths)
	if err != nil {
		emitDebug("MEMORY", fmt.Sprintf("load scopes: %v", err))
		return
	}

	// First pass: let the small model decide whether this job produces durable
	// memory worth writing at all.
	if decision, err := a.runMemoryMaintenancePass(client, req, scopes, "", ""); err != nil {
		emitDebug("MEMORY", fmt.Sprintf("planner failed: %v", err))
	} else if decision != nil {
		if err := applyMemoryDecision(decision, scopes); err != nil {
			emitDebug("MEMORY", fmt.Sprintf("apply decision: %v", err))
		}
	}

	// Second pass: enforce the hard cap on any scope that still exceeds the limit.
	for _, spec := range memoryScopeOrder {
		scope := scopes[spec.Key]
		if len(scope.Body) <= memoryMaintenanceHardLimitBytes {
			continue
		}
		if _, err := a.runMemoryMaintenancePass(client, req, scopes, spec.Key, "compress"); err != nil {
			emitDebug("MEMORY", fmt.Sprintf("compress %s: %v", spec.Key, err))
			if err := writeMemoryScope(scope.Path, hardTruncateMemoryBody(scope.Body, memoryMaintenanceHardLimitBytes)); err != nil {
				emitDebug("MEMORY", fmt.Sprintf("fallback truncate %s: %v", spec.Key, err))
			}
			continue
		} else {
			// runMemoryMaintenancePass writes the body itself.
			if updated, err := readMemoryScope(scope.Path); err == nil {
				scope.Body = updated
				scope.Size = len(updated)
				scope.OverCap = len(updated) > memoryMaintenanceHardLimitBytes
				scopes[spec.Key] = scope
			}
		}
	}
}

func (a *Agent) memoryMaintenanceClient() LLMClient {
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

func (a *Agent) runMemoryMaintenancePass(client LLMClient, req MemoryMaintenanceRequest, scopes map[string]memoryScopeState, forcedScope, forcedAction string) (*memoryMaintenanceDecision, error) {
	prompt := buildMemoryMaintenancePrompt(req, scopes, forcedScope, forcedAction)
	ctx, cancel := context.WithTimeout(context.Background(), memoryMaintenanceTimeoutSeconds*time.Second)
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

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case out := <-respCh:
		if out.err != nil {
			return nil, out.err
		}
		if out.msg == nil {
			return nil, fmt.Errorf("empty memory maintenance response")
		}
		a.RecordSideUsageFromMessage(out.msg)
		decision, err := parseMemoryMaintenanceDecision(out.msg.Content)
		if err != nil {
			return nil, err
		}
		if forcedScope != "" {
			decision.Scope = forcedScope
			if decision.Action == "noop" || decision.Action == "" {
				decision.Action = "compress"
			}
			if forcedAction != "" {
				decision.Action = forcedAction
			}
		}
		if err := applyMemoryDecision(decision, scopes); err != nil {
			return nil, err
		}
		return decision, nil
	}
}

func buildMemoryMaintenancePrompt(req MemoryMaintenanceRequest, scopes map[string]memoryScopeState, forcedScope, forcedAction string) string {
	var b strings.Builder
	b.WriteString("You are the automatic memory maintenance planner for ocode.\n")
	b.WriteString("Keep only durable, reusable facts. Prefer concise bullets.\n\n")
	b.WriteString("Choose exactly one action: noop, update, or compress.\n")
	b.WriteString("Choose exactly one scope: user, project, or global.\n")
	b.WriteString("Return JSON only with keys action, scope, body, and reason.\n")
	b.WriteString("If action is noop, body must be empty. If action is update or compress, body must be the full replacement file content.\n")
	b.WriteString("Prefer project memory for repository-specific decisions and history, user memory for stable cross-project preferences, and global history for reusable lessons.\n\n")
	if forcedScope != "" {
		b.WriteString("Forced scope: ")
		b.WriteString(forcedScope)
		b.WriteString("\n")
	}
	if forcedAction != "" {
		b.WriteString("Forced action: ")
		b.WriteString(forcedAction)
		b.WriteString("\n")
	}
	b.WriteString(fmt.Sprintf("Hard cap: %d bytes per memory file.\n\n", memoryMaintenanceHardLimitBytes))

	b.WriteString("Completed job:\n")
	b.WriteString("- kind: ")
	b.WriteString(req.Job.Kind)
	b.WriteString("\n- name: ")
	b.WriteString(req.Job.Name)
	b.WriteString("\n- status: ")
	b.WriteString(req.Job.Status)
	b.WriteString("\n")
	if req.Job.ID != "" {
		b.WriteString("- id: ")
		b.WriteString(req.Job.ID)
		b.WriteString("\n")
	}
	if req.Job.Result != "" {
		b.WriteString("- result: ")
		b.WriteString(truncateForMemoryPrompt(req.Job.Result, memoryMaintenancePromptBodyBytes))
		b.WriteString("\n")
	}

	if len(req.RecentMessages) > 0 {
		b.WriteString("\nRecent conversation context:\n")
		for _, msg := range tailMessages(req.RecentMessages, memoryMaintenancePromptContext) {
			role := msg.Role
			if role == "" {
				role = "assistant"
			}
			b.WriteString("- ")
			b.WriteString(role)
			b.WriteString(": ")
			b.WriteString(truncateForMemoryPrompt(msg.Content, memoryMaintenancePromptBodyBytes/2))
			b.WriteString("\n")
		}
	}

	b.WriteString("\nCurrent memory snapshot:\n")
	for _, spec := range memoryScopeOrder {
		scope := scopes[spec.Key]
		if scope.Path == "" {
			continue
		}
		b.WriteString("## ")
		b.WriteString(spec.Name)
		b.WriteString("\n")
		b.WriteString("- path: ")
		b.WriteString(scope.Path)
		b.WriteString("\n")
		b.WriteString(fmt.Sprintf("- size_bytes: %d\n", scope.Size))
		if scope.OverCap {
			b.WriteString("- status: over-cap\n")
		} else {
			b.WriteString("- status: within-cap\n")
		}
		b.WriteString("- body:\n")
		b.WriteString(renderMemoryBodyForPrompt(scope.Body))
		b.WriteString("\n")
	}

	return b.String()
}

func parseMemoryMaintenanceDecision(content string) (*memoryMaintenanceDecision, error) {
	jsonText := extractMaintenanceJSONObject(content)
	if jsonText == "" {
		return nil, fmt.Errorf("memory maintenance response did not contain JSON")
	}
	var dec memoryMaintenanceDecision
	if err := json.Unmarshal([]byte(jsonText), &dec); err != nil {
		return nil, err
	}
	dec.Action = strings.ToLower(strings.TrimSpace(dec.Action))
	dec.Scope = strings.ToLower(strings.TrimSpace(dec.Scope))
	dec.Body = strings.ReplaceAll(dec.Body, "\r\n", "\n")
	dec.Reason = strings.TrimSpace(dec.Reason)
	if dec.Action == "truncate" {
		dec.Action = "compress"
	}
	if dec.Action == "" {
		dec.Action = "noop"
	}
	if dec.Scope == "" {
		return nil, fmt.Errorf("memory maintenance decision missing scope")
	}
	if dec.Action != "noop" && dec.Action != "update" && dec.Action != "compress" {
		return nil, fmt.Errorf("invalid memory maintenance action %q", dec.Action)
	}
	if dec.Scope != "user" && dec.Scope != "project" && dec.Scope != "global" {
		return nil, fmt.Errorf("invalid memory maintenance scope %q", dec.Scope)
	}
	return &dec, nil
}

func applyMemoryDecision(decision *memoryMaintenanceDecision, scopes map[string]memoryScopeState) error {
	if decision == nil || decision.Action == "noop" {
		return nil
	}
	scope, ok := scopes[decision.Scope]
	if !ok {
		return fmt.Errorf("unknown memory scope %q", decision.Scope)
	}
	body := normalizeMemoryBody(decision.Body)
	if body == "" {
		return fmt.Errorf("memory maintenance decision for %s produced empty body", decision.Scope)
	}
	if len(body) > memoryMaintenanceHardLimitBytes {
		body = hardTruncateMemoryBody(body, memoryMaintenanceHardLimitBytes)
	}
	if scope.Body == body {
		return nil
	}
	if err := writeMemoryScope(scope.Path, body); err != nil {
		return err
	}
	scope.Body = body
	scope.Size = len(body)
	scope.OverCap = len(body) > memoryMaintenanceHardLimitBytes
	scopes[decision.Scope] = scope
	emitDebug("MEMORY", fmt.Sprintf("%s %s (%d bytes)", decision.Action, decision.Scope, len(body)))
	return nil
}

func loadMemoryScopes(paths memory.Paths) (map[string]memoryScopeState, error) {
	scopes := map[string]memoryScopeState{}
	for _, spec := range memoryScopeOrder {
		path := scopePathByKey(paths, spec.Key)
		body, err := readMemoryScope(path)
		if err != nil {
			return nil, err
		}
		scope := memoryScopeState{Name: spec.Name, Path: path, Body: body, Size: len(body), OverCap: len(body) > memoryMaintenanceHardLimitBytes}
		scopes[spec.Key] = scope
	}
	return scopes, nil
}

func scopePathByKey(paths memory.Paths, key string) string {
	switch key {
	case "user":
		return paths.User
	case "project":
		return paths.Project
	case "global":
		return paths.Global
	default:
		return ""
	}
}

func readMemoryScope(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", nil
	}
	body, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return strings.ReplaceAll(string(body), "\r\n", "\n"), nil
}

func writeMemoryScope(path, body string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("missing memory path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	normalized := normalizeMemoryBody(body)
	if existing, err := readMemoryScope(path); err == nil && normalizeMemoryBody(existing) == normalized {
		return nil
	}
	return os.WriteFile(path, []byte(normalized), 0o644)
}

func normalizeMemoryBody(body string) string {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	body = strings.TrimRight(body, "\n")
	if body == "" {
		return ""
	}
	return body + "\n"
}

func renderMemoryBodyForPrompt(body string) string {
	body = normalizeMemoryBody(body)
	if body == "" {
		return "(empty)"
	}
	if len(body) <= memoryMaintenancePromptBodyBytes {
		return body
	}
	head := truncateToBytes(body, memoryMaintenancePromptBodyBytes/2)
	tail := suffixForPrompt(body, memoryMaintenancePromptBodyBytes/3)
	return head + "\n... <memory body truncated for prompt> ...\n" + tail
}

func truncateForMemoryPrompt(s string, max int) string {
	if max <= 0 || s == "" {
		return ""
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	if len(s) <= max {
		return s
	}
	return truncateToBytes(s, max) + "…"
}

func truncateToBytes(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	end := max
	for end > 0 && !utf8.RuneStart(s[end]) {
		end--
	}
	if end <= 0 {
		return ""
	}
	return s[:end]
}

func suffixForPrompt(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	start := len(s) - max
	for start < len(s) && start > 0 && !utf8.RuneStart(s[start]) {
		start++
	}
	if start >= len(s) {
		return ""
	}
	return s[start:]
}

func hardTruncateMemoryBody(body string, max int) string {
	body = normalizeMemoryBody(body)
	if len(body) <= max {
		return body
	}
	marker := "\n\n[truncated to satisfy memory hard cap]\n"
	keep := max - len(marker)
	if keep <= 0 {
		return truncateToBytes(body, max)
	}
	return truncateToBytes(body, keep) + marker
}

func extractMaintenanceJSONObject(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		if idx := strings.Index(s[3:], "```"); idx >= 0 {
			s = strings.TrimSpace(s[3 : 3+idx])
		}
	}
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start < 0 || end < start {
		return ""
	}
	return s[start : end+1]
}

func tailMessages(msgs []Message, limit int) []Message {
	if limit <= 0 || len(msgs) == 0 {
		return nil
	}
	start := 0
	if len(msgs) > limit {
		start = len(msgs) - limit
	}
	out := make([]Message, 0, len(msgs)-start)
	for _, msg := range msgs[start:] {
		if strings.TrimSpace(msg.Content) == "" {
			continue
		}
		out = append(out, msg)
	}
	return out
}
