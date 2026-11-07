package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	gopsprocess "github.com/shirou/gopsutil/v4/process"
	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/crashguard"
	"github.com/u007/ocode/internal/paths"
	"github.com/u007/ocode/internal/session"
)

// tuiSessionStatusPath is where the TUI writes live turn state.
func tuiSessionStatusPath(id string) string {
	gd, err := paths.GlobalDataDir()
	if err != nil {
		return ""
	}
	return filepath.Join(gd, "session-status", id+".json")
}

// tuiStatusTTL is how old a TUI status record may be and still count as a live
// turn. A running turn rewrites its record on every auto-refresh tick (10 s).
const tuiStatusTTL = 2 * time.Minute

// tuiSessionStatus is the on-disk record the TUI writes (internal/tui/session_status_file.go).
type tuiSessionStatus struct {
	SessionID  string `json:"session_id"`
	TurnActive bool   `json:"turn_active"`
	PID        int    `json:"pid"`
	UpdatedAt  string `json:"updated_at"`
}

// tuiTurnActiveFromFile reports whether the TUI status record at path describes a
// live turn: turn_active is set, updated_at is within tuiStatusTTL of now, and
// the recorded pid is still running. A crashed or switched-away TUI leaves
// turn_active=true behind; the staleness and pid checks stop that reading as live.
func tuiTurnActiveFromFile(path string, now time.Time) bool {
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var st tuiSessionStatus
	if json.Unmarshal(b, &st) != nil || !st.TurnActive {
		return false
	}
	at, err := time.Parse(time.RFC3339Nano, st.UpdatedAt)
	if err != nil || now.Sub(at) > tuiStatusTTL {
		return false
	}
	return st.PID > 0 && tuiPIDAlive(st.PID)
}

// tuiPIDAlive reports whether pid is running. When the process table cannot be
// read it returns true, so the TTL alone decides rather than a guess.
func tuiPIDAlive(pid int) bool {
	exists, err := gopsprocess.PidExists(int32(pid))
	if err != nil {
		return true
	}
	return exists
}

// HandleSessionCommStatus returns a compact inter-session communication snapshot.
func (h *Handler) HandleSessionCommStatus(w http.ResponseWriter, r *http.Request, id string) {
	entry, err := h.sessions.Resolve(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	project := entry.ProjectRoot
	if project == "" {
		project = h.workDir
	}
	var msgs []agent.Message
	turnActive := h.sessions.IsTurnActive(id)
	if as := h.lookupAgentSession(id); as != nil && as.agent != nil && as.mu.TryLock() {
		msgs = append([]agent.Message(nil), as.messages...)
		as.mu.Unlock()
	} else if p := tuiSessionStatusPath(id); p != "" {
		turnActive = turnActive || tuiTurnActiveFromFile(p, time.Now())
	}
	if msgs == nil {
		if s, lerr := session.LoadForDir(project, id); lerr == nil && s != nil {
			msgs = s.Messages
		}
	}
	title := ""
	if t, found, terr := session.TitleForDir(project, id); terr == nil && found {
		title = t
	}
	lastAssistant := ""
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "assistant" && msgs[i].Content != "" {
			lastAssistant = msgs[i].Content
			break
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"session_id":    id,
		"project_path":  project,
		"title":         title,
		"turn_active":   turnActive,
		"goal":          h.pulseSessionGoal(id, project),
		"todo":          h.pulseSessionTodo(project, id),
		"last_message":  lastAssistant,
		"message_count": len(msgs),
	})
}

// maxQueuedMessages caps one session's pending queue for queue requests.
const maxQueuedMessages = 10

// Drainer poll backoff: starts fast, doubles to a ceiling while the session is busy.
const (
	queueDrainMinBackoff = 200 * time.Millisecond
	queueDrainMaxBackoff = 2 * time.Second
)

// queueError is a refusal of a queue request, carrying the HTTP status it maps to.
type queueError struct {
	status int
	msg    string
}

func (e *queueError) Error() string { return e.msg }

// HandleSessionQueue queues a message for a session that is busy here. Its
// refusals (unknown session, no resident agent, full queue) map to 404, 409 and
// 429; see enqueueQueuedMessage.
func (h *Handler) HandleSessionQueue(w http.ResponseWriter, r *http.Request, id string) {
	var req struct {
		Content string `json:"content"`
	}
	if err := readBodyJSON(r, &req); err != nil || req.Content == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}
	if err := h.enqueueQueuedMessage(id, req.Content); err != nil {
		var qe *queueError
		if errors.As(err, &qe) {
			writeError(w, qe.status, qe.msg)
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"session_id": id, "queued": true})
}

// enqueueQueuedMessage is the one entry point for queue requests (the HTTP
// route and session_send with queue=true). It refuses a target with no resident
// agent on this server, since a TUI-owned or disk-only session must not be
// written from here. It reserves a cap slot atomically, persists the message,
// and only then starts the drainer. A failed persist releases the slot, so the
// queue stays one-to-one with the persisted rows that bootstrap strips.
func (h *Handler) enqueueQueuedMessage(id, content string) error {
	entry, err := h.sessions.Resolve(id)
	if err != nil {
		return &queueError{status: http.StatusNotFound, msg: "session not found"}
	}
	if !h.hasResidentAgent(id) {
		return &queueError{status: http.StatusConflict, msg: fmt.Sprintf(
			"session %s has no resident agent on this server (it is idle on disk or owned by the TUI); queueing is refused, so send it once the session is running here", id)}
	}
	if !h.sessions.TryPushPending(id, content, maxQueuedMessages) {
		return &queueError{status: http.StatusTooManyRequests, msg: fmt.Sprintf("session queue is full (%d pending)", maxQueuedMessages)}
	}
	if err := h.persistUserMessage(entry, content); err != nil {
		h.sessions.RemovePendingLast(id, content)
		return fmt.Errorf("persist queued message: %w", err)
	}
	h.scheduleQueueDrain(id)
	return nil
}

// queueDrainState is the bookkeeping for one session's drainer. kick records a
// queue request that arrived while the drainer was running, so it re-checks
// before it exits.
type queueDrainState struct {
	kick bool
}

// scheduleQueueDrain starts the session's drainer, or marks the running one to
// re-check. There is at most one drainer per session.
func (h *Handler) scheduleQueueDrain(id string) {
	h.drainMu.Lock()
	if h.queueDrainers == nil {
		h.queueDrainers = make(map[string]*queueDrainState)
	}
	if st := h.queueDrainers[id]; st != nil {
		st.kick = true
		h.drainMu.Unlock()
		return
	}
	h.queueDrainers[id] = &queueDrainState{}
	h.drainMu.Unlock()
	crashguard.Go(func() { h.runQueueDrainer(id) })
}

// runQueueDrainer dispatches a drain turn for the session's persisted queue
// whenever the session is idle, until the queue is empty. It waits out a
// running turn, a compaction or a pending ask, so it never dispatches onto a
// paused session. A dispatched turn that consumes no queued message (cancelled,
// failed or paused) ends the drain; the next turn or queue request drains the
// rest. There is no wall-clock deadline: it stops when the queue empties, the
// resident agent is gone, or the server shuts down.
func (h *Handler) runQueueDrainer(id string) {
	backoff := queueDrainMinBackoff
	for {
		if !h.queueWorkRemains(id) {
			if h.retireQueueDrainer(id) {
				return
			}
			continue
		}
		if h.pulseSessionBusy(id) != nil {
			time.Sleep(backoff)
			backoff = nextQueueBackoff(backoff)
			continue
		}
		before := h.sessions.PendingCount(id)
		model := h.effectiveSessionModel(id)
		if _, err := h.dispatchTurn(id, model, "", turnOptions{drainPending: true, refuseIfBusy: true}); err != nil {
			// errTurnInFlight (a human send raced us) or shutdown: try again later.
			time.Sleep(backoff)
			backoff = nextQueueBackoff(backoff)
			continue
		}
		backoff = queueDrainMinBackoff
		for h.queueWorkRemains(id) && h.pulseSessionBusy(id) != nil {
			time.Sleep(backoff)
			backoff = nextQueueBackoff(backoff)
		}
		backoff = queueDrainMinBackoff
		if h.sessions.PendingCount(id) >= before {
			if h.retireQueueDrainer(id) {
				return
			}
		}
	}
}

// queueWorkRemains reports whether a drainer should keep going for id: queued
// messages exist, a resident agent can run them, and the server is not shutting down.
func (h *Handler) queueWorkRemains(id string) bool {
	return h.sessions.PendingCount(id) > 0 && h.hasResidentAgent(id) && !h.shutdownBegun()
}

// retireQueueDrainer removes id's drainer entry and reports true, unless a queue
// request arrived since the drainer last looked and work still remains. In that
// case it consumes the kick and reports false so the caller runs another pass.
// The re-check and the removal share drainMu, so no push is left behind.
func (h *Handler) retireQueueDrainer(id string) bool {
	h.drainMu.Lock()
	defer h.drainMu.Unlock()
	if st := h.queueDrainers[id]; st != nil && st.kick && h.queueWorkRemains(id) {
		st.kick = false
		return false
	}
	delete(h.queueDrainers, id)
	return true
}

// hasResidentAgent reports whether this server holds a live agent for id.
func (h *Handler) hasResidentAgent(id string) bool {
	as := h.lookupAgentSession(id)
	return as != nil && as.agent != nil
}

// shutdownBegun reports whether Shutdown has started.
func (h *Handler) shutdownBegun() bool {
	h.shutdownMu.Lock()
	defer h.shutdownMu.Unlock()
	return h.shutdownStarted
}

func nextQueueBackoff(d time.Duration) time.Duration {
	if d *= 2; d > queueDrainMaxBackoff {
		return queueDrainMaxBackoff
	}
	return d
}
