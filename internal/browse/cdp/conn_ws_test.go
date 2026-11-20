package cdp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// wsPeer is an in-process CDP websocket server. It answers each command by
// id, and handles three methods: Test.event pushes a Target.targetCreated
// event before its reply, Test.die closes the socket without replying, and any
// other method gets an empty result.
type wsPeer struct {
	mu       sync.Mutex
	queries  []string // raw query of each upgrade request
	badFrame bool     // set when a frame carried a NUL byte
}

func (p *wsPeer) snapshot() ([]string, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.queries...), p.badFrame
}

func (p *wsPeer) serve(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	p.queries = append(p.queries, r.URL.RawQuery)
	p.mu.Unlock()

	up := websocket.Upgrader{}
	ws, err := up.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer ws.Close()
	for {
		_, msg, err := ws.ReadMessage()
		if err != nil {
			return
		}
		if strings.IndexByte(string(msg), 0) >= 0 {
			p.mu.Lock()
			p.badFrame = true
			p.mu.Unlock()
		}
		var cmd struct {
			ID     int64  `json:"id"`
			Method string `json:"method"`
		}
		if err := json.Unmarshal(msg, &cmd); err != nil {
			return
		}
		switch cmd.Method {
		case "Test.die":
			return
		case "Test.event":
			_ = ws.WriteMessage(websocket.TextMessage,
				[]byte(`{"method":"Target.targetCreated","params":{"n":1}}`))
		}
		reply, _ := json.Marshal(map[string]any{
			"id":     cmd.ID,
			"result": map[string]string{"echo": cmd.Method},
		})
		if err := ws.WriteMessage(websocket.TextMessage, reply); err != nil {
			return
		}
	}
}

func newWSPeer(t *testing.T) (*wsPeer, string) {
	t.Helper()
	p := &wsPeer{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.serve(w, r)
	}))
	t.Cleanup(srv.Close)
	return p, "ws" + strings.TrimPrefix(srv.URL, "http") + "/devtools/browser/abc"
}

func TestDialWebSocketErrorsNeverCarryToken(t *testing.T) {
	// The network dial fails with a cause whose text is the full URL, query
	// token included, which is the leak the scrubber exists to stop. Without
	// the scrubber this case fails.
	const raw = "ws://192.0.2.1:9/devtools/browser/x?token=SECRETVALUE"
	leaky := func(_ context.Context, _, _ string) (net.Conn, error) {
		return nil, fmt.Errorf("dial failed for %s: connection reset", raw)
	}
	_, err := dialWebSocket(context.Background(), raw, leaky)
	if err == nil {
		t.Fatal("expected the injected dial failure")
	}
	if strings.Contains(err.Error(), "SECRETVALUE") {
		t.Fatalf("injected cause leaked the token: %v", err)
	}
	if !strings.Contains(err.Error(), "192.0.2.1:9") {
		t.Fatalf("error no longer names the host: %v", err)
	}

	// Reserve a port, then close it, so the dial is refused.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()

	cases := map[string]string{
		"closed port":    "ws://" + addr + "/devtools/browser/x?token=SECRETVALUE",
		"malformed URL":  "ws://[::1?token=SECRETVALUE",
		"bad scheme":     "http://" + addr + "/?token=SECRETVALUE",
		"no host":        "wss:///devtools?token=SECRETVALUE",
		"escaped secret": "ws://" + addr + "/?token=SECRET%56ALUE",
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			c, err := DialWebSocket(ctx, raw)
			if err == nil {
				c.Close()
				t.Fatal("expected a dial error")
			}
			for _, leak := range []string{"SECRETVALUE", "SECRET%56ALUE", "token="} {
				if strings.Contains(err.Error(), leak) {
					t.Fatalf("error leaks %q: %v", leak, err)
				}
			}
		})
	}
}

func TestWSErrorScrubsSecretsFromCauseText(t *testing.T) {
	tr := &wsTransport{host: "h.example:9", secrets: []string{"SECRETVALUE", "token=SECRETVALUE"}}
	err := tr.wrap("dial", errors.New("GET /x?token=SECRETVALUE: boom"))
	if strings.Contains(err.Error(), "SECRETVALUE") {
		t.Fatalf("token survived scrubbing: %v", err)
	}
	if !strings.Contains(err.Error(), "h.example:9") {
		t.Fatalf("host missing from error: %v", err)
	}
}

func TestWSTransportRoundTripEventAndServerClose(t *testing.T) {
	peer, base := newWSPeer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	c, err := DialWebSocket(ctx, base+"?token=abc123")
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	events, unsub := c.Subscribe("", "Target.targetCreated")
	defer unsub()

	var res struct {
		Echo string `json:"echo"`
	}
	if err := c.Call(ctx, "", "Test.event", nil, &res); err != nil {
		t.Fatalf("call: %v", err)
	}
	if res.Echo != "Test.event" {
		t.Fatalf("result matched wrong reply: %+v", res)
	}
	select {
	case ev := <-events:
		if string(ev) != `{"n":1}` {
			t.Fatalf("event params = %s", ev)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("event not delivered")
	}

	queries, bad := peer.snapshot()
	if len(queries) != 1 || queries[0] != "token=abc123" {
		t.Fatalf("upgrade query = %v, want the token passed through", queries)
	}
	if bad {
		t.Fatal("a websocket frame carried a NUL terminator")
	}

	// A server-side close must fail the pending caller with ErrConnClosed.
	err = c.Call(ctx, "", "Test.die", nil, nil)
	if !errors.Is(err, ErrConnClosed) {
		t.Fatalf("pending call error = %v, want ErrConnClosed", err)
	}
	select {
	case <-c.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("Done not closed after server close")
	}
	if err := c.Call(ctx, "", "Test.after", nil, nil); !errors.Is(err, ErrConnClosed) {
		t.Fatalf("call after server close = %v, want ErrConnClosed", err)
	}
}

func TestWSTransportClientCloseIsClean(t *testing.T) {
	_, base := newWSPeer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	c, err := DialWebSocket(ctx, base)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
	if err := c.Call(ctx, "", "Test.any", nil, nil); !errors.Is(err, ErrConnClosed) {
		t.Fatalf("call after close = %v, want ErrConnClosed", err)
	}
}
