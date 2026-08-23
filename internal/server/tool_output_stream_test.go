package server

import (
	"strings"
	"testing"
	"time"
)

var streamEpoch = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// TestCoalescerHoldsSmallChunks confirms the coalescer does not forward every
// pipe write. A chatty command emits thousands of small chunks; one bus event
// each would flood a 256-slot buffer that is shared across every session and
// panel, starving unrelated events.
func TestCoalescerHoldsSmallChunks(t *testing.T) {
	c := newToolOutputCoalescer()

	payload, flush := c.add("call-1", "tick\n", streamEpoch)
	if flush {
		t.Fatalf("small chunk must not flush immediately, got payload %q", payload)
	}
}

// TestCoalescerFlushesOnByteThreshold verifies a burst is forwarded promptly
// rather than waiting for the timer, so high-volume output stays responsive.
func TestCoalescerFlushesOnByteThreshold(t *testing.T) {
	c := newToolOutputCoalescer()

	big := strings.Repeat("x", toolOutputFlushBytes+1)
	payload, flush := c.add("call-1", big, streamEpoch)
	if !flush {
		t.Fatal("crossing the byte threshold must flush")
	}
	if payload != big {
		t.Fatalf("flushed payload length %d, want %d", len(payload), len(big))
	}
}

// TestCoalescerFlushesOnInterval verifies slow trickling output still reaches
// the UI: a command emitting one line every few seconds must not sit in the
// buffer until it happens to cross the byte threshold.
func TestCoalescerFlushesOnInterval(t *testing.T) {
	c := newToolOutputCoalescer()

	if _, flush := c.add("call-1", "first\n", streamEpoch); flush {
		t.Fatal("first small chunk should buffer, not flush")
	}
	payload, flush := c.add("call-1", "second\n", streamEpoch.Add(toolOutputFlushInterval))
	if !flush {
		t.Fatal("a chunk arriving after the flush interval must flush")
	}
	if payload != "first\nsecond\n" {
		t.Fatalf("payload = %q, want the coalesced buffer", payload)
	}
}

// TestCoalescerCapsPerCall bounds how much live output one tool call can push
// through the bus. Past the cap the UI stops receiving chunks — the
// authoritative (separately truncated) tool_result still carries the real
// output, so nothing is lost, but the stream must say it stopped rather than
// appear to end naturally.
func TestCoalescerCapsPerCall(t *testing.T) {
	c := newToolOutputCoalescer()
	now := streamEpoch

	var got strings.Builder
	// Push well past the cap in threshold-sized bursts.
	for range (toolOutputPerCallCap / toolOutputFlushBytes) + 4 {
		payload, flush := c.add("call-1", strings.Repeat("y", toolOutputFlushBytes), now)
		if flush {
			got.WriteString(payload)
		}
		now = now.Add(time.Millisecond)
	}

	streamed := got.String()
	if len(streamed) > toolOutputPerCallCap+len(toolOutputCapNotice)+toolOutputFlushBytes {
		t.Fatalf("streamed %d bytes, must be bounded near the %d cap", len(streamed), toolOutputPerCallCap)
	}
	if !strings.Contains(streamed, toolOutputCapNotice) {
		t.Fatal("hitting the cap must emit a notice so the UI does not present a truncated stream as complete")
	}
	if strings.Count(streamed, toolOutputCapNotice) != 1 {
		t.Fatalf("cap notice must be emitted exactly once, got %d", strings.Count(streamed, toolOutputCapNotice))
	}
}

// TestCoalescerFinishFlushesRemainderAndForgetsCall covers both the tail of a
// short command (output below every threshold would otherwise never be sent)
// and the absence of a per-call leak: state must not accumulate for the life of
// the server process, which is exactly the shape of bug under investigation.
func TestCoalescerFinishFlushesRemainderAndForgetsCall(t *testing.T) {
	c := newToolOutputCoalescer()

	if _, flush := c.add("call-1", "trailing", streamEpoch); flush {
		t.Fatal("small chunk should buffer")
	}
	payload, ok := c.finish("call-1")
	if !ok {
		t.Fatal("finish must flush the buffered remainder")
	}
	if payload != "trailing" {
		t.Fatalf("payload = %q, want %q", payload, "trailing")
	}
	if n := c.activeCalls(); n != 0 {
		t.Fatalf("finish must forget the call, %d still tracked", n)
	}

	if _, ok := c.finish("call-1"); ok {
		t.Fatal("finishing an unknown call must not report a flush")
	}
}

// TestCoalescerDropSessionReleasesOrphanedCalls covers the case where a call's
// tool_result never arrives, so finish is never called for it: the agent's tool
// loop returns early on mid-batch cancellation, skipping OnMessage for results
// that had already completed. Without a turn-end sweep those buffers would stay
// in the map for the life of the process — on the desktop, the life of the app.
func TestCoalescerDropSessionReleasesOrphanedCalls(t *testing.T) {
	c := newToolOutputCoalescer()

	c.add("sess-a\x00call-1", "buffered", streamEpoch)
	c.add("sess-a\x00call-2", "buffered", streamEpoch)
	c.add("sess-b\x00call-3", "buffered", streamEpoch)
	if c.activeCalls() != 3 {
		t.Fatalf("expected 3 tracked calls, got %d", c.activeCalls())
	}

	c.dropSession("sess-a")

	if c.activeCalls() != 1 {
		t.Fatalf("dropSession must release only its own session's calls, %d remain", c.activeCalls())
	}
	if _, ok := c.finish("sess-b\x00call-3"); !ok {
		t.Fatal("an unrelated session's buffered call must survive dropSession")
	}
}
