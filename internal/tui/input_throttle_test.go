package tui

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func TestWheelThrottleCoalescesFlood(t *testing.T) {
	w := wheelThrottle{interval: time.Second / 60} // ~16.6ms frame
	base := time.Unix(0, 0)

	// First event in a fresh window always passes.
	if !w.allow(base) {
		t.Fatal("first wheel event should pass")
	}
	// A flood of events 2ms apart within the same frame must be dropped.
	for i := 1; i <= 5; i++ {
		at := base.Add(time.Duration(i) * 2 * time.Millisecond)
		if w.allow(at) {
			t.Fatalf("event at +%dms should be coalesced (dropped)", i*2)
		}
	}
	// Once a full frame has elapsed, the next event passes again.
	if !w.allow(base.Add(20 * time.Millisecond)) {
		t.Fatal("event after one frame interval should pass")
	}
}

func TestWheelThrottleSlowScrollNeverDropped(t *testing.T) {
	w := wheelThrottle{interval: time.Second / 60}
	now := time.Unix(0, 0)
	// Deliberate single notches 100ms apart are never throttled.
	for i := 0; i < 4; i++ {
		now = now.Add(100 * time.Millisecond)
		if !w.allow(now) {
			t.Fatalf("slow deliberate scroll at iteration %d should pass", i)
		}
	}
}

func TestInputFilterOnlyThrottlesWheelAndMotion(t *testing.T) {
	filter := newInputFilter()
	// Non-wheel/non-motion messages always pass through unchanged.
	for i := 0; i < 3; i++ {
		if got := filter(nil, tea.KeyPressMsg{}); got == nil {
			t.Fatalf("key press %d must not be dropped by the filter", i)
		}
	}
}

func TestInputFilterThrottlesMotion(t *testing.T) {
	// newInputFilter uses time.Now() internally, so we verify the shape:
	// a rapid burst of motion events is partially dropped (first passes, rest drop
	// until a full frame elapses).
	filter := newInputFilter()
	// First motion event always passes.
	if got := filter(nil, tea.MouseMotionMsg{}); got == nil {
		t.Fatal("first motion event must not be dropped")
	}
	// Immediately subsequent events (same real nanosecond) are dropped because
	// they arrive within the same frame window.
	dropped := 0
	for i := 0; i < 5; i++ {
		if filter(nil, tea.MouseMotionMsg{}) == nil {
			dropped++
		}
	}
	if dropped == 0 {
		t.Fatal("rapid motion flood should have at least one event dropped")
	}
}
