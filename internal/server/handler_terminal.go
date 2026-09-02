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

	"github.com/creack/pty"
	"github.com/gorilla/websocket"
	"github.com/shirou/gopsutil/v4/process"

	"github.com/u007/ocode/internal/config"
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

const terminalMaxMessageSize = 32 * 1024

type terminalResizeMsg struct {
	Type string `json:"type"`
	Cols uint16 `json:"cols"`
	Rows uint16 `json:"rows"`
}

// HandleTerminalWS bridges a websocket to a pty-backed login shell running in
// the project working directory, giving the web UI a real interactive terminal
// (as opposed to POST /api/shell, which is one-shot command exec).
//
// A `project_path` query parameter selects which project's root the shell
// starts in. It must be one of the roots the server serves (workdir or a
// saved project — the same trust boundary as session resolution); anything
// else is rejected, so this never becomes "spawn a shell in an arbitrary
// directory". Absent means the server workdir, preserving the old contract.
//
// Shells outlive sockets. Each pty is a terminalSession keyed by the
// frontend's terminal_id; when the socket drops (page reload, network blip)
// the shell is detached and kept for terminalDetachTTL, and the next socket
// with the same id reattaches and gets the recent output replayed. Explicit
// tab close goes through DELETE /api/terminal/{id} (HandleTerminalKill).
//
// Framing: client -> server frames are raw keystrokes unless they start with
// `{"type":"resize"`, in which case they are parsed as a resize control
// message. Server -> client: one text frame `{"type":"attach","resumed":bool}`
// first, then binary pty output only.
func (h *Handler) HandleTerminalWS(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	workDir := h.workDir
	available := h.terminalAuthConfigured || h.terminalLoopback
	shellOverride := ""
	if h.cfg != nil {
		shellOverride = h.cfg.Ocode.TerminalShell
	}
	h.mu.Unlock()

	// Reject before upgrading so the browser sees a real HTTP status instead
	// of an opaque websocket failure.
	if !available {
		writeError(w, http.StatusForbidden, "terminal requires server authentication or a loopback bind address")
		return
	}
	if requested := r.URL.Query().Get("project_path"); requested != "" && requested != workDir {
		allowed := false
		for _, root := range h.allowedProjectRoots() {
			if requested == root {
				allowed = true
				break
			}
		}
		if !allowed {
			log.Printf("terminal: rejected project_path %q: not a registered project root", requested)
			writeError(w, http.StatusForbidden, "project_path is not a project registered with this server")
			return
		}
		workDir = requested
	}

	if workDir == "" {
		workDir = "."
	}

	// A registered project root can vanish from disk (deleted, renamed,
	// unmounted). pty.Start would fail with an opaque 500, so fall back to the
	// user's home directory and still hand out a shell.
	if _, statErr := os.Stat(workDir); statErr != nil {
		log.Printf("terminal: working directory %q unavailable, falling back to home dir: %v", workDir, statErr)
		home, homeErr := os.UserHomeDir()
		if homeErr != nil {
			log.Printf("terminal: resolve home dir fallback failed: %v", homeErr)
			home = "/"
		}
		workDir = home
	}

	shell := shellOverride
	if shell == "" {
		shell = config.DefaultTerminalShell()
	}

	// terminal_id (the frontend's TerminalPanel `id`) is the reattach key: a
	// reload or socket drop detaches the shell, and the next socket carrying
	// the same id resumes it. It also lets the terminal-processes emitter
	// correlate a pid with the tab the browser shows it under. An empty id
	// (old/other clients) gets a shell that dies with its socket, as before.
	terminalID := r.URL.Query().Get("terminal_id")

	// Anonymous sockets (no id) have nothing to reattach to: each spawns its
	// own shell that dies with its socket. They skip the reservation — the
	// shared empty key would serialize unrelated connections onto one fake
	// "create" — but still publish their session under the generated anon-N
	// key so the kill/shutdown paths can reach them.
	if terminalID == "" {
		anonSess, anonErr := h.startTerminalShell("", workDir, shell)
		if anonErr != nil {
			log.Printf("terminal: failed to start pty shell %q in %q: %v", shell, workDir, anonErr)
			writeError(w, http.StatusInternalServerError, "failed to start terminal")
			return
		}
		h.terminalSessions.put(anonSess.id, anonSess)
		h.serveFreshTerminal(w, r, anonSess)
		return
	}

	// reserve() decides, under the table lock, whether to reattach to a live
	// shell (it returns it) or hand this caller the exclusive right to spawn.
	// No pty is started before this decision, so reattach and project-mismatch
	// paths can never orphan a shell they didn't spawn, and N concurrent
	// sockets for the same brand-new id spawn exactly one shell: losers get
	// created=false and a done channel, then reattach to the winner.
	existing, created, done := h.terminalSessions.reserve(terminalID)
	if !created {
		if existing == nil {
			// Another socket is spawning right now; wait for it to publish.
			<-done
			existing = h.terminalSessions.lookup(terminalID)
			if existing == nil {
				// The winner's pty.Start failed; the client will retry.
				writeError(w, http.StatusInternalServerError, "failed to start terminal")
				return
			}
		}
		if existing.project != workDir {
			log.Printf("terminal %s: rejected reattach from project %q (owned by %q)", terminalID, workDir, existing.project)
			writeError(w, http.StatusConflict, "terminal_id belongs to a different project")
			return
		}
		ws, err := terminalUpgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("terminal %s: websocket upgrade for reattach failed: %v", terminalID, err)
			return
		}
		ws.SetReadLimit(terminalMaxMessageSize)
		if !existing.attach(ws, true) {
			// Shell exited between lookup and attach; the client will retry and
			// get a fresh shell.
			log.Printf("terminal %s: shell exited before reattach completed", terminalID)
			if err := ws.Close(); err != nil {
				log.Printf("terminal %s: failed to close websocket: %v", terminalID, err)
			}
			return
		}
		log.Printf("terminal %s: reattached (pid %d)", terminalID, existing.pid())
		h.serveTerminalSocket(existing, ws)
		return
	}

	// This caller won the reservation: spawn exactly one shell for the id.
	sess, err := h.startTerminalShell(terminalID, workDir, shell)
	// Publish the session (or the failure) before doing anything else so
	// waiters unblock promptly; on failure they re-lookup, find nothing, and
	// return an error the client will retry.
	h.terminalSessions.completeCreate(terminalID, sess)
	if err != nil {
		log.Printf("terminal: failed to start pty shell %q in %q: %v", shell, workDir, err)
		writeError(w, http.StatusInternalServerError, "failed to start terminal")
		return
	}
	h.serveFreshTerminal(w, r, sess)
}

// startTerminalShell spawns a pty-backed shell in workDir and returns it
// fully wired: process registered and readLoop running. It does NOT publish
// the session to the reattach table — callers do that (completeCreate for
// named ids, or the anonymous path) so concurrent waiters only ever see a
// fully started shell. On failure the shell is torn down so no process,
// pty fd, or goroutine survives.
func (h *Handler) startTerminalShell(terminalID, workDir, shell string) (*terminalSession, error) {
	cmd := terminalShellCommand(shell)
	cmd.Dir = workDir
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")

	ptmx, err := pty.Start(cmd)
	if err != nil {
		return nil, err
	}
	sess := newTerminalSession(terminalID, workDir, cmd, ptmx, h.terminalSessions.detachTTL, h.terminalExited)
	h.terminalProcs.register(terminalID, terminalProcEntry{Project: workDir, PID: int32(cmd.Process.Pid)})
	h.notifyTerminalProcsChanged()
	go sess.readLoop()
	return sess, nil
}

// serveFreshTerminal upgrades the websocket for a just-spawned shell, attaches
// it as a fresh (non-resumed) session, and serves it. It is the shared tail of
// both the anonymous path and the named-id reservation winner. On upgrade
// failure the shell is torn back down so a failed handshake doesn't leak a
// process until the detach TTL — unless a waiting socket already reattached
// and is actively using it, in which case that session must be left alone.
func (h *Handler) serveFreshTerminal(w http.ResponseWriter, r *http.Request, sess *terminalSession) {
	if sess == nil {
		writeError(w, http.StatusInternalServerError, "failed to start terminal")
		return
	}
	ws, err := terminalUpgrader.Upgrade(w, r, nil)
	if err != nil {
		// Upgrade already wrote an error response; tear the shell back down so
		// a failed handshake doesn't leak a process until the detach TTL.
		log.Printf("terminal: websocket upgrade failed: %v", err)
		if !sess.attached() {
			sess.kill()
		}
		return
	}
	ws.SetReadLimit(terminalMaxMessageSize)
	if !sess.attach(ws, false) {
		if err := ws.Close(); err != nil {
			log.Printf("terminal %s: failed to close websocket: %v", sess.id, err)
		}
		return
	}
	h.serveTerminalSocket(sess, ws)
}

// serveTerminalSocket runs the websocket -> pty direction (raw keystrokes plus
// resize control frames) until the socket goes away, then detaches the shell.
// The pty -> websocket direction lives in the session's own read loop, which
// outlives any single socket.
func (h *Handler) serveTerminalSocket(sess *terminalSession, ws *websocket.Conn) {
	defer sess.detach(ws)
	for {
		_, data, err := ws.ReadMessage()
		if err != nil {
			if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				log.Printf("terminal %s: websocket read failed, detaching: %v", sess.id, err)
			}
			return
		}
		if bytes.HasPrefix(bytes.TrimSpace(data), resizePrefix) {
			var msg terminalResizeMsg
			if err := json.Unmarshal(bytes.TrimSpace(data), &msg); err != nil {
				log.Printf("terminal %s: failed to parse resize control frame: %v", sess.id, err)
				continue
			}
			if msg.Cols == 0 || msg.Rows == 0 {
				continue
			}
			if err := pty.Setsize(sess.ptmx, &pty.Winsize{Cols: msg.Cols, Rows: msg.Rows}); err != nil {
				log.Printf("terminal %s: failed to resize pty to %dx%d: %v", sess.id, msg.Cols, msg.Rows, err)
			}
			continue
		}
		if _, err := sess.ptmx.Write(data); err != nil {
			log.Printf("terminal %s: failed to write to pty stdin: %v", sess.id, err)
			return
		}
	}
}

// terminalExited is the session exit hook: drop the shell from the reattach
// table and the processes registry once its pty read loop has reaped it.
func (h *Handler) terminalExited(s *terminalSession) {
	h.terminalSessions.remove(s.id, s)
	if s.resumable {
		h.terminalProcs.unregister(s.id)
		h.notifyTerminalProcsChanged()
	}
}

// HandleTerminalKill is the explicit close for a terminal tab. Because a
// dropped socket only detaches the shell, the frontend must call this when
// the user actually closes the tab; otherwise the shell would linger until
// the detach TTL.
func (h *Handler) HandleTerminalKill(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sess := h.terminalSessions.lookup(id)
	if sess == nil {
		writeError(w, http.StatusNotFound, "no such terminal")
		return
	}
	log.Printf("terminal %s: kill requested (pid %d)", id, sess.pid())
	// terminateProcessTree waits up to the grace period for the shell to
	// exit; don't hold the HTTP response for that.
	go sess.kill()
	w.WriteHeader(http.StatusNoContent)
}

// terminalShellCommand starts the configured Unix shell as a login shell so
// startup files such as ~/.zprofile and ~/.bash_profile can initialize PATH.
// This is especially important for the desktop app, whose process is usually
// launched by Finder or the Dock rather than from an already-initialized shell.
func terminalShellCommand(shell string) *exec.Cmd {
	return exec.Command(shell, "-l")
}

// isProcessDone reports whether a Kill error is just "the shell already
// exited", which is the expected case when the user types `exit`.
func isProcessDone(err error) bool {
	return errors.Is(err, os.ErrProcessDone)
}

// HandleTerminalProcesses returns a live snapshot of per-terminal CPU and
// memory usage. It exists so the Processes tab can render immediately on
// mount instead of waiting up to one poll interval for the next
// terminal_processes SSE envelope. The snapshot is sampled inline without
// reusing the emitter's Percent cache, so CPU% is expected to be 0 on this
// call — callers should treat memory as the authoritative instant value and
// let the SSE stream correct CPU on the next tick.
func (h *Handler) HandleTerminalProcesses(w http.ResponseWriter, r *http.Request) {
	entries := h.terminalProcs.snapshot()
	// Use a throwaway cache so this ad-hoc sample does not pollute the
	// emitter's long-lived Percent cache (Percent(0) needs consecutive calls
	// on the same Process handle to produce a meaningful delta).
	tmpCache := make(map[int32]*process.Process)
	touched := make(map[int32]bool)
	// Optional ?project= scoping so a per-project Processes panel does not
	// learn pids from other projects. Absent means global (backward compat for
	// callers that predate multi-project). Validation mirrors the terminal WS
	// project_path check.
	if proj := r.URL.Query().Get("project"); proj != "" {
		allowed := false
		for _, root := range h.allowedProjectRoots() {
			if proj == root {
				allowed = true
				break
			}
		}
		if !allowed {
			writeError(w, http.StatusForbidden, "project is not a project registered with this server")
			return
		}
		filtered := make(map[string]terminalProcEntry, len(entries))
		for id, e := range entries {
			if e.Project == proj {
				filtered[id] = e
			}
		}
		entries = filtered
	}
	stats := gatherProcessStats(entries, tmpCache, touched)
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(stats); err != nil {
		log.Printf("terminal processes: failed to encode response: %v", err)
	}
}
