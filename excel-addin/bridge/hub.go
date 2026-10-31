package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// maxResultBytes caps what one tool result may put into the model's context.
const maxResultBytes = 256 * 1024

// Event is one server-sent message to a pane.
type Event struct {
	Name string `json:"-"`
	Data any    `json:"-"`
}

// CallEvent asks the pane to run a tool.
type CallEvent struct {
	ID   string          `json:"id"`
	Tool string          `json:"tool"`
	Args json.RawMessage `json:"args"`
}

// Result is what the pane posts back for a call.
type Result struct {
	ID      string `json:"id"`
	OK      bool   `json:"ok"`
	Content string `json:"content,omitempty"`
	Error   string `json:"error,omitempty"`
}

var (
	ErrNoPane    = errors.New("no Excel task pane is connected")
	ErrPaneGone  = errors.New("the Excel task pane disconnected before the tool finished")
	ErrCallLimit = errors.New("too many tool calls in flight for this workbook")
)

// evictedReason is sent to a pane that another task pane for the same workbook displaces.
const evictedReason = "another task pane opened this workbook"

// pane is one connected task pane (one workbook, one page load).
type pane struct {
	workbook string
	// instance identifies one page load of the task pane. A reconnect of the same page load carries
	// the same instance; a different instance is a second pane and evicts this one.
	instance string
	events   chan Event
	// gone is closed when the pane is detached or replaced. Calls select on it, so it must never
	// carry a value.
	gone chan struct{}
	// done is what the stream handler waits on. It receives the eviction reason (only for an
	// eviction) and is then closed, so the handler can tell the pane why its stream ends.
	done chan string
	once sync.Once
}

// finish ends the pane exactly once. A non-empty reason is delivered on done before it closes.
func (p *pane) finish(reason string) {
	p.once.Do(func() {
		close(p.gone)
		if reason != "" {
			p.done <- reason // buffered 1, sent once under once
		}
		close(p.done)
	})
}

// discardQueued drops events that were queued for the pane but not yet written. Their calls are
// failed by failPane, so the pane must not run them after it has been displaced.
func (p *pane) discardQueued() {
	for {
		select {
		case <-p.events:
		default:
			return
		}
	}
}

type pending struct {
	pane *pane
	ch   chan Result
}

// Hub relays tool calls from the MCP side to the connected panes and results back.
// Locking: mu guards panes and calls; it is never held while sending on a channel
// that could block or while waiting for a result.
type Hub struct {
	mu      sync.Mutex
	panes   map[string]*pane
	calls   map[string]*pending
	counter atomic.Uint64
	// MaxInFlight bounds concurrent calls per workbook (a runaway agent must not queue unboundedly).
	MaxInFlight int
}

func NewHub() *Hub {
	return &Hub{panes: map[string]*pane{}, calls: map[string]*pending{}, MaxInFlight: 8}
}

// Attach registers a pane for a workbook. A newer attach for the same workbook replaces the older
// pane. If the instance differs, the older pane is evicted: its stream gets an "evicted" event
// before it closes, and its pane must stop reconnecting. If the instance is the same (a reconnect
// after a network blip) the replacement is silent. Either way the older pane's in-flight calls
// fail with ErrPaneGone. The returned done channel delivers the eviction reason, if any, and then
// closes.
func (h *Hub) Attach(workbook, instance string) (events <-chan Event, done <-chan string, detach func()) {
	p := &pane{workbook: workbook, instance: instance, events: make(chan Event, 32), gone: make(chan struct{}), done: make(chan string, 1)}
	h.mu.Lock()
	old := h.panes[workbook]
	h.panes[workbook] = p
	h.mu.Unlock()
	if old != nil {
		h.failPane(old)
		old.discardQueued()
		reason := ""
		if old.instance != instance {
			reason = evictedReason
		}
		old.finish(reason)
	}
	return p.events, p.done, func() {
		h.mu.Lock()
		if h.panes[workbook] == p {
			delete(h.panes, workbook)
		}
		h.mu.Unlock()
		h.failPane(p)
		p.finish("")
	}
}

// failPane fails every call that was waiting on p.
func (h *Hub) failPane(p *pane) {
	h.mu.Lock()
	var gone []*pending
	for id, c := range h.calls {
		if c.pane == p {
			gone = append(gone, c)
			delete(h.calls, id)
		}
	}
	h.mu.Unlock()
	for _, c := range gone {
		c.ch <- Result{OK: false, Error: ErrPaneGone.Error()} // buffered 1
	}
}

// Workbooks lists connected workbook ids, sorted.
func (h *Hub) Workbooks() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	ids := make([]string, 0, len(h.panes))
	for id := range h.panes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// resolve picks the pane for a call. An empty workbook is allowed only when exactly one pane is connected.
func (h *Hub) resolve(workbook string) (*pane, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if workbook != "" {
		if p, ok := h.panes[workbook]; ok {
			return p, nil
		}
		return nil, fmt.Errorf("%w (workbook %q; connected: %s)", ErrNoPane, workbook, listOrNone(h.panes))
	}
	switch len(h.panes) {
	case 0:
		return nil, ErrNoPane
	case 1:
		for _, p := range h.panes {
			return p, nil
		}
	}
	return nil, fmt.Errorf("several workbooks are connected (%s): pass the workbook argument from the [ocode:excel] context block", listOrNone(h.panes))
}

func listOrNone(m map[string]*pane) string {
	if len(m) == 0 {
		return "none"
	}
	ids := make([]string, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return strings.Join(ids, ", ")
}

func (h *Hub) inFlight(p *pane) int {
	n := 0
	for _, c := range h.calls {
		if c.pane == p {
			n++
		}
	}
	return n
}

// Call runs one tool in the pane and waits for its result, the timeout, or ctx cancellation.
// On timeout or cancellation the pane is told to abandon the call.
func (h *Hub) Call(ctx context.Context, workbook, tool string, args json.RawMessage, timeout time.Duration) (Result, error) {
	p, err := h.resolve(workbook)
	if err != nil {
		return Result{}, err
	}
	id := fmt.Sprintf("c%d", h.counter.Add(1))
	pc := &pending{pane: p, ch: make(chan Result, 1)}
	h.mu.Lock()
	if h.inFlight(p) >= h.MaxInFlight {
		h.mu.Unlock()
		return Result{}, ErrCallLimit
	}
	h.calls[id] = pc
	h.mu.Unlock()

	select {
	case p.events <- Event{Name: "call", Data: CallEvent{ID: id, Tool: tool, Args: args}}:
	case <-p.gone:
		h.drop(id)
		return Result{}, ErrPaneGone
	case <-ctx.Done():
		h.drop(id)
		return Result{}, ctx.Err()
	}

	t := time.NewTimer(timeout)
	defer t.Stop()
	select {
	case r := <-pc.ch:
		r.ID = id
		return clamp(r), nil
	case <-t.C:
		h.abandon(p, id)
		return Result{}, fmt.Errorf("the Excel task pane did not finish %s within %s", tool, timeout)
	case <-ctx.Done():
		h.abandon(p, id)
		return Result{}, ctx.Err()
	}
}

func (h *Hub) drop(id string) {
	h.mu.Lock()
	delete(h.calls, id)
	h.mu.Unlock()
}

// abandon forgets the call and asks the pane to cancel it (best effort, never blocks).
func (h *Hub) abandon(p *pane, id string) {
	h.drop(id)
	select {
	case p.events <- Event{Name: "cancel", Data: map[string]string{"id": id}}:
	default:
	}
}

// Deliver routes a pane's result to the waiting call. It returns false for an unknown or already
// finished id (late result after timeout), which is not an error for the pane.
func (h *Hub) Deliver(workbook string, r Result) bool {
	h.mu.Lock()
	c, ok := h.calls[r.ID]
	if ok && c.pane.workbook != workbook {
		h.mu.Unlock()
		return false // another workbook's pane cannot answer this call
	}
	if ok {
		delete(h.calls, r.ID)
	}
	h.mu.Unlock()
	if !ok {
		return false
	}
	c.ch <- r // buffered 1, single sender (the delete above makes this exclusive)
	return true
}

func clamp(r Result) Result {
	if len(r.Content) > maxResultBytes {
		r.Content = r.Content[:maxResultBytes] + "\n[bridge: result truncated at 256 KiB; narrow the range or page with offset_rows]"
	}
	return r
}
