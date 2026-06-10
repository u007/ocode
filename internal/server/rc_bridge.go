package server

import (
	"sync"

	"github.com/u007/ocode/internal/agent"
)

// RCRequest is a message from the web UI that should be forwarded to the TUI's agent.
// The TUI's Update loop picks this up, runs it through its agent, and sends the result back.
type RCRequest struct {
	Content  string
	StreamCh chan<- SSEEvent // for live SSE deltas (nil for non-streaming)
	ResultCh chan<- RCResult // final response
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
