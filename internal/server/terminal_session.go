//go:build !windows

package server

import (
	"bytes"
	"context"
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

// terminalReplayPendingCap bounds live output queued while an attaching
// websocket is receiving its replay. If the client cannot catch up within
// this bound, the connection is closed; durable history remains available for
// the next REST-to-WebSocket handoff.
const terminalReplayPendingCap = 256 * 1024

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
	writeMu     sync.Mutex
	ws          *websocket.Conn
	replay      []byte
	history     *terminalHistory
	detachTimer *time.Timer
	exited      bool
	replaying   bool
	pending     [][]byte
	pendingSize int
	replayOver  bool
	// done is closed by exit() after the pty read loop has drained final
	// output, synced/closed history, closed the socket, and run the exit
	// hook. Shutdown waits on it to guarantee the last bytes are persisted
	// before the process exits.
	done     chan struct{}
	doneOnce sync.Once
}

func newTerminalSession(id, project string, cmd *exec.Cmd, ptmx *os.File, detachTTL time.Duration, onExit func(*terminalSession)) *terminalSession {
	s := &terminalSession{
		id:        id,
		project:   project,
		resumable: id != "",
		cmd:       cmd,
		ptmx:      ptmx,
		detachTTL: detachTTL,
		history:   newTerminalHistory(project, id),
		onExit:    onExit,
		done:      make(chan struct{}),
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

// hasExited reports whether the shell has terminated (set by exit() before
// the table entry is removed). Callers use it to avoid reattaching to — or
// handing out — a session that is only awaiting teardown.
func (s *terminalSession) hasExited() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.exited
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
	if s.replaying {
		if s.replayOver || s.pendingSize+len(p) > terminalReplayPendingCap {
			// Preserve the durable record even when the live queue overflows.
			// The attach path will close this socket and the client can recover
			// these bytes through REST history.
			for _, queued := range s.pending {
				s.recordLocked(queued)
			}
			s.pending = nil
			s.pendingSize = 0
			s.replayOver = true
			s.recordLocked(p)
		} else {
			// readLoop reuses its input buffer on the next read, so queued
			// output must own its bytes. It is recorded only after the replay
			// has been sent, keeping the handoff cursor stable.
			queued := append([]byte(nil), p...)
			s.pending = append(s.pending, queued)
			s.pendingSize += len(queued)
		}
		s.mu.Unlock()
		return
	}
	s.recordLocked(p)
	if s.ws == nil {
		s.mu.Unlock()
		return
	}
	ws := s.ws
	s.mu.Unlock()

	// Gorilla permits one concurrent writer per connection. Revalidate the
	// socket after taking the writer lock because attach may have replaced it
	// while this delivery was preparing its frame.
	s.writeMu.Lock()
	s.mu.Lock()
	if s.ws != ws || s.replaying || s.replayOver {
		s.mu.Unlock()
		s.writeMu.Unlock()
		return
	}
	s.mu.Unlock()
	err := ws.WriteMessage(websocket.BinaryMessage, p)
	s.writeMu.Unlock()
	if err != nil {
		// Normal when the browser reloads or closes the tab; never silent.
		log.Printf("terminal %s: websocket write failed, detaching: %v", s.id, err)
		s.mu.Lock()
		if s.ws == ws {
			s.detachLocked(ws)
		}
		s.mu.Unlock()
	}
}

// recordLocked persists p and updates the bounded in-memory replay buffer.
// The caller must hold s.mu. Network writes deliberately happen elsewhere.
func (s *terminalSession) recordLocked(p []byte) {
	// Persist the full pty output to the append-only disk log FIRST — this is
	// the full-length record and the only thing that survives desktop exit /
	// server restart. The in-memory replay buffer below is kept at a small cap
	// purely for fast reattach repaint; it never loses history because the log
	// has it all and the frontend pages older content back from disk.
	s.history.write(p)
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
}

// attach makes ws the live socket, replacing (and closing) any previous one,
// cancels a pending detach timer, and sends the attach control frame followed
// by replay. With a history offset, replay starts at that disk cursor rather
// than the capped in-memory buffer. The session mutex only protects state and
// replay snapshots; network I/O is serialized by writeMu and happens without
// holding s.mu.
// The optional argument preserves the existing no-cursor attach behavior.
func (s *terminalSession) attach(ws *websocket.Conn, resumed bool, historyOffset ...int64) bool {
	hello, err := json.Marshal(terminalAttachMsg{Type: "attach", Resumed: resumed})
	if err != nil {
		log.Printf("terminal %s: failed to encode attach frame: %v", s.id, err)
		return true
	}

	// Keep all websocket writes serialized, including the attach control frame.
	// deliver never waits for this lock while holding s.mu.
	s.writeMu.Lock()
	s.mu.Lock()
	if s.exited {
		s.mu.Unlock()
		s.writeMu.Unlock()
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
	s.replaying = true
	s.pending = nil
	s.pendingSize = 0
	s.replayOver = false

	var offset, replayEnd int64
	var replay []byte
	if resumed {
		if len(historyOffset) > 0 {
			offset = historyOffset[0]
			replayEnd = s.history.byteLen()
		} else {
			replay = append([]byte(nil), s.replay...)
		}
	} else {
		// A shell can produce its initial prompt before the websocket upgrade
		// reaches attach. Preserve that output for the fresh socket too; the
		// old session lock naturally delayed delivery until after the hello.
		replay = append([]byte(nil), s.replay...)
	}
	s.mu.Unlock()

	// writeMu is held from the control frame through the replay and queued
	// output, so live delivery cannot interleave with the handoff.
	if err := ws.WriteMessage(websocket.TextMessage, hello); err != nil {
		log.Printf("terminal %s: failed to send attach frame: %v", s.id, err)
		s.finishAttachFailure(ws)
		s.writeMu.Unlock()
		return true
	}
	if resumed && len(historyOffset) > 0 {
		for offset < replayEnd {
			max := replayEnd - offset
			if max > terminalHistoryMaxPage {
				max = terminalHistoryMaxPage
			}
			chunk, err := s.history.readRange(offset, max)
			if err != nil {
				log.Printf("terminal %s: failed to replay history from offset %d: %v", s.id, offset, err)
				s.finishAttachFailure(ws)
				s.writeMu.Unlock()
				return true
			}
			if len(chunk) == 0 {
				break
			}
			if err := ws.WriteMessage(websocket.BinaryMessage, chunk); err != nil {
				log.Printf("terminal %s: failed to replay %d history bytes: %v", s.id, len(chunk), err)
				s.finishAttachFailure(ws)
				s.writeMu.Unlock()
				return true
			}
			offset += int64(len(chunk))
		}
	} else if len(replay) > 0 {
		if err := ws.WriteMessage(websocket.BinaryMessage, replay); err != nil {
			log.Printf("terminal %s: failed to replay %d bytes: %v", s.id, len(replay), err)
			s.finishAttachFailure(ws)
			s.writeMu.Unlock()
			return true
		}
	}

	for {
		s.mu.Lock()
		if s.ws != ws {
			s.mu.Unlock()
			s.writeMu.Unlock()
			return true
		}
		if s.replayOver {
			// Every byte was persisted before it was queued. Closing forces the
			// client to use durable REST history rather than dropping output.
			s.replaying = false
			s.pending = nil
			s.pendingSize = 0
			s.ws = nil
			s.mu.Unlock()
			if err := ws.Close(); err != nil {
				log.Printf("terminal %s: failed to close replay-overflow websocket: %v", s.id, err)
			}
			s.writeMu.Unlock()
			return true
		}
		pending := s.pending
		s.pending = nil
		s.pendingSize = 0
		if len(pending) == 0 {
			s.replaying = false
			s.mu.Unlock()
			break
		}
		s.mu.Unlock()

		for _, chunk := range pending {
			s.mu.Lock()
			s.recordLocked(chunk)
			s.mu.Unlock()
			if err := ws.WriteMessage(websocket.BinaryMessage, chunk); err != nil {
				log.Printf("terminal %s: failed to replay queued %d bytes: %v", s.id, len(chunk), err)
				s.finishAttachFailure(ws)
				s.writeMu.Unlock()
				return true
			}
		}
	}
	s.writeMu.Unlock()
	return true
}

// finishAttachFailure clears an attach in progress and closes ws. The caller
// must hold writeMu so no other goroutine writes the connection concurrently.
func (s *terminalSession) finishAttachFailure(ws *websocket.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ws != ws {
		return
	}
	s.replaying = false
	s.pending = nil
	s.pendingSize = 0
	s.replayOver = false
	s.detachLocked(ws)
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
// It closes done() last so shutdown waiters observe a fully drained session:
// final output delivered, history synced/closed, socket closed, registry
// entries removed.
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

	// Close (flush) the append-only history log. We do NOT delete it here — the
	// log survives so a reattach, desktop restart, or reload can still page the
	// full transcript. Explicit tab close (DELETE /api/terminal/{id}) deletes it.
	s.history.close()

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
	s.doneOnce.Do(func() { close(s.done) })
}

// waitDone reports whether the session fully exited before ctx expired.
func (s *terminalSession) waitDone(ctx context.Context) bool {
	select {
	case <-s.done:
		return true
	case <-ctx.Done():
		return false
	}
}

// shutdownGracefully sends SIGTERM to the shell's process group so a running
// command can trap it, flush its last output, and exit; the read loop then
// drains those final bytes into history before exit() closes done. It waits
// for done (bounded by ctx) and escalates to SIGKILL only when the deadline
// expires. Detach timers are cancelled so shutdown — unlike a socket drop —
// never leaves a shell lingering for the TTL.
func (s *terminalSession) shutdownGracefully(ctx context.Context) {
	s.mu.Lock()
	if s.detachTimer != nil {
		s.detachTimer.Stop()
		s.detachTimer = nil
	}
	exited := s.exited
	s.mu.Unlock()
	if exited {
		s.waitDone(ctx)
		return
	}
	log.Printf("server: terminating terminal %s (pid %d)", s.id, s.pid())
	terminateProcessTreeSignal(s.pid())
	select {
	case <-s.done:
	case <-ctx.Done():
		log.Printf("terminal %s: shutdown timed out, force-killing pid %d", s.id, s.pid())
		terminateProcessTreeKill(s.pid())
		select {
		case <-s.done:
		case <-ctx.Done():
		}
	}
}
