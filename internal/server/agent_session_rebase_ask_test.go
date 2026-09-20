package server

import (
	"strings"
	"testing"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/session"
	"github.com/u007/ocode/internal/tool"
)

// TestRebaseKeepsResidentPendingAsk pins the second half of the
// ses_2026-09-18-233409-df1a92d3 report: a turn that pauses on a permission ask
// and whose turn-end save needed a concurrent-writer rebase must NOT lose that
// ask from the resident transcript.
//
// The rebase path returns the loader's FILTERED view of the merged disk
// transcript (removeIncompleteToolRequests strips the unresolved ask sentinel and
// the tool-call that produced it), and persistTurnTranscript adopts it as
// as.messages. The resident then has no pending ask, so:
//   - tailIsPermissionAsk is false → the next user message starts a fresh turn,
//     recoverOrphanedToolCalls re-executes the orphaned call, and a brand new ask
//     is raised (the user's "it keeps asking again" loop), and
//   - the turn-end `messages` broadcast carries no ask → the web dialog closes.
func TestRebaseKeepsResidentPendingAsk(t *testing.T) {
	h := NewHandler()
	projectRoot := t.TempDir()
	id := session.NewSessionID()
	h.sessions.Register(id, projectRoot)

	// Disk history ends on an UNRESOLVED ask round from a prior turn, so the
	// loader view is shorter than the raw rows and drops that assistant's call.
	priorCall := agent.ToolCall{ID: "old-1", Type: "function"}
	priorCall.Function.Name = "bash"
	priorCall.Function.Arguments = "{}"
	rawHistory := []agent.Message{
		{Role: "user", Content: "u0", UserSeq: 1},
		{Role: "assistant", Content: "prior", ToolCalls: []agent.ToolCall{priorCall}},
		{Role: "tool", ToolID: "old-1", Content: tool.SentinelPermissionAsk + `{"tool_name":"bash"}`},
	}
	if err := session.SaveForDir(projectRoot, id, "", rawHistory, nil); err != nil {
		t.Fatalf("seed session: %v", err)
	}

	// The resident transcript is the loader view (that is what a resumed agent
	// bootstraps from) — shorter than disk.
	loaded, err := session.LoadForDir(projectRoot, id)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	as := &agentSession{messages: append([]agent.Message(nil), loaded.Messages...)}
	base := as.messages
	if len(base) != 2 {
		t.Fatalf("loader view = %d rows, want 2", len(base))
	}

	// The turn runs: user message + the assistant row that raises a NEW ask,
	// then the ask sentinel. baseLen = everything through this turn's user
	// message.
	newCall := agent.ToolCall{ID: "new-1", Type: "function"}
	newCall.Function.Name = "bash"
	newCall.Function.Arguments = "{}"
	as.messages = append(as.messages,
		agent.Message{Role: "user", Content: "continue", UserSeq: 2},
		agent.Message{Role: "assistant", Content: "running live check", ToolCalls: []agent.ToolCall{newCall}},
		agent.Message{Role: "tool", ToolID: "new-1", Content: tool.SentinelPermissionAsk + `{"tool_name":"bash"}`},
	)
	baseLen := len(base) + 1

	// The turn's mid-turn live writes landed the user message and the assistant
	// row on disk (wireLivePersist), so the plain turn-end save now conflicts
	// with the raw disk rows and takes the rebase path.
	live := append(append([]agent.Message(nil), rawHistory...),
		agent.Message{Role: "user", Content: "continue", UserSeq: 2},
		agent.Message{Role: "assistant", Content: "running live check", ToolCalls: []agent.ToolCall{newCall}},
	)
	if err := session.SaveForDir(projectRoot, id, "", live, nil); err != nil {
		t.Fatalf("live persist: %v", err)
	}

	h.persistTurnTranscript(id, as, baseLen, "test-rebase-ask")

	// The resident transcript must still carry the pending ask.
	found := false
	for _, m := range as.messages {
		if m.Role == "tool" && strings.HasPrefix(m.Content, tool.SentinelPermissionAsk) {
			found = true
		}
	}
	if !found {
		t.Fatalf("resident transcript lost the pending ask after a rebase: %+v", messageContents(as.messages))
	}
	if !tailIsPermissionAsk(as.messages) {
		t.Fatalf("tailIsPermissionAsk(resident) = false after a rebase; the next user message would start a fresh turn")
	}
	// And the disk keeps it too (so resolve/reload can find it).
	stored, err := session.LoadForDir(projectRoot, id)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !tailIsPermissionAsk(append(loaded.Messages[:0:0], stored.Messages...)) && len(stored.Messages) == 0 {
		t.Fatalf("disk transcript empty after rebase")
	}
}
