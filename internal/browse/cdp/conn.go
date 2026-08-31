package cdp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
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

// Conn is a minimal Chrome DevTools Protocol client over the
// --remote-debugging-pipe transport: NUL-terminated JSON on the pipe, with
// request/response correlation by id and per-session event subscriptions.
type Conn struct {
	r       io.Reader
	w       io.Writer
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
	c := &Conn{
		r:       r,
		w:       w,
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
		if cw, ok := c.w.(io.Closer); ok {
			_ = cw.Close()
		}
		c.finish(ErrConnClosed)
		if cr, ok := c.r.(io.Closer); ok {
			_ = cr.Close()
		}
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
func (c *Conn) Call(ctx context.Context, sessionID, method string, params any, result any) error {
	select {
	case <-c.done:
		return ErrConnClosed
	default:
	}

	id := c.nextID()
	b, err := json.Marshal(command{ID: id, Method: method, Params: params, SessionID: sessionID})
	if err != nil {
		return err
	}
	b = append(b, 0)
	ch := c.registerPending(id)
	defer c.removePending(id)

	if err := c.write(b); err != nil {
		select {
		case <-c.done:
			return ErrConnClosed
		default:
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
	rd := bufio.NewReader(c.r)
	for {
		frame, err := rd.ReadBytes(0) // grows without a line cap
		if err != nil {
			c.finish(ErrConnClosed)
			return
		}
		frame = bytes.TrimSpace(frame[:len(frame)-1]) // strip trailing \0 and stray whitespace
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

func (c *Conn) write(data []byte) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	_, err := c.w.Write(data)
	return err
}
