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
	out := make([]Message, 0, len(base)+1)
	out = append(out, base...)
	out = append(out, Message{Role: "user", Content: strings.TrimRight(b.String(), "\n")})
	return out
}
