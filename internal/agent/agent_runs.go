package agent

import (
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jamesmercstudio/ocode/internal/tool"
)

// RunStatus is the lifecycle state of an async subagent run.
type RunStatus string

const (
	RunRunning RunStatus = "running"
	RunDone    RunStatus = "done"
	RunFailed  RunStatus = "failed"
)

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

	mu         sync.Mutex
	transcript []Message
	done       chan struct{} // closed exactly once when the run reaches a terminal state
	doneOnce   sync.Once
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

func (r *AgentRun) statusValue() RunStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.Status
}

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

// tryFinishCancelled marks the run as Failed only if it is still Running.
// Used by CancelAll so the TUI reflects the cancelled state immediately,
// without racing with the goroutine that may call finishOK later.
func (r *AgentRun) tryFinishCancelled() {
	r.mu.Lock()
	cancelled := false
	if r.Status == RunRunning {
		r.Status = RunFailed
		r.Err = "cancelled"
		r.EndedAt = time.Now()
		cancelled = true
	}
	r.mu.Unlock()
	if cancelled {
		r.closeDone()
	}
}

// AgentRunRegistry holds the main agent's async subagent runs.
type AgentRunRegistry struct {
	mu      sync.Mutex
	runs    map[string]*AgentRun
	order   []string
	counter int
	onDone  func(*AgentRun)
}

func NewAgentRunRegistry() *AgentRunRegistry {
	return &AgentRunRegistry{
		runs: make(map[string]*AgentRun),
	}
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

func (r *AgentRunRegistry) Snapshot() []*AgentRun {
	r.mu.Lock()
	defer r.mu.Unlock()
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
		if run.statusValue() != RunRunning {
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
