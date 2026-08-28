package server

import (
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/auth"
	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/debuglog"
	"github.com/u007/ocode/internal/session"
	"github.com/u007/ocode/internal/tool"
)

// bootstrapMCPTimeout bounds the MCP tool enumeration wait during session
// bootstrap (30s per the design spec). Bootstrap proceeds without stragglers
// and emits a session_bootstrap warning event instead of hanging the first
// turn of a session.
const bootstrapMCPTimeout = 30 * time.Second

// turnHeartbeatInterval is how often a running turn emits turn_heartbeat on
// the event bus (10s per the design spec). Tests may shorten the interval on
// the Handler before starting a turn.
const turnHeartbeatInterval = 10 * time.Second

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

// buildAgentSession constructs a fresh agent session, emitting observable
// bootstrap stage events (session_bootstrap: model → tools → mcp → ready) and
// advancing the registry entry's bootstrap stage. **It must be called with no
// handler lock held**: every step here can block for a long time —
// InitBuiltinTools and LoadExternalTools touch the filesystem and can spawn
// plugin processes, NewAgent may auto-start a local model server, and the MCP
// wait is bounded by bootstrapMCPTimeout (an unreachable MCP server must not
// hang the first turn). The returned stage names the failing step when err is
// non-nil ("" on success), so callers can emit turn_error carrying it.
func (h *Handler) buildAgentSession(sessionID, model string, messages []agent.Message, projectRoot string) (*agentSession, string, error) {
	if model == "" {
		return nil, "model", fmt.Errorf("no model configured")
	}

	// Resolve effective profile per-session when windowId is bound, otherwise
	// fall back to env > window-state global (v1 global fallback).
	effCfg := h.cfg
	var prof string
	if entry := h.sessions.Lookup(sessionID); entry != nil {
		prof = h.resolveSessionProfile(entry)
	} else {
		prof = h.globalEffectiveProfile()
	}
	if prof != "" {
		if cfg, _, err := config.LoadEffectiveForProfile(prof); err == nil {
			effCfg = cfg
		}
	}
	// Stage "model": LLM client + agent shell.
	h.publishBootstrapStage(sessionID, "model")
	client := agent.NewClientWithProfile(effCfg, model, prof)
	if client == nil {
		return nil, "model", fmt.Errorf("failed to create LLM client for model %q (check the model id has a provider prefix and its credentials are connected)", model)
	}
	// Profile debug: emit active profile + overrides to the log tab when the
	// dedicated toggle is on (default off). This surfaces per-window effective
	// profile, not just the global fallback, so the log tab explains exactly
	// which keys/model the next turn will use.
	if profDebug := func() bool {
		h.mu.Lock()
		defer h.mu.Unlock()
		return h.cfg != nil && h.cfg.Ocode.ProfileDebug
	}(); profDebug {
		windowID := ""
		if entry := h.sessions.Lookup(sessionID); entry != nil {
			windowID = entry.WindowID
		}
		h.emitProfileDebugForWindow(windowID, prof, sessionID)
		// Also emit a session-scoped line with effective model/provider keys for
		// this specific build, so the log tab shows why a profile switch took
		// effect on this session's next turn.
		effModel := model
		if effCfg != nil && effCfg.Model != "" {
			effModel = effCfg.Model
		}
		activeLabel := prof
		if activeLabel == "" {
			activeLabel = "Default"
		}
		debuglog.Log.Append(debuglog.Entry{
			Kind:      debuglog.KindProfile,
			Message:   fmt.Sprintf("PROFILE session=%s window=%s active=%q effModel=%q project=%q", sessionID, windowID, activeLabel, effModel, projectRoot),
			SessionID: sessionID,
		})
	}
	lspMgr := h.lspManagerFor(projectRoot)
	tools := tool.InitBuiltinTools(lspMgr, effCfg, h.scheduler)
	ag := agent.NewAgent(client, tools, effCfg, lspMgr)
	ag.SetSessionID(sessionID)
	// The agent's workdir comes from the registry entry's project root, not
	// the process cwd — multi-project sessions run against their own repo
	// (environment prompt, file-edit snapshots, permissions, discovery all
	// follow SetWorkDir).
	if projectRoot != "" {
		ag.SetWorkDir(projectRoot)
	}

	// Wire secret redaction (tier-1 regex hook + tier-2 LLM scanner) from the
	// effective config, mirroring the TUI. This makes the Security & Redaction
	// settings (including a local LM Studio / local-model scanner) take effect
	// on the web/desktop server, not just the TUI.
	if effCfg != nil && effCfg.Ocode.Security.Redaction.Enabled {
		h.applyRedactionToAgent(ag, effCfg.Ocode.Security.Redaction)
	}

	// Stage "tools": external/plugin tools.
	h.publishBootstrapStage(sessionID, "tools")
	ag.LoadExternalTools(effCfg)

	// Stage "mcp": MCP tools with a bounded wait. Stragglers are dropped with
	// a warning event rather than stalling the bootstrap.
	h.publishBootstrapStage(sessionID, "mcp")
	timeout := h.mcpBootstrapTimeout
	if timeout <= 0 {
		timeout = bootstrapMCPTimeout
	}
	mcpTools, mcpErrs, timedOut := h.mcpCache.waitTimeout(timeout)
	ag.AddMCPTools(mcpTools)
	ag.AddMCPErrors(mcpErrs)
	if timedOut {
		h.publishBootstrapWarning(sessionID, "mcp", "MCP enumeration did not finish within 30s; proceeding without stragglers")
	}

	ag.SetAdvisorEnabled(h.advisorFlag())
	h.wireCompactCallbacks(sessionID, ag)
	as := &agentSession{agent: ag, messages: messages, model: model, profile: prof, credVersion: auth.ProfileCredentialVersion()}
	h.publishBootstrapStage(sessionID, "ready")
	return as, "", nil
}

// resolveSessionProfile computes the effective profile a session should run
// under, given its registry entry. Order: OCODE_PROFILE env (ephemeral, wins
// everywhere) > the window's active profile (when the session is bound to a
// window) > the v1 global fallback (most-recent window / env).
func (h *Handler) resolveSessionProfile(entry *sessionEntry) string {
	if v := os.Getenv("OCODE_PROFILE"); v != "" {
		return v
	}
	if entry.WindowID != "" {
		return h.getWindowProfile(entry.WindowID)
	}
	return h.globalEffectiveProfile()
}

// reconcileProfileAgent rebuilds the resident agent for id when the window's
// current active profile differs from the one the agent was built with. The
// design requires "mid-stream turns finish on the old profile; the next turn
// uses the new profile" — so a profile switch takes effect on the very next
// turn without restarting the app.
//
// It is a no-op (returns as unchanged) when:
//   - as is nil (the caller's normal bootstrap path handles it),
//   - the session is not window-bound,
//   - a turn is currently active (the in-flight turn finishes on the old
//     profile, per the design), or
//   - the profile hasn't changed.
//
// The rebuild calls buildAgentSession (slow: may spawn plugin/MCP processes),
// which is acceptable because profile switches are rare and happen at turn
// boundaries.
func (h *Handler) reconcileProfileAgent(id string, as *agentSession, model string) (*agentSession, error) {
	if as == nil {
		return nil, nil
	}
	entry := h.sessions.Lookup(id)
	if entry == nil {
		return as, nil
	}
	// Never tear down an agent mid-turn. The running turn keeps its pointer to
	// the old agent and finishes on the old profile; the rebuild lands on the
	// next turn instead.
	if h.sessions.IsTurnActive(id) {
		return as, nil
	}
	// A model switch (e.g. the desktop model picker) must rebuild the client
	// even when the profile is unbound to a window — the cached agentSession
	// otherwise keeps talking to whatever model it was originally built with.
	modelChanged := model != "" && model != as.model
	// The credential version is global, not per-profile: an in-place edit must
	// invalidate the cached client for window-unbound sessions too, so it is
	// read unconditionally rather than only on the window-bound path.
	curCredVersion := auth.ProfileCredentialVersion()
	cur := as.profile
	if entry.WindowID != "" {
		cur = h.resolveSessionProfile(entry)
	}
	if !modelChanged && cur == as.profile && curCredVersion == as.credVersion {
		return as, nil
	}
	newAs, stage, err := h.buildAgentSession(id, model, as.messages, entry.ProjectRoot)
	if err != nil {
		log.Printf("serve error: rebuild agent for %s (stage %s): %v", id, stage, err)
		return as, err
	}
	newAs.agent.SetParentAdvisorInFlight(as.agent.AdvisorGuard())
	h.replaceAgentSession(id, newAs)
	log.Printf("agent: rebuilt session %s (profile %s -> %s, credVersion %d -> %d, model %s -> %s)", id, as.profile, cur, as.credVersion, curCredVersion, as.model, model)
	return newAs, nil
}

// replaceAgentSession swaps the resident agent for id under h.mu, shutting down
// the previous one. Callers must NOT hold h.mu. The swap is atomic with respect
// to the map; an in-flight turn holding the old agent's lock continues
// undisturbed on the old agent (its pointer stays valid) and the next turn
// picks up the replacement.
//
// The old agent is shut down only when no turn is active for id: this is
// enforced here, not just by callers, so a future or racing caller can't
// tear down an agent mid-turn by skipping the IsTurnActive check.
func (h *Handler) replaceAgentSession(id string, as *agentSession) {
	h.mu.Lock()
	old, ok := h.agents[id]
	h.agents[id] = as
	h.mu.Unlock()
	if ok && old != as && old.agent != nil && !h.sessions.IsTurnActive(id) {
		old.agent.Shutdown()
	}
}

// registerAgentSession installs as under id unless a concurrent request
// already registered one, and returns the session that won. Because
// construction now happens outside h.mu, two first messages for the same
// session can race; the loser is shut down so its background workers don't
// linger. The session registry entry is bound to projectRoot (or keeps an
// already-resolved root when projectRoot is empty).
func (h *Handler) registerAgentSession(id string, as *agentSession, projectRoot string) *agentSession {
	entry := h.sessions.Register(id, projectRoot)
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
	h.sessions.setAgent(id, as)
	entry.lastActivity = time.Now()
	return as
}

// ensureAgentSession returns the live agent session for id, building it from
// the supplied history when it is not resident yet. Construction happens
// without h.mu held; only the lookup and the insert take it. projectRoot is
// the binding for new sessions ("" keeps an already-resolved root). The
// returned stage names the failing bootstrap step when err is non-nil.
func (h *Handler) ensureAgentSession(id, model string, messages []agent.Message, projectRoot string) (*agentSession, string, error) {
	if as := h.lookupAgentSession(id); as != nil {
		return as, "", nil
	}
	as, stage, err := h.buildAgentSession(id, model, messages, projectRoot)
	if err != nil {
		return nil, stage, err
	}
	return h.registerAgentSession(id, as, projectRoot), "", nil
}

// publishBusEvent publishes a session-scoped event directly on the unified
// bus — tagged with the session's owning project — and records the bus
// sequence as the session's reconcile watermark. These events are new in
// Part 03 (bootstrap stages, turn lifecycle) and only /api/events consumers
// know them, so there is no legacy mirror fan-out. Never called with h.mu
// or the session turn lock ordering inverted (it takes the registry lock
// briefly, which no caller holds while running a turn).
func (h *Handler) publishBusEvent(event, sessionID string, data any) {
	project := ""
	if e := h.sessions.Lookup(sessionID); e != nil {
		project = e.ProjectRoot
	}
	h.bus.Publish(event, project, sessionID, data)
	h.sessions.SetLastSeq(sessionID, h.bus.LastSeq())
}

// publishBootstrapStage records the stage on the registry entry and emits the
// session_bootstrap event for it.
func (h *Handler) publishBootstrapStage(sessionID, stage string) {
	h.sessions.SetBootstrapStage(sessionID, stage)
	h.publishBusEvent("session_bootstrap", sessionID, map[string]string{
		"session_id": sessionID,
		"stage":      stage,
	})
}

// publishBootstrapWarning emits a non-terminal session_bootstrap event for the
// given stage carrying a warning (used when the bounded MCP wait times out and
// bootstrap proceeds without stragglers).
func (h *Handler) publishBootstrapWarning(sessionID, stage, warning string) {
	h.publishBusEvent("session_bootstrap", sessionID, map[string]any{
		"session_id": sessionID,
		"stage":      stage,
		"warning":    warning,
	})
}

// publishTurnStarted emits turn_started for a session entering a turn.
func (h *Handler) publishTurnStarted(sessionID string) {
	h.publishBusEvent("turn_started", sessionID, map[string]string{"session_id": sessionID})
}

// publishTurnDone emits the terminal turn_done for a successful turn. In
// headless mode it goes through broadcastEvent so the legacy mirror and the
// bus both get it; when an RC bridge is attached the mirror is fed by the
// bridge, so the bus publish is direct.
func (h *Handler) publishTurnDone(sessionID, model string) {
	// Release any tool-output buffers whose tool_result never arrived (the
	// agent's tool loop returns early on mid-batch cancellation), so nothing is
	// retained past the turn that created it.
	h.toolOutput.dropSession(sessionID)
	ev := DoneEvent{SessionID: sessionID, Model: model}
	if h.RCBridge() == nil {
		h.broadcastEvent(SSEEvent{SessionID: sessionID, Event: "turn_done", Data: ev})
		// Push a fresh session-tagged status snapshot so the web sidebar's
		// Context gauge reflects the turn that just finished (the transcript
		// was already persisted above). Without this the gauge only updates
		// on tab activation or reconnect.
		h.publishTurnStatusSnapshot(sessionID)
		return
	}
	h.publishBusEvent("turn_done", sessionID, ev)
}

// publishTurnError emits turn_error for a failed turn. stage is the failing
// bootstrap stage when the failure was bootstrap-caused ("" for a turn error),
// per the design spec: "Bootstrap failure emits turn_error carrying the
// failing stage." In headless mode the legacy mirror also receives an "error"
// frame (its existing streaming-clearing signal).
func (h *Handler) publishTurnError(sessionID string, err error, stage string) {
	// See publishTurnDone: a failed turn must release its buffers too.
	h.toolOutput.dropSession(sessionID)
	data := map[string]any{"session_id": sessionID, "error": err.Error()}
	if stage != "" {
		data["stage"] = stage
	}
	h.publishBusEvent("turn_error", sessionID, data)
	if h.RCBridge() == nil {
		h.broadcastEvent(SSEEvent{SessionID: sessionID, Event: "error", Data: map[string]string{"error": err.Error()}})
	}
}

// getOrCreateAgentSession returns the in-memory agent session for id, loading
// its transcript from disk when it is not resident. It resolves the session's
// owning project through the registry (so sessions from any registered
// project load, not just the server's own workdir). Callers must NOT hold
// h.mu.
func (h *Handler) getOrCreateAgentSession(id string) (*agentSession, error) {
	if as := h.lookupAgentSession(id); as != nil {
		return as, nil
	}
	entry, err := h.sessions.Resolve(id)
	if err != nil {
		return nil, fmt.Errorf("session not found: %w", err)
	}
	s, err := session.LoadForDir(entry.ProjectRoot, id)
	if err != nil {
		return nil, fmt.Errorf("session not found: %w", err)
	}
	model := ""
	if h.cfg != nil {
		model = h.cfg.Model
	}
	as, _, err := h.ensureAgentSession(id, model, s.Messages, entry.ProjectRoot)
	return as, err
}

// findPendingSession locates the session whose most recent tool-call round
// contains a pending ask (permission or question) whose tool-call id is
// requestID. It returns that session **with its lock already held** — the
// caller must unlock it — so the matched message cannot change before the
// caller acts on it.
//
// isPendingAsk tests one message, not the whole transcript: a round can pause
// on more than one unresolved ask at once (see trailingToolRunStart), so the
// match is searched for across the whole trailing tool-call round rather than
// assumed to be the literal last message.
//
// The candidate list is snapshotted under h.mu and the tails are then inspected
// under each session's own lock. Reading as.messages under h.mu alone is a data
// race against a running turn, and taking as.mu while holding h.mu would invert
// the lock order (a turn holds as.mu and then takes h.mu via the title
// generator) and deadlock.
func (h *Handler) findPendingSession(sessionID, requestID string, isPendingAsk func(agent.Message) bool) (*agentSession, string) {
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
		matched := false
		for i := trailingToolRunStart(c.as.messages); i < len(c.as.messages); i++ {
			m := c.as.messages[i]
			if m.ToolID == requestID && isPendingAsk(m) {
				matched = true
				break
			}
		}
		if matched {
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
// It is called both inline (synchronous API) and from a turn-job goroutine
// (the async API); the returned text is the assistant reply for the
// synchronous callers.
//
// Turn lifecycle: the registry entry is marked turn-active, turn_started is
// emitted, a heartbeat ticker emits turn_heartbeat while the turn runs, and
// turn_done / turn_error terminates the state. These flow for headless and
// bridged sessions alike (Part 06: the TUI does not consume the web bus, so
// its own mirror rendering is unaffected); only the mirror-specific frames
// (session_started, user_message, live deltas, messages snapshot) stay
// headless-only, because a bridged TUI broadcasts its own equivalents.
func (h *Handler) runTurn(sessionID string, as *agentSession, content string, opts turnOptions) (string, error) {
	as.mu.Lock()
	defer as.mu.Unlock()

	if tailIsPermissionAsk(as.messages) {
		return "", ErrPermissionPending
	}

	as.messages = append(as.messages, agent.Message{Role: "user", Content: content})
	messages := append([]agent.Message(nil), as.messages...)

	// Turn lifecycle: mark active, start the heartbeat, emit turn_started.
	// The session_started marker (set at session creation) survives a
	// bootstrap failure: the first turn that actually runs emits the frame
	// correlated to the creating tab.
	emitSessionStarted := false
	if rid, ok := h.sessions.ConsumeSessionStart(sessionID); ok {
		opts.requestID = rid
		emitSessionStarted = true
	} else if opts.sessionStarted {
		emitSessionStarted = true
	}
	h.sessions.setTurnActive(sessionID, true)
	heartbeatStop := make(chan struct{})
	heartbeatDone := make(chan struct{})
	go h.turnHeartbeat(sessionID, heartbeatStop, heartbeatDone)
	defer func() {
		close(heartbeatStop)
		<-heartbeatDone
		h.sessions.setTurnActive(sessionID, false)
		h.flushStrandedInjections(sessionID, as)
	}()
	h.publishTurnStarted(sessionID)

	// In headless mode (no RC bridge), wire up streaming callbacks so live
	// tokens and tool activity are broadcast to SSE mirror subscribers.
	headless := h.RCBridge() == nil
	if !headless && emitSessionStarted {
		// Bridged: the TUI mirrors its own frames, but the request-id that
		// correlates session_started back to the creating browser tab exists
		// only here — publish it on the unified bus so the correlation is not
		// silently dropped (broadcastEvent below already dual-publishes the
		// headless frame to the bus).
		h.publishBusEvent("session_started", sessionID, map[string]string{
			"session_id": sessionID,
			"request_id": opts.requestID,
		})
	}
	if headless {
		if emitSessionStarted {
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
		// Keep whatever the turn produced before it failed. Step returns the
		// completed rounds alongside the error, and those were already streamed
		// to the browser — discarding them here is what made a failed turn
		// reopen as nothing but the user's own message.
		h.commitPartialTranscript(sessionID, as, append(as.messages, resp...), headless)
		h.publishTurnError(sessionID, err, "")
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

	_ = h.saveSession(sessionID, "", as.messages, nil)

	// Headless-only: generate a title for an untitled session after its first
	// turn (mirrors the TUI; no-op when an RC bridge is attached).
	h.maybeGenerateSessionTitle(sessionID, as)

	// Broadcast the authoritative message snapshot so the SSE mirror (and any
	// connected browser) is in sync, then the terminal turn_done.
	if headless {
		h.broadcastEvent(SSEEvent{
			SessionID: sessionID,
			Event:     "messages",
			Data:      as.messages,
		})
	}
	h.publishTurnDone(sessionID, as.model)

	// Post-turn auto-compaction check (mirrors the TUI's trigger). Runs in a
	// goroutine when over threshold; the result lands via OnCompact
	// (applyCompactResult), which takes as.mu itself.
	as.agent.MaybeCompactAsync(as.messages)

	return reply.String(), nil
}

// commitPartialTranscript stores, persists and mirrors the transcript of a turn
// that failed part-way through. Every message in msgs was already streamed to
// the browser (and every tool result in it already ran), so a failed final LLM
// round must not erase it: without this, reopening the session shows nothing
// but the user's own message. mirror is false for bridged sessions, which
// broadcast their own frames.
func (h *Handler) commitPartialTranscript(sessionID string, as *agentSession, msgs []agent.Message, mirror bool) {
	if len(msgs) == 0 {
		return
	}
	// Copy: callers build msgs with append() over the session's own slice, so
	// storing it directly would leave two slice headers sharing one backing
	// array — a later append through either one (an injection flush, a compact
	// result) would write into the other's elements.
	as.messages = append([]agent.Message(nil), msgs...)
	if err := h.saveSession(sessionID, "", as.messages, nil); err != nil {
		log.Printf("serve error: persisting partial transcript for session %s: %v", sessionID, err)
	}
	if mirror {
		h.broadcastEvent(SSEEvent{
			SessionID: sessionID,
			Event:     "messages",
			Data:      as.messages,
		})
	}
}

// turnHeartbeat emits turn_heartbeat on the bus every interval while a turn
// runs, so a client watching the session can distinguish "still running" from
// "stalled" (the frontend's 30s watchdog). It exits when stop is closed and
// closes done on exit so the turn can join it.
func (h *Handler) turnHeartbeat(sessionID string, stop <-chan struct{}, done chan<- struct{}) {
	defer close(done)
	interval := h.turnHeartbeatInterval
	if interval <= 0 {
		interval = turnHeartbeatInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			h.publishBusEvent("turn_heartbeat", sessionID, map[string]string{"session_id": sessionID})
		}
	}
}

// turnJob is one queued async turn. persistAck is closed once the user
// message is durable on disk (before the bootstrap starts), letting the HTTP
// handler return 202 without racing the agent build; err is set when the
// persist itself failed.
type turnJob struct {
	content    string
	model      string
	opts       turnOptions
	persistAck chan struct{}
	err        error
}

// sessionTurnLock returns the per-session mutex that serializes turn jobs
// (persist → bootstrap → turn) for one session. Jobs on different sessions
// run fully in parallel. The mutexes live for the registry entry's lifetime.
func (h *Handler) sessionTurnLock(id string) *sync.Mutex {
	h.turnMu.Lock()
	defer h.turnMu.Unlock()
	if h.turnLocks == nil {
		h.turnLocks = make(map[string]*sync.Mutex)
	}
	l, ok := h.turnLocks[id]
	if !ok {
		l = &sync.Mutex{}
		h.turnLocks[id] = l
	}
	return l
}

// dispatchTurn starts a turn on its own goroutine, serialized per session
// (single-flight bootstrap + ordered turns). model is used only when the
// session has no resident agent yet. The caller waits on job.persistAck to
// return 202 once the message is durable.
func (h *Handler) dispatchTurn(id, model, content string, opts turnOptions) (*turnJob, error) {
	job := &turnJob{content: content, model: model, opts: opts, persistAck: make(chan struct{})}
	go h.executeTurnJob(id, job)
	return job, nil
}

// executeTurnJob runs one queued turn end to end, under the session's turn
// lock so persist and turn ordering never interleave:
//
//  1. Persist the user message to the session's on-disk transcript, then
//     close persistAck (the caller's 202 gate). A bootstrap failure after
//     this point never loses the message — it stays durable and the next job
//     retries the bootstrap.
//  2. Bootstrap the agent if none is resident. Single-flight is guaranteed by
//     the session lock: at most one job runs per session at a time.
//  3. Turn every pending message in order — the current job's plus any that
//     were persisted while an earlier bootstrap was failing. Each successful
//     (or failed-after-start) turn shifts one pending message; messages whose
//     bootstrap never succeeded stay pending for the retry.
func (h *Handler) executeTurnJob(id string, job *turnJob) {
	lock := h.sessionTurnLock(id)
	lock.Lock()
	defer lock.Unlock()

	entry := h.sessions.Lookup(id)
	if entry == nil {
		job.err = fmt.Errorf("session not found")
		close(job.persistAck)
		return
	}

	// 1. Durable persist before the caller's 202.
	if err := h.persistUserMessage(entry, job.content); err != nil {
		log.Printf("serve error: persist user message for %s: %v", id, err)
		job.err = err
		close(job.persistAck)
		return
	}
	h.sessions.PushPending(id, job.content)
	close(job.persistAck)

	// 2. Bootstrap once if needed. Failure surfaces as turn_error carrying the
	// failing stage; the message stays pending (durable) for the next job.
	as := h.lookupAgentSession(id)
	if as == nil {
		var err error
		var stage string
		as, stage, err = h.bootstrapEntryAgent(entry, job.model)
		if err != nil {
			h.sessions.SetBootstrapFailed(id, stage)
			h.sessions.SetBootstrapError(id, err.Error())
			h.publishTurnError(id, err, stage)
			log.Printf("serve error: bootstrap agent for %s (stage %s): %v", id, stage, err)
			return
		}
	}

	// A profile switch takes effect on the next turn: rebuild the resident
	// agent on the window's new active profile before turning the first
	// pending message. reconcileProfileAgent is a no-op when no turn is active
	// and the profile hasn't changed.
	reconcileModel := job.model
	if reconcileModel == "" {
		h.mu.Lock()
		if h.cfg != nil {
			reconcileModel = h.cfg.Model
		}
		h.mu.Unlock()
	}
	if reb, err := h.reconcileProfileAgent(id, as, reconcileModel); err != nil {
		log.Printf("serve error: reconcile profile for %s: %v", id, err)
	} else {
		as = reb
	}

	// 3. Turn every pending message in order.
	for {
		content, ok := h.sessions.PendingFront(id)
		if !ok {
			break
		}
		_, err := h.runTurn(id, as, content, job.opts)
		if errors.Is(err, ErrPermissionPending) {
			// runTurn refused before appending — leave the message pending
			// (it stays durable on disk) so it is retried once the session's
			// permission ask is resolved, instead of shifting it away unturned.
			h.publishTurnError(id, err, "")
			return
		}
		// Shift regardless of remaining outcomes: on success the reply is in
		// the transcript; on failure the user message was appended in memory
		// (and remains durable on disk), so the next retry must not
		// re-append it.
		h.sessions.ShiftPending(id)
		if err != nil {
			return // runTurn published turn_error
		}
		// Only the first turn of the job carries the request's turn options
		// (session_started correlation); catch-up turns are plain.
		job.opts = turnOptions{}
	}
}

// bootstrapEntryAgent builds the agent for a registry entry from the
// session's on-disk transcript, stripping the trailing messages that are
// still pending (persisted but not yet turned — runTurn re-appends them at
// turn time). Emits session_bootstrap stage events via buildAgentSession.
func (h *Handler) bootstrapEntryAgent(entry *sessionEntry, model string) (*agentSession, string, error) {
	if model == "" && h.cfg != nil {
		model = h.cfg.Model
	}
	var history []agent.Message
	if s, err := session.LoadForDir(entry.ProjectRoot, entry.SessionID); err == nil {
		history = s.Messages
	}
	if n := h.sessions.PendingCount(entry.SessionID); n > 0 && n <= len(history) {
		history = history[:len(history)-n]
	}
	as, stage, err := h.buildAgentSession(entry.SessionID, model, history, entry.ProjectRoot)
	if err != nil {
		return nil, stage, err
	}
	return h.registerAgentSession(entry.SessionID, as, entry.ProjectRoot), "", nil
}

// persistUserMessage appends the user message to the session's on-disk
// transcript (under its owning project root) before the 202 returns, so a
// bootstrap failure after 202 never loses it. The in-memory transcript picks
// the message up at turn time (runTurn's append); the registry's pending
// count keeps the two in sync.
func (h *Handler) persistUserMessage(entry *sessionEntry, content string) error {
	var msgs []agent.Message
	if s, err := session.LoadForDir(entry.ProjectRoot, entry.SessionID); err == nil {
		msgs = s.Messages
	}
	msgs = append(msgs, agent.Message{Role: "user", Content: content})
	return session.SaveForDir(entry.ProjectRoot, entry.SessionID, "", msgs, nil)
}

// tryEnqueueInjection hands content to sessionID's agent for mid-turn
// splicing if (and only if) a turn is currently active on it, so the message
// reaches the LLM at the next tool-call boundary of the running turn rather
// than starting a whole new queued turn after it. Returns false — meaning
// the caller should fall through to the normal dispatchTurn path — when no
// turn is active. Agent.EnqueueInjection is safe to call regardless, so the
// narrow race against the turn finishing right after the IsTurnActive check
// costs nothing: flushStrandedInjections (runTurn's defer) is the backstop
// for anything left in the queue once the turn ends.
func (h *Handler) tryEnqueueInjection(sessionID, content string) bool {
	as := h.lookupAgentSession(sessionID)
	if as == nil || !h.sessions.IsTurnActive(sessionID) {
		return false
	}
	as.agent.EnqueueInjection(agent.Message{Role: "user", Content: content})
	return true
}

// flushStrandedInjections drains any message left in as.agent's injection
// queue when a turn ends (the race tryEnqueueInjection can't fully close:
// enqueued after the last Step loop check but before setTurnActive(false)).
// Each stranded message is dispatched as an ordinary follow-up turn so
// nothing submitted by the user is ever silently dropped.
func (h *Handler) flushStrandedInjections(sessionID string, as *agentSession) {
	for _, m := range as.agent.DrainPendingInjections() {
		if _, err := h.dispatchTurn(sessionID, "", m.Content, turnOptions{}); err != nil {
			log.Printf("serve: flush stranded injection for %s: %v", sessionID, err)
		}
	}
}

// wireCompactCallbacks attaches OnCompact to a server-built agent so async
// auto-compaction results land back in the session transcript. The TUI wires
// its own callbacks (tui.wireCompactCallbacks); without this server-side
// equivalent, headless (web/desktop) compaction results were silently dropped
// — OnCompact was nil, so MaybeCompactAsync had no effect even if called.
func (h *Handler) wireCompactCallbacks(sessionID string, ag *agent.Agent) {
	ag.OnCompact = func(r agent.CompactResult) {
		h.applyCompactResult(sessionID, r)
	}
}

// applyCompactResult splices an async compaction summary into the session
// transcript, persists it, and broadcasts the new snapshot so connected
// browsers drop their stale (pre-compaction) message lists. Runs on the
// compaction goroutine, so it takes the per-session lock itself.
//
// The result's splice indices refer to the snapshot taken when compaction
// started; turns only ever append, so they stay valid as long as the
// transcript has not shrunk since (a racing manual /compact can shrink it —
// in that case the stale result is dropped).
func (h *Handler) applyCompactResult(sessionID string, r agent.CompactResult) {
	if !r.OK {
		if r.Err != nil {
			log.Printf("serve: auto-compaction failed for session %s: %v", sessionID, r.Err)
		}
		return
	}
	as := h.lookupAgentSession(sessionID)
	if as == nil {
		log.Printf("serve: auto-compaction result for unknown session %s dropped", sessionID)
		return
	}
	as.mu.Lock()
	defer as.mu.Unlock()
	if len(as.messages) < r.OriginalLen || r.ReplaceFrom < 0 || r.ReplaceTo > len(as.messages) || r.ReplaceFrom > r.ReplaceTo {
		log.Printf("serve: auto-compaction result for session %s dropped: transcript changed (len=%d, splice=[%d:%d), snapshot=%d)",
			sessionID, len(as.messages), r.ReplaceFrom, r.ReplaceTo, r.OriginalLen)
		return
	}
	compacted := make([]agent.Message, 0, r.ReplaceFrom+1+len(as.messages)-r.ReplaceTo)
	compacted = append(compacted, as.messages[:r.ReplaceFrom]...)
	compacted = append(compacted, r.Summary)
	compacted = append(compacted, as.messages[r.ReplaceTo:]...)
	as.messages = compacted

	if err := h.saveSession(sessionID, "", as.messages, nil); err != nil {
		log.Printf("serve: persisting compacted transcript for session %s: %v", sessionID, err)
	}
	if h.RCBridge() == nil {
		h.broadcastEvent(SSEEvent{
			SessionID: sessionID,
			Event:     "messages",
			Data:      as.messages,
		})
	}
}

// saveSession persists a transcript to the session's owning project's storage
// dir — multi-project sessions must not land in the server's own project (the
// process workdir). Falls back to the process default only when the registry
// entry is unknown.
func (h *Handler) saveSession(sessionID, title string, msgs []agent.Message, metadata map[string]any) error {
	if e := h.sessions.Lookup(sessionID); e != nil && e.ProjectRoot != "" {
		return session.SaveForDir(e.ProjectRoot, sessionID, title, msgs, metadata)
	}
	return session.Save(sessionID, title, msgs, metadata)
}

// loadSession is the read-side counterpart to saveSession: it resolves the
// session's owning project from the registry before loading, so a
// multi-project session is found even when it is not the process's own
// project. Falls back to the process default only when the registry entry is
// unknown.
func (h *Handler) loadSession(sessionID string) (*session.Session, error) {
	if e := h.sessions.Lookup(sessionID); e != nil && e.ProjectRoot != "" {
		return session.LoadForDir(e.ProjectRoot, sessionID)
	}
	return session.Load(sessionID)
}
