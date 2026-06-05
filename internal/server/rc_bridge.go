package server

import (
	"sync"

	"github.com/jamesmercstudio/ocode/internal/agent"
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
