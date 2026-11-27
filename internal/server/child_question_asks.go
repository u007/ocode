package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/tool"
)

// childQuestionResolvedTTL is how long a RESOLVED parked-question id is
// remembered. It has the same job as childPermResolvedTTL: a stale answer or
// dismissal for an id the browser still holds must 404 WITHOUT falling through
// to findPendingSession, which takes a blocking as.mu that the parked question
// (raised from inside the turn) keeps held.
const childQuestionResolvedTTL = childPermResolvedTTL

// childQuestionOutcome is what a parked question resolves to: the user's answer
// sets, or dismissed=true when the user dismissed it, the wait timed out, the
// parent turn was stopped, or the caller's context ended.
type childQuestionOutcome struct {
	answers   []tool.QuestionAnswerSet
	dismissed bool
}

// childQuestionAsk is one parked question raised through agent.QuestionAsker.
// The blocked caller reads respCh; delivering the outcome into it resumes it.
type childQuestionAsk struct {
	event  QuestionEvent
	respCh chan childQuestionOutcome
}

// deliver hands the outcome to the parked caller. Buffered(1) so the send never
// blocks, even when the caller already gave up on the park.
func (c *childQuestionAsk) deliver(out childQuestionOutcome) {
	select {
	case c.respCh <- out:
	default:
	}
}

// childQuestionAsks is the per-session registry of parked agent questions. It
// mirrors childPermAsks: its OWN mutex, never as.mu. The question is raised
// from inside runTurn, which holds as.mu for the whole turn, so livePendingAsks
// reads this registry while as.mu is held and the resolve endpoints deliver into
// it without touching as.mu.
//
// A nil *childQuestionAsks is a valid, empty registry (every method is nil-safe).
type childQuestionAsks struct {
	mu       sync.Mutex
	m        map[string]*childQuestionAsk
	order    []string
	resolved map[string]time.Time
	// timeout is the park bound. A field, not a package var, so a test can
	// shorten it without a global that parallel tests would race on.
	timeout time.Duration
}

func newChildQuestionAsks() *childQuestionAsks {
	return &childQuestionAsks{
		m:        make(map[string]*childQuestionAsk),
		resolved: make(map[string]time.Time),
		timeout:  childPermAskTimeout,
	}
}

// add registers a parked question under id. False when the registry is nil or
// the id is taken; the caller must then dismiss rather than park.
func (c *childQuestionAsks) add(id string, ask *childQuestionAsk) bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.m[id]; exists {
		return false
	}
	if c.m == nil {
		c.m = make(map[string]*childQuestionAsk)
	}
	c.m[id] = ask
	c.order = append(c.order, id)
	delete(c.resolved, id)
	return true
}

// get returns a parked question without removing it.
func (c *childQuestionAsks) get(id string) *childQuestionAsk {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.m[id]
}

// take removes a parked question and returns it, so exactly one resolver can
// deliver. A second resolve finds nothing and 404s.
func (c *childQuestionAsks) take(id string) *childQuestionAsk {
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
	delete(c.resolved, id)
	return ask
}

// list returns the parked questions as `question` SSE / pending_asks payloads,
// in registration order.
func (c *childQuestionAsks) list() []QuestionEvent {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.m) == 0 {
		return nil
	}
	out := make([]QuestionEvent, 0, len(c.m))
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

// wasResolvedRecently reports whether id was resolved here recently enough that
// a second resolve should 404 rather than fall through to the main-agent path.
func (c *childQuestionAsks) wasResolvedRecently(id string) bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pruneResolvedLocked(time.Now())
	_, ok := c.resolved[id]
	return ok
}

// markResolved records id as resolved now.
func (c *childQuestionAsks) markResolved(id string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.resolved == nil {
		c.resolved = make(map[string]time.Time)
	}
	now := time.Now()
	c.resolved[id] = now
	c.pruneResolvedLocked(now)
}

// denyAll dismisses every parked question and empties the registry, so no
// blocked caller outlives its session (idle eviction, a rebuild that shuts the
// old agent down, or server shutdown).
func (c *childQuestionAsks) denyAll() {
	if c == nil {
		return
	}
	c.mu.Lock()
	if c.resolved == nil {
		c.resolved = make(map[string]time.Time)
	}
	asks := make([]*childQuestionAsk, 0, len(c.m))
	now := time.Now()
	for _, id := range c.order {
		if ask, ok := c.m[id]; ok {
			asks = append(asks, ask)
			c.resolved[id] = now
		}
	}
	c.m = make(map[string]*childQuestionAsk)
	c.order = nil
	c.pruneResolvedLocked(now)
	c.mu.Unlock()

	// Deliver outside the lock, as childPermAsks.denyAll does.
	for _, ask := range asks {
		ask.deliver(childQuestionOutcome{dismissed: true})
	}
}

func (c *childQuestionAsks) dropOrderLocked(id string) {
	for i, cur := range c.order {
		if cur == id {
			c.order = append(c.order[:i], c.order[i+1:]...)
			return
		}
	}
}

// pruneResolvedLocked drops resolved entries older than childQuestionResolvedTTL.
func (c *childQuestionAsks) pruneResolvedLocked(now time.Time) {
	for id, at := range c.resolved {
		if now.Sub(at) > childQuestionResolvedTTL {
			delete(c.resolved, id)
		}
	}
}

// childQuestionSeq backs the id fallback for the (practically impossible) case
// where crypto/rand fails. It only has to be unique in-process.
var childQuestionSeq atomic.Uint64

// newChildQuestionRequestID mints the request_id a parked question is answered
// by. The prefix keeps it distinct from a tool-call id, and the random body
// stops a hand-crafted resolve from aiming at another session's parked question.
func newChildQuestionRequestID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("childquestion-%d", childQuestionSeq.Add(1))
	}
	return "childquestion-" + hex.EncodeToString(b[:])
}

// newServerQuestionAsker builds the agent.QuestionAsker a headless server agent
// installs. It is the question twin of newServerSubAgentAsker:
//
//  1. register the parked question BEFORE the broadcast, so an answer that races
//     the frame's arrival already finds the entry;
//  2. broadcast the same `question` SSE frame the main agent's question tool
//     emits (QuestionEvent, request_id = the parked id);
//  3. block until the answer, ctx.Done(), the parent's current StopCh(), or the
//     park timeout. Every non-answer arm returns dismissed=true and broadcasts
//     question_resolved so the browser dialog closes.
func (h *Handler) newServerQuestionAsker(sessionID string, as *agentSession) agent.QuestionAsker {
	return func(ctx context.Context, prompts []tool.QuestionPrompt) ([]tool.QuestionAnswerSet, bool) {
		reg := as.childQuestions
		id := newChildQuestionRequestID()
		ask := &childQuestionAsk{
			event:  QuestionEvent{RequestID: id, Questions: prompts},
			respCh: make(chan childQuestionOutcome, 1),
		}
		if !reg.add(id, ask) {
			log.Printf("serve: parked question not registrable (session=%s); dismissing", sessionID)
			return nil, true
		}

		h.broadcastEvent(SSEEvent{
			SessionID: sessionID,
			Event:     "question",
			Data:      ask.event,
		})

		// The parent's CURRENT stop channel, read per ask (see
		// newServerSubAgentAsker for why it is not captured once at install).
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
		case out := <-ask.respCh:
			return out.answers, out.dismissed
		case <-ctx.Done():
			out := h.abortParkedQuestion(sessionID, reg, id, ask, "question context cancelled")
			return out.answers, out.dismissed
		case <-parentStop:
			out := h.abortParkedQuestion(sessionID, reg, id, ask, "parent session cancelled")
			return out.answers, out.dismissed
		case <-timer.C:
			out := h.abortParkedQuestion(sessionID, reg, id, ask, "no answer within "+timeout.String())
			return out.answers, out.dismissed
		}
	}
}

// abortParkedQuestion completes a park that left without a user answer. It
// takes the entry itself; if it is already gone, a user answer or denyAll won
// the race. Both resolvers remove the entry and then deliver into respCh, so a
// blocking receive here is guaranteed to return and is the exact outcome.
// Otherwise this arm owns the resolution: it marks the id resolved and tells the
// browser to close the dialog.
func (h *Handler) abortParkedQuestion(sessionID string, reg *childQuestionAsks, id string, ask *childQuestionAsk, reason string) childQuestionOutcome {
	if reg.take(id) == nil {
		return <-ask.respCh
	}
	reg.markResolved(id)
	log.Printf("serve: parked question dismissed: session=%s request=%s (%s)", sessionID, id, reason)
	h.broadcastEvent(SSEEvent{
		SessionID: sessionID,
		Event:     "question_resolved",
		Data:      map[string]string{"request_id": id},
	})
	return childQuestionOutcome{dismissed: true}
}

// parkedQuestionFor locates the parked question for requestID WITHOUT taking
// as.mu, mirroring findChildPermAsk. It scans the session named by sessionID, or
// every live session when sessionID is empty, and returns the owning session id
// and entry. stale=true means no live entry exists but the id was resolved
// recently, so the caller must 404 instead of falling through to the blocking
// main-agent lookup.
func (h *Handler) parkedQuestionFor(sessionID, requestID string) (owner string, as *agentSession, ask *childQuestionAsk, stale bool) {
	type candidate struct {
		id string
		as *agentSession
	}
	h.mu.Lock()
	var candidates []candidate
	if sessionID != "" {
		if cur, ok := h.agents[agentKey(sessionID)]; ok {
			candidates = append(candidates, candidate{sessionID, cur})
		}
	} else {
		for id, cur := range h.agents {
			candidates = append(candidates, candidate{id, cur})
		}
	}
	h.mu.Unlock()

	for _, c := range candidates {
		if got := c.as.childQuestions.get(requestID); got != nil {
			return c.id, c.as, got, false
		}
	}
	for _, c := range candidates {
		if c.as.childQuestions.wasResolvedRecently(requestID) {
			return "", nil, nil, true
		}
	}
	return "", nil, nil, false
}

// questionAnswerSetsFromPayload converts the browser's answer payload into the
// tool.QuestionAnswerSet form the asker returns, preserving every selected
// option (multi-select questions).
func questionAnswerSetsFromPayload(in []questionAnswerPayload) []tool.QuestionAnswerSet {
	answers := make([]tool.QuestionAnswerSet, 0, len(in))
	for _, a := range in {
		set := tool.QuestionAnswerSet{Header: a.Header, Question: a.Question}
		for _, v := range a.Answers {
			set.Answers = append(set.Answers, tool.QuestionAnswer{Label: v.Label, Text: v.Text, Custom: v.Custom})
		}
		answers = append(answers, set)
	}
	return answers
}

// resolveParkedQuestion delivers an answer or dismissal into a parked question
// and broadcasts question_resolved. Like childPermResolve, take() is the single
// removal point, and nothing here takes as.mu.
func (h *Handler) resolveParkedQuestion(w http.ResponseWriter, owner string, as *agentSession, requestID string, out childQuestionOutcome) {
	reg := as.childQuestions
	ask := reg.take(requestID)
	if ask == nil {
		writeError(w, http.StatusNotFound, "no pending question found for request_id")
		return
	}
	reg.markResolved(requestID)
	ask.deliver(out)
	h.broadcastEvent(SSEEvent{
		SessionID: owner,
		Event:     "question_resolved",
		Data:      map[string]string{"request_id": requestID},
	})
	writeJSON(w, http.StatusOK, ChatResponse{})
}
