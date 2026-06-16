package agent

import (
	"strings"
	"testing"

	"github.com/u007/ocode/internal/notebus"
)

// TestGroupReconcile_HandoffTextStable: the rendered
// hand-off is byte-stable for the same input. Two calls
// with the same bus + tracker produce the same text. This
// is the prompt-cache-stability guarantee.
func TestGroupReconcile_HandoffTextStable(t *testing.T) {
	bus := notebus.NewBus("grp")
	// Don't start the bus; we're just reading the snapshot.
	// The bus is unstarted but we can still append after
	// starting, then read the snapshot.
	_ = bus
	bus2 := notebus.NewBus("grp")
	bus2.Start(t.Context())
	defer func() { bus2.Stop(); <-bus2.Done() }()
	if _, err := bus2.Append(notebus.Note(0, "a1", "x.go", "test", 0)); err != nil {
		t.Fatal(err)
	}

	tracker := newGroupTracker()
	tracker.Record("a1", "completed", nil)

	m1 := reconcileHandoffMessage(bus2, tracker)
	if m1 == nil {
		t.Fatal("first handoff is nil")
	}
	m2 := reconcileHandoffMessage(bus2, tracker)
	if m2 == nil {
		t.Fatal("second handoff is nil")
	}
	if m1.Content != m2.Content {
		t.Errorf("handoff is not stable:\n--- m1 ---\n%s\n--- m2 ---\n%s", m1.Content, m2.Content)
	}
}

// TestGroupReconcile_HandoffIncludesMarker: the handoff
// message has the [ocode:reconcile] marker so the
// orchestrator's prompt assembler can recognize it and
// route it correctly.
func TestGroupReconcile_HandoffIncludesMarker(t *testing.T) {
	bus := notebus.NewBus("grp")
	bus.Start(t.Context())
	defer func() { bus.Stop(); <-bus.Done() }()
	if _, err := bus.Append(notebus.Note(0, "a1", "x.go", "test", 0)); err != nil {
		t.Fatal(err)
	}
	tracker := newGroupTracker()
	tracker.Record("a1", "completed", nil)

	m := reconcileHandoffMessage(bus, tracker)
	if m == nil {
		t.Fatal("handoff is nil")
	}
	if !strings.HasPrefix(m.Content, "[ocode:reconcile]") {
		t.Errorf("handoff missing [ocode:reconcile] marker:\n%s", m.Content)
	}
}

// TestGroupReconcile_NilBusReturnsNil: a nil bus returns
// nil (no handoff). The caller skips appending any
// message. The non-group path is a complete no-op.
func TestGroupReconcile_NilBusReturnsNil(t *testing.T) {
	tracker := newGroupTracker()
	tracker.Record("a1", "completed", nil)
	if m := reconcileHandoffMessage(nil, tracker); m != nil {
		t.Errorf("nil bus returned non-nil handoff: %+v", m)
	}
}

// TestGroupReconcile_EmptyGroupReturnsNil: a bus with no
// entries AND a tracker with no records returns nil. The
// orchestrator does not need a handoff for an empty group
// (no notes, no agents, nothing to reconcile).
func TestGroupReconcile_EmptyGroupReturnsNil(t *testing.T) {
	bus := notebus.NewBus("grp")
	bus.Start(t.Context())
	defer func() { bus.Stop(); <-bus.Done() }()
	tracker := newGroupTracker()
	if m := reconcileHandoffMessage(bus, tracker); m != nil {
		t.Errorf("empty group returned non-nil handoff: %+v", m)
	}
}
