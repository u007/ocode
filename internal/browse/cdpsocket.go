package browse

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"github.com/u007/ocode/internal/browse/cdp"
	"github.com/u007/ocode/internal/crashguard"
)

// clientMsg is the inbound JSON from the browser panel.
type clientMsg struct {
	T             string  `json:"t"`
	RequestID     string  `json:"requestId,omitempty"`
	URL           string  `json:"url,omitempty"`
	W             int     `json:"w,omitempty"`
	H             int     `json:"h,omitempty"`
	DPR           float64 `json:"dpr,omitempty"`
	Kind          string  `json:"kind,omitempty"`
	X             float64 `json:"x,omitempty"`
	Y             float64 `json:"y,omitempty"`
	Button        string  `json:"button,omitempty"`
	Buttons       int     `json:"buttons,omitempty"`
	ClickCount    int     `json:"clickCount,omitempty"`
	DeltaX        float64 `json:"deltaX,omitempty"`
	DeltaY        float64 `json:"deltaY,omitempty"`
	Modifiers     int     `json:"modifiers,omitempty"`
	Key           string  `json:"key,omitempty"`
	Code          string  `json:"code,omitempty"`
	Text          string  `json:"text,omitempty"`
	AutoRepeat    bool    `json:"autoRepeat,omitempty"`
	Factor        float64 `json:"factor,omitempty"`
	Query         string  `json:"query,omitempty"`
	Backwards     bool    `json:"backwards,omitempty"`
	CaseSensitive bool    `json:"caseSensitive,omitempty"`
	Points        []struct {
		ID int     `json:"id"`
		X  float64 `json:"x"`
		Y  float64 `json:"y"`
	} `json:"points,omitempty"`
	// CDP request fields (DOM.getNodeForLocation / DOM.describeNode).
	Method string                 `json:"method,omitempty"`
	Params map[string]interface{} `json:"params,omitempty"`
}

// cdpSink implements cdp.FrameSink by forwarding to the single writer channel,
// via entry's closed-guard so a send never races entry.closeSend().
type cdpSink struct {
	entry *cdpSocketEntry
}

func (s *cdpSink) Frame(width, height uint32, jpeg []byte) {
	buf := make([]byte, 8+len(jpeg))
	binary.BigEndian.PutUint32(buf[0:4], width)
	binary.BigEndian.PutUint32(buf[4:8], height)
	copy(buf[8:], jpeg)
	s.entry.trySend(wsOut{isBinary: true, data: buf})
}

func (s *cdpSink) Console(ev cdp.ConsoleEvent) {
	m := map[string]any{"t": "console", "level": ev.Level, "args": ev.Args, "ts": ev.TS}
	b, _ := json.Marshal(m)
	s.entry.trySend(wsOut{data: b})
}

func (s *cdpSink) Network(ev cdp.NetworkEvent) {
	m := map[string]any{
		"t":          "network",
		"requestId":  ev.RequestID,
		"method":     ev.Method,
		"url":        ev.URL,
		"status":     ev.Status,
		"durationMs": ev.DurationMs,
		"ts":         ev.TS,
		"size":       ev.Size,
	}
	if ev.Blocked != "" {
		m["blocked"] = ev.Blocked
	}
	if ev.ContentType != "" {
		m["contentType"] = ev.ContentType
	}
	if len(ev.RequestHeaders) > 0 {
		m["requestHeaders"] = ev.RequestHeaders
	}
	if len(ev.ResponseHeaders) > 0 {
		m["responseHeaders"] = ev.ResponseHeaders
	}
	if ev.PostData != "" {
		m["postData"] = ev.PostData
	}
	b, _ := json.Marshal(m)
	s.entry.trySend(wsOut{data: b})
}

// FileChooser asks the SPA to open a native file picker for the page's
// intercepted <input type=file>; the picked files come back through
// POST /api/browse/upload → Server.SetFiles.
func (s *cdpSink) FileChooser(multiple bool) {
	b, _ := json.Marshal(map[string]any{"t": "fileChooser", "multiple": multiple})
	s.entry.trySend(wsOut{data: b})
}

func (s *cdpSink) Performance(metrics map[string]float64) {
	m := map[string]any{"t": "performance", "metrics": metrics}
	b, _ := json.Marshal(m)
	s.entry.trySend(wsOut{data: b})
}

func (s *cdpSink) Error(msg string) {
	m := map[string]any{"t": "error", "message": msg}
	b, _ := json.Marshal(m)
	s.entry.trySend(wsOut{data: b})
	s.entry.trySend(wsOut{isClose: true, closeCode: 1011, closeText: msg})
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
	entry := &cdpSocketEntry{
		send:   send,
		wsConn: conn,
		closeFn: func(code int, text string) error {
			// Used by Revoke to close this connection. WriteControl (unlike
			// WriteMessage) is safe to call concurrently with the writer
			// goroutine's WriteMessage calls per gorilla/websocket's
			// concurrency contract, so this doesn't race the single writer.
			_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(code, text), time.Now().Add(time.Second))
			return conn.Close()
		},
	}
	sink := &cdpSink{entry: entry}

	// Handle replacement: second socket for same key closes first with "replaced".
	s.cdpMu.Lock()
	if old, ok := s.cdpSocks[stateKey]; ok {
		// Queue replaced error to old writer; it will close after.
		old.trySend(wsOut{data: marshalError("replaced")})
		old.trySend(wsOut{isClose: true, closeCode: 1011, closeText: "replaced"})
		// Do not delete old yet; its writer will clean up, but we replace map entry now.
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

	// Sync the authoritative recording state so the performance tab never
	// claims "recording" while the CDP domain is disabled (or vice versa).
	// Re-sent on every attach, including reconnects.
	if b, merr := json.Marshal(map[string]any{"t": "perfState", "recording": target.PerfRecording()}); merr == nil {
		entry.trySend(wsOut{data: b})
	}

	// Reader loop (this goroutine).
	// Ensure Detach on exit, not Revoke.
	defer func() {
		// Signal writer to exit if still running.
		target.DetachSink(sink)
		s.cdpMu.Lock()
		if cur, ok := s.cdpSocks[stateKey]; ok && cur == entry {
			delete(s.cdpSocks, stateKey)
		}
		s.cdpMu.Unlock()
		// Close send to wake writer if it's still waiting. Guarded against a
		// racing trySend (e.g. from Revoke or a replacement socket) by
		// entry.mu, so this never closes a channel a sender is mid-send on.
		entry.closeSend()
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
		case "fileChooserCancel":
			// User dismissed the picker: drop the pending chooser so a stale
			// upload cannot land in a later input.
			if err := target.SetFiles(context.Background(), nil); err != nil && !errors.Is(err, cdp.ErrNoFileChooser) {
				s.log.Printf("browse cdp: cancel file chooser for %s: %v", stateKey, err)
			}
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
				Buttons:    cm.Buttons,
				ClickCount: cm.ClickCount,
				DeltaX:     cm.DeltaX,
				DeltaY:     cm.DeltaY,
				Modifiers:  cm.Modifiers,
			})
		case "key":
			_ = target.Key(context.Background(), cdp.KeyEvent{
				Kind:       cm.Kind,
				Key:        cm.Key,
				Code:       cm.Code,
				Text:       cm.Text,
				Modifiers:  cm.Modifiers,
				AutoRepeat: cm.AutoRepeat,
			})
		case "zoom":
			if zt, ok := target.(interface {
				SetZoom(context.Context, float64) error
			}); ok {
				if err := zt.SetZoom(context.Background(), cm.Factor); err != nil {
					s.log.Printf("browse cdp: zoom for %s: %v", stateKey, err)
				}
			}
		case "touch":
			if tt, ok := target.(interface {
				Touch(context.Context, cdp.TouchEvent) error
			}); ok {
				ev := cdp.TouchEvent{Kind: cm.Kind, Modifiers: cm.Modifiers}
				for _, p := range cm.Points {
					ev.Points = append(ev.Points, cdp.TouchPoint{ID: p.ID, X: p.X, Y: p.Y})
				}
				if err := tt.Touch(context.Background(), ev); err != nil {
					s.log.Printf("browse cdp: touch for %s: %v", stateKey, err)
				}
			}
		case "insertText":
			// Host clipboard paste / IME commit → caret insertion. Optional
			// method: the chromeTarget interface is intentionally not extended.
			if it, ok := target.(interface {
				InsertText(context.Context, string) error
			}); ok {
				if err := it.InsertText(context.Background(), cm.Text); err != nil {
					s.log.Printf("browse cdp: insertText for %s: %v", stateKey, err)
				}
			}
		case "getSelection":
			// Copy bridge: report the page's selected text so the SPA can
			// write it to the host clipboard.
			if st, ok := target.(interface {
				SelectionText(context.Context) (string, error)
			}); ok {
				text, err := st.SelectionText(context.Background())
				if err != nil {
					s.log.Printf("browse cdp: getSelection for %s: %v", stateKey, err)
					break
				}
				if b, merr := json.Marshal(map[string]any{"t": "selection", "text": text}); merr == nil {
					select {
					case send <- wsOut{data: b}:
					default:
					}
				}
			}
		case "getResponseBody":
			body, isBase64, truncated, err := target.GetResponseBody(context.Background(), cm.RequestID)
			resp := map[string]any{"t": "responseBody", "requestId": cm.RequestID}
			if err != nil {
				resp["error"] = err.Error()
			} else {
				resp["body"] = body
				resp["base64Encoded"] = isBase64
				resp["truncated"] = truncated
			}
			b, _ := json.Marshal(resp)
			select {
			case send <- wsOut{data: b}:
			default:
			}
		case "scrollTo":
			// Restore a persisted scroll offset (CSS px in cm.Y). Best-effort:
			// the page may still be rendering; the SPA retries a few times.
			// The concrete *cdp.Target offers ScrollTo/ScrollY but the
			// chromeTarget interface is intentionally NOT extended (test
			// fakes must not grow new required methods) - assert optionally.
			if st, ok := target.(interface {
				ScrollTo(context.Context, float64, float64) error
			}); ok {
				_ = st.ScrollTo(context.Background(), 0, cm.Y)
			}
		case "getScroll":
			// Report the live scroll offset so the SPA can persist it.
			if st, ok := target.(interface {
				ScrollY(context.Context) (float64, error)
			}); ok {
				if y, err := st.ScrollY(context.Background()); err == nil {
					if b, merr := json.Marshal(map[string]any{"t": "scroll", "y": y}); merr == nil {
						select {
						case send <- wsOut{data: b}:
						default:
						}
					}
				}
			}
		case "cdp_request":
			// DOM.getNodeForLocation / DOM.describeNode (context-menu lookup).
			// Reuse string requestId correlation from the client.
			reqCtx, reqCancel := context.WithTimeout(context.Background(), 5*time.Second)
			crashguard.Go(func() {
				defer reqCancel()
				resp := map[string]any{"t": "cdp_response", "requestId": cm.RequestID}
				switch cm.Method {
				case "DOM.getNodeForLocation":
					if nl, ok := target.(nodeLocationTarget); ok {
						loc, err := nl.GetNodeForLocation(reqCtx, int(cm.X), int(cm.Y))
						if err != nil {
							resp["error"] = err.Error()
						} else {
							resp["result"] = loc
						}
					} else {
						resp["error"] = cdp.ErrDisconnected
					}
				case "DOM.describeNode":
					if dn, ok := target.(describeNodeTarget); ok {
						nodeId := 0
						if nid, ok := cm.Params["nodeId"]; ok {
							switch v := nid.(type) {
							case float64:
								nodeId = int(v)
							case int:
								nodeId = v
							case string:
								if n, err := strconv.Atoi(v); err == nil {
									nodeId = n
								}
							}
						}
						opts := map[string]any{"depth": 0, "pierce": false}
						result, err := dn.DescribeNode(reqCtx, nodeId, opts)
						if err != nil {
							resp["error"] = err.Error()
						} else {
							resp["result"] = result
						}
					} else {
						resp["error"] = cdp.ErrDisconnected
					}
				default:
					resp["error"] = "unknown_method: " + cm.Method
				}
				if b, merr := json.Marshal(resp); merr == nil {
					entry.trySend(wsOut{data: b})
				}
			})
		case "perfStart", "perfStop":
			// Toggle metrics collection. Errors are reported back in the
			// perfState ack (not discarded) so the tab stays in sync with
			// the authoritative backend state.
			var perr error
			if cm.T == "perfStart" {
				perr = target.PerfStart(context.Background())
			} else {
				perr = target.PerfStop(context.Background())
			}
			presp := map[string]any{"t": "perfState", "recording": target.PerfRecording()}
			if perr != nil {
				presp["error"] = perr.Error()
				s.log.Printf("browse cdp: %s for %s: %v", cm.T, stateKey, perr)
			}
			if pb, merr := json.Marshal(presp); merr == nil {
				select {
				case send <- wsOut{data: pb}:
				default:
				}
			}
		default:
			s.log.Printf("browse cdp: unknown client t=%q", cm.T)
		}
	}
}
