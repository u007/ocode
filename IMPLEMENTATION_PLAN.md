# Implementation Plan: Three Features for Ocode

**Date:** 2026-05-29  
**Features:** Responsive Prompt, Subagent Retry Status, OpenAI WebSocket Transport

---

## Context Summary

### Current State of Ocode

**Architecture:**
- Go + Bubble Tea TUI framework
- HTTP/SSE for LLM streaming (not WebSocket)
- Fixed prompt height (3 lines)
- Basic subagent support with permission dialogs

**Key Files:**
- `internal/tui/model.go` — Main TUI model (8000+ lines)
- `internal/agent/client.go` — LLM client with OpenAI support
- `internal/agent/subagent.go` — Subagent definitions
- `internal/config/config.go` — Configuration

---

## Feature 1: Responsive Prompt Sizing

### What OpenCode Does
OpenCode's prompt now:
- **Dynamically adjusts height** based on terminal size
- **Default formula:** `max_height = max(6, floor(terminal_height / 3))`
- **Configurable** via `tui.prompt.max_height` and `tui.prompt.max_width`
- **Auto-width** option for prompt to scale with terminal

### Current Ocode State
```go
// internal/tui/model.go:964
ta.SetHeight(3)  // HARDCODED to 3 lines
ta.MaxWidth = 80  // HARDCODED to 80 chars
```

Window resize is handled but doesn't adjust prompt:
```go
case tea.WindowSizeMsg:
    m.width = msg.Width
    m.height = msg.Height
    m.layout()  // layout() doesn't adjust prompt height
```

### Implementation Plan

**Step 1: Add config fields**
```go
// internal/config/config.go
type TUIConfig struct {
    Prompt *PromptConfig `json:"prompt,omitempty"`
}

type PromptConfig struct {
    MaxHeight *int    `json:"max_height,omitempty"`
    MaxWidth  *int    `json:"max_width,omitempty"`  // or "auto"
}
```

**Step 2: Calculate prompt height in layout()**
```go
func (m *model) layout() {
    // ... existing code ...
    
    // Calculate responsive prompt height
    promptHeight := m.promptHeight()
    m.input.SetHeight(promptHeight)
    
    // ... rest of layout ...
}

func (m *model) promptHeight() int {
    // Use config if set
    if m.config != nil && m.config.TUI != nil && m.config.TUI.Prompt != nil {
        if m.config.TUI.Prompt.MaxHeight != nil {
            return *m.config.TUI.Prompt.MaxHeight
        }
    }
    
    // Default: max(6, terminal_height / 3)
    h := m.height / 3
    if h < 6 {
        h = 6
    }
    return h
}
```

**Step 3: Update layout() to use responsive height**
```go
func (m *model) layout() {
    if m.width <= 0 || m.height <= 0 {
        return
    }

    panelWidth := m.panelWidth()
    innerWidth := panelWidth - 7
    if innerWidth < 1 {
        innerWidth = 1
    }
    m.input.SetWidth(innerWidth)
    m.input.MaxWidth = innerWidth
    
    // NEW: Responsive prompt height
    m.input.SetHeight(m.promptHeight())
    
    m.viewport.SetWidth(innerWidth)
    // ... rest unchanged ...
}
```

**Files to Modify:**
- `internal/config/config.go` — Add PromptConfig struct
- `internal/tui/model.go` — Add promptHeight() method, update layout()

**Effort:** 1-2 days  
**Risk:** Low — purely additive, no behavior change for existing users

---

## Feature 2: Subagent Retry Status Surfacing

### What OpenCode Does
When a subagent fails and retries:
1. Shows a **dialog alert** with the retry error message
2. Surfaces retry status in the **session navigation**
3. Provides visibility into background operations

```typescript
function enterChild(sessionID: string) {
    navigate({ type: "session", sessionID })
    const status = sync.data.session_status[sessionID]
    if (status?.type === "retry") 
        void DialogAlert.show(dialog, "Retry Error", status.message)
}
```

### Current Ocode State
- Subagents run in background via `task` tool
- Permission dialogs exist for subagent operations
- **No retry status visibility** — if a subagent fails, user doesn't know
- Subagent results come back via `JobEvent` channel

```go
// internal/agent/agent.go
type JobEvent struct {
    Kind       string // "process" or "agent"
    ID         string
    Name       string
    Status     string // exited/killed/done/failed
    Result     string
    Background bool
    ToolCallID string
}
```

### Implementation Plan

**Step 1: Add retry tracking to Agent**
```go
// internal/agent/agent.go
type Agent struct {
    // ... existing fields ...
    subAgentRetries map[string]int  // track retry counts
    subAgentErrors  map[string]string  // track last error
}

type SubAgentStatus struct {
    ID          string
    Name        string
    Retries     int
    LastError   string
    Status      string  // running/retry/failed/done
}
```

**Step 2: Surface retry status in TUI**
```go
// internal/tui/model.go
type model struct {
    // ... existing fields ...
    subAgentStatuses map[string]*agent.SubAgentStatus
    showRetryDialog  bool
    retryDialogMsg   string
}

// Handle subagent status updates
case agent.SubAgentStatusMsg:
    m.subAgentStatuses[msg.Status.ID] = msg.Status
    if msg.Status.Status == "retry" && msg.Status.LastError != "" {
        m.showRetryDialog = true
        m.retryDialogMsg = fmt.Sprintf("Subagent %s retrying: %s", 
            msg.Status.Name, msg.Status.LastError)
    }
```

**Step 3: Add retry dialog rendering**
```go
func (m model) renderRetryDialog() string {
    if !m.showRetryDialog {
        return ""
    }
    
    style := lipgloss.NewStyle().
        Border(lipgloss.RoundedBorder()).
        BorderForeground(lipgloss.Color("11")).
        Padding(1, 2).
        Width(60)
    
    content := fmt.Sprintf(
        "⚠ Subagent Retry\n\n%s\n\nPress Enter to dismiss",
        m.retryDialogMsg,
    )
    
    return style.Render(content)
}
```

**Step 4: Wire up in Update()**
```go
case tea.KeyPressMsg:
    if m.showRetryDialog {
        if msg.String() == "enter" || msg.String() == "esc" {
            m.showRetryDialog = false
            return m, nil
        }
    }
    // ... existing key handling ...
```

**Files to Modify:**
- `internal/agent/agent.go` — Add retry tracking, SubAgentStatus type
- `internal/tui/model.go` — Add retry dialog, handle status updates
- `internal/agent/subagent.go` — Emit status on retry

**Effort:** 2-3 days  
**Risk:** Medium — requires careful handling of concurrent status updates

---

## Feature 3: OpenAI WebSocket Transport

### What OpenCode Does
OpenCode added **WebSocket transport** for OpenAI's Responses API:
- **Protocol:** `responses_websockets=2026-02-06`
- **Benefits:** 
  - Bidirectional communication (not just SSE)
  - Better error recovery
  - Connection pooling
  - Automatic retry on connection drop

```typescript
// packages/opencode/src/plugin/openai/ws.ts
export function connectResponsesWebSocket(options) {
    return new Promise((resolve, reject) => {
        const socket = new WebSocket(url, { headers })
        // ... connection logic ...
    })
}
```

### Current Ocode State
Ocode uses **HTTP SSE** for streaming:
```go
// internal/agent/client.go:457
func parseOpenAIChatCompletionsStream(body io.Reader, ...) (*Message, json.RawMessage, error) {
    scanner := bufio.NewScanner(body)
    for scanner.Scan() {
        // Parse SSE lines
        // Extract delta content
    }
}
```

**Limitations of current approach:**
- One-way streaming (client → server via HTTP, server → client via SSE)
- No bidirectional communication
- Connection drops require full reconnect
- No built-in retry mechanism

### Implementation Plan

**Step 1: Add WebSocket dependency**
```bash
go get github.com/gorilla/websocket
```

**Step 2: Create WebSocket client**
```go
// internal/agent/websocket.go (NEW FILE)
package agent

import (
    "context"
    "encoding/json"
    "fmt"
    "net/url"
    "sync"
    
    "github.com/gorilla/websocket"
)

const (
    wsProtocolHeader = "responses_websockets=2026-02-06"
    wsConnectTimeout = 10 * time.Second
)

type WebSocketClient struct {
    conn        *websocket.Conn
    mu          sync.Mutex
    baseURL     string
    apiKey      string
    connected   bool
}

type WSMessage struct {
    Type    string          `json:"type"`
    Payload json.RawMessage `json:"payload,omitempty"`
    Error   string          `json:"error,omitempty"`
}

func NewWebSocketClient(baseURL, apiKey string) *WebSocketClient {
    return &WebSocketClient{
        baseURL: baseURL,
        apiKey:  apiKey,
    }
}

func (w *WebSocketClient) Connect(ctx context.Context) error {
    // Convert HTTP URL to WebSocket URL
    wsURL, err := toWebSocketURL(w.baseURL)
    if err != nil {
        return fmt.Errorf("convert URL: %w", err)
    }
    
    // Create WebSocket connection
    dialer := websocket.Dialer{
        HandshakeTimeout: wsConnectTimeout,
    }
    
    headers := map[string]string{
        "Authorization": "Bearer " + w.apiKey,
        "openai-beta":   wsProtocolHeader,
    }
    
    conn, _, err := dialer.DialContext(ctx, wsURL, httpHeaderFromMap(headers))
    if err != nil {
        return fmt.Errorf("websocket connect: %w", err)
    }
    
    w.conn = conn
    w.connected = true
    return nil
}

func (w *WebSocketClient) Send(ctx context.Context, msg *WSMessage) error {
    w.mu.Lock()
    defer w.mu.Unlock()
    
    if !w.connected {
        return fmt.Errorf("websocket not connected")
    }
    
    return w.conn.WriteJSON(msg)
}

func (w *WebSocketClient) Receive(ctx context.Context) (*WSMessage, error) {
    w.mu.Lock()
    defer w.mu.Unlock()
    
    if !w.connected {
        return nil, fmt.Errorf("websocket not connected")
    }
    
    var msg WSMessage
    if err := w.conn.ReadJSON(&msg); err != nil {
        return nil, fmt.Errorf("read websocket: %w", err)
    }
    
    return &msg, nil
}

func (w *WebSocketClient) Close() error {
    w.mu.Lock()
    defer w.mu.Unlock()
    
    if w.conn != nil {
        w.connected = false
        return w.conn.Close()
    }
    return nil
}

func toWebSocketURL(httpURL string) (string, error) {
    u, err := url.Parse(httpURL)
    if err != nil {
        return "", err
    }
    
    switch u.Scheme {
    case "http":
        u.Scheme = "ws"
    case "https":
        u.Scheme = "wss"
    default:
        return "", fmt.Errorf("unsupported scheme: %s", u.Scheme)
    }
    
    return u.String(), nil
}
```

**Step 3: Add WebSocket transport to client**
```go
// internal/agent/client.go

type GenericClient struct {
    // ... existing fields ...
    useWebSocket bool  // NEW: enable WebSocket transport
}

func (c *GenericClient) chatOpenAI(ctx context.Context, messages []Message, tools []map[string]interface{}) (*Message, error) {
    // Check if WebSocket should be used
    if c.useWebSocket && c.Provider == "openai" {
        return c.chatOpenAIWebSocket(ctx, messages, tools)
    }
    
    // ... existing HTTP SSE logic ...
}

func (c *GenericClient) chatOpenAIWebSocket(ctx context.Context, messages []Message, tools []map[string]interface{}) (*Message, error) {
    wsClient := NewWebSocketClient(c.BaseURL, c.APIKey)
    defer wsClient.Close()
    
    // Connect
    if err := wsClient.Connect(ctx); err != nil {
        // Fallback to HTTP SSE on connection failure
        return c.chatOpenAIHTTP(ctx, messages, tools)
    }
    
    // Build request payload
    payload := map[string]interface{}{
        "model":    c.Model,
        "messages": messages,
        "stream":   true,
    }
    // ... add tools, reasoning, etc. ...
    
    // Send via WebSocket
    if err := wsClient.Send(ctx, &WSMessage{
        Type:    "response.create",
        Payload: payload,
    }); err != nil {
        // Fallback to HTTP SSE
        return c.chatOpenAIHTTP(ctx, messages, tools)
    }
    
    // Receive streaming responses
    return w.receiveStream(ctx, wsClient)
}

func (w *WebSocketClient) receiveStream(ctx context.Context, client *WebSocketClient) (*Message, error) {
    var content strings.Builder
    var reasoningContent strings.Builder
    
    for {
        select {
        case <-ctx.Done():
            return nil, ctx.Err()
        default:
            msg, err := client.Receive(ctx)
            if err != nil {
                return nil, err
            }
            
            switch msg.Type {
            case "response.output_text.delta":
                // Handle text delta
                var delta struct {
                    Delta string `json:"delta"`
                }
                json.Unmarshal(msg.Payload, &delta)
                content.WriteString(delta.Delta)
                
            case "response.completed":
                // Handle completion
                return &Message{
                    Role:            "assistant",
                    Content:         content.String(),
                    ReasoningContent: reasoningContent.String(),
                }, nil
                
            case "error":
                return nil, fmt.Errorf("websocket error: %s", msg.Error)
            }
        }
    }
}
```

**Step 4: Add config option**
```go
// internal/config/config.go
type ProviderConfig struct {
    // ... existing fields ...
    UseWebSocket *bool `json:"use_websocket,omitempty"`
}

// In opencode.json:
{
    "providers": {
        "openai": {
            "use_websocket": true
        }
    }
}
```

**Step 5: Add fallback mechanism**
```go
func (c *GenericClient) chatOpenAIWithFallback(ctx context.Context, messages []Message, tools []map[string]interface{}) (*Message, error) {
    // Try WebSocket first if enabled
    if c.useWebSocket {
        msg, err := c.chatOpenAIWebSocket(ctx, messages, tools)
        if err == nil {
            return msg, nil
        }
        // Log error and fallback
        emitDebug("websocket", fmt.Sprintf("WebSocket failed, falling back to HTTP: %v", err))
    }
    
    // Fallback to HTTP SSE
    return c.chatOpenAIHTTP(ctx, messages, tools)
}
```

**Files to Create/Modify:**
- `internal/agent/websocket.go` — NEW: WebSocket client
- `internal/agent/client.go` — Add WebSocket transport option
- `internal/config/config.go` — Add use_websocket config
- `go.mod` — Add gorilla/websocket dependency

**Effort:** 5-7 days  
**Risk:** Medium-High — requires careful error handling and fallback logic

---

## Implementation Order

### Phase 1: Quick Wins (Days 1-2)
1. **Responsive Prompt Sizing** — Low risk, immediate UX improvement
2. **Subagent Retry Status** — Medium risk, improves debugging

### Phase 2: Core Feature (Days 3-7)
3. **OpenAI WebSocket Transport** — Medium-High risk, improves reliability

---

## Testing Strategy

### Responsive Prompt
- [ ] Test on different terminal sizes (80x24, 120x40, 200x60)
- [ ] Verify prompt height adjusts dynamically
- [ ] Test config override works
- [ ] Ensure no layout breaking on small terminals

### Subagent Retry Status
- [ ] Trigger subagent failure and verify retry dialog appears
- [ ] Test dialog dismissal (Enter/Esc)
- [ ] Verify concurrent subagent status updates
- [ ] Test with multiple subagents running

### WebSocket Transport
- [ ] Test with OpenAI API key
- [ ] Test fallback to HTTP SSE on connection failure
- [ ] Test with custom base URLs
- [ ] Verify streaming works correctly
- [ ] Test error handling (invalid API key, rate limits)

---

## Risk Mitigation

1. **Responsive Prompt:** Purely additive, no breaking changes
2. **Subagent Status:** Add retry tracking without modifying existing flow
3. **WebSocket:** Always fallback to HTTP SSE, never break existing functionality

---

*Generated: 2026-05-29*  
*Next: Review and approve plan before implementation*
