package server

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/u007/ocode/internal/agent"
)

// childPermAskTimeout bounds how long a sub-agent permission ask may park
// waiting for a human before it auto-denies.
//
// Parent cancellation does NOT reliably reach a sub-agent that is mid-Step (see
// the "Known limits" section of
// docs/superpowers/specs/2026-10-02-subagent-permission-ask-design.md): the
// child snapshots its own stop channel and only consults the parent's for queue
// admission. So the cancel path is not what bounds the exposure — this timeout
// is. Without it a forgotten dialog would hold a turn (and its session lock)
// open indefinitely.
//
// The park is only time spent WAITING ON A HUMAN, which is why it can afford to
// be generous: long enough that someone who steps away still comes back to a
// live dialog, short enough that an abandoned one cannot pin a turn.
const childPermAskTimeout = 10 * time.Minute

// childPermResolvedTTL is how long a RESOLVED child ask id (delivered, auto-denied
// or swept by denyAll) is remembered after the fact. Its only job is to let
// HandleResolvePermission answer 404 for an id the browser is still holding
// WITHOUT falling through to findPendingSession: that helper takes a BLOCKING
// as.mu, and a child ask parks while the parent's turn holds exactly that lock,
// so falling through would pin the HTTP connection behind the parked child.
// Kept short — it only has to outlive a duplicate click or one that raced the
// resolution.
const childPermResolvedTTL = 2 * time.Minute

// childPermAsk is one parked sub-agent permission ask. The child goroutine is
// blocked reading respCh; delivering the user's decision into it is what resumes
// the child's tool call.
type childPermAsk struct {
	req    agent.PermissionRequest
	event  PermissionEvent
	respCh chan agent.PermissionResponse
}

// deliver hands the decision to the parked child. Buffered(1) so the send never
// blocks even when the child has already given up (the timeout/cancel arm won the
// select and returned).
func (c *childPermAsk) deliver(level agent.PermissionLevel) {
	select {
	case c.respCh <- agent.PermissionResponse{Level: level}:
	default:
	}
}

// childPermAsks is the per-session registry of parked sub-agent permission
// asks.
//
// It has its OWN mutex and never takes as.mu. That is the whole reason the
// design works: runTurn holds as.mu for the entire turn, and a synchronous
// sub-agent dispatch is parked INSIDE that turn, so anything the turn must expose
// to the outside world has to live outside as.mu. livePendingAsks reads this
// registry while as.mu is held; HandleResolvePermission delivers into it.
//
// A nil *childPermAsks is a valid, empty registry (every method is nil-safe), so
// a session built directly as a literal — the shape most tests use — simply has
// no sub-agent asks.
type childPermAsks struct {
	mu sync.Mutex
	m  map[string]*childPermAsk
	// order preserves registration order so the dialog queue is deterministic
	// (map iteration order is not).
	order []string
	// resolved remembers recently resolved ids with their resolution time; see
	// childPermResolvedTTL for why a stale resolve must not fall through to the
	// blocking main-agent path.
	resolved map[string]time.Time
	// timeout is the park bound for asks raised through this registry. It is a
	// field rather than a package var so a test can shorten it without a global
	// that parallel tests would race on.
	timeout time.Duration
}

func newChildPermAsks() *childPermAsks {
	return &childPermAsks{
		m:        make(map[string]*childPermAsk),
		resolved: make(map[string]time.Time),
		timeout:  childPermAskTimeout,
	}
}

// add registers an ask under id. Returns false when the registry is nil or the
// id is already taken, in which case the caller must deny rather than park (a
// duplicate id would mean two children racing on one channel).
func (c *childPermAsks) add(id string, ask *childPermAsk) bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.m[id]; exists {
		return false
	}
	if c.m == nil {
		c.m = make(map[string]*childPermAsk)
	}
	c.m[id] = ask
	c.order = append(c.order, id)
	c.forgetLocked(id)
	return true
}

// get returns a parked ask without removing it, for a caller that must run
// guards (and may still fail) before committing to delivery.
func (c *childPermAsks) get(id string) *childPermAsk {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.m[id]
}

// take removes an ask and returns it, so exactly one resolve can deliver. A
// second resolve for the same id finds nothing and 404s — the same shape as a
// second resolve of an already-answered main-agent ask.
func (c *childPermAsks) take(id string) *childPermAsk {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	ask := c.m[id]
	if ask == nil {
		return nil
	}
	delete(c.m, id)
	c.dropOrderLocked(id)
	c.forgetLocked(id)
	return ask
}

// list returns the parked asks as SSE/pending_asks payloads, in registration
// order.
func (c *childPermAsks) list() []PermissionEvent {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.m) == 0 {
		return nil
	}
	out := make([]PermissionEvent, 0, len(c.m))
	for _, id := range c.order {
		if ask, ok := c.m[id]; ok {
			out = append(out, ask.event)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// wasResolvedRecently reports whether id was already resolved here (delivered to
// the child, auto-denied, or swept by denyAll) recently enough that a second
// resolve for it should 404 rather than fall through to the main-agent path.
// See childPermResolvedTTL for why the fall-through is not safe.
func (c *childPermAsks) wasResolvedRecently(id string) bool {
	if c == nil {
		return false
	}
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pruneResolvedLocked(now)
	_, ok := c.resolved[id]
	return ok
}

// markResolved records id as resolved now. Called after a successful delivery
// AND from the auto-deny arms, so a duplicate resolve 404s instead of
// re-delivering into a channel nobody is reading.
func (c *childPermAsks) markResolved(id string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.resolved == nil {
		c.resolved = make(map[string]time.Time)
	}
	c.resolved[id] = time.Now()
	c.pruneResolvedLocked(time.Now())
}

// denyAll resolves every parked ask with a deny and empties the registry, so no
// child goroutine outlives its session (idle eviction, a rebuild that shuts the
// old agent down, or server shutdown).
func (c *childPermAsks) denyAll() {
	if c == nil {
		return
	}
	c.mu.Lock()
	asks := make([]*childPermAsk, 0, len(c.m))
	for _, id := range c.order {
		if ask, ok := c.m[id]; ok {
			asks = append(asks, ask)
			c.resolved[id] = time.Now()
		}
	}
	c.m = make(map[string]*childPermAsk)
	c.order = nil
	c.pruneResolvedLocked(time.Now())
	c.mu.Unlock()

	// Deliver outside the lock: a child resuming on its answer can call back
	// into the registry, and this must never hold mu across that.
	for _, ask := range asks {
		ask.deliver(agent.PermissionDeny)
	}
}

func (c *childPermAsks) dropOrderLocked(id string) {
	for i, cur := range c.order {
		if cur == id {
			c.order = append(c.order[:i], c.order[i+1:]...)
			return
		}
	}
}

func (c *childPermAsks) forgetLocked(id string) {
	delete(c.resolved, id)
}

// pruneResolvedLocked drops entries older than childPermResolvedTTL. Pruned on
// every touch, so the map cannot grow without bound in a long-lived process.
func (c *childPermAsks) pruneResolvedLocked(now time.Time) {
	for id, at := range c.resolved {
		if now.Sub(at) > childPermResolvedTTL {
			delete(c.resolved, id)
		}
	}
}

// childPermSeq backs the request-id fallback for the (practically impossible)
// case where crypto/rand fails. It only has to make ids unique in-process.
var childPermSeq atomic.Uint64

// newChildPermRequestID mints the request_id a sub-agent ask is resolved by.
//
// The prefix marks it as NOT a tool-call id: HandleResolvePermission takes one
// request_id field for both kinds of ask, and a sub-agent's ask is not tied to
// a tool call the server could execute on its behalf (the parked child executes
// it itself once the answer arrives). The random body makes a guess infeasible,
// so a hand-crafted resolve cannot aim at another session's parked ask.
func newChildPermRequestID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("childperm-%d", childPermSeq.Add(1))
	}
	return "childperm-" + hex.EncodeToString(b[:])
}

// newServerSubAgentAsker builds the permission-ask callback a headless server
// agent installs for its sub-agents. It mirrors the TUI's asker
// (internal/tui/model.go) line for line, with the channel replaced by the
// session's registry so the ask is visible to HTTP callers.
//
// The flow, and why each step is where it is:
//
//  1. register {request, respCh} — BEFORE the broadcast, so a resolve that
//     races the frame's arrival already finds the entry.
//  2. emit the `permission` SSE frame. Nothing else emits one for a child: the
//     server never wires Agent.OnSubAgentMessage, and a child ask never reaches
//     the parent's OnMessage mirror (see attachRunTranscript in subagent.go), so
//     this is the only thing that can surface the ask at all.
//  3. block on the answer.
//
// Blocking is the point: the child's goroutine is the one that needs the answer,
// so it delivers it directly with no re-Step protocol that would have to survive
// shutdownTransient.
func (h *Handler) newServerSubAgentAsker(sessionID string, as *agentSession) func(agent.PermissionRequest) agent.PermissionResponse {
	return func(req agent.PermissionRequest) agent.PermissionResponse {
		// Capture the registry once: a nil registry makes add fail, and the
		// timeout read below must not dereference it.
		reg := as.childAsks
		id := newChildPermRequestID()
		// Buffered(1): the delivery must never block. The TUI does the same, and
		// an unbuffered channel deadlocks whenever the answer arrives after this
		// goroutine gave up on the park (timeout/cancel already returned).
		respCh := make(chan agent.PermissionResponse, 1)
		ask := &childPermAsk{
			req:    req,
			event:  newPermissionEvent(id, req),
			respCh: respCh,
		}
		if !reg.add(id, ask) {
			// No registry (or a duplicate id): deny rather than park, so the
			// child is never left waiting on a channel nobody will answer.
			log.Printf("serve: sub-agent permission ask not registrable (session=%s tool=%s); denying", sessionID, req.ToolName)
			return agent.PermissionResponse{Level: agent.PermissionDeny}
		}

		h.broadcastEvent(SSEEvent{
			SessionID: sessionID,
			Event:     "permission",
			Data:      ask.event,
		})

		// The parent's CURRENT stop channel, read per ask: Cancel closes it and
		// ResetCancellation/RearmMaintenance replace it, so capturing it once at
		// install time would watch a channel that is no longer the live one.
		// Watching it covers Stop AND idle eviction AND a rebuild that shuts the
		// old agent down (Agent.Shutdown calls Cancel).
		var parentStop <-chan struct{}
		if as.agent != nil {
			parentStop = as.agent.StopCh()
		}
		timeout := reg.timeout
		if timeout <= 0 {
			timeout = childPermAskTimeout
		}
		timer := time.NewTimer(timeout)
		defer timer.Stop()

		select {
		case resp := <-respCh:
			return resp
		case <-parentStop:
			h.autoDenyChildPerm(sessionID, id, "parent session cancelled")
			return agent.PermissionResponse{Level: agent.PermissionDeny}
		case <-timer.C:
			h.autoDenyChildPerm(sessionID, id, "no answer within "+timeout.String())
			return agent.PermissionResponse{Level: agent.PermissionDeny}
		}
	}
}

// autoDenyChildPerm completes an ask that left the park without a user decision.
// It removes the entry, remembers the id as auto-resolved (so a stale resolve
// 404s instead of falling through to findPendingSession's blocking lock), and
// broadcasts permission_resolved so the browser's dialog closes by itself —
// otherwise it would sit on screen inviting a click that can only fail.
func (h *Handler) autoDenyChildPerm(sessionID, requestID, reason string) {
	if ask := h.childPermAskByID(sessionID, requestID); ask != nil {
		ask.deliver(agent.PermissionDeny)
	}
	log.Printf("serve: sub-agent permission ask auto-denied: session=%s request=%s (%s)", sessionID, requestID, reason)
	h.broadcastEvent(SSEEvent{
		SessionID: sessionID,
		Event:     "permission_resolved",
		Data:      map[string]string{"request_id": requestID},
	})
}

// childPermAskByID removes and returns the parked ask for requestID in this
// session, remembering it as resolved. Used by the auto-deny arms; a user
// decision goes through HandleResolvePermission instead, which runs the
// always-allow guards BEFORE calling take.
func (h *Handler) childPermAskByID(sessionID, requestID string) *childPermAsk {
	as := h.lookupAgentSession(sessionID)
	if as == nil {
		return nil
	}
	ask := as.childAsks.take(requestID)
	if ask != nil {
		as.childAsks.markResolved(requestID)
	}
	return ask
}

// findChildPermAsk locates the session whose registry holds requestID, returning
// the session AND the ask WITHOUT removing it (the resolve path runs guards
// first and may still fail with nothing delivered).
//
// It mirrors findPendingSession's candidate snapshot — h.mu to collect, released
// before any registry lock — but never touches as.mu, because for a child ask
// that lock is held for the entire park.
func (h *Handler) findChildPermAsk(sessionID, requestID string) (*agentSession, *childPermAsk) {
	type candidate struct {
		id string
		as *agentSession
	}

	h.mu.Lock()
	var candidates []candidate
	if sessionID != "" {
		if as, ok := h.agents[sessionID]; ok {
			candidates = append(candidates, candidate{sessionID, as})
		}
	} else {
		for id, as := range h.agents {
			candidates = append(candidates, candidate{id, as})
		}
	}
	h.mu.Unlock()

	for _, c := range candidates {
		if ask := c.as.childAsks.get(requestID); ask != nil {
			return c.as, ask
		}
	}
	return nil, nil
}
