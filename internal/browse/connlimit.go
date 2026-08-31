package browse

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"
)

// Spec § External mode limits (2026-08-30 design doc line ~142): "per-stateKey
// concurrent upstream connection cap 32". v1 shipped without it (see
// docs/architecture/v1-connection-cap-exclusion.md); this is the prescribed
// follow-up semaphore around handleExternal/handleLocal upstream work.
const (
	maxUpstreamConnsPerKey = 32
	// upstreamSlotWait bounds how long an over-limit request queues before
	// failing. The queue wait happens BEFORE the 30s upstream fetch timeout
	// starts (handlers acquire before building the upstream request), so it
	// needs its own deadline; waiters are never parked indefinitely behind
	// a wedged upstream.
	upstreamSlotWait = 5 * time.Second
)

// errUpstreamBusy is returned when a slot could not be acquired within
// upstreamSlotWait. Handlers answer it with 503 + Retry-After.
var errUpstreamBusy = errors.New("browse: too many concurrent upstream connections")

// upstreamSlots is one per-stateKey counting semaphore. refs counts BOTH
// holders and waiters: dropping the map entry while waiters exist would let
// a fresh acquire mint a second semaphore and silently double the cap. The
// entry is deleted the instant refs reaches zero, so abandoned stateKeys
// (closed tabs) leave nothing behind — no TTL sweep needed.
type upstreamSlots struct {
	ch   chan struct{}
	refs int
}

// connLimiter holds the per-stateKey semaphores. External and local traffic
// for one stateKey share a single semaphore — they consume the same upstream
// resource and the spec's "32" is per-stateKey, not per-mode.
type connLimiter struct {
	mu    sync.Mutex
	keys  map[string]*upstreamSlots
	limit int
	wait  time.Duration // overridable in tests
}

func newConnLimiter(limit int) *connLimiter {
	return &connLimiter{
		keys:  make(map[string]*upstreamSlots),
		limit: limit,
		wait:  upstreamSlotWait,
	}
}

// acquire reserves an upstream connection slot for key. It blocks up to the
// configured wait or until ctx is cancelled, whichever comes first. The
// returned release func is safe to call exactly once (guarded internally);
// handlers defer it so the slot is held until ALL upstream work for the
// request — body streaming or a hijacked WS tunnel — is done.
func (l *connLimiter) acquire(ctx context.Context, key string) (release func(), err error) {
	l.mu.Lock()
	s := l.keys[key]
	if s == nil {
		s = &upstreamSlots{ch: make(chan struct{}, l.limit)}
		l.keys[key] = s
	}
	s.refs++
	l.mu.Unlock()

	abandon := func(cause error) (func(), error) {
		l.mu.Lock()
		l.dropRefLocked(key, s)
		l.mu.Unlock()
		return nil, cause
	}

	timer := time.NewTimer(l.wait)
	defer timer.Stop()
	select {
	case s.ch <- struct{}{}:
		return sync.OnceFunc(func() {
			<-s.ch
			l.mu.Lock()
			l.dropRefLocked(key, s)
			l.mu.Unlock()
		}), nil
	case <-ctx.Done():
		return abandon(ctx.Err())
	case <-timer.C:
		return abandon(errUpstreamBusy)
	}
}

// dropRefLocked releases one reference and deletes the entry when the last
// holder/waiter is gone. An entry is only ever deleted at refs==0, at which
// point its channel is necessarily empty (every token in flight holds a ref).
func (l *connLimiter) dropRefLocked(key string, s *upstreamSlots) {
	s.refs--
	if s.refs <= 0 {
		if cur, ok := l.keys[key]; ok && cur == s {
			delete(l.keys, key)
		}
	}
}

// failBusy answers an over-cap request. Like failNav it closes the loading
// nav event for top-level documents only (Part 07: exactly one loading + one
// terminal per nav — acquire runs after the loading emit, so busy must emit
// the terminal), and replies 503 + Retry-After (the queue deadline already
// elapsed; a short Retry-After lets the browser's pending fetch back off
// without hammering).
func (s *Server) failBusy(w http.ResponseWriter, r *http.Request, t target, mode string) {
	if isDocumentRequest(r) {
		s.emitNav(NavEvent{StateKey: t.StateKey, URL: upstreamOrigin(t) + t.Path, Status: http.StatusServiceUnavailable, Mode: mode, Error: "upstream busy"})
	}
	w.Header().Set("Retry-After", "1")
	http.Error(w, "browse: too many concurrent upstream connections", http.StatusServiceUnavailable)
}

// inUse reports the number of live per-key entries. Test hook.
func (l *connLimiter) inUse() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.keys)
}
