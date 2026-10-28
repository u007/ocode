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
	var inProgress []string
	for _, it := range snap.OpenItems {
		if it.Status == "in_progress" {
			inProgress = append(inProgress, it.ID)
		}
	}
	if len(inProgress) > 0 {
		subject := fmt.Sprintf("%s is", inProgress[0])
		if len(inProgress) > 1 {
			subject = strings.Join(inProgress, ", ") + " are"
		}
		return fmt.Sprintf(
			"Plan status: %s in_progress. As soon as an item is verified, run todo_update {\"revision\": %d, \"op\": \"set_status\", \"item_id\": %q, \"status\": \"done\", \"result\": \"<one-line outcome>\"}, then mark the next item in_progress before starting it. Never batch status updates.\n",
			subject, snap.Revision, inProgress[0],
		)
	}
	blocked := blockedTodoIDs(snap.Blocked)
	for _, it := range snap.OpenItems {
		if blocked[it.ID] {
			continue
		}
		return fmt.Sprintf(
			"Plan status: no plan item is in_progress. Before your next tool call, run todo_update {\"revision\": %d, \"op\": \"set_status\", \"item_id\": %q, \"status\": \"in_progress\"}. Next ready item %s: %s. Never batch status updates.\n",
			snap.Revision, it.ID, it.ID, oneLineTodoText(it.Text),
		)
	}
	// Defensive: every open item waits on another open item, which the store's
	// cycle check rejects at write time, so this is not reachable through the
	// tools. Name no target rather than recommend one that cannot start.
	return "Plan status: no ready plan item. Re-read the plan with todoread and clear a blocked item's dependencies before starting work.\n"
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

// blockedTodoIDs extracts the ids from TodoSnapshot.Blocked notes ("t3 by t1,
// t2"), so the start directive never recommends an item the store will refuse
// to move to in_progress.
func blockedTodoIDs(notes []string) map[string]bool {
	out := make(map[string]bool, len(notes))
	for _, note := range notes {
		id, _, ok := strings.Cut(note, " by ")
		if ok && id != "" {
			out[id] = true
		}
	}
	return out
}
