package server

import (
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/session"
)

// maxGeneratedTitleLen caps a server-generated session title. Mirrors the
// TUI's maxExplicitTitleLen so titles look consistent across surfaces.
const maxGeneratedTitleLen = 80

// titleGenState tracks sessions for which a background title-generation call
// is already running. The map lives on the Handler so concurrent chat turns
// (POST /api/sessions/{id}/message or SSE streams) can't double-fire title
// generation for the same session.
type titleGenState struct {
	mu     sync.Mutex
	active map[string]bool
}

func sessionTitleGenerationAllowed(s session.Session) bool {
	return !s.TitleGenerated
}

func newTitleGenState() *titleGenState {
	return &titleGenState{active: make(map[string]bool)}
}

// maybeGenerateSessionTitle mirrors the TUI's maybeGenerateTitle for the
// headless server path (web/desktop with no TUI attached). After the first
// assistant turn of an untitled session it kicks off a one-shot, background
// title-generation LLM call; on success it persists the title and broadcasts a
// "status" SSE event so connected browsers can update their tab titles live.
//
// It is deliberately a no-op when an RC bridge is attached: TUI-owned sessions
// are titled by the TUI, and the server must not generate a competing title.
// The call never blocks the chat turn — generation runs in a goroutine owned
// by agent.GenerateTitleAsync.
//
// The caller should hold as.mu (the turn is still executing under it), which
// makes reading as.messages safe.
func (h *Handler) maybeGenerateSessionTitle(sessionID string, as *agentSession) {
	if h.rc != nil {
		return
	}
	if as == nil || as.agent == nil {
		return
	}

	// Save derives a fallback title from the first user message, but
	// TitleGenerated distinguishes that fallback from an explicit or LLM title.
	// Only the latter should suppress generation.
	s, err := h.loadSession(sessionID)
	if err != nil {
		return
	}
	if !sessionTitleGenerationAllowed(*s) {
		return
	}

	// Extract the first user message and the latest assistant reply — the same
	// inputs the TUI uses for title generation.
	userMsg, assistantMsg := firstAndLastMessageTexts(as.messages)
	if strings.TrimSpace(userMsg) == "" {
		return
	}

	if !h.tryClaimTitleGen(sessionID) {
		return
	}

	as.agent.GenerateTitleAsync(userMsg, assistantMsg, func(title string) {
		h.finishSessionTitle(sessionID, title)
	})
}

// tryClaimTitleGen atomically marks sessionID as having a title-generation
// call in flight. Returns false (and claims nothing) if one is already
// running. Pure guard, separately testable.
func (h *Handler) tryClaimTitleGen(sessionID string) bool {
	h.titleGen.mu.Lock()
	defer h.titleGen.mu.Unlock()
	if h.titleGen.active[sessionID] {
		return false
	}
	h.titleGen.active[sessionID] = true
	return true
}

// finishSessionTitle persists a generated title (if non-empty) and broadcasts
// a fresh status snapshot with session_id + session_title so the web tab bar
// can update. Runs on the title-generation goroutine; all shared state is
// guarded.
func (h *Handler) finishSessionTitle(sessionID, title string) {
	h.titleGen.mu.Lock()
	delete(h.titleGen.active, sessionID)
	h.titleGen.mu.Unlock()

	if strings.TrimSpace(title) == "" {
		return
	}
	title = truncateSessionTitle(title, maxGeneratedTitleLen)

	// Persist with the *current* message list so the append-only ojsonl writer
	// can compute its delta correctly (passing a stale shorter list would
	// slice out of range). Lock the agent session to read it; if the session
	// has gone away, fall back to the last stored messages.
	h.mu.Lock()
	as := h.agents[sessionID]
	h.mu.Unlock()

	var msgs []agent.Message
	if as != nil {
		as.mu.Lock()
		msgs = append([]agent.Message(nil), as.messages...)
		as.mu.Unlock()
	} else {
		s, err := h.loadSession(sessionID)
		if err != nil {
			return
		}
		msgs = s.Messages
	}
	if err := h.saveSession(sessionID, title, msgs, nil); err != nil {
		log.Printf("serve: save generated title for %s: %v", sessionID, err)
		return
	}

	// Broadcast a status event carrying session_id + session_title so the web
	// can replace the tab label without polling. Context fields ride along
	// (via applySessionContext) — this broadcast lands right after the first
	// turn of a headless session, and a snapshot without them would wipe the
	// sidebar's Context gauge until the next per-session fetch.
	snap := h.buildStatusSnapshot()
	snap.SessionID = sessionID
	snap.SessionTitle = title
	if entry, err := h.sessions.Resolve(sessionID); err == nil && entry.ProjectRoot != "" {
		snap.CWD = entry.ProjectRoot
	}
	h.applySessionContext(&snap, sessionID)
	h.broadcastEvent(SSEEvent{SessionID: sessionID, Event: "status", Data: snap})
}

// firstAndLastMessageTexts returns the first user message text and the last
// assistant message text from a message list — the inputs for title
// generation, mirroring the TUI's usage.
func firstAndLastMessageTexts(msgs []agent.Message) (string, string) {
	var userMsg, assistantMsg string
	for _, m := range msgs {
		if m.Role == "user" && strings.TrimSpace(m.Content) != "" && userMsg == "" {
			userMsg = m.Content
		}
		if m.Role == "assistant" && strings.TrimSpace(m.Content) != "" {
			assistantMsg = m.Content
		}
	}
	return userMsg, assistantMsg
}

// truncateSessionTitle truncates a title to maxLen runes, appending "..." when
// cut. Kept local to the server package (the TUI has its own copy) to avoid a
// server → tui import.
func truncateSessionTitle(s string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen-3]) + "..."
}

// lastUserAndLastAssistantTexts returns the last user and last assistant
// message texts — the inputs for title regeneration (latest task), mirroring
// the TUI's regenerateTitle which uses lastUserMessageText +
// lastAssistantContent. Falls through to empty strings when no messages exist.
func lastUserAndLastAssistantTexts(msgs []agent.Message) (string, string) {
	var userMsg, assistantMsg string
	for i := len(msgs) - 1; i >= 0; i-- {
		m := msgs[i]
		if assistantMsg == "" && m.Role == "assistant" && strings.TrimSpace(m.Content) != "" {
			assistantMsg = m.Content
		}
		if userMsg == "" && m.Role == "user" && strings.TrimSpace(m.Content) != "" {
			userMsg = m.Content
		}
		if userMsg != "" && assistantMsg != "" {
			break
		}
	}
	// If we found no user message in reverse scan (e.g. no assistant yet),
	// still return the first user prompt so a single-turn session can title.
	if userMsg == "" {
		for _, m := range msgs {
			if m.Role == "user" && strings.TrimSpace(m.Content) != "" {
				userMsg = m.Content
				break
			}
		}
	}
	return userMsg, assistantMsg
}

// HandleGenerateSessionTitle regenerates the session title from the latest
// task (last user message + last assistant message), mirroring the TUI
// sidebar "✦ gen" button. It blocks up to ~20s for the LLM to return a title,
// persists it, broadcasts a status event, and returns the new title. Returns
// 409 if a generation is already in flight for this session.
func (h *Handler) HandleGenerateSessionTitle(w http.ResponseWriter, r *http.Request, id string) {
	if h.rc != nil {
		writeError(w, http.StatusConflict, "title generation owned by TUI")
		return
	}
	if !h.tryClaimTitleGen(id) {
		writeError(w, http.StatusConflict, "title generation already in progress")
		return
	}
	// Ensure we release the claim on every early return; finishSessionTitle
	// also releases it on success via its own delete.
	release := func() {
		h.titleGen.mu.Lock()
		delete(h.titleGen.active, id)
		h.titleGen.mu.Unlock()
	}

	s, err := h.loadSession(id)
	if err != nil {
		release()
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	// Prefer live agentSession messages (may include unsaved turn), else use disk.
	var msgs []agent.Message
	h.mu.Lock()
	as := h.agents[id]
	h.mu.Unlock()
	if as != nil {
		as.mu.Lock()
		msgs = append([]agent.Message(nil), as.messages...)
		as.mu.Unlock()
		// Ensure an agent exists to actually generate the title.
		if as.agent == nil {
			release()
			writeError(w, http.StatusServiceUnavailable, "no agent for session")
			return
		}
	} else {
		msgs = s.Messages
		// No live agent — try to build one from the session so headless-regenerate works.
		tmpAS, err := h.getOrCreateAgentSession(id)
		if err != nil || tmpAS == nil || tmpAS.agent == nil {
			release()
			writeError(w, http.StatusServiceUnavailable, "no agent for session")
			return
		}
		as = tmpAS
	}

	userMsg, assistantMsg := lastUserAndLastAssistantTexts(msgs)
	if strings.TrimSpace(userMsg) == "" {
		release()
		writeError(w, http.StatusBadRequest, "no conversation to title")
		return
	}

	// Branch: if we already claimed, finishSessionTitle will delete the entry.
	// Generate synchronously by waiting on GenerateTitleAsync.
	done := make(chan string, 1)
	as.agent.GenerateTitleAsync(userMsg, assistantMsg, func(title string) {
		done <- title
	})

	var title string
	select {
	case title = <-done:
	case <-time.After(20 * time.Second):
		release()
		writeError(w, http.StatusGatewayTimeout, "title generation timed out")
		return
	case <-r.Context().Done():
		release()
		return
	}

	if strings.TrimSpace(title) == "" {
		release()
		writeError(w, http.StatusInternalServerError, "title generation returned empty")
		return
	}
	title = truncateSessionTitle(title, maxGeneratedTitleLen)

	// Persist with the current message list and broadcast.
	// Reuse finishSessionTitle's save+broadcast but avoid double-delete of the
	// titleGen entry: finishSessionTitle deletes it, so we must NOT have already
	// deleted. We claimed above and haven't released on success, so let it delete.
	// To avoid it reading stale msgs, we pass through its normal path which
	// re-reads the live agentSession.
	h.finishSessionTitle(id, title)
	writeJSON(w, http.StatusOK, map[string]string{"title": title})
}
