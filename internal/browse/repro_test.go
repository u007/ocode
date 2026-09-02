//go:build repro

package browse

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/u007/ocode/internal/tool"
)

// TestReproCDPWSLoadsForever reproduces "chrome cdp on browser tab loads
// forever, nothing" against the real path: real browse.Server, real Chrome,
// real WS upgrade. It asserts a nav event to the upstream with status 200
// plus at least one screencast frame AFTER that navigation — proof the
// pipeline renders, not just that the socket opened. Gated on
// OCODE_CHROME_PATH like the cdp integration suite.
func TestReproCDPWSLoadsForever(t *testing.T) {
	chromePath := os.Getenv("OCODE_CHROME_PATH")
	if chromePath == "" {
		chromePath = "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
	}
	if _, err := os.Stat(chromePath); err != nil {
		t.Skipf("chrome not found at %s (set OCODE_CHROME_PATH)", chromePath)
	}

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, "<html><body><h1>hello repro</h1></body></html>")
	}))
	defer upstream.Close()
	t.Logf("upstream at %s", upstream.URL)

	sup := tool.NewProcessSupervisor(tool.ProcessSupervisorOptions{})
	defer sup.Shutdown(context.Background())

	bs := New("tok", nil, Options{
		ChromePath: chromePath,
		Supervisor: sup,
	})
	defer bs.Close(context.Background())

	// Capture nav events as they would flow on the SSE bus.
	var (
		navMu  sync.Mutex
		navEvs []NavEvent
	)
	bs.SetNavPublisher(func(_ string, ev NavEvent) {
		navMu.Lock()
		navEvs = append(navEvs, ev)
		navMu.Unlock()
	})

	ln, base, err := bs.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	go func() { _ = http.Serve(ln, bs.Handler()) }()
	t.Logf("browse origin at %s", base)

	spaOrigin := "http://127.0.0.1:4096"
	key := "tab:repro"
	grant := bs.MintGrant(key, spaOrigin)
	wsURL := "ws" + strings.TrimPrefix(base, "http") + "/b/" + key + "/__cdp?__grant=" + grant

	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	conn, resp, err := dialer.Dial(wsURL, http.Header{"Origin": []string{spaOrigin}})
	if err != nil {
		if resp != nil {
			t.Fatalf("dial: %v (http %d)", err, resp.StatusCode)
		}
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// Mimic ChromeViewport mount: nav then resize (in wire order).
	if err := conn.WriteJSON(map[string]any{"t": "nav", "url": upstream.URL}); err != nil {
		t.Fatalf("write nav: %v", err)
	}
	if err := conn.WriteJSON(map[string]any{"t": "resize", "w": 1280, "h": 800, "dpr": 2}); err != nil {
		t.Fatalf("write resize: %v", err)
	}
	start := time.Now()

	// errorsAs is errors.As (kept explicit for the build-tagged test).
	errorsAs := func(err error, target any) bool { return errors.As(err, target) }

	deadline := time.Now().Add(20 * time.Second)
	frames := 0
	for time.Now().Before(deadline) {
		_ = conn.SetReadDeadline(time.Now().Add(15 * time.Second))
		mt, data, err := conn.ReadMessage()
		if err != nil {
			var ce *websocket.CloseError
			if ok := errorsAs(err, &ce); ok {
				t.Logf("WS CLOSED at %.1fs: code=%d text=%q", time.Since(start).Seconds(), ce.Code, ce.Text)
			} else {
				t.Logf("WS read error at %.1fs: %v", time.Since(start).Seconds(), err)
			}
			break // connection is done; no further reads
		}
		if mt == websocket.BinaryMessage {
			if len(data) < 8 {
				t.Fatalf("frame too short: %d bytes", len(data))
			}
			frames++
			t.Logf("frame #%d (%d bytes: %dx%d)", frames, len(data), uint32(data[0])<<24|uint32(data[1])<<16|uint32(data[2])<<8|uint32(data[3]), uint32(data[4])<<24|uint32(data[5])<<16|uint32(data[6])<<8|uint32(data[7]))
			continue
		}
		var m map[string]any
		if json.Unmarshal(data, &m) == nil && m["t"] == "error" {
			t.Fatalf("server error: %v", m["message"])
		}
		t.Logf("text msg: %s", string(data))
	}

	// The page must have actually loaded: a nav event to the upstream URL
	// with status 200 (Network.responseReceived for the Document).
	navMu.Lock()
	evs := append([]NavEvent(nil), navEvs...)
	navMu.Unlock()
	got200 := false
	for _, ev := range evs {
		t.Logf("nav event: %+v", ev)
		if ev.URL == upstream.URL+"/" && ev.Status == 200 {
			got200 = true
		}
	}
	if !got200 {
		t.Fatalf("upstream %s never loaded (status 200 nav missing); events: %+v", upstream.URL, evs)
	}
	if frames == 0 {
		t.Fatalf("no screencast frame after successful nav")
	}
	t.Logf("OK: nav 200 + %d frames", frames)
}
