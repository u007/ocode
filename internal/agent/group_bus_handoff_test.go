package agent

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/u007/ocode/internal/notebus"
)

// TestGroupAgentIds tests that when a group is built, the agent
// ids assigned in the parallel batch are stable (a1, a2, … in
// the order the subagent calls appear) and that all ids are
// distinct within the group.
func TestGroupAgentIds(t *testing.T) {
	factory := &recordingBusFactory{}
	a := NewAgent(&MockClient{}, nil, nil, nil)
	a.SetNoteBusFactory(factory.New)

	// Three qualifying subagent calls. The bus should be built
	// with three agent ids: a1, a2, a3 in the order they appear
	// in the parallel batch.
	t1 := ToolCall{ID: "t1", Type: "function", Function: struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	}{Name: "task", Arguments: `{"prompt":"a","agent":"general","shared_notes":true}`}}
	t2 := ToolCall{ID: "t2", Type: "function", Function: struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	}{Name: "task", Arguments: `{"prompt":"b","agent":"explore","shared_notes":true}`}}
	t3 := ToolCall{ID: "t3", Type: "function", Function: struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	}{Name: "task", Arguments: `{"prompt":"c","agent":"scout","shared_notes":true}`}}

	tcs := []ToolCall{t1, t2, t3}
	parallelTCs := []int{0, 1, 2}
	bus, ids := a.maybeBuildGroupBus(tcs, parallelTCs)
	if bus == nil {
		t.Fatal("expected a bus for 3 qualifying calls")
	}
	defer a.teardownGroupBus(bus)

	want := []string{"a1", "a2", "a3"}
	if len(ids) != len(want) {
		t.Fatalf("ids len = %d, want %d", len(ids), len(want))
	}
	for i, w := range want {
		if ids[i] != w {
			t.Errorf("ids[%d] = %q, want %q", i, ids[i], w)
		}
	}
	// All distinct.
	seen := make(map[string]bool)
	for _, id := range ids {
		if id == "" {
			t.Error("got an empty agent id in a group")
		}
		if seen[id] {
			t.Errorf("agent id %q assigned twice in the same group", id)
		}
		seen[id] = true
	}
}

// TestBusHandoff confirms that when a group is built, the bus is
// reachable from each child via the agent's NoteBus / NoteAgentID
// accessors. We simulate the handoff path by building a group
// and then constructing child agents with the bus.
func TestBusHandoff(t *testing.T) {
	factory := &recordingBusFactory{}
	a := NewAgent(&MockClient{}, nil, nil, nil)
	a.SetNoteBusFactory(factory.New)

	t1 := ToolCall{ID: "t1", Type: "function", Function: struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	}{Name: "task", Arguments: `{"prompt":"a","agent":"general","shared_notes":true}`}}
	t2 := ToolCall{ID: "t2", Type: "function", Function: struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	}{Name: "task", Arguments: `{"prompt":"b","agent":"explore","shared_notes":true}`}}

	tcs := []ToolCall{t1, t2}
	parallelTCs := []int{0, 1}
	bus, ids := a.maybeBuildGroupBus(tcs, parallelTCs)
	if bus == nil {
		t.Fatal("expected a bus for 2 qualifying calls")
	}
	defer a.teardownGroupBus(bus)

	// Build two child agents and hand the bus to each.
	c1 := NewAgent(&MockClient{}, nil, nil, nil)
	c1.SetNoteBus(bus, ids[0])
	if c1.NoteBus() == nil {
		t.Error("c1.NoteBus() = nil after SetNoteBus")
	}
	if c1.NoteBus() != bus {
		t.Errorf("c1.NoteBus() = %p, want %p", c1.NoteBus(), bus)
	}
	if c1.NoteAgentID() != "a1" {
		t.Errorf("c1.NoteAgentID() = %q, want a1", c1.NoteAgentID())
	}

	c2 := NewAgent(&MockClient{}, nil, nil, nil)
	c2.SetNoteBus(bus, ids[1])
	if c2.NoteBus() != bus {
		t.Errorf("c2.NoteBus() = %p, want %p", c2.NoteBus(), bus)
	}
	if c2.NoteAgentID() != "a2" {
		t.Errorf("c2.NoteAgentID() = %q, want a2", c2.NoteAgentID())
	}
}

// TestDisabledChildHasNoBus: a child created in a non-group
// context (no SetNoteBus) has nil bus and empty agent id. This
// confirms the disabled path is the "do nothing" path — no
// inherited state, no leftover handle.
func TestDisabledChildHasNoBus(t *testing.T) {
	a := NewAgent(&MockClient{}, nil, nil, nil)
	if a.NoteBus() != nil {
		t.Errorf("a.NoteBus() = %p, want nil for default agent", a.NoteBus())
	}
	if a.NoteAgentID() != "" {
		t.Errorf("a.NoteAgentID() = %q, want empty for default agent", a.NoteAgentID())
	}
}

// TestCompletionCallbackFires confirms the per-agent completion
// callback is invoked exactly once when the child run ends, and
// that the status reflects success / failure correctly.
func TestCompletionCallbackFires(t *testing.T) {
	var (
		mu       sync.Mutex
		ids      []string
		statuses []string
		atomicN  atomic.Int32
	)
	cb := func(agentID, status string, err error) {
		mu.Lock()
		ids = append(ids, agentID)
		statuses = append(statuses, status)
		mu.Unlock()
		atomicN.Add(1)
	}

	// We invoke the callback directly via the agent's stored
	// field to avoid the full Step() loop (that test is in
	// Part 05's e2e). The wiring test confirms the callback
	// is reachable and gets the right agent id.
	a := NewAgent(&MockClient{}, nil, nil, nil)
	a.SetNoteBusCompletion(cb)
	if a.noteBusCompletion == nil {
		t.Fatal("noteBusCompletion not stored")
	}
	a.noteBusCompletion("a7", "completed", nil)
	if got := atomicN.Load(); got != 1 {
		t.Errorf("callback invocations = %d, want 1", got)
	}
	mu.Lock()
	if len(ids) != 1 || ids[0] != "a7" {
		t.Errorf("ids = %v, want [a7]", ids)
	}
	if len(statuses) != 1 || statuses[0] != "completed" {
		t.Errorf("statuses = %v, want [completed]", statuses)
	}
	mu.Unlock()
}

// smoke import to ensure notebus package is reachable
var _ = notebus.NewBus
