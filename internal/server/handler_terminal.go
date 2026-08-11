//go:build !windows

package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/creack/pty"
	"github.com/gorilla/websocket"
)

// terminalUpgrader upgrades /api/terminal/ws. The origin check is same-origin
// only: this endpoint hands out an interactive shell, so a permissive check
// would allow cross-site WebSocket hijacking (any page a logged-in user
// visits could drive the pty from their browser). Comparing against r.Host
// rather than a configured address keeps this correct behind the tailscale
// path prefix and in the desktop shell, both of which load the SPA from the
// server's own origin.
var terminalUpgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin:     terminalSameOrigin,
}

// terminalSameOrigin reports whether the request's Origin matches the host it
// was sent to. A missing Origin is allowed: only browsers send the header, and
// non-browser clients cannot be tricked into a cross-site request. This is the
// second line of defence — the route is also behind authMiddleware, which
// requires the bearer header or ?token= before the handler ever runs.
func terminalSameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		log.Printf("terminal: rejecting websocket with unparseable Origin %q: %v", origin, err)
		return false
	}
	if !strings.EqualFold(u.Host, r.Host) {
		log.Printf("terminal: rejecting cross-origin websocket from %q (host %q)", origin, r.Host)
		return false
	}
	return true
}

// resizePrefix is the cheap discriminator between a control frame and raw
// keystrokes. Everything the browser sends is a text frame, so a prefix check
// (rather than a separate message type or a framing byte) keeps the client
// side free of binary/text send juggling.
var resizePrefix = []byte(`{"type":"resize"`)

const (
	terminalMaxMessageSize = 32 * 1024
	terminalMaxSessions    = 8
)

type terminalResizeMsg struct {
	Type string `json:"type"`
	Cols uint16 `json:"cols"`
	Rows uint16 `json:"rows"`
}

func (h *Handler) reserveTerminalSession() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.activeTerminalSessions >= terminalMaxSessions {
		return false
	}
	h.activeTerminalSessions++
	return true
}

func (h *Handler) releaseTerminalSession() {
	h.mu.Lock()
	if h.activeTerminalSessions > 0 {
		h.activeTerminalSessions--
	}
	h.mu.Unlock()
}

// HandleTerminalWS bridges a websocket to a pty-backed login shell running in
// the project working directory, giving the web UI a real interactive terminal
// (as opposed to POST /api/shell, which is one-shot command exec).
//
// The handler is stateless per connection: one websocket == one pty == one
// shell process. Multiple terminal tabs in the UI simply open multiple
// connections, so there is no server-side session registry to keep in sync.
//
// Framing: client -> server frames are raw keystrokes unless they start with
// `{"type":"resize"`, in which case they are parsed as a resize control
// message. Server -> client frames are always binary pty output.
func (h *Handler) HandleTerminalWS(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	workDir := h.workDir
	available := h.terminalAuthConfigured || h.terminalLoopback
	h.mu.Unlock()

	// Reject before upgrading so the browser sees a real HTTP status instead
	// of an opaque websocket failure.
	if !available {
		writeError(w, http.StatusForbidden, "terminal requires server authentication or a loopback bind address")
		return
	}
	if !h.reserveTerminalSession() {
		writeError(w, http.StatusTooManyRequests, "too many active terminal sessions")
		return
	}
	defer h.releaseTerminalSession()

	if workDir == "" {
		workDir = "."
	}

	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}

	cmd := exec.Command(shell)
	cmd.Dir = workDir
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")

	ptmx, err := pty.Start(cmd)
	if err != nil {
		log.Printf("terminal: failed to start pty shell %q in %q: %v", shell, workDir, err)
		writeError(w, http.StatusInternalServerError, "failed to start terminal")
		return
	}

	ws, err := terminalUpgrader.Upgrade(w, r, nil)
	if err != nil {
		// Upgrade already wrote an error response; tear the pty back down so
		// a failed handshake doesn't leak a shell process.
		log.Printf("terminal: websocket upgrade failed: %v", err)
		if killErr := cmd.Process.Kill(); killErr != nil {
			log.Printf("terminal: failed to kill pty shell after upgrade failure: %v", killErr)
		}
		_ = cmd.Wait()
		if closeErr := ptmx.Close(); closeErr != nil {
			log.Printf("terminal: failed to close pty after upgrade failure: %v", closeErr)
		}
		return
	}
	ws.SetReadLimit(terminalMaxMessageSize)

	// Both the reader goroutine and the writer loop can fail at once, so
	// teardown runs exactly once: kill the shell, reap it (Kill alone leaves a
	// zombie), close the pty fd, close the socket.
	var closeOnce sync.Once
	shutdown := func() {
		closeOnce.Do(func() {
			if err := cmd.Process.Kill(); err != nil && !isProcessDone(err) {
				log.Printf("terminal: failed to kill pty shell: %v", err)
			}
			_ = cmd.Wait()
			if err := ptmx.Close(); err != nil {
				log.Printf("terminal: failed to close pty: %v", err)
			}
			if err := ws.Close(); err != nil {
				log.Printf("terminal: failed to close websocket: %v", err)
			}
		})
	}
	defer shutdown()

	// pty output -> websocket
	go func() {
		defer shutdown()
		buf := make([]byte, 4096)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				if werr := ws.WriteMessage(websocket.BinaryMessage, buf[:n]); werr != nil {
					// Normal when the browser tab closes; log at the same
					// level as any other write failure so it is never silent.
					log.Printf("terminal: websocket write failed, closing session: %v", werr)
					return
				}
			}
			if err != nil {
				// EOF here means the shell exited (user typed `exit`).
				log.Printf("terminal: pty read ended: %v", err)
				return
			}
		}
	}()

	// websocket -> pty stdin (plus resize control frames)
	for {
		_, data, err := ws.ReadMessage()
		if err != nil {
			if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				log.Printf("terminal: websocket read failed, closing session: %v", err)
			}
			return
		}
		if bytes.HasPrefix(bytes.TrimSpace(data), resizePrefix) {
			var msg terminalResizeMsg
			if err := json.Unmarshal(bytes.TrimSpace(data), &msg); err != nil {
				log.Printf("terminal: failed to parse resize control frame: %v", err)
				continue
			}
			if msg.Cols == 0 || msg.Rows == 0 {
				continue
			}
			if err := pty.Setsize(ptmx, &pty.Winsize{Cols: msg.Cols, Rows: msg.Rows}); err != nil {
				log.Printf("terminal: failed to resize pty to %dx%d: %v", msg.Cols, msg.Rows, err)
			}
			continue
		}
		if _, err := ptmx.Write(data); err != nil {
			log.Printf("terminal: failed to write to pty stdin: %v", err)
			return
		}
	}
}

// isProcessDone reports whether a Kill error is just "the shell already
// exited", which is the expected case when the user types `exit`.
func isProcessDone(err error) bool {
	return errors.Is(err, os.ErrProcessDone)
}
