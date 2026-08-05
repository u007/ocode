package server

import (
	"log"
	"strings"
	"sync"

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
	s, err := session.Load(sessionID)
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
		s, err := session.Load(sessionID)
		if err != nil {
			return
		}
		msgs = s.Messages
	}
	if err := session.Save(sessionID, title, msgs, nil); err != nil {
		log.Printf("serve: save generated title for %s: %v", sessionID, err)
		return
	}

	// Broadcast a status event carrying session_id + session_title so the web
	// can replace the tab label without polling.
	snap := h.buildStatusSnapshot()
	snap.SessionID = sessionID
	snap.SessionTitle = title
	h.broadcastEvent(SSEEvent{Event: "status", Data: snap})
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
