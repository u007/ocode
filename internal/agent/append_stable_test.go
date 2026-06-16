package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/notebus"
)

// TestContextAppendStable_NoBus confirms the cache-stability
// invariant: when the agent has no bus (and no other
// per-loop volatile state), two consecutive Step invocations
// with the same input produce byte-identical message slices
// before the tail. The "tail" is whatever volatile injection
// runs at the end of the loop; in the no-bus case that is
// nothing, so the entire slice must match.
//
// This test exists to lock down the invariant before we add
// any per-loop volatile content. If a future change re-orders
// or re-renders stable content (e.g. swapping out a context
// loader), this test fails.
func TestContextAppendStable_NoBus(t *testing.T) {
	a := NewAgent(&MockClient{}, nil, nil, nil)
	in := []Message{
		{Role: "user", Content: "hello"},
	}
	// Use PrepareMessages (the stable-content assembler) to
	// generate the prefix. Without a bus and without other
	// per-loop volatile inputs, two calls must produce
	// identical output.
	first := a.PrepareMessages(in, "")
	second := a.PrepareMessages(in, "")
	if !messagesEqual(first, second) {
		t.Errorf("PrepareMessages is not stable across calls:\n--- first ---\n%s\n--- second ---\n%s",
			dumpMessages(first), dumpMessages(second))
	}
}

// TestContextAppendStable_NotesTailInert confirms the
// per-loop notes tail does not corrupt the stable prefix
// when the bus is empty. Two calls with the same input must
// produce byte-identical message slices — the <oc-log> block
// is not added (no delta), so the slice is the same.
func TestContextAppendStable_NotesTailInert(t *testing.T) {
	bus := notebus.NewBus("grp")
	a := NewAgent(&MockClient{}, nil, nil, nil)
	a.SetNoteBus(bus, "a2")
	bus.Start(context.Background())
	defer func() { bus.Stop(); <-bus.Done() }()

	// First call: no new entries → no block → no change.
	first := injectNotesTail([]Message{{Role: "user", Content: "x"}}, a)
	second := injectNotesTail([]Message{{Role: "user", Content: "x"}}, a)
	if !messagesEqual(first, second) {
		t.Errorf("inert notes tail not stable:\n--- first ---\n%s\n--- second ---\n%s",
			dumpMessages(first), dumpMessages(second))
	}
	if strings.Contains(dumpMessages(first), "<oc-log ") {
		t.Errorf("empty-delta path produced a block:\n%s", dumpMessages(first))
	}
}

// TestContextAppendStable_BlockOnlyInTail confirms that when a
// delta IS injected, the BLOCK is the only difference between
// the prior and the new message slice. The prefix (everything
// before the block) must be byte-identical to what it was
// before the new entries appeared. This is the literal
// definition of "stable prefix + volatile tail".
func TestContextAppendStable_BlockOnlyInTail(t *testing.T) {
	bus := notebus.NewBus("grp")
	a := NewAgent(&MockClient{}, nil, nil, nil)
	a.SetNoteBus(bus, "a2")
	bus.Start(context.Background())
	defer func() { bus.Stop(); <-bus.Done() }()

	base := []Message{
		{Role: "system", Content: "STABLE-SYS"},
		{Role: "user", Content: "STABLE-USER"},
	}
	// First call: no entries, no block.
	first := injectNotesTail(base, a)
	// Add an entry. Second call: block appears at tail.
	if _, err := bus.Append(notebus.Note(0, "a1", "x.go", "new", 0)); err != nil {
		t.Fatal(err)
	}
	second := injectNotesTail(base, a)

	if len(first) >= len(second) {
		t.Fatalf("len(first)=%d, len(second)=%d (second should be longer by 1)",
			len(first), len(second))
	}
	// Compare the first len(first) messages — the prefix.
	for i := 0; i < len(first); i++ {
		if first[i].Content != second[i].Content {
			t.Errorf("prefix[%d] content changed:\n--- first ---\n%s\n--- second ---\n%s",
				i, first[i].Content, second[i].Content)
		}
	}
	// The new last message must be the block.
	last := second[len(second)-1]
	if !strings.Contains(last.Content, "<oc-log ") {
		t.Errorf("new tail is not a notes block:\n%s", last.Content)
	}
}

// TestContextAppendStable_BlockReplacedCleanly: a second
// loop with new entries produces a NEW block at the tail
// (replacing the old one), not a growing list of blocks.
// The cached prefix is the prior prefix + new block; the
// OLD block is dropped (it was already part of the
// transcript, and re-injecting it would double-count).
func TestContextAppendStable_BlockReplacedCleanly(t *testing.T) {
	bus := notebus.NewBus("grp")
	a := NewAgent(&MockClient{}, nil, nil, nil)
	a.SetNoteBus(bus, "a2")
	bus.Start(context.Background())
	defer func() { bus.Stop(); <-bus.Done() }()

	// First delta: one entry.
	if _, err := bus.Append(notebus.Note(0, "a1", "x.go", "first-note", 0)); err != nil {
		t.Fatal(err)
	}
	first := injectNotesTail([]Message{{Role: "user", Content: "x"}}, a)
	if c := strings.Count(dumpMessages(first), "<oc-log "); c != 1 {
		t.Errorf("first <oc-log> count = %d, want 1", c)
	}

	// Advance watermark by calling Delta (the bus auto-advances
	// on every Delta call).
	_ = bus.Delta("a2")

	// Second delta: another entry.
	if _, err := bus.Append(notebus.Note(0, "a3", "y.go", "second-note", 0)); err != nil {
		t.Fatal(err)
	}
	second := injectNotesTail([]Message{{Role: "user", Content: "x"}}, a)
	// Still exactly one <oc-log> block (the new one).
	if c := strings.Count(dumpMessages(second), "<oc-log "); c != 1 {
		t.Errorf("second <oc-log> count = %d, want 1 (block replaced, not appended)", c)
	}
	if strings.Contains(dumpMessages(second), "first-note") {
		t.Errorf("second block still contains the FIRST note (should be replaced):\n%s",
			dumpMessages(second))
	}
	if !strings.Contains(dumpMessages(second), "second-note") {
		t.Errorf("second block missing the new note:\n%s", dumpMessages(second))
	}
}

// TestContextAppendStable_StableContentNotReordered guards
// against regressions where a future change reorders stable
// fragments (e.g. swaps system prompt and context). The
// prefix order must be the same across calls.
func TestContextAppendStable_StableContentNotReordered(t *testing.T) {
	a := NewAgent(&MockClient{}, nil, nil, nil)
	in := []Message{
		{Role: "user", Content: "hello"},
	}
	first := a.PrepareMessages(in, "")
	// Check that the first system message is the environment
	// prompt (the canonical first fragment). If a future
	// change reorders, this assertion catches it.
	if len(first) == 0 {
		t.Fatal("PrepareMessages returned no messages")
	}
	if first[0].Role != "system" {
		t.Errorf("first message role = %q, want system", first[0].Role)
	}
	if !strings.Contains(first[0].Content, "[ocode:environment]") {
		t.Errorf("first system message missing environment marker:\n%s", first[0].Content)
	}
}

// messagesEqual returns true iff two message slices have the
// same length and identical Content on every position. Role
// is checked too; other fields (ToolID, ToolCalls, …) are
// not relevant for cache stability and are not asserted.
func messagesEqual(a, b []Message) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Role != b[i].Role {
			return false
		}
		if a[i].Content != b[i].Content {
			return false
		}
	}
	return true
}

// dumpMessages is a debug helper for failure messages.
func dumpMessages(ms []Message) string {
	var b strings.Builder
	for i, m := range ms {
		b.WriteString("[")
		// idx + role in one tag
		b.WriteString(itoa(i))
		b.WriteString(":")
		b.WriteString(m.Role)
		b.WriteString("] ")
		b.WriteString(m.Content)
		b.WriteString("\n")
	}
	return b.String()
}

// itoa is a tiny no-allocation int-to-string for the dump helper.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
