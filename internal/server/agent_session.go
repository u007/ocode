package server

import (
	"fmt"
	"log"
	"strings"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/session"
	"github.com/u007/ocode/internal/tool"
)

// This file owns the lifecycle of per-session agents and the execution of a
// single turn.
//
// The invariant every caller must respect: **h.mu is a short-lived map lock,
// never a work lock.** It may only be held while looking a session up in
// h.agents or inserting one — never across agent construction, an LLM call, a
// Step, or a compaction. Holding it across slow work serializes the whole
// server: every other session's send, the run-state polls, the config
// endpoints and the desktop shell's dock badge all take h.mu, so one busy
// session makes every other session look stuck. That was the original
// "session doesn't run while another session is running" bug.

// lookupAgentSession returns the live agent session for id, or nil. It holds
// h.mu only for the map read.
func (h *Handler) lookupAgentSession(id string) *agentSession {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.agents[id]
}

// advisorFlag reads the shared advisor gate under h.mu (it is flipped from the
// web sidebar via handler_config.go).
func (h *Handler) advisorFlag() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.advisorEnabled
}

// buildAgentSession constructs a fresh agent session. **It must be called with
// no handler lock held**: every step here can block for a long time —
// InitBuiltinTools and LoadExternalTools touch the filesystem and can spawn
// plugin processes, NewAgent may auto-start a local model server, and
// mcpCache.wait() blocks until the process-wide MCP enumeration finishes
// (unbounded, and an unreachable MCP server makes it slow).
func (h *Handler) buildAgentSession(model string, messages []agent.Message) (*agentSession, error) {
	if model == "" {
		return nil, fmt.Errorf("no model configured")
	}
	client := agent.NewClient(h.cfg, model)
	if client == nil {
		return nil, fmt.Errorf("failed to create LLM client")
	}

	lspMgr := h.sharedLSPManager()
	tools := tool.InitBuiltinTools(lspMgr, h.cfg, h.scheduler)
	ag := agent.NewAgent(client, tools, h.cfg, lspMgr)
	ag.LoadExternalTools(h.cfg)
	mcpTools, mcpErrs := h.mcpCache.wait()
	ag.AddMCPTools(mcpTools)
	ag.AddMCPErrors(mcpErrs)
	ag.SetAdvisorEnabled(h.advisorFlag())

	return &agentSession{agent: ag, messages: messages, model: model}, nil
}

// registerAgentSession installs as under id unless a concurrent request
// already registered one, and returns the session that won. Because
// construction now happens outside h.mu, two first messages for the same
// session can race; the loser is shut down so its background workers don't
// linger.
func (h *Handler) registerAgentSession(id string, as *agentSession) *agentSession {
	h.mu.Lock()
	if existing, ok := h.agents[id]; ok {
		h.mu.Unlock()
		if as.agent != nil {
			as.agent.Shutdown()
		}
		return existing
	}
	h.agents[id] = as
	h.mu.Unlock()
	return as
}

// ensureAgentSession returns the live agent session for id, building it from
// the supplied history when it is not resident yet. Construction happens
// without h.mu held; only the lookup and the insert take it.
func (h *Handler) ensureAgentSession(id, model string, messages []agent.Message) (*agentSession, error) {
	if as := h.lookupAgentSession(id); as != nil {
		return as, nil
	}
	as, err := h.buildAgentSession(model, messages)
	if err != nil {
		return nil, err
	}
	return h.registerAgentSession(id, as), nil
}

// getOrCreateAgentSession returns the in-memory agent session for id, loading
// its transcript from disk when it is not resident. Callers must NOT hold
// h.mu.
func (h *Handler) getOrCreateAgentSession(id string) (*agentSession, error) {
	if as := h.lookupAgentSession(id); as != nil {
		return as, nil
	}
	s, err := session.Load(id)
	if err != nil {
		return nil, fmt.Errorf("session not found: %w", err)
	}
	model := ""
	if h.cfg != nil {
		model = h.cfg.Model
	}
	return h.ensureAgentSession(id, model, s.Messages)
}

// findPendingSession locates the session whose transcript tail is a pending ask
// (permission or question) whose tool-call id is requestID. It returns that
// session **with its lock already held** — the caller must unlock it — so the
// tail it matched cannot change before the caller acts on it.
//
// The candidate list is snapshotted under h.mu and the tails are then inspected
// under each session's own lock. Reading as.messages under h.mu alone is a data
// race against a running turn, and taking as.mu while holding h.mu would invert
// the lock order (a turn holds as.mu and then takes h.mu via the title
// generator) and deadlock.
func (h *Handler) findPendingSession(sessionID, requestID string, tailIsAsk func([]agent.Message) bool) (*agentSession, string) {
	type candidate struct {
		id string
		as *agentSession
	}

	h.mu.Lock()
	var candidates []candidate
	if sessionID != "" {
		if as, ok := h.agents[sessionID]; ok {
			candidates = append(candidates, candidate{sessionID, as})
		}
	} else {
		for id, as := range h.agents {
			candidates = append(candidates, candidate{id, as})
		}
	}
	h.mu.Unlock()

	for _, c := range candidates {
		c.as.mu.Lock()
		if tailIsAsk(c.as.messages) && c.as.messages[len(c.as.messages)-1].ToolID == requestID {
			return c.as, c.id
		}
		c.as.mu.Unlock()
	}
	return nil, ""
}

// turnOptions carries the per-call variations of a turn.
type turnOptions struct {
	// sessionStarted emits the `session_started` frame before the user echo
	// (set on the request that created the session).
	sessionStarted bool
	// requestID correlates `session_started` back to the browser tab that
	// asked for a brand-new session.
	requestID string
}

// runTurn executes one agent turn: appends the user message, steps the agent,
// persists the transcript and broadcasts the result to the SSE mirror. It
// takes the per-session lock (so turns on one session serialize) and **never
// takes h.mu**, so turns on different sessions run fully in parallel.
//
// It is called both inline (synchronous API) and from a goroutine (the async
// API); the returned text is the assistant reply for the synchronous callers.
func (h *Handler) runTurn(sessionID string, as *agentSession, content string, opts turnOptions) (string, error) {
	as.mu.Lock()
	defer as.mu.Unlock()

	as.messages = append(as.messages, agent.Message{Role: "user", Content: content})
	messages := append([]agent.Message(nil), as.messages...)

	// In headless mode (no RC bridge), wire up streaming callbacks so live
	// tokens and tool activity are broadcast to SSE mirror subscribers.
	headless := h.RCBridge() == nil
	if headless {
		if opts.sessionStarted {
			h.broadcastEvent(SSEEvent{
				SessionID: sessionID,
				Event:     "session_started",
				Data: map[string]string{
					"session_id": sessionID,
					"request_id": opts.requestID,
				},
			})
		}
		// Broadcast the user message so the SSE mirror can echo it.
		h.broadcastEvent(SSEEvent{
			SessionID: sessionID,
			Event:     "user_message",
			Data:      map[string]string{"content": content},
		})
		h.wireHeadlessAgentCallbacks(sessionID, as.agent)
	}

	resp, err := as.agent.Step(messages)
	if err != nil {
		log.Printf("serve error: agent step: %v", err)
		if headless {
			h.broadcastEvent(SSEEvent{
				SessionID: sessionID,
				Event:     "error",
				Data:      map[string]string{"error": err.Error()},
			})
		}
		return "", err
	}

	as.messages = append(as.messages, resp...)

	var reply strings.Builder
	for _, m := range resp {
		if m.Role == "assistant" && m.Content != "" {
			reply.WriteString(m.Content)
		}
	}

	_ = session.Save(sessionID, "", as.messages, nil)

	// Headless-only: generate a title for an untitled session after its first
	// turn (mirrors the TUI; no-op when an RC bridge is attached).
	h.maybeGenerateSessionTitle(sessionID, as)

	// Broadcast the authoritative message snapshot and turn_done so the SSE
	// mirror (and any connected browser) is in sync.
	if headless {
		h.broadcastEvent(SSEEvent{
			SessionID: sessionID,
			Event:     "messages",
			Data:      as.messages,
		})
		h.broadcastEvent(SSEEvent{
			SessionID: sessionID,
			Event:     "turn_done",
			Data:      DoneEvent{SessionID: sessionID, Model: as.model},
		})
	}

	return reply.String(), nil
}

// startTurnAsync runs a turn on its own goroutine. The HTTP response is
// written before the turn starts, so the browser does not hold a connection
// open for the whole turn — with HTTP/1.1's six-connections-per-origin cap, a
// held connection per running session is what made a second session appear
// stuck. All output reaches the browser over the persistent SSE mirror, which
// is where the web UI renders turns from anyway.
func (h *Handler) startTurnAsync(sessionID string, as *agentSession, content string, opts turnOptions) {
	go func() {
		if _, err := h.runTurn(sessionID, as, content, opts); err != nil {
			// runTurn already logged and broadcast the error frame.
			return
		}
	}()
}
