package cdp

import (
	"encoding/json"
	"sync"
)

// subBufSize is the buffered size of each event-subscription channel. When
// full, the OLDEST event is dropped (never block the reader goroutine).
const subBufSize = 64

// EventSub is one live subscription: a buffered channel of raw event params,
// plus a drop counter. Cancel (returned by Subscribe) or Conn.Close closes it.
type EventSub struct {
	mu      sync.Mutex
	ch      chan json.RawMessage
	dropped int64
	closed  bool
}

// C exposes the event channel.
func (s *EventSub) C() <-chan json.RawMessage { return s.ch }

// Dropped reports how many events were dropped oldest-first because the
// channel was full (i.e. the consumer fell behind).
func (s *EventSub) Dropped() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dropped
}

// push delivers one event, dropping the oldest if the channel is full. It is
// called from the reader goroutine; the mutex serializes it against close() so
// a send never races a channel close (dispatch skips closed subs via s.closed).
func (s *EventSub) push(ev json.RawMessage) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	select {
	case s.ch <- ev:
		s.mu.Unlock()
		return
	default:
	}
	// Buffer full: drop the oldest, then enqueue (flush with capacity restored).
	select {
	case <-s.ch:
		s.dropped++
	default:
	}
	s.ch <- ev
	s.mu.Unlock()
}

// close is idempotent: closing an already-closed (or an EventSub never added)
// channel is a no-op.
func (s *EventSub) close() {
	s.mu.Lock()
	if !s.closed {
		s.closed = true
		close(s.ch)
	}
	s.mu.Unlock()
}

// subKey addresses the subscription registry: "" sessionID selects
// browser-level (sessionId-less) events.
type subKey struct {
	sessionID string
	method    string
}

// finished reports whether the connection has shut down (done closed).
func (c *Conn) finished() bool {
	select {
	case <-c.done:
		return true
	default:
		return false
	}
}

// Subscribe delivers raw event params for (sessionID, method) on a buffered
// channel. "" sessionID subscribes to browser-level events. cancel() removes
// the subscription and closes its channel; Conn.Close does the same for all
// remaining subscriptions. Callers must always propagate cancel.
func (c *Conn) Subscribe(sessionID, method string) (<-chan json.RawMessage, func()) {
	s := &EventSub{ch: make(chan json.RawMessage, subBufSize)}
	k := subKey{sessionID, method}
	c.subsMu.Lock()
	// A subscription fabricated after Close/EOF would never be closed (the
	// closeSubscriptions pass has already run) and leak an open channel. Detect
	// that under subsMu (the same lock serializing the close pass) and hand
	// back an already-closed channel with a no-op cancel; it never enters c.subs,
	// so it cannot be closed twice.
	if c.finished() {
		c.subsMu.Unlock()
		close(s.ch)
		var noop sync.Once
		return s.C(), func() { noop.Do(func() {}) }
	}
	c.subs[k] = append(c.subs[k], s)
	c.subsMu.Unlock()

	var once sync.Once
	cancel := func() {
		once.Do(func() {
			c.subsMu.Lock()
			if list := c.subs[k]; len(list) > 0 {
				for i, sb := range list {
					if sb == s {
						c.subs[k] = append(list[:i], list[i+1:]...)
						break
					}
				}
				if len(c.subs[k]) == 0 {
					delete(c.subs, k)
				}
			}
			c.subsMu.Unlock()
			s.close()
		})
	}
	return s.C(), cancel
}

// Dropped sums the drop counter across all subscribers for (sessionID, method).
func (c *Conn) Dropped(sessionID, method string) int64 {
	var n int64
	c.subsMu.RLock()
	for _, s := range c.subs[subKey{sessionID, method}] {
		n += s.Dropped()
	}
	c.subsMu.RUnlock()
	return n
}

// dispatchEvent fans a sessionId+method event out to matching subscribers. It
// copies the subscriber pointers while holding the read lock, then releases it
// before the (non-blocking) sends, so a concurrent cancel mutating the registry
// cannot race the iteration over the backing array.
func (c *Conn) dispatchEvent(m wireMessage) {
	c.subsMu.RLock()
	subs := append([]*EventSub(nil), c.subs[subKey{m.SessionID, m.Method}]...)
	c.subsMu.RUnlock()
	for _, s := range subs {
		s.push(m.Params)
	}
}

// closeSubscriptions closes every remaining subscription channel (used by
// finish on close/EOF).
func (c *Conn) closeSubscriptions() {
	c.subsMu.Lock()
	all := make([]*EventSub, 0, len(c.subs))
	for _, l := range c.subs {
		all = append(all, l...)
	}
	c.subs = make(map[subKey][]*EventSub)
	c.subsMu.Unlock()
	for _, s := range all {
		s.close()
	}
}
