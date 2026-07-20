package agent

import (
	"context"
	"testing"

	"github.com/u007/ocode/internal/notebus"
)

// TestWriteTouches verifies that a child in a group with a
// successful write/edit/apply_patch call appends exactly one
// touch per call to the bus, with the child's id and the target
// file path. Read/glob/grep produce no touches. No bus → no
// touches (no nil deref).
func TestWriteTouches(t *testing.T) {
	bus := notebus.NewBus("grp")
	child := NewAgent(&MockClient{}, nil, nil, nil)
	child.SetNoteBus(bus, "a1")
	bus.SetNow(func() int64 { return 1_700_000_000 })
	bus.Start(busBackground(t))
	defer func() { bus.Stop(); <-bus.Done() }()

	// Simulate the touch path for a write tool. We call the
	// helper directly so the test stays unit-focused on the
	// touch-appending logic, not the full Step loop.
	appendWriteTouchIfGrouped(child, "write", `{"path":"a.go","content":"x"}`)
	appendWriteTouchIfGrouped(child, "edit", `{"path":"b.go","oldtext":"a","newtext":"b"}`)
	appendWriteTouchIfGrouped(child, "apply_patch", `{"path":"c.go"}`)
	appendWriteTouchIfGrouped(child, "multi_file_edit", `{"edits":[{"path":"d.go","search":"x","replace":"y"},{"path":"e.go","search":"u","replace":"v"}]}`)
	// Read tools: no touch.
	appendWriteTouchIfGrouped(child, "read", `{"path":"d.go"}`)
	appendWriteTouchIfGrouped(child, "glob", `{"pattern":"*.go"}`)
	appendWriteTouchIfGrouped(child, "grep", `{"pattern":"foo"}`)

	busDelta := bus.Delta("a2") // from a2's perspective
	touches := 0
	for _, e := range busDelta {
		if e.Kind == notebus.KindTouch {
			touches++
		}
	}
	if touches != 5 {
		t.Errorf("touch count = %d, want 5 (write+edit+apply_patch+multi_file_edit paths)", touches)
	}
	wantFiles := map[string]bool{"a.go": false, "b.go": false, "c.go": false, "d.go": false, "e.go": false}
	for _, e := range busDelta {
		if e.Kind == notebus.KindTouch {
			if _, ok := wantFiles[e.File]; ok {
				wantFiles[e.File] = true
			}
		}
	}
	for f, seen := range wantFiles {
		if !seen {
			t.Errorf("expected touch for file %q, got none", f)
		}
	}
}

// TestWriteTouches_NoBusNoOp confirms that when the child has no
// bus, the touch path is a complete no-op (no panic, no work).
func TestWriteTouches_NoBusNoOp(t *testing.T) {
	child := NewAgent(&MockClient{}, nil, nil, nil)
	appendWriteTouchIfGrouped(child, "write", `{"path":"a.go","content":"x"}`)
	// Defensive regression guard: a future change that
	// unconditionally dereferences a.noteBus would panic here.
}

// TestWriteTouches_ApplyPatchArgumentShapes documents that
// apply_patch (the ocode tool) accepts its target path under
// several argument keys. The touch must work regardless of
// which one the caller used.
func TestWriteTouches_ApplyPatchArgumentShapes(t *testing.T) {
	bus := notebus.NewBus("grp")
	child := NewAgent(&MockClient{}, nil, nil, nil)
	child.SetNoteBus(bus, "a1")
	bus.Start(busBackground(t))
	defer func() { bus.Stop(); <-bus.Done() }()

	appendWriteTouchIfGrouped(child, "apply_patch", `{"path":"x.go","patch":"@@"}`)
	// No path key — should not panic.
	appendWriteTouchIfGrouped(child, "apply_patch", `{"patch":"@@"}`)

	delta := bus.Delta("a2")
	touches := 0
	for _, e := range delta {
		if e.Kind == notebus.KindTouch && e.File == "x.go" {
			touches++
		}
	}
	if touches != 1 {
		t.Errorf("apply_patch touch count = %d, want 1", touches)
	}
}

// busBackground returns a context.Background so the bus owner
// goroutine can run. The test's deferred Stop()+Done() is what
// eventually tears the bus down; we don't need a cancellable
// context here because the test is short.
func busBackground(t *testing.T) context.Context {
	t.Helper()
	return context.Background()
}
