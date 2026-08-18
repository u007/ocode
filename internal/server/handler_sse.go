package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/session"
)

type SSEEvent struct {
	// SessionID is transport metadata used to route events to the correct web
	// session. It is not part of the internal event JSON payload; the SSE
	// writer emits it as the standard `id` field so existing event payloads stay
	// backwards-compatible.
	SessionID string      `json:"-"`
	Event     string      `json:"event"`
	Data      interface{} `json:"data"`
}

type TextDelta struct {
	Delta string `json:"delta"`
}

type ToolStartEvent struct {
	Tool    string `json:"tool"`
	Command string `json:"command,omitempty"`
	Content string `json:"content,omitempty"`
}

type ToolResultEvent struct {
	Tool   string `json:"tool"`
	Output string `json:"output"`
}

type ToolErrorEvent struct {
	Tool  string `json:"tool"`
	Error string `json:"error"`
}

// PermissionCheckEvent mirrors an OnPermissionCheck callback: the auto-
// permission LLM judge started (Active true) or finished (Active false)
// deciding whether Tool may run, using Model as the judge.
type PermissionCheckEvent struct {
	Tool   string `json:"tool"`
	Model  string `json:"model"`
	Active bool   `json:"active"`
}

type DoneEvent struct {
	SessionID string `json:"session_id"`
	Model     string `json:"model"`
}

func (h *Handler) HandleChatStream(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session")
	message := r.URL.Query().Get("message")

	if message == "" {
		writeError(w, http.StatusBadRequest, "message is required")
		return
	}

	model := r.URL.Query().Get("model")
	if model == "" {
		model = h.cfg.Model
	}
	if model == "" {
		writeError(w, http.StatusBadRequest, "no model configured")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	// If we have an RC bridge, forward to the TUI's agent instead of using our own.
	if rc := h.RCBridge(); rc != nil {
		// Send session info
		sendSSE(w, flusher, "session", map[string]string{"session_id": rc.SessionID})

		// Create a streaming channel to relay live events from the TUI
		streamCh := make(chan SSEEvent, 32)
		resultCh := make(chan RCResult, 1)

		// Send request to TUI. External clients (e.g. the Telegram bot) pass
		// ?remote=telegram so the TUI knows to handle permission/question asks
		// remotely instead of opening its local dialog (which would otherwise
		// pause the turn with no one at the terminal to approve).
		select {
		case rc.RcCh <- RCRequest{Content: message, StreamCh: streamCh, ResultCh: resultCh, RemoteApproval: r.URL.Query().Get("remote") == "telegram"}:
		case <-time.After(5 * time.Second):
			writeError(w, http.StatusServiceUnavailable, "TUI is busy, try again")
			return
		}

		// Relay streaming events until done
		for {
			select {
			case event, ok := <-streamCh:
				if !ok {
					// Stream channel closed, wait for final result
					select {
					case result := <-resultCh:
						if result.Error != nil {
							sendSSE(w, flusher, "error", map[string]string{"error": result.Error.Error()})
							return
						}
						sendSSE(w, flusher, "done", DoneEvent{
							SessionID: rc.SessionID,
							Model:     rc.Model,
						})
						return
					case <-time.After(30 * time.Second):
						sendSSE(w, flusher, "error", map[string]string{"error": "timed out waiting for agent response"})
						return
					}
				}
				sendSSE(w, flusher, event.Event, event.Data)
			case result := <-resultCh:
				// Drain any remaining stream events
				for done := false; !done; {
					select {
					case ev := <-streamCh:
						sendSSE(w, flusher, ev.Event, ev.Data)
					default:
						done = true
					}
				}
				if result.Error != nil {
					sendSSE(w, flusher, "error", map[string]string{"error": result.Error.Error()})
					return
				}
				sendSSE(w, flusher, "done", DoneEvent{
					SessionID: rc.SessionID,
					Model:     rc.Model,
				})
				return
			case <-time.After(5 * time.Minute):
				sendSSE(w, flusher, "error", map[string]string{"error": "agent response timed out"})
				return
			}
		}
	}

	as := h.lookupAgentSession(sessionID)

	if as == nil {
		if sessionID == "" {
			sessionID = session.NewSessionID()
		}

		var messages []agent.Message
		projectRoot := ""
		if entry, err := h.sessions.Resolve(sessionID); err == nil {
			projectRoot = entry.ProjectRoot
			if s, err := session.LoadForDir(entry.ProjectRoot, sessionID); err == nil {
				messages = s.Messages
			}
		}

		// Built with no handler lock held — see agent_session.go. The resolved
		// project root keeps the agent bound to the session's owning project;
		// a brand-new (unresolvable) session on this legacy endpoint binds to
		// the process workdir as before.
		var err error
		var stage string
		as, stage, err = h.ensureAgentSession(sessionID, model, messages, projectRoot)
		if err != nil {
			h.publishTurnError(sessionID, err, stage)
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	as.mu.Lock()
	defer as.mu.Unlock()

	as.messages = append(as.messages, agent.Message{Role: "user", Content: message})
	messages := append([]agent.Message(nil), as.messages...)
	ag := as.agent
	sessModel := as.model

	sendSSE(w, flusher, "session", map[string]string{"session_id": sessionID})

	// Wire up streaming callbacks so events fire during Step()
	ag.OnDelta = func(kind, text string) {
		if kind == "text" {
			sendSSE(w, flusher, "text", TextDelta{Delta: text})
		}
	}
	ag.OnMessage = func(m agent.Message) {
		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			for _, tc := range m.ToolCalls {
				sendSSE(w, flusher, "tool_start", ToolStartEvent{
					Tool:    tc.Function.Name,
					Command: tc.Function.Arguments,
				})
			}
		}
		if m.Role == "tool" {
			sendSSE(w, flusher, "tool_result", ToolResultEvent{
				Tool:   "tool",
				Output: m.Content,
			})
		}
	}

	resp, err := ag.Step(messages)
	if err != nil {
		log.Printf("serve error: agent step: %v", err)
		sendSSE(w, flusher, "error", map[string]string{"error": err.Error()})
		return
	}

	as.messages = append(as.messages, resp...)
	_ = session.Save(sessionID, "", as.messages, nil)

	// Headless-only: generate a title for an untitled session after its first
	// turn (mirrors the TUI; no-op when an RC bridge is attached).
	h.maybeGenerateSessionTitle(sessionID, as)

	sendSSE(w, flusher, "done", DoneEvent{
		SessionID: sessionID,
		Model:     sessModel,
	})

	// Post-turn auto-compaction check (mirrors runTurn).
	ag.MaybeCompactAsync(as.messages)
}

// HandleSessionMessages is the persistent live mirror of the bridged TUI session.
// On connect it sends the full message list (history), then forwards every live
// event the TUI broadcasts — user messages, thinking/text token deltas, tool
// activity, and an authoritative "messages" snapshot at each turn boundary. This
// is what makes /rc a 2-way live mirror: activity originating in the TUI (or any
// other browser) reaches every connected browser here in real time.
func (h *Handler) HandleSessionMessages(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	h.mu.Lock()
	rc := h.rc
	h.mu.Unlock()

	sessionID := r.URL.Query().Get("session")

	// Optional allowlist of event names (?events=status,turn_done). When set,
	// only those events are forwarded — lets the web tab bar subscribe to
	// status updates without receiving every message/tool payload on the bus.
	var eventFilter map[string]bool
	if ev := r.URL.Query().Get("events"); ev != "" {
		eventFilter = make(map[string]bool)
		for _, name := range strings.Split(ev, ",") {
			if name = strings.TrimSpace(name); name != "" {
				eventFilter[name] = true
			}
		}
	}
	forward := func(ev SSEEvent) {
		if eventFilter != nil && !eventFilter[ev.Event] {
			return
		}
		if sessionID != "" && ev.SessionID != "" && ev.SessionID != sessionID {
			return
		}
		sendSSEWithSession(w, flusher, ev.SessionID, ev.Event, ev.Data)
	}

	if rc != nil {
		// RC bridge mode: subscribe to the TUI's broadcast channel.
		sub := rc.Subscribe()
		defer rc.Unsubscribe(sub)

		// Send the current history immediately so a freshly-loaded (or reconnecting)
		// browser is in sync before live events start flowing.
		forward(SSEEvent{SessionID: rc.SessionID, Event: "messages", Data: rc.GetMessages()})

		ctx := r.Context()
		for {
			select {
			case <-ctx.Done():
				return
			case ev := <-sub:
				// Part 06: every bridge frame must arrive tagged with its real
				// session id — RCBridge.Broadcast stamps at source and the old
				// re-stamping compensation is gone. An untagged frame is a
				// producer bug; drop it loudly instead of guessing which
				// session it belongs to.
				if ev.SessionID == "" {
					log.Printf("rc bridge: ERROR dropping untagged frame %q at mirror (no session id)", ev.Event)
					continue
				}
				forward(ev)
			}
		}
	}

	// Headless/serve mode: subscribe to the handler's local event bus before
	// loading history, so events produced during the disk read stay queued.
	sub := h.subscribeHeadless()
	defer h.unsubscribeHeadless(sub)

	// Load current messages from disk so the browser gets history immediately.
	var initMsgs []agent.Message
	if sessionID != "" {
		if entry, err := h.sessions.Resolve(sessionID); err == nil {
			if s, err := session.LoadForDir(entry.ProjectRoot, sessionID); err == nil {
				initMsgs = s.Messages
			}
		}
	}
	forward(SSEEvent{SessionID: sessionID, Event: "messages", Data: initMsgs})

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-sub:
			forward(ev)
		}
	}
}

func sendSSE(w http.ResponseWriter, flusher http.Flusher, event string, data interface{}) {
	sendSSEWithSession(w, flusher, "", event, data)
}

func sendSSEWithSession(w http.ResponseWriter, flusher http.Flusher, sessionID, event string, data interface{}) {
	jsonData, _ := json.Marshal(data)
	// Always emit an id line. An empty id resets EventSource.lastEventId so an
	// untagged status/history frame cannot inherit the preceding session.
	fmt.Fprintf(w, "id: %s\n", sessionID)
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, jsonData)
	flusher.Flush()
}
