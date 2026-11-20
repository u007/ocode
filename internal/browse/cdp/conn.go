package cdp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

// ErrConnClosed is returned by Call when the pipe is closed (Chrome died) or
// the connection was explicitly closed, instead of a response arriving.
var ErrConnClosed = errors.New("cdp: connection closed")

// CDPError is a protocol error returned by a {"id":…,"error":{…}} reply.
// Method records the command that produced the error.
type CDPError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Method  string
}

func (e *CDPError) Error() string {
	return fmt.Sprintf("cdp: %s: %d %s", e.Method, e.Code, e.Message)
}

// command is the wire shape of a message the Conn writes. sessionId is
// omitted for browser-level commands.
type command struct {
	ID        int64  `json:"id"`
	Method    string `json:"method"`
	Params    any    `json:"params,omitempty"`
	SessionID string `json:"sessionId,omitempty"`
}

type wireError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// wireMessage is the wire shape of a message the Conn reads: a reply carries
// id+result/error; an event carries method+params.
type wireMessage struct {
	ID        *int64          `json:"id"`
	Method    string          `json:"method"`
	Params    json.RawMessage `json:"params"`
	Result    json.RawMessage `json:"result"`
	SessionID string          `json:"sessionId"`
	Error     *wireError      `json:"error"`
}

// pendingResp is delivered on a pending Call's channel.
type pendingResp struct {
	result json.RawMessage
	cdpErr *CDPError
	err    error
}

// transport moves whole CDP messages over one connection. Each implementation
// owns its framing: the pipe transport NUL-terminates every message, the
// websocket transport sends one message per text frame.
type transport interface {
	// write sends one marshalled command. deadline, when non-zero, bounds the
	// write on transports that support one.
	write(msg []byte, deadline time.Time) error
	// read blocks for the next message from the peer and returns its JSON
	// body without framing. A non-nil error ends the stream.
	read() ([]byte, error)
	// close tears the connection down and must unblock a pending read.
	close() error
}

// pipeTransport is the --remote-debugging-pipe framing: NUL-terminated JSON.
type pipeTransport struct {
	r  io.Reader
	w  io.Writer
	rd *bufio.Reader
}

func (p *pipeTransport) write(msg []byte, deadline time.Time) error {
	if dw, ok := p.w.(interface{ SetWriteDeadline(time.Time) error }); ok {
		if err := dw.SetWriteDeadline(deadline); err != nil {
			return err
		}
	}
	_, err := p.w.Write(append(msg, 0))
	return err
}

func (p *pipeTransport) read() ([]byte, error) {
	frame, err := p.rd.ReadBytes(0) // grows without a line cap
	if err != nil {
		return nil, ErrConnClosed
	}
	return bytes.TrimSpace(frame[:len(frame)-1]), nil // strip trailing \0 and stray whitespace
}

func (p *pipeTransport) close() error {
	if cw, ok := p.w.(io.Closer); ok {
		_ = cw.Close()
	}
	if cr, ok := p.r.(io.Closer); ok {
		_ = cr.Close()
	}
	return nil
}

// Conn is a minimal Chrome DevTools Protocol client, with request/response
// correlation by id and per-session event subscriptions. It speaks over a
// transport: the --remote-debugging-pipe pipe (NewConn) or a websocket
// (DialWebSocket).
type Conn struct {
	tr      transport
	wmu     sync.Mutex // serializes writes
	pmu     sync.Mutex // guards id and pending
	id      int64      // next command id (start at 1)
	pending map[int64]chan *pendingResp

	done     chan struct{}
	doneOnce sync.Once
	closed   sync.Once

	subsMu sync.RWMutex
	subs   map[subKey][]*EventSub
}

// NewConn starts a Conn over the given pipe ends and begins reading frames in
// a background goroutine. Closing the returned Conn closes writer (and reader,
// when it is an io.Closer) and shuts the reader loop down.
func NewConn(r io.Reader, w io.Writer) *Conn {
	return newConn(&pipeTransport{r: r, w: w, rd: bufio.NewReader(r)})
}

// newConn wraps a transport and starts its reader goroutine.
func newConn(tr transport) *Conn {
	c := &Conn{
		tr:      tr,
		pending: make(map[int64]chan *pendingResp),
		done:    make(chan struct{}),
		subs:    make(map[subKey][]*EventSub),
	}
	go c.readLoop()
	return c
}

// Done is closed when the reader exits (pipe EOF, i.e. Chrome died) or Close
// shuts the connection down.
func (c *Conn) Done() <-chan struct{} { return c.done }

// Close closes the pipe, fails all pending calls with ErrConnClosed, closes
// all subscription channels, and is idempotent.
func (c *Conn) Close() error {
	c.closed.Do(func() {
		_ = c.tr.close()
		c.finish(ErrConnClosed)
	})
	return nil
}

// finish runs once: closes done, fails all pending calls, closes all
// subscription channels.
func (c *Conn) finish(err error) {
	c.doneOnce.Do(func() {
		close(c.done)
		c.pmu.Lock()
		for id, ch := range c.pending {
			ch <- &pendingResp{err: err}
			delete(c.pending, id)
		}
		c.pmu.Unlock()
		c.closeSubscriptions()
	})
}

// Call sends a command and blocks until the matching response, ctx
// cancellation, or connection close. params may be nil. result, when non-nil,
// is decoded from the response's result object. Protocol errors return
// *CDPError; a closed pipe returns ErrConnClosed.
// defaultCallTimeout bounds every round-trip whose caller passed a context
// without a deadline. Chrome can accept a command and never answer it (a
// renderer blocked on alert(), a session torn down mid-flight, a lost frame),
// and most callers sit on single serialized goroutines — the websocket
// reader, the Fetch interception loop, the screencast ack loop — so one
// unanswered reply used to wedge the whole tab with no recovery path.
const defaultCallTimeout = 30 * time.Second

func (c *Conn) Call(ctx context.Context, sessionID, method string, params any, result any) error {
	select {
	case <-c.done:
		return ErrConnClosed
	default:
	}
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, defaultCallTimeout)
		defer cancel()
	}

	id := c.nextID()
	b, err := json.Marshal(command{ID: id, Method: method, Params: params, SessionID: sessionID})
	if err != nil {
		return err
	}
	ch := c.registerPending(id)
	defer c.removePending(id)

	deadline, _ := ctx.Deadline()
	if err := c.write(b, deadline); err != nil {
		select {
		case <-c.done:
			return ErrConnClosed
		default:
		}
		if errors.Is(err, os.ErrDeadlineExceeded) {
			// Chrome stopped draining its command pipe, and a partial frame
			// may already be on it: the stream is unusable either way. Tear
			// the connection down so every waiter is released and the
			// Manager's exit watcher relaunches Chrome on the next nav.
			_ = c.Close()
		}
		return err
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case pr := <-ch:
		switch {
		case pr.err != nil:
			return pr.err
		case pr.cdpErr != nil:
			pr.cdpErr.Method = method
			return pr.cdpErr
		case result != nil:
			return json.Unmarshal(pr.result, result)
		default:
			return nil
		}
	case <-c.done:
		return ErrConnClosed
	}
}

func (c *Conn) readLoop() {
	for {
		frame, err := c.tr.read()
		if err != nil {
			c.finish(err)
			return
		}
		if len(frame) == 0 {
			continue
		}
		var m wireMessage
		if err := json.Unmarshal(frame, &m); err != nil {
			continue // malformed frame: ignore
		}
		if m.ID != nil {
			c.dispatchResponse(m)
		} else if m.Method != "" {
			c.dispatchEvent(m)
		}
	}
}

func (c *Conn) nextID() int64 {
	c.pmu.Lock()
	c.id++
	id := c.id
	c.pmu.Unlock()
	return id
}

func (c *Conn) registerPending(id int64) chan *pendingResp {
	ch := make(chan *pendingResp, 1)
	c.pmu.Lock()
	c.pending[id] = ch
	c.pmu.Unlock()
	return ch
}

func (c *Conn) removePending(id int64) {
	c.pmu.Lock()
	delete(c.pending, id)
	c.pmu.Unlock()
}

func (c *Conn) dispatchResponse(m wireMessage) {
	c.pmu.Lock()
	id := *m.ID
	ch, ok := c.pending[id]
	if ok {
		delete(c.pending, id)
	}
	c.pmu.Unlock()
	if !ok {
		return
	}
	var pr *pendingResp
	if m.Error != nil {
		pr = &pendingResp{cdpErr: &CDPError{Code: m.Error.Code, Message: m.Error.Message}}
	} else {
		pr = &pendingResp{result: m.Result}
	}
	ch <- pr // buffered(1); a late reply to a removed id is dropped above
}

// write serializes one command onto the transport. Pipe writes block when
// Chrome stops draining fd 3 (~64 KB of buffer), and wmu serializes every call
// in the process behind the first blocked writer, so the caller's deadline is
// applied to the write as well as the reply wait.
func (c *Conn) write(data []byte, deadline time.Time) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	return c.tr.write(data, deadline)
}
