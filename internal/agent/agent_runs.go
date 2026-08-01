package agent

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/u007/ocode/internal/tool"
)

// ErrAgentQueueCancelled is returned by AgentRunRegistry.Acquire when the
// caller's stop channel closes while a dispatch is still waiting for a
// concurrency slot.
var ErrAgentQueueCancelled = errors.New("agent dispatch cancelled while queued")

// RunStatus is the lifecycle state of an async subagent run.
type RunStatus string

const (
	RunRunning   RunStatus = "running"
	RunDone      RunStatus = "done"
	RunFailed    RunStatus = "failed"
	RunCancelled RunStatus = "cancelled"
	// RunQueued means the run is registered but waiting for a concurrency
	// slot (see AgentRunRegistry.Acquire) before its subagent actually
	// starts stepping.
	RunQueued RunStatus = "queued"
)

// IsActive reports whether a run can still start or is currently executing.
// Queued runs are active and must not be pruned or reported as finished.
func (s RunStatus) IsActive() bool {
	return s == RunRunning || s == RunQueued
}

// IsTerminal reports whether a run has reached a final state.
func (s RunStatus) IsTerminal() bool {
	return s == RunDone || s == RunFailed || s == RunCancelled
}

// transcriptCap bounds the per-run stored message count.
const transcriptCap = 200

// AgentRun is one async subagent execution.
type AgentRun struct {
	ID         string
	Name       string
	Status     RunStatus
	Result     string
	Err        string
	StartedAt  time.Time
	EndedAt    time.Time
	Procs      *tool.ProcessRegistry // the subagent's process registry
	Sub        *Agent                // the subagent (for teardown)
	Cancel     func()                // cancels the subagent's Step loop
	Background bool                  // true if the LLM launched this with run_in_background; false means the parent's task tool call already received the full result synchronously
	ToolCallID string                // the originating task tool_call id (best-effort; empty if unknown)
	Dispatcher string                // identity of the agent that dispatched this run

	mu           sync.Mutex
	transcript   []Message
	done         chan struct{} // closed exactly once when the run reaches a terminal state
	doneOnce     sync.Once
	inputTokens  int64
	outputTokens int64

	// Retry tracking
	RetryCount int       // number of retries attempted
	LastError  string    // last error message if retrying
	RetryingAt time.Time // when the last retry started
}

// AddUsage accumulates input/output token counts reported by the provider.
// Safe to call from any goroutine.
func (r *AgentRun) AddUsage(in, out int64) {
	r.mu.Lock()
	r.inputTokens += in
	r.outputTokens += out
	r.mu.Unlock()
}

// Usage returns the accumulated input/output token counts.
func (r *AgentRun) Usage() (int64, int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.inputTokens, r.outputTokens
}

// closeDone closes the done channel exactly once, so waiters unblock when the
// run reaches a terminal state (done, failed, or cancelled).
func (r *AgentRun) closeDone() {
	r.doneOnce.Do(func() {
		if r.done != nil {
			close(r.done)
		}
	})
}

// Done returns a channel that is closed when the run reaches a terminal state.
func (r *AgentRun) Done() <-chan struct{} { return r.done }

// ModelLabel returns "provider/model" (or just "model" when no provider) for
// the subagent backing this run. Returns "" when Sub is nil or its client has
// not been initialized yet (e.g. a run still being set up).
func (r *AgentRun) ModelLabel() string {
	if r.Sub == nil {
		return ""
	}
	c := r.Sub.Client()
	if c == nil {
		return ""
	}
	p := r.Sub.GetProvider()
	m := c.GetModel()
	if p != "" {
		return p + "/" + m
	}
	return m
}

func (r *AgentRun) statusValue() RunStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.Status
}

// CurrentStatus returns the run status under its mutex. Consumers outside the
// agent package should use this accessor rather than reading Status directly.
func (r *AgentRun) CurrentStatus() RunStatus { return r.statusValue() }

func (r *AgentRun) appendTranscript(m Message) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.transcript = append(r.transcript, m)
	if len(r.transcript) > transcriptCap {
		r.transcript = r.transcript[len(r.transcript)-transcriptCap:]
	}
}

func (r *AgentRun) transcriptCopy() []Message {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Message, len(r.transcript))
	copy(out, r.transcript)
	return out
}

// LastLines returns the last n non-empty text lines across the transcript.
func (r *AgentRun) LastLines(n int) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var lines []string
	for _, m := range r.transcript {
		if m.Content == "" {
			continue
		}
		for _, ln := range strings.Split(m.Content, "\n") {
			if strings.TrimSpace(ln) != "" {
				lines = append(lines, strings.TrimSpace(ln))
			}
		}
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}

func (r *AgentRun) finishOK(result string) {
	r.mu.Lock()
	if r.Status != RunRunning {
		r.mu.Unlock()
		return
	}
	r.Status = RunDone
	r.Result = result
	r.EndedAt = time.Now()
	r.mu.Unlock()
	r.closeDone()
}

func (r *AgentRun) finishErr(err string) {
	r.mu.Lock()
	if r.Status != RunRunning {
		r.mu.Unlock()
		return
	}
	r.Status = RunFailed
	r.Err = err
	r.EndedAt = time.Now()
	r.mu.Unlock()
	r.closeDone()
}

// tryFinishCancelled marks the run as Cancelled only if it is still Running
// or Queued. Used by CancelAll and CancelOwned so the TUI reflects the
// cancelled state immediately, without racing with the goroutine that may
// call finishOK later.
func (r *AgentRun) tryFinishCancelled() {
	r.mu.Lock()
	cancelled := false
	if r.Status == RunRunning || r.Status == RunQueued {
		r.Status = RunCancelled
		r.Err = "cancelled"
		r.EndedAt = time.Now()
		cancelled = true
	}
	r.mu.Unlock()
	if cancelled {
		r.closeDone()
	}
}

// markQueued transitions a freshly-created run into the Queued state while
// it waits for a concurrency slot from AgentRunRegistry.Acquire.
func (r *AgentRun) markQueued() {
	r.mu.Lock()
	if r.Status == RunRunning {
		r.Status = RunQueued
	}
	r.mu.Unlock()
}

// beginExecution transitions a queued run back to Running once it has
// acquired a concurrency slot. It returns false if cancellation won the race
// while the run was queued. Unlimited runs begin in Running and are accepted
// here as well.
func (r *AgentRun) beginExecution() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	switch r.Status {
	case RunQueued:
		r.Status = RunRunning
		return true
	case RunRunning:
		return true
	}
	return false
}

// MarkRetrying records that the run is being retried after an error.
func (r *AgentRun) MarkRetrying(errMsg string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.RetryCount++
	r.LastError = errMsg
	r.RetryingAt = time.Now()
}

// RetryStatus returns the current retry state.
func (r *AgentRun) RetryStatus() (count int, lastError string, retryingAt time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.RetryCount, r.LastError, r.RetryingAt
}

// IsRetrying returns true if the run is currently in a retry state.
func (r *AgentRun) IsRetrying() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.RetryCount > 0 && r.Status == RunRunning
}

// agentConcurrencyLimiter is the shared semaphore behind AgentRunRegistry's
// concurrency gating. It is a separate object (rather than plain fields on
// AgentRunRegistry) so multiple registries — one per Agent instance, main or
// sub — can share a single pool of slots via ShareLimiterFrom. Without this
// indirection, every subagent's fresh AgentRunRegistry would enforce its own
// independent cap, letting the true number of concurrently active agents
// multiply with nesting depth instead of respecting one session-wide limit.
type agentConcurrencyLimiter struct {
	mu      sync.Mutex
	max     int
	holders int
	queued  int
	changed chan struct{}
}

func (l *agentConcurrencyLimiter) setMax(n int) {
	if n < 0 {
		n = 0
	}
	l.mu.Lock()
	l.max = n
	l.broadcastLocked()
	l.mu.Unlock()
}

func (l *agentConcurrencyLimiter) getMax() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.max
}

func (l *agentConcurrencyLimiter) getQueued() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.queued
}

func (l *agentConcurrencyLimiter) acquire(stopCh <-chan struct{}) (func(), error) {
	// Check cancellation before admission, including the unlimited path. This
	// matters during Agent.Shutdown: an old dispatch must not be admitted just
	// because the configured limit is zero.
	if isClosed(stopCh) {
		return nil, ErrAgentQueueCancelled
	}

	l.mu.Lock()
	waiting := false
	for {
		if isClosed(stopCh) {
			if waiting {
				l.queued--
			}
			l.mu.Unlock()
			return nil, ErrAgentQueueCancelled
		}
		if l.max == 0 || l.holders < l.max {
			if waiting {
				l.queued--
			}
			l.holders++
			l.mu.Unlock()
			var once sync.Once
			return func() {
				once.Do(func() {
					l.mu.Lock()
					if l.holders > 0 {
						l.holders--
					}
					l.broadcastLocked()
					l.mu.Unlock()
				})
			}, nil
		}
		if !waiting {
			l.queued++
			waiting = true
		}
		changed := l.changed
		l.mu.Unlock()
		select {
		case <-stopCh:
		case <-changed:
		}
		l.mu.Lock()
	}
}

// broadcastLocked wakes every waiter after a limit or holder change. Closing
// and replacing the notification channel avoids lost wakeups while retaining
// all existing holders; unlike replacing a semaphore channel, live resizing
// cannot strand waiters or lose accounting.
func (l *agentConcurrencyLimiter) broadcastLocked() {
	if l.changed == nil {
		l.changed = make(chan struct{})
		return
	}
	close(l.changed)
	l.changed = make(chan struct{})
}

func isClosed(ch <-chan struct{}) bool {
	if ch == nil {
		return false
	}
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

// AgentRunRegistry holds the main agent's async subagent runs.
type AgentRunRegistry struct {
	mu      sync.Mutex
	runs    map[string]*AgentRun
	order   []string
	counter int
	onDone  func(*AgentRun)

	// limiter caps how many dispatches (across this registry AND any
	// registries it shares the limiter with, see ShareLimiterFrom) may hold
	// a slot at once. Every registry gets its own private limiter by
	// default; TaskTool.Execute reassigns a subagent's limiter to its
	// dispatcher's so the whole call tree respects one session-wide cap.
	limiter *agentConcurrencyLimiter
}

func NewAgentRunRegistry() *AgentRunRegistry {
	return &AgentRunRegistry{
		runs:    make(map[string]*AgentRun),
		limiter: &agentConcurrencyLimiter{},
	}
}

// ShareLimiterFrom makes r draw concurrency slots from the same pool as
// parent, so dispatches through r count against parent's (and everyone
// else's who shares that same limiter) cap instead of getting an
// independent allowance. Must be called before any Acquire on r; typically
// right after a subagent's AgentRunRegistry is constructed and before its
// Step loop starts.
func (r *AgentRunRegistry) ShareLimiterFrom(parent *AgentRunRegistry) {
	if parent == nil || parent.limiter == nil {
		return
	}
	r.limiter = parent.limiter
}

// SetMaxConcurrent sets the concurrency limit for Acquire calls. n<=0 means
// unlimited. Existing holders remain accounted for when the limit changes, and
// blocked waiters are re-evaluated immediately. Since the limiter may be
// shared (see ShareLimiterFrom), this affects every registry sharing it.
func (r *AgentRunRegistry) SetMaxConcurrent(n int) {
	r.limiter.setMax(n)
}

// MaxConcurrent returns the current concurrency limit (0 = unlimited).
func (r *AgentRunRegistry) MaxConcurrent() int {
	return r.limiter.getMax()
}

// QueuedCount returns the number of dispatches currently blocked in Acquire
// waiting for a concurrency slot, across every registry sharing this limiter.
func (r *AgentRunRegistry) QueuedCount() int {
	return r.limiter.getQueued()
}

// Acquire blocks until a concurrency slot is available, then returns a
// release func the caller must invoke exactly once when done. If no limit is
// configured (SetMaxConcurrent(0) or never called), it returns immediately
// with a no-op release. stopCh, if non-nil, aborts the wait with
// ErrAgentQueueCancelled when closed.
func (r *AgentRunRegistry) Acquire(stopCh <-chan struct{}) (func(), error) {
	return r.limiter.acquire(stopCh)
}

func (r *AgentRunRegistry) SetOnDone(fn func(*AgentRun)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.onDone = fn
}

func (r *AgentRunRegistry) New(name string) *AgentRun {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.counter++
	id := "agent-run-" + strconv.Itoa(r.counter)
	run := &AgentRun{
		ID:        id,
		Name:      name,
		Status:    RunRunning,
		StartedAt: time.Now(),
		done:      make(chan struct{}),
	}
	r.runs[id] = run
	r.order = append(r.order, id)
	return run
}

func (r *AgentRunRegistry) Get(id string) (*AgentRun, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	run, ok := r.runs[id]
	return run, ok
}

// PruneCompleted removes completed (done/failed) runs beyond keepMax, keeping
// the most recently finished ones. Running runs are never removed. The number
// of removed runs is returned.
func (r *AgentRunRegistry) PruneCompleted(keepMax int) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.pruneCompletedLocked(keepMax)
}

// pruneCompletedLocked is the lock-holding half of PruneCompleted.
func (r *AgentRunRegistry) pruneCompletedLocked(keepMax int) int {
	type entry struct {
		id  string
		end time.Time
	}
	var completed []entry
	for id, run := range r.runs {
		if run.statusValue().IsTerminal() {
			completed = append(completed, entry{id: id, end: run.EndedAt})
		}
	}
	if len(completed) <= keepMax {
		return 0
	}
	// Sort by end time descending (newest first).
	sort.Slice(completed, func(i, j int) bool {
		return completed[i].end.After(completed[j].end)
	})
	toRemove := completed[keepMax:]
	removed := 0
	for _, e := range toRemove {
		delete(r.runs, e.id)
		removed++
	}
	if removed > 0 {
		newOrder := make([]string, 0, len(r.runs))
		for _, id := range r.order {
			if _, ok := r.runs[id]; ok {
				newOrder = append(newOrder, id)
			}
		}
		r.order = newOrder
	}
	return removed
}

func (r *AgentRunRegistry) Snapshot() []*AgentRun {
	r.mu.Lock()
	defer r.mu.Unlock()
	// Auto-prune: keep at most 30 completed runs so the list doesn't grow
	// unbounded. This is called on every TUI render cycle; the overhead of
	// pruning 50-odd entries is negligible.
	r.pruneCompletedLocked(30)
	out := make([]*AgentRun, 0, len(r.order))
	for _, id := range r.order {
		out = append(out, r.runs[id])
	}
	return out
}

func (r *AgentRunRegistry) RunningCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	count := 0
	for _, run := range r.runs {
		if run.statusValue() == RunRunning {
			count++
		}
	}
	return count
}

// CancelOwned cancels a subagent run if and only if the caller's dispatcher
// identity matches the run's Dispatcher field. Returns an error for unknown
// task IDs or dispatcher mismatches. Cancelling an already-terminated run is
// a reported no-op (no error). On success, invokes the run's Cancel func and
// marks it as RunCancelled.
func (r *AgentRunRegistry) CancelOwned(taskID, dispatcher string) error {
	r.mu.Lock()
	run, ok := r.runs[taskID]
	r.mu.Unlock()
	if !ok {
		return fmt.Errorf("unknown task: %s", taskID)
	}
	if run.Dispatcher != dispatcher {
		return fmt.Errorf("task %s is not owned by %q", taskID, dispatcher)
	}
	if status := run.statusValue(); status != RunRunning && status != RunQueued {
		return nil // already terminal; no-op
	}
	if run.Cancel != nil {
		run.Cancel()
	}
	run.tryFinishCancelled()
	return nil
}

// CancelAll cancels every running subagent and marks it cancelled immediately.
// Shared process teardown is owned by the session supervisor.
func (r *AgentRunRegistry) CancelAll() {
	r.mu.Lock()
	runs := make([]*AgentRun, 0, len(r.runs))
	for _, run := range r.runs {
		runs = append(runs, run)
	}
	r.mu.Unlock()
	for _, run := range runs {
		if !run.statusValue().IsActive() {
			continue
		}
		if run.Cancel != nil {
			run.Cancel()
		}
		run.tryFinishCancelled()
	}
}

// notifyDone calls the onDone callback outside the lock.
func (r *AgentRunRegistry) notifyDone(run *AgentRun) {
	r.mu.Lock()
	fn := r.onDone
	r.mu.Unlock()
	if fn != nil {
		fn(run)
	}
}

// TranscriptPublic returns a copy of the run's transcript.
func (r *AgentRun) TranscriptPublic() []Message { return r.transcriptCopy() }
