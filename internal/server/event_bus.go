package server

import (
	"log"
	"sync"
)

// Envelope is the wire format for every event on the unified bus. All
// emitters publish tagged at source: Event is the event type, Project is the
// project root the event concerns (empty for process-global events like
// logs), SessionID is the session the event belongs to (empty for
// project/process-scoped events), Seq is a single global monotonic counter
// per server process (gap detection only — never replay), and Data is the
// payload.
type Envelope struct {
	Event     string `json:"event"`
	Project   string `json:"project,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	Seq       uint64 `json:"seq"`
	Data      any    `json:"data"`
}

// busBufferSize is the per-subscriber queue. Subscribers that fall behind
// drop events (with a log) rather than stalling the publisher — the same
// non-blocking discipline as broadcastEvent in handler.go.
const busBufferSize = 256

// sessionScopedEvents are the event types that must carry a session id.
// Publishing one without a session id is a loud error (never a silent
// ambiguity about which session the event belongs to). Part 03 adds the
// bootstrap and turn-lifecycle events: session_bootstrap (stages tools/mcp/
// model + terminal ready and the MCP-timeout warning), turn_started,
// turn_heartbeat, turn_done, turn_error.
var sessionScopedEvents = map[string]bool{
	"session_started":     true,
	"session_bootstrap":   true,
	"user_message":        true,
	"thinking":            true,
	"text":                true,
	"tool_start":          true,
	"tool_result":         true,
	"turn_started":        true,
	"turn_heartbeat":      true,
	"turn_done":           true,
	"turn_error":          true,
	"messages":            true,
	"question":            true,
	"question_resolved":   true,
	"permission":          true,
	"permission_resolved": true,
	"error":               true,
	"runs":                true,
}

// EventBus is the single server-side broadcaster. Every published event is
// stamped with a monotonic seq and fanned out to all subscribers
// non-blockingly. Subscribers declare the projects they view; ViewedProjects
// drives the subscriber-aware git/spending emitters.
type EventBus struct {
	mu          sync.Mutex
	subs        map[chan Envelope]struct{}
	subProjects map[chan Envelope][]string
	seq         uint64
}

// NewEventBus returns an empty bus.
func NewEventBus() *EventBus {
	return &EventBus{
		subs:        make(map[chan Envelope]struct{}),
		subProjects: make(map[chan Envelope][]string),
	}
}

// Subscribe registers a subscriber that declares interest in the given
// projects (for git/spending emission scope) and returns its channel. The
// caller must Unsubscribe when done.
func (b *EventBus) Subscribe(projects []string) chan Envelope {
	ch := make(chan Envelope, busBufferSize)
	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.subProjects[ch] = projects
	b.mu.Unlock()
	return ch
}

// SubscriberCount returns how many subscribers are currently registered.
// Emitters that should only work while a client is connected (runs, git,
// spending) gate their loops on this being non-zero.
func (b *EventBus) SubscriberCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.subs)
}

// Unsubscribe removes a previously registered subscriber.
func (b *EventBus) Unsubscribe(ch chan Envelope) {
	b.mu.Lock()
	delete(b.subs, ch)
	delete(b.subProjects, ch)
	b.mu.Unlock()
}

// ViewedProjects returns the distinct project roots that at least one
// subscriber is currently viewing. Emitters for per-project data (git status)
// restrict themselves to this set so work is scoped to what clients see.
func (b *EventBus) ViewedProjects() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	counts := make(map[string]int)
	for _, projects := range b.subProjects {
		for _, p := range projects {
			counts[p]++
		}
	}
	out := make([]string, 0, len(counts))
	for p := range counts {
		out = append(out, p)
	}
	return out
}

// Publish stamps the next monotonic seq onto an envelope and fans it out to
// every subscriber. Sends are non-blocking: a slow subscriber drops the event
// (with a log line) rather than stalling the publisher. A session-scoped
// event published without a session id is logged as an error — the event is
// still delivered, but the publisher is told loudly.
func (b *EventBus) Publish(event, project, sessionID string, data any) {
	if sessionScopedEvents[event] && sessionID == "" {
		log.Printf("event bus: ERROR publishing %q without a session id (project %q)", event, project)
	}
	b.mu.Lock()
	b.seq++
	env := Envelope{Event: event, Project: project, SessionID: sessionID, Seq: b.seq, Data: data}
	for ch := range b.subs {
		select {
		case ch <- env:
		default:
			log.Printf("event bus: dropping %q event for slow subscriber (seq %d)", event, b.seq)
		}
	}
	b.mu.Unlock()
}

// LastSeq returns the most recently stamped global sequence. It is used to
// record the per-session reconcile watermark (GET /api/sessions/:id/state's
// last_seq) after a session-scoped publish. Reading it slightly after the
// publish is fine — the value is only ever compared for staleness.
func (b *EventBus) LastSeq() uint64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.seq
}
