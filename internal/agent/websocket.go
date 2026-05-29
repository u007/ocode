package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	wsProtocolHeader = "responses_websockets=2026-02-06"
	wsConnectTimeout = 10 * time.Second
	wsReadTimeout    = 5 * time.Minute
)

// WSMessage represents a WebSocket message for OpenAI Responses API.
type WSMessage struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
	Error   string          `json:"error,omitempty"`
}

// WebSocketClient handles WebSocket connections to OpenAI.
type WebSocketClient struct {
	conn      *websocket.Conn
	mu        sync.Mutex
	baseURL   string
	apiKey    string
	connected bool
}

// NewWebSocketClient creates a new WebSocket client.
func NewWebSocketClient(baseURL, apiKey string) *WebSocketClient {
	return &WebSocketClient{
		baseURL: baseURL,
		apiKey:  apiKey,
	}
}

// Connect establishes a WebSocket connection.
func (w *WebSocketClient) Connect(ctx context.Context) error {
	wsURL, err := toWebSocketURL(w.baseURL)
	if err != nil {
		return fmt.Errorf("convert URL: %w", err)
	}

	dialer := websocket.Dialer{
		HandshakeTimeout: wsConnectTimeout,
	}

	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+w.apiKey)
	headers.Set("openai-beta", wsProtocolHeader)

	conn, _, err := dialer.DialContext(ctx, wsURL, headers)
	if err != nil {
		return fmt.Errorf("websocket connect: %w", err)
	}

	w.conn = conn
	w.connected = true
	return nil
}

// Send sends a message via WebSocket.
func (w *WebSocketClient) Send(ctx context.Context, msg *WSMessage) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.connected {
		return fmt.Errorf("websocket not connected")
	}

	return w.conn.WriteJSON(msg)
}

// Receive receives a message from WebSocket.
func (w *WebSocketClient) Receive(ctx context.Context) (*WSMessage, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.connected {
		return nil, fmt.Errorf("websocket not connected")
	}

	// Set read deadline
	if err := w.conn.SetReadDeadline(time.Now().Add(wsReadTimeout)); err != nil {
		return nil, fmt.Errorf("set read deadline: %w", err)
	}

	var msg WSMessage
	if err := w.conn.ReadJSON(&msg); err != nil {
		return nil, fmt.Errorf("read websocket: %w", err)
	}

	return &msg, nil
}

// Close closes the WebSocket connection.
func (w *WebSocketClient) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.conn != nil {
		w.connected = false
		return w.conn.Close()
	}
	return nil
}

// IsConnected returns true if the WebSocket is connected.
func (w *WebSocketClient) IsConnected() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.connected
}

// toWebSocketURL converts an HTTP URL to a WebSocket URL.
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

// SupportsWebSocket checks if the provider supports WebSocket transport.
func SupportsWebSocket(provider string) bool {
	return strings.EqualFold(provider, "openai")
}
