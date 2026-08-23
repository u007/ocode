package server

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	// toolOutputFlushInterval is the longest a buffered chunk waits before
	// being forwarded, so slow trickling output still reaches the UI.
	toolOutputFlushInterval = 100 * time.Millisecond
	// toolOutputFlushBytes forwards a burst promptly instead of waiting out the
	// interval, keeping high-volume output responsive.
	toolOutputFlushBytes = 8 << 10 // 8KiB
	// toolOutputPerCallCap bounds how much live output a single tool call may
	// push onto the shared event bus. The bus buffer is 256 slots shared across
	// every session and panel, so one noisy command must not be able to starve
	// unrelated events. The authoritative result still arrives via tool_result.
	toolOutputPerCallCap = 256 << 10 // 256KiB
)

// toolOutputCapNotice marks a live stream that stopped early at the per-call
// cap, so a truncated stream is never presented as a complete one.
var toolOutputCapNotice = fmt.Sprintf(
	"\n… [live output paused after %dKB — full result follows on completion]\n",
	toolOutputPerCallCap>>10)

// toolOutputCoalescer batches incremental tool output per tool call.
//
// Deliberately goroutine-free: no ticker, no per-call timer, no background
// flusher. Every decision is made inline on the goroutine that produced the
// chunk, with the current time passed in. A per-tool-call goroutine would be a
// leak of exactly the shape being investigated in the desktop memory report,
// and a shared ticker would keep buffers alive past their call.
//
// Callers must invoke finish when a tool call completes, both to flush the
// tail of a short command and to drop the call's state.
type toolOutputCoalescer struct {
	mu    sync.Mutex
	calls map[string]*toolOutputCall
}

type toolOutputCall struct {
	buf       []byte
	sent      int
	lastFlush time.Time
	started   bool
	capped    bool
}

func newToolOutputCoalescer() *toolOutputCoalescer {
	return &toolOutputCoalescer{calls: make(map[string]*toolOutputCall)}
}

// add buffers a chunk and reports the payload to broadcast, if any.
//
// The cap check short-circuits before any buffering or allocation so that a
// command still writing hard after the cap costs a map lookup and a bool test
// per chunk, not a copy.
func (c *toolOutputCoalescer) add(callID, chunk string, now time.Time) (string, bool) {
	if chunk == "" {
		return "", false
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	call := c.calls[callID]
	if call == nil {
		call = &toolOutputCall{lastFlush: now}
		c.calls[callID] = call
	}
	if call.capped {
		return "", false
	}
	if !call.started {
		call.started = true
		call.lastFlush = now
	}

	call.buf = append(call.buf, chunk...)

	// Past the cap: emit whatever is buffered plus a one-time notice, then stop
	// accumulating for this call entirely.
	if call.sent+len(call.buf) >= toolOutputPerCallCap {
		call.capped = true
		payload := string(call.buf) + toolOutputCapNotice
		call.buf = nil
		call.sent += len(payload)
		call.lastFlush = now
		return payload, true
	}

	if len(call.buf) >= toolOutputFlushBytes || now.Sub(call.lastFlush) >= toolOutputFlushInterval {
		payload := string(call.buf)
		call.buf = nil
		call.sent += len(payload)
		call.lastFlush = now
		return payload, true
	}
	return "", false
}

// finish flushes any buffered tail for the call and forgets it. Returns false
// when there is nothing to send (or the call is unknown).
func (c *toolOutputCoalescer) finish(callID string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	call := c.calls[callID]
	if call == nil {
		return "", false
	}
	delete(c.calls, callID)
	if len(call.buf) == 0 {
		return "", false
	}
	return string(call.buf), true
}

// dropSession releases every buffered call belonging to a session.
//
// finish alone is not sufficient: it runs from the tool_result branch of
// OnMessage, and the agent's tool loop returns early when cancellation lands
// mid-batch, skipping OnMessage for results that had already completed. Those
// entries would otherwise be retained for the life of the Handler — which on the
// desktop is the life of the app. Called when a turn ends, successfully or not.
func (c *toolOutputCoalescer) dropSession(sessionID string) {
	prefix := sessionID + "\x00"
	c.mu.Lock()
	defer c.mu.Unlock()
	for key := range c.calls {
		if strings.HasPrefix(key, prefix) {
			delete(c.calls, key)
		}
	}
}

// activeCalls reports how many calls currently hold buffered state. A healthy
// server returns to zero between turns; a climbing value means finish is not
// being called somewhere.
func (c *toolOutputCoalescer) activeCalls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.calls)
}
