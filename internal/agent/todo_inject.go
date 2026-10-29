package agent

import (
	"fmt"
	"strings"

	"github.com/u007/ocode/internal/tool"
)

// todoMarker is the prefix on the user-role block that re-anchors the agent
// on the current todo plan. Per the persistent-todo design (PLAN-agent-crew/
// 03-persistent-todo.md), the block is intentionally user-role rather than
// system-role: every system-role message is hoisted into the cached system
// block (see collectAndRemoveSystemMessages in client.go), so a system-role
// block that grows with the plan would rewrite and bust the cached system
// prompt on every turn. The user-role tail rides the uncached suffix and
// coalesces with the current user turn, which is exactly what we want.
const todoMarker = "[ocode:todo]"

// todoScope addresses this agent's own plan: the project root enclosing its
// WorkDir ("" = process cwd) and its conversation id. It is what the todo tools
// receive in their context and what the re-anchor and auto-continue reads use,
// so a server session is served its own file and never the TUI's global
// "current" todo session (which a server never sets).
func (a *Agent) todoScope() tool.TodoScope {
	return tool.TodoScope{ProjectRoot: tool.TodoProjectRootFor(a.WorkDir()), SessionID: a.OpenCodeSessionID()}
}

// injectTodoTail appends a user-role re-anchor block carrying this agent's
// session todo plan, but ONLY when the list is non-empty and has at least
// one open item. A finished or absent list injects nothing, which is the
// cache-stability invariant: a no-op turn keeps the messages slice
// byte-identical so the cached prefix survives.
//
// The agent loop calls this alongside the other tail injectors
// (injectNotesTail, injectDiscoveryContext). Like them, base is not mutated.
func (a *Agent) injectTodoTail(base []Message) []Message {
	// The re-anchor — and especially its imperative status directive — belongs
	// to the plan's owner. Sub-agents and ask/advisor helpers share the parent's
	// session id (so todoScope resolves the PARENT file) but have
	// todowrite/todo_update stripped (mainAgentOnlyTools). Telling them to run
	// todo_update would name a tool they do not have.
	if !a.PlanPromptEnabled() {
		return base
	}
	snap := tool.ReadTodoSnapshot(a.todoScope())
	if !snap.HasOpenItems || snap.Content == "" {
		return base
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", todoMarker)
	fmt.Fprintf(&b, "Current plan (revision %d). The list is on disk at .ocode/todo/<session-id>.md; use todoread for the canonical copy before any mutation, and re-run todowrite/todo_update to keep the file in sync.\n\n", snap.Revision)
	b.WriteString(snap.Content)
	if len(snap.Blocked) > 0 {
		fmt.Fprintf(&b, "Blocked: %s\n", strings.Join(snap.Blocked, "; "))
	}
	b.WriteString("\n")
	b.WriteString(todoStatusDirective(snap))
	b.WriteString("\n")
	out := make([]Message, 0, len(base)+1)
	out = append(out, base...)
	out = append(out, Message{Role: "user", Content: strings.TrimRight(b.String(), "\n")})
	return out
}

// todoStatusDirective is the action line appended after the plan. The list
// alone says WHAT the plan is, not what to do with it; observed long sessions
// write the plan once and then never call todo_update, so every item stays
// pending and the sidebar never shows current work. The directive names the
// exact call (including the revision the model must cite) for the next status
// transition. It is derived only from the snapshot, so an unchanged plan
// produces a byte-identical block — the user-role suffix must stay stable for
// prompt caching.
func todoStatusDirective(snap tool.TodoSnapshot) string {
	blocked := make(map[string]bool, len(snap.BlockedIDs))
	for _, id := range snap.BlockedIDs {
		blocked[id] = true
	}

	var inProgress []string
	for _, it := range snap.OpenItems {
		if it.Status == "in_progress" && !blocked[it.ID] {
			inProgress = append(inProgress, it.ID)
		}
	}
	if len(inProgress) > 0 {
		subject := inProgress[0] + " is"
		if len(inProgress) > 1 {
			subject = strings.Join(inProgress, ", ") + " are"
		}
		return fmt.Sprintf("Plan status: %s in_progress. If you are working this plan, the moment the item is verified run todo_update {\"revision\": %d, \"op\": \"set_status\", \"item_id\": %q, \"status\": \"done\", \"result\": \"<one-line outcome>\"}, then cite the revision that call returns when you mark the next ready item in_progress. Never batch status updates.", subject, snap.Revision, inProgress[0])
	}
	// set_deps does not check dependencies, but a done transition does, so an
	// in-progress item can be blocked. Naming "done" there would be a call the
	// store rejects on every Step; name the recovery instead.
	for _, it := range snap.OpenItems {
		if it.Status == "in_progress" {
			return fmt.Sprintf("Plan status: %s is in_progress but blocked by an open dependency. Run todo_update {\"revision\": %d, \"op\": \"set_deps\", \"item_id\": %q, \"deps\": []} to clear it, or finish the dependency first; a blocked item cannot be marked done.", it.ID, snap.Revision, it.ID)
		}
	}
	for _, it := range snap.OpenItems {
		if blocked[it.ID] {
			continue
		}
		return fmt.Sprintf("Plan status: no plan item is in_progress. If you are continuing this plan, before your next tool call run todo_update {\"revision\": %d, \"op\": \"set_status\", \"item_id\": %q, \"status\": \"in_progress\"} (run todoread first only if the plan changed since this snapshot). The next ready item is %s: %s. Never batch status updates.", snap.Revision, it.ID, it.ID, oneLineTodoText(it.Text))
	}
	// Defensive: every open item waits on another open item, which the store's
	// cycle check rejects at write time, so this is not reachable through the
	// tools (it can still come from a hand-edited file). Name no target rather
	// than recommend one that cannot start.
	return "Plan status: no ready plan item. Re-read the plan with todoread and use todo_update set_deps to clear a blocked or circular dependency before starting work."
}

// oneLineTodoText keeps model-written plan text from breaking the directive:
// newlines and runs of whitespace collapse to single spaces, and very long text
// is truncated by rune count (never mid-byte).
func oneLineTodoText(text string) string {
	flat := strings.Join(strings.Fields(text), " ")
	const maxRunes = 120
	runes := []rune(flat)
	if len(runes) <= maxRunes {
		return flat
	}
	return string(runes[:maxRunes]) + "…"
}
