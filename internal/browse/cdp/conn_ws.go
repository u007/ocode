package cdp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// wsHandshakeTimeout bounds the upgrade when the caller's context has no
// deadline of its own, so a black-holed host cannot hang a dial forever.
const wsHandshakeTimeout = 30 * time.Second

// DialWebSocket connects to a browser-level CDP websocket such as
// wss://host/devtools/browser/<id>?token=<secret> and returns a Conn that
// speaks one JSON message per text frame.
//
// The URL's query carries credentials, so no error returned from here ever
// contains it: parse failures are reported without the raw URL, and every
// other error names only the host with the query values scrubbed.
func DialWebSocket(ctx context.Context, rawURL string) (*Conn, error) {
	return dialWebSocket(ctx, rawURL, nil)
}

// dialWebSocket is DialWebSocket with the network dial injectable, so tests
// can make the transport fail with a cause whose text contains the full URL.
// A nil netDial uses the default dialer.
func dialWebSocket(ctx context.Context, rawURL string, netDial func(ctx context.Context, network, addr string) (net.Conn, error)) (*Conn, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		// url.Parse quotes the whole input in its error, token included.
		return nil, errors.New("cdp: websocket URL is malformed")
	}
	if u.Scheme != "ws" && u.Scheme != "wss" {
		return nil, fmt.Errorf("cdp: websocket URL scheme must be ws or wss, got %q", u.Scheme)
	}
	if u.Host == "" {
		return nil, errors.New("cdp: websocket URL has no host")
	}
	t := &wsTransport{host: u.Host, secrets: querySecrets(u)}

	d := websocket.Dialer{HandshakeTimeout: wsHandshakeTimeout, NetDialContext: netDial}
	ws, resp, err := d.DialContext(ctx, rawURL, nil)
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		return nil, t.wrap("dial", err)
	}
	t.ws = ws
	return newConn(t), nil
}

// querySecrets returns every query value and the raw query of u, the strings
// that must never appear in an error. Empty values are skipped: an empty
// needle would match everywhere.
func querySecrets(u *url.URL) []string {
	var out []string
	if u.RawQuery != "" {
		out = append(out, u.RawQuery)
	}
	for _, vs := range u.Query() {
		for _, v := range vs {
			if v != "" {
				out = append(out, v)
			}
		}
	}
	return out
}

// wsTransport is the CDP-over-websocket framing: one JSON message per text
// frame, with no NUL terminator.
type wsTransport struct {
	ws      *websocket.Conn
	host    string   // host[:port] only; never the URL
	secrets []string // scrubbed from every error text
}

func (t *wsTransport) write(msg []byte, deadline time.Time) error {
	if err := t.ws.SetWriteDeadline(deadline); err != nil {
		return t.wrap("write", err)
	}
	if err := t.ws.WriteMessage(websocket.TextMessage, msg); err != nil {
		return t.wrap("write", err)
	}
	return nil
}

// read returns ErrConnClosed (via errors.Is) once the peer closes or the
// stream breaks, so every pending Call fails with the same sentinel the pipe
// transport uses.
func (t *wsTransport) read() ([]byte, error) {
	_, msg, err := t.ws.ReadMessage()
	if err != nil {
		return nil, &wsError{op: "read", host: t.host, msg: t.scrub(err), kept: []error{ErrConnClosed}}
	}
	return msg, nil
}

func (t *wsTransport) close() error {
	// Best-effort close frame so the peer sees a clean shutdown. The write is
	// bounded so a stalled peer cannot block Close.
	_ = t.ws.WriteControl(websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
		time.Now().Add(time.Second))
	return t.ws.Close()
}

// wsSentinels are the causes a wsError may keep identity for. Only these are
// matched by errors.Is; the underlying error is never exposed.
var wsSentinels = []error{ErrConnClosed, os.ErrDeadlineExceeded, context.DeadlineExceeded, context.Canceled}

// wrap names the operation and host. The cause is reduced to the sentinels it
// matches, so errors.Is keeps working for them while the cause itself is
// dropped.
func (t *wsTransport) wrap(op string, err error) error {
	e := &wsError{op: op, host: t.host, msg: t.scrub(err)}
	for _, s := range wsSentinels {
		if errors.Is(err, s) {
			e.kept = append(e.kept, s)
		}
	}
	return e
}

func (t *wsTransport) scrub(err error) string {
	s := err.Error()
	for _, v := range t.secrets {
		s = strings.ReplaceAll(s, v, "<redacted>")
	}
	return s
}

// wsError is a websocket failure. Error prints the host and the scrubbed
// message. It matches errors.Is only for the sentinels in kept, and carries no
// Unwrap, so no underlying error text can leave the package.
type wsError struct {
	op   string
	host string
	msg  string
	kept []error
}

func (e *wsError) Error() string {
	return "cdp: websocket " + e.op + " " + e.host + ": " + e.msg
}

// Is reports whether target is one of the sentinels this failure kept.
func (e *wsError) Is(target error) bool {
	for _, s := range e.kept {
		if s == target {
			return true
		}
	}
	return false
}
