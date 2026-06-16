package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/notebus"
)

// TestDeltaInjection_Basic confirms a child with a non-empty
// delta gets a single <oc-log since="N">…</oc-log> block at
// the very tail of the assembled context. The block contains
// only the agent's delta (seq > lastSeen AND by != agent),
// resolved notes excluded. The agent's own notes are not
// included.
func TestDeltaInjection_Basic(t *testing.T) {
	bus := notebus.NewBus("grp")
	child := NewAgent(&MockClient{}, nil, nil, nil)
	child.SetNoteBus(bus, "a2")
	bus.Start(context.Background())
	defer func() { bus.Stop(); <-bus.Done() }()

	// Seed: a1 writes a note, a3 writes a note. a2 is the
	// observer in this test.
	if _, err := bus.Append(notebus.Note(0, "a1", "x.go", "from a1", 0)); err != nil {
		t.Fatal(err)
	}
	if _, err := bus.Append(notebus.Note(0, "a3", "y.go", "from a3", 0)); err != nil {
		t.Fatal(err)
	}

	// Render the delta block for a2.
	baseMessages := []Message{
		{Role: "system", Content: "[ocode:environment]\n<env>\n  ...\n</env>"},
		{Role: "user", Content: "hello"},
	}
	out := injectNotesTail(baseMessages, child)
	joined := concatMessages(out)

	// The block must appear at the tail, exactly once.
	if c := strings.Count(joined, "<oc-log "); c != 1 {
		t.Errorf("<oc-log count = %d, want 1", c)
	}
	if c := strings.Count(joined, "</oc-log>"); c != 1 {
		t.Errorf("</oc-log> count = %d, want 1", c)
	}
	// The block must contain both notes (a1 and a3 wrote).
	if !strings.Contains(joined, "from a1") {
		t.Errorf("delta block missing a1's note:\n%s", joined)
	}
	if !strings.Contains(joined, "from a3") {
		t.Errorf("delta block missing a3's note:\n%s", joined)
	}
	// The block must reference a2's own authored notes as
	// never appearing in its own delta. (a2 hasn't written
	// anything yet, so this is trivially true here, but
	// TestDeltaInjection_OwnNotesNeverReInjected covers the
	// real case.)
}

// TestDeltaInjection_EmptyInjectsNothing confirms a child with
// an empty delta (nothing new since last loop) gets NO block
// at all. This is the cache-stability invariant: no per-loop
// volatility means the prefix is byte-identical to the prior
// loop.
func TestDeltaInjection_EmptyInjectsNothing(t *testing.T) {
	bus := notebus.NewBus("grp")
	child := NewAgent(&MockClient{}, nil, nil, nil)
	child.SetNoteBus(bus, "a2")
	bus.Start(context.Background())
	defer func() { bus.Stop(); <-bus.Done() }()

	// First, drive a delta so the watermark advances past head.
	// The bus auto-advances lastSeen on every Delta call.
	_ = bus.Delta("a2")

	// Now no new entries exist. The next delta is empty.
	base := []Message{
		{Role: "system", Content: "[ocode:environment]\n<env>\n  ...\n</env>"},
		{Role: "user", Content: "hello"},
	}
	out := injectNotesTail(base, child)
	if len(out) != len(base) {
		t.Errorf("empty-delta injection len = %d, want %d (no new message)",
			len(out), len(base))
	}
	// Joined bytes are identical to the input (no new tail).
	got := concatMessages(out)
	want := concatMessages(base)
	if got != want {
		t.Errorf("empty-delta injection altered prefix:\n--- got ---\n%s\n--- want ---\n%s",
			got, want)
	}
}

// TestDeltaInjection_OwnNotesNeverReInjected confirms that a
// child never sees its own notes in the injected block. The
// design says: "an agent's own notes are already in its
// transcript; re-injecting them wastes tokens and
// double-counts."
func TestDeltaInjection_OwnNotesNeverReInjected(t *testing.T) {
	bus := notebus.NewBus("grp")
	child := NewAgent(&MockClient{}, nil, nil, nil)
	child.SetNoteBus(bus, "a2")
	bus.Start(context.Background())
	defer func() { bus.Stop(); <-bus.Done() }()

	// a2 authors two notes. a1 authors one. We inject a2's
	// tail: only a1's note should appear.
	if _, err := bus.Append(notebus.Note(0, "a2", "x.go", "own-1", 0)); err != nil {
		t.Fatal(err)
	}
	if _, err := bus.Append(notebus.Note(0, "a2", "y.go", "own-2", 0)); err != nil {
		t.Fatal(err)
	}
	if _, err := bus.Append(notebus.Note(0, "a1", "z.go", "peer", 0)); err != nil {
		t.Fatal(err)
	}

	out := injectNotesTail([]Message{{Role: "user", Content: "hi"}}, child)
	joined := concatMessages(out)
	if strings.Contains(joined, "own-1") || strings.Contains(joined, "own-2") {
		t.Errorf("a2's own notes appeared in its injected block:\n%s", joined)
	}
	if !strings.Contains(joined, "peer") {
		t.Errorf("peer's note missing from a2's injected block:\n%s", joined)
	}
}

// TestDeltaInjection_BlockAtTail confirms the <oc-log> block
// sits AFTER all stable content. Stable content here = the
// system prompt and the user message. A regression that
// interleaves the block earlier (e.g. before the system
// prompt) would bust the cache and fail this test.
func TestDeltaInjection_BlockAtTail(t *testing.T) {
	bus := notebus.NewBus("grp")
	child := NewAgent(&MockClient{}, nil, nil, nil)
	child.SetNoteBus(bus, "a2")
	bus.Start(context.Background())
	defer func() { bus.Stop(); <-bus.Done() }()

	if _, err := bus.Append(notebus.Note(0, "a1", "x.go", "from a1", 0)); err != nil {
		t.Fatal(err)
	}

	base := []Message{
		{Role: "system", Content: "[ocode:environment]\nSTABLE-SYS"},
		{Role: "user", Content: "STABLE-USER"},
	}
	out := injectNotesTail(base, child)
	if len(out) != len(base)+1 {
		t.Fatalf("out len = %d, want %d (base + 1 block)", len(out), len(base)+1)
	}
	// Last message must be the <oc-log> system message.
	last := out[len(out)-1]
	if last.Role != "system" {
		t.Errorf("last message role = %q, want system", last.Role)
	}
	if !strings.Contains(last.Content, "<oc-log ") {
		t.Errorf("last message missing <oc-log>:\n%s", last.Content)
	}
	// Stable content must be byte-identical to the input.
	if out[0].Content != base[0].Content {
		t.Errorf("first message content changed:\n--- got ---\n%s\n--- want ---\n%s",
			out[0].Content, base[0].Content)
	}
	if out[1].Content != base[1].Content {
		t.Errorf("second message content changed")
	}
}

// TestDeltaInjection_ResolvedNotesExcluded confirms a resolved
// note never appears in a peer's delta, even after the resolve
// is recorded. The bus suppresses resolved notes on read
// (verified in TestResolvedNotesExcludedFromDelta); this test
// pins the wire-through to the injection layer.
func TestDeltaInjection_ResolvedNotesExcluded(t *testing.T) {
	bus := notebus.NewBus("grp")
	child := NewAgent(&MockClient{}, nil, nil, nil)
	child.SetNoteBus(bus, "a2")
	bus.Start(context.Background())
	defer func() { bus.Stop(); <-bus.Done() }()

	if _, err := bus.Append(notebus.Note(0, "a1", "x.go", "to-resolve", 0)); err != nil {
		t.Fatal(err)
	}
	// a3 resolves seq=1.
	if _, err := bus.Append(notebus.Resolve(0, "a3", 1, 0)); err != nil {
		t.Fatal(err)
	}

	out := injectNotesTail([]Message{{Role: "user", Content: "hi"}}, child)
	joined := concatMessages(out)
	if strings.Contains(joined, "to-resolve") {
		t.Errorf("resolved note still in delta:\n%s", joined)
	}
	// No <oc-log> block should be present (delta is empty).
	if strings.Contains(joined, "<oc-log ") {
		t.Errorf("<oc-log> block present with empty delta:\n%s", joined)
	}
}

// TestDeltaInjection_NoBusNoBlock confirms the injection helper
// is a no-op when the agent has no bus. The non-group path
// must not add or alter any messages.
func TestDeltaInjection_NoBusNoBlock(t *testing.T) {
	child := NewAgent(&MockClient{}, nil, nil, nil)
	// No SetNoteBus. The tail helper is a no-op.
	base := []Message{
		{Role: "system", Content: "STABLE"},
		{Role: "user", Content: "hi"},
	}
	out := injectNotesTail(base, child)
	if len(out) != len(base) {
		t.Errorf("out len = %d, want %d (no bus → no block)", len(out), len(base))
	}
	for i := range base {
		if out[i].Content != base[i].Content {
			t.Errorf("message %d content changed", i)
		}
	}
}

// concatMessages joins message contents with markers so a
// test can substring-search the assembled context. The
// contents are joined in order with role tags so a test can
// see the structural shape (system vs user vs tool).
func concatMessages(ms []Message) string {
	var b strings.Builder
	for i, m := range ms {
		fmt.Fprintf(&b, "[%d:%s] %s\n", i, m.Role, m.Content)
	}
	_ = b.String() // keep import used if compiler complains
	return b.String()
}
