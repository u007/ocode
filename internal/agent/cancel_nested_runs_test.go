package agent

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/u007/ocode/internal/tool"
)

// nestedLeafTool blocks in Execute until released, standing in for a nested
// sub-agent's tool call that is genuinely in flight when the user hits Stop.
type nestedLeafTool struct {
	entered chan struct{}
	release chan struct{}
}

func (n *nestedLeafTool) Name() string        { return "leaf" }
func (n *nestedLeafTool) Description() string { return "" }
func (n *nestedLeafTool) Parallel() bool      { return false }
func (n *nestedLeafTool) Definition() map[string]interface{} {
	return map[string]interface{}{"name": "leaf"}
}
func (n *nestedLeafTool) Execute(json.RawMessage) (string, error) {
	select {
	case <-n.entered:
	default:
		close(n.entered)
	}
	<-n.release
	return "leaf done", nil
}

// TestCancelAllReachesNestedRuns is the regression test for "I hit Stop and the
// task is STILL shown as running".
//
// CancelAll only walks THIS registry's runs. A sub-agent that dispatched its
// own children owns a separate registry (subAgent.runs), and buildRunDTO /
// agentRunChildren render those nested runs verbatim — so a nested run that
// CancelAll never reaches keeps Status==RunRunning in the Agents panel and the
// web agent-runs tree forever, even though the parent is cancelled.
func TestCancelAllReachesNestedRuns(t *testing.T) {
	parent := NewAgentRunRegistry()

	// The parent sub-agent: a real Agent, so it owns its own run registry
	// exactly as TaskTool creates it.
	sub := newTestAgent(nil, nil, nil, nil)
	childRuns := sub.Runs()
	if childRuns == nil {
		t.Fatal("sub-agent has no run registry")
	}

	leaf := childRuns.New("leaf")
	leaf.Sub = newTestAgent(nil, nil, nil, nil)
	leaf.Cancel = func() {}
	leaf.markQueued() // an active-but-not-yet-running nested run

	run := parent.New("orchestrator")
	run.Sub = sub
	run.Cancel = sub.Cancel

	parent.CancelAll()

	if run.statusValue() != RunCancelled {
		t.Fatalf("parent status = %s, want %s", run.statusValue(), RunCancelled)
	}
	if leaf.statusValue() != RunCancelled {
		t.Fatalf("nested run status = %s, want %s (nested registries must be cancelled too, or the UI shows it as still running)", leaf.statusValue(), RunCancelled)
	}
	if leaf.Err != "cancelled" {
		t.Fatalf("nested run Err = %q, want \"cancelled\"", leaf.Err)
	}
}

// TestCancelAllNestedCycleTerminates guards the recursion: a run whose Sub
// points back at an ancestor registry must not spin forever.
func TestCancelAllNestedCycleTerminates(t *testing.T) {
	reg := NewAgentRunRegistry()
	agent := newTestAgent(nil, nil, nil, nil)
	run := reg.New("self")
	run.Sub = agent
	run.Cancel = func() {}
	// Plant a run in the agent's own registry whose Sub is this agent again,
	// forming registry -> agent -> registry -> agent -> ...
	nested := agent.Runs().New("nested")
	nested.Sub = agent
	nested.Cancel = func() {}

	done := make(chan struct{})
	go func() {
		defer close(done)
		reg.CancelAll()
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("CancelAll did not terminate on a cyclic run graph")
	}
	if nested.statusValue() != RunCancelled {
		t.Fatalf("nested status = %s, want %s", nested.statusValue(), RunCancelled)
	}
}

// TestShutdownCancelsNestedRuns pins the same invariant through the public
// teardown entry point: Agent.Shutdown() must leave no descendant run active.
func TestShutdownCancelsNestedRuns(t *testing.T) {
	sub := newTestAgent(nil, nil, nil, nil)
	leaf := sub.Runs().New("leaf")
	leaf.Sub = newTestAgent(nil, nil, nil, nil)
	leaf.Cancel = func() {}

	parent := newTestAgent(nil, nil, nil, nil)
	run := parent.Runs().New("orchestrator")
	run.Sub = sub
	run.Cancel = sub.Cancel

	parent.Shutdown()

	if leaf.statusValue() != RunCancelled {
		t.Fatalf("after Shutdown, nested run status = %s, want %s", leaf.statusValue(), RunCancelled)
	}
}

// compile-time guard: the leaf tool must satisfy the Tool interface.
var _ tool.Tool = (*nestedLeafTool)(nil)
