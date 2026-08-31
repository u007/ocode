package cdp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"
)

func contextBackground() context.Context { return context.Background() }

const nul = "\x00"

// fakePeer holds the "Chrome" ends of the pipe pair: cmdR reads the commands
// the Conn writes, respW writes responses/events that the Conn reads.
type fakePeer struct {
	t     *testing.T
	cmdR  *io.PipeReader
	respW *io.PipeWriter
	conn  *Conn
}

func newFakePeer(t *testing.T) *fakePeer {
	t.Helper()
	wR, wW := io.Pipe() // Conn writes commands to wW; peer reads from wR
	rR, rW := io.Pipe() // peer writes responses to rW; Conn reads from rR
	c := NewConn(rR, wW)
	p := &fakePeer{t: t, cmdR: wR, respW: rW, conn: c}
	t.Cleanup(func() { c.Close(); _ = wR.Close(); _ = rW.Close() })
	return p
}

type cmd struct {
	id        int64
	method    string
	sessionID string
	params    map[string]any
}

// next reads the next \0-terminated command the Conn wrote and decodes it.
func (p *fakePeer) next() cmd {
	p.t.Helper()
	buf := make([]byte, 0, 512)
	tmp := make([]byte, 2048)
	for {
		n, err := p.cmdR.Read(tmp)
		buf = append(buf, tmp[:n]...)
		if err != nil {
			p.t.Fatalf("next: reading command: %v", err)
		}
		if i := bytes.IndexByte(buf, 0); i >= 0 {
			var m struct {
				ID        int64          `json:"id"`
				Method    string         `json:"method"`
				SessionID string         `json:"sessionId"`
				Params    map[string]any `json:"params"`
			}
			if err := json.Unmarshal(buf[:i], &m); err != nil {
				p.t.Fatalf("next: unmarshal %q: %v", buf[:i], err)
			}
			return cmd{id: m.ID, method: m.Method, sessionID: m.SessionID, params: m.Params}
		}
	}
}

// respond writes a raw (non-null-terminated) response frame for the Conn.
func (p *fakePeer) respond(raw string) {
	p.t.Helper()
	if _, err := p.respW.Write([]byte(raw + nul)); err != nil {
		p.t.Fatalf("respond: %v", err)
	}
}

func TestCallWritesOneNullTerminatedMessageWithSequentialIDs(t *testing.T) {
	p := newFakePeer(t)
	ctx := contextBackground()

	done := make(chan error, 1)
	go func() { done <- p.conn.Call(ctx, "", "Browser.close", map[string]any{"a": 1}, nil) }()
	c := p.next()
	if c.id != 1 {
		t.Fatalf("first id = %d, want 1", c.id)
	}
	if c.method != "Browser.close" {
		t.Fatalf("method = %q, want Browser.close", c.method)
	}
	if v, ok := c.params["a"]; !ok || v != float64(1) {
		t.Fatalf("params = %v, want a=1", c.params)
	}
	p.respond(`{"id":1,"result":{}}`)
	if err := <-done; err != nil {
		t.Fatalf("Call: %v", err)
	}

	go func() { done <- p.conn.Call(ctx, "", "Browser.getVersion", nil, nil) }()
	c = p.next()
	if c.id != 2 {
		t.Fatalf("second id = %d, want 2", c.id)
	}
	p.respond(`{"id":2,"result":{}}`)
	if err := <-done; err != nil {
		t.Fatalf("second Call: %v", err)
	}
}

func TestCallLargeResponseNoScannerCap(t *testing.T) {
	p := newFakePeer(t)
	target := make([]byte, 2*1024*1024)
	for i := range target {
		target[i] = byte('A' + i%26)
	}
	var got struct{ Data string `json:"data"` }
	done := make(chan error, 1)
	go func() { done <- p.conn.Call(context.Background(), "", "Foo.bar", nil, &got) }()
	_ = p.next()
	body, err := json.Marshal(map[string]string{"data": string(target)})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	frame := append([]byte(`{"id":1,"result":`), body...)
	frame = append(frame, '}', 0)
	if _, err := p.respW.Write(frame); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("Call: %v", err)
	}
	if len(got.Data) != len(target) || got.Data != string(target) {
		t.Fatalf("large response mismatch: got %d bytes, want %d", len(got.Data), len(target))
	}
}

func TestDoneOnPeerEOFAndInflightCallClosed(t *testing.T) {
	p := newFakePeer(t)
	done := make(chan error, 1)
	go func() { done <- p.conn.Call(context.Background(), "", "Foo.bar", nil, nil) }()
	_ = p.next()
	// Closing the peer's write end is EOF on the Conn's reader.
	_ = p.respW.Close()
	select {
	case err := <-done:
		if err != ErrConnClosed {
			t.Fatalf("Call err = %v, want ErrConnClosed", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("in-flight Call did not return after peer EOF")
	}
	select {
	case <-p.conn.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("Done() not closed after peer EOF")
	}
}
