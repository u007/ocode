package cdp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
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

	// proxy credentials for Fetch.authRequired (Chrome does not honor userinfo
	// in Target.createBrowserContext proxyServer).
	proxyUser string
	proxyPass string

	// request timing for Network telemetry
	reqStart map[string]time.Time
	reqMu    sync.Mutex

	// pending requests for correlation: requestID → in-flight details
	pendingReqs map[string]*pendingReq
	pendingMu   sync.Mutex

	// completed requests for on-demand body lookup: requestID → completed metadata
	completedReqs map[string]*completedReq
	completedMu   sync.Mutex

	// performance metrics from CDP Performance.metrics events
	perfMetrics map[string]float64
	perfMu      sync.Mutex

	// handler cancels
	cancels []func()

	// navHost is the canonical host:port of the current top-level page,
	// registered with the egress proxy (AllowHost) so a user-chosen
	// loopback/LAN dev server is dialable in Chrome mode. Empty when none.
	navHost string

	// chooser is the intercepted <input type=file> waiting for files (see
	// startFileChooser / SetFiles). nil when none is pending.
	chooserMu sync.Mutex
	chooser   *pendingChooser
}

// pendingChooser is one intercepted Page.fileChooserOpened.
type pendingChooser struct {
	backendNodeID int
	multiple      bool
}

// pendingReq tracks an in-flight request for correlation.
type pendingReq struct {
	Method          string
	URL             string
	RequestHeaders  map[string]string
	PostData        string // bounded request body from requestWillBeSent
	StartTime       time.Time
	ResponseStatus  int
	ResponseHeaders map[string]string
	ContentType     string
	Blocked         string
}

// completedReq holds metadata after loadingFinished for on-demand body lookup.
type completedReq struct {
	Method          string
	URL             string
	RequestHeaders  map[string]string
	PostData        string
	ResponseStatus  int
	ResponseHeaders map[string]string
	ContentType     string
	Size            int64
}

// maxPostDataLen is the maximum characters to capture from request postData.
const maxPostDataLen = 64 * 1024

// maxResponseBodyLen is the maximum characters to fetch for response bodies.
const maxResponseBodyLen = 256 * 1024

// maxCompletedReqs is the maximum number of completed requests to retain for body lookup.
const maxCompletedReqs = 200

// maxHeaderCount is the maximum number of headers to forward per side.
const maxHeaderCount = 20

// maxHeaderValueLen is the maximum character length for a single header value.
const maxHeaderValueLen = 200

// sensitiveHeaderPrefixes are header names (lowercased) that must be redacted.
var sensitiveHeaderPrefixes = []string{
	"authorization", "proxy-authorization", "cookie", "set-cookie",
	"x-api-key", "x-auth-token", "x-csrf-token",
}

// redactHeaders copies headers, capping count/value length and redacting
// sensitive entries. Returns nil if input is nil.
func redactHeaders(h map[string]string) map[string]string {
	if h == nil {
		return nil
	}
	out := make(map[string]string, min(len(h), maxHeaderCount))
	n := 0
	for k, v := range h {
		if n >= maxHeaderCount {
			break
		}
		lk := strings.ToLower(k)
		redact := false
		for _, prefix := range sensitiveHeaderPrefixes {
			if lk == prefix || strings.HasPrefix(lk, prefix+"-") {
				redact = true
				break
			}
		}
		if redact {
			out[k] = "[redacted]"
		} else if len(v) > maxHeaderValueLen {
			out[k] = v[:maxHeaderValueLen] + "…"
		} else {
			out[k] = v
		}
		n++
	}
	return out
}

// setTopLevelHost records rawURL's host as the target's top-level page:
// swaps the egress AllowHost registration and toggles Chrome's certificate
// check — ignored only while the page is on a loopback host (self-signed dev
// certs, parity with local mode's auto-allow), enforced everywhere else.
func (t *Target) setTopLevelHost(ctx context.Context, rawURL string) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Hostname() == "" {
		return
	}
	port := u.Port()
	if port == "" {
		port = "80"
		if u.Scheme == "https" {
			port = "443"
		}
	}
	hostport := net.JoinHostPort(strings.ToLower(u.Hostname()), port)

	t.mu.Lock()
	prev := t.navHost
	t.navHost = hostport
	t.mu.Unlock()
	if prev == hostport {
		return
	}
	t.manager.mu.Lock()
	proxy := t.manager.proxy
	t.manager.mu.Unlock()
	if proxy != nil {
		if prev != "" {
			proxy.ReleaseHost(prev)
		}
		proxy.AllowHost(hostport)
	}
	if err := t.conn.Call(ctx, t.sessionID, "Security.setIgnoreCertificateErrors",
		map[string]bool{"ignore": isLoopbackHostname(u.Hostname())}, nil); err != nil && t.manager.opts.Log != nil {
		t.manager.opts.Log.Printf("browse cdp: Security.setIgnoreCertificateErrors for %s: %v", hostport, err)
	}
}

// releaseTopLevelHost drops the egress registration when the target goes away.
func (t *Target) releaseTopLevelHost() {
	t.mu.Lock()
	prev := t.navHost
	t.navHost = ""
	t.mu.Unlock()
	if prev == "" {
		return
	}
	t.manager.mu.Lock()
	proxy := t.manager.proxy
	t.manager.mu.Unlock()
	if proxy != nil {
		proxy.ReleaseHost(prev)
	}
}

// isLoopbackHostname: "localhost", "*.localhost", or a loopback IP literal.
func isLoopbackHostname(h string) bool {
	h = strings.ToLower(strings.TrimSuffix(h, "."))
	if h == "localhost" || strings.HasSuffix(h, ".localhost") {
		return true
	}
	ip, err := netip.ParseAddr(strings.Trim(h, "[]"))
	return err == nil && ip.IsLoopback()
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
	chAuth, c9 := t.conn.Subscribe(t.sessionID, "Fetch.authRequired")
	chPaused, c10 := t.conn.Subscribe(t.sessionID, "Fetch.requestPaused")
	chFinished, c11 := t.conn.Subscribe(t.sessionID, "Network.loadingFinished")
	chPerf, c12 := t.conn.Subscribe(t.sessionID, "Performance.metrics")
	t.cancels = append(t.cancels, c1, c2, c3, c4, c5, c6, c7, c8, c9, c10, c11, c12)
	t.reqStart = make(map[string]time.Time)
	t.pendingReqs = make(map[string]*pendingReq)
	t.completedReqs = make(map[string]*completedReq)
	t.perfMetrics = make(map[string]float64)

	go t.handleScreencast(chFrame)
	go t.handleFrameNavigated(chNav)
	go t.handleNavigatedWithinDocument(chWithin)
	go t.handleResponseReceived(chResp)
	go t.handleLoadingFailed(chFail)
	go t.handleRequestWillBeSent(chReq)
	go t.handleLoadingFinished(chFinished)
	go t.handlePerformanceMetrics(chPerf)
	go t.handleConsole(chConsole)
	go t.handleException(chExc)
	go t.handleAuthRequired(chAuth)
	go t.handleRequestPaused(chPaused)

	// Enable Fetch auth handling (proxy 407). Must be after the subscriptions
	// above so no auth challenge is missed before the handler is live. Also
	// handle requestPaused for any patterns Chrome may emit (the handler just
	// continues the request).
	_ = t.conn.Call(context.Background(), t.sessionID, "Fetch.enable", map[string]any{"handleAuthRequests": true}, nil)

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
			// Check scheme. Allow Chrome's internal navigation-error page
			// (chrome-error://chromewebdata/) through: it is what Chrome renders
			// when a load fails (proxy/TLS/DNS error). The real error is already
			// emitted via Network.loadingFailed; blanking the page here would
			// both hide that error and misreport it as "unsupported URL scheme".
			if isHTTPSScheme(ev.Frame.URL) {
				// Redirect / link click / history: Chrome moved the top-level
				// page itself; keep the egress + cert policy in step.
				t.setTopLevelHost(context.Background(), ev.Frame.URL)
			}
			if ev.Frame.URL != "" && !isHTTPSScheme(ev.Frame.URL) && !strings.HasPrefix(ev.Frame.URL, "about:") && !strings.HasPrefix(ev.Frame.URL, "chrome-error://") {
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
				URL     string            `json:"url"`
				Status  int               `json:"status"`
				Headers map[string]string `json:"headers"`
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
		// An egress-proxy policy 403 identifies itself via X-Ocode-Blocked
		blocked := ""
		for k, v := range ev.Response.Headers {
			if strings.EqualFold(k, "X-Ocode-Blocked") {
				blocked = v
				break
			}
		}
		// Correlate: store response details in pending req.
		t.pendingMu.Lock()
		if pr, ok := t.pendingReqs[ev.RequestID]; ok {
			pr.ResponseStatus = ev.Response.Status
			pr.ResponseHeaders = redactHeaders(ev.Response.Headers)
			pr.Blocked = blocked
			for k, v := range ev.Response.Headers {
				if strings.EqualFold(k, "content-type") {
					pr.ContentType = v
					break
				}
			}
		}
		t.pendingMu.Unlock()
	}
}

func (t *Target) handleLoadingFailed(ch <-chan json.RawMessage) {
	for raw := range ch {
		var ev struct {
			RequestID     string `json:"requestId"`
			Type          string `json:"type"`
			FrameID       string `json:"frameId"`
			ErrorText     string `json:"errorText"`
			BlockedReason string `json:"blockedReason"`
			Canceled      bool   `json:"canceled"`
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
			if strings.Contains(ev.ErrorText, "ERR_TUNNEL") || strings.Contains(ev.ErrorText, "ERR_PROXY") {
				blocked = "private address"
			} else if ev.BlockedReason != "" {
				blocked = ev.BlockedReason
			}
		}
		// Correlate: emit failed request from pending details.
		t.pendingMu.Lock()
		pr, ok := t.pendingReqs[ev.RequestID]
		if ok {
			delete(t.pendingReqs, ev.RequestID)
		}
		t.pendingMu.Unlock()
		t.reqMu.Lock()
		start := t.reqStart[ev.RequestID]
		delete(t.reqStart, ev.RequestID)
		t.reqMu.Unlock()

		var dur int64
		if !start.IsZero() {
			dur = time.Since(start).Milliseconds()
		}
		t.mu.Lock()
		sink := t.sink
		t.mu.Unlock()
		if sink != nil {
			ne := NetworkEvent{
				Status:  0,
				TS:      time.Now().UnixMilli(),
				Blocked: blocked,
			}
			if pr != nil {
				ne.RequestID = ev.RequestID
				ne.Method = pr.Method
				ne.URL = pr.URL
				ne.DurationMs = dur
				ne.RequestHeaders = pr.RequestHeaders
			}
			sink.Network(ne)
		}
	}
}

func (t *Target) handleRequestWillBeSent(ch <-chan json.RawMessage) {
	for raw := range ch {
		var ev struct {
			RequestID string `json:"requestId"`
			Request   struct {
				URL      string            `json:"url"`
				Method   string            `json:"method"`
				Headers  map[string]string `json:"headers"`
				PostData string            `json:"postData"`
			} `json:"request"`
		}
		_ = json.Unmarshal(raw, &ev)
		now := time.Now()
		t.reqMu.Lock()
		t.reqStart[ev.RequestID] = now
		t.reqMu.Unlock()
		// Capture bounded request body.
		postData := ev.Request.PostData
		if len(postData) > maxPostDataLen {
			postData = postData[:maxPostDataLen] + "…"
		}
		// Store request details for correlation.
		t.pendingMu.Lock()
		t.pendingReqs[ev.RequestID] = &pendingReq{
			Method:         ev.Request.Method,
			URL:            ev.Request.URL,
			RequestHeaders: redactHeaders(ev.Request.Headers),
			PostData:       postData,
			StartTime:      now,
		}
		// Evict stale entries (>60s old) to bound memory.
		if len(t.pendingReqs) > 200 {
			for id, r := range t.pendingReqs {
				if now.Sub(r.StartTime) > 60*time.Second {
					delete(t.pendingReqs, id)
				}
			}
		}
		t.pendingMu.Unlock()
	}
}

func (t *Target) handleLoadingFinished(ch <-chan json.RawMessage) {
	for raw := range ch {
		var ev struct {
			RequestID         string  `json:"requestId"`
			Timestamp         float64 `json:"timestamp"`
			EncodedDataLength int64   `json:"encodedDataLength"`
		}
		_ = json.Unmarshal(raw, &ev)
		// Correlate: emit completed request from pending details.
		t.pendingMu.Lock()
		pr, ok := t.pendingReqs[ev.RequestID]
		if ok {
			delete(t.pendingReqs, ev.RequestID)
		}
		t.pendingMu.Unlock()
		t.reqMu.Lock()
		start := t.reqStart[ev.RequestID]
		delete(t.reqStart, ev.RequestID)
		t.reqMu.Unlock()

		var dur int64
		if !start.IsZero() {
			dur = time.Since(start).Milliseconds()
		}
		t.mu.Lock()
		sink := t.sink
		t.mu.Unlock()
		if sink != nil {
			ne := NetworkEvent{
				TS:   time.Now().UnixMilli(),
				Size: ev.EncodedDataLength,
			}
			if pr != nil {
				ne.RequestID = ev.RequestID
				ne.Method = pr.Method
				ne.URL = pr.URL
				ne.DurationMs = dur
				ne.RequestHeaders = pr.RequestHeaders
				ne.Status = pr.ResponseStatus
				ne.ResponseHeaders = pr.ResponseHeaders
				ne.ContentType = pr.ContentType
				ne.Blocked = pr.Blocked
				ne.PostData = pr.PostData
				// Store in completed cache for on-demand body lookup.
				t.completedMu.Lock()
				t.completedReqs[ev.RequestID] = &completedReq{
					Method:          pr.Method,
					URL:             pr.URL,
					RequestHeaders:  pr.RequestHeaders,
					PostData:        pr.PostData,
					ResponseStatus:  pr.ResponseStatus,
					ResponseHeaders: pr.ResponseHeaders,
					ContentType:     pr.ContentType,
					Size:            ev.EncodedDataLength,
				}
				// Evict oldest if over capacity.
				if len(t.completedReqs) > maxCompletedReqs {
					// Simple eviction: remove first entry (map iteration is random).
					for id := range t.completedReqs {
						delete(t.completedReqs, id)
						break
					}
				}
				t.completedMu.Unlock()
			} else {
				ne.RequestID = ev.RequestID
			}
			sink.Network(ne)
		}
	}
}

// GetResponseBody fetches the response body for a completed request on demand.
// Returns the body text, whether it's base64-encoded, and whether it was truncated.
// Must be called from the target's goroutine context (has access to conn).
func (t *Target) GetResponseBody(ctx context.Context, requestID string) (body string, isBase64 bool, truncated bool, err error) {
	// Check completed cache first.
	t.completedMu.Lock()
	cr, ok := t.completedReqs[requestID]
	t.completedMu.Unlock()
	if !ok {
		return "", false, false, fmt.Errorf("request %s not found or expired", requestID)
	}
	// Skip body fetch for non-text content types.
	ct := cr.ContentType
	if strings.HasPrefix(ct, "image/") || strings.HasPrefix(ct, "font/") ||
		strings.HasPrefix(ct, "video/") || strings.HasPrefix(ct, "audio/") ||
		strings.HasPrefix(ct, "application/octet-stream") {
		return "", false, false, nil
	}
	// Fetch via CDP.
	t.mu.Lock()
	conn := t.conn
	sid := t.sessionID
	t.mu.Unlock()
	if conn == nil {
		return "", false, false, fmt.Errorf("no CDP connection")
	}
	var result struct {
		Body          string `json:"body"`
		Base64Encoded bool   `json:"base64Encoded"`
	}
	err = conn.Call(ctx, sid, "Network.getResponseBody", map[string]any{
		"requestId": requestID,
	}, &result)
	if err != nil {
		return "", false, false, fmt.Errorf("getResponseBody: %w", err)
	}
	body = result.Body
	isBase64 = result.Base64Encoded
	if len(body) > maxResponseBodyLen {
		body = body[:maxResponseBodyLen] + "…"
		truncated = true
	}
	return body, isBase64, truncated, nil
}

// handlePerformanceMetrics processes CDP Performance.metrics events.
func (t *Target) handlePerformanceMetrics(ch <-chan json.RawMessage) {
	for raw := range ch {
		var ev struct {
			Metrics []struct {
				Name  string  `json:"name"`
				Value float64 `json:"value"`
			} `json:"metrics"`
		}
		_ = json.Unmarshal(raw, &ev)
		t.perfMu.Lock()
		for _, m := range ev.Metrics {
			t.perfMetrics[m.Name] = m.Value
		}
		t.perfMu.Unlock()
		// Forward to sink for frontend consumption.
		t.mu.Lock()
		sink := t.sink
		t.mu.Unlock()
		if sink != nil {
			sink.Performance(t.getPerformanceSnapshot())
		}
	}
}

// getPerformanceSnapshot returns a copy of the current performance metrics.
func (t *Target) getPerformanceSnapshot() map[string]float64 {
	t.perfMu.Lock()
	defer t.perfMu.Unlock()
	snap := make(map[string]float64, len(t.perfMetrics))
	for k, v := range t.perfMetrics {
		snap[k] = v
	}
	return snap
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

func (t *Target) handleAuthRequired(ch <-chan json.RawMessage) {
	for raw := range ch {
		var ev struct {
			RequestID     string `json:"requestId"`
			AuthChallenge struct {
				Source string `json:"source"` // "Server" | "Proxy"
			} `json:"authChallenge"`
		}
		_ = json.Unmarshal(raw, &ev)
		if ev.AuthChallenge.Source == "Proxy" && t.proxyUser != "" {
			_ = t.conn.Call(context.Background(), t.sessionID, "Fetch.continueWithAuth", map[string]any{
				"requestId": ev.RequestID,
				"authChallengeResponse": map[string]string{
					"response": "ProvideCredentials",
					"username": t.proxyUser,
					"password": t.proxyPass,
				},
			}, nil)
		} else {
			_ = t.conn.Call(context.Background(), t.sessionID, "Fetch.continueWithAuth", map[string]any{
				"requestId":             ev.RequestID,
				"authChallengeResponse": map[string]string{"response": "Default"},
			}, nil)
		}
	}
}

func (t *Target) handleRequestPaused(ch <-chan json.RawMessage) {
	for raw := range ch {
		var ev struct {
			RequestID string `json:"requestId"`
		}
		_ = json.Unmarshal(raw, &ev)
		_ = t.conn.Call(context.Background(), t.sessionID, "Fetch.continueRequest", map[string]string{"requestId": ev.RequestID}, nil)
	}
}

// Navigation and input methods

func (t *Target) Navigate(ctx context.Context, url string) error {
	if !isHTTPSScheme(url) {
		return ErrBadScheme
	}
	t.setTopLevelHost(ctx, url)
	// Emit "loading" BEFORE the call: Chrome answers Page.navigate only once
	// the navigation commits, which can be after Network.responseReceived has
	// already emitted the document status. Emitting afterwards would land the
	// Status 0 last and pin the SPA on loading=true forever.
	t.manager.emitNav(NavEvent{StateKey: t.stateKey, URL: url, Status: 0})
	if err := t.conn.Call(ctx, t.sessionID, "Page.navigate", map[string]string{"url": url}, nil); err != nil {
		t.manager.emitNav(NavEvent{StateKey: t.stateKey, URL: url, Error: err.Error()})
		return err
	}
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
		Entries      []struct {
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
	if sink != nil {
		_ = t.conn.Call(context.Background(), t.sessionID, "Page.stopScreencast", nil, nil)
	}
}

// DetachSink clears the target's sink only if it is still sink, so a socket
// that reconnected and replaced the sink (Attach reuses the target for the
// same stateKey) is not silently blanked when the earlier socket's cleanup
// runs after the replacement has already taken over.
func (t *Target) DetachSink(sink FrameSink) {
	t.mu.Lock()
	if t.sink != sink {
		t.mu.Unlock()
		return
	}
	t.sink = nil
	t.mu.Unlock()
	_ = t.conn.Call(context.Background(), t.sessionID, "Page.stopScreencast", nil, nil)
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

// FileChooserSink is implemented by sinks that can ask the user for files
// when the page opens a file chooser. Optional: sinks without it simply
// leave the chooser pending (the page's input never fires change).
type FileChooserSink interface {
	FileChooser(multiple bool)
}

// ErrNoFileChooser is returned by SetFiles when the page has no file chooser
// waiting — the user picked files after the page moved on, or nothing asked.
var ErrNoFileChooser = errors.New("no file chooser pending")

// startFileChooser makes Chrome report file-chooser opens as events instead
// of showing a native dialog (the screencast has no UI for one) and forwards
// them to the sink so the SPA can open its own picker.
func (t *Target) startFileChooser() {
	ch, cancel := t.conn.Subscribe(t.sessionID, "Page.fileChooserOpened")
	t.cancels = append(t.cancels, cancel)
	go t.handleFileChooserOpened(ch)
	if err := t.conn.Call(context.Background(), t.sessionID, "Page.setInterceptFileChooserDialog",
		map[string]bool{"enabled": true}, nil); err != nil && t.manager.opts.Log != nil {
		t.manager.opts.Log.Printf("browse cdp: Page.setInterceptFileChooserDialog: %v", err)
	}
}

func (t *Target) handleFileChooserOpened(ch <-chan json.RawMessage) {
	for raw := range ch {
		var ev struct {
			Mode          string `json:"mode"` // selectSingle | selectMultiple
			BackendNodeID int    `json:"backendNodeId"`
		}
		if err := json.Unmarshal(raw, &ev); err != nil || ev.BackendNodeID == 0 {
			if t.manager.opts.Log != nil {
				t.manager.opts.Log.Printf("browse cdp: Page.fileChooserOpened without backendNodeId: %s (err=%v)", raw, err)
			}
			continue
		}
		multiple := ev.Mode == "selectMultiple"
		t.chooserMu.Lock()
		t.chooser = &pendingChooser{backendNodeID: ev.BackendNodeID, multiple: multiple}
		t.chooserMu.Unlock()
		t.mu.Lock()
		sink := t.sink
		t.mu.Unlock()
		if fs, ok := sink.(FileChooserSink); ok {
			fs.FileChooser(multiple)
		}
	}
}

// SetFiles answers the pending file chooser with local paths (DOM.
// setFileInputFiles fires the input's change event). An empty paths means
// the user cancelled: the chooser is dropped and the input left empty.
func (t *Target) SetFiles(ctx context.Context, paths []string) error {
	t.chooserMu.Lock()
	pc := t.chooser
	t.chooser = nil
	t.chooserMu.Unlock()
	if pc == nil {
		return ErrNoFileChooser
	}
	if len(paths) == 0 {
		return nil
	}
	if !pc.multiple && len(paths) > 1 {
		paths = paths[:1]
	}
	return t.conn.Call(ctx, t.sessionID, "DOM.setFileInputFiles", map[string]any{
		"files": paths, "backendNodeId": pc.backendNodeID,
	}, nil)
}
