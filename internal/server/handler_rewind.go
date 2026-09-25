package server

import (
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/session"
)

const pendingRewindTokenBytes = 32

var commitPendingRewindForDir = session.CommitPendingRewindForDir

// pendingRewindDTO is the client-safe view of a durable rewind resource. The
// fingerprint and target content remain server-only capabilities; returning
// either would let a client infer or replay more history than the API contract
// intentionally exposes.
type pendingRewindDTO struct {
	Token            string                      `json:"token"`
	SessionID        string                      `json:"session_id"`
	Status           session.PendingRewindStatus `json:"status"`
	ExpiresAt        time.Time                   `json:"expires_at"`
	TargetIndex      int                         `json:"target_index"`
	UserSeq          int                         `json:"user_seq,omitempty"`
	CommittedUserSeq int                         `json:"committed_user_seq"`
}

type pendingRewindPrepareRequest struct {
	TargetIndex      *int    `json:"targetIndex"`
	TargetIndexSnake *int    `json:"target_index"`
	TargetContent    *string `json:"targetContent"`
	TargetContentAlt *string `json:"target_content"`
	UserSeq          *int    `json:"userSeq"`
	UserSeqSnake     *int    `json:"user_seq"`
}

func (r pendingRewindPrepareRequest) values() (targetIndex int, targetContent string, userSeq int, err error) {
	if r.TargetIndex == nil && r.TargetIndexSnake == nil {
		return 0, "", 0, errors.New("targetIndex is required")
	}
	if r.TargetIndex != nil && r.TargetIndexSnake != nil && *r.TargetIndex != *r.TargetIndexSnake {
		return 0, "", 0, errors.New("targetIndex values conflict")
	}
	if r.TargetIndex != nil {
		targetIndex = *r.TargetIndex
	} else {
		targetIndex = *r.TargetIndexSnake
	}

	if r.TargetContent == nil && r.TargetContentAlt == nil {
		return 0, "", 0, errors.New("targetContent is required")
	}
	if r.TargetContent != nil && r.TargetContentAlt != nil && *r.TargetContent != *r.TargetContentAlt {
		return 0, "", 0, errors.New("targetContent values conflict")
	}
	if r.TargetContent != nil {
		targetContent = *r.TargetContent
	} else {
		targetContent = *r.TargetContentAlt
	}

	if r.UserSeq != nil && r.UserSeqSnake != nil && *r.UserSeq != *r.UserSeqSnake {
		return 0, "", 0, errors.New("userSeq values conflict")
	}
	if r.UserSeq != nil {
		userSeq = *r.UserSeq
	} else if r.UserSeqSnake != nil {
		userSeq = *r.UserSeqSnake
	}
	if targetIndex < 0 || userSeq < 0 {
		return 0, "", 0, errors.New("targetIndex and userSeq must be non-negative")
	}
	return targetIndex, targetContent, userSeq, nil
}

func newPendingRewindDTO(resource *session.PendingRewind) pendingRewindDTO {
	return pendingRewindDTO{
		Token:            resource.Token,
		SessionID:        resource.SessionID,
		Status:           resource.Status,
		ExpiresAt:        resource.ExpiresAt,
		TargetIndex:      resource.TargetIndex,
		UserSeq:          resource.UserSeq,
		CommittedUserSeq: resource.CommittedUserSeq,
	}
}

func validPendingRewindToken(token string) bool {
	if len(token) != pendingRewindTokenBytes*2 {
		return false
	}
	_, err := hex.DecodeString(token)
	return err == nil
}

func pendingRewindHTTPError(err error) (int, string) {
	switch {
	case errors.Is(err, session.ErrPendingRewindInvalidTarget):
		return http.StatusBadRequest, "invalid pending rewind target"
	case errors.Is(err, session.ErrPendingRewindNotFound), errors.Is(err, session.ErrNoStoredSession):
		return http.StatusNotFound, "pending rewind not found"
	case errors.Is(err, session.ErrPendingRewindStale):
		return http.StatusConflict, "pending rewind is stale"
	case errors.Is(err, session.ErrPendingRewindExpired):
		return http.StatusGone, "pending rewind has expired"
	case errors.Is(err, session.ErrPendingRewindAlreadyCommitted):
		return http.StatusGone, "pending rewind has already been committed"
	default:
		return http.StatusInternalServerError, "failed to access pending rewind"
	}
}

func writePendingRewindError(w http.ResponseWriter, action, sessionID string, err error) {
	status, message := pendingRewindHTTPError(err)
	// Keep the capability token and target content out of logs. These fields
	// are sufficient to correlate a failed durable operation with its session.
	if status == http.StatusInternalServerError {
		log.Printf("server: pending_rewind action=%q session_id=%q status=%d error=%v", action, sessionID, status, err)
	} else {
		log.Printf("server: pending_rewind action=%q session_id=%q status=%d error_type=%T", action, sessionID, status, err)
	}
	writeError(w, status, message)
}

func (h *Handler) resolvePendingRewindSession(w http.ResponseWriter, id string) (*sessionEntry, bool) {
	entry, err := h.sessions.Resolve(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return nil, false
	}
	return entry, true
}

// HandlePreparePendingRewind arms the one durable rewind resource for a
// session. The turn lock is acquired only after project resolution and before
// reading turn state or touching the session store, matching executeTurnJob's
// lock order.
func (h *Handler) HandlePreparePendingRewind(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	entry, ok := h.resolvePendingRewindSession(w, id)
	if !ok {
		return
	}

	var req pendingRewindPrepareRequest
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	targetIndex, targetContent, userSeq, err := req.values()
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	lock := h.sessionTurnLock(id)
	lock.Lock()
	defer lock.Unlock()
	if h.sessions.IsTurnActive(id) {
		writeError(w, http.StatusConflict, "cannot prepare rewind while turn is active")
		return
	}

	resource, err := session.PreparePendingRewindForDir(entry.ProjectRoot, id, targetIndex, targetContent, userSeq)
	if err != nil {
		writePendingRewindError(w, "prepare", id, err)
		return
	}
	writeJSON(w, http.StatusOK, newPendingRewindDTO(resource))
}

// HandlePendingRewindStatus is intentionally read-only: it does not take the
// per-session turn lock and therefore cannot block a running turn.
func (h *Handler) HandlePendingRewindStatus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	entry, ok := h.resolvePendingRewindSession(w, id)
	if !ok {
		return
	}
	token := r.PathValue("token")
	if !validPendingRewindToken(token) {
		writeError(w, http.StatusBadRequest, "invalid pending rewind token")
		return
	}

	resource, err := session.PendingRewindStatusForDir(entry.ProjectRoot, id, token)
	if err != nil {
		writePendingRewindError(w, "status", id, err)
		return
	}
	writeJSON(w, http.StatusOK, newPendingRewindDTO(resource))
}

// HandleCancelPendingRewind removes an armed or stale resource without
// changing the transcript. A committed or expired resource is consumed and
// cannot be cancelled, so it is reported as 410.
func (h *Handler) HandleCancelPendingRewind(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	entry, ok := h.resolvePendingRewindSession(w, id)
	if !ok {
		return
	}
	token := r.PathValue("token")
	if !validPendingRewindToken(token) {
		writeError(w, http.StatusBadRequest, "invalid pending rewind token")
		return
	}

	lock := h.sessionTurnLock(id)
	lock.Lock()
	defer lock.Unlock()
	if h.sessions.IsTurnActive(id) {
		writeError(w, http.StatusConflict, "cannot cancel rewind while turn is active")
		return
	}

	resource, err := session.PendingRewindStatusForDir(entry.ProjectRoot, id, token)
	if err != nil {
		writePendingRewindError(w, "cancel", id, err)
		return
	}
	if resource.Status == session.PendingRewindStatusCommitted {
		writeError(w, http.StatusGone, "pending rewind has already been committed")
		return
	}
	if err := session.CancelPendingRewindForDir(entry.ProjectRoot, id, token); err != nil {
		writePendingRewindError(w, "cancel", id, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"cancelled": true})
}

func pendingRewindCommittedMessages(result *session.PendingRewindCommitResult, content string) ([]agent.Message, error) {
	if result == nil || result.Rewind.Status != session.PendingRewindStatusCommitted {
		return nil, fmt.Errorf("pending rewind commit returned no committed resource")
	}
	messages := append([]agent.Message(nil), result.KeptPrefix...)
	return append(messages, agent.Message{
		Role:    "user",
		Content: content,
		UserSeq: result.Rewind.CommittedUserSeq,
	}), nil
}
