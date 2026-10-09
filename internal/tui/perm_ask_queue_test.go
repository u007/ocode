package tui

import (
	"encoding/json"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/tool"
)

// Regression: a single model message can dispatch several ask-capable tool
// calls at once (webfetch, websearch, github_* and every MCP tool are
// Parallel() == true and default-ask; a parallel ask can also sit next to a
// sequential one such as bash). Agent.Step then delivers N PERMISSION_ASK: tool
// messages back-to-back through OnMessage in the same frame.
//
// The TUI keeps ONE dialog (showPermDialog / pendingPermission /
// pendingToolCallID). Before the queue these N messages overwrote each other:
// the last ask won the dialog and the earlier ones were dropped from it
// entirely, so the user answered against a transcript whose visible prompt
// belonged to a *different* ask, and each dropped ask later resurfaced as a
// duplicate orphan-recovery ask (buildAgentMessagesSnapshot skips every sentinel,
// so the earlier tool_call becomes an orphan and recoverOrphanedToolCalls
// re-executes it).
//
// The invariant: asks are presented in the order the agent produced them, one
// at a time, and none is discarded.

func permAskToolMsg(t *testing.T, toolCallID, toolName, command string) agent.Message {
	t.Helper()
	req := agent.PermissionRequest{
		ToolName: toolName,
		Scope:    agent.PermissionScopeTool,
		Rule:     "tool." + toolName,
		Command:  command,
	}
	payload, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal permission request: %v", err)
	}
	return agent.Message{
		Role:    "tool",
		ToolID:  toolCallID,
		Content: tool.SentinelPermissionAsk + string(payload),
	}
}

// newPermQueueTestModel builds the minimum model the ask path needs: an agent
// (handlePermissionChoice refuses to act without one), a laid-out chat view so
// the dialog geometry is real, and theme styles so rendering does not panic.
func newPermQueueTestModel(t *testing.T) model {
	t.Helper()
	m := newTestModel()
	m.agent = newTestAgent(nil, nil, &config.Config{}, nil)
	m.ready = true
	upd, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = upd.(model)
	return m
}

// deliverAsks feeds one tool-result batch through the same Update branch the
// agent's OnMessage pump uses (internal/tui/model.go, `case []agent.Message`).
func deliverAsks(t *testing.T, m model, asks ...agent.Message) model {
	t.Helper()
	upd, _ := m.Update(asks)
	return upd.(model)
}

func TestPermAskQueue_SecondAskDoesNotReplaceTheFirst(t *testing.T) {
	m := newPermQueueTestModel(t)
	m = deliverAsks(t, m,
		permAskToolMsg(t, "call_a", "webfetch", "https://a.example"),
		permAskToolMsg(t, "call_b", "websearch", "golang channels"),
	)

	if !m.showPermDialog {
		t.Fatal("showPermDialog = false, want the first ask's dialog open")
	}
	if m.pendingToolCallID != "call_a" {
		t.Errorf("pendingToolCallID = %q, want %q (the first ask must win the dialog; asks are presented in order)", m.pendingToolCallID, "call_a")
	}
	if m.pendingPermission.ToolName != "webfetch" {
		t.Errorf("pendingPermission.ToolName = %q, want webfetch", m.pendingPermission.ToolName)
	}
	if len(m.permAskQueue) != 1 {
		t.Fatalf("permAskQueue has %d entries, want 1 (the second ask must be queued, not dropped)", len(m.permAskQueue))
	}
	if got := m.permAskQueue[0].toolCallID; got != "call_b" {
		t.Errorf("queued toolCallID = %q, want call_b", got)
	}
	// Both asks still get a transcript line so the user can read what is coming.
	prompts := 0
	for _, msg := range m.messages {
		if msg.text == renderPermissionPrompt(m.permAskQueue[0].req) {
			prompts++
		}
	}
	if prompts != 1 {
		t.Errorf("found %d transcript lines for the queued ask, want 1", prompts)
	}
}

func TestPermAskQueue_AnsweringOnePromotesTheNext(t *testing.T) {
	m := newPermQueueTestModel(t)
	m = deliverAsks(t, m,
		permAskToolMsg(t, "call_a", "webfetch", "https://a.example"),
		permAskToolMsg(t, "call_b", "websearch", "golang channels"),
		permAskToolMsg(t, "call_c", "webfetch", "https://c.example"),
	)
	if len(m.permAskQueue) != 2 {
		t.Fatalf("permAskQueue has %d entries, want 2", len(m.permAskQueue))
	}

	m.permDialogInput("y")
	if !m.showPermDialog {
		t.Fatal("showPermDialog = false after answering ask 1, want ask 2 promoted into the dialog")
	}
	if m.pendingToolCallID != "call_b" {
		t.Errorf("pendingToolCallID = %q, want call_b (next queued ask, FIFO)", m.pendingToolCallID)
	}
	if m.pendingPermission.ToolName != "websearch" {
		t.Errorf("pendingPermission.ToolName = %q, want websearch", m.pendingPermission.ToolName)
	}
	if len(m.permAskQueue) != 1 || m.permAskQueue[0].toolCallID != "call_c" {
		t.Fatalf("permAskQueue = %+v, want exactly [call_c]", m.permAskQueue)
	}
	if m.permConfirm != "" {
		t.Errorf("permConfirm = %q, want \"\" after a plain allow (a stale always-allow step would corrupt the next dialog)", m.permConfirm)
	}
	// Denying ask 2 promotes ask 3 — only the LAST answer closes the dialog.
	m.permDialogInput("n")
	if !m.showPermDialog || m.pendingToolCallID != "call_c" {
		t.Fatalf("after denying ask 2: dialog=%v callID=%q, want ask 3 promoted", m.showPermDialog, m.pendingToolCallID)
	}
	// The third answer drains the queue and closes the dialog for good.
	m.permDialogInput("n")
	if m.showPermDialog {
		t.Error("showPermDialog = true after the last ask was denied, want false")
	}
	if len(m.permAskQueue) != 0 {
		t.Errorf("permAskQueue = %+v, want empty", m.permAskQueue)
	}
	if m.pendingToolCallID != "" {
		t.Errorf("pendingToolCallID = %q, want \"\" once the queue drains", m.pendingToolCallID)
	}
}

// A deny must promote the next ask exactly like an allow — otherwise the
// remaining asks of the batch are stranded behind a closed dialog.
func TestPermAskQueue_DenyAlsoPromotesTheNext(t *testing.T) {
	m := newPermQueueTestModel(t)
	m = deliverAsks(t, m,
		permAskToolMsg(t, "call_a", "bash", "rm -rf build"),
		permAskToolMsg(t, "call_b", "webfetch", "https://b.example"),
	)
	m.permDialogInput("n")
	if !m.showPermDialog {
		t.Fatal("showPermDialog = false after denying ask 1, want ask 2 promoted")
	}
	if m.pendingToolCallID != "call_b" {
		t.Errorf("pendingToolCallID = %q, want call_b", m.pendingToolCallID)
	}
}

// The two-phase always-allow step ("a" then "y") is NOT a terminal outcome, so
// it must keep the SAME ask on screen — promoting here would answer ask 1 and
// silently swap ask 2 in underneath the confirmation the user is reading.
func TestPermAskQueue_AlwaysAllowConfirmKeepsTheSameAsk(t *testing.T) {
	m := newPermQueueTestModel(t)
	m = deliverAsks(t, m,
		permAskToolMsg(t, "call_a", "webfetch", "https://a.example"),
		permAskToolMsg(t, "call_b", "websearch", "golang channels"),
	)
	m.permDialogInput("a")
	if m.permConfirm != "a" {
		t.Fatalf("permConfirm = %q, want \"a\" (the always-allow confirmation step should be showing)", m.permConfirm)
	}
	if m.pendingToolCallID != "call_a" {
		t.Errorf("pendingToolCallID = %q, want call_a — the confirm step describes ask 1, not ask 2", m.pendingToolCallID)
	}
	if len(m.permAskQueue) != 1 {
		t.Errorf("permAskQueue has %d entries, want 1 (nothing promoted before the confirm resolves)", len(m.permAskQueue))
	}
	// Confirming is terminal: ask 2 takes the dialog.
	m.permDialogInput("y")
	if !m.showPermDialog {
		t.Fatal("showPermDialog = false after confirming always-allow, want ask 2 promoted")
	}
	if m.pendingToolCallID != "call_b" {
		t.Errorf("pendingToolCallID = %q, want call_b", m.pendingToolCallID)
	}
}

// Backing out of the confirm step ("n" at the confirm screen) is also not
// terminal: the original ask must still be on screen.
func TestPermAskQueue_AlwaysAllowBackKeepsTheSameAsk(t *testing.T) {
	m := newPermQueueTestModel(t)
	m = deliverAsks(t, m,
		permAskToolMsg(t, "call_a", "webfetch", "https://a.example"),
		permAskToolMsg(t, "call_b", "websearch", "golang channels"),
	)
	m.permDialogInput("a")
	m.permDialogInput("n")
	if !m.showPermDialog {
		t.Fatal("showPermDialog = false after backing out of the confirm step, want ask 1 still up")
	}
	if m.pendingToolCallID != "call_a" {
		t.Errorf("pendingToolCallID = %q, want call_a", m.pendingToolCallID)
	}
	if m.permConfirm != "" {
		t.Errorf("permConfirm = %q, want \"\"", m.permConfirm)
	}
}

// A sub-agent's ask shares the same dialog slot, and its goroutine is blocked
// on a response channel. A main-agent sentinel arriving in the same frame must
// queue behind it, not overwrite the request that the pending respCh belongs to.
func TestPermAskQueue_MainAgentAskQueuesBehindAnOpenSubAgentDialog(t *testing.T) {
	m := newPermQueueTestModel(t)
	respCh := make(chan agent.PermissionResponse, 1)
	upd, _ := m.Update(subAgentPermAskMsg{
		req:    agent.PermissionRequest{ToolName: "bash", Scope: agent.PermissionScopeBashPrefix, Rule: "bash(git*)", Command: "git push"},
		respCh: respCh,
	})
	m = upd.(model)
	if !m.showPermDialog || m.pendingSubAgentResp == nil {
		t.Fatalf("precondition: sub-agent dialog not open (showPermDialog=%v resp=%v)", m.showPermDialog, m.pendingSubAgentResp != nil)
	}

	m = deliverAsks(t, m, permAskToolMsg(t, "call_a", "webfetch", "https://a.example"))
	if m.pendingToolCallID != "" {
		t.Errorf("pendingToolCallID = %q, want \"\" — a queued main-agent ask must not stamp over the sub-agent's slot", m.pendingToolCallID)
	}
	if m.pendingPermission.Command != "git push" {
		t.Errorf("pendingPermission.Command = %q, want %q — the sub-agent's request must survive", m.pendingPermission.Command, "git push")
	}
	if len(m.permAskQueue) != 1 || m.permAskQueue[0].toolCallID != "call_a" {
		t.Fatalf("permAskQueue = %+v, want exactly [call_a]", m.permAskQueue)
	}

	// Answering the sub-agent ask hands the level back on respCh and promotes
	// the queued main-agent ask into the now-free dialog.
	m.permDialogInput("y")
	select {
	case resp := <-respCh:
		if resp.Level != agent.PermissionAllow {
			t.Errorf("sub-agent response level = %v, want allow", resp.Level)
		}
	default:
		t.Fatal("sub-agent respCh received nothing; the blocked goroutine would hang forever")
	}
	if !m.showPermDialog {
		t.Fatal("showPermDialog = false, want the queued main-agent ask promoted")
	}
	if m.pendingToolCallID != "call_a" {
		t.Errorf("pendingToolCallID = %q, want call_a", m.pendingToolCallID)
	}
}

// Each queued ask carries its own tool name/args, so the substituted tool result
// is spliced against the right ToolID — a shared/popped field would write ask 2's
// result over ask 1's sentinel.
//
// This also pins an ordering constraint: permDialogInput must build the
// replacement command BEFORE the queue advances. executeApprovedTool is a value
// receiver that reads pendingToolCallID when it is called, so promoting first
// would silently resolve every ask against whichever one is now on screen.
func TestPermAskQueue_ResolvedAsksKeepTheirOwnIdentity(t *testing.T) {
	m := newPermQueueTestModel(t)
	m = deliverAsks(t, m,
		permAskToolMsg(t, "call_a", "webfetch", "https://a.example"),
		permAskToolMsg(t, "call_b", "webfetch", "https://b.example"),
	)

	seen := map[string]bool{}
	for i := 0; i < 2 && m.showPermDialog; i++ {
		want := m.pendingToolCallID
		cmd, closed := m.permDialogInput("y")
		if !closed {
			t.Fatalf("iteration %d: permDialogInput reported the dialog still open", i)
		}
		if cmd == nil {
			t.Fatalf("iteration %d: no command to substitute the answer", i)
		}
		msg := cmd()
		batch, ok := msg.([]agent.Message)
		if !ok || len(batch) != 1 {
			t.Fatalf("iteration %d: approved tool produced %#v, want one []agent.Message", i, msg)
		}
		if batch[0].ToolID != want {
			t.Fatalf("iteration %d: substituted ToolID = %q, want %q — the answer must land on the ask it was given for", i, batch[0].ToolID, want)
		}
		if seen[want] {
			t.Fatalf("ask %q was answered twice", want)
		}
		seen[want] = true
	}
	if len(seen) != 2 {
		t.Errorf("answered %d distinct asks, want 2 (%v)", len(seen), seen)
	}
}

// An ask names a tool call in the round that produced it, so it must not
// survive a session change: resolving it afterwards would splice a result into a
// transcript that no longer holds that call, and a parked sub-agent respCh would
// leak its goroutine. Every transcript-rebuild site (session load, /new, an /rc
// rewind) clears the whole ask state.
func TestPermAskQueue_ClearedWhenTheTranscriptIsRebuilt(t *testing.T) {
	m := newPermQueueTestModel(t)
	m = deliverAsks(t, m,
		permAskToolMsg(t, "call_a", "webfetch", "https://a.example"),
		permAskToolMsg(t, "call_b", "websearch", "golang channels"),
	)
	if !m.showPermDialog || len(m.permAskQueue) != 1 {
		t.Fatalf("precondition: showPermDialog=%v queue=%d", m.showPermDialog, len(m.permAskQueue))
	}

	m.clearPermAskState()
	if m.showPermDialog {
		t.Error("showPermDialog = true after clearPermAskState, want false")
	}
	if len(m.permAskQueue) != 0 {
		t.Errorf("permAskQueue = %+v, want empty", m.permAskQueue)
	}
	if m.pendingToolCallID != "" || m.pendingPermission.ToolName != "" || m.pendingToolName != "" {
		t.Errorf("pending slot not cleared: callID=%q tool=%q req=%q", m.pendingToolCallID, m.pendingToolName, m.pendingPermission.ToolName)
	}
	if m.pendingSubAgentResp != nil || m.rcPendingPerm != nil || m.permConfirm != "" {
		t.Errorf("clearPermAskState left dialog state behind: subResp=%v rc=%v confirm=%q", m.pendingSubAgentResp != nil, m.rcPendingPerm != nil, m.permConfirm)
	}
}

// The turn must NOT resume while an ask is still unanswered. The agent strips
// every PERMISSION_ASK sentinel when rebuilding history
// (buildAgentMessagesSnapshot), so re-Stepping with one outstanding ask turns it
// into an orphan that recoverOrphanedToolCalls re-executes — a duplicate ask the
// user never chose, or (worse) one that an "always allow" on the previous ask
// has already made moot.
func TestPermAskQueue_UnansweredAskHoldsTheTurn(t *testing.T) {
	m := newPermQueueTestModel(t)
	m = deliverAsks(t, m,
		permAskToolMsg(t, "call_a", "webfetch", "https://a.example"),
		permAskToolMsg(t, "call_b", "websearch", "golang channels"),
	)

	// The replacement result for ask 1 arrives with no sentinel of its own.
	// The batch-handler resume guard is keyed on the dialog being open.
	m.permDialogInput("y")
	if !m.showPermDialog {
		t.Fatal("showPermDialog = false; ask 2 should be up, holding the turn")
	}
	repl := m.executeApprovedTool(m.pendingToolName, m.pendingToolArgs, "")()
	batch, ok := repl.([]agent.Message)
	if !ok || len(batch) != 1 {
		t.Fatalf("approved tool produced %#v, want one []agent.Message", repl)
	}
	if strings.HasPrefix(batch[0].Content, tool.SentinelPermissionAsk) {
		t.Fatal("replacement result carries a sentinel; the resume guard would not be exercised")
	}
	// Feeding it must leave ask 2 unanswered — i.e. still the live dialog.
	after := deliverAsks(t, m, batch...)
	if after.pendingToolCallID != "call_b" || !after.showPermDialog {
		t.Errorf("after the first answer: dialog=%v callID=%q, want ask 2 still pending", after.showPermDialog, after.pendingToolCallID)
	}
}

// The resume guard is only observable through the command Update returns:
// `case []agent.Message` returns m.askAgent() and nothing else, so a nil cmd
// proves the turn was held. Without the guard the agent re-steps while ask 2 is
// unanswered; buildAgentMessagesSnapshot then strips ask 2's sentinel, turning
// it into an orphan that recoverOrphanedToolCalls re-executes.
func TestPermAskQueue_UpdateReturnsNoAskCmdWhileAnAskIsOpen(t *testing.T) {
	m := newPermQueueTestModel(t)
	m = deliverAsks(t, m,
		permAskToolMsg(t, "call_a", "webfetch", "https://a.example"),
		permAskToolMsg(t, "call_b", "websearch", "golang channels"),
	)

	// The replacement result for ask 1 carries no sentinel of its own, so the
	// sentinel scan cannot hold the turn — only the dialog guard can.
	m.permDialogInput("y")
	repl, ok := m.executeApprovedTool(m.pendingToolName, m.pendingToolArgs, "")().([]agent.Message)
	if !ok || len(repl) != 1 {
		t.Fatalf("replacement = %#v, want one []agent.Message", repl)
	}
	if strings.HasPrefix(repl[0].Content, tool.SentinelPermissionAsk) {
		t.Fatal("replacement carries a sentinel; the guard is not what is under test")
	}

	upd, cmd := m.Update(repl)
	after := upd.(model)
	if cmd != nil {
		t.Error("Update returned a command (askAgent) while ask 2 is still unanswered; the turn must be held")
	}
	if !after.showPermDialog || after.pendingToolCallID != "call_b" {
		t.Errorf("dialog=%v callID=%q, want ask 2 still on screen", after.showPermDialog, after.pendingToolCallID)
	}

	// Draining the queue releases the turn: the same delivery now resumes.
	denyCmd, closed := after.permDialogInput("n")
	if !closed {
		t.Fatal("the last answer left the dialog open")
	}
	denyBatch, ok := denyCmd().([]agent.Message)
	if !ok || len(denyBatch) != 1 {
		t.Fatalf("denial produced %#v, want one []agent.Message", denyBatch)
	}
	upd2, resume := after.Update(denyBatch)
	if resume == nil {
		t.Error("turn still held after the last ask was answered")
	}
	if upd2.(model).showPermDialog {
		t.Error("showPermDialog = true after the final answer, want false")
	}
}

// A non-terminal answer must NOT advance the queue. Two reachable shapes:
//
//  1. an unrecognised key, which re-opens the same ask with a hint; and
//  2. "always allow" on a request the permission layer classifies as harmful,
//     which is refused outright.
//
// In both cases the dialog still describes the SAME ask. Promoting here would
// swap the next ask in underneath the message the user is reading, so the
// confirmation they act on would belong to a different tool call.
func TestPermAskQueue_NonTerminalAnswersKeepTheSameAsk(t *testing.T) {
	t.Run("unrecognised choice", func(t *testing.T) {
		m := newPermQueueTestModel(t)
		m = deliverAsks(t, m,
			permAskToolMsg(t, "call_a", "webfetch", "https://a.example"),
			permAskToolMsg(t, "call_b", "websearch", "golang channels"),
		)
		m.permDialogInput("z")
		if !m.showPermDialog {
			t.Fatal("showPermDialog = false after an unrecognised choice; the ask must stay up")
		}
		if m.pendingToolCallID != "call_a" {
			t.Errorf("pendingToolCallID = %q, want call_a", m.pendingToolCallID)
		}
		if len(m.permAskQueue) != 1 {
			t.Errorf("permAskQueue has %d entries, want 1 (nothing promoted)", len(m.permAskQueue))
		}
	})

	t.Run("harmful request refuses always-allow", func(t *testing.T) {
		m := newPermQueueTestModel(t)
		// The fixture has to satisfy BOTH gates for the branch to be reachable
		// through the keyboard: harmful (agent.IsHarmfulRequest) AND
		// always-allowable (permAlwaysRuleAvailable). A bash-prefix-scoped
		// harmful git command fails the second gate — AlwaysRuleChoiceAvailable
		// withholds always-allow for mutating git subcommands — so the ask must
		// be tool-scoped. Verified against both predicates, not assumed.
		harmful := agent.PermissionRequest{
			ToolName: "bash",
			Scope:    agent.PermissionScopeTool,
			Rule:     "tool.bash",
			Command:  "git reset --hard",
			Args:     json.RawMessage(`{"command":"git reset --hard"}`),
		}
		if !agent.IsHarmfulRequest(harmful) {
			t.Fatal("fixture is not classified harmful; the branch under test is unreachable")
		}
		if !permAlwaysRuleAvailable(harmful) {
			t.Fatal("fixture is not always-allowable, so permDialogInput(\"a\") would never reach the branch")
		}
		payload, err := json.Marshal(harmful)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		m = deliverAsks(t, m,
			agent.Message{Role: "tool", ToolID: "call_a", Content: tool.SentinelPermissionAsk + string(payload)},
			permAskToolMsg(t, "call_b", "websearch", "golang channels"),
		)
		// "a" arms the confirmation, "y" confirms it — and the confirmation is
		// refused, which is a non-terminal outcome.
		m.permDialogInput("a")
		if m.permConfirm != "a" {
			t.Fatalf("permConfirm = %q, want \"a\"", m.permConfirm)
		}
		m.permDialogInput("y")
		if !m.showPermDialog {
			t.Fatal("showPermDialog = false after a refused always-allow; the ask must stay up")
		}
		if m.pendingToolCallID != "call_a" {
			t.Errorf("pendingToolCallID = %q, want call_a", m.pendingToolCallID)
		}
		if len(m.permAskQueue) != 1 {
			t.Errorf("permAskQueue has %d entries, want 1 (nothing promoted)", len(m.permAskQueue))
		}
	})
}
