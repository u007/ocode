package server

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/debuglog"
	"github.com/u007/ocode/internal/lsp"
	"github.com/u007/ocode/internal/monaco"
	"github.com/u007/ocode/internal/projects"
	"github.com/u007/ocode/internal/scheduler"
	"github.com/u007/ocode/internal/secretjob"
	"github.com/u007/ocode/internal/session"
	shellpkg "github.com/u007/ocode/internal/shell"
	ocodesync "github.com/u007/ocode/internal/sync"
	"github.com/u007/ocode/internal/tabs"
)

type Handler struct {
	mu        sync.Mutex
	agents    map[string]*agentSession
	cfg       *config.Config
	rc        *RCBridge          // set when proxying to a TUI session
	scheduler *scheduler.Service // when set, the `cron` tool is wired into agent sessions
	// sessions is the single authority for session ID → project root + agent
	// lifecycle. Every session-scoped handler resolves through it, so sessions
	// from any registered project load and run (no more cross-project 404s).
	sessions *SessionManager
	// secretJobs runs cancellable, progress-reporting directory-wide
	// encrypt/decrypt jobs (see handler_secret.go), streaming progress as
	// secret_progress/secret_done/secret_error/secret_cancelled events on bus.
	secretJobs *secretjob.Manager

	// bus is the unified tagged event bus (Part 02). Every emitters publishes
	// envelopes here; /api/events streams them to web clients.
	bus *EventBus
	// runsEmitterOn and watchEmittersOn are the once-per-handler start guards
	// for the server-push emitters (runs / git / spending / logs), so repeated
	// /api/events connections only launch each loop once.
	runsEmitterOn          atomic.Bool
	watchEmittersOn        atomic.Bool
	logsEmitterOn          atomic.Bool
	terminalProcsEmitterOn atomic.Bool
	// mcpCache holds the process-wide MCP tool enumeration. MCP server config
	// (h.cfg.MCP) is identical for every session, so connecting to each
	// server is done once per process instead of once per session - see
	// newMCPCache.
	mcpCache *mcpCache
	// advisorEnabled is the runtime gate for the advisor tool, shared by all
	// agents this handler creates. Seeded from config, flipped from the web
	// sidebar, never persisted back to config.
	advisorEnabled bool
	// windowProfiles caches windowId -> activeProfile (empty = Default).
	// Hydrated from window-state.json once at startup; mutated only via
	// handleSetWindowActiveProfile which also persists to disk. Avoids file I/O
	// on every auth resolution.
	windowProfiles   map[string]string
	windowProfilesMu sync.RWMutex
	workDir          string // server project root; overridden by the boot path
	projects         *projects.Store
	projectGroups    *projects.GroupStore
	tabsStore        *tabs.Store
	monaco           *monaco.Store
	// terminalAuthConfigured and terminalLoopback are set by Server.New. A
	// terminal is only exposed without credentials when the server is bound to
	// a loopback address.
	terminalAuthConfigured bool
	terminalLoopback       bool
	// terminalProcs tracks the pid of every open terminal pty, read by the
	// terminal-processes emitter to report per-terminal CPU/mem.
	terminalProcs *terminalRegistry
	// terminalSessions owns the live pty shells so a reconnecting socket can
	// reattach to its shell after a page reload instead of respawning it.
	terminalSessions *terminalSessionTable
	// terminalProcsWake is a one-slot wake signal for the
	// terminal-processes emitter so a newly opened terminal pushes its
	// memory footprint immediately instead of waiting for the next ticker
	// interval. Capacity 1 + non-blocking send keeps it edge-triggered.
	terminalProcsWake chan struct{}

	// headlessSubs is the subscriber list for broadcasting live SSE events
	// in headless/serve mode (when no RC bridge is active). The SSE mirror
	// endpoint subscribes here and chat endpoints broadcast deltas through
	// this list, so the browser receives streaming tokens even without a TUI.
	headlessSubs map[chan SSEEvent]struct{}
	headlessMu   sync.Mutex

	// toolOutput batches incremental tool output into tool_output events.
	// Keyed by session+call so concurrent tool calls stay independent; entries
	// are released when their tool_result arrives.
	toolOutput *toolOutputCoalescer

	// turnMu guards turnLocks, the per-session turn serialization mutexes
	// (Part 03 persist-then-202 / async bootstrap). Turns on different
	// sessions run in parallel; turns on one session are strictly ordered.
	turnMu    sync.Mutex
	turnLocks map[string]*sync.Mutex
	// turnHeartbeatInterval is the turn_heartbeat period (10s default; tests
	// shorten it). It must be set before any turn starts for that handler.
	turnHeartbeatInterval time.Duration
	// sseKeepaliveInterval is the /api/events idle-comment period (20s
	// default; tests shorten it). Set before the first events connection.
	sseKeepaliveInterval time.Duration
	// mcpBootstrapTimeout bounds the MCP wait during agent bootstrap (30s
	// default; tests shorten it). Set before any bootstrap for that handler.
	mcpBootstrapTimeout time.Duration

	// titleGen guards one-shot session-title generation for headless turns
	// (web/desktop, no TUI). See title_gen.go.
	titleGen *titleGenState

	// syncMu guards syncClient/syncStop, which back the /api/sync/* routes
	// (web/desktop equivalent of the TUI's /login, /logout).
	syncMu     sync.Mutex
	syncClient *ocodesync.Client
	syncStop   func()

	// lspMgrs holds one LSP manager per project root. Sessions bound to the
	// same project share a manager (multiple tabs on one repo don't spawn
	// redundant gopls processes), while sessions on different registered
	// projects each get language servers rooted at their own repo — see
	// lspManagerFor.
	lspMu   sync.Mutex
	lspMgrs map[string]*lsp.Manager
}

// SetTerminalAccessPolicy configures the security boundary for the terminal
// endpoints. It is kept on Handler so direct handler tests exercise the same
// policy as the server routes.
func (h *Handler) SetTerminalAccessPolicy(authConfigured, loopback bool) {
	h.mu.Lock()
	h.terminalAuthConfigured = authConfigured
	h.terminalLoopback = loopback
	h.mu.Unlock()
}

// lspManagerFor returns the LSP manager rooted at the given project root,
// creating it on first use. Sessions on the same project share a manager so
// multiple tabs don't spawn redundant language-server processes; sessions on
// different registered projects get managers rooted at their own repo. An
// empty root falls back to the server's workdir (single-project servers, TUI
// RC bridge) and then to ".".
func (h *Handler) lspManagerFor(root string) *lsp.Manager {
	if root == "" {
		root = h.workDir
	}
	if root == "" {
		root = "."
	}
	h.lspMu.Lock()
	defer h.lspMu.Unlock()
	if h.lspMgrs == nil {
		h.lspMgrs = make(map[string]*lsp.Manager)
	}
	mgr, ok := h.lspMgrs[root]
	if !ok {
		shared := false
		if h.cfg != nil {
			shared = h.cfg.LSPShared
		}
		mgr = lsp.NewManagerWithShared(root, shared)
		h.lspMgrs[root] = mgr
		// Mirror the TUI's eager warmup (tui/model.go) so the web/desktop
		// sidebar shows language servers immediately instead of staying empty
		// until the agent happens to invoke an LSP tool. Skipped under `go
		// test` for the same reason the TUI skips it: WarmUp spawns external
		// servers that race temp-dir cleanup.
		if !testing.Testing() {
			go mgr.WarmUp(root)
		}
	}
	return mgr
}

// collectLSPStatuses reports the servers active across every per-project LSP
// manager, in the same shape the TUI's collectLSPStatuses builds from its own
// manager — used for the headless (no RC bridge) web/desktop status path.
func (h *Handler) collectLSPStatuses() []LSPStatus {
	h.lspMu.Lock()
	mgrs := make([]*lsp.Manager, 0, len(h.lspMgrs))
	for _, m := range h.lspMgrs {
		mgrs = append(mgrs, m)
	}
	h.lspMu.Unlock()

	out := []LSPStatus{}
	for _, mgr := range mgrs {
		active := mgr.ActiveServers()
		if len(active) == 0 {
			continue
		}

		var errByCmd, warnByCmd map[string]int
		if ds := mgr.Diagnostics(); ds != nil {
			errByCmd = make(map[string]int)
			warnByCmd = make(map[string]int)
			for _, d := range ds.All() {
				switch d.Severity {
				case lsp.SeverityError:
					errByCmd[d.ServerCmd]++
				case lsp.SeverityWarning:
					warnByCmd[d.ServerCmd]++
				}
			}
		}

		for _, s := range active {
			out = append(out, LSPStatus{
				Cmd:                 s.Cmd,
				LangID:              s.LangID,
				State:               "running",
				DiagnosticsErrors:   errByCmd[s.Cmd],
				DiagnosticsWarnings: warnByCmd[s.Cmd],
			})
		}
	}
	return out
}

type agentSession struct {
	agent    *agent.Agent
	messages []agent.Message
	model    string
	// profile is the effective profile name the agent was built with ("" = base).
	// Compared against the window's current active profile on each turn so a
	// profile switch takes effect on the next turn without an app restart.
	profile string
	// credVersion snapshots auth.ProfileCredentialVersion() at build time, so
	// an in-place credential edit on the same profile (not just a switch to a
	// different profile) also triggers a rebuild — see reconcileProfileAgent.
	credVersion int64
	mu          sync.Mutex
}

func NewHandler() *Handler {
	cfg, err := config.Load()
	if err != nil {
		// A malformed ocodeconfig (e.g. the reserved browser.extensions key)
		// must not be silently ignored; surface it on the desktop/web boot path
		// that browser-mode consumers ride on. Other load sites (TUI, auth)
		// may still swallow; this is the one serving path that must not.
		log.Printf("config: load ocode config: %v", err)
	}
	agent.ApplyAgentConfig(cfg)
	advisorEnabled := cfg == nil || cfg.Ocode.Advisor.Enabled
	// Direct Handler users (including tests) still need a useful project root.
	// The desktop/server boot path replaces this with its explicit project root
	// through SetWorkDir before serving requests.
	defaultWorkDir, _ := os.Getwd()

	projStore, projGroupStore, err := projects.NewStore()
	if err != nil {
		log.Printf("handler: init project store: %v (multi-project UI disabled)", err)
	}

	tabsStore, err := tabs.NewStore()
	if err != nil {
		log.Printf("handler: init tab store: %v (open-tab persistence disabled)", err)
	}

	monacoStore, err := monaco.NewStore()
	if err != nil {
		log.Printf("handler: init monaco store: %v (editor config disabled)", err)
	}

	h := &Handler{
		agents:            make(map[string]*agentSession),
		cfg:               cfg,
		advisorEnabled:    advisorEnabled,
		workDir:           defaultWorkDir,
		projects:          projStore,
		projectGroups:     projGroupStore,
		tabsStore:         tabsStore,
		monaco:            monacoStore,
		headlessSubs:      make(map[chan SSEEvent]struct{}),
		toolOutput:        newToolOutputCoalescer(),
		mcpCache:          newMCPCache(),
		titleGen:          newTitleGenState(),
		bus:               NewEventBus(),
		terminalProcs:     newTerminalRegistry(),
		terminalSessions:  newTerminalSessionTable(),
		terminalProcsWake: make(chan struct{}, 1),
		secretJobs:        secretjob.NewManager(),
	}

	// The session registry is the single authority for session → project root
	// + agent lifecycle. Its resolution search space is the handler's own
	// workdir first (backward compat with single-project servers) plus every
	// saved project root; the onEvict hook keeps the legacy h.agents mirror in
	// sync when idle agents are released.
	h.sessions = NewSessionManager(defaultSessionIdleTimeout, h.allowedProjectRoots, func(sessionID string) {
		h.mu.Lock()
		as := h.agents[sessionID]
		delete(h.agents, sessionID)
		h.mu.Unlock()
		// Drop the per-session turn lock too — otherwise one mutex leaks per
		// session id the process has ever served (the map is keyed by id and
		// nothing else removed entries).
		h.turnMu.Lock()
		delete(h.turnLocks, sessionID)
		h.turnMu.Unlock()
		// Shut down the released agent so plugin/LSP/background workers
		// don't linger past eviction (mirrors the register-dedup path).
		if as != nil && as.agent != nil {
			as.agent.Shutdown()
		}
	})

	h.mcpCache.warm(cfg)
	h.windowProfiles = make(map[string]string)
	if m, err := config.WindowStateForTest(); err == nil {
		for k, v := range m {
			h.windowProfiles[k] = v
		}
	}

	// Wire the agent's debug log sink so the Log tab (backed by debuglog.Log,
	// see handler_logs.go) receives entries in headless/desktop mode. The TUI
	// wires the same sink before starting its alt-screen; guard so a server
	// started from within an RC-bridged TUI session doesn't clobber it.
	if agent.DebugAppend == nil {
		agent.DebugAppend = func(kind, msg string) {
			debuglog.Log.Append(debuglog.Entry{Kind: debuglog.EntryKind(kind), Message: msg})
		}
	}
	return h
}

// allowedProjectRoots returns the set of project roots this server serves:
// its own workdir first (backward compat with single-project servers) plus
// every saved project root. It is the shared trust boundary for anything that
// binds work to a project directory — session resolution (SessionManager) and
// the interactive terminal's per-project cwd both validate against it.
func (h *Handler) allowedProjectRoots() []string {
	roots := make([]string, 0, 4)
	if h.workDir != "" {
		roots = append(roots, h.workDir)
	}
	if h.projects != nil {
		for _, p := range h.projects.List() {
			if p.Path != "" {
				roots = append(roots, p.Path)
			}
		}
	}
	return roots
}

// notifyTerminalProcsChanged wakes the terminal-processes emitter so a
// freshly registered terminal publishes its memory footprint without waiting
// for the next ticker tick. Non-blocking and edge-triggered.
func (h *Handler) notifyTerminalProcsChanged() {
	select {
	case h.terminalProcsWake <- struct{}{}:
	default:
	}
}

// SetWorkDir sets the working directory for git commands (used in tests) and
// reloads h.cfg from that directory's opencode.json. Without the reload, a
// server booted from a different cwd than the project it ends up serving
// (desktop shell, web server launched via a wrapper) would keep using
// provider overrides (API keys, base URLs) from the wrong project, or none at
// all — see internal/desktop/boot.go, which calls this before Serve starts.
func (h *Handler) SetWorkDir(dir string) {
	h.workDir = dir
	config.SetWorkDir(dir)

	cfg, err := config.Load()
	if err != nil {
		log.Printf("handler: reload config for workdir %q: %v", dir, err)
		return
	}
	agent.ApplyAgentConfig(cfg)

	mcpCache := newMCPCache()
	mcpCache.warm(cfg)

	h.mu.Lock()
	h.cfg = cfg
	h.advisorEnabled = cfg == nil || cfg.Ocode.Advisor.Enabled
	h.mcpCache = mcpCache
	h.mu.Unlock()
}

// RCBridge returns the bridge to a TUI session, or nil if the handler is
// running headless. Used by the TUI status endpoints to read the live
// snapshot the TUI pushes via the bridge.
func (h *Handler) RCBridge() *RCBridge {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.rc
}

// subscribeHeadless registers a new channel for live SSE events in headless
// mode and returns it. The caller must call unsubscribeHeadless when done.
func (h *Handler) subscribeHeadless() chan SSEEvent {
	ch := make(chan SSEEvent, 256)
	h.headlessMu.Lock()
	if h.headlessSubs == nil {
		h.headlessSubs = make(map[chan SSEEvent]struct{})
	}
	h.headlessSubs[ch] = struct{}{}
	h.headlessMu.Unlock()
	return ch
}

// unsubscribeHeadless removes a previously registered subscriber channel.
func (h *Handler) unsubscribeHeadless(ch chan SSEEvent) {
	h.headlessMu.Lock()
	delete(h.headlessSubs, ch)
	h.headlessMu.Unlock()
}

// broadcastEvent delivers a live event to all subscribers. In headless mode
// it goes to headlessSubs; when an RC bridge is active it goes through the
// bridge instead (which the TUI uses to push events). Sends are non-blocking:
// a slow consumer drops the event rather than stalling the caller.
//
// Every headless event is also published on the unified event bus (Part 02) —
// the single multiplexed stream /api/events carries the same payloads the
// legacy mirror did, tagged with the session's owning project. The legacy
// headlessSubs fan-out stays until Part 06 deletes the old endpoints.
func (h *Handler) broadcastEvent(ev SSEEvent) {
	// When an RC bridge is active, the TUI pushes events through the bridge.
	// Our local streaming callbacks should not also push directly — the TUI
	// already handles broadcasting. Only broadcast locally in headless mode.
	h.headlessMu.Lock()
	defer h.headlessMu.Unlock()
	for ch := range h.headlessSubs {
		select {
		case ch <- ev:
		default:
		}
	}
	// Publish to the unified bus too. Session-scoped events carry the
	// session's project root from the registry (cheap map lookup, no disk —
	// every turn runs on a registered session). Project/session stay empty for
	// process-global events like status. The session's reconcile watermark
	// (GET /api/sessions/:id/state last_seq) follows the bus sequence.
	project := ""
	if ev.SessionID != "" {
		if e := h.sessions.Lookup(ev.SessionID); e != nil {
			project = e.ProjectRoot
		}
	}
	h.bus.Publish(ev.Event, project, ev.SessionID, ev.Data)
	if ev.SessionID != "" {
		seq := h.bus.LastSeq()
		h.sessions.SetLastSeq(ev.SessionID, seq)
		// Buffer streaming frames for mid-turn reload replay (session_manager.go
		// appendLiveFrame) — a no-op for any event type outside liveFrameEvents
		// or a session with no active turn.
		h.sessions.appendLiveFrame(ev.SessionID, ev.Event, ev.Data, seq)
	}
}

// wireHeadlessAgentCallbacks installs the OnDelta/OnMessage hooks that stream
// live agent activity to the SSE mirror subscribers in headless mode. Shared by
// the chat, send-message, and question-answer handlers so all three broadcast
// identically. When a `question` tool prompt pauses the turn, the OnMessage tool
// branch also emits a `question` frame so a connected browser can render the
// prompt. The per-call `user_message` broadcast stays at each call site — a
// question-answer re-Step has no user message to echo. Likewise, when a tool
// call pauses on a PERMISSION_ASK sentinel (no OnPermissionAsk callback is wired
// in headless mode), the tool branch emits a `permission` frame so a connected
// browser can render the approve/deny dialog.
func (h *Handler) wireHeadlessAgentCallbacks(sessionID string, ag *agent.Agent) {
	// Map OnDelta kinds to SSE event names matching the TUI RC bridge pattern:
	// "reasoning" → "thinking", "text" → "text".
	ag.OnDelta = func(kind, text string) {
		event := kind
		if kind == "reasoning" {
			event = "thinking"
		}
		h.broadcastEvent(SSEEvent{
			SessionID: sessionID,
			Event:     event,
			Data:      TextDelta{Delta: text},
		})
	}
	// Stream incremental tool output (e.g. live bash stdout/stderr) so a long
	// command shows progress instead of a bubble stuck on "running…".
	//
	// Unlike the TUI — which backpressures its single in-process consumer
	// rather than lose transcript text — this must never block: the producer is
	// the tool's own output pump, and a slow or vanished browser must not be
	// able to stall a running command. Chunks are coalesced, capped per call,
	// and dropped on bus backpressure; the authoritative content still arrives
	// via tool_result.
	ag.OnToolOutput = func(toolCallID, chunk string) {
		payload, flush := h.toolOutput.add(sessionID+"\x00"+toolCallID, chunk, time.Now())
		if !flush {
			return
		}
		h.broadcastEvent(SSEEvent{
			SessionID: sessionID,
			Event:     "tool_output",
			Data:      ToolOutputEvent{CallID: toolCallID, Chunk: payload},
		})
	}
	ag.OnPermissionCheck = func(toolName, modelLabel string, active bool) {
		h.broadcastEvent(SSEEvent{
			SessionID: sessionID,
			Event:     "permission_check",
			Data:      PermissionCheckEvent{Tool: toolName, Model: modelLabel, Active: active},
		})
	}
	ag.OnAdvisorCheckpoint = func(kind string, running bool) {
		h.broadcastEvent(SSEEvent{
			SessionID: sessionID,
			Event:     "advisor_checkpoint",
			Data:      AdvisorCheckpointEvent{Kind: kind, Active: running},
		})
	}
	ag.OnMessage = func(m agent.Message) {
		if m.Role == "user" {
			// A message injected mid-turn from the session's live queue (see
			// tryEnqueueInjection) — the normal user_message broadcast in
			// runTurn only covers the turn's own opening message, so an
			// injected one needs its own frame to show up in a connected
			// browser as soon as the agent picks it up.
			h.broadcastEvent(SSEEvent{
				SessionID: sessionID,
				Event:     "user_message",
				Data:      map[string]string{"content": m.Content},
			})
			return
		}
		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			for _, tc := range m.ToolCalls {
				h.broadcastEvent(SSEEvent{
					SessionID: sessionID,
					Event:     "tool_start",
					Data: ToolStartEvent{
						Tool:    tc.Function.Name,
						CallID:  tc.ID,
						Command: tc.Function.Arguments,
					},
				})
			}
		}
		if m.Role == "tool" {
			// Flush whatever the call buffered below the thresholds and release
			// its state, so a short command's tail is not stranded and no
			// per-call buffer outlives the call.
			if tail, ok := h.toolOutput.finish(sessionID + "\x00" + m.ToolID); ok {
				h.broadcastEvent(SSEEvent{
					SessionID: sessionID,
					Event:     "tool_output",
					Data:      ToolOutputEvent{CallID: m.ToolID, Chunk: tail},
				})
			}
			h.broadcastEvent(SSEEvent{
				SessionID: sessionID,
				Event:     "tool_result",
				Data:      ToolResultEvent{Tool: "tool", CallID: m.ToolID, Output: m.Content},
			})
			if prompts, ok := parseQuestionAsk(m.Content); ok {
				h.broadcastEvent(SSEEvent{
					SessionID: sessionID,
					Event:     "question",
					Data:      QuestionEvent{RequestID: m.ToolID, Questions: prompts},
				})
			}
			if req, ok := parsePermissionAsk(m.Content); ok {
				h.broadcastEvent(SSEEvent{
					SessionID: sessionID,
					Event:     "permission",
					Data:      newPermissionEvent(m.ToolID, req),
				})
			}
		}
	}
}

func (h *Handler) HandleChat(w http.ResponseWriter, r *http.Request) {
	var req ChatRequest
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Content == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}

	windowID := req.WindowID
	if windowID == "" {
		windowID = r.Header.Get("X-Window-Id")
	}
	if windowID == "" {
		windowID = r.URL.Query().Get("windowId")
	}
	if windowID == "" {
		windowID = r.URL.Query().Get("window_id")
	}
	// Normalize windowID: trim spaces, allow empty = no binding
	if windowID != "" {
		windowID = strings.TrimSpace(windowID)
	}

	model := req.Model
	if model == "" {
		model = h.effectiveSessionModel(req.SessionID)
	}

	sid := req.SessionID
	projectRoot := req.ProjectPath
	if projectRoot == "" {
		projectRoot = h.workDir
	}
	// A session must be bound to a real project root. An empty root would make
	// the agent fall back to the server process's cwd (for the desktop app,
	// typically $HOME), and markdown discovery would then sweep the whole home
	// directory — blocking the first turn for minutes. Fail fast instead.
	if projectRoot == "" {
		writeError(w, http.StatusBadRequest, "project_path is required (no server workdir to fall back to)")
		return
	}

	createdSession := false
	var entry *sessionEntry
	if sid != "" {
		// Bind the session to its project root in the registry (explicit
		// project_path wins; a resolved existing session keeps its root) so
		// the agent is built against the right workdir and cross-project 404s
		// become impossible for sessions that exist.
		if req.ProjectPath != "" {
			h.sessions.RegisterWithWindow(sid, req.ProjectPath, windowID)
		}
		var err error
		entry, err = h.sessions.Resolve(sid)
		if err != nil {
			// Session exists in no registered project. Today this call site is
			// lenient (a missing session silently starts with empty history);
			// preserve that by binding to the default root and continuing.
			entry = h.sessions.RegisterWithWindow(sid, projectRoot, windowID)
		} else if windowID != "" {
			h.sessions.SetWindowID(sid, windowID)
			entry = h.sessions.Lookup(sid)
		}
	} else {
		// A model is only required to create a new session.
		if model == "" {
			writeError(w, http.StatusBadRequest, "no model configured")
			return
		}
		sid = session.NewSessionID()
		createdSession = true
		entry = h.sessions.RegisterWithWindow(sid, projectRoot, windowID)
		// Persist an explicitly-requested model as this session's override at
		// creation time (before any turn), so a "new chat with model X" pick
		// survives resume/restart instead of silently falling back to the
		// global config default. Later saves pass nil metadata and the sqlite
		// append preserves this row. Sessions created without an explicit
		// model get no override and keep following the global default.
		if req.Model != "" {
			if err := h.saveSession(sid, "", nil, map[string]any{"model": req.Model}); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
		}
	}

	opts := turnOptions{sessionStarted: createdSession, requestID: req.RequestID}

	// Async (web/desktop): persist-then-202 — the user message is written to
	// the session's on-disk transcript before the 202 returns, and the agent
	// bootstrap (if needed) runs on a per-session goroutine with observable
	// stage events. A bootstrap failure after 202 never loses the message.
	if req.Async {
		if createdSession {
			// The session_started marker survives a bootstrap failure: the
			// first turn that actually runs emits the frame correlated back
			// to the tab that created the session.
			h.sessions.SetSessionStart(sid, req.RequestID)
		}
		job, err := h.dispatchTurn(sid, model, req.Content, opts)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		select {
		case <-job.persistAck:
			if job.err != nil {
				writeError(w, http.StatusInternalServerError, job.err.Error())
				return
			}
		case <-r.Context().Done():
			return
		}
		writeJSON(w, http.StatusAccepted, ChatResponse{
			SessionID: sid,
			Model:     model,
		})
		return
	}

	// Sync (scheduler, Telegram, external API clients): build inline and wait
	// for the result.
	as := h.lookupAgentSession(sid)
	if as == nil {
		var messages []agent.Message
		if req.SessionID != "" && entry != nil {
			if s, err := session.LoadForDir(entry.ProjectRoot, req.SessionID); err == nil {
				messages = s.Messages
			}
		}
		// Built with no handler lock held — see agent_session.go.
		var err error
		var stage string
		as, stage, err = h.ensureAgentSession(sid, model, messages, entry.ProjectRoot)
		if err != nil {
			h.publishTurnError(sid, err, stage)
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	} else {
		// Existing session on the sync path: reconcile a model/profile change
		// before stepping. HandleSendMessage does this for async web turns;
		// this covers the non-async existing-session path which previously
		// skipped reconcile entirely and kept the stale model.
		if reb, err := h.reconcileProfileAgent(sid, as, model); err != nil {
			h.publishTurnError(sid, err, "profile")
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("agent error: %v", err))
			return
		} else {
			as = reb
		}
	}

	content, err := h.runTurn(sid, as, req.Content, opts)
	if errors.Is(err, ErrPermissionPending) {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("agent error: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, ChatResponse{
		Content:   content,
		SessionID: sid,
		Model:     as.model,
	})
}

func (h *Handler) HandleListSessions(w http.ResponseWriter, r *http.Request) {
	limit := 0 // 0 means return all
	offset := 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}

	refs, total, err := session.ListRefsPaginated(limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to list sessions: %v", err))
		return
	}

	result := make([]SessionInfo, 0, len(refs))
	for _, ref := range refs {
		result = append(result, SessionInfo{
			ID:        ref.ID,
			Title:     ref.Title,
			CreatedAt: ref.CreatedAt.Format(time.RFC3339),
			UpdatedAt: ref.UpdatedAt.Format(time.RFC3339),
		})
	}

	writeJSON(w, http.StatusOK, SessionListResponse{Sessions: result, Total: total})
}

func (h *Handler) HandleGetSession(w http.ResponseWriter, r *http.Request, id string) {
	h.mu.Lock()
	rc := h.rc
	h.mu.Unlock()

	// Parse optional pagination params: limit (max messages from end) and
	// offset (skip this many from the end, for loading older messages).
	limit := 0 // 0 means return all
	offset := 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}

	paginate := func(all []agent.Message) []agent.Message {
		total := len(all)
		if limit == 0 || limit >= total-offset {
			// Return everything up to the offset point.
			end := total - offset
			if end < 0 {
				end = 0
			}
			return all[:end]
		}
		start := total - offset - limit
		if start < 0 {
			start = 0
		}
		return all[start : total-offset]
	}

	// If this is the RC session, return in-memory messages from the bridge.
	if rc != nil && rc.SessionID == id {
		all := rc.GetMessages()
		msgs := paginate(all)
		writeJSON(w, http.StatusOK, SessionDetail{
			SessionInfo: SessionInfo{
				ID:        rc.SessionID,
				Title:     "",
				CreatedAt: time.Now().Format(time.RFC3339),
				UpdatedAt: time.Now().Format(time.RFC3339),
			},
			Messages: msgs,
			Total:    len(all),
		})
		return
	}

	// Resolve the session's owning project through the registry so sessions
	// from any registered project load, not just the server's own workdir.
	entry, err := h.sessions.Resolve(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	msgs, total, err := session.PaginatedLoad(entry.ProjectRoot, id, limit, offset)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	s, _ := session.LoadForDir(entry.ProjectRoot, id)
	// s may be nil if file vanished between PaginatedLoad and LoadForDir; fall back to msgs metadata.
	title := ""
	created := time.Now()
	updated := time.Now()
	if s != nil {
		title = s.Title
		created = s.CreatedAt
		updated = s.UpdatedAt
	}
	writeJSON(w, http.StatusOK, SessionDetail{
		SessionInfo: SessionInfo{
			ID:        id,
			Title:     title,
			CreatedAt: created.Format(time.RFC3339),
			UpdatedAt: updated.Format(time.RFC3339),
		},
		Messages: msgs,
		Total:    total,
	})
}

func (h *Handler) HandleSendMessage(w http.ResponseWriter, r *http.Request, id string) {
	var req struct {
		Content  string `json:"content"`
		WindowID string `json:"windowId,omitempty"`
		// Async: see ChatRequest.Async.
		Async bool `json:"async,omitempty"`
	}
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Content == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}

	// Part 06: uniform send path. Resolve the session first, then route by
	// its registry entry — no more "any message goes to the TUI when a
	// bridge is attached" global forwarding. A bridged TUI session (id ==
	// the bridge's session) is forwarded through the bridge; every other
	// session runs on the server's own agent.
	entry, err := h.sessions.Resolve(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	// Per-window profile binding: capture windowId from body/header/query for
	// later turns (buildAgentSession uses entry.WindowID).
	windowID := req.WindowID
	if windowID == "" {
		windowID = r.Header.Get("X-Window-Id")
	}
	if windowID == "" {
		windowID = r.URL.Query().Get("windowId")
	}
	if windowID != "" {
		windowID = strings.TrimSpace(windowID)
		if windowID != "" {
			h.sessions.SetWindowID(id, windowID)
			entry = h.sessions.Lookup(id)
		}
	}

	if rc := h.RCBridge(); rc != nil && id == rc.SessionID {
		if req.Async {
			// Acknowledge immediately; the TUI streams the turn to the mirror
			// via its own broadcast channel (the bridge re-broadcasts to the
			// unified bus, Part 06).
			select {
			case rc.RcCh <- RCRequest{Content: req.Content, ResultCh: make(chan RCResult, 1)}:
			default:
				writeError(w, http.StatusServiceUnavailable, "TUI is busy, try again")
				return
			}
			writeJSON(w, http.StatusAccepted, ChatResponse{SessionID: id, Model: rc.Model})
			return
		}

		resultCh := make(chan RCResult, 1)
		select {
		case rc.RcCh <- RCRequest{Content: req.Content, ResultCh: resultCh}:
		case <-time.After(5 * time.Second):
			writeError(w, http.StatusServiceUnavailable, "TUI is busy, try again")
			return
		}

		select {
		case result := <-resultCh:
			if result.Error != nil {
				writeError(w, http.StatusInternalServerError, fmt.Sprintf("agent error: %v", result.Error))
				return
			}
			var content strings.Builder
			for _, m := range result.Messages {
				if m.Role == "assistant" && m.Content != "" {
					content.WriteString(m.Content)
				}
			}
			writeJSON(w, http.StatusOK, ChatResponse{
				Content:   content.String(),
				SessionID: id,
				Model:     rc.Model,
			})
		case <-time.After(5 * time.Minute):
			writeError(w, http.StatusGatewayTimeout, "agent response timed out")
		}
		return
	}

	as := h.lookupAgentSession(id)
	if as == nil {
		s, err := session.LoadForDir(entry.ProjectRoot, id)
		if err != nil {
			writeError(w, http.StatusNotFound, "session not found")
			return
		}

		model := h.effectiveSessionModel(id)
		if model == "" {
			writeError(w, http.StatusBadRequest, "no model configured")
			return
		}

		// Built with no handler lock held — see agent_session.go.
		var stage string
		as, stage, err = h.ensureAgentSession(id, model, s.Messages, entry.ProjectRoot)
		if err != nil {
			h.publishTurnError(id, err, stage)
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	// A profile switch takes effect on the next turn: if the window's active
	// A profile or model switch takes effect on the next turn: if the
	// window's active profile or the configured model changed since this
	// agent was built, rebuild it before the next turn.
	desiredModel := h.effectiveSessionModel(id)
	if reb, err := h.reconcileProfileAgent(id, as, desiredModel); err != nil {
		h.publishTurnError(id, err, "profile")
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("agent error: %v", err))
		return
	} else {
		as = reb
	}

	// A turn already running on this session slots the message into the live
	// Step loop at the next tool-call boundary instead of waiting for the
	// whole turn to finish and queuing a brand new one behind it.
	if h.tryEnqueueInjection(id, req.Content) {
		writeJSON(w, http.StatusAccepted, ChatResponse{SessionID: id, Model: as.model})
		return
	}

	// Async: persist-then-202 — the message is durable on disk before the 202
	// returns; bootstrap (if needed) and the turn run on a per-session
	// goroutine with events streamed over the unified bus.
	if req.Async {
		job, err := h.dispatchTurn(id, desiredModel, req.Content, turnOptions{})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		select {
		case <-job.persistAck:
			if job.err != nil {
				writeError(w, http.StatusInternalServerError, job.err.Error())
				return
			}
		case <-r.Context().Done():
			return
		}
		writeJSON(w, http.StatusAccepted, ChatResponse{SessionID: id, Model: as.model})
		return
	}

	content, err := h.runTurn(id, as, req.Content, turnOptions{})
	if errors.Is(err, ErrPermissionPending) {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("agent error: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, ChatResponse{
		Content:   content,
		SessionID: id,
		Model:     as.model,
	})
}

func (h *Handler) HandleListModels(w http.ResponseWriter, r *http.Request) {
	// Mark the currently configured model as active.
	currentModel := ""
	if h.cfg != nil {
		currentModel = h.cfg.Model
	}

	// Query params for direct model list retrieval (used by opencode-compatible clients
	// and for provider-specific live refresh):
	//   ?provider=groq   — filter to a single provider (provider/model ids)
	//   ?refresh=true    — force live fetch from provider APIs (may block on network)
	providerFilter := strings.TrimSpace(r.URL.Query().Get("provider"))
	refreshParam := strings.TrimSpace(r.URL.Query().Get("refresh"))
	refresh := refreshParam == "1" || strings.EqualFold(refreshParam, "true")

	// Mirror the TUI model picker ordering (openModelPicker in
	// internal/tui/picker.go): Recently Used first, then ★ Favorites, then the
	// remaining registry models grouped alphabetically by provider/model.
	// Favorites / recents that are not in the registry are still listed, just
	// like the TUI shows them regardless of registry membership.
	favorites := config.LoadFavorites()
	recents := config.LoadRecentModels()
	var registry []string
	var favSet map[string]bool
	// Provider-filtered retrieval: return only that provider's models (with live refresh
	// when requested). This is the direct API for clients that need a provider's live
	// model list (e.g. GET /api/models?provider=groq&refresh=true).
	if providerFilter != "" {
		if refresh {
			registry = agent.ProviderModels(providerFilter)
		} else {
			registry = agent.ProviderModelsCached(providerFilter)
			if len(registry) == 0 {
				registry = agent.ProviderModels(providerFilter)
			}
		}
		sort.Strings(registry)
		favorites = nil
		favSet = make(map[string]bool)
		// Return filtered list directly without global favorites ordering.
		modelsFiltered := make([]ModelInfo, 0, len(registry))
		seen := make(map[string]bool, len(registry))
		for _, id := range registry {
			if seen[id] {
				continue
			}
			seen[id] = true
			provider, modelName, ok := splitModelID(id)
			if !ok {
				provider = providerFilter
				modelName = id
			}
			modelsFiltered = append(modelsFiltered, ModelInfo{
				Name:        id,
				Model:       modelName,
				Provider:    provider,
				Active:      id == currentModel,
				DisplayName: agent.ModelDisplayName(id),
			})
		}
		if len(modelsFiltered) == 0 && h.cfg != nil {
			for name := range h.cfg.Provider {
				if name == providerFilter {
					modelsFiltered = append(modelsFiltered, ModelInfo{
						Name:     name,
						Model:    name,
						Provider: name,
					})
				}
			}
		}
		writeJSON(w, http.StatusOK, modelsFiltered)
		return
	}
	if refresh {
		registry = agent.AllProviderModels()
	} else {
		registry = agent.AllProviderModelsCached()
	}

	shown := make(map[string]bool)
	var ordered []string
	addModel := func(id string) {
		if id == "" || shown[id] {
			return
		}
		shown[id] = true
		ordered = append(ordered, id)
	}

	// 1. Recently used (in saved order), 2. Favorites (in saved order).
	recentSet := make(map[string]bool)
	for _, r := range recents {
		recentSet[r] = true
		addModel(r)
	}
	favSet = make(map[string]bool)
	for _, f := range favorites {
		favSet[f] = true
		addModel(f)
	}

	// 3. Remaining registry models, alphabetically by provider then model
	// (equivalent to sorting the "provider/model" ids).
	rest := make([]string, 0, len(registry))
	for _, id := range registry {
		if !shown[id] {
			rest = append(rest, id)
		}
	}
	sort.Strings(rest)
	for _, id := range rest {
		addModel(id)
	}

	models := make([]ModelInfo, 0, len(ordered))
	for _, id := range ordered {
		provider, modelName, ok := splitModelID(id)
		if !ok {
			provider = "other"
			modelName = id
		}
		models = append(models, ModelInfo{
			Name:        id,
			Model:       modelName,
			Provider:    provider,
			Active:      id == currentModel,
			DisplayName: agent.ModelDisplayName(id),
			// Raw membership flags: a model that is both favorite and recent is
			// placed in the Recently Used section by the consumer (the TUI's
			// dedupe), but Favorite stays true so the UI's favorite toggle
			// reflects IsFavorite, mirroring ctrl+f in the TUI.
			Recent:   recentSet[id],
			Favorite: favSet[id],
		})
	}

	// If registry is empty, fall back to configured model + provider keys.
	if len(models) == 0 && h.cfg != nil {
		if currentModel != "" {
			models = append(models, ModelInfo{
				Name:     currentModel,
				Model:    currentModel,
				Provider: "configured",
				Active:   true,
			})
		}
		for name := range h.cfg.Provider {
			models = append(models, ModelInfo{
				Name:     name,
				Model:    name,
				Provider: name,
			})
		}
	}

	writeJSON(w, http.StatusOK, models)
}

// containsStr reports whether s is present in xs.
func containsStr(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// splitModelID splits "provider/model" into provider and model parts.
func splitModelID(id string) (provider, model string, ok bool) {
	for i := 0; i < len(id); i++ {
		if id[i] == '/' {
			return id[:i], id[i+1:], true
		}
	}
	return "", "", false
}

func (h *Handler) HandleCompactSession(w http.ResponseWriter, r *http.Request, id string) {
	as, err := h.getOrCreateAgentSession(id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	// Compaction is an LLM call. It runs under the per-session lock only —
	// holding h.mu across it would freeze every other session's turn for its
	// whole duration.
	as.mu.Lock()
	defer as.mu.Unlock()

	result, enabled := as.agent.Compact(as.messages)
	if !enabled {
		writeError(w, http.StatusUnprocessableEntity, "compaction disabled in config")
		return
	}
	if !result.OK {
		if result.Err != nil {
			writeError(w, http.StatusInternalServerError, result.Err.Error())
			return
		}
		writeError(w, http.StatusUnprocessableEntity, "nothing to compact")
		return
	}

	before := as.messages[:result.ReplaceFrom]
	after := as.messages[result.ReplaceTo:]
	compacted := make([]agent.Message, 0, len(before)+1+len(after))
	compacted = append(compacted, before...)
	compacted = append(compacted, result.Summary)
	compacted = append(compacted, after...)
	as.messages = compacted

	_ = h.saveSession(id, "", as.messages, nil)

	// Broadcast the compacted snapshot so the SSE mirror (and every connected
	// browser) replaces its stale message list — otherwise the web transcript
	// keeps showing the pre-compaction messages and its context size never
	// drops. Matches the chat handler's post-turn broadcast.
	if h.RCBridge() == nil {
		h.broadcastEvent(SSEEvent{
			SessionID: id,
			Event:     "messages",
			Data:      as.messages,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"original_len":  result.OriginalLen,
		"compacted_len": len(as.messages),
	})
}

func (h *Handler) HandleRecapSession(w http.ResponseWriter, r *http.Request, id string) {
	as, err := h.getOrCreateAgentSession(id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	// Snapshot the transcript under the session lock, then release it: Recap is
	// an LLM call and must not block the session's next turn (nor, via h.mu,
	// every other session).
	as.mu.Lock()
	if len(as.messages) == 0 {
		as.mu.Unlock()
		writeError(w, http.StatusUnprocessableEntity, "no messages to recap")
		return
	}
	msgs := make([]agent.Message, len(as.messages))
	copy(msgs, as.messages)
	ag := as.agent
	as.mu.Unlock()

	text := ag.Recap(msgs, "")

	writeJSON(w, http.StatusOK, map[string]string{"recap": text})
}

func (h *Handler) HandleExportSession(w http.ResponseWriter, r *http.Request, id string) {
	s, err := h.loadSession(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	var b strings.Builder
	for _, msg := range s.Messages {
		if msg.Role == "user" || msg.Role == "assistant" {
			role := strings.ToUpper(msg.Role[:1]) + msg.Role[1:]
			b.WriteString(fmt.Sprintf("## %s\n\n%s\n\n", role, msg.Content))
		}
	}

	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="ocode_export_%s.md"`, id))
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, b.String())
}

func (h *Handler) HandleExportClaudeSession(w http.ResponseWriter, r *http.Request, id string) {
	s, err := h.loadSession(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	if len(s.Messages) == 0 {
		writeError(w, http.StatusUnprocessableEntity, "no messages to export")
		return
	}

	path, err := session.AppendClaudeSession(id, s.Messages)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"path": path})
}

func (h *Handler) HandleShareSession(w http.ResponseWriter, r *http.Request, id string) {
	s, err := h.loadSession(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	var b strings.Builder
	title := s.Title
	if title == "" {
		title = "ocode session " + id
	}
	fmt.Fprintf(&b, "# %s\n\n", title)
	fmt.Fprintf(&b, "Session ID: `%s`  \nCreated: %s\n\n---\n\n", id, s.CreatedAt.Format(time.RFC3339))

	for _, msg := range s.Messages {
		if msg.Role != "user" && msg.Role != "assistant" {
			continue
		}
		if msg.Content == "" {
			continue
		}
		role := strings.ToUpper(msg.Role[:1]) + msg.Role[1:]
		fmt.Fprintf(&b, "**%s:** %s\n\n", role, msg.Content)
	}

	writeJSON(w, http.StatusOK, map[string]string{"markdown": b.String()})
}

// HandleBtw appends a "By the way" user message to a session.
func (h *Handler) HandleBtw(w http.ResponseWriter, r *http.Request, id string) {
	var req struct {
		Content string `json:"content"`
	}
	if err := readBodyJSON(r, &req); err != nil || req.Content == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}

	s, err := h.loadSession(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	msg := agent.Message{
		Role:    "user",
		Content: "By the way: " + req.Content,
	}
	s.Messages = append(s.Messages, msg)

	if err := h.saveSession(id, s.Title, s.Messages, nil); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "noted"})
}

func (h *Handler) HandleSetSessionTitle(w http.ResponseWriter, r *http.Request, id string) {
	var req struct {
		Title string `json:"title"`
	}
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Title == "" {
		writeError(w, http.StatusBadRequest, "title cannot be empty")
		return
	}

	s, err := h.loadSession(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	if err := h.saveSession(id, req.Title, s.Messages, nil); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"title": req.Title})
}

func (h *Handler) HandleSessionContext(w http.ResponseWriter, r *http.Request, id string) {
	entry, err := h.sessions.Resolve(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	s, err := session.LoadForDir(entry.ProjectRoot, id)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	var totalChars int
	for _, msg := range s.Messages {
		totalChars += len(msg.Content) + len(msg.ReasoningContent)
		for _, tc := range msg.ToolCalls {
			totalChars += len(tc.Function.Arguments)
		}
	}

	// Prefer the live TUI values when bridged — the model name + max context
	// come from the running model, not from a snapshot saved to disk.
	model := ""
	maxTokens := 0
	if h.rc != nil {
		if live := h.rc.TUIStatus(); live.ContextModel != "" {
			model = live.ContextModel
			maxTokens = live.ContextMaxTokens
		}
	}
	if model == "" {
		model = h.effectiveSessionModel(id)
	}
	if maxTokens == 0 {
		maxTokens = int(agent.ModelWindow(model))
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"session_id":       id,
		"message_count":    len(s.Messages),
		"estimated_tokens": totalChars / 4,
		"max_tokens":       maxTokens,
		"model":            model,
	})
}

// HandleShellCommand executes a shell command and returns the output.
// This provides cross-platform shell execution for the web UI (! prefix commands).
//
// The actual spawn-and-capture work is delegated to internal/shell so the
// TUI agent loop and the server share one implementation (timeout,
// Setpgid, exit-code extraction, error-string policy). The handler is
// responsible for the HTTP-level concerns: input validation, response
// shape, and the workDir defaulting chain (request → server workDir → ".").
func (h *Handler) HandleShellCommand(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Command string `json:"command"`
		WorkDir string `json:"workDir,omitempty"`
	}
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Command == "" {
		writeError(w, http.StatusBadRequest, "command is required")
		return
	}

	// Use configured work directory if not specified
	workDir := req.WorkDir
	if workDir == "" {
		workDir = h.workDir
	}
	if workDir == "" {
		workDir = "."
	}

	res := shellpkg.Run(req.Command, workDir)

	errMsg := ""
	if res.Err != nil {
		errMsg = res.Err.Error()
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"output":   res.Output,
		"exitCode": res.ExitCode,
		"error":    errMsg,
	})
}
