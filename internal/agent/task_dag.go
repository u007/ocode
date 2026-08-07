package agent

// In-batch DAG scheduling for the `task` tool.
//
// When a parallel batch contains at least one `task` call that declares an
// `id` or `depends_on`, the legacy WaitGroup fan-out is replaced by the
// scheduler in this file. The scheduler is invoked from the parallel
// section of Agent.Step; it walks the graph wave-by-wave, never acquires a
// concurrency slot until every predecessor has resolved, and writes its
// per-node outcomes back into the caller's `results []Message` slice at
// the pre-computed positions so the surrounding loop is byte-identical to
// the legacy path.
//
// Omitting both fields preserves the legacy flat fan-out — `dagHasDependencies`
// is the gate.
//
// The slot invariant (a node must not acquire a concurrency slot until every
// one of its dependencies has resolved) is load-bearing: the alternative —
// letting a child block on its predecessors *after* it has acquired a slot —
// deadlocks a 3-node chain under `max_concurrent_agents=2`, because the
// first child holds the only available slot and the next child cannot start.
// All waiting here happens in the scheduler, never inside the dispatched
// child.

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/u007/ocode/internal/notebus"
	"github.com/u007/ocode/internal/tool"
)

// dagNode is one entry in the parsed task-batch DAG.
//
// We keep only what the scheduler needs: the position in the original
// parallel batch (so the result can be written back into the caller's
// `results []Message` slice at the right index), the id (caller-chosen
// label, "" if anonymous), the predecessor ids, and the original tool call
// so the caller can dispatch it.
type dagNode struct {
	idx      int
	id       string
	deps     []string
	toolCall ToolCall
	// background is true when this node's task call carries
	// run_in_background (or its `background` alias). Such a node returns a
	// "state: running" placeholder immediately rather than a result, so it
	// may not be named as a predecessor — see buildDAG.
	background bool
}

// isDAGEligibleCall reports whether a parallel tool call participates in the
// dependency graph. ONLY subagent dispatches do.
//
// This filter is load-bearing. `id` is a perfectly ordinary parameter name:
// `agent_status`, `bash_output`, and `kill_shell` are all Parallel() with a
// REQUIRED `id`. Without this check a batch containing a single
// `bash_output{"id":"shell-1"}` and no task calls at all would route through
// the scheduler, and two `bash_output` calls on different shells would collide
// on the duplicate-id rule and neither would run.
//
// Non-eligible calls still become anonymous nodes so they are dispatched
// normally in the first frontier — they are simply invisible to the id
// namespace and to depends_on.
func isDAGEligibleCall(tc ToolCall) bool {
	return tc.Function.Name == "task" || tc.Function.Name == "agent"
}

// dagParsed is the validator output: every node in the batch, the set of
// nodes that declared an id or a depends_on, and a map of id → node for
// fast lookup when resolving predecessors.
//
// `hasDependencies` is true when at least one call in the parallel batch
// declared either field. The fan-out gate calls this to decide whether to
// route through the scheduler or run the unchanged legacy path.
type dagParsed struct {
	nodes           []*dagNode
	named           map[string]*dagNode
	hasDependencies bool
}

// dagHasDependencies is the cheap pre-check the fan-out calls. It returns
// true when at least one parallel tool call in the batch declares an `id`
// or a non-empty `depends_on`. The argument is the sub-slice of tool calls
// that already passed the parallel-tool filter; we only look at those.
func dagHasDependencies(parallelCalls []ToolCall) bool {
	for _, tc := range parallelCalls {
		if !isDAGEligibleCall(tc) {
			continue
		}
		var p struct {
			ID   string   `json:"id"`
			Deps []string `json:"depends_on"`
		}
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &p); err != nil {
			// Malformed JSON is somebody else's problem; the existing
			// dispatch path will report the parse error per node. The
			// DAG route must not be the first place such an error
			// surfaces.
			continue
		}
		if p.ID != "" || len(p.Deps) > 0 {
			return true
		}
	}
	return false
}

// buildDAG parses a parallel batch into the validator's view. It enforces:
//  1. depends_on with empty id is rejected (you cannot name a predecessor
//     without naming yourself).
//  2. duplicate ids are rejected.
//  3. self-edges (a node names its own id) are rejected.
//  4. unknown ids in depends_on are rejected.
//  5. cycles are rejected.
//
// The returned `hasDependencies` is true if any node declared an id or a
// depends_on — even an invalid batch — so the caller can route through
// the scheduler (which then emits the validation error as a per-node
// result and skips the affected component).
func buildDAG(parallelCalls []ToolCall) (*dagParsed, error) {
	p := &dagParsed{
		nodes: make([]*dagNode, len(parallelCalls)),
		named: map[string]*dagNode{},
	}

	// First pass: parse every call, assign sequential nodes, and
	// detect has-dependencies + duplicates + id-less-with-deps +
	// self-edges.
	seenID := map[string]int{}
	for i, tc := range parallelCalls {
		if !isDAGEligibleCall(tc) {
			// Not a subagent dispatch: it keeps its slot in the batch and
			// is dispatched as an anonymous node, but its arguments are
			// never parsed for id/depends_on. See isDAGEligibleCall.
			p.nodes[i] = &dagNode{idx: i, toolCall: tc}
			continue
		}
		var raw struct {
			ID         string   `json:"id"`
			Deps       []string `json:"depends_on"`
			Background bool     `json:"run_in_background"`
			BgAlias    bool     `json:"background"`
		}
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &raw); err != nil {
			// Leave node constructed with empty id/deps; the
			// existing dispatch path will report the JSON error.
			// We still record the node so anonymous (no id/deps)
			// calls in the same batch keep the byte-identical
			// legacy path.
			p.nodes[i] = &dagNode{idx: i, toolCall: tc}
			continue
		}
		if raw.ID != "" || len(raw.Deps) > 0 {
			p.hasDependencies = true
		}
		if len(raw.Deps) > 0 && raw.ID == "" {
			return nil, fmt.Errorf("task tool call at index %d declares depends_on but has no id; every node with a dependency must name itself", i)
		}
		if raw.ID != "" {
			if prev, dup := seenID[raw.ID]; dup {
				return nil, fmt.Errorf("duplicate id %q on task tool call (first seen at index %d, again at %d)", raw.ID, prev, i)
			}
			seenID[raw.ID] = i
		}
		for _, d := range raw.Deps {
			if d == raw.ID {
				return nil, fmt.Errorf("task tool call %q (index %d) depends on its own id", raw.ID, i)
			}
		}
		p.nodes[i] = &dagNode{
			idx:        i,
			id:         raw.ID,
			deps:       raw.Deps,
			toolCall:   tc,
			background: raw.Background || raw.BgAlias,
		}
		if raw.ID != "" {
			p.named[raw.ID] = p.nodes[i]
		}
	}

	// Second pass: resolve every depends_on id; reject unknowns. We
	// only check this when hasDependencies is true, because an
	// all-anonymous batch is the legacy path and must not be perturbed
	// by a validator error.
	if p.hasDependencies {
		for _, n := range p.nodes {
			for _, d := range n.deps {
				dep, ok := p.named[d]
				if !ok {
					return nil, fmt.Errorf("task tool call %q (index %d) depends on unknown id %q (every id in depends_on must be set on a sibling task call in the same parallel batch)", n.id, n.idx, d)
				}
				// A background dispatch returns a "state: running"
				// placeholder immediately instead of a result, so a
				// dependent would be released against a placeholder and
				// silently run without the input it was promised. Reject
				// the edge rather than let that happen.
				if dep.background {
					return nil, fmt.Errorf("task tool call %q (index %d) depends on %q, which sets run_in_background; a background dispatch returns immediately with a run id instead of a result, so it cannot be a predecessor — drop run_in_background from %q or remove the dependency", n.id, n.idx, d, d)
				}
			}
		}
	}

	// Third pass: detect cycles via DFS with three-colour marking
	// (white = unvisited, grey = on the current path, black = fully
	// explored). This is O(V+E) and the only piece of the validator
	// that can return a non-obvious error: print the cycle path so the
	// model can fix it.
	if p.hasDependencies {
		const (
			white = 0
			grey  = 1
			black = 2
		)
		colour := make(map[*dagNode]int, len(p.nodes))
		for _, n := range p.nodes {
			colour[n] = white
		}
		var path []string
		var cycleErr error
		var visit func(n *dagNode) bool
		visit = func(n *dagNode) bool {
			if cycleErr != nil {
				return false
			}
			colour[n] = grey
			path = append(path, n.id)
			for _, d := range n.deps {
				dep, ok := p.named[d]
				if !ok {
					// Already caught by the unknown-id pass.
					continue
				}
				switch colour[dep] {
				case grey:
					// Found a back-edge: dep is on the current
					// path ahead of n. Print the cycle slice
					// so the model can locate it.
					start := 0
					for i, p := range path {
						if p == dep.id {
							start = i
							break
						}
					}
					cycle := append([]string{}, path[start:]...)
					cycle = append(cycle, dep.id)
					cycleErr = fmt.Errorf("task dependency cycle detected: %s", strings.Join(cycle, " -> "))
					return false
				case white:
					if !visit(dep) {
						return false
					}
				}
			}
			colour[n] = black
			path = path[:len(path)-1]
			return true
		}
		// Sort the visit order by id so error messages are stable
		// across runs (map iteration would otherwise shuffle them).
		order := make([]*dagNode, 0, len(p.nodes))
		for _, n := range p.nodes {
			if n.id != "" {
				order = append(order, n)
			}
		}
		sort.Slice(order, func(i, j int) bool { return order[i].id < order[j].id })
		for _, n := range order {
			if colour[n] == white {
				if !visit(n) {
					break
				}
			}
		}
		if cycleErr != nil {
			return nil, cycleErr
		}
	}

	return p, nil
}

// dagNodeResult is the per-node outcome the scheduler writes back into the
// caller's `results []Message` slice at the pre-computed index. The caller
// treats success and failure identically from the position-in-slice
// perspective: both populate the slot.
type dagNodeResult struct {
	idx     int
	skipped bool
	skipMsg string
	result  string
	err     error
	images  []Image
}

// shapeToolResult converts a raw tool outcome into the three fields a tool
// Message carries: the (possibly truncated) content, the full text for the
// TUI's expanded view when truncation happened, and the user-facing notice.
//
// Both the legacy parallel fan-out and the DAG scheduler call this, so a call
// routed through the scheduler keeps its NoticedError notice and its
// DisplayContent instead of silently losing them.
func shapeToolResult(toolName, callID, result string, err error) (content, display, notice string) {
	if err != nil {
		var ne *tool.NoticedError
		if errors.As(err, &ne) {
			notice = ne.Notice
		}
		result = fmt.Sprintf("Error: %v", err)
	} else if toolName == "imagegen" {
		// Only imagegen currently embeds a display-only cost notice behind
		// SuccessNoticeSeparator. Gate the split to that tool so the shared
		// result path is not parsed for every tool call.
		if idx := strings.Index(result, tool.SuccessNoticeSeparator); idx >= 0 {
			notice = result[:idx]
			result = result[idx+len(tool.SuccessNoticeSeparator):]
		}
	}
	full := result
	content = TruncateToolResult(callID, result)
	if full != content {
		display = full
	}
	return content, display, notice
}

// dagDispatchFn runs a single node. The scheduler owns slot acquisition
// (through the dispatch closure's call into TaskTool, which calls
// AcquireForRun internally) and predecessor waiting; the closure only
// executes the tool call. It returns the same shape as
// handleToolCallWithImages (string, []Image, error) so the scheduler
// builds a tool-result Message the same way the legacy path does.
//
// predecessorContext is the labelled, truncated concatenation of every
// satisfied predecessor's final result; the dispatch closure is
// responsible for re-marshalling the tool-call arguments with this
// context baked in (see `dagContextFrom`).
type dagDispatchFn func(tc ToolCall, binding *taskBinding, toolCallID string, predecessorContext string) (string, []Image, error)

// dagScheduler runs the parsed DAG. It is created once per batch and
// outlives any single node's execution: the reconcile handoff + bus
// teardown live above the scheduler call in Agent.Step, and the scheduler
// returns only after every node has resolved (success, failure, or skip).
type dagScheduler struct {
	parsed      *dagParsed
	stopCh      <-chan struct{}
	isCancelled func() bool
	dispatch    dagDispatchFn

	// perNode stores the final per-node outcome; the caller reads it
	// once we return and copies into the `results []Message` slice.
	perNode []*dagNodeResult

	// doneCh is closed when the node's execution is complete (success
	// or terminal failure or skip). Dependents select on it BEFORE
	// dispatching — this is the slot invariant: a dependent does not
	// start its work until its predecessors are done.
	doneCh map[*dagNode]chan struct{}

	// failed records whether a node errored (vs. being skipped). The
	// scheduler reads this when composing a transitive dependent's
	// skip message: we name the FIRST failed predecessor in the
	// chain, not the closest skipper.
	failed map[*dagNode]bool

	// aborted signals the wave scheduler that no further work should
	// be launched — set true on cancellation or any node failure.
	// The wave scheduler is the only writer; node goroutines read it
	// under `mu` to decide between dispatching and recording a skip.
	aborted     bool
	abortReason string
	mu          sync.Mutex
}

// newDAGScheduler wires the parsed graph, the stop channel, and the
// dispatch closure. It does not start any work — the caller invokes
// `run`.
func newDAGScheduler(parsed *dagParsed, stopCh <-chan struct{}, isCancelled func() bool, dispatch dagDispatchFn) *dagScheduler {
	s := &dagScheduler{
		parsed:      parsed,
		stopCh:      stopCh,
		isCancelled: isCancelled,
		dispatch:    dispatch,
		perNode:     make([]*dagNodeResult, len(parsed.nodes)),
		doneCh:      make(map[*dagNode]chan struct{}, len(parsed.nodes)),
		failed:      make(map[*dagNode]bool, len(parsed.nodes)),
	}
	for _, n := range parsed.nodes {
		s.doneCh[n] = make(chan struct{})
	}
	return s
}

// run executes the DAG. The contract: when run returns, every node has a
// perNode result (success, error, or skipped) and every doneCh is closed.
// No goroutine remains parked: dependents that observed cancellation or
// failure exit via the done-channel select and never launch a child.
//
// The implementation is a per-node goroutine that waits for each
// predecessor's doneCh sequentially before deciding whether to dispatch.
// This is the slot invariant's load-bearing piece: by the time a
// dispatch goroutine reaches the dispatch closure (which in turn calls
// AcquireForRun), every predecessor has already resolved, so the limiter
// is asked for a slot in the same atomic step as the dispatch — there is
// no window in which the child holds a slot and waits on a predecessor
// (which would deadlock under a tight limit).
//
// Failure semantics: any node that returns an error marks the graph
// `aborted`. Subsequent node goroutines see the flag on dispatch and
// record a skip result instead of running. The skip message names the
// failed predecessor; for a transitive dependent two hops down, the
// message names the ORIGINAL failed node, not the intermediate skipper.
//
// Cancellation: the stopCh is observed at every wait point (predecessor
// doneCh select). When the channel closes, every unstarted node is
// recorded as cancelled, every in-flight node's `isCancelled` check
// returns true on its next iteration, and `run` returns once the in-flight
// set drains. No goroutine remains parked because the waits are selects
// on `doneCh` vs `stopCh`, and stopCh always wins.
func (s *dagScheduler) run(groupBus *notebus.Bus, groupAgentIDs []string, groupTracker *groupTracker) {
	// inDegree tracks how many of a node's predecessors are still
	// unresolved. A node with in-degree 0 is ready to dispatch as
	// soon as the scheduler reaches it.
	inDegree := make(map[*dagNode]int, len(s.parsed.nodes))
	// successors: every node that names `n` as a predecessor. We use
	// this to decrement in-degree on completion without scanning the
	// full graph per node.
	successors := make(map[*dagNode][]*dagNode, len(s.parsed.nodes))
	for _, n := range s.parsed.nodes {
		inDegree[n] = len(n.deps)
		for _, d := range n.deps {
			dep := s.parsed.named[d]
			successors[dep] = append(successors[dep], n)
		}
	}

	// frontier holds the set of nodes whose in-degree has just hit
	// zero and that should be dispatched. The scheduler goroutine
	// picks nodes off the frontier and launches their per-node
	// goroutines.
	var frontier []*dagNode
	for _, n := range s.parsed.nodes {
		if inDegree[n] == 0 {
			frontier = append(frontier, n)
		}
	}

	// activeCount is the number of in-flight nodes (dispatched and
	// not yet recorded a perNode result). The scheduler waits on the
	// cond until activeCount hits zero, signalling the graph is
	// fully drained.
	var activeCount int
	var frontierMu sync.Mutex
	frontierCond := sync.NewCond(&frontierMu)

	// The only place that adds to the frontier is nodeDone, which
	// holds frontierMu.

	// signalAborted flips the aborted flag and broadcasts so any
	// goroutine blocked in the cond (e.g. waiting for a successor
	// that will never come) wakes up and re-evaluates.
	signalAborted := func(reason string) {
		s.mu.Lock()
		if !s.aborted {
			s.aborted = true
			s.abortReason = reason
		}
		s.mu.Unlock()
		frontierCond.Broadcast()
	}

	// nodeDone is the bookkeeping every node goroutine runs in its
	// defer: decrement in-degree on every successor, and for any
	// that drop to zero, add it to the frontier. in-degree
	// decrements are racy: two predecessors of the same successor
	// can finish concurrently, so we serialise the in-degree map
	// touches under frontierMu (the same lock that protects the
	// frontier slice and activeCount — one lock for one piece of
	// related bookkeeping).
	nodeDone := func(n *dagNode) {
		frontierMu.Lock()
		for _, succ := range successors[n] {
			inDegree[succ]--
			if inDegree[succ] == 0 {
				frontier = append(frontier, succ)
			}
		}
		activeCount--
		if activeCount == 0 {
			frontierCond.Broadcast()
		}
		frontierCond.Broadcast()
		frontierMu.Unlock()
	}

	// launchNode is the per-node goroutine. It blocks on every
	// predecessor's doneCh (with stopCh as a side-channel for
	// cancellation), then decides to either dispatch (if not aborted
	// and not cancelled) or record a skip. It MUST be called only
	// when the node is on the frontier — its predecessor waits are
	// the only correctness gate.
	launchNode := func(n *dagNode) {
		go func() {
			defer func() {
				// Release dependents: close doneCh so any
				// successor blocked on it can proceed.
				close(s.doneCh[n])
				// Bookkeeping: signal successors, decrement
				// active count, and broadcast in case we
				// are the last node.
				nodeDone(n)
			}()

			// Wait for every predecessor to finish, OR for
			// the batch to be cancelled. We do NOT acquire
			// a slot here — slot acquisition lives in the
			// dispatch closure, and it runs only after this
			// wait returns successfully.
			for _, d := range n.deps {
				dep := s.parsed.named[d]
				select {
				case <-s.doneCh[dep]:
				case <-s.stopCh:
					signalAborted("cancelled")
					s.recordSkip(n, "cancelled before dispatch")
					return
				}
			}

			// All predecessors done. Decide whether to
			// actually run the dispatch.
			//
			// Failure propagation is per-EDGE, never graph-global: a
			// node is skipped only when one of its own predecessors
			// failed or was itself skipped. A node with no
			// dependencies — including every non-task parallel call
			// in the batch — must still run when an unrelated node
			// fails. A graph-global abort flag would make that a race
			// between goroutines rather than a property of the graph.
			if reason, blocked := s.predecessorBlocked(n); blocked {
				s.recordSkip(n, reason)
				return
			}

			// Cancellation, by contrast, IS batch-global: the whole
			// turn is going away.
			s.mu.Lock()
			cancelled := s.aborted
			cancelReason := s.abortReason
			s.mu.Unlock()
			if cancelled {
				s.recordSkip(n, cancelReason)
				return
			}
			if s.isCancelled() {
				s.recordSkip(n, "cancelled before dispatch")
				return
			}

			// Build the predecessor context block. Order
			// is the order the predecessor ids appear in
			// n.deps so the child sees a stable order.
			predecessorCtx := s.buildPredecessorContext(n)

			// Build the binding for this node. Batch-order
			// agent ids (a1, a2, …) are stable regardless
			// of execution order; groupAgentIDs is indexed
			// by position in parallelTCs, which matches
			// n.idx here.
			var binding *taskBinding
			if groupBus != nil && n.idx < len(groupAgentIDs) && groupAgentIDs[n.idx] != "" {
				binding = &taskBinding{bus: groupBus, agentID: groupAgentIDs[n.idx], tracker: groupTracker}
			}

			result, images, err := s.dispatch(n.toolCall, binding, n.toolCall.ID, predecessorCtx)

			// Store the RAW result and the error; the error-text
			// rendering, the notice extraction, and the truncation all
			// happen once in buildResults via shapeToolResult, so this
			// path produces the same Message shape as the legacy fan-out.
			s.perNode[n.idx] = &dagNodeResult{
				idx:    n.idx,
				result: result,
				images: images,
				err:    err,
			}

			if err != nil {
				// Record the failure for this node's own dependents
				// to observe. We deliberately do NOT signal a global
				// abort: unrelated nodes are none of this failure's
				// business. Dependents learn about it through
				// predecessorBlocked when their wait returns.
				s.mu.Lock()
				s.failed[n] = true
				s.mu.Unlock()
			}
		}()
	}

	// Main scheduler loop. We pop ready nodes off the frontier
	// slice and launch them. As nodeDone callbacks fire, new
	// successors are appended to the same slice; we keep
	// popping until the slice is empty AND every in-flight
	// goroutine has finished. The cond-var wait blocks the
	// scheduler when the slice is empty but in-flight
	// goroutines are still running (because every successor
	// they release will wake us with a Broadcast).
	//
	// activeCount is the total number of node goroutines that
	// have been launched and not yet finished. It is bumped
	// just before each launch (and pre-seeded to len(frontier)
	// for the initial wave) and decremented in nodeDone. The
	// scheduler breaks out of its main loop only when both the
	// frontier is empty AND activeCount is zero — meaning
	// every node has reached a terminal state.

	for {
		frontierMu.Lock()
		// Pull all ready nodes off the frontier in one
		// critical section. Two predecessors completing
		// simultaneously can both append to the frontier
		// before the scheduler grabs the lock, so a single
		// pull is the atomicity we need to avoid launching
		// the same successor twice.
		var toLaunch []*dagNode
		if len(frontier) > 0 {
			toLaunch = append(toLaunch, frontier...)
			frontier = frontier[:0]
		}
		// If the slice is empty and no goroutines are
		// running, the graph is fully drained.
		if len(toLaunch) == 0 && activeCount == 0 {
			frontierMu.Unlock()
			break
		}
		frontierMu.Unlock()
		// Launch outside the lock — launchNode's goroutine
		// will acquire the lock only when it has results to
		// report. activeCount must be incremented BEFORE the
		// goroutine starts, so a fast-completing goroutine
		// cannot decrement below zero.
		if len(toLaunch) > 0 {
			frontierMu.Lock()
			activeCount += len(toLaunch)
			frontierMu.Unlock()
		}
		for _, n := range toLaunch {
			launchNode(n)
		}
		// If we did not launch anything this round, wait
		// for an in-flight node to release a successor (or
		// itself to finish). activeCount > 0 here because
		// we just checked len(toLaunch) == 0.
		if len(toLaunch) == 0 {
			frontierMu.Lock()
			for activeCount > 0 && len(frontier) == 0 {
				frontierCond.Wait()
			}
			frontierMu.Unlock()
		}
	}
}

// firstFailedPredecessor walks n's predecessors and returns the id of
// the first one that actually failed (vs. was itself skipped for
// cancellation). The scheduler uses this to phrase the skip message
// for transitive nodes: a dependent two hops down names the original
// failing node, not the intermediate skipper.
func (s *dagScheduler) firstFailedPredecessor(n *dagNode) (string, bool) {
	for _, d := range n.deps {
		dep := s.parsed.named[d]
		s.mu.Lock()
		failed := s.failed[dep]
		s.mu.Unlock()
		if failed {
			return d, true
		}
	}
	return "", false
}

// predecessorBlocked reports whether node n must be skipped because one of
// its OWN direct predecessors failed or was itself skipped, and returns the
// reason to record.
//
// This is what scopes failure to the failed node's transitive successors: a
// node with no dependencies can never be blocked, and a node two hops down
// inherits the skip message verbatim, so it still names the ORIGINAL failing
// predecessor rather than the intermediate skipper.
//
// It is safe to read perNode without a lock here: the caller has already
// received every predecessor's doneCh close, and a predecessor writes its
// perNode entry before its deferred close runs — the channel close is the
// happens-before edge.
func (s *dagScheduler) predecessorBlocked(n *dagNode) (string, bool) {
	if d, ok := s.firstFailedPredecessor(n); ok {
		return fmt.Sprintf("dependency %q failed", d), true
	}
	for _, d := range n.deps {
		dep := s.parsed.named[d]
		if r := s.perNode[dep.idx]; r != nil && r.skipped {
			return r.skipMsg, true
		}
	}
	return "", false
}

// recordSkip writes a skip result for a node that did not launch. The
// reason is preserved verbatim so transitive dependents can be told the
// original failing predecessor (not an intermediate skipper).
func (s *dagScheduler) recordSkip(n *dagNode, reason string) {
	s.perNode[n.idx] = &dagNodeResult{
		idx:     n.idx,
		skipped: true,
		skipMsg: reason,
		result:  fmt.Sprintf("skipped: %s", reason),
	}
}

// buildPredecessorContext composes the labelled predecessor-output block
// that gets prepended to the child's `context`. Order is the order the
// predecessor ids appear in the node's `depends_on`, so the child sees a
// stable, predictable order regardless of completion timing.
//
// The output of each predecessor is bounded by `TruncateToolResult` (using
// the predecessor's tool call id so the file-on-disk cache is shared with
// the same call's parent-visible result) and labelled with the
// predecessor's id. If a predecessor produced an error or was itself
// skipped, we still surface a labelled block — saying so is more useful to
// the child than silently dropping the edge.
func (s *dagScheduler) buildPredecessorContext(n *dagNode) string {
	if len(n.deps) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Predecessor task results:\n")
	for _, d := range n.deps {
		dep := s.parsed.named[d]
		b.WriteString("- [")
		b.WriteString(d)
		b.WriteString("] ")
		r := s.perNode[dep.idx]
		if r == nil {
			b.WriteString("(no result recorded)\n")
			continue
		}
		if r.skipped {
			b.WriteString("skipped: ")
			b.WriteString(r.skipMsg)
			b.WriteString("\n")
			continue
		}
		if r.err != nil {
			b.WriteString("error: ")
			b.WriteString(r.err.Error())
			b.WriteString("\n")
			continue
		}
		// Bounded by the existing per-tool-call truncation helper.
		// Reusing dep.toolCall.ID keys the on-disk cache to the
		// predecessor's *original* tool call id, so the parent
		// and the dependent see the same off-loaded chunk.
		b.WriteString(TruncateToolResult(dep.toolCall.ID, r.result))
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// buildResults converts the scheduler's per-node outcomes into a
// `[]Message` ordered by the caller's `parallelIdx` — `msgs[k]` is the
// result of the call at `parallelIdx[k]`. The caller (Agent.Step)
// can then write `results[i] = msgs[k]` for the matching i.
//
// `s.perNode` and `s.parsed.nodes` are both keyed by the ORIGINAL
// call index (`n.idx`), so we look up the per-node result by the
// `parallelIdx[k]` value, not by `k`.
func (s *dagScheduler) buildResults(parallelIdx []int) []Message {
	msgs := make([]Message, len(parallelIdx))
	for k, i := range parallelIdx {
		var r *dagNodeResult
		if i < len(s.perNode) {
			r = s.perNode[i]
		}
		if r == nil {
			msgs[k] = Message{
				Role:    "tool",
				ToolID:  "",
				Content: "skipped: no result recorded",
			}
			continue
		}
		tc := s.parsed.nodes[i].toolCall
		content, display, notice := shapeToolResult(tc.Function.Name, tc.ID, r.result, r.err)
		msgs[k] = Message{
			Role:           "tool",
			ToolID:         tc.ID,
			Content:        content,
			Images:         r.images,
			Notice:         notice,
			DisplayContent: display,
		}
	}
	return msgs
}

// dagContextFrom is the helper the dispatch closure uses to augment the
// tool-call arguments with the predecessor-output block. It re-marshals
// the original arguments with the predecessor block prepended to the
// `context` field, separated by a blank line so the resulting system
// message remains readable.
//
// The augmented params is re-marshalled and passed back to TaskTool.Execute
// as a json.RawMessage, so the public Execute signature is unchanged.
// (The DAG path re-builds the args exactly the way the parent LLM would
// have; the dispatch closure then calls Execute normally.)
func dagContextFrom(originalArgs json.RawMessage, predecessorContext string) (json.RawMessage, error) {
	if predecessorContext == "" {
		return originalArgs, nil
	}
	var p taskToolParams
	if err := json.Unmarshal(originalArgs, &p); err != nil {
		return nil, err
	}
	if p.Context != "" {
		p.Context = p.Context + "\n\n" + predecessorContext
	} else {
		p.Context = predecessorContext
	}
	return json.Marshal(p)
}

// errDAGValidationPrefix is the prefix the result-merge step uses when
// a batch was rejected at validation time (unknown id, cycle, etc.).
// The caller writes this as a tool-result Content on every parallel
// index in the batch and skips launching the scheduler.
const errDAGValidationPrefix = "task DAG validation failed: "

// runDAGFromValidated is the public entry point Agent.Step uses after
// `dagHasDependencies` returns true. It is split out so the caller does
// not have to know about the scheduler type.
//
//   - parallelCalls: the sub-slice of `resp.ToolCalls` indexed by
//     parallelTCs, in order.
//   - groupBus / groupAgentIDs / groupTracker: the same values the
//     legacy fan-out receives; nil/empty if no group.
//   - dispatch: the closure that runs a single node, building the
//     same shape handleToolCallWithImages would.
//
// Returns the per-node Message slice aligned with `parallelCalls` — msgs[k]
// is the result of parallelCalls[k], i.e. of the call at parallelTCs[k] in
// the caller's resp.ToolCalls. The caller must therefore assign
// `results[parallelTCs[k]] = msgs[k]`, NOT `msgs[parallelTCs[k]]`.
// (`buildResults` is the authority on this contract; TestBuildResultsAlignment
// pins it.) A non-nil error means validation failed; the caller reports it on
// the subagent-dispatch positions and dispatches the rest normally.
func runDAGFromValidated(
	parallelCalls []ToolCall,
	stopCh <-chan struct{},
	isCancelled func() bool,
	groupBus *notebus.Bus,
	groupAgentIDs []string,
	groupTracker *groupTracker,
	dispatch dagDispatchFn,
) ([]Message, error) {
	parsed, err := buildDAG(parallelCalls)
	if err != nil {
		return nil, fmt.Errorf("%s%s", errDAGValidationPrefix, err.Error())
	}
	if !parsed.hasDependencies {
		// Defensive: the caller is expected to gate on
		// `dagHasDependencies` already. An all-anonymous batch
		// returning here signals a logic error in the gate; we
		// report it as a validation failure rather than silently
		// dropping the work.
		return nil, fmt.Errorf("%sbatch passed the gate but contains no declared dependencies", errDAGValidationPrefix)
	}
	sched := newDAGScheduler(parsed, stopCh, isCancelled, dispatch)
	sched.run(groupBus, groupAgentIDs, groupTracker)
	// parallelIdx is the position-to-result mapping: every entry is
	// just `i` because the caller passed the parallel slice directly
	// (no reordering). The function is parameterised so the test
	// harness can exercise the same code path with a custom order.
	parallelIdx := make([]int, len(parallelCalls))
	for i := range parallelCalls {
		parallelIdx[i] = i
	}
	return sched.buildResults(parallelIdx), nil
}
