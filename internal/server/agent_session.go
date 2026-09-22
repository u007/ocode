package server

import (
	"errors"
	"fmt"
	"log"
	"math"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/auth"
	"github.com/u007/ocode/internal/computer"
	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/crashguard"
	"github.com/u007/ocode/internal/debuglog"
	"github.com/u007/ocode/internal/session"
	"github.com/u007/ocode/internal/tool"
	"github.com/u007/ocode/internal/usage"
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

// spentUSDMetadataKey is the session-metadata key holding the per-session
// accumulated LLM spend (USD), written at turn end and read back for a
// restored (non-live) session.
//
// It is deliberately the same key the TUI's sidebarTelemetry writes and reads
// (telemetryFromSessionMetadata), so a session that moves between the TUI and
// a headless web/desktop turn keeps ONE running total, and a session created
// by the TUI shows its history in the web sidebar instead of starting at 0.
const spentUSDMetadataKey = "spend"

// addSpendUSD adds v USD to the session's accumulated spend. Safe for
// concurrent use (atomic).
func (as *agentSession) addSpendUSD(v float64) {
	if as == nil || v == 0 {
		return
	}
	as.spentMicros.Add(int64(math.Round(v * 1e6)))
}

// seedSpend raises the accumulator to a restored session total, never lowers
// it. Called at agent build with the persisted total and at a rebuild with the
// outgoing agent's live total, so the gauge never regresses to 0 when an agent
// is rebuilt (profile reconcile, model switch, plugin reload, idle eviction,
// server restart) — that regression was why a session's spend history
// disappeared.
func (as *agentSession) seedSpend(usd float64) {
	if as == nil || usd <= 0 {
		return
	}
	micros := int64(math.Round(usd * 1e6))
	for {
		cur := as.spentMicros.Load()
		if micros <= cur || as.spentMicros.CompareAndSwap(cur, micros) {
			return
		}
	}
}

// sessionSpendFromMetadata extracts the persisted per-session spend total from
// transcript metadata, tolerating the numeric shapes a JSON round-trip can
// produce (float64) and typed writers (int, int64, float32).
func sessionSpendFromMetadata(md map[string]any) float64 {
	if md == nil {
		return 0
	}
	switch v := md[spentUSDMetadataKey].(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int64:
		return float64(v)
	case int:
		return float64(v)
	}
	return 0
}

// spendUSD returns the session's accumulated spend in USD.
func (as *agentSession) spendUSD() float64 {
	if as == nil {
		return 0
	}
	return float64(as.spentMicros.Load()) / 1e6
}

// addSpendFromMessages adds the Spend of a Step's new messages to the
// session total. The main turn's spend lives on its assistant/tool messages;
// summing the Step delta (never the whole transcript) avoids double counting.
func (as *agentSession) addSpendFromMessages(msgs []agent.Message) {
	var delta float64
	for i := range msgs {
		if msgs[i].Spend != nil {
			delta += *msgs[i].Spend
		}
	}
	as.addSpendUSD(delta)
}

// persistSessionSpend writes the session's accumulated spend into its
// transcript metadata so the web/desktop gauge survives an agent rebuild,
// idle eviction, or server restart. Metadata-only, so it cannot conflict with
// filtered transcript rows (see session.UpdateMetadataForDir).
func (h *Handler) persistSessionSpend(sessionID string, usd float64) {
	if sessionID == "" {
		return
	}
	projectRoot := h.sessionProjectRoot(sessionID)
	if projectRoot == "" {
		return
	}
	if err := session.UpdateMetadataForDir(projectRoot, sessionID, func(md map[string]any) {
		md[spentUSDMetadataKey] = usd
	}); err != nil {
		log.Printf("serve: persist spend for %s: %v", sessionID, err)
	}
}

// recordTurnUsage writes one usage-ledger row per Step message that carried
// provider usage, attributed to sessionID — the headless counterpart of the
// TUI's recordUsageFromMessage (internal/tui/model.go). Written
// asynchronously so a slow ledger write never blocks the turn; failures are
// logged, never fatal. promptTokens is normalized (excludes cache reads) so
// ledger ratios stay uniform across providers.
func (h *Handler) recordTurnUsage(sessionID string, as *agentSession, msgs []agent.Message) {
	provider := ""
	if as != nil && as.agent != nil {
		provider = as.agent.GetProvider()
	}
	for i := range msgs {
		msg := msgs[i]
		if msg.Usage == nil && msg.Spend == nil {
			continue
		}
		model := msg.Model
		if model == "" && as != nil {
			model = as.model
		}
		promptTokens := int64(0)
		completionTokens := int64(0)
		cacheReadTokens := int64(0)
		totalTokens := int64(0)
		if u := msg.Usage; u != nil {
			promptTokens = u.NormalizedPromptTokens()
			if u.CompletionTokens != nil {
				completionTokens = *u.CompletionTokens
			}
			if u.CacheReadTokens != nil {
				cacheReadTokens = *u.CacheReadTokens
			}
			if u.TotalTokens != nil {
				totalTokens = *u.TotalTokens
			} else {
				totalTokens = promptTokens + cacheReadTokens + completionTokens
			}
		}
		spend := 0.0
		if msg.Spend != nil {
			spend = *msg.Spend
		}
		crashguard.Go(func() {
			if err := usage.RecordUsageForSession(time.Now(), sessionID, model, provider,
				promptTokens, completionTokens, cacheReadTokens, totalTokens, spend); err != nil {
				log.Printf("usage: record for session %s: %v", sessionID, err)
			}
		})
	}
}

// advisorFlag reads the shared advisor gate under h.mu (it is flipped from the
// web sidebar via handler_config.go).
func (h *Handler) advisorFlag() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.advisorEnabled
}

// projectHostFor returns the saved remote host for projectRoot when it is
// registered as a remote (SSH/WSL) project on THIS server, else "" for a local
// project. A non-empty result drives the environment prompt's "Project host"
// line, which tells the model that the project root belongs to another machine
// (remote-project chat traffic is proxied to that host's `ocode serve
// --remote` process, so the project files, shell, home, and config/session
// paths all resolve there — see
// docs/superpowers/plans/2026-09-17-remote-project-agent-on-host/).
//
// h.projects.List() takes the store's own mutex, so this must be called with no
// handler lock held (buildAgentSession already honors that).
func (h *Handler) projectHostFor(projectRoot string) string {
	if h.projects == nil || projectRoot == "" {
		return ""
	}
	for _, p := range h.projects.List() {
		if p.Host != "" && p.Path == projectRoot {
			return p.Host
		}
	}
	return ""
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
	h.mu.Lock()
	effCfg := h.cfg
	h.mu.Unlock()
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
	var computerDriver tool.ComputerDriver
	var computerDriverErr error
	if effCfg != nil && effCfg.Ocode.ComputerUse.Enabled {
		computerDriver, computerDriverErr = computer.New(h.computerSup)
	}
	tools := tool.InitBuiltinToolsWithComputerDriver(lspMgr, effCfg, h.scheduler, computerDriver, computerDriverErr)
	ag := agent.NewAgent(client, tools, effCfg, lspMgr)
	ag.SetSessionID(sessionID)
	// The agent's workdir comes from the registry entry's project root, not
	// the process cwd — multi-project sessions run against their own repo
	// (environment prompt, file-edit snapshots, permissions, discovery all
	// follow SetWorkDir). An empty projectRoot means "the server's project
	// dir", never the process cwd: the desktop .app launches with cwd "/", so
	// leaving workDir empty made confinedPath resolve relative tool paths
	// ("TODO.md") against "/" and reject them as outside the working directory.
	if projectRoot == "" {
		projectRoot = h.workDir
	}
	ag.SetWorkDir(projectRoot)
	// Apply this session's own persisted permission-mode override, if any, so
	// every build path (bootstrap, profile reconcile, plugin reload) restores
	// the chat's mode. Per-session on purpose: one chat's yolo/sandbox toggle
	// must never leak into another chat or project. Resolution touches the
	// disk, so it happens here (no handler lock is held during construction).
	if mode, ok := sessionPermissionModeForDir(projectRoot, sessionID); ok {
		if pm := ag.Permissions(); pm != nil {
			pm.SetMode(mode)
		}
	}
	// Tag the environment prompt when this project is a remote (SSH/WSL)
	// project, so the model knows the project root (and the paths around it)
	// belong to another machine. Empty for local projects, keeping their prompt
	// byte-identical.
	ag.SetProjectHost(h.projectHostFor(projectRoot))
	// Child (sub-agent) sessions persist next to their parent, in the same
	// project's storage dir. The task tool calls this on every streamed
	// sub-agent message and once at completion, so it must be the live
	// (never-regress, coalescing) async save — a synchronous write per
	// message would stall the child's Step loop.
	ag.SetChildSessionPersistence(func(childID, title string, msgs []agent.Message, meta map[string]any) error {
		return session.SaveAsyncForDir(projectRoot, childID, title, msgs, meta)
	})

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

	// Seed the runtime advisor gate from the session's own persisted override
	// when it has one; sessions that never toggled follow the process-wide
	// default. Per-session on purpose: one chat's advisor toggle must never
	// leak into another chat, and a resume/restart re-seeds from metadata.
	ag.SetAdvisorEnabled(h.advisorSeed(sessionID, h.advisorFlag()))
	h.wireCompactCallbacks(sessionID, ag)
	as := &agentSession{agent: ag, messages: messages, model: model, profile: prof, credVersion: auth.ProfileCredentialVersion()}
	// Restore this session's spend history before anything reads the gauge. The
	// total lives in transcript metadata (the same "spend" key the TUI writes),
	// and a freshly built agent starts at zero, so without this seed the
	// sidebar would show only the current turn's spend after a rebuild/resume.
	if prev, err := session.LoadForDir(projectRoot, sessionID); err == nil {
		as.seedSpend(sessionSpendFromMetadata(prev.Metadata))
	}
	// Accumulate side-path spend (advisor, compaction, title, …) into this
	// session's own total. The main turn's spend is recorded from Step
	// messages in runTurn; without this, side queries would vanish from the
	// web's per-session gauge (the TUI wires the same callback for its own
	// sidebar). Uses the atomic accumulator, so no as.mu needed here.
	ag.OnSideUsage = func(_, _, _, _ int64, spend *float64) {
		if spend != nil {
			as.addSpendUSD(*spend)
		}
	}
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
	if ok && old != as {
		// Never let a rebuild regress the session's spend: the outgoing agent
		// holds turn spend that may not be persisted to metadata yet.
		as.seedSpend(old.spendUSD())
		if old.agent != nil && !h.sessions.IsTurnActive(id) {
			old.agent.Shutdown()
		}
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
	// Resolve this session's own persisted permission-mode override BEFORE
	// taking h.mu: the lookup may touch the disk, and h.mu is a short-lived
	// map lock, never a work lock. Per-session on purpose — a chat's
	// yolo/sandbox toggle must never leak into another chat or project.
	// Prefer the registry's resolved root: projectRoot may be "" ("keep the
	// already-resolved root"), and an empty root would read the wrong storage
	// dir for a multi-project session.
	permRoot := projectRoot
	if permRoot == "" {
		permRoot = entry.ProjectRoot
	}
	permMode, hasPermMode := sessionPermissionModeForDir(permRoot, id)
	h.mu.Lock()
	if existing, ok := h.agents[id]; ok {
		h.mu.Unlock()
		if as.agent != nil {
			as.agent.Shutdown()
		}
		return existing
	}
	h.agents[id] = as
	if as.agent != nil && hasPermMode {
		if pm := as.agent.Permissions(); pm != nil {
			pm.SetMode(permMode)
		}
	}
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
	startedAt, _ := h.sessions.TurnTiming(sessionID)
	if startedAt.IsZero() {
		startedAt = time.Now()
	}
	sessionCreatedAt := h.sessionCreatedAt(sessionID)
	data := map[string]string{
		"session_id": sessionID,
		"started_at": startedAt.UTC().Format(time.RFC3339Nano),
	}
	if !sessionCreatedAt.IsZero() {
		data["session_created_at"] = sessionCreatedAt.UTC().Format(time.RFC3339Nano)
	}
	h.publishBusEvent("turn_started", sessionID, data)
	// Push a session-tagged status snapshot so the web shows current-input
	// elapsed immediately (headless mode only; bridged TUI pushes its own
	// snapshot via broadcastTUIStatus with TurnStartedAt).
	if h.RCBridge() == nil {
		h.publishTurnStatusSnapshot(sessionID)
	}
}

// sessionCreatedAt returns the creation time of sessionID, or zero if unknown.
// It reads through the SessionManager's resolver (disk scan per project root)
// so it works for sessions that have not yet had an agent built.
func (h *Handler) sessionCreatedAt(sessionID string) time.Time {
	if sessionID == "" {
		return time.Time{}
	}
	entry, err := h.sessions.Resolve(sessionID)
	if err != nil || entry == nil {
		return time.Time{}
	}
	s, err := session.LoadForDir(entry.ProjectRoot, sessionID)
	if err != nil || s == nil {
		return time.Time{}
	}
	return s.CreatedAt
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
	startedAt, endedAt := h.sessions.TurnTiming(sessionID)
	if endedAt.IsZero() {
		endedAt = time.Now()
	}
	var tookMs int64
	if !startedAt.IsZero() {
		tookMs = endedAt.Sub(startedAt).Milliseconds()
	}
	ev := map[string]any{
		"session_id": sessionID,
		"model":      model,
	}
	if !startedAt.IsZero() {
		ev["started_at"] = startedAt.UTC().Format(time.RFC3339Nano)
		ev["ended_at"] = endedAt.UTC().Format(time.RFC3339Nano)
		ev["took_ms"] = tookMs
	}
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
	startedAt, endedAt := h.sessions.TurnTiming(sessionID)
	if endedAt.IsZero() {
		endedAt = time.Now()
	}
	var tookMs int64
	if !startedAt.IsZero() {
		tookMs = endedAt.Sub(startedAt).Milliseconds()
	}
	data := map[string]any{"session_id": sessionID, "error": err.Error()}
	if stage != "" {
		data["stage"] = stage
	}
	if !startedAt.IsZero() {
		data["started_at"] = startedAt.UTC().Format(time.RFC3339Nano)
		data["ended_at"] = endedAt.UTC().Format(time.RFC3339Nano)
		data["took_ms"] = tookMs
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
	model := h.effectiveSessionModel(id)
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
	// retryLast re-runs the existing transcript tail in place instead of
	// appending a new user message. Set by HandleRetrySession (the composer's
	// retry action after a Stop or an LLM-loop error); runTurn then skips both
	// the user-row append and the `user_message` echo, so a retry never
	// duplicates the user's message. Mirrors the TUI's Ctrl+Y retry
	// (model.retryLastLLMError).
	retryLast bool
}

// runTurn executes one agent turn: appends the user message (unless
// opts.retryLast re-runs the existing transcript tail), steps the agent,
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

	// A retry re-runs the transcript tail in place: the user's message is
	// already the last turn in the transcript, so appending it again would
	// duplicate it (and re-echo it to the browser). Skip both — Step runs on
	// the existing rows, exactly like the TUI's Ctrl+Y retry.
	var userSeq int
	if !opts.retryLast {
		userSeq = nextUserSeq(as.messages)
		as.messages = append(as.messages, agent.Message{Role: "user", Content: content, UserSeq: userSeq})
	}
	messages := append([]agent.Message(nil), as.messages...)
	// turnBaseLen is the turn's base transcript (everything through this
	// turn's user message) — captured BEFORE the auto-continue loop below can
	// append resume prompts to `messages`. persistTurnTranscript must compare
	// against this base: passing the grown len(messages) made the stored
	// prefix mismatch and the turn-end reconcile hard-diverge, silently
	// dropping the mid-turn notices and resume prompts from memory.
	turnBaseLen := len(messages)

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
	stopHeartbeat := h.startTurnHeartbeat(sessionID)
	defer func() {
		stopHeartbeat()
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
		// Broadcast the user message so the SSE mirror can echo it. The frame
		// carries the same user_seq stamped on the persisted/transcript copy, so
		// the web frontend can dedupe the snapshot-before-SSE race by
		// (sessionId, user_seq) identity. A retry re-runs the existing tail, so
		// there is no new user row to echo.
		if !opts.retryLast {
			h.broadcastEvent(SSEEvent{
				SessionID: sessionID,
				Event:     "user_message",
				Data:      map[string]any{"content": content, "user_seq": userSeq},
			})
		}
		h.wireHeadlessAgentCallbacks(sessionID, as.agent)
		// Live-persist each completed step message as the turn streams, so a
		// crash mid-turn loses at most the in-flight LLM round. Headless
		// only: a bridged TUI persists its own transcript live.
		h.wireLivePersist(sessionID, as, messages)
	}

	// Ensure a prior Cancel() doesn't permanently poison this session:
	// ResetCancellation replaces a closed stop channel with a fresh one
	// so the next Step isn't immediately cancelled.
	as.agent.ResetCancellation()

	// A new user-submitted turn is fresh direction, so clear the
	// consecutive-subagent-dispatch counter the re-dispatch guard reads
	// (agent.subagentDispatchLimit). Without this the guard counts across
	// TURNS, not just within one runaway loop: the reset was wired into the
	// TUI's send path only, so in the web/desktop (headless) server a resident
	// agent that dispatched the same subagent N times was locked out for the
	// rest of its life, across every later user message. Reset ONCE here, not
	// per Step below, so an auto-continue chain inside one turn still shares
	// the cap. (Cron does not need this: scheduler_runner builds a fresh agent
	// per firing, whose counter starts at zero.)
	as.agent.ResetSubagentDispatch()

	// Auto-continue chain state (mirrors the TUI's autoContinueCount):
	// consecutive auto-fired resumes within one runTurn call share the cap;
	// any human-submitted turn starts a fresh chain.
	autoContinueCount := 0
	for {
		resp, err := as.agent.Step(messages)
		if err != nil {
			log.Printf("serve error: agent step: %v", err)
			// Keep whatever the turn produced before it failed. Step returns the
			// completed rounds alongside the error, and those were already streamed
			// to the browser — discarding them here is what made a failed turn
			// reopen as nothing but the user's own message.
			h.commitPartialTranscript(sessionID, as, as.messages, resp, headless)
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
		// Agent.Step can return (newMsgs, nil) when cancelled right after a
		// successful LLM call (isCancelled check inside Step). Treat that as a
		// cancellation so the caller stops draining queued messages.
		if as.agent.Cancelled() {
			h.commitPartialTranscript(sessionID, as, as.messages, resp, headless)
			cancelErr := fmt.Errorf("cancelled")
			h.publishTurnError(sessionID, cancelErr, "")
			if headless {
				h.broadcastEvent(SSEEvent{
					SessionID: sessionID,
					Event:     "error",
					Data:      map[string]string{"error": cancelErr.Error()},
				})
			}
			return "", cancelErr
		}

		as.messages = append(as.messages, resp...)
		// Accumulate this Step's main-path spend into the session total so the
		// web/desktop Context panel can show a per-session figure.
		as.addSpendFromMessages(resp)
		// Mirror the TUI's usage ledger for headless turns: web/desktop LLM
		// calls were never recorded, so /api/spending and /usage undercounted
		// them. Records carry the session id, so a session's spend can be
		// summed back (usage.SessionSpend) even if its metadata total is lost.
		if headless {
			h.recordTurnUsage(sessionID, as, resp)
		}
		// Keep the LLM input (`messages`) in lockstep with the persisted
		// transcript. Agent.Step returns its newMsgs without mutating the
		// caller's slice, so without this the resumed Step below would receive
		// the original user turn + resume hint with none of the assistant/tool
		// rows the step just produced — the model would re-plan from scratch
		// instead of continuing.
		messages = append(messages, resp...)

		// General-purpose auto-continue (mirrors the TUI streamDoneMsg path):
		// a turn cut off by the /max-step cap resumes immediately up to the
		// chain cap; a naturally-ended turn is optionally triaged by the
		// configured judge model (typesafe Decide or chat YES/NO). A pause on
		// an unresolved permission/question ask never auto-continues — the
		// turn is over until the dialog is answered.
		should, triageDetail := h.autoContinueShouldResume(sessionID, as, autoContinueCount)
		if should {
			if hint, ok := h.fireAutoContinue(sessionID, as, headless); ok {
				autoContinueCount++
				messages = append(messages, hint)
				continue
			}
		}
		// The chain declined to fire (judge said the reply finished, or a
		// guard blocked the resume): surface the triage outcome so the turn
		// does not silently look done. StepLimitHitDetail covers the
		// step-limit/error cases below; this covers the judge verdict.
		if triageDetail != "" {
			h.appendTranscriptNotice(as, headless, triageDetail)
		}

		break
	}

	// The chain has settled. Surface WHY the turn ended when it is not a
	// natural completion, so the web UI shows "cut off / declined" instead of
	// silently looking done (the TUI parity gap this closes).
	if detail := as.agent.StepLimitHitDetail(nil); detail != "" {
		h.appendTranscriptNotice(as, headless, detail)
	}

	var reply strings.Builder
	// Only this turn's rows — everything appended after the base captured
	// before the auto-continue loop. Iterating all of as.messages (the previous
	// behavior) returned the whole session's assistant text to the synchronous
	// POST /api/chat callers, growing every turn.
	for _, m := range as.messages[turnBaseLen:] {
		if m.Role == "assistant" && m.Content != "" {
			reply.WriteString(m.Content)
		}
	}

	// Persist the turn durably before the UI broadcast. baseLen is the
	// turn's base transcript (everything through the user message); the
	// reconcile inside persistTurnTranscript rebases this turn's response
	// on top of rows another writer appended concurrently, and on true
	// base divergence re-syncs memory to disk (logged) so the session
	// stays writable instead of every later save conflicting forever.
	h.persistTurnTranscript(sessionID, as, turnBaseLen, "turn-end")

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

// autoContinueChainCap bounds consecutive auto-fired resumes within one
// server turn, mirroring the TUI's autoContinueMaxChain (agent.AutoContinueChainCap).
const autoContinueChainCap = agent.AutoContinueChainCap

// autoContinueShouldResume decides whether the just-finished Step should be
// auto-resumed, mirroring the TUI streamDoneMsg logic for headless (and
// bridged-web session) turns: a hard /max-step cutoff resumes immediately up
// to the chain cap; a naturally-ended turn is triaged by the configured judge
// model (typesafe Decide or chat YES/NO) when auto-continue is enabled. A
// pause on an unresolved permission/question ask, an errored turn, or a
// session waiting on its dialog never auto-continues. detail is the
// user-facing triage outcome for the transcript.
func (h *Handler) autoContinueShouldResume(sessionID string, as *agentSession, chainCount int) (should bool, detail string) {
	if as == nil || as.agent == nil {
		return false, ""
	}
	// A turn paused on a pending permission/question sentinel must not be
	// resumed behind the user's back — the dialog owns the next move.
	if tailIsPermissionAsk(as.messages) || tailIsQuestionAsk(as.messages) {
		return false, ""
	}
	if !as.agent.AutoContinueEnabled() {
		return false, ""
	}
	if chainCount >= autoContinueChainCap {
		return false, ""
	}
	// Hard signal first (free, no extra LLM call): the turn was cut off by
	// the /max-step cap and forced into "stop and summarize".
	if as.agent.StepLimitHit() {
		return true, fmt.Sprintf("step-limit cutoff (chain %d/%d) — resuming", chainCount+1, autoContinueChainCap)
	}
	// Natural stop: ask the configured triage judge whether the reply looks
	// interrupted. No judge configured → skip silently (opt-in feature).
	// The caller's Step error never reaches here (runTurn returns on it), so
	// there is no end-of-turn error to report to the judge.
	resume, detail, err := as.agent.AutoContinueJudgeSync(as.messages, nil)
	if err != nil {
		log.Printf("serve: auto-continue judge for %s: %v", sessionID, err)
	}
	if resume && detail != "" {
		detail = fmt.Sprintf("%s (chain %d/%d)", detail, chainCount+1, autoContinueChainCap)
	}
	return resume, detail
}

// fireAutoContinue appends the auto-continue hint plus the explicit resume
// prompt as a user message (transcript + LLM input) and returns it so the
// runTurn loop can feed the next Step. Mirrors the TUI fireAutoContinue
// wording so both surfaces read the same.
func (h *Handler) fireAutoContinue(sessionID string, as *agentSession, headless bool) (agent.Message, bool) {
	if as == nil || as.agent == nil {
		return agent.Message{}, false
	}
	stepLimited := as.agent.StepLimitHit()
	continuePrompt := "Continue the task from where you left off; do not just repeat any previous summary."
	if stepLimited {
		// The step-limit cutoff just told the model "Stop using tools and
		// respond with a summary" — explicitly countermand it, or the model
		// is likely to just re-summarize instead of resuming work.
		continuePrompt = "The step limit has been reset — you may use tools again. " + continuePrompt
	}
	reason := "the auto-continue judge flagged this reply as cut off"
	if stepLimited {
		reason = "cut off by /max-step"
	}
	hint := fmt.Sprintf("↩ auto-continue — %s, resuming", reason)
	notice := agent.Message{Role: "assistant", Content: "", Notice: hint}
	h.appendTranscriptMessage(as, headless, notice)
	prompt := agent.Message{Role: "user", Content: continuePrompt, UserSeq: nextUserSeq(as.messages)}
	as.messages = append(as.messages, prompt)
	// Keep the live-persisted view in lockstep with the transcript (see
	// appendTranscriptMessage) so the turn-end reconcile sees a prefix.
	if headless && as.liveAppend != nil {
		as.liveAppend(prompt)
	}
	if headless {
		h.broadcastEvent(SSEEvent{
			SessionID: sessionID,
			Event:     "user_message",
			Data:      map[string]any{"content": prompt.Content, "user_seq": prompt.UserSeq},
		})
	}
	return prompt, true
}

// appendTranscriptMessage adds a UI-only message to the transcript (persisted
// with the turn, shown by the web) WITHOUT feeding it to the LLM: runTurn's
// `messages` slice is the LLM input and deliberately does not receive it.
// Content is empty and the text rides Message.Notice; both provider builders
// keep it out of the request (the OpenAI converter skips notice-only rows;
// Anthropic drops zero-block messages), the same contract as the TUI's
// transient messages.
func (h *Handler) appendTranscriptMessage(as *agentSession, headless bool, m agent.Message) {
	as.messages = append(as.messages, m)
	// Mirror into the live-persist view (headless only; bridged turns persist
	// their own transcript). A mid-chain row that only lives in memory makes the
	// turn-end reconcile merge a suffix that is not a prefix of what is on disk,
	// duplicating step rows and reordering this row.
	if headless && as.liveAppend != nil {
		as.liveAppend(m)
	}
}

// appendTranscriptNotice surfaces an end-of-turn status one-liner (why the
// turn ended) in the transcript without polluting the LLM input.
func (h *Handler) appendTranscriptNotice(as *agentSession, headless bool, detail string) {
	h.appendTranscriptMessage(as, headless, agent.Message{Role: "assistant", Content: "", Notice: detail})
}

// commitPartialTranscript stores, persists and mirrors the transcript of a
// turn that failed part-way through. Every message in base+resp was already
// streamed to the browser (and every tool result in it already ran), so a
// failed final LLM round must not erase it: without this, reopening the
// session shows nothing but the user's own message. mirror is false for
// bridged sessions, which broadcast their own frames.
// turn (or partial turn), with one bounded concurrent-writer reconcile: on
// a save conflict, the raw disk transcript is reloaded and merged (stored
// rows kept, the caller's not-yet-stored suffix appended after them — see
// session.ReconcileAppendForDir), so a racing writer's rows and this
// session's response both survive instead of the response staying
// memory-only with every later save conflicting. If the base itself
// diverged (two writers produced different content for the same position),
// no safe merge exists: memory is re-synced to the stored transcript and
// the dropped in-memory suffix is logged explicitly, so the session stays
// writable and the loss is visible rather than silently permanent.
func (h *Handler) persistTurnTranscript(sessionID string, as *agentSession, baseLen int, label string) {
	merged, err := h.reconcileTurnSave(sessionID, as, baseLen)
	if err == nil {
		if merged != nil {
			// A successful rebase may have inserted another writer's rows
			// between this turn's base and suffix. Adopt the merged transcript
			// so the next turn's base matches disk. It is the UNFILTERED view:
			// a trailing pending ask (PERMISSION_ASK/QUESTION sentinel + its
			// tool-call) must survive here, or tailIsPermissionAsk goes false
			// and the next user message re-executes the orphaned call instead of
			// waiting for the dialog (ses_2026-09-18-233409-df1a92d3).
			as.messages = merged
		}
		return
	}
	if session.IsConflictErr(err) {
		// Hard divergence: converge memory to the stored transcript so the
		// NEXT save is guaranteed consistent instead of conflicting
		// forever. Broadcast ordering (below) uses the converged view, so
		// the UI mirrors what is actually durable.
		s, loadErr := h.loadSession(sessionID)
		if loadErr != nil {
			log.Printf("serve: %s save for %s diverged and disk reload failed: %v (save err: %v)", label, sessionID, loadErr, err)
			return
		}
		log.Printf("serve: %s save for %s diverged from a concurrent writer; re-synced to disk (stored %d msgs, in-memory %d msgs, dropped suffix %d msgs)", label, sessionID, len(s.Messages), len(as.messages), max(0, len(as.messages)-len(s.Messages)))
		as.messages = s.Messages
		return
	}
	log.Printf("serve: %s save for %s: %v", label, sessionID, err)
}

// reconcileTurnSave resolves the session's owning project exactly like
// saveSession, then persists with the bounded concurrent-writer rebase.
func (h *Handler) reconcileTurnSave(sessionID string, as *agentSession, baseLen int) ([]agent.Message, error) {
	if e, ok := h.sessions.SnapshotEntry(sessionID); ok && e.ProjectRoot != "" {
		return session.ReconcileAppendForDirWithMessages(e.ProjectRoot, sessionID, "", as.messages, baseLen, nil)
	}
	return session.ReconcileAppendWithMessages(sessionID, "", as.messages, baseLen, nil)
}

// commitPartialTranscript keeps whatever the turn produced before it
// failed/was cancelled: it persists base+resp synchronously (with the
// concurrent-writer reconcile of persistTurnTranscript) before the error
// frame goes out, then mirrors the transcript.
func (h *Handler) commitPartialTranscript(sessionID string, as *agentSession, base []agent.Message, resp []agent.Message, mirror bool) {
	if len(base) == 0 && len(resp) == 0 {
		return
	}
	// Copy: callers build base/resp over the session's own slice, so
	// storing the append result directly would leave two slice headers
	// sharing one backing array — a later append through either one (an
	// injection flush, a compact result) would write into the other's
	// elements.
	as.messages = append(append([]agent.Message(nil), base...), resp...)
	h.persistTurnTranscript(sessionID, as, len(base), "partial-transcript")
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

// startTurnHeartbeat starts the periodic turn_heartbeat publisher for a
// session and returns a function that stops and joins it. EVERY path that holds
// turnActive=true must publish heartbeats for the duration, or the web client's
// 30s stall watchdog (web/src/hooks/useTurnWatchdog.ts) marks a still-running
// session "stalled". runTurn started the ticker inline; the permission-answer
// and question-answer continuation Steps set turnActive without one, so a
// continuation that streamed for minutes (with turn_active:true but no
// heartbeat) showed up as a false "stalled" badge in the project list.
func (h *Handler) startTurnHeartbeat(sessionID string) func() {
	stop := make(chan struct{})
	done := make(chan struct{})
	go h.turnHeartbeat(sessionID, stop, done)
	return func() {
		close(stop)
		<-done
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

func (h *Handler) saveLockFor(path string) *sync.Mutex {
	h.saveMu.Lock()
	defer h.saveMu.Unlock()
	if h.saveLocks == nil {
		h.saveLocks = make(map[string]*sync.Mutex)
	}
	l, ok := h.saveLocks[path]
	if !ok {
		l = &sync.Mutex{}
		h.saveLocks[path] = l
	}
	return l
}

// dispatchTurn starts a turn on its own goroutine, serialized per session
// (single-flight bootstrap + ordered turns). model is used only when the
// session has no resident agent yet. The caller waits on job.persistAck to
// return 202 once the message is durable.
//
// The job is registered in turnInFlight BEFORE the goroutine starts, so a
// HandleCancelSession racing this dispatch (cancel arrives between the
// request and the job's first pendingCancel check) still sees an active
// session and records the cancellation. The counter is released when the job
// goroutine finishes — executing, erroring, or cancelled — so a queued
// second job keeps the session visible as in-flight while the first drains.
func (h *Handler) dispatchTurn(id, model, content string, opts turnOptions) (*turnJob, error) {
	job := &turnJob{content: content, model: model, opts: opts, persistAck: make(chan struct{})}
	// Refuse new turns once shutdown has begun: shutdown joins a bounded job
	// set, so a turn dispatched after the join starts could create plugin/model
	// processes, register an agent, and write after shutdown began.
	h.shutdownMu.Lock()
	if h.shutdownStarted {
		h.shutdownMu.Unlock()
		return nil, fmt.Errorf("server is shutting down")
	}
	h.turnJobsWG.Add(1)
	h.shutdownMu.Unlock()
	h.cancelMu.Lock()
	if h.turnInFlight == nil {
		h.turnInFlight = make(map[string]int)
	}
	h.turnInFlight[id]++
	h.cancelMu.Unlock()
	go func() {
		defer h.turnJobsWG.Done()
		h.executeTurnJob(id, job)
		h.cancelMu.Lock()
		if n := h.turnInFlight[id] - 1; n <= 0 {
			delete(h.turnInFlight, id)
		} else {
			h.turnInFlight[id] = n
		}
		h.cancelMu.Unlock()
	}()
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

	// If HandleCloseSession marked this session close-pending while the turn
	// (or its bootstrap) was in flight, release the agent as soon as the job
	// exits — the release couldn't run while the turn owned the agent, and a
	// RACE at this point must not leave the teardown to the 5-min idle sweep.
	// Runs on every exit path (success, error, cancellation, failed/aborted
	// bootstrap) because the marker is drained and only acted on when set.
	defer h.drainPendingClose(id)

	entry := h.sessions.Lookup(id)
	if entry == nil {
		job.err = fmt.Errorf("session not found")
		close(job.persistAck)
		return
	}

	// 1. Durable persist before the caller's 202. A retry has no new user row
	// to persist — its message is already the transcript tail — so skip the
	// write and the pending queue entirely; appending it again would duplicate
	// the user's message in the transcript.
	if job.opts.retryLast {
		close(job.persistAck)
	} else {
		if err := h.persistUserMessage(entry, job.content); err != nil {
			log.Printf("serve error: persist user message for %s: %v", id, err)
			job.err = err
			close(job.persistAck)
			return
		}
		h.sessions.PushPending(id, job.content)
		close(job.persistAck)
	}

	// Check for cancellation that arrived during persist or before bootstrap.
	// If cancelled, keep the pending message for retry after Resume — don't
	// shift it, just publish a cancelled turn_error so the UI clears.
	if h.consumePendingCancel(id) {
		h.publishTurnError(id, fmt.Errorf("cancelled"), "")
		return
	}

	// 2. Bootstrap once if needed. Failure surfaces as turn_error carrying the
	// failing stage; the message stays pending (durable) for the next job.
	as := h.lookupAgentSession(id)
	if as == nil {
		var err error
		var stage string
		as, stage, err = h.bootstrapEntryAgent(entry, job.model)
		// Bootstrap cancellation check: if Stop was pressed while the agent
		// was being built (agent not yet registered), abort without consuming
		// the pending message so it can be retried after Resume.
		if h.isPendingCancel(id) {
			h.consumePendingCancel(id)
			if as != nil && as.agent != nil {
				as.agent.Cancel()
			}
			h.publishTurnError(id, fmt.Errorf("cancelled"), stage)
			return
		}
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
		reconcileModel = h.effectiveSessionModel(id)
	}
	if reb, err := h.reconcileProfileAgent(id, as, reconcileModel); err != nil {
		log.Printf("serve error: reconcile profile for %s: %v", id, err)
	} else {
		as = reb
	}

	// Also check cancellation after reconcile before starting turns.
	if h.consumePendingCancel(id) {
		if as != nil && as.agent != nil {
			as.agent.Cancel()
		}
		h.publishTurnError(id, fmt.Errorf("cancelled"), "")
		return
	}

	// 3a. A retry re-runs the existing transcript tail exactly once, in place —
	// there is no queued message to drain. Mirrors runTurn's retryLast handling.
	if job.opts.retryLast {
		_, err := h.runTurn(id, as, "", job.opts)
		// Treat a post-cancellation successful return as a cancellation so the
		// pending-cancel marker does not linger (see the draining loop below).
		if err == nil && as != nil && as.agent != nil && as.agent.Cancelled() {
			err = fmt.Errorf("cancelled")
		}
		if errors.Is(err, ErrPermissionPending) {
			h.publishTurnError(id, err, "")
			return
		}
		if err != nil {
			h.consumePendingCancel(id)
		}
		return
	}

	// 3. Turn every pending message in order.
	for {
		content, ok := h.sessions.PendingFront(id)
		if !ok {
			break
		}
		// Cancellation between pending items — stop draining queued messages
		// until the user resumes. Preserve remaining queue.
		if h.isPendingCancel(id) {
			h.consumePendingCancel(id)
			h.publishTurnError(id, fmt.Errorf("cancelled"), "")
			return
		}
		_, err := h.runTurn(id, as, content, job.opts)
		// Treat a post-cancellation successful return as a cancellation so
		// queued messages are not auto-drained. Agent.Step can return
		// (newMsgs, nil) when cancelled after a successful LLM call.
		if err == nil && as != nil && as.agent != nil && as.agent.Cancelled() {
			err = fmt.Errorf("cancelled")
		}
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
		// If cancelled (or any error), stop draining — don't start the next
		// queued turn until Resume. The remaining pending messages stay queued.
		if err != nil {
			// Ensure the cancel flag is cleared so the next turn isn't
			// immediately poisoned.
			h.consumePendingCancel(id)
			return // runTurn published turn_error
		}
		// Only the first turn of the job carries the request's turn options
		// (session_started correlation); catch-up turns are plain.
		job.opts = turnOptions{}
		// Check again after a successful turn in case Stop arrived during
		// the turn's tail — don't auto-drain the next queued message.
		if h.isPendingCancel(id) {
			h.consumePendingCancel(id)
			return
		}
	}
}

// bootstrapEntryAgent builds the agent for a registry entry from the
// session's on-disk transcript, stripping the trailing messages that are
// still pending (persisted but not yet turned — runTurn re-appends them at
// turn time). Emits session_bootstrap stage events via buildAgentSession.
func (h *Handler) bootstrapEntryAgent(entry *sessionEntry, model string) (*agentSession, string, error) {
	if model == "" {
		model = h.effectiveSessionModel(entry.SessionID)
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
	// AppendUserMessageForDir loads the current disk transcript, appends the
	// user message, and retries (bounded) when another writer appended
	// concurrently — the exact race that used to surface as "conflicting
	// message at seq N (concurrent writers diverged)" and drop the user's
	// message. See session.AppendUserMessageForDir.
	return session.AppendUserMessageForDir(entry.ProjectRoot, entry.SessionID, content)
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
	// Mid-turn injection is still NEW user input, so it must clear the
	// consecutive-subagent-dispatch counter too — otherwise the guard keeps
	// refusing with "without any new user input" after the user just sent one.
	as.agent.ResetSubagentDispatch()
	return true
}

// nextUserSeq is session.NextUserSeq: the durable per-session sequence for
// the NEXT user message. Shared with the async pre-persist
// (session.AppendUserMessageForDir) so the on-disk and in-memory copies of
// one user message carry the same seq and serialize identically.
func nextUserSeq(messages []agent.Message) int {
	return session.NextUserSeq(messages)
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
	if len(as.messages) < r.OriginalLen || r.ReplaceFrom < 0 || r.ReplaceTo > len(as.messages) || r.ReplaceFrom > r.ReplaceTo {
		as.mu.Unlock()
		log.Printf("serve: auto-compaction result for session %s dropped: transcript changed (len=%d, splice=[%d:%d), snapshot=%d)",
			sessionID, len(as.messages), r.ReplaceFrom, r.ReplaceTo, r.OriginalLen)
		return
	}
	compacted := make([]agent.Message, 0, r.ReplaceFrom+1+len(as.messages)-r.ReplaceTo)
	compacted = append(compacted, as.messages[:r.ReplaceFrom]...)
	compacted = append(compacted, r.Summary)
	compacted = append(compacted, as.messages[r.ReplaceTo:]...)
	as.messages = compacted

	if err := h.replaceSession(sessionID, "", as.messages, nil); err != nil {
		log.Printf("serve: persisting compacted transcript for session %s: %v", sessionID, err)
	}
	if h.RCBridge() == nil {
		h.broadcastEvent(SSEEvent{
			SessionID: sessionID,
			Event:     "messages",
			Data:      as.messages,
		})
	}
	as.mu.Unlock()

	// Refresh the per-session status snapshot so the sidebar's Context gauge
	// reflects the shrunken transcript. Async auto-compaction runs on its own
	// goroutine; without this push the gauge kept the stale pre-compaction
	// reading until the next turn. Runs after the unlock (no-op under an RC
	// bridge).
	h.publishTurnStatusSnapshot(sessionID)
}

// saveSession persists a transcript to the session's owning project's storage
// dir — multi-project sessions must not land in the server's own project (the
// process workdir). Falls back to the process default only when the registry
// entry is unknown. The root is read from an immutable snapshot so a
// concurrent rebinding cannot move persistence mid-call.
func (h *Handler) saveSession(sessionID, title string, msgs []agent.Message, metadata map[string]any) error {
	if e, ok := h.sessions.SnapshotEntry(sessionID); ok && e.ProjectRoot != "" {
		return session.SaveForDir(e.ProjectRoot, sessionID, title, msgs, metadata)
	}
	return session.Save(sessionID, title, msgs, metadata)
}

// rewriteAskResult mirrors an in-memory ask resolution (the user answered a
// PERMISSION_ASK / QUESTION_PROMPT tool result at msgs[seq]) onto the stored
// row BEFORE the continuation Steps. The resolve handlers rewrite that row's
// content in place; unless disk changes identically, every later live/sync
// save overlaps the stored sentinel with different bytes and drops or
// conflicts, freezing the on-disk transcript at the already-answered ask
// while the agent keeps going (ses_2026-09-10-153254-b55bec37). Failure is
// logged, not fatal: the continuation still runs and its turn-end save
// reports the divergence itself.
func (h *Handler) rewriteAskResult(sessionID string, msgs []agent.Message, seq int) {
	if seq < 0 || seq >= len(msgs) {
		log.Printf("serve: rewrite ask result for %s: seq %d out of range (%d msgs)", sessionID, seq, len(msgs))
		return
	}
	e, ok := h.sessions.SnapshotEntry(sessionID)
	if !ok || e.ProjectRoot == "" {
		log.Printf("serve: rewrite ask result for %s: session has no project root", sessionID)
		return
	}
	if err := session.RewriteAskResultForDir(e.ProjectRoot, sessionID, seq, msgs[seq]); err != nil {
		log.Printf("serve: rewrite ask result for %s seq %d: %v", sessionID, seq, err)
	}
}

// replaceSession persists an authoritative transcript replacement into the
// session's owning project's storage dir. Compaction must go through this
// path: ordinary saves can no longer shrink the transcript — a shorter
// snapshot conflicts (session.ErrTranscriptConflict) instead of silently
// deleting rows another writer appended — while ReplaceForDir rewrites the
// stored history to exactly msgs and bumps history_gen so queued
// pre-compaction live snapshots drop instead of resurrecting replaced
// history. Falls back to the process default only when the registry entry
// is unknown.
func (h *Handler) replaceSession(sessionID, title string, msgs []agent.Message, metadata map[string]any) error {
	if e, ok := h.sessions.SnapshotEntry(sessionID); ok && e.ProjectRoot != "" {
		return session.ReplaceForDir(e.ProjectRoot, sessionID, title, msgs, metadata)
	}
	return session.Replace(sessionID, title, msgs, metadata)
}

// saveSessionAsync enqueues a live transcript snapshot for background
// persistence and returns immediately. It resolves the session's owning
// project exactly like saveSession. Snapshots use live (never-regress)
// semantics, so a queued snapshot that a newer sync save already superseded
// is a harmless no-op; the turn-end saveSession stays authoritative.
func (h *Handler) saveSessionAsync(sessionID, title string, msgs []agent.Message, metadata map[string]any) error {
	if e, ok := h.sessions.SnapshotEntry(sessionID); ok && e.ProjectRoot != "" {
		return session.SaveAsyncForDir(e.ProjectRoot, sessionID, title, msgs, metadata)
	}
	return session.SaveAsync(sessionID, title, msgs, metadata)
}

// wireLivePersist wraps the agent's OnMessage — installed by the caller
// (wireHeadlessAgentCallbacks, or the inline callbacks in handler_sse.go),
// which must run immediately before — so each message completed during
// the upcoming Step is appended to the session's on-disk transcript in the
// background instead of waiting for turn end. base is the about-to-Step
// from; it is persisted immediately too, so the turn's opening user message
// is durable even if the LLM call never returns.
//
// The closure holds its own mutex because OnMessage can fire from parallel
// tool-dispatch goroutines inside Step, not just the calling goroutine. Only
// the snapshot copy is taken under it; the enqueue itself runs outside.
func (h *Handler) wireLivePersist(sessionID string, as *agentSession, base []agent.Message) {
	prev := as.agent.OnMessage
	var mu sync.Mutex
	live := append([]agent.Message(nil), base...)
	if err := h.saveSessionAsync(sessionID, "", live, nil); err != nil {
		log.Printf("serve: live persist for %s: %v", sessionID, err)
	}
	// appendLive is shared by the OnMessage wrapper (step messages) and by
	// appendTranscriptMessage/fireAutoContinue (mid-chain notice and resume
	// prompt). Both must feed the same view or the turn-end reconcile sees a
	// suffix that is not a prefix of what was persisted.
	appendLive := func(m agent.Message) {
		mu.Lock()
		live = append(live, m)
		cp := append([]agent.Message(nil), live...)
		mu.Unlock()
		if err := h.saveSessionAsync(sessionID, "", cp, nil); err != nil {
			log.Printf("serve: live persist for %s: %v", sessionID, err)
		}
	}
	as.agent.OnMessage = func(m agent.Message) {
		if prev != nil {
			prev(m)
		}
		appendLive(m)
	}
	as.liveAppend = appendLive
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
