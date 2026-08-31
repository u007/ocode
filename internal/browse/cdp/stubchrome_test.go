package cdp

import (
	"bytes"
	"encoding/json"
	"io"
	"sync"
)

// stubCall records a CDP call received by the stub server.
type stubCall struct {
	Method    string
	SessionID string
	Params    json.RawMessage
}

// stubChrome is a fake CDP server over io.Pipe speaking \0 framing.
// It replies with canned results and lets tests inject events.
type stubChrome struct {
	rCh     *io.PipeReader // server reads commands from here
	wCh     *io.PipeWriter // server writes responses/events here
	conn    *Conn
	callsMu sync.Mutex
	calls   []stubCall
	seq     int
}

// newStubChrome creates a pipe pair and a Conn. The returned conn is what
// the Manager should use; the stubChrome owns the server side.
func newStubChrome() *stubChrome {
	wR, wW := io.Pipe()
	rR, rW := io.Pipe()
	c := NewConn(rR, wW)
	sc := &stubChrome{rCh: wR, wCh: rW, conn: c}
	go sc.serve()
	return sc
}

func (s *stubChrome) Conn() *Conn { return s.conn }

func (s *stubChrome) Close() {
	_ = s.conn.Close()
	_ = s.rCh.Close()
	_ = s.wCh.Close()
}

func (s *stubChrome) Calls() []stubCall {
	s.callsMu.Lock()
	defer s.callsMu.Unlock()
	cp := make([]stubCall, len(s.calls))
	copy(cp, s.calls)
	return cp
}

func (s *stubChrome) CallsFor(method string) []stubCall {
	var out []stubCall
	for _, c := range s.Calls() {
		if c.Method == method {
			out = append(out, c)
		}
	}
	return out
}

func (s *stubChrome) serve() {
	// Read \0-terminated frames from rCh, reply.
	buf := make([]byte, 0, 4096)
	tmp := make([]byte, 4096)
	for {
		n, err := s.rCh.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
			for {
				idx := bytes.IndexByte(buf, 0)
				if idx < 0 {
					break
				}
				frame := buf[:idx]
				buf = buf[idx+1:]
				s.handleFrame(frame)
			}
		}
		if err != nil {
			return
		}
	}
}

func (s *stubChrome) handleFrame(frame []byte) {
	var msg struct {
		ID        *int64          `json:"id"`
		Method    string          `json:"method"`
		Params    json.RawMessage `json:"params"`
		SessionID string          `json:"sessionId"`
	}
	if err := json.Unmarshal(frame, &msg); err != nil {
		return
	}
	if msg.ID != nil {
		s.callsMu.Lock()
		s.calls = append(s.calls, stubCall{Method: msg.Method, SessionID: msg.SessionID, Params: msg.Params})
		s.seq++
		seq := s.seq
		s.callsMu.Unlock()

		// Build canned result
		var result any
		switch msg.Method {
		case "Target.createBrowserContext":
			result = map[string]string{"browserContextId": jsonBrowserContextID(seq)}
		case "Target.createTarget":
			result = map[string]string{"targetId": jsonTargetID(seq)}
		case "Target.attachToTarget":
			result = map[string]string{"sessionId": jsonSessionID(seq)}
		case "Target.getBrowserContexts":
			result = map[string]any{"browserContextIds": []string{}}
		default:
			result = map[string]any{}
		}
		raw, _ := json.Marshal(result)
		reply := map[string]any{"id": *msg.ID, "result": json.RawMessage(raw)}
		if msg.SessionID != "" {
			reply["sessionId"] = msg.SessionID
		}
		b, _ := json.Marshal(reply)
		b = append(b, 0)
		_, _ = s.wCh.Write(b)
	} else {
		// Event from client — ignore
	}
}

func jsonBrowserContextID(n int) string { return "ctx-" + itoa(n) }
func jsonTargetID(n int) string         { return "target-" + itoa(n) }
func jsonSessionID(n int) string        { return "sess-" + itoa(n) }

func itoa(n int) string { return jsonNumber(n) }
func jsonNumber(n int) string {
	// avoid strconv import for tiny helper
	if n == 0 {
		return "0"
	}
	b := make([]byte, 0, 10)
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// InjectEvent sends an event to the client Conn (Manager).
func (s *stubChrome) InjectEvent(sessionID, method string, params any) {
	b, _ := json.Marshal(params)
	msg := map[string]any{"method": method, "params": json.RawMessage(b)}
	if sessionID != "" {
		msg["sessionId"] = sessionID
	}
	j, _ := json.Marshal(msg)
	j = append(j, 0)
	_, _ = s.wCh.Write(j)
}

// InjectEventRaw sends a pre-marshaled params RawMessage.
func (s *stubChrome) InjectEventRaw(sessionID, method string, raw json.RawMessage) {
	msg := map[string]any{"method": method, "params": raw}
	if sessionID != "" {
		msg["sessionId"] = sessionID
	}
	j, _ := json.Marshal(msg)
	j = append(j, 0)
	_, _ = s.wCh.Write(j)
}
