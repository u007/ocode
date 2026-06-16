package agent

import (
	"sync"
	"testing"

	"github.com/u007/ocode/internal/notebus"
)

// TestSharedNotesToggle_Schema confirms the task tool definition
// accepts an optional `shared_notes` boolean. The LLM is what reads
// this schema, but the test is the contract — if the property goes
// missing, the toggle stops being reachable from the prompt.
func TestSharedNotesToggle_Schema(t *testing.T) {
	a := NewAgent(&MockClient{}, nil, nil, nil)
	taskTool, ok := a.tools["task"].(*TaskTool)
	if !ok {
		t.Fatalf("task tool type = %T", a.tools["task"])
	}
	def := taskTool.Definition()
	params, ok := def["parameters"].(map[string]interface{})
	if !ok {
		t.Fatalf("task definition has no parameters: %+v", def)
	}
	props, ok := params["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("task parameters has no properties: %+v", params)
	}
	sn, ok := props["shared_notes"]
	if !ok {
		t.Fatalf("task schema missing shared_notes: %+v", props)
	}
	sm, ok := sn.(map[string]interface{})
	if !ok {
		t.Fatalf("shared_notes is not an object: %T", sn)
	}
	if sm["type"] != "boolean" {
		t.Errorf("shared_notes.type = %v, want boolean", sm["type"])
	}
}

// recordingBusFactory records every bus the agent creates. Tests
// install it via Agent.SetNoteBusFactory; production code uses the
// default factory. The factory returns the bus it just created so
// callers (the parallel block) can also keep a handle.
type recordingBusFactory struct {
	mu       sync.Mutex
	buses    []*notebus.Bus
	groupIDs []string
}

func (f *recordingBusFactory) New(groupID string) *notebus.Bus {
	b := notebus.NewBus(groupID)
	f.mu.Lock()
	f.buses = append(f.buses, b)
	f.groupIDs = append(f.groupIDs, groupID)
	f.mu.Unlock()
	return b
}

func (f *recordingBusFactory) Count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.buses)
}

// TestSharedNotesToggle_GroupBusCreation exercises the bus-creation
// helper directly. The full Step() flow is tested via the e2e in
// Part 05; here we just verify that the helper sees 2+ qualifying
// subagent calls and constructs exactly one bus.
func TestSharedNotesToggle_GroupBusCreation(t *testing.T) {
	factory := &recordingBusFactory{}
	a := NewAgent(&MockClient{}, nil, nil, nil)
	a.SetNoteBusFactory(factory.New)

	t1 := ToolCall{ID: "t1", Type: "function", Function: struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	}{Name: "task", Arguments: `{"prompt":"job1","agent":"general","shared_notes":true}`}}
	t2 := ToolCall{ID: "t2", Type: "function", Function: struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	}{Name: "task", Arguments: `{"prompt":"job2","agent":"explore","shared_notes":true}`}}

	tcs := []ToolCall{t1, t2}
	parallelTCs := []int{0, 1}
	bus, ids := a.maybeBuildGroupBus(tcs, parallelTCs)
	if bus == nil {
		t.Fatal("maybeBuildGroupBus returned nil bus for 2 qualifying calls")
	}
	defer a.teardownGroupBus(bus)
	if got := factory.Count(); got != 1 {
		t.Errorf("bus factory call count = %d, want 1 (one group)", got)
	}
	if len(ids) != 2 || ids[0] != "a1" || ids[1] != "a2" {
		t.Errorf("ids = %v, want [a1 a2]", ids)
	}
}

// TestSharedNotesToggle_NoBusForSingleCall: a single qualifying
// call (shared_notes:true) does NOT create a bus. The design says
// a bus exists only when 2+ child-runs are spawned concurrently.
func TestSharedNotesToggle_NoBusForSingleCall(t *testing.T) {
	factory := &recordingBusFactory{}
	a := NewAgent(&MockClient{}, nil, nil, nil)
	a.SetNoteBusFactory(factory.New)

	t1 := ToolCall{ID: "t1", Type: "function", Function: struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	}{Name: "task", Arguments: `{"prompt":"solo","agent":"general","shared_notes":true}`}}
	tcs := []ToolCall{t1}
	parallelTCs := []int{0}
	bus, ids := a.maybeBuildGroupBus(tcs, parallelTCs)
	if bus != nil {
		t.Errorf("maybeBuildGroupBus returned bus for single call: %v", bus)
		a.teardownGroupBus(bus)
	}
	if got := factory.Count(); got != 0 {
		t.Errorf("bus factory call count = %d, want 0 (single call, no bus)", got)
	}
	if ids != nil {
		t.Errorf("ids = %v, want nil", ids)
	}
}

// TestSharedNotesToggle_NoBusWhenDisabled: a parallel batch with
// shared_notes unset (or false) does NOT create a bus.
func TestSharedNotesToggle_NoBusWhenDisabled(t *testing.T) {
	factory := &recordingBusFactory{}
	a := NewAgent(&MockClient{}, nil, nil, nil)
	a.SetNoteBusFactory(factory.New)
	t1 := ToolCall{ID: "t1", Type: "function", Function: struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	}{Name: "task", Arguments: `{"prompt":"a","agent":"general"}`}}
	t2 := ToolCall{ID: "t2", Type: "function", Function: struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	}{Name: "task", Arguments: `{"prompt":"b","agent":"explore"}`}}
	tcs := []ToolCall{t1, t2}
	parallelTCs := []int{0, 1}
	bus, _ := a.maybeBuildGroupBus(tcs, parallelTCs)
	if bus != nil {
		t.Errorf("maybeBuildGroupBus returned bus when toggle off")
		a.teardownGroupBus(bus)
	}
	if got := factory.Count(); got != 0 {
		t.Errorf("bus factory call count = %d, want 0 (toggle off, no bus)", got)
	}
}

// TestSharedNotesToggle_MixedBatch: a batch where some calls have
// shared_notes:true and others do not. The plan is silent on
// "mixed" (one toggle on, one off). The current behavior is that
// any single non-qualifying call is treated as a non-group member —
// the qualifying calls alone do not form a group. We test that
// the helper returns nil for the mixed case (no group bus) which
// matches the conservative reading: shared_notes must be on for
// every member of a group. If a future spec wants "some of us
// share", relax this test.
func TestSharedNotesToggle_MixedBatch(t *testing.T) {
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
	}{Name: "task", Arguments: `{"prompt":"b","agent":"explore"}`}}
	tcs := []ToolCall{t1, t2}
	parallelTCs := []int{0, 1}
	bus, _ := a.maybeBuildGroupBus(tcs, parallelTCs)
	if bus != nil {
		t.Errorf("mixed batch should not form a group, got bus %v", bus)
		a.teardownGroupBus(bus)
	}
	if got := factory.Count(); got != 0 {
		t.Errorf("bus factory call count = %d, want 0 (mixed batch)", got)
	}
}
