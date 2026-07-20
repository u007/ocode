package agent

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/u007/ocode/internal/notebus"
)

// maybeBuildGroupBus inspects the parallel batch and returns a
// fresh bus + per-call agent id map if the batch qualifies as a
// "group" (2+ subagent calls with shared_notes:true). The return
// values are (nil, nil) when the batch does NOT qualify — the
// non-group path is the common case (single subagent, no toggle,
// or non-subagent parallel tools).
//
// The agent ids are stable for the group's lifetime: a1, a2, a3, …
// in the order the calls appear in the parallel batch. They are
// returned in the same order as parallelTCs so the caller can zip
// them back: parallelTCs[i] corresponds to agentIDs[i].
//
// A nil bus means "no group; do not attach". A non-nil bus has
// been Start()'d with the caller's context — see attachBusToTaskTool.
func (a *Agent) maybeBuildGroupBus(tcs []ToolCall, parallelTCs []int) (*notebus.Bus, []string) {
	// Identify which parallel tool calls are subagent calls with
	// shared_notes:true. The first pass records the indices.
	var subagentIdxs []int
	for _, i := range parallelTCs {
		if i >= len(tcs) {
			continue
		}
		tc := tcs[i]
		if tc.Function.Name != "task" && tc.Function.Name != "agent" {
			continue
		}
		var p struct {
			SharedNotes bool `json:"shared_notes"`
		}
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &p); err != nil {
			continue
		}
		if p.SharedNotes {
			subagentIdxs = append(subagentIdxs, i)
		}
	}
	// "Group" requires 2+ such calls. A single call has nobody to
	// coordinate with, and the design says no bus.
	if len(subagentIdxs) < 2 {
		return nil, nil
	}

	// Build the bus with a deterministic group id. The id combines
	// a per-batch nonce (so distinct groups in the same session do
	// not collide) and a short random suffix (so resume paths can
	// find the right sidecar even after a crash).
	groupID := a.nextGroupID()

	bus := a.noteBusFactory(groupID)
	if bus == nil {
		// Defensive: factory returned nil (test stub or broken
		// override). Treat as "no bus" — the non-group path
		// remains correct.
		return nil, nil
	}

	// Wire secret redaction BEFORE Start: every note body is
	// scrubbed before it reaches the log, the sidecar, the delta,
	// or the reconcile hand-off, so a secret in a reviewed diff
	// never propagates to peer agents' prompts or to disk.
	bus.SetRedactor(notebus.RedactBody)

	// Wire the UI observer: when the main agent has set an
	// OnNoteBusEntry callback, forward every bus append to it
	// so the TUI can display note entries as they are posted.
	if a.OnNoteBusEntry != nil {
		bus.SetOnAppend(func(e notebus.Entry) {
			a.OnNoteBusEntry(e)
		})
	}

	// Optional: attach a sidecar so the bus persists.
	if a.noteBusDir != "" {
		sc, err := notebus.NewSidecar(a.noteBusDir, groupID)
		if err != nil {
			emitDebug("NOTEBUS", fmt.Sprintf("sidecar open failed: %v", err))
		} else {
			bus.SetPersist(sc)
		}
	}

	// Start the bus on a context derived from the agent's stop
	// channel so the owner also exits if the agent shuts down
	// before teardown runs. stopChContext's watcher selects on
	// ctx.Done() as well, so the cancel stored here releases that
	// watcher in teardownGroupBus — no leaked goroutine per group.
	ctx, cancel := stopChContext(a.StopCh())
	a.noteBusCancel = cancel
	bus.Start(ctx)

	// Build the agent-id list. The ids are stable across the
	// group: a1, a2, ... in the order the subagent calls appear
	// in the original tool-call list (so a1 is always the first
	// subagent the orchestrator dispatched).
	ids := make([]string, len(parallelTCs))
	for k, i := range parallelTCs {
		for j, idx := range subagentIdxs {
			if idx == i {
				ids[k] = fmt.Sprintf("a%d", j+1)
				break
			}
		}
	}
	emitDebug("NOTEBUS", fmt.Sprintf("group %q created with %d agents (subagents=%d)", groupID, len(subagentIdxs), len(subagentIdxs)))
	return bus, ids
}

// taskBinding carries the per-call group context (bus, agent id, and
// completion tracker) for a single parallel subagent dispatch. It is
// threaded through handleToolCall → executeToolCall and applied to a
// per-call copy of the task tool, so concurrent goroutines never
// mutate the shared *TaskTool instance.
type taskBinding struct {
	bus     *notebus.Bus
	agentID string
	tracker *groupTracker
}

// teardownGroupBus stops the bus, closes the persist sink, and
// releases the stop-channel watcher created in maybeBuildGroupBus.
// It is safe to call with a nil bus (the common case). The function
// is idempotent — calling it twice is fine.
func (a *Agent) teardownGroupBus(bus *notebus.Bus) {
	if bus == nil {
		return
	}
	// Stop signals the owner goroutine to drain pending requests
	// and exit. Done() closes once the goroutine has fully exited
	// and the persist sink (if any) has been Closed.
	bus.Stop()
	<-bus.Done()
	// Cancel the derived context so stopChContext's watcher
	// goroutine returns instead of parking on the agent stop
	// channel for the rest of the agent's lifetime.
	if a.noteBusCancel != nil {
		a.noteBusCancel()
		a.noteBusCancel = nil
	}
}

// nextGroupID returns a stable, collision-resistant group id. The
// id is derived from a counter + a short random suffix. The counter
// ensures uniqueness within a single session (so two groups in the
// same Step loop do not collide); the random suffix is just extra
// defense in depth.
var groupIDSuffix = func() string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "0"
	}
	return hex.EncodeToString(b[:])
}

func (a *Agent) nextGroupID() string {
	// a.noteBusSessionTag may be empty in tests; fall back to
	// "main". The tag is the only place a session id enters the
	// group id; production wiring sets it to the parent's session
	// id so the sidecar can be located on resume.
	tag := a.noteBusSessionTag
	if tag == "" {
		tag = "main"
	}
	return fmt.Sprintf("grp-%s-%s", tag, groupIDSuffix())
}

// isTaskOrAgent returns true if the tool name is the subagent
// dispatcher. Used by other helpers that key on the tool kind.
func isTaskOrAgent(name string) bool {
	return name == "task" || name == "agent"
}

// sharedNotesArg returns the parsed `shared_notes` boolean from a
// task/agent tool call. Returns false on any parse error or missing
// field. This is the toggle that opts a call into a group bus.
func sharedNotesArg(args string) bool {
	if args == "" {
		return false
	}
	var p struct {
		SharedNotes bool `json:"shared_notes"`
	}
	if err := json.Unmarshal([]byte(args), &p); err != nil {
		return false
	}
	return p.SharedNotes
}
