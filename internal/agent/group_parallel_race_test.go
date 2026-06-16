package agent

import (
	"encoding/json"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/u007/ocode/internal/notebus"
)

// taskArgs builds a task tool-call arguments blob for the given agent
// with shared_notes enabled.
func taskArgs(agentType, prompt string) json.RawMessage {
	return json.RawMessage(`{"prompt":"` + prompt + `","agent":"` + agentType + `","shared_notes":true}`)
}

// TestGroupDispatch_NoRaceAndCorrectAttribution drives the real
// concurrent-dispatch surface: two grouped subagent calls executed at
// the same time through executeToolCall with distinct per-call
// bindings. Before the fix, every goroutine mutated the shared
// *TaskTool (groupBus/agentID/groupTracker), which both data-raced and
// could attribute a1's binding to a2 (or nil). With the per-call copy,
// each child must see exactly its own id.
//
// Run under -race; the assertion proves attribution, the -race flag
// proves the absence of the shared-state data race.
func TestGroupDispatch_NoRaceAndCorrectAttribution(t *testing.T) {
	a := NewAgent(&MockClient{Response: &Message{Role: "assistant", Content: "done"}}, nil, nil, nil)
	factory := &recordingBusFactory{}
	a.SetNoteBusFactory(factory.New)

	tcs := []ToolCall{
		{ID: "t1", Function: struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}{Name: "task", Arguments: string(taskArgs("general", "job1"))}},
		{ID: "t2", Function: struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}{Name: "task", Arguments: string(taskArgs("explore", "job2"))}},
	}
	bus, ids := a.maybeBuildGroupBus(tcs, []int{0, 1})
	if bus == nil || len(ids) != 2 {
		t.Fatalf("group not formed: bus=%v ids=%v", bus, ids)
	}
	defer a.teardownGroupBus(bus)

	tracker := newGroupTracker()

	var wg sync.WaitGroup
	for i := range tcs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			binding := &taskBinding{bus: bus, agentID: ids[i], tracker: tracker}
			if _, err := a.executeToolCall(tcs[i].Function.Name, json.RawMessage(tcs[i].Function.Arguments), binding); err != nil {
				t.Errorf("dispatch %s failed: %v", ids[i], err)
			}
		}(i)
	}
	wg.Wait()

	// Each child must have reported completion under its own id —
	// proof the binding flowed per-call rather than being clobbered.
	if got := tracker.Status("a1"); got != "completed" {
		t.Errorf("a1 status = %q, want completed", got)
	}
	if got := tracker.Status("a2"); got != "completed" {
		t.Errorf("a2 status = %q, want completed", got)
	}
}

// TestGroupBus_NoGoroutineLeakAfterTeardown verifies that
// maybeBuildGroupBus's owner goroutine AND its stop-channel watcher
// both exit after teardownGroupBus. Before the fix, the watcher parked
// on the agent stop channel for the agent's lifetime, leaking one
// goroutine + one uncancelled context per fan-out.
func TestGroupBus_NoGoroutineLeakAfterTeardown(t *testing.T) {
	a := NewAgent(&MockClient{}, nil, nil, nil)
	tcs := []ToolCall{
		{ID: "t1", Function: struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}{Name: "task", Arguments: string(taskArgs("general", "a"))}},
		{ID: "t2", Function: struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}{Name: "task", Arguments: string(taskArgs("explore", "b"))}},
	}

	base := runtime.NumGoroutine()
	bus, _ := a.maybeBuildGroupBus(tcs, []int{0, 1})
	if bus == nil {
		t.Fatal("group not formed")
	}
	a.teardownGroupBus(bus)

	// The owner + watcher exit asynchronously; poll until the count
	// settles back to the baseline (tolerating scheduling lag).
	for i := 0; i < 50; i++ {
		if runtime.NumGoroutine() <= base {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Errorf("goroutines after teardown = %d, want <= baseline %d (leak)", runtime.NumGoroutine(), base)
}

// TestGroupBus_RedactorWiredInProduction verifies that the production
// group-bus construction path (maybeBuildGroupBus) installs the secret
// redactor, so a note body containing a secret is scrubbed before it
// ever reaches the log / delta / sidecar. This guards the
// "redaction not wired in production" regression.
func TestGroupBus_RedactorWiredInProduction(t *testing.T) {
	a := NewAgent(&MockClient{}, nil, nil, nil)
	tcs := []ToolCall{
		{ID: "t1", Function: struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}{Name: "task", Arguments: string(taskArgs("general", "a"))}},
		{ID: "t2", Function: struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}{Name: "task", Arguments: string(taskArgs("explore", "b"))}},
	}
	bus, _ := a.maybeBuildGroupBus(tcs, []int{0, 1})
	if bus == nil {
		t.Fatal("group not formed")
	}
	defer a.teardownGroupBus(bus)

	secret := "ghp_1234567890abcdefghijklmnopqrstuvwxyz"
	if _, err := bus.Append(notebus.Note(0, "a1", "x.go", "leaked "+secret, 0)); err != nil {
		t.Fatalf("append: %v", err)
	}
	snap := bus.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("snapshot len = %d, want 1", len(snap))
	}
	if strings.Contains(snap[0].Body, secret) {
		t.Errorf("raw secret present — redactor not wired in production:\n%s", snap[0].Body)
	}
	if !strings.Contains(snap[0].Body, "REDACTED") {
		t.Errorf("body not redacted:\n%s", snap[0].Body)
	}
}
