package server

import (
	"sync"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/tool"
)

// CronDelivery is a passive notification pushed from a host-side cron
// delivery sink (e.g. the scheduler's outbox drainer) into the live TUI
// session. Unlike RCRequest, it does NOT trigger an agent turn — the TUI
// just appends a system message to the transcript so the user sees the
// scheduled job's result in the same chat where the job is running.
//
// The CronPusher interface (PushCronResult) is satisfied by the Telegram
// bot today; the scheduler's drainer routes the same Delivery to
// RCBridge.CronDeliveryCh, so the TUI is just another sink.
type CronDelivery struct {
	JobID       string
	JobName     string
	Owner       string
	Result      string
	Error       string
	DeliveredAt int64 // unix seconds
}

// RCRequest is a message from the web UI that should be forwarded to the TUI's agent.
// The TUI's Update loop picks this up, runs it through its agent, and sends the result back.
type RCRequest struct {
	Content  string
	StreamCh chan<- SSEEvent // for live SSE deltas (nil for non-streaming)
	ResultCh chan<- RCResult // final response
	// RemoteApproval marks a request driven by an external client (e.g. the
	// Telegram bot) that wants to handle permission asks / question prompts
	// itself rather than via the TUI's local dialog. The TUI branches on this
	// so the web /rc UI (RemoteApproval=false) keeps its existing behavior.
	RemoteApproval bool
}

// RCResolution carries a decision for a permission ask or the answers to a
// question prompt back from an external client (Telegram bot) to the TUI.
type RCResolution struct {
	// RequestID is the tool-call id of the paused permission/question ask.
	RequestID string `json:"request_id"`
	// Decision is "allow", "deny", or "always" (persist an allow rule).
	Decision string `json:"decision"`
	// Answers answers a question prompt; one set per question, preserving
	// multiple selections (see tool.QuestionAnswerSet).
	Answers []tool.QuestionAnswerSet `json:"answers"`
}

// RCResult is the final response from the TUI's agent after processing an RCRequest.
type RCResult struct {
	Messages []agent.Message
	Error    error
}

// RCBridge is set on the Handler when the web server is proxying to a TUI session.
// Instead of having its own agent, the server forwards requests through rcCh
// and the TUI's Update loop processes them.
type RCBridge struct {
	// RcCh is the channel to send RCRequests to the TUI.
	// The TUI reads from this and processes requests through its own agent.
	RcCh chan RCRequest

	// ResolveCh carries permission/question resolutions from external clients
	// (e.g. the Telegram bot) back to the TUI. The TUI reads from this while a
	// remote-controlled turn is paused on an ask. Never closed while the server
	// is alive — an HTTP handler sending to a closed channel would panic.
	ResolveCh chan RCResolution

	// CronDeliveryCh carries passive notifications from a host-side cron
	// sink (e.g. the scheduler's outbox drainer) into the TUI's chat. The
	// TUI appends the delivery as a system message without triggering an
	// agent turn. Buffered (size 8) so a slow TUI does not back-pressure
	// the drainer; older deliveries are dropped if the buffer fills.
	CronDeliveryCh chan CronDelivery

	// Token is the per-instance RC auth token that external clients (Telegram
	// bot, web UI) must present to drive this session. It is the same secret as
	// the server password and the rc.Registry entry token. The permission/
	// question resolution handlers require it so that only an authenticated
	// client can approve, deny, or mint permission decisions.
	Token string

	// SessionID is the TUI session being controlled.
	SessionID string

	// Model is the model being used.
	Model string

	// mu protects Messages and agent from concurrent access.
	mu sync.RWMutex

	// Messages is the current conversation history, maintained by the TUI
	// and updated after each stream completes. The handler reads this
	// to return existing messages to the web UI.
	Messages []agent.Message

	// agent is the TUI's live agent. The handler uses it to apply runtime
	// toggles (e.g. advisor on/off) directly to the agent executing requests.
	agent *agent.Agent

	// subscribers receive live mirror events (deltas, tool activity, snapshots)
	// pushed by the TUI. Each persistent /api/chat/messages connection registers
	// one. Guarded by mu.
	subscribers map[chan SSEEvent]struct{}

	// status holds the latest TUI status snapshot (model, advisor, IDE,
	// session, cwd, context, spending, modified files, LSP servers, etc.).
	// Updated by the TUI whenever a tracked field changes and broadcast as a
	// `status` SSE event. Read by GET /api/tui-status for initial page loads.
	status *tuiStatusStore
}

// PushCronDelivery enqueues a delivery for the TUI to consume. It is
// safe to call from any goroutine (drainer, HTTP handler, etc.). When the
// buffer is full, the oldest pending delivery is dropped — we never
// block a producer because the cron job has already run.
func (b *RCBridge) PushCronDelivery(d CronDelivery) {
	if b == nil || b.CronDeliveryCh == nil {
		return
	}
	select {
	case b.CronDeliveryCh <- d:
	default:
		// Buffer full: drop the oldest by reading one and trying again.
		select {
		case <-b.CronDeliveryCh:
		default:
		}
		select {
		case b.CronDeliveryCh <- d:
		default:
		}
	}
}

// Subscribe registers a new live-event channel and returns it. The caller must
// Unsubscribe when its connection ends.
func (b *RCBridge) Subscribe() chan SSEEvent {
	ch := make(chan SSEEvent, 256)
	b.mu.Lock()
	if b.subscribers == nil {
		b.subscribers = make(map[chan SSEEvent]struct{})
	}
	b.subscribers[ch] = struct{}{}
	b.mu.Unlock()
	return ch
}

// Unsubscribe removes a previously registered channel.
func (b *RCBridge) Unsubscribe(ch chan SSEEvent) {
	b.mu.Lock()
	delete(b.subscribers, ch)
	b.mu.Unlock()
}

// Broadcast delivers a live event to every subscriber. Sends are non-blocking:
// a slow consumer drops the event rather than stalling the TUI. The authoritative
// "messages" snapshot emitted at each turn boundary self-heals any dropped delta.
func (b *RCBridge) Broadcast(ev SSEEvent) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for ch := range b.subscribers {
		select {
		case ch <- ev:
		default:
		}
	}
}

// SetAgent records the TUI's current live agent (called from TUI goroutine
// whenever the active agent is set or switched).
func (b *RCBridge) SetAgent(a *agent.Agent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.agent = a
}

// Agent returns the TUI's current live agent, or nil if none is set.
func (b *RCBridge) Agent() *agent.Agent {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.agent
}

// SendResolution queues a permission/question resolution for the TUI. It is
// non-blocking: if the TUI is not currently listening (or the buffer is full)
// it returns false and the caller should surface a 503. Never blocks, so an
// HTTP handler cannot hang the server.
func (b *RCBridge) SendResolution(res RCResolution) bool {
	if b.ResolveCh == nil {
		return false
	}
	select {
	case b.ResolveCh <- res:
		return true
	default:
		return false
	}
}

// SetMessages updates the bridge's message list (called from TUI goroutine).
func (b *RCBridge) SetMessages(msgs []agent.Message) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.Messages = msgs
}

// GetMessages returns a copy of the current message list (called from handler goroutine).
func (b *RCBridge) GetMessages() []agent.Message {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]agent.Message, len(b.Messages))
	copy(out, b.Messages)
	return out
}

// StatusStore returns the live TUIStatus holder for this bridge. The TUI calls
// .Set(snap, b) on the returned store to push a new snapshot and broadcast a
// `status` SSE event; the REST handler calls .Snapshot() for GET /api/tui-status.
func (b *RCBridge) StatusStore() *tuiStatusStore {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.status == nil {
		b.status = &tuiStatusStore{}
	}
	return b.status
}

// TUIStatus returns the most recent TUI status snapshot. Safe to call from
// any goroutine.
func (b *RCBridge) TUIStatus() TUIStatus {
	if b == nil {
		return TUIStatus{}
	}
	return b.StatusStore().Snapshot()
}
