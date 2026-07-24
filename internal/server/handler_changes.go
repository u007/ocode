package server

import (
	"errors"
	"net/http"

	"github.com/u007/ocode/internal/changes"
)

// changeAuthorDTO mirrors changes.ChangeAuthor for the web.
type changeAuthorDTO struct {
	AgentID   string `json:"agentId"`
	AgentName string `json:"agentName"`
	Changes   int    `json:"changes"`
}

// fileChangeDTO mirrors changes.FileChange for the web "changes" tab. Status
// uses word labels ("added"/"modified"/"deleted"), NOT changes.FileStatus's
// glyph-based String() ("+"/"M"/"-"), which is TUI-only.
type fileChangeDTO struct {
	OriginalPath     string            `json:"originalPath"`
	Status           string            `json:"status"`
	FirstBackupPath  string            `json:"firstBackupPath"`
	Undoable         bool              `json:"undoable"`
	UndoAllTCID      string            `json:"undoAllTcId"`
	ChangeCount      int               `json:"changeCount"`
	Authors          []changeAuthorDTO `json:"authors"`
	CreatedAt        string            `json:"createdAt"`
	UpdatedAt        string            `json:"updatedAt"`
	LastBashCommand  string            `json:"lastBashCommand"`
	LastBashExitCode int               `json:"lastBashExitCode"`
}

// statusName maps changes.FileStatus to the web's word-label contract.
func statusName(s changes.FileStatus) string {
	switch s {
	case changes.FileAdded:
		return "added"
	case changes.FileModified:
		return "modified"
	case changes.FileDeleted:
		return "deleted"
	default:
		return "modified"
	}
}

func buildFileChangeDTO(fc changes.FileChange) fileChangeDTO {
	authors := make([]changeAuthorDTO, 0, len(fc.Authors))
	for _, a := range fc.Authors {
		authors = append(authors, changeAuthorDTO{
			AgentID:   a.AgentID,
			AgentName: a.AgentName,
			Changes:   a.Changes,
		})
	}
	return fileChangeDTO{
		OriginalPath:     fc.OriginalPath,
		Status:           statusName(fc.Status),
		FirstBackupPath:  fc.FirstBackupPath,
		Undoable:         fc.Undoable,
		UndoAllTCID:      fc.UndoAllTCID,
		ChangeCount:      fc.ChangeCount,
		Authors:          authors,
		CreatedAt:        fc.CreatedAt.Format(timeFormatRFC3339),
		UpdatedAt:        fc.UpdatedAt.Format(timeFormatRFC3339),
		LastBashCommand:  fc.LastBashCommand,
		LastBashExitCode: fc.LastBashExitCode,
	}
}

const timeFormatRFC3339 = "2006-01-02T15:04:05.999999999Z07:00"

// changesSnapshot builds the DTO list for the active session's registry.
// Returns an empty slice (never nil) when no agent/registry is active — the
// same "legitimate empty state" contract runsSnapshot uses.
func (h *Handler) changesSnapshot(sessionID string) []fileChangeDTO {
	ag := h.activeAgentForRuns(sessionID)
	if ag == nil || ag.Changes() == nil {
		return []fileChangeDTO{}
	}
	list := ag.Changes().List()
	out := make([]fileChangeDTO, 0, len(list))
	for _, fc := range list {
		out = append(out, buildFileChangeDTO(fc))
	}
	return out
}

// HandleListChanges returns the current session's file-change list as JSON.
func (h *Handler) HandleListChanges(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.changesSnapshot(r.URL.Query().Get("session")))
}

// changeDiffDTO carries the unified diff for one file.
type changeDiffDTO struct {
	Path  string `json:"path"`
	Patch string `json:"patch"`
}

// HandleChangesDiff returns the unified diff for one file in the session's
// change list. 404 if path isn't currently in the list (stale client row).
func (h *Handler) HandleChangesDiff(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session")
	path := r.URL.Query().Get("path")

	list := h.changesSnapshot(sessionID)
	var found *fileChangeDTO
	for i := range list {
		if list[i].OriginalPath == path {
			found = &list[i]
			break
		}
	}
	if found == nil {
		writeError(w, http.StatusNotFound, "path not found in changes list")
		return
	}

	patch, err := changes.RenderDiff(found.FirstBackupPath, path)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "render diff: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, changeDiffDTO{Path: path, Patch: patch})
}

type undoChangeRequest struct {
	Path string `json:"path"`
}

// writeUndoError maps a changes.Registry undo error to an HTTP response.
// Errors from UndoFile/UndoBlock/LatestToolCall are wrapped via
// errors.Join, so callers MUST use errors.Is, never ==.
func writeUndoError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, changes.ErrNotUndoable):
		writeJSON(w, http.StatusConflict, map[string]string{"error": "not_undoable"})
	case errors.Is(err, changes.ErrNoChanges):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no_changes"})
	default:
		writeError(w, http.StatusBadRequest, err.Error())
	}
}

// HandleUndoChangeFile restores a file to its pre-session state.
func (h *Handler) HandleUndoChangeFile(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session")
	ag := h.activeAgentForRuns(sessionID)
	if ag == nil || ag.Changes() == nil {
		writeError(w, http.StatusNotFound, "no active agent for session")
		return
	}
	var req undoChangeRequest
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := ag.Changes().UndoFile(req.Path); err != nil {
		writeUndoError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{})
}

// HandleUndoChangeBlock undoes the most recent tool call on a file.
func (h *Handler) HandleUndoChangeBlock(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session")
	ag := h.activeAgentForRuns(sessionID)
	if ag == nil || ag.Changes() == nil {
		writeError(w, http.StatusNotFound, "no active agent for session")
		return
	}
	var req undoChangeRequest
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	tcid, err := ag.Changes().LatestToolCall(req.Path)
	if err != nil {
		writeUndoError(w, err)
		return
	}
	if err := ag.Changes().UndoBlock(req.Path, tcid); err != nil {
		writeUndoError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{})
}
