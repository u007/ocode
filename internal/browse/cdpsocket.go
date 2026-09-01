package browse

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"github.com/u007/ocode/internal/browse/cdp"
)

// clientMsg is the inbound JSON from the browser panel.
type clientMsg struct {
	T          string  `json:"t"`
	URL        string  `json:"url,omitempty"`
	W          int     `json:"w,omitempty"`
	H          int     `json:"h,omitempty"`
	DPR        float64 `json:"dpr,omitempty"`
	Kind       string  `json:"kind,omitempty"`
	X          float64 `json:"x,omitempty"`
	Y          float64 `json:"y,omitempty"`
	Button     string  `json:"button,omitempty"`
	ClickCount int     `json:"clickCount,omitempty"`
	DeltaX     float64 `json:"deltaX,omitempty"`
	DeltaY     float64 `json:"deltaY,omitempty"`
	Modifiers  int     `json:"modifiers,omitempty"`
	Key        string  `json:"key,omitempty"`
	Code       string  `json:"code,omitempty"`
	Text       string  `json:"text,omitempty"`
}

// cdpSink implements cdp.FrameSink by forwarding to the single writer channel.
type cdpSink struct {
	send chan wsOut
}

func (s *cdpSink) Frame(width, height uint32, jpeg []byte) {
	buf := make([]byte, 8+len(jpeg))
	binary.BigEndian.PutUint32(buf[0:4], width)
	binary.BigEndian.PutUint32(buf[4:8], height)
	copy(buf[8:], jpeg)
	select {
	case s.send <- wsOut{isBinary: true, data: buf}:
	default:
		// drop if full (should not happen with buffered channel)
	}
}

func (s *cdpSink) Console(ev cdp.ConsoleEvent) {
	m := map[string]any{"t": "console", "level": ev.Level, "args": ev.Args, "ts": ev.TS}
	b, _ := json.Marshal(m)
	select {
	case s.send <- wsOut{data: b}:
	default:
	}
}

func (s *cdpSink) Network(ev cdp.NetworkEvent) {
	m := map[string]any{"t": "network", "method": ev.Method, "url": ev.URL, "status": ev.Status, "durationMs": ev.DurationMs, "ts": ev.TS}
	if ev.Blocked != "" {
		m["blocked"] = ev.Blocked
	}
	b, _ := json.Marshal(m)
	select {
	case s.send <- wsOut{data: b}:
	default:
	}
}

func (s *cdpSink) Error(msg string) {
	m := map[string]any{"t": "error", "message": msg}
	b, _ := json.Marshal(m)
	select {
	case s.send <- wsOut{data: b}:
	default:
	}
	select {
	case s.send <- wsOut{isClose: true, closeCode: 1011, closeText: msg}:
	default:
	}
}

func marshalError(msg string) []byte {
	m := map[string]any{"t": "error", "message": msg}
	b, _ := json.Marshal(m)
	return b
}

// handleCDP upgrades the per-stateKey WebSocket that carries screencast frames,
// telemetry, and input. The wire format is defined in the plan's § Interfaces
// produced.
func (s *Server) handleCDP(w http.ResponseWriter, r *http.Request) {
	// Extract stateKey from /b/{stateKey}/__cdp (Go 1.22 pattern).
	stateKey := r.PathValue("stateKey")
	if stateKey == "" {
		// Fallback for httptest direct calls without pattern.
		p := strings.TrimPrefix(r.URL.Path, "/b/")
		if parts := strings.SplitN(p, "/", 2); len(parts) == 2 && parts[1] == "__cdp" {
			stateKey = parts[0]
		}
	}
	if stateKey == "" {
		http.Error(w, "browse: missing state key", http.StatusBadRequest)
		return
	}
	grant := r.URL.Query().Get("__grant")
	if grant == "" {
		http.Error(w, "browse: missing grant", http.StatusUnauthorized)
		return
	}
	_, sk, ok := s.auth.redeem(grant, false)
	if !ok {
		http.Error(w, "browse: invalid grant", http.StatusUnauthorized)
		return
	}
	if sk != stateKey {
		http.Error(w, "browse: state key mismatch", http.StatusForbidden)
		return
	}
	expectedOrigin := s.spaOriginFor(stateKey)
	origin := r.Header.Get("Origin")
	if origin != expectedOrigin {
		http.Error(w, "browse: origin mismatch", http.StatusForbidden)
		return
	}

	// Upgrade. CheckOrigin re-validates after redeem (grant already consumed).
	upgrader := websocket.Upgrader{
		ReadBufferSize:  4096,
		WriteBufferSize: 4096,
		CheckOrigin: func(r *http.Request) bool {
			return r.Header.Get("Origin") == s.spaOriginFor(stateKey)
		},
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.log.Printf("browse cdp: upgrade failed: %v", err)
		return
	}

	// If no manager (e.g. in tests without init, or chrome not configured), fail gracefully.
	if s.cdp == nil {
		b, _ := json.Marshal(map[string]any{"t": "error", "message": "chrome not found — set browser.chrome_path"})
		_ = conn.WriteMessage(websocket.TextMessage, b)
		_ = conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(1011, "chrome not found"))
		_ = conn.Close()
		return
	}

	send := make(chan wsOut, 32)
	sink := &cdpSink{send: send}

	// Handle replacement: second socket for same key closes first with "replaced".
	s.cdpMu.Lock()
	if old, ok := s.cdpSocks[stateKey]; ok {
		// Queue replaced error to old writer; it will close after.
		select {
		case old.send <- wsOut{data: marshalError("replaced")}:
		default:
		}
		select {
		case old.send <- wsOut{isClose: true, closeCode: 1011, closeText: "replaced"}:
		default:
		}
		// Do not delete old yet; its writer will clean up, but we replace map entry now.
	}
	entry := &cdpSocketEntry{
		send:   send,
		wsConn: conn,
		closeFn: func(code int, text string) error {
			// Used by Revoke to close this connection.
			_ = conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(code, text))
			return conn.Close()
		},
	}
	s.cdpSocks[stateKey] = entry
	s.cdpMu.Unlock()

	// Attach to Chrome (or fake). Handle known errors with JSON error+1011.
	target, err := s.cdp.Attach(r.Context(), stateKey, sink)
	if err != nil {
		msg := err.Error()
		if errors.Is(err, cdp.ErrUnsupportedPlatform) {
			msg = "Chrome mode is not supported on Windows yet"
		}
		// For ErrChromeNotFound keep its message as is (matches spec text).
		b, _ := json.Marshal(map[string]any{"t": "error", "message": msg})
		_ = conn.WriteMessage(websocket.TextMessage, b)
		_ = conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(1011, msg))
		_ = conn.Close()
		s.cdpMu.Lock()
		if cur, ok := s.cdpSocks[stateKey]; ok && cur == entry {
			delete(s.cdpSocks, stateKey)
		}
		s.cdpMu.Unlock()
		return
	}

	// Writer goroutine: single writer for this conn (gorilla concurrency rule).
	doneWriter := make(chan struct{})
	go func() {
		defer close(doneWriter)
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case msg, ok := <-send:
				if !ok {
					return
				}
				if msg.isClose {
					_ = conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(msg.closeCode, msg.closeText))
					return
				}
				if msg.isBinary {
					_ = conn.WriteMessage(websocket.BinaryMessage, msg.data)
				} else {
					_ = conn.WriteMessage(websocket.TextMessage, msg.data)
				}
			case <-ticker.C:
				_ = conn.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(10*time.Second))
			}
		}
	}()

	// Reader loop (this goroutine).
	// Ensure Detach on exit, not Revoke.
	defer func() {
		// Signal writer to exit if still running.
		// Close send if not already closed; writer will exit on channel close or isClose.
		// We avoid double-close by checking map entry.
		target.Detach()
		s.cdpMu.Lock()
		if cur, ok := s.cdpSocks[stateKey]; ok && cur == entry {
			delete(s.cdpSocks, stateKey)
		}
		s.cdpMu.Unlock()
		// Close send to wake writer if it's still waiting.
		// Use non-blocking close via recover.
		func() {
			defer func() { _ = recover() }()
			close(send)
		}()
		// Wait for writer to flush (with timeout).
		select {
		case <-doneWriter:
		case <-time.After(time.Second):
		}
		_ = conn.Close()
	}()

	// Configure read deadlines/pong handler.
	_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.SetPongHandler(func(string) error {
		_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		mt, data, err := conn.ReadMessage()
		if err != nil {
			// Client closed or error.
			break
		}
		if mt != websocket.TextMessage {
			continue
		}
		var cm clientMsg
		if err := json.Unmarshal(data, &cm); err != nil {
			s.log.Printf("browse cdp: malformed client JSON: %v", err)
			continue
		}
		// Dispatch.
		switch cm.T {
		case "nav":
			_ = target.Navigate(context.Background(), cm.URL)
		case "back":
			_ = target.Back(context.Background())
		case "forward":
			_ = target.Forward(context.Background())
		case "reload":
			_ = target.Reload(context.Background())
		case "resize":
			dpr := cm.DPR
			if dpr == 0 {
				dpr = 1
			}
			_ = target.Resize(context.Background(), cm.W, cm.H, dpr)
		case "mouse":
			_ = target.Mouse(context.Background(), cdp.MouseEvent{
				Kind:       cm.Kind,
				X:          cm.X,
				Y:          cm.Y,
				Button:     cm.Button,
				ClickCount: cm.ClickCount,
				DeltaX:     cm.DeltaX,
				DeltaY:     cm.DeltaY,
				Modifiers:  cm.Modifiers,
			})
		case "key":
			_ = target.Key(context.Background(), cdp.KeyEvent{
				Kind:      cm.Kind,
				Key:       cm.Key,
				Code:      cm.Code,
				Text:      cm.Text,
				Modifiers: cm.Modifiers,
			})
		default:
			s.log.Printf("browse cdp: unknown client t=%q", cm.T)
		}
	}
}
