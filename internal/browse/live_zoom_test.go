package browse

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/u007/ocode/internal/browse/cdp"
	"github.com/u007/ocode/internal/tool"
)

// TestCDP_InputScalingLiveAtZoom drives real Chrome through the per-tab CDP
// socket: the page is laid out at viewport/zoom, the click is sent in canvas
// px, and the page reports where it saw the click. Live only: set
// OCODE_CHROME_PATH (same gate as the server's browser live tests).
func TestCDP_InputScalingLiveAtZoom(t *testing.T) {
	chromePath := os.Getenv("OCODE_CHROME_PATH")
	if chromePath == "" {
		t.Skip("OCODE_CHROME_PATH not set; skipping real-Chrome input-scaling test")
	}
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><body style="margin:0">
<button id="b" style="position:absolute;left:100px;top:100px;width:120px;height:40px"
 onclick="console.log('HIT '+event.clientX+','+event.clientY)">x</button>
<script>console.log('READY')</script></body>`)
	}))
	t.Cleanup(page.Close)

	mgr := cdp.NewManager(cdp.ManagerOptions{
		ChromePath:  chromePath,
		IdleTimeout: 2 * time.Minute,
		Supervisor:  tool.NewProcessSupervisor(tool.ProcessSupervisorOptions{GracePeriod: 2 * time.Second}),
	})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := mgr.Close(ctx); err != nil {
			t.Logf("closing headless Chrome: %v", err)
		}
	})

	s := New("tok", nil)
	s.SetSPAOrigin("http://example.com")
	s.SetCDPManager(&realManagerAdapter{mgr})
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)

	grant := s.MintGrant("tab:live", "http://example.com")
	conn, _, err := (&websocket.Dialer{}).Dial(wsURL(ts, "tab:live", grant),
		http.Header{"Origin": []string{"http://example.com"}})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	send := func(v any) {
		t.Helper()
		b, _ := json.Marshal(v)
		if err := conn.WriteMessage(websocket.TextMessage, b); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	frames := make(chan []byte, 128)
	go func() {
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				close(frames)
				return
			}
			select {
			case frames <- data:
			default:
			}
		}
	}()
	waitConsole := func(want string, timeout time.Duration) (string, bool) {
		t.Helper()
		deadline := time.After(timeout)
		for {
			select {
			case data, ok := <-frames:
				if !ok {
					return "", false
				}
				var msg struct {
					T    string `json:"t"`
					Args []any  `json:"args"`
				}
				if json.Unmarshal(data, &msg) != nil || msg.T != "console" {
					continue
				}
				text := strings.Join(anyStrings(msg.Args), " ")
				if strings.Contains(text, want) {
					return text, true
				}
			case <-deadline:
				return "", false
			}
		}
	}

	send(map[string]any{"t": "resize", "w": 800, "h": 600, "dpr": 1})
	send(map[string]any{"t": "zoom", "factor": 1.5})
	send(map[string]any{"t": "nav", "url": page.URL})
	if _, ok := waitConsole("READY", 20*time.Second); !ok {
		t.Fatal("page never loaded")
	}

	// Canvas px for the button centre (layout 160,120) at zoom 1.5.
	send(map[string]any{"t": "mouse", "kind": "move", "x": 240, "y": 180, "button": "none"})
	send(map[string]any{"t": "mouse", "kind": "down", "x": 240, "y": 180, "button": "left", "buttons": 1, "clickCount": 1})
	send(map[string]any{"t": "mouse", "kind": "up", "x": 240, "y": 180, "button": "left", "buttons": 0, "clickCount": 1})

	text, ok := waitConsole("HIT", 10*time.Second)
	if !ok {
		t.Fatal("click never reached the page")
	}
	if !strings.Contains(text, "HIT 160,120") {
		t.Fatalf("click landed at %q, want HIT 160,120 (canvas 240,180 at zoom 1.5)", text)
	}
}

func anyStrings(in []any) []string {
	out := make([]string, 0, len(in))
	for _, v := range in {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
