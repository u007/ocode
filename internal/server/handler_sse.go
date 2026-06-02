package server

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/jamesmercstudio/ocode/internal/agent"
	"github.com/jamesmercstudio/ocode/internal/session"
	"github.com/jamesmercstudio/ocode/internal/tool"
)

type SSEEvent struct {
	Event string      `json:"event"`
	Data  interface{} `json:"data"`
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

	model := h.cfg.Model
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

	h.mu.Lock()

	var as *agentSession
	if sessionID != "" {
		as = h.agents[sessionID]
	}

	if as == nil {
		if sessionID == "" {
			sessionID = session.NewSessionID()
		}

		var messages []agent.Message
		if sessionID != "" {
			s, err := session.Load(sessionID)
			if err == nil {
				messages = s.Messages
			}
		}

		client := agent.NewClient(h.cfg, model)
		if client == nil {
			h.mu.Unlock()
			writeError(w, http.StatusInternalServerError, "failed to create LLM client")
			return
		}

		tools, _ := tool.LoadBuiltins(h.cfg)
		ag := agent.NewAgent(client, tools, h.cfg)
		ag.LoadExternalTools(h.cfg)

		as = &agentSession{
			agent:    ag,
			messages: messages,
			model:    model,
		}
		h.agents[sessionID] = as
	}

	as.messages = append(as.messages, agent.Message{Role: "user", Content: message})
	messages := as.messages
	ag := as.agent
	sessModel := as.model
	h.mu.Unlock()

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
		sendSSE(w, flusher, "error", map[string]string{"error": err.Error()})
		return
	}

	h.mu.Lock()
	as.messages = append(as.messages, resp...)
	_ = session.Save(sessionID, "", as.messages, nil)
	h.mu.Unlock()

	sendSSE(w, flusher, "done", DoneEvent{
		SessionID: sessionID,
		Model:     sessModel,
	})
}

func sendSSE(w http.ResponseWriter, flusher http.Flusher, event string, data interface{}) {
	jsonData, _ := json.Marshal(data)
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, jsonData)
	flusher.Flush()
}
