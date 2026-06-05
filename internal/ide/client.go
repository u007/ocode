package ide

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Client maintains a WebSocket connection to the Claude Code VS Code extension
// and forwards editor events to the TUI via the out channel. All diagnostics go
// through the standard log package (which the TUI redirects to its debug panel);
// the client NEVER writes to stdout/stderr, and reaches the UI only via out —
// both required by the alt-screen TUI-safety rules.
type Client struct {
	lock *Lock
	out  chan<- Update

	writeMu sync.Mutex
	conn    *websocket.Conn

	mu      sync.Mutex
	reqID   int
	pending map[int]string // request id -> method label (e.g. "initialize", "tools/call:getOpenEditors")
}

// NewClient builds a client for the given lock. Call Run to start it.
func NewClient(lock *Lock, out chan<- Update) *Client {
	return &Client{lock: lock, out: out, pending: make(map[int]string)}
}

// Run connects and serves until ctx is cancelled, reconnecting with capped
// backoff on connection loss. It blocks; callers run it in a goroutine.
func (c *Client) Run(ctx context.Context) {
	attempt := 0
	for {
		if ctx.Err() != nil {
			return
		}
		err := c.serve(ctx)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			log.Printf("ide: connection lost: %v", err)
		}
		c.emit(ctx, Update{Kind: UpdateDisconnected})

		attempt++
		delay := time.Duration(1<<uint(min(attempt-1, 4))) * time.Second // 1s,2s,4s,8s,16s cap
		if delay > 10*time.Second {
			delay = 10 * time.Second
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
	}
}

// serve dials, performs the handshake-driven read loop, and returns when the
// connection closes or ctx is cancelled. On a clean connect, attempt backoff is
// reset by the caller observing no immediate error.
func (c *Client) serve(ctx context.Context) error {
	u := url.URL{Scheme: "ws", Host: fmt.Sprintf("127.0.0.1:%d", c.lock.Port)}
	header := http.Header{}
	if c.lock.AuthToken != "" {
		header.Set("x-claude-code-ide-authorization", c.lock.AuthToken)
	}

	dialer := *websocket.DefaultDialer
	dialer.HandshakeTimeout = 5 * time.Second
	conn, _, err := dialer.DialContext(ctx, u.String(), header)
	if err != nil {
		return fmt.Errorf("dial %s: %w", u.String(), err)
	}

	c.writeMu.Lock()
	c.conn = conn
	c.writeMu.Unlock()
	defer func() {
		c.writeMu.Lock()
		_ = conn.Close()
		c.conn = nil
		c.writeMu.Unlock()
		c.mu.Lock()
		c.pending = make(map[int]string)
		c.mu.Unlock()
	}()

	// Close the socket promptly when ctx is cancelled so the blocking read returns.
	go func() {
		<-ctx.Done()
		_ = conn.Close()
	}()

	c.emit(ctx, Update{Kind: UpdateConnected})
	if err := c.request("initialize", map[string]any{
		"protocolVersion": MCPProtocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "ocode", "version": "0.0.0"},
	}); err != nil {
		return err
	}

	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return err
		}
		c.handle(ctx, data)
	}
}

// --- JSON-RPC plumbing ------------------------------------------------------

type rpcIn struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type rpcOut struct {
	JSONRPC string `json:"jsonrpc"`
	ID      any    `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

func (m rpcIn) intID() (int, bool) {
	if len(m.ID) == 0 {
		return 0, false
	}
	var n int
	if err := json.Unmarshal(m.ID, &n); err == nil {
		return n, true
	}
	return 0, false
}

func (c *Client) send(v any) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if c.conn == nil {
		return fmt.Errorf("ide: no connection")
	}
	return c.conn.WriteJSON(v)
}

// notify sends a JSON-RPC notification (no id).
func (c *Client) notify(method string, params any) error {
	return c.send(rpcOut{JSONRPC: "2.0", Method: method, Params: params})
}

// request sends a JSON-RPC request and records its method label for response
// correlation. For tools/call the label is "tools/call:<tool>".
func (c *Client) request(method string, params any) error {
	c.mu.Lock()
	c.reqID++
	id := c.reqID
	label := method
	if method == "tools/call" {
		if p, ok := params.(map[string]any); ok {
			if name, ok := p["name"].(string); ok {
				label = "tools/call:" + name
			}
		}
	}
	c.pending[id] = label
	c.mu.Unlock()
	return c.send(rpcOut{JSONRPC: "2.0", ID: id, Method: method, Params: params})
}

func (c *Client) callTool(name string) error {
	return c.request("tools/call", map[string]any{"name": name, "arguments": map[string]any{}})
}

// handle dispatches one incoming JSON-RPC message.
func (c *Client) handle(ctx context.Context, data []byte) {
	var m rpcIn
	if err := json.Unmarshal(data, &m); err != nil {
		log.Printf("ide: bad message: %v", err)
		return
	}

	// Server-pushed notifications carry a method but no response id.
	switch m.Method {
	case "selection_changed":
		if sel, ok := parseSelectionParams(m.Params); ok {
			c.emit(ctx, Update{Kind: UpdateSelection, Selection: sel})
		}
		// Refresh the open-tab list whenever focus/selection moves.
		if err := c.callTool("getOpenEditors"); err != nil {
			log.Printf("ide: getOpenEditors refresh: %v", err)
		}
		return
	case "at_mentioned":
		if men, ok := parseMentionParams(m.Params); ok {
			c.emit(ctx, Update{Kind: UpdateMention, Mention: men})
		}
		return
	}

	// Otherwise it's a response to one of our requests.
	id, ok := m.intID()
	if !ok {
		return
	}
	c.mu.Lock()
	label := c.pending[id]
	delete(c.pending, id)
	c.mu.Unlock()
	if m.Error != nil {
		log.Printf("ide: %s error: %s", label, m.Error.Message)
		return
	}

	switch label {
	case "initialize":
		if err := c.notify("notifications/initialized", nil); err != nil {
			log.Printf("ide: initialized: %v", err)
			return
		}
		// Prime the editor list and current selection on connect.
		if err := c.callTool("getOpenEditors"); err != nil {
			log.Printf("ide: getOpenEditors: %v", err)
		}
		if err := c.callTool("getCurrentSelection"); err != nil {
			log.Printf("ide: getCurrentSelection: %v", err)
		}
	case "tools/call:getOpenEditors":
		if eds, ok := parseOpenEditors(toolText(m.Result)); ok {
			c.emit(ctx, Update{Kind: UpdateOpenEditors, OpenEditors: eds})
		}
	case "tools/call:getCurrentSelection":
		if sel, ok := parseSelectionParams(toolText(m.Result)); ok {
			c.emit(ctx, Update{Kind: UpdateSelection, Selection: sel})
		}
	}
}

func (c *Client) emit(ctx context.Context, u Update) {
	select {
	case c.out <- u:
	case <-ctx.Done():
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
