package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"

	"github.com/u007/ocode/internal/agent"
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
	} else {
		if p := tuiSessionStatusPath(id); p != "" {
			if b, err := os.ReadFile(p); err == nil {
				var st struct {
					TurnActive bool `json:"turn_active"`
				}
				if json.Unmarshal(b, &st) == nil {
					turnActive = st.TurnActive
				}
			}
		}
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

// HandleSessionQueue persists a message onto a session's pending queue.
func (h *Handler) HandleSessionQueue(w http.ResponseWriter, r *http.Request, id string) {
	var req struct {
		Content string `json:"content"`
	}
	if err := readBodyJSON(r, &req); err != nil || req.Content == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}
	entry, err := h.sessions.Resolve(id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	project := entry.ProjectRoot
	if project == "" {
		project = h.workDir
	}
	if h.sessions.PendingCount(id) >= 10 {
		writeError(w, http.StatusTooManyRequests, "session queue is full")
		return
	}
	if err := h.persistUserMessage(entry, req.Content); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.sessions.PushPending(id, req.Content)
	h.scheduleQueueDrain(id)
	writeJSON(w, http.StatusAccepted, map[string]any{"session_id": id, "queued": true})
}
