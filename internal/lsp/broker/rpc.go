package broker

import (
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"time"
)

const (
	requestKind  = "rpc_request"
	responseKind = "rpc_response"
	notifyKind   = "rpc_notification"
	pushKind     = "rpc_push"
)

// Upstream is the broker-owned protocol endpoint. It deliberately has no LSP
// dependency so the transport package remains usable by the broker process.
type Upstream interface {
	Call(method string, params json.RawMessage) (json.RawMessage, error)
	Notify(method string, params json.RawMessage) error
}

type rpcMessage struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
}

type rpcResult struct {
	msg rpcMessage
	err error
}

// PushSender lets an Upstream send unprompted server-originated notifications
// (e.g. LSP publishDiagnostics) on a connection ServeRPC is already serving.
// Safe for concurrent use, including concurrently with ServeRPC's own
// response writes on the same connection.
type PushSender struct {
	conn    net.Conn
	writeMu *sync.Mutex
}

func (p *PushSender) Push(method string, params any) error {
	b, err := json.Marshal(rpcMessage{Method: method, Params: mustJSON(params)})
	if err != nil {
		return err
	}
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	return WriteFrame(p.conn, Envelope{Protocol: ProtocolVersion, Kind: pushKind, Payload: b})
}

// ServeRPC proxies one authenticated connection to the single upstream.
// Server-originated *requests* remain unsupported; server-originated
// *notifications* can be sent via the PushSender handed to registerPush
// (nil to skip) for as long as this connection stays open. The caller is
// responsible for discarding its reference to the PushSender once ServeRPC
// returns, since the connection is closed at that point.
func ServeRPC(conn net.Conn, upstream Upstream, registerPush func(*PushSender)) {
	defer conn.Close()
	var writeMu sync.Mutex
	if registerPush != nil {
		registerPush(&PushSender{conn: conn, writeMu: &writeMu})
	}
	for {
		var env Envelope
		if err := ReadFrame(conn, &env); err != nil {
			return
		}
		var msg rpcMessage
		if err := json.Unmarshal(env.Payload, &msg); err != nil {
			return
		}
		switch env.Kind {
		case requestKind:
			result, err := upstream.Call(msg.Method, msg.Params)
			resp := rpcMessage{Result: result}
			if err != nil {
				resp.Error = err.Error()
			}
			b, _ := json.Marshal(resp)
			writeMu.Lock()
			err = WriteFrame(conn, Envelope{Protocol: ProtocolVersion, Kind: responseKind, RequestID: env.RequestID, Payload: b})
			writeMu.Unlock()
			if err != nil {
				return
			}
		case notifyKind:
			if err := upstream.Notify(msg.Method, msg.Params); err != nil {
				return
			}
		default:
			return
		}
	}
}

// RPCClient is the authenticated request/response broker transport.
type RPCClient struct {
	conn        net.Conn
	mu          sync.Mutex
	writeMu     sync.Mutex
	next        uint64
	pending     map[string]chan rpcResult
	closed      chan struct{}
	closeOnce   sync.Once
	pushMu      sync.Mutex
	pushHandler func(method string, params json.RawMessage)
}

func NewRPCClient(conn net.Conn) *RPCClient {
	c := &RPCClient{conn: conn, pending: make(map[string]chan rpcResult), closed: make(chan struct{})}
	go c.readLoop()
	return c
}

// OnPush registers the handler invoked for unprompted server-originated
// notifications (see PushSender). Only one handler is kept; a later call
// replaces the previous one. Must be called before push notifications are
// expected to avoid missing early ones — there is no replay buffer.
func (c *RPCClient) OnPush(handler func(method string, params json.RawMessage)) {
	c.pushMu.Lock()
	c.pushHandler = handler
	c.pushMu.Unlock()
}

func (c *RPCClient) shutdown(err error) {
	c.closeOnce.Do(func() {
		_ = c.conn.Close()
		close(c.closed)
		c.mu.Lock()
		for id, ch := range c.pending {
			delete(c.pending, id)
			ch <- rpcResult{err: err}
		}
		c.mu.Unlock()
	})
}

func (c *RPCClient) readLoop() {
	for {
		var env Envelope
		if err := ReadFrame(c.conn, &env); err != nil {
			c.shutdown(fmt.Errorf("broker connection closed: %w", err))
			return
		}
		var msg rpcMessage
		if json.Unmarshal(env.Payload, &msg) != nil {
			continue
		}
		if env.Kind == pushKind {
			c.pushMu.Lock()
			h := c.pushHandler
			c.pushMu.Unlock()
			if h != nil {
				h(msg.Method, msg.Params)
			}
			continue
		}
		if env.Kind != responseKind {
			continue
		}
		c.mu.Lock()
		ch := c.pending[env.RequestID]
		delete(c.pending, env.RequestID)
		c.mu.Unlock()
		if ch != nil {
			ch <- rpcResult{msg: msg}
		}
	}
}
func (c *RPCClient) write(env Envelope) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return WriteFrame(c.conn, env)
}
func (c *RPCClient) Call(method string, params any) (json.RawMessage, error) {
	return c.CallTimeout(method, params, 20*time.Second)
}

func (c *RPCClient) CallTimeout(method string, params any, timeout time.Duration) (json.RawMessage, error) {
	b, err := json.Marshal(rpcMessage{Method: method, Params: mustJSON(params)})
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.next++
	id := fmt.Sprint(c.next)
	ch := make(chan rpcResult, 1)
	c.pending[id] = ch
	c.mu.Unlock()
	if err = c.write(Envelope{Protocol: ProtocolVersion, Kind: requestKind, RequestID: id, Payload: b}); err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, err
	}
	select {
	case result := <-ch:
		if result.err != nil {
			return nil, result.err
		}
		msg := result.msg
		if msg.Error != "" {
			return nil, fmt.Errorf("LSP error: %s", msg.Error)
		}
		return msg.Result, nil
	case <-c.closed:
		return nil, fmt.Errorf("broker connection closed")
	case <-time.After(timeout):
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, fmt.Errorf("broker request %q timed out after %s", method, timeout)
	}
}
func (c *RPCClient) Notify(method string, params any) error {
	b, err := json.Marshal(rpcMessage{Method: method, Params: mustJSON(params)})
	if err != nil {
		return err
	}
	return c.write(Envelope{Protocol: ProtocolVersion, Kind: notifyKind, Payload: b})
}
func (c *RPCClient) Close() error {
	c.shutdown(fmt.Errorf("broker connection closed"))
	return nil
}
