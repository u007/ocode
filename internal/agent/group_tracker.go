package agent

import (
	"context"
	"fmt"
	"sync"

	"github.com/u007/ocode/internal/notebus"
)

// groupTracker is the orchestrator-side per-group completion
// ledger. The parallel block creates one per group (lives in the
// outer Step frame) and the bus's onCompletion callback writes
// to it. reconcile (Part 05) reads it to surface unreviewed
// partitions and to decide whether a verify-agent escalation is
// warranted.
type groupTracker struct {
	mu         sync.Mutex
	status     map[string]string // agentID -> "completed" | "failed" | "cancelled"
	errs       map[string]error
	partitions map[string]string // agentID -> partition label (free-form, e.g. "auth/session")
}

func newGroupTracker() *groupTracker {
	return &groupTracker{
		status:     make(map[string]string),
		errs:       make(map[string]error),
		partitions: make(map[string]string),
	}
}

// Record atomically records an agent's final status. Idempotent:
// the last write wins. err may be nil.
func (g *groupTracker) Record(agentID, status string, err error) {
	if g == nil {
		return
	}
	g.mu.Lock()
	g.status[agentID] = status
	if err != nil {
		g.errs[agentID] = err
	} else {
		delete(g.errs, agentID)
	}
	g.mu.Unlock()
}

// SetPartition associates a partition label with an agent. The
// orchestrator sets this from the static partition it computed
// at dispatch time, so reconcile can attribute unreviewed
// partitions to specific files / subsystems.
func (g *groupTracker) SetPartition(agentID, partition string) {
	if g == nil {
		return
	}
	g.mu.Lock()
	g.partitions[agentID] = partition
	g.mu.Unlock()
}

// Status returns the agent's status, or "" if no record exists.
func (g *groupTracker) Status(agentID string) string {
	if g == nil {
		return ""
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.status[agentID]
}

// HasUnreviewed returns true if any agent's status is failed or
// cancelled. Used by reconcile to surface "this partition was
// not reviewed" rather than imply full coverage.
func (g *groupTracker) HasUnreviewed() bool {
	if g == nil {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, s := range g.status {
		if s == "failed" || s == "cancelled" {
			return true
		}
	}
	return false
}

// Unreviewed returns the set of agentIDs whose status is failed
// or cancelled, mapped to their status. Reconcile renders this
// as an "Unreviewed partitions" section.
func (g *groupTracker) Unreviewed() map[string]string {
	out := map[string]string{}
	if g == nil {
		return out
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	for id, s := range g.status {
		if s == "failed" || s == "cancelled" {
			out[id] = s
		}
	}
	return out
}

// Partitions returns a copy of the per-agent partition labels.
// Used by reconcile to list which files were assigned to which
// agent.
func (g *groupTracker) Partitions() map[string]string {
	out := map[string]string{}
	if g == nil {
		return out
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	for k, v := range g.partitions {
		out[k] = v
	}
	return out
}

// Statuses returns the per-agent record in the
// notebus.AgentStatuses shape the pre-pass expects. The
// partition label is included so the unreviewed section can
// surface the missed slice of the work.
func (g *groupTracker) Statuses() notebus.AgentStatuses {
	out := notebus.AgentStatuses{}
	if g == nil {
		return out
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	for id, s := range g.status {
		out[id] = notebus.AgentStatus{Status: s, Partition: g.partitions[id]}
	}
	return out
}

// maybeBuildGroupBusForTest is a test-only variant of
// maybeBuildGroupBus that does NOT start the bus (so the test
// can drive it directly without contending with a background
// goroutine owned by the parallel block). The test's
// teardownGroupBus call is still safe — it short-circuits on a
// nil bus, so it does not have to be paired with a Start.
//
// Implementation note: the production helper assigns ids and
// builds the bus but always Start()s it. The test re-runs the
// bus's goroutine by Stop+Start — Start is idempotent (sync.Once)
// so the second Start is a no-op. To work around that, this
// helper builds the bus directly via the factory and assigns
// ids from the parallel batch, mirroring the production path
// without ever starting the bus.
func (a *Agent) maybeBuildGroupBusForTest(tcs []ToolCall, parallelTCs []int) (*notebus.Bus, []string) {
	// Re-implement the id assignment inline so we don't depend
	// on the production helper's Start.
	var subagentIdxs []int
	for _, i := range parallelTCs {
		if i >= len(tcs) {
			continue
		}
		tc := tcs[i]
		if tc.Function.Name != "task" && tc.Function.Name != "agent" {
			continue
		}
		if sharedNotesArg(tc.Function.Arguments) {
			subagentIdxs = append(subagentIdxs, i)
		}
	}
	if len(subagentIdxs) < 2 {
		return nil, nil
	}
	groupID := a.nextGroupID()
	bus := a.noteBusFactory(groupID)
	if bus == nil {
		return nil, nil
	}
	// Build agent ids (a1, a2, …) in the order the subagent
	// calls appear in the parallel batch.
	ids := make([]string, len(parallelTCs))
	for k, i := range parallelTCs {
		for j, idx := range subagentIdxs {
			if idx == i {
				ids[k] = fmt.Sprintf("a%d", j+1)
				break
			}
		}
	}
	return bus, ids
}

// testContext returns a context the bus owner can run on. Tests
// are short and finish before any meaningful cancellation
// matters; we use a never-cancelled background context.
func testContext() context.Context { return context.Background() }
