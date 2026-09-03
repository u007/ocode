package server

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/session"
)

// Shutdown gracefully tears down everything the handler owns within ctx:
//   - all resident agent sessions (cancels turns, drains maintenance workers)
//   - every running terminal pty (SIGTERM -> wait -> SIGKILL of the process
//     group, so a foreground command gets a chance to flush before exit)
//   - queued live session writes (session history appended mid-turn)
//
// It is bounded by ctx: callers pass a TTL context so a quit never hangs. The
// terminal path is the headline case — interactive shells and their children
// must not be killed with a bare SIGKILL on desktop exit.
func (h *Handler) Shutdown(ctx context.Context) {
	h.shutdownAgentSessions(ctx)
	h.shutdownTerminals(ctx)
	h.flushLiveSessionWrites(ctx)
}

// flushLiveSessionWrites drains session workers' queued snapshots so a quit
// immediately after a turn never drops the turn's streamed transcript.
// Agent shutdown above may have consumed most of ctx, so the drain gets a
// capped slice of whatever budget remains (skipped entirely when none does);
// FlushAll reserves its barriers against worker retirement, so this cannot
// strand on a dead worker, only time out.
func (h *Handler) flushLiveSessionWrites(ctx context.Context) {
	const maxFlushBudget = 5 * time.Second
	budget := maxFlushBudget
	if dl, ok := ctx.Deadline(); ok {
		if rem := time.Until(dl); rem <= 0 {
			return
		} else if rem < budget {
			budget = rem
		}
	}
	if err := session.FlushAll(budget); err != nil {
		log.Printf("server: live session flush: %v", err)
	}
}

// shutdownAgentSessions shuts down every resident agent session plus every
// dispatched turn job — including jobs still inside bootstrapEntryAgent that
// own no resident agent yet. Each shutdown runs in its own goroutine so one
// slow session can't stall the others, and the whole pass returns when all
// finish or ctx expires.
func (h *Handler) shutdownAgentSessions(ctx context.Context) {
	// Close admission first so no new turn can start plugin/model processes,
	// register an agent, or write after the join below begins.
	h.shutdownMu.Lock()
	h.shutdownStarted = true
	h.shutdownMu.Unlock()

	// Interrupt every session with a dispatched-but-unfinished job (this
	// records pendingCancel where a job can observe it and cancels resident
	// agents + background runs), then join all jobs — including bootstrap
	// work that has no agent in h.agents yet — before tearing down agents.
	h.cancelMu.Lock()
	inFlight := make([]string, 0, len(h.turnInFlight))
	for id, n := range h.turnInFlight {
		if n > 0 {
			inFlight = append(inFlight, id)
		}
	}
	h.cancelMu.Unlock()
	for _, id := range inFlight {
		h.interruptSessionWork(id)
	}
	joinDone := make(chan struct{})
	go func() {
		h.turnJobsWG.Wait()
		close(joinDone)
	}()
	select {
	case <-joinDone:
	case <-ctx.Done():
		log.Printf("server: turn job shutdown timed out, proceeding with agent teardown")
	}

	h.mu.Lock()
	sessions := make([]*agent.Agent, 0, len(h.agents))
	for _, as := range h.agents {
		if as != nil && as.agent != nil {
			sessions = append(sessions, as.agent)
		}
	}
	h.mu.Unlock()

	if len(sessions) == 0 {
		return
	}

	var wg sync.WaitGroup
	for _, ag := range sessions {
		wg.Add(1)
		go func(a *agent.Agent) {
			defer wg.Done()
			done := make(chan struct{})
			go func() {
				a.Shutdown()
				close(done)
			}()
			select {
			case <-done:
			case <-ctx.Done():
			}
		}(ag)
	}

	doneCh := make(chan struct{})
	go func() {
		wg.Wait()
		close(doneCh)
	}()
	select {
	case <-doneCh:
	case <-ctx.Done():
		log.Printf("server: agent session shutdown timed out, proceeding with exit")
	}
}

// shutdownTerminals gracefully terminates every open terminal pty. pty.Start
// makes each shell a session leader (pgid == pid), so signaling the negative
// pid reaches the shell and its children. terminateProcessTree sends SIGTERM,
// waits up to grace, then escalates to SIGKILL.
func (h *Handler) shutdownTerminals(ctx context.Context) {
	grace := terminalGraceDuration(ctx)
	for id, entry := range h.terminalProcs.snapshot() {
		if entry.PID <= 0 {
			continue
		}
		log.Printf("server: terminating terminal %s (pid %d, grace %s)", id, entry.PID, grace)
		terminateProcessTree(int(entry.PID), grace)
	}
}

// terminalGraceDuration returns how long to wait for a terminal's process group
// to exit after SIGTERM before escalating to SIGKILL. It is capped at 2s and
// shrinks if the surrounding shutdown TTL is about to expire, so terminals
// never consume the whole budget.
func terminalGraceDuration(ctx context.Context) time.Duration {
	const maxGrace = 2 * time.Second
	if dl, ok := ctx.Deadline(); ok {
		if rem := time.Until(dl); rem > 0 && rem < maxGrace {
			return rem / 2
		}
	}
	return maxGrace
}
