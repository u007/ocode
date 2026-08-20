package broker

import (
	"context"
	"encoding/json"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

type testUpstream struct{ mu sync.Mutex }

func (u *testUpstream) Call(method string, _ json.RawMessage) (json.RawMessage, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	return json.Marshal(method + "-reply")
}
func (u *testUpstream) Notify(string, json.RawMessage) error { return nil }

func TestRPCTwoClientsRouteResponses(t *testing.T) {
	i := Identity{Root: "/tmp/project", Executable: "/bin/fake", LanguageID: "go"}
	m, err := NewMetadata(i, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	u := &testUpstream{}
	l, err := Listen(m, func(c net.Conn) { ServeRPC(c, u, nil) })
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	m = l.Metadata()
	ctx := context.Background()
	c1, err := Connect(ctx, m, "one", 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	c2, err := Connect(ctx, m, "two", 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	r1 := NewRPCClient(c1)
	r2 := NewRPCClient(c2)
	defer r1.Close()
	defer r2.Close()
	var wg sync.WaitGroup
	wg.Add(2)
	got := make(chan string, 2)
	for _, c := range []*RPCClient{r1, r2} {
		go func(c *RPCClient) {
			defer wg.Done()
			b, e := c.Call("query", nil)
			if e != nil {
				t.Error(e)
				return
			}
			var s string
			_ = json.Unmarshal(b, &s)
			got <- s
		}(c)
	}
	wg.Wait()
	close(got)
	values := ""
	for s := range got {
		values += s
	}
	if !strings.Contains(values, "query-reply") || len(values) != 22 {
		t.Fatalf("responses=%q", values)
	}
}

func TestServeRPCPushReachesClient(t *testing.T) {
	i := Identity{Root: "/tmp/project", Executable: "/bin/fake", LanguageID: "go"}
	m, err := NewMetadata(i, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	u := &testUpstream{}
	senderCh := make(chan *PushSender, 1)
	l, err := Listen(m, func(c net.Conn) {
		ServeRPC(c, u, func(p *PushSender) { senderCh <- p })
	})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	m = l.Metadata()
	conn, err := Connect(context.Background(), m, "one", 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	r := NewRPCClient(conn)
	defer r.Close()

	got := make(chan string, 1)
	r.OnPush(func(method string, params json.RawMessage) {
		var s string
		_ = json.Unmarshal(params, &s)
		got <- method + ":" + s
	})

	sender := <-senderCh
	if err := sender.Push("textDocument/publishDiagnostics", "clean"); err != nil {
		t.Fatal(err)
	}

	select {
	case v := <-got:
		if v != "textDocument/publishDiagnostics:clean" {
			t.Fatalf("push=%q", v)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("push not received")
	}
}

func TestConnectRejectsBadAuthentication(t *testing.T) {
	i := Identity{Root: "/tmp/project", Executable: "/bin/fake", LanguageID: "go"}
	m, _ := NewMetadata(i, 1, 1)
	l, err := Listen(m, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	m = l.Metadata()
	m.Token = "wrong"
	if _, err := Connect(context.Background(), m, "bad", 1, 0); err == nil {
		t.Fatal("expected authentication failure")
	}
}

func TestRPCClientCloseUnblocksPendingCall(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	rpc := NewRPCClient(client)
	defer rpc.Close()
	requestRead := make(chan struct{})
	go func() {
		var env Envelope
		if ReadFrame(server, &env) == nil {
			close(requestRead)
		}
	}()
	result := make(chan error, 1)
	go func() {
		_, err := rpc.CallTimeout("blocked", nil, time.Minute)
		result <- err
	}()
	select {
	case <-requestRead:
	case <-time.After(time.Second):
		t.Fatal("request was not sent")
	}
	if err := rpc.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if err == nil || !strings.Contains(err.Error(), "connection closed") {
			t.Fatalf("unexpected call error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("pending call remained blocked after close")
	}
	if len(rpc.pending) != 0 {
		t.Fatalf("pending calls leaked: %d", len(rpc.pending))
	}
}
