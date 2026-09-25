package server

import (
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/session"
	"github.com/u007/ocode/internal/usage"
)

// HandleSessionState is the reconcile endpoint (Part 03). The frontend derives
// streaming state from it — bootstrap_stage and turn_active — and uses
// last_seq to detect events it may have missed during a reconnect. Reconcile
// is state fetch + transcript refetch for the persisted transcript; the one
// deliberate exception is live_frames, which replays whatever streaming
// text/thinking/tool activity is still buffered from the current turn (see
// appendLiveFrame in session_manager.go) so a mid-turn reload doesn't lose the
// in-progress reply while waiting for turn_done.
//
// A session that exists in no registered project 404s; every other session
// (registered explicitly, resolved from disk, or a bridged TUI session) gets a
// state snapshot.
func (h *Handler) HandleSessionState(w http.ResponseWriter, r *http.Request, id string) {
	entry, err := h.sessions.Resolve(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	state, ok := h.sessions.State(id)
	if !ok {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	resp := sessionStateResponse{SessionState: state}
	// Cross-process change signal (see session.StoredRevisionForDir): clients
	// poll this endpoint and refetch an open session's transcript when the
	// token moves. Computed only for a stored session — a bridged/in-memory
	// session (or an unknown project root) has no file to watch and reports
	// nothing, which callers treat as "never revalidate".
	if entry.ProjectRoot != "" {
		if rev, rerr := session.StoredRevisionForDir(entry.ProjectRoot, id); rerr != nil {
			log.Printf("serve: session %s revision: %v", id, rerr)
		} else {
			resp.Revision = rev
		}
	}
	// Attach the live pending ask (if any) from the resident agent's trailing
	// tool round. The frontend already derives pending asks from a fetched
	// transcript's sentinels, but that only works when the paused tool result
	// actually reached disk: a session whose in-memory ask outlived a failed /
	// conflicting save (or whose pause post-dated the last write) has no
	// sentinel to recover from, leaving the browser with no dialog while every
	// send is rejected by ErrPermissionPending. Reading the live transcript is
	// the only authoritative source in that case.
	//
	// livePendingAsks takes h.mu only to read the session pointer, releases
	// it, then takes as.mu — preserving the as.mu → h.mu lock order (see
	// agent_session.go).
	resp.PendingAsks = h.livePendingAsks(id)
	// Settled-on-an-unfinished-turn flag: the session is idle and unattended,
	// and its stored/live transcript tail is not a reply. Drives the chat's
	// "The previous reply was interrupted" notice. Fail-open (see
	// sessionInterrupted).
	resp.Interrupted = h.sessionInterrupted(id, entry.ProjectRoot)
	writeJSON(w, http.StatusOK, resp)
}

// sessionStateResponse is SessionState plus the live pending-ask payload.
// SessionState is embedded so every existing field serializes exactly as
// before; pending_asks is omitempty so an idle session's body is unchanged.
type sessionStateResponse struct {
	SessionState
	PendingAsks *PendingAsks `json:"pending_asks,omitempty"`
	// Revision is an opaque token that changes when this session's STORED
	// transcript changes — by any writer, in any process sharing the project's
	// session storage (desktop + dev server, TUI, ...). The web client polls
	// this endpoint while a tab is open and refetches the transcript when the
	// token moves, which is how an out-of-process /compact (or any turn)
	// reaches a client connected to a different server process. Absent for a
	// bridged/in-memory session with no stored file.
	Revision string `json:"revision,omitempty"`
	// Interrupted reports that the session has SETTLED on an unfinished turn:
	// no turn is active or in flight, the session is not parked on an ask, and
	// the last stored/live transcript row is not a reply — an answered ask, a
	// user row, or a tool_calls-only assistant. The web chat renders an inline
	// "The previous reply was interrupted" row with a Continue action. Absent
	// (false) whenever we cannot tell, so a session never invents an
	// interruption.
	Interrupted bool `json:"interrupted,omitempty"`
}

// PendingAsks is the unresolved permission/question prompt(s) a live agent
// session is currently paused on. Each entry mirrors the corresponding SSE
// frame (PermissionEvent / QuestionEvent) so the browser can dispatch it
// through the same PERMISSION_REQUEST / QUESTION_REQUEST reducer path a live
// event would take.
type PendingAsks struct {
	Permissions []PermissionEvent `json:"permissions,omitempty"`
	Questions   []QuestionEvent   `json:"questions,omitempty"`
}

// livePendingAsks returns the unresolved asks held in the session's resident
// agent transcript, or nil when the session has no live agent, a turn is in
// flight, or it is not paused on one. A resolved ask has its sentinel replaced
// in place, so this returns nil again once the user answers.
//
// The read is a non-blocking TryLock on purpose: runTurn holds as.mu for the
// whole turn (minutes), and this runs inside the reconcile HTTP handler the
// browser and watchdog poll — a blocking lock would pin an HTTP connection
// behind the turn (the "stuck session" class, see AGENTS). While a turn holds
// the lock it cannot yet be paused on an ask (the pause and the unlock happen
// together when the step returns), so reporting nothing is correct; any ask it
// does raise arrives over SSE.
func (h *Handler) livePendingAsks(id string) *PendingAsks {
	as := h.lookupAgentSession(id)
	if as == nil {
		return nil
	}
	if !as.mu.TryLock() {
		return nil
	}
	defer as.mu.Unlock()
	return pendingAsksFromMessages(as.messages)
}

// pendingAsksFromMessages extracts unresolved asks from the trailing tool
// round of msgs. A single round may pause on more than one ask (parallel
// dispatch runs several calls before the pause check), so the whole trailing
// run is scanned — see trailingToolRunStart.
func pendingAsksFromMessages(msgs []agent.Message) *PendingAsks {
	var out PendingAsks
	for i := trailingToolRunStart(msgs); i < len(msgs); i++ {
		m := msgs[i]
		if m.Role != "tool" {
			continue
		}
		if req, ok := parsePermissionAsk(m.Content); ok {
			out.Permissions = append(out.Permissions, newPermissionEvent(m.ToolID, req))
			continue
		}
		if prompts, ok := parseQuestionAsk(m.Content); ok {
			out.Questions = append(out.Questions, QuestionEvent{RequestID: m.ToolID, Questions: prompts})
		}
	}
	if len(out.Permissions) == 0 && len(out.Questions) == 0 {
		return nil
	}
	return &out
}

// HandleSessionStatus returns the per-session status snapshot: a superset of
// today's GET /api/tui-status snapshot (model, advisor, OCR, cwd, LSP servers)
// plus the context data that previously lived only on GET /api/sessions/:id/
// context (context_current_tokens, context_max_tokens, context_model) and the
// session identity itself. Unlike the process-global tui-status snapshot, the
// cwd is the session's owning project root and session_id is populated, so a
// multi-project frontend can render each tab's status independently.
func (h *Handler) HandleSessionStatus(w http.ResponseWriter, r *http.Request, id string) {
	entry, err := h.sessions.Resolve(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	rc := h.RCBridge()
	bridged := rc != nil && rc.SessionID == id
	// Create the project LSP manager during status hydration, not only when a
	// turn is dispatched. Restored/cold headless sessions otherwise have no
	// manager or warmup goroutine, so the desktop sidebar can show an empty
	// list until the user sends another message. A bridged TUI already owns its
	// manager and must not get a duplicate local one.
	lspRoot := entry.ProjectRoot
	if bridged {
		if liveRoot := rc.TUIStatus().CWD; liveRoot != "" {
			lspRoot = liveRoot
		}
	}
	if lspRoot == "" {
		lspRoot = h.workDir
	}
	if lspRoot == "" {
		lspRoot = "."
	}
	if !bridged {
		h.lspManagerFor(lspRoot)
	}

	snap := h.buildStatusSnapshot()
	baseModel, baseCWD := snap.MainModel, snap.CWD
	// Per-session model override takes precedence over the process-wide config
	// model, so each chat tab shows and runs its own model instead of a single
	// global value reflected across every open session.
	snap.MainModel = h.effectiveSessionModel(id)
	// The bridged TUI session is driven by the TUI itself: its live snapshot
	// wins over the persisted/default model so the web mirrors what the TUI is
	// actually running right now.
	if rc := h.RCBridge(); rc != nil && rc.SessionID == id {
		if live := rc.TUIStatus(); live.MainModel != "" {
			snap.MainModel = live.MainModel
		}
	}
	snap.SessionID = id
	if entry.ProjectRoot != "" {
		snap.CWD = entry.ProjectRoot
	}
	// The manager key and LSPStatus.Root are canonicalized for headless
	// sessions; compare canonically, but preserve the session's established CWD
	// spelling for the status contract and sidebar filter. A bridged TUI
	// remains the authority for its own status.
	if bridged {
		live := rc.TUIStatus()
		if live.CWD != "" {
			snap.CWD = live.CWD
		}
		// TUI managers historically report "." as their root. Copy before
		// normalizing so the bridge's published snapshot is not mutated, and
		// translate that relative identity to the session's displayed cwd for
		// the web root filter.
		liveServers := append([]LSPStatus(nil), live.LSPServers...)
		if live.CWD != "" {
			canonicalRoot := lspProjectRootKey(live.CWD)
			for i := range liveServers {
				if liveServers[i].Root == "" || liveServers[i].Root == "." || lspProjectRootKey(liveServers[i].Root) == canonicalRoot {
					liveServers[i].Root = live.CWD
				}
			}
		}
		snap.LSPServers = liveServers
	} else {
		snap.LSPServers = h.lspStatusesForRoot(snap.CWD)
	}
	applySessionModelPrompt(&snap, baseModel, baseCWD)
	// Populate persisted session title so the web tab bar shows the
	// authoritative title (auto fallback or LLM-generated) immediately,
	// not just after a generated-title status broadcast.
	if s, err := session.LoadForDir(entry.ProjectRoot, id); err == nil {
		snap.SessionTitle = s.Title
		if !s.CreatedAt.IsZero() {
			snap.SessionCreatedAt = s.CreatedAt.UTC().Format(time.RFC3339Nano)
		}
	}
	h.applySessionContext(&snap, id)
	h.applySessionUsage(&snap, id)
	h.applyTurnTiming(&snap, id)
	// Permission mode is per session: without this the snapshot would report
	// the process-wide config default and a chat's yolo/sandbox toggle would
	// look global in the sidebar again.
	h.applySessionPermissionFields(&snap, id)
	h.applySessionThinkingBudget(&snap, id)
	// Advisor gate is per session too: stamp the chat's own value so one tab's
	// toggle does not render on every other tab's sidebar.
	h.applySessionAdvisorFields(&snap, id)
	snap.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)

	writeJSON(w, http.StatusOK, snap)
}

// applySessionContext fills snap's context-window fields (context_current_
// tokens, context_max_tokens, context_model) for the session id.
//
// context_current_tokens prefers the backend's provider-reported value:
//   - the bridged TUI session uses the TUI's live ContextCurrentTokens, which
//     it captured from the provider's last response;
//   - a headless (web/desktop) session uses its live agent's LastInputTokens,
//     captured from resp.Usage on the most recent LLM call;
//   - when the agent has compacted, LastInputTokens was cleared (it described
//     the pre-compaction shape) and is replaced by the agent's
//     CompactedContextTokens estimate of the spliced transcript, so a /compact
//     makes the sidebar gauge reflect the reduced context instead of going
//     "unknown" until the next turn.
//
// When neither is available — no live agent in this process, e.g. a restored
// session whose agent was idle-evicted (30 min) or never built since server
// start — it falls back to a chars/4 estimate over the persisted transcript so
// switching tabs shows a value instead of "unknown". Provider/TUI readings
// always win over the estimate.
//
// Every session-tagged status snapshot must go through this before it is
// broadcast or returned: buildStatusSnapshot() alone omits all three fields,
// and because they are omitempty on the wire, a snapshot without them makes
// the web sidebar's Context gauge drop to zero ("not reflected") until the
// next per-session fetch.
//
// This must stay lock-free with respect to the agentSession mutex: it is
// called from publishTurnStatusSnapshot, which runTurn/permission-continuation
// invoke while holding as.mu. Both agent reads are atomic.
func (h *Handler) applySessionContext(snap *TUIStatus, id string) {
	model := ""
	maxTokens := 0
	var current int64

	if rc := h.RCBridge(); rc != nil && id == rc.SessionID {
		if live := rc.TUIStatus(); live.ContextModel != "" {
			model = live.ContextModel
			maxTokens = live.ContextMaxTokens
			current = int64(live.ContextCurrentTokens)
		}
	}
	if current == 0 {
		if as := h.lookupAgentSession(id); as != nil && as.agent != nil {
			current = as.agent.LastInputTokens()
			if current == 0 {
				current = as.agent.CompactedContextTokens()
			}
		}
	}
	if current == 0 {
		// No provider reading available in this process — fall back to the
		// persisted transcript so a session switch does not render "unknown".
		current = h.estimateContextFromTranscript(id)
	}
	if model == "" && h.cfg != nil {
		model = h.effectiveSessionModel(id)
	}
	if maxTokens == 0 {
		maxTokens = int(agent.ModelWindow(model))
	}
	snap.ContextCurrentTokens = int(current)
	snap.ContextMaxTokens = maxTokens
	snap.ContextModel = model
}

// applySessionUsage sets snap's per-session spend (spending_usd) and token
// counts (input_tokens/output_tokens/cached_tokens/total_tokens) with a strict
// precedence so the same usage is never counted twice:
//
//  1. the live agent's accumulated totals (the authoritative in-process value);
//  2. the session's persisted metadata (survives rebuild/restart; the same keys
//     the TUI's sidebarTelemetry writes);
//  3. spend only, when BOTH are absent, the usage ledger's rows tagged with this
//     session id (usage.SessionSpend) — recovery for a session whose metadata
//     total was lost.
//
// Tokens have no ledger equivalent and the transcript does not persist per-
// message Usage (agent.Message.Usage is json:"-", so it exists only in memory),
// so a restored session with no token metadata simply reports no token
// breakdown — the same "n/a" the TUI shows when sidebarTelemetry has no data.
// Every session that runs through this server from now on persists its totals
// at turn end (persistSessionTelemetry), so only pre-existing sessions are
// affected.
//
// Zero means unknown and is omitted (omitempty), so the web falls back to the
// process-wide daily total for spend and renders no token breakdown.
//
// Must stay lock-free with respect to the agentSession mutex: it is called from
// publishTurnStatusSnapshot (see its doc), which runTurn invokes while holding
// as.mu. Every live-agent read below is atomic for that reason.
func (h *Handler) applySessionUsage(snap *TUIStatus, id string) {
	coveredSpend := false
	coveredTokens := false
	if as := h.lookupAgentSession(id); as != nil {
		if v := as.spendUSD(); v > 0 {
			snap.SpendingUSD = v
			coveredSpend = true
		}
		if as.hasUsage() {
			snap.InputTokens, snap.OutputTokens, snap.CachedTokens, snap.TotalTokens = as.usageSnapshot()
			coveredTokens = true
		}
	}
	if coveredSpend && coveredTokens {
		return
	}
	projectRoot := h.sessionProjectRoot(id)
	if projectRoot == "" {
		return
	}
	if s, err := session.LoadForDir(projectRoot, id); err == nil {
		if !coveredSpend {
			if v := sessionSpendFromMetadata(s.Metadata); v > 0 {
				snap.SpendingUSD = v
				coveredSpend = true
			}
		}
		if !coveredTokens {
			in, out, cached, total := sessionUsageFromMetadata(s.Metadata)
			if in > 0 || out > 0 || cached > 0 || total > 0 {
				snap.InputTokens, snap.OutputTokens, snap.CachedTokens, snap.TotalTokens = in, out, cached, total
				coveredTokens = true
			}
		}
	}
	// Last resort for spend only: sum this session's attributed ledger rows.
	// Only reached when there is no live agent and no persisted metadata total,
	// so it cannot double-count either of the values above.
	if !coveredSpend {
		if v, err := usage.SessionSpend(id); err == nil && v > 0 {
			snap.SpendingUSD = v
		}
	}
}

// estimateContextFromTranscript approximates the current context-window
// occupancy of a session from its persisted transcript (chars/4). It is the
// fallback used by applySessionContext/HandleSessionContext only when no
// provider-reported value exists. Returns 0 when the session cannot be
// resolved/loaded.
func (h *Handler) estimateContextFromTranscript(id string) int64 {
	entry, err := h.sessions.Resolve(id)
	if err != nil {
		return 0
	}
	s, err := session.LoadForDir(entry.ProjectRoot, id)
	if err != nil {
		return 0
	}
	return estimateContextFromMessages(s.Messages)
}

// estimateContextFromMessages sums the transcript's character weight (content,
// reasoning, and tool-call arguments) and divides by 4. Kept in one place so
// every fallback path uses the same approximation.
func estimateContextFromMessages(msgs []agent.Message) int64 {
	var totalChars int
	for _, msg := range msgs {
		totalChars += len(msg.Content) + len(msg.ReasoningContent)
		for _, tc := range msg.ToolCalls {
			totalChars += len(tc.Function.Arguments)
		}
	}
	return int64(totalChars / 4)
}

// activityEventFromAgent converts the agent's activity tracker into the wire form
// of an `agent_activity` event. RFC3339 start timestamps and the field names
// match what the TUI stamps into TUIStatus (see its status builder), so the
// web's StatusBar renders either source identically.
func activityEventFromAgent(sessionID string, snap agent.ActivitySnapshot) AgentActivityEvent {
	ev := AgentActivityEvent{SessionID: sessionID, LLMRunning: snap.LLMRunning}
	if len(snap.ActiveTools) > 0 {
		ev.ActiveTools = make([]ToolActivityStatus, 0, len(snap.ActiveTools))
		for _, ta := range snap.ActiveTools {
			ev.ActiveTools = append(ev.ActiveTools, ToolActivityStatus{
				Name:      ta.Name,
				StartedAt: ta.StartedAt.Format(time.RFC3339),
			})
		}
	}
	if len(snap.ActiveAgents) > 0 {
		ev.ActiveAgents = append([]string(nil), snap.ActiveAgents...)
	}
	return ev
}

// applySessionActivity stamps the agent-loop activity fields onto a per-session
// status snapshot from the session's LIVE agent, so a full snapshot taken
// mid-turn (a model switch, a title-gen, a compact) agrees with the
// `agent_activity` events instead of blanking the status bar's activity row.
//
// No-op when a TUI bridge is attached: that surface owns its own activity feed
// and publishes complete snapshots itself. Also a no-op for a session with no
// resident agent (idle-evicted, restored, or not yet built), which by
// definition has nothing running.
func (h *Handler) applySessionActivity(snap *TUIStatus, sessionID string) {
	if h.RCBridge() != nil {
		return
	}
	as := h.lookupAgentSession(sessionID)
	if as == nil || as.agent == nil {
		return
	}
	ev := activityEventFromAgent(sessionID, as.agent.Activity().Snapshot())
	snap.LLMRunning = ev.LLMRunning
	snap.ActiveTools = ev.ActiveTools
	snap.ActiveAgents = ev.ActiveAgents
}

// applyTurnTiming fills snap's turn timing fields from the SessionManager's
// authoritative turn lifecycle (setTurnActive). Used for every per-session
// status snapshot so the web can show current-input elapsed and last-took
// without an extra round-trip.
func (h *Handler) applyTurnTiming(snap *TUIStatus, id string) {
	if startedAt, endedAt := h.sessions.TurnTiming(id); !startedAt.IsZero() {
		snap.TurnStartedAt = startedAt.UTC().Format(time.RFC3339Nano)
		if h.sessions.IsTurnActive(id) {
			snap.TurnElapsedMs = time.Since(startedAt).Milliseconds()
		}
		if !endedAt.IsZero() {
			snap.TurnEndedAt = endedAt.UTC().Format(time.RFC3339Nano)
			snap.TurnTookMs = endedAt.Sub(startedAt).Milliseconds()
		}
	}
	// SessionCreatedAt is populated by the caller when it has the session
	// metadata; if not yet set, try to load it here as fallback.
	if snap.SessionCreatedAt == "" {
		if entry, err := h.sessions.Resolve(id); err == nil {
			if s, err := session.LoadForDir(entry.ProjectRoot, id); err == nil && !s.CreatedAt.IsZero() {
				snap.SessionCreatedAt = s.CreatedAt.UTC().Format(time.RFC3339Nano)
			}
		}
	}
}

// publishTurnStatusSnapshot broadcasts a fresh session-tagged "status" event
// whose context fields were resolved from the backend's provider-reported
// usage (the just-finished turn's agent LastInputTokens, or the agent's
// post-compaction estimate after a /compact). Called right after a headless
// turn completes and after a manual/auto compaction so the web/desktop
// sidebar's Context gauge moves with every change instead of only on tab
// activation or reconnect. No-op when an RC bridge is attached — the TUI owns
// the status feed for its sessions and pushes its own snapshots.
//
// Callers must NOT hold as.mu (applySessionContext reads only agent atomics to
// stay safe for the callers that already do, but keeping session/broadcast work
// out of the locked region avoids inverting the as.mu → h.mu lock order).
func (h *Handler) publishTurnStatusSnapshot(sessionID string) {
	if h.RCBridge() != nil {
		return
	}
	snap := h.buildStatusSnapshot()
	baseModel, baseCWD := snap.MainModel, snap.CWD
	snap.SessionID = sessionID
	var projectRoot string
	if entry, err := h.sessions.Resolve(sessionID); err == nil {
		projectRoot = entry.ProjectRoot
		if projectRoot != "" {
			snap.CWD = projectRoot
		}
	}
	h.applySessionContext(&snap, sessionID)
	h.applySessionUsage(&snap, sessionID)
	if projectRoot != "" {
		if s, err := session.LoadForDir(projectRoot, sessionID); err == nil && !s.CreatedAt.IsZero() {
			snap.SessionCreatedAt = s.CreatedAt.UTC().Format(time.RFC3339Nano)
		}
	}
	h.applyTurnTiming(&snap, sessionID)
	// Agent-loop activity (llm_running / active_tools / active_agents) from the
	// live agent, so this full snapshot agrees with the mid-turn
	// `agent_activity` events instead of blanking the status bar's activity row.
	h.applySessionActivity(&snap, sessionID)
	// Per-session permission mode (see applySessionPermissionFields).
	h.applySessionPermissionFields(&snap, sessionID)
	h.applySessionThinkingBudget(&snap, sessionID)
	// Per-session advisor gate (see applySessionAdvisorFields).
	h.applySessionAdvisorFields(&snap, sessionID)
	// Reflect the session's effective (override-or-default) model so the
	// sidebar's Context gauge and Model row stay in sync per session.
	snap.MainModel = h.effectiveSessionModel(sessionID)
	applySessionModelPrompt(&snap, baseModel, baseCWD)
	snap.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	// Persist the per-session spend total so it survives an agent rebuild /
	// idle eviction / restart; the snapshot above carries it for the live gauge.
	if as := h.lookupAgentSession(sessionID); as != nil {
		h.persistSessionTelemetry(sessionID, as)
	}
	h.broadcastEvent(SSEEvent{SessionID: sessionID, Event: "status", Data: snap})
}

// effectiveSessionModel returns the model that should drive the agent for the
// given session. A per-session override persisted in the session transcript's
// metadata (metadata["model"]) wins over the process-wide config model, which
// is the fallback for sessions that have never picked their own. This is what
// makes the sidebar's model a per-chat-session setting rather than one global
// value mirrored across every open tab.
func (h *Handler) effectiveSessionModel(id string) string {
	if id != "" {
		if entry, err := h.sessions.Resolve(id); err == nil {
			if s, err := session.LoadForDir(entry.ProjectRoot, id); err == nil && s.Metadata != nil {
				if m, ok := s.Metadata["model"].(string); ok && m != "" {
					return m
				}
			}
		}
	}
	// SetWorkDir replaces h.cfg under h.mu. Keep the lock limited to this
	// in-memory read; session resolution and transcript loading above must stay
	// outside the handler map lock.
	h.mu.Lock()
	model := ""
	if h.cfg != nil {
		model = h.cfg.Model
	}
	h.mu.Unlock()
	return model
}

// setSessionModelOverride persists (or clears, when model == "") a per-session
// model override in the session transcript metadata so the choice survives
// restart and resume. It returns the model
// that is now in effect for the session.
func (h *Handler) setSessionModelOverride(id, model string) (string, error) {
	entry, err := h.sessions.Resolve(id)
	if err != nil {
		return "", err
	}
	// Metadata-only write: a full load→save of the transcript conflicts
	// whenever the stored file holds rows the loader filters out (see
	// session.UpdateMetadataForDir), which made every model pick 404.
	err = session.UpdateMetadataForDir(entry.ProjectRoot, id, func(md map[string]any) {
		if model == "" {
			delete(md, "model")
		} else {
			md["model"] = model
		}
	})
	if err != nil {
		return "", err
	}
	return h.effectiveSessionModel(id), nil
}

// pushSessionStatusSnapshot broadcasts a session-tagged status snapshot whose
// model reflects the session's effective (override-or-default) model. Used
// after a web-initiated per-session model change so that tab's sidebar updates
// immediately without touching any other session. Skipped only for the
// bridged TUI's own session — the TUI owns that session's status feed and
// would clobber a web override on its next snapshot; every other session
// (which gets no TUI-side snapshots) relies on this push to update live.
func (h *Handler) pushSessionStatusSnapshot(id string) {
	if rc := h.RCBridge(); rc != nil && rc.SessionID == id {
		return
	}
	snap := h.buildStatusSnapshot()
	baseModel, baseCWD := snap.MainModel, snap.CWD
	snap.SessionID = id
	snap.MainModel = h.effectiveSessionModel(id)
	if entry, err := h.sessions.Resolve(id); err == nil && entry.ProjectRoot != "" {
		snap.CWD = entry.ProjectRoot
	}
	applySessionModelPrompt(&snap, baseModel, baseCWD)
	h.applySessionContext(&snap, id)
	h.applySessionUsage(&snap, id)
	h.applyTurnTiming(&snap, id)
	h.applySessionPermissionFields(&snap, id)
	h.applySessionThinkingBudget(&snap, id)
	h.applySessionAdvisorFields(&snap, id)
	if snap.SessionCreatedAt == "" {
		if entry, err := h.sessions.Resolve(id); err == nil {
			if s, err := session.LoadForDir(entry.ProjectRoot, id); err == nil && !s.CreatedAt.IsZero() {
				snap.SessionCreatedAt = s.CreatedAt.UTC().Format(time.RFC3339Nano)
			}
		}
	}
	snap.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	h.broadcastEvent(SSEEvent{SessionID: id, Event: "status", Data: snap})
}

// HandleSetSessionModel sets a per-session model override for id. It validates
// the model id the same way the global config-model setter does (a bare id
// without a provider prefix can't be resolved back to a provider), persists
// it in the session transcript metadata, and pushes a fresh per-session status
// snapshot so the web sidebar updates at once.
func (h *Handler) HandleSetSessionModel(w http.ResponseWriter, r *http.Request, id string) {
	var req struct {
		Model string `json:"model"`
	}
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !strings.Contains(req.Model, "/") && !strings.Contains(req.Model, ":") {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("model %q has no provider prefix; use a \"provider/model\" id", req.Model))
		return
	}
	h.mu.Lock()
	cfgNil := h.cfg == nil
	h.mu.Unlock()
	if cfgNil {
		writeError(w, http.StatusInternalServerError, "config not loaded")
		return
	}
	effective, err := h.setSessionModelOverride(id, req.Model)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	// Mirror the TUI's finishModelSwitch and the global config-model setter
	// (handler_config.go): every model pick lands in the shared recent list so
	// the picker's "Recently Used" section stays in sync across TUI, web and
	// desktop regardless of which surface made the switch.
	if strings.Contains(req.Model, "/") {
		if err := config.SaveRecentModel(req.Model); err != nil {
			log.Printf("save recent model: %v", err)
		}
	}
	h.pushSessionStatusSnapshot(id)
	writeJSON(w, http.StatusOK, map[string]string{"model": effective, "session_id": id})
}

// HandleClearSessionModel removes a per-session model override for id, so the
// session falls back to the process-wide config model again.
func (h *Handler) HandleClearSessionModel(w http.ResponseWriter, r *http.Request, id string) {
	effective, err := h.setSessionModelOverride(id, "")
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	h.pushSessionStatusSnapshot(id)
	writeJSON(w, http.StatusOK, map[string]string{"model": effective, "session_id": id})
}
