package tui

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/u007/ocode/internal/agent"
)

// compactFailureModel builds a session part-way through a compaction that has a
// deferred turn. It goes through newModel rather than a bare model literal
// because the composer textarea's internal viewport is only initialised there,
// and the restore path writes to the composer.
func compactFailureModel() model {
	m := newTestModel()
	m.ready = true
	sized, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if resized, ok := sized.(model); ok {
		m = resized
	}
	m.agent = newTestAgent(fakeCompactSummaryClient{}, nil, compactCfg(), nil)
	m.messages = []message{{role: roleUser, text: "one"}, {role: roleAssistant, text: "two"}}
	m.pendingCompactUIIdx = []int{-1, 0, 1}
	m.pendingCompactResume = true
	return m
}

func compactFailureResult() agent.CompactResult {
	return agent.CompactResult{Err: errors.New("compact: summary timed out: context deadline exceeded")}
}

// Both compaction trigger points (the askAgent pre-flight and the post-turn
// stream-done check) defer the turn by setting pendingCompactResume, and
// compactFinishedMsg used to honour it unconditionally. A pass that failed did
// not shrink the context, so the re-dispatch re-sent the same oversized prompt
// and immediately re-armed the compaction that had just failed — one warning
// per turn, forever, until the user switched sessions.
//
// The contract is "after a failure nothing auto-dispatches": no deferred turn is
// re-sent, and no queued command is drained. A drained command would also reset
// the composer, wiping the text just restored into it.
func TestFailedCompactionDoesNotResumeTheDeferredTurn(t *testing.T) {
	m := compactFailureModel()
	m.queuedItems = []queuedItem{{kind: queueItemInput, text: "queued while compacting"}}
	before := len(m.messages)

	updated, _ := m.Update(compactFinishedMsg{result: compactFailureResult()})
	got := updated.(model)

	if got.pendingCompactResume {
		t.Fatal("the resume flag must be consumed, not left armed")
	}
	if got.pendingCompactUIIdx != nil {
		t.Fatal("the pending compaction mapping must be cleared after a failure")
	}
	// The only new transcript lines are the failure notice and the restore hint.
	// A re-dispatched turn or a drained command would have added more.
	var added []string
	for _, msg := range got.messages[before:] {
		added = append(added, msg.text)
	}
	for _, text := range added {
		if !strings.Contains(text, "Compaction failed") && !strings.Contains(text, "returned to the input box") {
			t.Fatalf("a failed compaction dispatched work: unexpected transcript line %q", text)
		}
	}
	// Text submissions move to the composer; only commands stay parked.
	if !strings.Contains(got.input.Value(), "queued while compacting") {
		t.Fatalf("the queued message must be restored to the composer, got %q", got.input.Value())
	}
	for _, item := range got.queuedItems {
		if item.kind != queueItemCommand {
			t.Fatalf("text items must leave the queue, got %#v", got.queuedItems)
		}
	}
}

// Control for the assertion above. Without it, "nothing was dispatched" could
// pass for the wrong reason — e.g. the tail being dead for every result. A
// SUCCESSFUL pass must still drain the queue and resume the deferred turn.
func TestSuccessfulCompactionStillDrainsAndResumes(t *testing.T) {
	m := compactFailureModel()
	m.queuedItems = []queuedItem{{kind: queueItemInput, text: "queued while compacting"}}
	snapshot, uiIdx := m.buildAgentMessagesSnapshot()
	result, enabled := m.agent.Compact(snapshot)
	if !enabled || !result.OK {
		t.Fatalf("expected a compactable result, enabled=%v result=%+v", enabled, result)
	}
	m.pendingCompactUIIdx = uiIdx
	before := len(m.messages)

	updated, _ := m.Update(compactFinishedMsg{result: result})
	got := updated.(model)

	if got.pendingCompactResume {
		t.Fatal("the resume flag must be consumed")
	}
	if len(got.queuedItems) != 0 {
		t.Fatalf("a successful compaction must still drain the queue, got %#v", got.queuedItems)
	}
	if len(got.messages) <= before {
		t.Fatal("a successful compaction must still dispatch the queued message as a turn")
	}
}

// Nothing the user submitted may be silently dropped when a pass fails. The
// queue is the only place those messages live at that point, so they go back
// into the composer, joined with a newline and placed BEFORE whatever the user
// has since typed — their draft stays intact at the end.
func TestFailedCompactionRestoresQueuedMessagesIntoTheComposer(t *testing.T) {
	m := compactFailureModel()
	m.queuedItems = []queuedItem{
		{kind: queueItemInput, text: "first queued"},
		{kind: queueItemCompactInput, text: "second queued"},
		{kind: queueItemCommand, text: "/compact"}, // a command is not composer prose
	}
	m.input.SetValue("draft the user is typing")

	updated, _ := m.Update(compactFinishedMsg{result: compactFailureResult()})
	got := updated.(model)

	want := "first queued\nsecond queued\ndraft the user is typing"
	if got.input.Value() != want {
		t.Fatalf("composer after a failed compaction:\n got %q\nwant %q", got.input.Value(), want)
	}
	// The command stays queued so it remains dispatchable — it is not text the
	// user was drafting, and inlining it would corrupt the joined block.
	if len(got.queuedItems) != 1 {
		t.Fatalf("expected only the command to remain queued, got %#v", got.queuedItems)
	}
	if got.queuedItems[0].kind != queueItemCommand || got.queuedItems[0].text != "/compact" {
		t.Fatalf("queued command was altered: %#v", got.queuedItems[0])
	}
}

// A message typed while a turn was streaming is ALSO handed to the live agent
// loop (EnqueueInjection) and kept in the queue only so it can be recalled.
// Restoring it to the composer without dropping the injection would send it
// twice: once spliced into the next turn, once as the retyped message.
func TestFailedCompactionDropsTheMatchingPendingInjection(t *testing.T) {
	m := compactFailureModel()
	m.queuedItems = []queuedItem{{kind: queueItemInput, text: "injected while streaming"}}
	m.agent.EnqueueInjection(agent.Message{Role: "user", Content: "injected while streaming"})
	if !m.agent.HasPendingInjections() {
		t.Fatal("precondition: the injection should be pending")
	}

	updated, _ := m.Update(compactFinishedMsg{result: compactFailureResult()})
	got := updated.(model)

	if !strings.Contains(got.input.Value(), "injected while streaming") {
		t.Fatalf("expected the message restored to the composer, got %q", got.input.Value())
	}
	if got.agent.HasPendingInjections() {
		t.Fatal("the matching pending injection survived — the restored message would be sent twice")
	}
}

func TestMergeQueuedIntoDraft(t *testing.T) {
	tests := []struct {
		name       string
		queued     []string
		draft      string
		want       string
		wantOffset int // rune index where the user's own draft begins
	}{
		{
			name:       "empty draft",
			queued:     []string{"one", "two"},
			draft:      "",
			want:       "one\ntwo",
			wantOffset: 7,
		},
		{
			name:       "draft is kept as the suffix",
			queued:     []string{"queued"},
			draft:      "typing away",
			want:       "queued\ntyping away",
			wantOffset: 7,
		},
		{
			name:       "no queued text leaves the draft alone",
			queued:     nil,
			draft:      "typing away",
			want:       "typing away",
			wantOffset: 0,
		},
		{
			name:       "insertion order is preserved",
			queued:     []string{"a", "b", "c"},
			draft:      "z",
			want:       "a\nb\nc\nz",
			wantOffset: 6,
		},
		{
			name:       "blank queued entries are dropped, not turned into blank lines",
			queued:     []string{"a", "   ", "b"},
			draft:      "z",
			want:       "a\nb\nz",
			wantOffset: 4,
		},
		{
			name:       "multibyte draft keeps the offset in runes",
			queued:     []string{"héllo"},
			draft:      "ünïcödé",
			want:       "héllo\nünïcödé",
			wantOffset: 6,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, offset := mergeQueuedIntoDraft(tc.queued, tc.draft)
			if got != tc.want {
				t.Fatalf("mergeQueuedIntoDraft(%q, %q) = %q, want %q", tc.queued, tc.draft, got, tc.want)
			}
			if offset != tc.wantOffset {
				t.Fatalf("draft offset = %d, want %d (merged %q)", offset, tc.wantOffset, got)
			}
			// The offset must actually index the user's own draft.
			if r := []rune(got); offset > len(r) || string(r[offset:]) != tc.draft {
				t.Fatalf("offset %d does not land on the draft %q within %q", offset, tc.draft, got)
			}
		})
	}
}
