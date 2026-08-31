//go:build !windows

package server

import (
	"bytes"
	"encoding/json"
	"log"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// terminalReplayCap bounds the per-shell replay buffer: the most recent pty
// output kept server-side so a reattaching socket can repaint what happened
// before and during the disconnect.
const terminalReplayCap = 256 * 1024

// terminalKillGrace is how long a shell gets between SIGTERM and SIGKILL when
// the user closes its tab or its detach TTL expires.
const terminalKillGrace = 2 * time.Second

// terminalAttachMsg is the text control frame sent to the browser first on
// every (re)connect. Every other server -> client frame is binary pty output,
// so the client can key on frame type alone. resumed=true tells the client a
// replay of the existing shell's recent output follows, so it should clear
// any locally restored scrollback instead of showing it twice.
type terminalAttachMsg struct {
	Type    string `json:"type"`
	Resumed bool   `json:"resumed"`
}

var anonTerminalSeq atomic.Int64

// terminalSession is one pty-backed shell and whichever websocket is
// currently driving it. The shell outlives the socket: on disconnect the
// session detaches and arms a TTL timer; a new socket for the same id
// reattaches and cancels it.
type terminalSession struct {
	id        string
	project   string
	resumable bool
	cmd       *exec.Cmd
	ptmx      *os.File
	detachTTL time.Duration
	onExit    func(*terminalSession)

	mu          sync.Mutex
	ws          *websocket.Conn
	replay      []byte
	detachTimer *time.Timer
	exited      bool
}

func newTerminalSession(id, project string, cmd *exec.Cmd, ptmx *os.File, detachTTL time.Duration, onExit func(*terminalSession)) *terminalSession {
	s := &terminalSession{
		id:        id,
		project:   project,
		resumable: id != "",
		cmd:       cmd,
		ptmx:      ptmx,
		detachTTL: detachTTL,
		onExit:    onExit,
	}
	if s.id == "" {
		// Anonymous sockets still need a table key so shutdown/kill paths
		// can find the shell; the frontend never generates this prefix.
		s.id = "anon-" + strconv.FormatInt(anonTerminalSeq.Add(1), 10)
	}
	return s
}

func (s *terminalSession) pid() int {
	return s.cmd.Process.Pid
}

func (s *terminalSession) attached() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ws != nil
}

// readLoop pumps pty output into the replay buffer and to the attached
// socket until the shell exits, then runs the exit teardown. It is the only
// goroutine that reads ptmx, so it is also the one that reaps the process.
func (s *terminalSession) readLoop() {
	buf := make([]byte, 4096)
	for {
		n, err := s.ptmx.Read(buf)
		if n > 0 {
			s.deliver(buf[:n])
		}
		if err != nil {
			// EOF/EIO here means the shell exited (user typed `exit`, or it
			// was killed by the detach TTL / DELETE endpoint).
			log.Printf("terminal %s: pty read ended: %v", s.id, err)
			break
		}
	}
	s.exit()
}

// deliver appends output to the replay buffer and forwards it to the live
// socket. A failed socket write is treated as a disconnect: the socket is
// detached and the shell keeps running.
func (s *terminalSession) deliver(p []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.replay = append(s.replay, p...)
	if len(s.replay) > terminalReplayCap {
		// Drop the oldest bytes, then skip to the next line start so the
		// replay never opens mid-escape-sequence.
		trimmed := s.replay[len(s.replay)-terminalReplayCap:]
		if i := bytes.IndexByte(trimmed, '\n'); i >= 0 {
			trimmed = trimmed[i+1:]
		}
		s.replay = append(s.replay[:0], trimmed...)
	}
	if s.ws == nil {
		return
	}
	if err := s.ws.WriteMessage(websocket.BinaryMessage, p); err != nil {
		// Normal when the browser reloads or closes the tab; never silent.
		log.Printf("terminal %s: websocket write failed, detaching: %v", s.id, err)
		s.detachLocked(s.ws)
	}
}

// attach makes ws the live socket, replacing (and closing) any previous one,
// cancels a pending detach timer, and sends the attach control frame followed
// by the replay buffer when resuming. It returns false if the shell already
// exited, in which case the caller should just close ws.
func (s *terminalSession) attach(ws *websocket.Conn, resumed bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.exited {
		return false
	}
	if s.detachTimer != nil {
		s.detachTimer.Stop()
		s.detachTimer = nil
	}
	if s.ws != nil && s.ws != ws {
		if err := s.ws.Close(); err != nil {
			log.Printf("terminal %s: failed to close superseded websocket: %v", s.id, err)
		}
	}
	s.ws = ws
	hello, err := json.Marshal(terminalAttachMsg{Type: "attach", Resumed: resumed})
	if err != nil {
		log.Printf("terminal %s: failed to encode attach frame: %v", s.id, err)
		return true
	}
	if err := ws.WriteMessage(websocket.TextMessage, hello); err != nil {
		log.Printf("terminal %s: failed to send attach frame: %v", s.id, err)
		s.detachLocked(ws)
		return true
	}
	if resumed && len(s.replay) > 0 {
		if err := ws.WriteMessage(websocket.BinaryMessage, s.replay); err != nil {
			log.Printf("terminal %s: failed to replay %d bytes: %v", s.id, len(s.replay), err)
			s.detachLocked(ws)
		}
	}
	return true
}

// detach drops ws as the live socket (if it still is) and, for resumable
// sessions, arms the TTL timer that kills the shell if nobody reattaches.
// Anonymous sessions have nothing to reattach to and are killed at once.
func (s *terminalSession) detach(ws *websocket.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.detachLocked(ws)
}

func (s *terminalSession) detachLocked(ws *websocket.Conn) {
	if s.ws != ws || ws == nil {
		return
	}
	s.ws = nil
	if err := ws.Close(); err != nil {
		log.Printf("terminal %s: failed to close websocket: %v", s.id, err)
	}
	if s.exited {
		return
	}
	if !s.resumable {
		go terminateProcessTree(s.pid(), terminalKillGrace)
		return
	}
	if s.detachTimer != nil {
		s.detachTimer.Stop()
	}
	pid := s.pid()
	s.detachTimer = time.AfterFunc(s.detachTTL, func() {
		log.Printf("terminal %s: detached for %s with no reattach, killing pid %d", s.id, s.detachTTL, pid)
		terminateProcessTree(pid, terminalKillGrace)
	})
}

// kill terminates the shell now (explicit tab close). The read loop observes
// the pty closing and runs exit, which is what releases the socket and
// registry entries — so kill never has to race with it.
func (s *terminalSession) kill() {
	s.mu.Lock()
	if s.detachTimer != nil {
		s.detachTimer.Stop()
		s.detachTimer = nil
	}
	exited := s.exited
	s.mu.Unlock()
	if exited {
		return
	}
	terminateProcessTree(s.pid(), terminalKillGrace)
}

// exit runs exactly once when the pty read loop ends: reap the process (Kill
// alone leaves a zombie), close the pty fd, close the live socket so the
// browser shows "session ended", and let the handler drop its registrations.
func (s *terminalSession) exit() {
	s.mu.Lock()
	s.exited = true
	if s.detachTimer != nil {
		s.detachTimer.Stop()
		s.detachTimer = nil
	}
	ws := s.ws
	s.ws = nil
	s.mu.Unlock()

	if err := s.cmd.Process.Kill(); err != nil && !isProcessDone(err) {
		log.Printf("terminal %s: failed to kill pty shell: %v", s.id, err)
	}
	_ = s.cmd.Wait()
	if err := s.ptmx.Close(); err != nil {
		log.Printf("terminal %s: failed to close pty: %v", s.id, err)
	}
	if ws != nil {
		if err := ws.Close(); err != nil {
			log.Printf("terminal %s: failed to close websocket: %v", s.id, err)
		}
	}
	s.onExit(s)
}
