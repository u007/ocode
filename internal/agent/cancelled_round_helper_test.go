package agent

import (
	"testing"

	"github.com/u007/ocode/internal/tool"
)

// assertNoDuplicateToolIDs fails when the transcript carries two tool messages
// for one tool_call_id. That is transcript corruption: provider builders reject
// or mangle it, and the web renderEntries has to drop the duplicate copy
// (ChatPanel.tsx `consumed` one-to-one guard), so the second result is
// invisible there while still being sent to the model.
func assertNoDuplicateToolIDs(t *testing.T, msgs []Message) {
	t.Helper()
	seen := map[string]int{}
	for _, m := range msgs {
		if m.Role != "tool" || m.ToolID == "" {
			continue
		}
		seen[m.ToolID]++
	}
	for id, n := range seen {
		if n > 1 {
			t.Errorf("tool_call_id %q has %d tool results, want exactly 1:\n%+v", id, n, msgs)
		}
	}
}

// assertEveryCallAnswered is the invariant the whole change exists to protect:
// every assistant tool_call has a matching tool message, so nothing renders as
// "running…" forever and nothing gets re-executed by recoverOrphanedToolCalls.
func assertEveryCallAnswered(t *testing.T, msgs []Message) {
	t.Helper()
	answered := map[string]bool{}
	for _, m := range msgs {
		if m.Role == "tool" && m.ToolID != "" {
			answered[m.ToolID] = true
		}
	}
	for i, m := range msgs {
		if m.Role != "assistant" {
			continue
		}
		for _, tc := range m.ToolCalls {
			if !answered[tc.ID] {
				t.Fatalf("assistant tool_call %s at index %d was never answered (renders as running, re-executed next turn):\n%+v", tc.ID, i, msgs)
			}
		}
	}
}

func TestFinishCancelledRoundNeverDuplicatesPublishedResults(t *testing.T) {
	// Reproduces the publish-loop exit: results[0] was already appended to
	// newMsgs/messages, and the cancel landed on results[1]. The helper must
	// count results[0] as answered WITHOUT emitting it again, and must answer
	// results[2], which never ran.
	tcA := ToolCall{ID: "call-a", Type: "function"}
	tcA.Function.Name = "alpha"
	tcA.Function.Arguments = `{}`
	tcB := ToolCall{ID: "call-b", Type: "function"}
	tcB.Function.Name = "beta"
	tcB.Function.Arguments = `{}`
	tcC := ToolCall{ID: "call-c", Type: "function"}
	tcC.Function.Name = "gamma"
	tcC.Function.Arguments = `{}`

	results := []Message{
		{Role: "tool", ToolID: "call-a", Content: "alpha done"},
		{Role: "tool", ToolID: "call-b", Content: "beta done"},
		{}, // call-c never executed — zero Message
	}

	a := NewAgent(nil, nil, nil, nil)
	var emitted []Message
	a.OnMessage = func(m Message) { emitted = append(emitted, m) }

	resp := &Message{Role: "assistant", ToolCalls: []ToolCall{tcA, tcB, tcC}}
	newMsgs := []Message{*resp, results[0]}
	messages := []Message{*resp, results[0]}

	got, err := a.finishCancelledRound(newMsgs, messages, resp, results, 1)
	if err != nil {
		t.Fatalf("finishCancelledRound err = %v", err)
	}

	// call-a was already in newMsgs before the call; it must appear exactly once.
	seen := map[string]int{}
	for _, m := range got {
		if m.Role == "tool" && m.ToolID != "" {
			seen[m.ToolID]++
		}
	}
	if seen["call-a"] != 1 {
		t.Errorf("call-a appears %d times, want 1 (already published before the cancel):\n%+v", seen["call-a"], got)
	}
	if seen["call-b"] != 1 {
		t.Errorf("call-b appears %d times, want 1 (completed but unpublished):\n%+v", seen["call-b"], got)
	}
	// call-c never ran, so it must be answered with the cancellation text.
	if seen["call-c"] != 1 {
		t.Fatalf("call-c appears %d times, want 1:\n%+v", seen["call-c"], got)
	}
	last := got[len(got)-1]
	if last.ToolID != "call-c" || last.Content != tool.ToolCancelledResult {
		t.Errorf("last message = %+v, want call-c carrying %q", last, tool.ToolCancelledResult)
	}

	// The live-UI stream must see each result exactly once too — a duplicate
	// OnMessage would make the TUI/web render the same block twice. call-a is
	// deliberately absent: the CALLER already published it, so re-emitting it
	// here is precisely the duplicate this helper must avoid.
	emittedIDs := map[string]int{}
	for _, m := range emitted {
		if m.Role == "tool" && m.ToolID != "" {
			emittedIDs[m.ToolID]++
		}
	}
	if emittedIDs["call-a"] != 0 {
		t.Errorf("OnMessage re-emitted the already-published %s %d time(s), want 0", "call-a", emittedIDs["call-a"])
	}
	for _, id := range []string{"call-b", "call-c"} {
		if emittedIDs[id] != 1 {
			t.Errorf("OnMessage emitted %s %d times, want 1", id, emittedIDs[id])
		}
	}

	assertNoDuplicateToolIDs(t, got)
	assertEveryCallAnswered(t, got)
}

// TestFinishCancelledRoundIgnoresTheZeroResultSlots pins the zero-Message rule:
// results is pre-sized to the batch, so an unexecuted slot is the zero Message.
// Treating it as "answered" would leave the call orphaned — the exact bug.
func TestFinishCancelledRoundIgnoresTheZeroResultSlots(t *testing.T) {
	tc := ToolCall{ID: "call-1", Type: "function"}
	tc.Function.Name = "never_ran"
	tc.Function.Arguments = `{}`

	a := NewAgent(nil, nil, nil, nil)
	resp := &Message{Role: "assistant", ToolCalls: []ToolCall{tc}}

	got, err := a.finishCancelledRound([]Message{*resp}, []Message{*resp}, resp, []Message{{}}, 0)
	if err != nil {
		t.Fatalf("finishCancelledRound err = %v", err)
	}
	assertEveryCallAnswered(t, got)
	if len(got) != 2 || got[1].Content != tool.ToolCancelledResult {
		t.Fatalf("got %+v, want the zero slot answered with %q", got, tool.ToolCancelledResult)
	}
}

// TestFinishCancelledRoundClampsPublishedBeyondResults proves the defensive
// clamp: a caller that reports more published entries than exist must not panic
// on results[published:] (which would take the process down mid-turn).
//
// newMsgs is seeded with the one real result, matching a caller that already
// published it — the clamp then correctly emits nothing more.
func TestFinishCancelledRoundClampsPublishedBeyondResults(t *testing.T) {
	tc := ToolCall{ID: "call-1", Type: "function"}
	tc.Function.Name = "alpha"
	tc.Function.Arguments = `{}`

	a := NewAgent(nil, nil, nil, nil)
	resp := &Message{Role: "assistant", ToolCalls: []ToolCall{tc}}
	results := []Message{{Role: "tool", ToolID: "call-1", Content: "done"}}

	got, err := a.finishCancelledRound([]Message{*resp, results[0]}, []Message{*resp, results[0]}, resp, results, 99)
	if err != nil {
		t.Fatalf("finishCancelledRound err = %v", err)
	}
	assertNoDuplicateToolIDs(t, got)
	assertEveryCallAnswered(t, got)
}

// TestFinishCancelledRoundIsIdempotentPerCall guards the two-call-site shape:
// the helper must never append a second result for an id it already answered,
// even when the caller hands it a results slice that overlaps newMsgs.
func TestFinishCancelledRoundIsIdempotentPerCall(t *testing.T) {
	tc := ToolCall{ID: "call-1", Type: "function"}
	tc.Function.Name = "alpha"
	tc.Function.Arguments = `{}`

	a := NewAgent(nil, nil, nil, nil)
	resp := &Message{Role: "assistant", ToolCalls: []ToolCall{tc}}
	results := []Message{{Role: "tool", ToolID: "call-1", Content: "done"}}

	// Two sequential exits both believing they own the whole round must not
	// double-emit: the second sees published == len(results) and adds nothing.
	first, _ := a.finishCancelledRound([]Message{*resp}, []Message{*resp}, resp, results, 0)
	second, _ := a.finishCancelledRound(first, append([]Message{*resp}, first[1:]...), resp, results, len(results))

	assertNoDuplicateToolIDs(t, second)
	if n := len(second) - len(first); n != 0 {
		t.Errorf("second call appended %d extra message(s), want 0:\n%+v", n, second)
	}
}

// finishCancelledRound must not append into the caller's results backing array
// when it has spare capacity.
func TestFinishCancelledRound_DoesNotMutateResultsBackingArray(t *testing.T) {
	a := &Agent{}
	resp := &Message{Role: "assistant", ToolCalls: []ToolCall{{ID: "call-a"}, {ID: "call-b"}}}
	backing := make([]Message, 1, 4)
	backing[0] = Message{Role: "tool", ToolID: "call-a", Content: "ok"}
	results := backing[:1]
	if _, err := a.finishCancelledRound([]Message{*resp}, []Message{*resp}, resp, results, 0); err != nil {
		t.Fatalf("finishCancelledRound err = %v", err)
	}
	if got := backing[:2][1]; got.ToolID != "" || got.Content != "" {
		t.Errorf("results backing array was written to: %+v", got)
	}
}
