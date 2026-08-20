package server

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/u007/ocode/internal/agent"
)

// Shutdown gracefully tears down everything the handler owns within ctx:
//   - all resident agent sessions (cancels turns, drains maintenance workers)
//   - every running terminal pty (SIGTERM -> wait -> SIGKILL of the process
//     group, so a foreground command gets a chance to flush before exit)
//
// It is bounded by ctx: callers pass a TTL context so a quit never hangs. The
// terminal path is the headline case — interactive shells and their children
// must not be killed with a bare SIGKILL on desktop exit.
func (h *Handler) Shutdown(ctx context.Context) {
	h.shutdownAgentSessions(ctx)
	h.shutdownTerminals(ctx)
}

// shutdownAgentSessions shuts down every resident agent session. Each shutdown
// runs in its own goroutine so one slow session can't stall the others, and the
// whole pass returns when all finish or ctx expires.
func (h *Handler) shutdownAgentSessions(ctx context.Context) {
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
