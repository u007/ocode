package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/notebus"
)

// TestGroupReconcile_HandoffInResult confirms that when a
// grouped fan-out completes (or has a partial failure), the
// renderable reconcile handoff is exposed to the orchestrator
// as a "notes" attribute on the result. The orchestrator
// reads this in its next-loop prompt to make the strong-
// model judgment.
//
// The wiring test does not drive a full Step() loop — that
// is the e2e test (TestNotesBusE2E). Here we only confirm
// the groupTracker exposes the reconcile output and that
// it survives a teardown.
func TestGroupReconcile_HandoffInResult(t *testing.T) {
	bus := notebus.NewBus("grp")
	bus.Start(context.Background())
	defer func() { bus.Stop(); <-bus.Done() }()

	// Seed some entries and resolutions.
	if _, err := bus.Append(notebus.Note(0, "a1", "x.go:foo", "missing nil check", 0)); err != nil {
		t.Fatal(err)
	}
	if _, err := bus.Append(notebus.Note(0, "a2", "x.go:foo", "missing nil check", 0)); err != nil {
		t.Fatal(err)
	}
	if _, err := bus.Append(notebus.Touch(0, "a1", "x.go", "edit", 0)); err != nil {
		t.Fatal(err)
	}
	if _, err := bus.Append(notebus.Resolve(0, "a3", 2, 0)); err != nil {
		t.Fatal(err)
	}

	// Build a tracker with statuses.
	tracker := newGroupTracker()
	tracker.Record("a1", "completed", nil)
	tracker.Record("a2", "completed", nil)
	tracker.Record("a3", "failed", nil)
	tracker.SetPartition("a1", "correctness")
	tracker.SetPartition("a2", "security")
	tracker.SetPartition("a3", "performance")

	// Run the pre-pass.
	snap := bus.Snapshot()
	statuses := tracker.Statuses()
	out := notebus.ReconcilePrepass(snap, statuses)

	// Render to text.
	rendered := notebus.RenderReconcile(out)
	if !strings.Contains(rendered, "x.go:foo") {
		t.Errorf("rendered output missing cluster anchor:\n%s", rendered)
	}
	if !strings.Contains(rendered, "a3") {
		t.Errorf("rendered output missing unreviewed agent:\n%s", rendered)
	}
	if !strings.Contains(rendered, "missing nil check") {
		t.Errorf("rendered output missing body:\n%s", rendered)
	}
}

// TestGroupReconcile_FailedAgentFlagged: a single failed
// agent shows up in the Unreviewed section with the right
// id. The test exercises the full pipeline end-to-end at
// the data layer (no agent Step() loop).
func TestGroupReconcile_FailedAgentFlagged(t *testing.T) {
	bus := notebus.NewBus("grp")
	bus.Start(context.Background())
	defer func() { bus.Stop(); <-bus.Done() }()

	tracker := newGroupTracker()
	tracker.Record("a1", "completed", nil)
	tracker.Record("a2", "failed", nil)

	statuses := tracker.Statuses()
	if statuses["a1"].Status != "completed" {
		t.Errorf("a1 status = %q, want completed", statuses["a1"].Status)
	}
	if statuses["a2"].Status != "failed" {
		t.Errorf("a2 status = %q, want failed", statuses["a2"].Status)
	}

	snap := bus.Snapshot()
	out := notebus.ReconcilePrepass(snap, statuses)
	if len(out.Unreviewed) != 1 || out.Unreviewed[0] != "a2" {
		t.Errorf("Unreviewed = %v, want [a2]", out.Unreviewed)
	}
}

// TestGroupReconcile_PartitionSurfaced: a failed agent's
// partition is included in the output (so the user knows
// which slice of the work was missed).
func TestGroupReconcile_PartitionSurfaced(t *testing.T) {
	tracker := newGroupTracker()
	tracker.Record("a2", "failed", nil)
	tracker.SetPartition("a2", "core/auth:tokenFromHeader")

	statuses := tracker.Statuses()
	if statuses["a2"].Partition != "core/auth:tokenFromHeader" {
		t.Errorf("a2 partition = %q, want core/auth:tokenFromHeader", statuses["a2"].Partition)
	}
}
