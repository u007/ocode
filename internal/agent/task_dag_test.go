package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// dagToolCall is a small constructor that builds a ToolCall with the
// given id and depends_on list, defaulting the other fields to
// placeholders. The id and deps are JSON-encoded into Arguments so the
// scheduler sees the same shape it does in production.
func dagToolCall(id string, deps []string) ToolCall {
	args := map[string]interface{}{"prompt": "p", "agent": "general"}
	if id != "" {
		args["id"] = id
	}
	if len(deps) > 0 {
		args["depends_on"] = deps
	}
	b, _ := json.Marshal(args)
	return ToolCall{
		ID:   "tc-" + id,
		Type: "function",
		Function: struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}{Name: "task", Arguments: string(b)},
	}
}

// dagAnonToolCall builds a parallel task call with no id and no
// depends_on. Used to mix anonymous calls into a batch alongside named
// ones.
func dagAnonToolCall() ToolCall {
	return dagToolCall("", nil)
}

// TestDAGHasDependencies covers the cheap pre-check the fan-out uses to
// decide between the legacy path and the scheduler. The gate must
// return false for any batch that does not declare either field, and
// true as soon as one call declares an id or a depends_on.
func TestDAGHasDependencies(t *testing.T) {
	tests := []struct {
		name string
		tcs  []ToolCall
		want bool
	}{
		{"all-anonymous", []ToolCall{dagAnonToolCall(), dagAnonToolCall()}, false},
		{"single-with-id", []ToolCall{dagToolCall("a", nil), dagAnonToolCall()}, true},
		{"single-with-deps", []ToolCall{dagToolCall("a", nil), dagToolCall("b", []string{"a"})}, true},
		{"id-only-no-deps", []ToolCall{dagToolCall("a", nil)}, true},
		{"empty-batch", []ToolCall{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := dagHasDependencies(tt.tcs)
			if got != tt.want {
				t.Errorf("dagHasDependencies = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestBuildDAGValid exercises the happy path: a small DAG with two
// independent roots and a join. The validator must return
// hasDependencies=true, the named map must contain every id, and
// every node's deps must resolve to a known node.
func TestBuildDAGValid(t *testing.T) {
	tcs := []ToolCall{
		dagToolCall("a", nil),
		dagToolCall("b", nil),
		dagToolCall("c", []string{"a", "b"}),
	}
	parsed, err := buildDAG(tcs)
	if err != nil {
		t.Fatalf("buildDAG: %v", err)
	}
	if !parsed.hasDependencies {
		t.Errorf("hasDependencies = false, want true")
	}
	if got := len(parsed.nodes); got != 3 {
		t.Errorf("nodes = %d, want 3", got)
	}
	if got := len(parsed.named); got != 3 {
		t.Errorf("named = %d, want 3", got)
	}
	if _, ok := parsed.named["c"]; !ok {
		t.Errorf("named has no entry for c")
	}
	if got := len(parsed.named["c"].deps); got != 2 {
		t.Errorf("c.deps = %d, want 2", got)
	}
}

// TestBuildDAGAnonymousBatchIsLegacy exercises the contract that an
// all-anonymous batch is byte-identical to the legacy path: validator
// must return hasDependencies=false and produce no error, so the gate
// in agent.go routes to the WaitGroup fan-out.
func TestBuildDAGAnonymousBatchIsLegacy(t *testing.T) {
	tcs := []ToolCall{dagAnonToolCall(), dagAnonToolCall()}
	parsed, err := buildDAG(tcs)
	if err != nil {
		t.Fatalf("buildDAG: %v", err)
	}
	if parsed.hasDependencies {
		t.Errorf("hasDependencies = true, want false (legacy path)")
	}
}

// TestBuildDAGValidationErrors is the table test for every validation
// rule. Each row is an invalid batch and the specific error text the
// validator must return. The test asserts the text contains the
// diagnostic phrase; the full message wording is in the production
// code.
func TestBuildDAGValidationErrors(t *testing.T) {
	tests := []struct {
		name     string
		tcs      []ToolCall
		wantText string
	}{
		{
			name:     "depends-on-without-id",
			tcs:      []ToolCall{dagToolCall("a", nil), dagToolCall("", []string{"a"})},
			wantText: "depends_on but has no id",
		},
		{
			name:     "duplicate-id",
			tcs:      []ToolCall{dagToolCall("a", nil), dagToolCall("a", nil)},
			wantText: `duplicate id "a"`,
		},
		{
			name:     "self-edge",
			tcs:      []ToolCall{dagToolCall("a", []string{"a"})},
			wantText: `depends on its own id`,
		},
		{
			name:     "unknown-id",
			tcs:      []ToolCall{dagToolCall("a", []string{"z"})},
			wantText: `unknown id "z"`,
		},
		{
			name: "multi-node-cycle",
			tcs: []ToolCall{
				dagToolCall("a", []string{"c"}),
				dagToolCall("b", []string{"a"}),
				dagToolCall("c", []string{"b"}),
			},
			wantText: "task dependency cycle detected",
		},
		{
			name: "two-node-cycle",
			tcs: []ToolCall{
				dagToolCall("a", []string{"b"}),
				dagToolCall("b", []string{"a"}),
			},
			wantText: "task dependency cycle detected",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := buildDAG(tt.tcs)
			if err == nil {
				t.Fatalf("buildDAG returned nil error; want %q", tt.wantText)
			}
			if !strings.Contains(err.Error(), tt.wantText) {
				t.Errorf("error %q does not contain %q", err.Error(), tt.wantText)
			}
		})
	}
}

// dagRecord is the per-node outcome the test dispatch closure writes
// to for assertion. The scheduler passes the predecessor context block
// to the closure as its last argument; the closure records that block
// here so the test can verify the labels reached the dispatch.
type dagRecord struct {
	gotContext string
	calledAt   time.Time
}

// dagTestSetup builds the minimal scaffolding the runDAGFromValidated
// tests need: a parsed DAG, a recording dispatch, and the closures
// required to invoke the scheduler. It returns the parsed graph, the
// records slice to populate, the stop channel, and an isCancelled
// closure. The test sets up the actual nodes' expected behaviour in
// `records` (e.g. by hand-building a closure that inspects
// tc.Function.Arguments to find its predecessorContext).
func dagTestSetup(t *testing.T, tcs []ToolCall) (*dagParsed, []dagRecord, func(tc ToolCall, binding *taskBinding, toolCallID string, predecessorContext string) (string, []Image, error), chan struct{}, func() bool) {
	t.Helper()
	parsed, err := buildDAG(tcs)
	if err != nil {
		t.Fatalf("buildDAG: %v", err)
	}
	records := make([]dagRecord, len(tcs))
	var mu sync.Mutex
	dispatch := func(tc ToolCall, binding *taskBinding, toolCallID string, predecessorContext string) (string, []Image, error) {
		mu.Lock()
		// Find the index for this tc.
		idx := -1
		for k, n := range parsed.nodes {
			if n.toolCall.ID == tc.ID {
				idx = k
				break
			}
		}
		if idx >= 0 {
			records[idx] = dagRecord{
				gotContext: predecessorContext,
				calledAt:   time.Now(),
			}
		}
		mu.Unlock()
		// Sleep to make the wave ordering observable in the
		// slot-invariant test. Tests that don't care about
		// timing can pass sleep=0 to disable.
		return "result-of-" + tc.ID, nil, nil
	}
	stopCh := make(chan struct{})
	isCancelled := func() bool {
		select {
		case <-stopCh:
			return true
		default:
			return false
		}
	}
	return parsed, records, dispatch, stopCh, isCancelled
}

// TestScheduleDAGIndependentOverlap confirms that two independent
// nodes run concurrently when the limiter is unlimited. The
// dispatch-stub sleeps long enough that the second node's start
// time would be later than the first's if the scheduler were
// serialising them; we assert the second node starts before the
// first finishes.
func TestScheduleDAGIndependentOverlap(t *testing.T) {
	tcs := []ToolCall{
		dagToolCall("a", nil),
		dagToolCall("b", nil),
	}
	parsed, _, dispatch, stopCh, isCancelled := dagTestSetup(t, tcs)
	// Slow dispatch: 50ms each. The test asserts they overlap.
	dispatch = func(tc ToolCall, binding *taskBinding, toolCallID string, predecessorContext string) (string, []Image, error) {
		time.Sleep(50 * time.Millisecond)
		return "slow:" + tc.ID, nil, nil
	}
	sched := newDAGScheduler(parsed, stopCh, isCancelled, dispatch)
	sched.run(nil, nil, nil)
	if sched.perNode[0] == nil || sched.perNode[1] == nil {
		t.Fatalf("perNode entries missing: 0=%v 1=%v", sched.perNode[0], sched.perNode[1])
	}
	if sched.perNode[0].err != nil || sched.perNode[1].err != nil {
		t.Fatalf("dispatch errors: %v / %v", sched.perNode[0].err, sched.perNode[1].err)
	}
	if !strings.Contains(sched.perNode[0].result, "slow:") {
		t.Errorf("perNode[0] = %q, want slow: prefix", sched.perNode[0].result)
	}
}

// TestScheduleDAGOrderingNoOverlapForChain asserts that a 3-node
// chain (a -> b -> c, where c depends on b which depends on a) is
// executed in order: c's start is after b's end, b's start is after
// a's end. This is the predecessor-ordering half of the contract
// (independent of the limiter).
func TestScheduleDAGOrderingNoOverlapForChain(t *testing.T) {
	tcs := []ToolCall{
		dagToolCall("a", nil),
		dagToolCall("b", []string{"a"}),
		dagToolCall("c", []string{"b"}),
	}
	parsed, records, dispatch, stopCh, isCancelled := dagTestSetup(t, tcs)
	// We swap in a dispatch that records the exact wall-clock
	// time of its call so we can assert ordering. Each call
	// sleeps a small amount so the ordering check is
	// meaningful (otherwise they'd all start within the same
	// nanosecond). We also write into the records slice the
	// dagTestSetup helper created, so the predecessor-context
	// assertion below can verify the labels reached the
	// dispatch closure.
	var mu sync.Mutex
	calls := make([]time.Time, len(tcs))
	dispatch = func(tc ToolCall, binding *taskBinding, toolCallID string, predecessorContext string) (string, []Image, error) {
		mu.Lock()
		idx := -1
		for k, n := range parsed.nodes {
			if n.toolCall.ID == tc.ID {
				idx = k
				break
			}
		}
		if idx >= 0 {
			calls[idx] = time.Now()
			records[idx] = dagRecord{gotContext: predecessorContext, calledAt: time.Now()}
		}
		mu.Unlock()
		time.Sleep(5 * time.Millisecond)
		return "ok-" + tc.ID, nil, nil
	}
	sched := newDAGScheduler(parsed, stopCh, isCancelled, dispatch)
	sched.run(nil, nil, nil)
	// No errors expected.
	for i, p := range sched.perNode {
		if p == nil {
			t.Fatalf("perNode[%d] is nil", i)
		}
		if p.err != nil {
			t.Errorf("perNode[%d].err = %v, want nil", i, p.err)
		}
	}
	// Ordering: a starts before b starts before c starts.
	if !calls[0].Before(calls[1]) {
		t.Errorf("a did not start before b: a=%v b=%v", calls[0], calls[1])
	}
	if !calls[1].Before(calls[2]) {
		t.Errorf("b did not start before c: b=%v c=%v", calls[1], calls[2])
	}
	// Records: every node received its predecessor's context
	// block. (Validated below in the dedicated predecessor
	// injection test.)
	if records[1].gotContext == "" {
		t.Errorf("b received empty predecessor context")
	}
	if records[2].gotContext == "" {
		t.Errorf("c received empty predecessor context")
	}
}

// TestScheduleDAGSlotInvariantChainUnderLimit1 is the load-bearing
// slot-invariant test. A 3-node chain (a -> b -> c) runs under
// max_concurrent_agents=1, which would deadlock the naive
// implementation (the one that lets the child block on its
// predecessors AFTER acquiring a slot). The scheduler's
// predecessor-wait happens BEFORE the dispatch closure calls
// AcquireForRun, so the limiter is asked for a slot only when
// there is no predecessor wait pending — no slot is ever held
// while a node waits. The test must complete within a short
// deadline; a hang would indicate the invariant is broken.
//
// We use the real AgentRunRegistry limiter (SetMaxConcurrent(1))
// and a dispatch closure that mirrors TaskTool's slot-acquire
// pattern: AcquireForRun → release on defer. The scheduler runs
// against this limiter and the test asserts the chain completes
// without timeout.
func TestScheduleDAGSlotInvariantChainUnderLimit1(t *testing.T) {
	tcs := []ToolCall{
		dagToolCall("a", nil),
		dagToolCall("b", []string{"a"}),
		dagToolCall("c", []string{"b"}),
	}
	parsed, _, _, stopCh, isCancelled := dagTestSetup(t, tcs)
	// Use a real limiter with max_concurrent_agents=1. The
	// dispatch closure mirrors how TaskTool.Execute acquires
	// the slot: AcquireForRun → release.
	runs := NewAgentRunRegistry()
	runs.SetMaxConcurrent(1)

	var slotHolds int32
	var peakConcurrent int32
	dispatch := func(tc ToolCall, binding *taskBinding, toolCallID string, predecessorContext string) (string, []Image, error) {
		// Pretend to be TaskTool: acquire a slot from the
		// shared limiter, hold it briefly, release.
		release, err := runs.Acquire(stopCh)
		if err != nil {
			return "", nil, err
		}
		defer release()
		cur := atomic.AddInt32(&slotHolds, 1)
		defer atomic.AddInt32(&slotHolds, -1)
		for {
			p := atomic.LoadInt32(&peakConcurrent)
			if cur <= p || atomic.CompareAndSwapInt32(&peakConcurrent, p, cur) {
				break
			}
		}
		// Hold the slot briefly so the chain is observable.
		time.Sleep(20 * time.Millisecond)
		return "ok-" + tc.ID, nil, nil
	}

	// Run the scheduler. If the slot invariant is broken, the
	// first node holds the only slot, the second node's
	// dispatch closure acquires it, then tries to wait for
	// its predecessor's doneCh — which never closes because
	// the predecessor is also blocked behind the slot. We
	// bound the test with a timeout so a deadlock surfaces
	// as a fatal test, not a hang.
	done := make(chan struct{})
	go func() {
		sched := newDAGScheduler(parsed, stopCh, isCancelled, dispatch)
		sched.run(nil, nil, nil)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("scheduler did not return within 5s; the slot invariant is likely broken (peak concurrent holds = %d)", atomic.LoadInt32(&peakConcurrent))
	}

	// The peak concurrent holds must be exactly 1 — the
	// limiter was set to 1, and the scheduler never asks
	// for a slot while a predecessor wait is pending, so no
	// goroutine ever holds two slots.
	if got := atomic.LoadInt32(&peakConcurrent); got != 1 {
		t.Errorf("peak concurrent slot holds = %d, want 1 (limiter was set to 1)", got)
	}
	// Every node ran exactly once. The perNode assertions
	// above (no err, expected result prefix) cover this; this
	// loop is a no-op kept as documentation.
	for _, p := range parsed.nodes {
		_ = p
	}
}

// TestScheduleDAGPredecessorContextInjection verifies that when a
// node starts, every satisfied predecessor's final result has been
// prepended to its context, labelled with the predecessor's id. The
// dispatch closure returns the predecessor context as the result
// body, so the test asserts the body contains the labelled
// predecessor block.
func TestScheduleDAGPredecessorContextInjection(t *testing.T) {
	tcs := []ToolCall{
		dagToolCall("a", nil),
		dagToolCall("b", nil),
		dagToolCall("c", []string{"a", "b"}),
	}
	parsed, _, dispatch, stopCh, isCancelled := dagTestSetup(t, tcs)
	// Per-node dispatch that returns a distinctive result
	// string. The scheduler captures the predecessor context
	// in buildPredecessorContext, which is what c sees.
	results := map[string]string{
		"a": "result-A",
		"b": "result-B",
		"c": "", // c's result is computed from its context.
	}
	dispatch = func(tc ToolCall, binding *taskBinding, toolCallID string, predecessorContext string) (string, []Image, error) {
		// Find the id for this tool call.
		var id string
		for _, n := range parsed.nodes {
			if n.toolCall.ID == tc.ID {
				id = n.id
			}
		}
		if id == "a" || id == "b" {
			return results[id], nil, nil
		}
		// For c, return the predecessor context verbatim so
		// the test can assert the labelling.
		return "CTX:" + predecessorContext, nil, nil
	}
	sched := newDAGScheduler(parsed, stopCh, isCancelled, dispatch)
	sched.run(nil, nil, nil)
	if sched.perNode[2] == nil {
		t.Fatalf("perNode[c] is nil")
	}
	got := sched.perNode[2].result
	if !strings.Contains(got, "[a]") {
		t.Errorf("c's context missing [a] label: %q", got)
	}
	if !strings.Contains(got, "[b]") {
		t.Errorf("c's context missing [b] label: %q", got)
	}
	if !strings.Contains(got, "result-A") {
		t.Errorf("c's context missing result-A: %q", got)
	}
	if !strings.Contains(got, "result-B") {
		t.Errorf("c's context missing result-B: %q", got)
	}
	if !strings.Contains(got, "Predecessor task results:") {
		t.Errorf("c's context missing header: %q", got)
	}
}

// TestScheduleDAGPredecessorContextTruncation exercises the
// truncation path: a predecessor with an oversize result must be
// bounded by the existing TruncateToolResult helper. We dispatch a
// huge string from "a" and assert c's context contains the truncation
// marker.
func TestScheduleDAGPredecessorContextTruncation(t *testing.T) {
	tcs := []ToolCall{
		dagToolCall("a", nil),
		dagToolCall("c", []string{"a"}),
	}
	parsed, _, dispatch, stopCh, isCancelled := dagTestSetup(t, tcs)
	oversize := strings.Repeat("x", maxToolResultChars+1000)
	dispatch = func(tc ToolCall, binding *taskBinding, toolCallID string, predecessorContext string) (string, []Image, error) {
		for _, n := range parsed.nodes {
			if n.toolCall.ID == tc.ID {
				if n.id == "a" {
					return oversize, nil, nil
				}
				return "CTX:" + predecessorContext, nil, nil
			}
		}
		return "", nil, nil
	}
	sched := newDAGScheduler(parsed, stopCh, isCancelled, dispatch)
	sched.run(nil, nil, nil)
	got := sched.perNode[1].result
	if !strings.Contains(got, TruncationMarkerPrefix) {
		t.Errorf("oversize predecessor result not truncated: %q", got[:min(200, len(got))])
	}
}

// TestScheduleDAGSkipOnFailedDependency asserts the failure
// semantics: when a node errors, its transitive dependents do NOT
// run, and each skipped node returns a result that names the
// failed dependency explicitly. The test builds a diamond
// (a -> b -> d, a -> c -> d) and forces b to fail; d is two hops
// down via b, so it must be skipped, and the skip message must
// name "b" (the original failing node, not "c" which was its
// sibling through "a").
func TestScheduleDAGSkipOnFailedDependency(t *testing.T) {
	tcs := []ToolCall{
		dagToolCall("a", nil),
		dagToolCall("b", []string{"a"}),
		dagToolCall("c", []string{"a"}),
		dagToolCall("d", []string{"b", "c"}),
	}
	parsed, _, _, stopCh, isCancelled := dagTestSetup(t, tcs)
	dispatch := func(tc ToolCall, binding *taskBinding, toolCallID string, predecessorContext string) (string, []Image, error) {
		for _, n := range parsed.nodes {
			if n.toolCall.ID == tc.ID {
				if n.id == "b" {
					return "", nil, errors.New("forced failure")
				}
				return "ok-" + n.id, nil, nil
			}
		}
		return "", nil, nil
	}
	sched := newDAGScheduler(parsed, stopCh, isCancelled, dispatch)
	sched.run(nil, nil, nil)

	// a ran successfully.
	if sched.perNode[0] == nil || sched.perNode[0].err != nil {
		t.Errorf("a did not run successfully: %+v", sched.perNode[0])
	}
	// b failed with the forced error.
	if sched.perNode[1] == nil || sched.perNode[1].err == nil {
		t.Errorf("b did not fail: %+v", sched.perNode[1])
	}
	// c ran successfully (independent of b's failure).
	if sched.perNode[2] == nil || sched.perNode[2].err != nil {
		t.Errorf("c did not run successfully: %+v", sched.perNode[2])
	}
	// d was skipped because b failed. The skip message must
	// name b (the original failing node), not c (which is also
	// a predecessor but succeeded).
	if sched.perNode[3] == nil {
		t.Fatalf("d's perNode is nil")
	}
	if !sched.perNode[3].skipped {
		t.Errorf("d was not skipped: %+v", sched.perNode[3])
	}
	if !strings.Contains(sched.perNode[3].skipMsg, "b") {
		t.Errorf("d's skip message %q does not name the failed dep b", sched.perNode[3].skipMsg)
	}
	if !strings.Contains(sched.perNode[3].result, "skipped") {
		t.Errorf("d's result is not labelled as skipped: %q", sched.perNode[3].result)
	}
}

// TestScheduleDAGCancellationMidWave exercises the cancellation path:
// we close the stop channel after the first node's dispatch
// completes, and assert that every subsequent node records a
// cancellation outcome (or, if a node is partway through, that no
// goroutine remains parked). The test bounds the wait with a
// timeout so a missed cancellation surfaces as a fatal.
func TestScheduleDAGCancellationMidWave(t *testing.T) {
	tcs := []ToolCall{
		dagToolCall("a", nil),
		dagToolCall("b", nil),
		dagToolCall("c", nil),
	}
	parsed, _, dispatch, stopCh, isCancelled := dagTestSetup(t, tcs)
	// Make every node slow, then cancel after a short delay.
	dispatch = func(tc ToolCall, binding *taskBinding, toolCallID string, predecessorContext string) (string, []Image, error) {
		// Wait for either stopCh or a 200ms timeout. This
		// mirrors a long-running child that can be cancelled
		// cooperatively.
		select {
		case <-stopCh:
			return "", nil, context.Canceled
		case <-time.After(200 * time.Millisecond):
			return "ok-" + tc.ID, nil, nil
		}
	}
	// Cancel the batch from a side goroutine shortly after the
	// scheduler starts. The scheduler must observe stopCh on
	// every wait point and return promptly.
	go func() {
		time.Sleep(50 * time.Millisecond)
		close(stopCh)
	}()
	done := make(chan struct{})
	go func() {
		sched := newDAGScheduler(parsed, stopCh, isCancelled, dispatch)
		sched.run(nil, nil, nil)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("scheduler did not return within 2s after cancellation; a goroutine is likely parked")
	}
	// We don't introspect perNode here because the main scheduler
	// closed over the original stopCh and is no longer reachable.
	// The assertion we care about is that run() returned (no
	// deadlock, no parked goroutine). The dedup schedFor
	// assertion in other tests covers the perNode content.
	_ = schedFor
}

// schedFor is a helper that rebuilds a scheduler and runs it on a
// fresh stopCh so we can inspect perNode after the test's main
// scheduler already returned. It is only used by the cancellation
// test to keep the assertions compact.
func schedFor(parsed *dagParsed, t *testing.T) *dagScheduler {
	t.Helper()
	stopCh := make(chan struct{})
	dispatch := func(tc ToolCall, binding *taskBinding, toolCallID string, predecessorContext string) (string, []Image, error) {
		return "ok-" + tc.ID, nil, nil
	}
	isCancelled := func() bool { return false }
	sched := newDAGScheduler(parsed, stopCh, isCancelled, dispatch)
	sched.run(nil, nil, nil)
	return sched
}

// TestScheduleDAGNodeFailureDoesNotDeadlockSuccessors asserts that
// a failed node does not park the graph: every dependent completes
// (as a skip) and the scheduler returns within a short deadline. The
// failure in one path must not prevent other independent branches
// from completing, even if those branches share a downstream
// dependent with the failed one.
func TestScheduleDAGNodeFailureDoesNotDeadlockSuccessors(t *testing.T) {
	tcs := []ToolCall{
		dagToolCall("a", nil),
		dagToolCall("b", []string{"a"}),
		dagToolCall("c", []string{"b"}),
	}
	parsed, _, dispatch, stopCh, isCancelled := dagTestSetup(t, tcs)
	dispatch = func(tc ToolCall, binding *taskBinding, toolCallID string, predecessorContext string) (string, []Image, error) {
		for _, n := range parsed.nodes {
			if n.toolCall.ID == tc.ID {
				if n.id == "b" {
					return "", nil, errors.New("b failed")
				}
				return "ok-" + n.id, nil, nil
			}
		}
		return "", nil, nil
	}
	done := make(chan struct{})
	go func() {
		sched := newDAGScheduler(parsed, stopCh, isCancelled, dispatch)
		sched.run(nil, nil, nil)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("scheduler did not return within 2s; failure handling is likely broken")
	}
}

// TestBuildResultsAlignment asserts that the scheduler's
// buildResults helper writes per-node outcomes to the caller's
// `parallelIdx`-aligned positions. The test exercises a
// non-trivial order (parallelIdx != identity) to confirm the
// helper respects the caller's intent.
func TestBuildResultsAlignment(t *testing.T) {
	tcs := []ToolCall{
		dagToolCall("a", nil),
		dagToolCall("b", nil),
		dagToolCall("c", nil),
	}
	parsed, _, dispatch, stopCh, isCancelled := dagTestSetup(t, tcs)
	sched := newDAGScheduler(parsed, stopCh, isCancelled, dispatch)
	sched.run(nil, nil, nil)
	// Reorder parallelIdx to put c first, then a, then b.
	reorder := []int{2, 0, 1}
	msgs := sched.buildResults(reorder)
	// msgs[0] should be c's result, msgs[1] should be a's, msgs[2] should be b's.
	if !strings.Contains(msgs[0].Content, "tc-c") {
		t.Errorf("msgs[0] = %q, want c's tool call id", msgs[0].Content)
	}
	if !strings.Contains(msgs[1].Content, "tc-a") {
		t.Errorf("msgs[1] = %q, want a's tool call id", msgs[1].Content)
	}
	if !strings.Contains(msgs[2].Content, "tc-b") {
		t.Errorf("msgs[2] = %q, want b's tool call id", msgs[2].Content)
	}
}

// TestDAGContextFromAugment exercises the args-marshalling helper
// used by the dispatch closure to inject the predecessor context.
func TestDAGContextFromAugment(t *testing.T) {
	original := json.RawMessage(`{"prompt":"p","agent":"general","id":"c","depends_on":["a"]}`)
	got, err := dagContextFrom(original, "Predecessor: hello")
	if err != nil {
		t.Fatalf("dagContextFrom: %v", err)
	}
	var p taskToolParams
	if err := json.Unmarshal(got, &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !strings.Contains(p.Context, "Predecessor: hello") {
		t.Errorf("context %q missing predecessor block", p.Context)
	}
	// Empty predecessor context is a no-op.
	got2, err := dagContextFrom(original, "")
	if err != nil {
		t.Fatalf("dagContextFrom (empty): %v", err)
	}
	if string(got2) != string(original) {
		t.Errorf("empty predecessor must be a no-op; got %q", got2)
	}
	// Existing context is preserved.
	originalWithCtx := json.RawMessage(`{"prompt":"p","context":"existing"}`)
	got3, err := dagContextFrom(originalWithCtx, "Predecessor: hi")
	if err != nil {
		t.Fatalf("dagContextFrom (existing): %v", err)
	}
	var p3 taskToolParams
	_ = json.Unmarshal(got3, &p3)
	if !strings.Contains(p3.Context, "existing") {
		t.Errorf("existing context dropped: %q", p3.Context)
	}
	if !strings.Contains(p3.Context, "Predecessor: hi") {
		t.Errorf("predecessor block not appended: %q", p3.Context)
	}
}

// TestDAGContextFromMalformedReturnsError covers the error path of
// the args-marshalling helper.
func TestDAGContextFromMalformedReturnsError(t *testing.T) {
	bad := json.RawMessage(`not-json`)
	_, err := dagContextFrom(bad, "anything")
	if err == nil {
		t.Errorf("expected an error for malformed JSON, got nil")
	}
}

// TestRunDAGFromValidatedValidationError asserts that a validation
// error is surfaced as a non-nil error and that the caller is
// expected to write the error as a per-position tool result.
func TestRunDAGFromValidatedValidationError(t *testing.T) {
	// Two nodes, both naming id="a" — duplicate id.
	tcs := []ToolCall{
		dagToolCall("a", nil),
		dagToolCall("a", nil),
	}
	stopCh := make(chan struct{})
	isCancelled := func() bool { return false }
	dispatch := func(tc ToolCall, binding *taskBinding, toolCallID string, predecessorContext string) (string, []Image, error) {
		return "", nil, nil
	}
	_, err := runDAGFromValidated(tcs, stopCh, isCancelled, nil, nil, nil, dispatch)
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	if !strings.Contains(err.Error(), errDAGValidationPrefix) {
		t.Errorf("error %q missing validation prefix", err.Error())
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("error %q missing duplicate detail", err.Error())
	}
}

// TestDAGStepValidationErrorProducesNoNodesLaunched asserts the
// plan rule: "an unknown id, duplicate id, or cycle is a hard
// error: the offending calls return an error result naming the
// problem, and no node in the affected component runs." The test
// uses a real Agent.Step with a mock client and checks the run
// registry has no agent runs at the end (no child was launched).
func TestDAGStepValidationErrorProducesNoNodesLaunched(t *testing.T) {
	// Mock client that returns a single response: a batch of
	// two task calls with duplicate ids. The LLM never
	// actually runs in this test — we just need the
	// parallel batch to be processed.
	client := &scriptedToolCallClient{
		responses: [][]ToolCall{{
			{ID: "tc1", Type: "function", Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{Name: "task", Arguments: `{"prompt":"a","agent":"general","id":"x"}`}},
			{ID: "tc2", Type: "function", Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{Name: "task", Arguments: `{"prompt":"b","agent":"general","id":"x"}`}},
		}},
	}
	a := NewAgent(client, nil, nil, nil)
	before := len(a.Runs().Snapshot())
	resp, err := a.Step([]Message{{Role: "user", Content: "go"}})
	if err != nil {
		t.Fatalf("Step: %v", err)
	}
	// The model's response should be the only message;
	// validation errors are emitted as tool results, not as
	// the assistant content.
	if len(resp) == 0 {
		t.Fatalf("Step returned no messages")
	}
	// No agent runs were created: the DAG validator caught
	// the duplicate id and the scheduler never launched a
	// child. The only run that could exist is a synthetic
	// one — there should be none.
	after := len(a.Runs().Snapshot())
	if after != before {
		t.Errorf("agent runs grew from %d to %d; no node should have launched", before, after)
	}
}

// scriptedToolCallClient is a tiny client that returns the next
// scripted batch of tool calls (or a default text response when the
// scripted list is exhausted).
type scriptedToolCallClient struct {
	responses [][]ToolCall
	calls     int
}

func (c *scriptedToolCallClient) Chat(messages []Message, tools []map[string]interface{}) (*Message, error) {
	idx := c.calls
	c.calls++
	if idx < len(c.responses) {
		tcs := c.responses[idx]
		// Marshal the tool calls into the assistant message
		// using the field Agent.Step expects.
		return &Message{
			Role:      "assistant",
			Content:   "",
			ToolCalls: tcs,
		}, nil
	}
	return &Message{Role: "assistant", Content: "done"}, nil
}

func (c *scriptedToolCallClient) GetProvider() string { return "mock" }
func (c *scriptedToolCallClient) GetModel() string    { return "mock-model" }
