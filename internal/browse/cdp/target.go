package cdp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Target represents one browser context + page target for a stateKey.
type Target struct {
	manager          *Manager
	stateKey         string
	browserContextID string
	targetID         string
	sessionID        string
	conn             *Conn

	mu   sync.Mutex
	sink FrameSink

	// viewport state
	vpW, vpH int
	vpDPR    float64

	// main frame tracking
	mainFrameID string

	// request timing for Network telemetry
	reqStart map[string]time.Time
	reqMu    sync.Mutex

	// handler cancels
	cancels []func()
}

func (t *Target) startHandlers() {
	if t.conn == nil {
		return
	}
	// Screencast frames
	chFrame, c1 := t.conn.Subscribe(t.sessionID, "Page.screencastFrame")
	chNav, c2 := t.conn.Subscribe(t.sessionID, "Page.frameNavigated")
	chWithin, c3 := t.conn.Subscribe(t.sessionID, "Page.navigatedWithinDocument")
	chResp, c4 := t.conn.Subscribe(t.sessionID, "Network.responseReceived")
	chFail, c5 := t.conn.Subscribe(t.sessionID, "Network.loadingFailed")
	chReq, c6 := t.conn.Subscribe(t.sessionID, "Network.requestWillBeSent")
	chConsole, c7 := t.conn.Subscribe(t.sessionID, "Runtime.consoleAPICalled")
	chExc, c8 := t.conn.Subscribe(t.sessionID, "Runtime.exceptionThrown")
	t.cancels = append(t.cancels, c1, c2, c3, c4, c5, c6, c7, c8)
	t.reqStart = make(map[string]time.Time)

	go t.handleScreencast(chFrame)
	go t.handleFrameNavigated(chNav)
	go t.handleNavigatedWithinDocument(chWithin)
	go t.handleResponseReceived(chResp)
	go t.handleLoadingFailed(chFail)
	go t.handleRequestWillBeSent(chReq)
	go t.handleConsole(chConsole)
	go t.handleException(chExc)

	// Learn main frame via getFrameTree async
	go func() {
		var res struct {
			FrameTree struct {
				Frame struct {
					ID string `json:"id"`
				} `json:"frame"`
			} `json:"frameTree"`
		}
		_ = t.conn.Call(context.Background(), t.sessionID, "Page.getFrameTree", nil, &res)
		if res.FrameTree.Frame.ID != "" {
			t.mu.Lock()
			if t.mainFrameID == "" {
				t.mainFrameID = res.FrameTree.Frame.ID
			}
			t.mu.Unlock()
		}
	}()
}

func (t *Target) handleScreencast(ch <-chan json.RawMessage) {
	for raw := range ch {
		var ev struct {
			Data      string `json:"data"`
			SessionID int    `json:"sessionId"`
			Metadata  struct {
				DeviceWidth  uint32 `json:"deviceWidth"`
				DeviceHeight uint32 `json:"deviceHeight"`
			} `json:"metadata"`
		}
		_ = json.Unmarshal(raw, &ev)
		data, err := base64.StdEncoding.DecodeString(ev.Data)
		if err != nil {
			// try raw bytes as fallback
			data = []byte(ev.Data)
		}
		t.mu.Lock()
		sink := t.sink
		t.mu.Unlock()
		if sink != nil {
			sink.Frame(ev.Metadata.DeviceWidth, ev.Metadata.DeviceHeight, data)
		}
		// Ack after sink returns
		_ = t.conn.Call(context.Background(), t.sessionID, "Page.screencastFrameAck", map[string]int{"sessionId": ev.SessionID}, nil)
	}
}

func (t *Target) handleFrameNavigated(ch <-chan json.RawMessage) {
	for raw := range ch {
		var ev struct {
			Frame struct {
				ID       string `json:"id"`
				ParentID string `json:"parentId"`
				URL      string `json:"url"`
			} `json:"frame"`
		}
		_ = json.Unmarshal(raw, &ev)
		isMain := ev.Frame.ParentID == ""
		if isMain {
			t.mu.Lock()
			if t.mainFrameID == "" {
				t.mainFrameID = ev.Frame.ID
			}
			isMainKnown := t.mainFrameID == ev.Frame.ID
			t.mu.Unlock()
			if !isMainKnown {
				continue
			}
			// Check scheme
			if ev.Frame.URL != "" && !isHTTPSScheme(ev.Frame.URL) && !strings.HasPrefix(ev.Frame.URL, "about:") {
				// Navigate to about:blank and emit error
				_ = t.conn.Call(context.Background(), t.sessionID, "Page.navigate", map[string]string{"url": "about:blank"}, nil)
				t.manager.emitNav(NavEvent{StateKey: t.stateKey, Error: "unsupported URL scheme"})
			}
		}
	}
}

func (t *Target) handleNavigatedWithinDocument(ch <-chan json.RawMessage) {
	for raw := range ch {
		var ev struct {
			FrameID string `json:"frameId"`
			URL     string `json:"url"`
		}
		_ = json.Unmarshal(raw, &ev)
		t.mu.Lock()
		mainID := t.mainFrameID
		t.mu.Unlock()
		if mainID != "" && ev.FrameID != mainID {
			continue
		}
		t.manager.emitNav(NavEvent{StateKey: t.stateKey, URL: ev.URL, Status: 200})
	}
}

func (t *Target) handleResponseReceived(ch <-chan json.RawMessage) {
	for raw := range ch {
		var ev struct {
			RequestID string `json:"requestId"`
			Type      string `json:"type"`
			FrameID   string `json:"frameId"`
			Response  struct {
				URL    string `json:"url"`
				Status int    `json:"status"`
			} `json:"response"`
		}
		_ = json.Unmarshal(raw, &ev)
		t.mu.Lock()
		mainID := t.mainFrameID
		t.mu.Unlock()
		// Determine if main document response
		if ev.Type == "Document" && (mainID == "" || ev.FrameID == mainID) {
			// Update mainFrameID if unknown
			if mainID == "" {
				t.mu.Lock()
				t.mainFrameID = ev.FrameID
				t.mu.Unlock()
			}
			t.manager.emitNav(NavEvent{StateKey: t.stateKey, URL: ev.Response.URL, Status: ev.Response.Status})
		}
		// Network telemetry
		t.reqMu.Lock()
		start, ok := t.reqStart[ev.RequestID]
		t.reqMu.Unlock()
		var dur int64
		if ok {
			dur = time.Since(start).Milliseconds()
		}
		t.mu.Lock()
		sink := t.sink
		t.mu.Unlock()
		if sink != nil {
			sink.Network(NetworkEvent{
				URL:        ev.Response.URL,
				Status:     ev.Response.Status,
				DurationMs: dur,
				TS:         time.Now().UnixMilli(),
			})
		}
	}
}

func (t *Target) handleLoadingFailed(ch <-chan json.RawMessage) {
	for raw := range ch {
		var ev struct {
			RequestID  string `json:"requestId"`
			Type       string `json:"type"`
			FrameID    string `json:"frameId"`
			ErrorText  string `json:"errorText"`
			BlockedReason string `json:"blockedReason"`
			Canceled   bool   `json:"canceled"`
		}
		_ = json.Unmarshal(raw, &ev)
		// Main document error → nav error
		t.mu.Lock()
		mainID := t.mainFrameID
		t.mu.Unlock()
		isMain := ev.Type == "Document" && (mainID == "" || ev.FrameID == mainID)
		if isMain {
			errText := ev.ErrorText
			// Map tunnel/proxy errors
			if strings.Contains(errText, "ERR_TUNNEL_CONNECTION_FAILED") || strings.Contains(errText, "ERR_PROXY_CONNECTION_FAILED") {
				errText = errText + " not reachable from Chrome mode — open externally"
			}
			t.manager.emitNav(NavEvent{StateKey: t.stateKey, Error: errText, Status: 0})
		}
		// Network telemetry with blocked
		blocked := ""
		if strings.Contains(ev.ErrorText, "ERR_TUNNEL_CONNECTION_FAILED") || ev.BlockedReason != "" {
			// Map tunnel/proxy block to private address
			if strings.Contains(ev.ErrorText, "ERR_TUNNEL") || strings.Contains(ev.ErrorText, "ERR_PROXY") {
				blocked = "private address"
			} else if ev.BlockedReason != "" {
				blocked = ev.BlockedReason
			}
		}
		t.mu.Lock()
		sink := t.sink
		t.mu.Unlock()
		if sink != nil {
			sink.Network(NetworkEvent{
				Status:  0,
				Blocked: blocked,
				TS:      time.Now().UnixMilli(),
			})
		}
	}
}

func (t *Target) handleRequestWillBeSent(ch <-chan json.RawMessage) {
	for raw := range ch {
		var ev struct {
			RequestID string `json:"requestId"`
			Request   struct {
				URL    string `json:"url"`
				Method string `json:"method"`
			} `json:"request"`
		}
		_ = json.Unmarshal(raw, &ev)
		t.reqMu.Lock()
		t.reqStart[ev.RequestID] = time.Now()
		t.reqMu.Unlock()
		t.mu.Lock()
		sink := t.sink
		t.mu.Unlock()
		if sink != nil {
			sink.Network(NetworkEvent{
				Method: ev.Request.Method,
				URL:    ev.Request.URL,
				TS:     time.Now().UnixMilli(),
			})
		}
	}
}

func (t *Target) handleConsole(ch <-chan json.RawMessage) {
	for raw := range ch {
		var ev struct {
			Type string `json:"type"`
			Args []struct {
				Value       *string `json:"value"`
				Description *string `json:"description"`
			} `json:"args"`
			Timestamp float64 `json:"timestamp"`
		}
		_ = json.Unmarshal(raw, &ev)
		level := ev.Type
		switch level {
		case "warning":
			level = "warn"
		case "log", "info", "error", "debug", "warn":
		default:
			level = "log"
		}
		args := make([]string, 0, len(ev.Args))
		for _, a := range ev.Args {
			if a.Value != nil {
				args = append(args, *a.Value)
			} else if a.Description != nil {
				args = append(args, *a.Description)
			}
		}
		t.mu.Lock()
		sink := t.sink
		t.mu.Unlock()
		if sink != nil {
			sink.Console(ConsoleEvent{Level: level, Args: args, TS: int64(ev.Timestamp)})
		}
	}
}

func (t *Target) handleException(ch <-chan json.RawMessage) {
	for raw := range ch {
		var ev struct {
			ExceptionDetails struct {
				Text      string `json:"text"`
				Exception *struct {
					Description string `json:"description"`
				} `json:"exception"`
				URL        string `json:"url"`
				LineNumber int    `json:"lineNumber"`
			} `json:"exceptionDetails"`
			Timestamp float64 `json:"timestamp"`
		}
		_ = json.Unmarshal(raw, &ev)
		text := ev.ExceptionDetails.Text
		if ev.ExceptionDetails.Exception != nil && ev.ExceptionDetails.Exception.Description != "" {
			text = ev.ExceptionDetails.Exception.Description
		}
		if ev.ExceptionDetails.URL != "" {
			text = fmt.Sprintf("%s (%s:%d)", text, ev.ExceptionDetails.URL, ev.ExceptionDetails.LineNumber)
		}
		t.mu.Lock()
		sink := t.sink
		t.mu.Unlock()
		if sink != nil {
			sink.Console(ConsoleEvent{Level: "error", Args: []string{text}, TS: int64(ev.Timestamp)})
		}
	}
}

// Navigation and input methods

func (t *Target) Navigate(ctx context.Context, url string) error {
	if !isHTTPSScheme(url) {
		return ErrBadScheme
	}
	err := t.conn.Call(ctx, t.sessionID, "Page.navigate", map[string]string{"url": url}, nil)
	if err != nil {
		return err
	}
	t.manager.emitNav(NavEvent{StateKey: t.stateKey, URL: url, Status: 0})
	return nil
}

func (t *Target) Back(ctx context.Context) error {
	return t.navHistory(ctx, -1)
}
func (t *Target) Forward(ctx context.Context) error {
	return t.navHistory(ctx, 1)
}
func (t *Target) navHistory(ctx context.Context, delta int) error {
	var res struct {
		CurrentIndex int `json:"currentIndex"`
		Entries []struct {
			ID  int    `json:"id"`
			URL string `json:"url"`
		} `json:"entries"`
	}
	if err := t.conn.Call(ctx, t.sessionID, "Page.getNavigationHistory", nil, &res); err != nil {
		return err
	}
	targetIdx := res.CurrentIndex + delta
	if targetIdx < 0 || targetIdx >= len(res.Entries) {
		return nil
	}
	return t.conn.Call(ctx, t.sessionID, "Page.navigateToHistoryEntry", map[string]int{"entryId": res.Entries[targetIdx].ID}, nil)
}

func (t *Target) Reload(ctx context.Context) error {
	return t.conn.Call(ctx, t.sessionID, "Page.reload", nil, nil)
}

func (t *Target) Resize(ctx context.Context, w, h int, dpr float64) error {
	t.mu.Lock()
	t.vpW = w
	t.vpH = h
	t.vpDPR = dpr
	t.mu.Unlock()
	maxW := int(float64(w) * dpr)
	maxH := int(float64(h) * dpr)
	if maxW == 0 {
		maxW = w
	}
	if maxH == 0 {
		maxH = h
	}
	if err := t.conn.Call(ctx, t.sessionID, "Emulation.setDeviceMetricsOverride", map[string]any{
		"width": w, "height": h, "deviceScaleFactor": dpr, "mobile": false,
	}, nil); err != nil {
		return err
	}
	return t.restartScreencastWith(ctx, maxW, maxH)
}

func (t *Target) Mouse(ctx context.Context, ev MouseEvent) error {
	var typ string
	switch ev.Kind {
	case "move":
		typ = "mouseMoved"
	case "down":
		typ = "mousePressed"
	case "up":
		typ = "mouseReleased"
	case "wheel":
		typ = "mouseWheel"
	default:
		typ = ev.Kind
	}
	params := map[string]any{
		"type": typ, "x": ev.X, "y": ev.Y, "button": ev.Button, "clickCount": ev.ClickCount, "modifiers": ev.Modifiers,
	}
	if ev.Kind == "wheel" {
		params["deltaX"] = ev.DeltaX
		params["deltaY"] = ev.DeltaY
	}
	return t.conn.Call(ctx, t.sessionID, "Input.dispatchMouseEvent", params, nil)
}

func (t *Target) Key(ctx context.Context, ev KeyEvent) error {
	var typ string
	switch ev.Kind {
	case "down":
		typ = "keyDown"
	case "up":
		typ = "keyUp"
	case "char":
		typ = "char"
	default:
		typ = ev.Kind
	}
	return t.conn.Call(ctx, t.sessionID, "Input.dispatchKeyEvent", map[string]any{
		"type": typ, "key": ev.Key, "code": ev.Code, "text": ev.Text, "modifiers": ev.Modifiers,
	}, nil)
}

func (t *Target) Detach() {
	t.mu.Lock()
	sink := t.sink
	t.sink = nil
	t.mu.Unlock()
	// Stop screencast if we had a sink
	if sink != nil {
		_ = t.conn.Call(context.Background(), t.sessionID, "Page.stopScreencast", nil, nil)
	}
	for _, fn := range t.cancels {
		fn()
	}
	t.cancels = nil
}

func (t *Target) restartScreencast(ctx context.Context) error {
	t.mu.Lock()
	w, h, dpr := t.vpW, t.vpH, t.vpDPR
	t.mu.Unlock()
	maxW, maxH := 1280, 800
	if w > 0 && h > 0 {
		if dpr > 0 {
			maxW = int(float64(w) * dpr)
			maxH = int(float64(h) * dpr)
		} else {
			maxW = w
			maxH = h
		}
	}
	return t.restartScreencastWith(ctx, maxW, maxH)
}

func (t *Target) restartScreencastWith(ctx context.Context, maxW, maxH int) error {
	_ = t.conn.Call(ctx, t.sessionID, "Page.stopScreencast", nil, nil)
	return t.conn.Call(ctx, t.sessionID, "Page.startScreencast", map[string]any{
		"format": "jpeg", "quality": 70, "maxWidth": maxW, "maxHeight": maxH, "everyNthFrame": 1,
	}, nil)
}
