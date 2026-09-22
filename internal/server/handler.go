package server

import (
	"context"
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
	"github.com/u007/ocode/internal/auth"
	"github.com/u007/ocode/internal/computer"
	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/contextbudget"
	"github.com/u007/ocode/internal/debuglog"
	"github.com/u007/ocode/internal/lsp"
	"github.com/u007/ocode/internal/monaco"
	"github.com/u007/ocode/internal/projects"
	"github.com/u007/ocode/internal/scheduler"
	"github.com/u007/ocode/internal/secretjob"
	"github.com/u007/ocode/internal/session"
	shellpkg "github.com/u007/ocode/internal/shell"
	"github.com/u007/ocode/internal/skill"
	ocodesync "github.com/u007/ocode/internal/sync"
	"github.com/u007/ocode/internal/sysperm"
	"github.com/u007/ocode/internal/tabs"
	"github.com/u007/ocode/internal/tool"
)

type Handler struct {
	mu            sync.Mutex
	computerUseMu sync.Mutex
	computerSup   *tool.ProcessSupervisor
	// requestComputerPermissions triggers the OS permission prompts for
	// computer use. Overridable in tests so the suite never fires a real
	// consent dialog; nil falls back to computer.RequestPermissions.
	requestComputerPermissions func(context.Context) computer.PermissionReport
	// sysPermMu serializes read-modify-write of the system-permissions
	// sub-tree (persisted + the in-memory h.cfg copy).
	sysPermMu sync.Mutex
	// requestSystemPermission triggers one OS permission request for the
	// System Permissions settings section. Overridable in tests so the suite
	// never fires a real consent dialog; nil falls back to sysperm.Request.
	requestSystemPermission func(context.Context, sysperm.Entry) sysperm.RequestResult
	// systemPermissionCatalog builds the catalog for the System Permissions
	// section. Overridable in tests so no OS probe runs; nil falls back to
	// sysperm.Catalog over the persisted config + allowed project roots.
	systemPermissionCatalog func(context.Context) []sysperm.Entry
	// procSup is the server's process supervisor (the same one computerSup
	// aliases), used for server-owned long-lived children that are not
	// computer-use — today the per-project `ssh -N -L` port forwards
	// (handler_portmaps.go). nil for a bare NewHandler().
	procSup *tool.ProcessSupervisor
	// portMaps owns one remote.ForwardManager per remote project for the
	// project-scoped /api/portmaps panel. nil for a bare NewHandler(); wired
	// by server.New from procSup.
	portMaps *portMapRegistry
	// remoteHosts owns one remote.RemoteWorkspace per remote host, created
	// lazily on first use. nil for a bare NewHandler(); wired by server.New.
	remoteHosts *remoteHostRegistry
	agents      map[string]*agentSession
	cfg         *config.Config
	rc          *RCBridge          // set when proxying to a TUI session
	scheduler   *scheduler.Service // when set, the `cron` tool is wired into agent sessions
	// sessions is the single authority for session ID → project root + agent
	// lifecycle. Every session-scoped handler resolves through it, so sessions
	// from any registered project load and run (no more cross-project 404s).
	sessions *SessionManager
	// secretJobs runs cancellable, progress-reporting directory-wide
	// encrypt/decrypt jobs (see handler_secret.go), streaming progress as
	// secret_progress/secret_done/secret_error/secret_cancelled events on bus.
	secretJobs *secretjob.Manager
	// cliToolJobs tracks background CLI-utility installs started by the
	// web/desktop `/tools` command (see handler_clitools.go). Install shells
	// out to the platform package manager and can block for minutes, so it is
	// job-id + poll rather than a long-held request.
	cliToolJobs *cliToolJobManager
	// mcpAuthJobs tracks background MCP OAuth flows started by the web/desktop
	// `/mcp-auth` command (see handler_mcp_auth.go). The flow blocks up to 2
	// minutes waiting for the browser callback, so it is job-id + poll.
	mcpAuthJobs *mcpAuthJobManager

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
	// advisorEnabled is the process-wide runtime gate for the advisor tool,
	// used by sessions with no per-session override and by the pre-session
	// sidebar/Settings fallback. Seeded from config, flipped from the web
	// Settings form, never persisted back to config. A chat that toggles the
	// advisor persists its own override in transcript metadata
	// (advisorEnabledMetadataKey) and never touches this field, so one chat's
	// toggle cannot leak into another. See advisor_session.go.
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
	// shellSessions owns the persistent interactive shell a `!` command runs
	// in, keyed by frontend tab id. Lazy-created on first use; closed on tab
	// close, moved on /reset-id, reaped when idle.
	shellSessions *shellSessionRegistry
	// remoteProjectMu serializes remote terminal admission with edits to the
	// saved remote-project identity. This closes the check-then-reserve race
	// where an old terminal could be published after an edit commits.
	remoteProjectMu sync.Mutex
	// remoteShellCache memoizes per-host shell probes (canonical
	// remote.Target.String() → remote.RemoteShellInfo) so the Settings UI and
	// terminal-config reads do not ssh on every request. Guarded by its own
	// mutex: probes run outside h.mu (an ssh round-trip must never hold the
	// handler lock).
	remoteShellCacheMu sync.Mutex
	remoteShellCache   map[string]remoteShellCacheEntry
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
	// cancelMu guards pendingCancel, the per-session cancellation flags for
	// HandleCancelSession. A cancel that arrives before the agent is registered
	// (e.g. during bootstrap) is held here so executeTurnJob can observe it.
	cancelMu      sync.Mutex
	pendingCancel map[string]bool
	// turnInFlight counts dispatched turn jobs that have not finished yet
	// (registered by dispatchTurn before the job goroutine starts, released
	// by the job's deferred completion). A cancel arriving between dispatch
	// and the job's first pendingCancel check must still be honored, so
	// HandleCancelSession treats a session with an in-flight job as active.
	// A counter (not a bool) is required: multiple async sends can dispatch
	// concurrently, and one job clearing a bool would hide a still-queued job
	// from cancellation.
	turnInFlight map[string]int
	// shutdownMu guards shutdownStarted. Once Shutdown begins, dispatchTurn
	// refuses new turn jobs so shutdown can join a bounded set; turnJobsWG
	// tracks every dispatched job (including jobs still bootstrapping an
	// agent, which own no resident agent yet) so shutdown waits for them
	// instead of missing them.
	shutdownMu      sync.Mutex
	shutdownStarted bool
	turnJobsWG      sync.WaitGroup

	// closePending marks sessions whose HandleCloseSession release could not
	// run immediately (an active turn held the agent). executeTurnJob drains
	// the marker after the turn (or bootstrap) finishes and releases the
	// agent then, so closing a session while it is running still tears the
	// backend down promptly instead of waiting for the idle eviction loop.
	// Guarded by cancelMu.
	closePending map[string]bool
	// saveMu guards saveLocks, the per-path file-save serialization mutexes.
	// Concurrent PUT /api/files/content for the same realTarget are
	// serialized so the compare-and-write 409 check is not TOCTOU-racy
	// against other ocode saves (external editors remain uncooperative).
	saveMu    sync.Mutex
	saveLocks map[string]*sync.Mutex
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

	// syncMu guards syncClientInst/syncStop, which back the /api/sync/* routes
	// (web/desktop equivalent of the TUI's /login, /logout). The lock is
	// owned internally by syncClient() and detachSyncClientForLogout() —
	// callers must NOT hold syncMu. This centralizes the lock contract so
	// a future caller cannot race on the shared *sync.Client.
	syncMu         sync.Mutex
	syncClientInst *ocodesync.Client
	syncStop       func()
	// syncConfiguredURL is the cfg.Ocode.SyncURL value last used to build
	// syncClientInst, so syncClient() only rebuilds it when that setting
	// actually changes at runtime — never on every call, which would clobber
	// a client pointed at a test server or otherwise built out-of-band.
	syncConfiguredURL string

	// lspMgrs holds one LSP manager per project root. Sessions bound to the
	// same project share a manager (multiple tabs on one repo don't spawn
	// redundant gopls processes), while sessions on different registered
	// projects each get language servers rooted at their own repo — see
	// lspManagerFor.
	lspMu   sync.Mutex
	lspMgrs map[string]*lsp.Manager

	// mediaTokens holds in-memory capability tokens for the local audio/video
	// streaming path (see media_tokens.go). A <video>/<audio> element can't
	// send the Authorization header, so the SPA exchanges its bearer for a
	// single-file, short-lived token it can put in the URL.
	mediaTokens *mediaTokenStore
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

// SetTerminalDetachTTL sets how long a detached shell is kept alive awaiting
// reattach. Called by Server.SetRemoteMode(true) to extend it to 24 h on the
// host; a fresh handler defaults to the 30 min local TTL.
func (h *Handler) SetTerminalDetachTTL(d time.Duration) {
	h.terminalSessions.mu.Lock()
	h.terminalSessions.detachTTL = d
	h.terminalSessions.mu.Unlock()
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
				Root:                mgr.Root(),
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
	// liveAppend mirrors a mid-turn transcript row into the in-flight
	// live-persist view (set by wireLivePersist for headless turns). Without it,
	// rows appended directly to messages — the auto-continue notice and resume
	// prompt — never reach disk, and a concurrent-writer reconcile at turn end
	// sees a non-prefix suffix and duplicates/reorders rows. Nil for bridged
	// turns, where the TUI persists its own transcript.
	liveAppend func(agent.Message)
	mu         sync.Mutex
	// spentMicros is this session's accumulated LLM spend in USD micros
	// (1e-6 USD), summed from each turn's Step messages' Spend plus side-path
	// calls (advisor/compact) via OnSideUsage. Atomic: written by the turn
	// goroutine and read by HTTP status handlers without taking as.mu.
	spentMicros atomic.Int64
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
	if cfg != nil {
		agent.SyncHarnessFromConfig(cfg.Ocode.FakeAgent)
	}
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
		agents:           make(map[string]*agentSession),
		cfg:              cfg,
		advisorEnabled:   advisorEnabled,
		workDir:          defaultWorkDir,
		projects:         projStore,
		projectGroups:    projGroupStore,
		tabsStore:        tabsStore,
		monaco:           monacoStore,
		headlessSubs:     make(map[chan SSEEvent]struct{}),
		toolOutput:       newToolOutputCoalescer(),
		mcpCache:         newMCPCache(),
		titleGen:         newTitleGenState(),
		bus:              NewEventBus(),
		terminalProcs:    newTerminalRegistry(),
		terminalSessions: newTerminalSessionTable(),
		shellSessions: newShellSessionRegistry(func(opts shellpkg.SessionOptions) (shellSession, error) {
			return shellpkg.NewSession(opts)
		}, defaultSessionIdleTimeout, time.Now),
		mediaTokens:       newMediaTokenStore(),
		terminalProcsWake: make(chan struct{}, 1),
		secretJobs:        secretjob.NewManager(),
		cliToolJobs:       newCLIToolJobManager(),
		mcpAuthJobs:       newMCPAuthJobManager(),
		saveLocks:         make(map[string]*sync.Mutex),
		pendingCancel:     make(map[string]bool),
		turnInFlight:      make(map[string]int),
		closePending:      make(map[string]bool),
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

	// Reap persistent `!` shells that have been idle past the session idle
	// timeout, so a tab that is never explicitly closed does not pin a shell
	// (and its rc environment) for the life of the process.
	h.shellSessions.startReaper(defaultSessionIdleTimeout / 4)

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
	// Discovery notices: the TUI renders these as transient "~ Discovered: …" /
	// "~ Indexing: …" transcript lines (see appendDiscoveryNotice in
	// internal/tui/model.go). The headless web had no equivalent surface, so
	// mirror them onto the bus with the same payload shape as the token deltas
	// (TextDelta). OnDiscovery fires when turn ranking attaches new
	// skills/MCP/docs; OnMDIndexing fires while a project doc's summary is being
	// generated. Both are cosmetic/transient — a dropped frame is harmless.
	ag.OnDiscovery = func(names string) {
		if names == "" {
			return
		}
		h.broadcastEvent(SSEEvent{
			SessionID: sessionID,
			Event:     "discovery",
			Data:      TextDelta{Delta: names},
		})
	}
	ag.OnMDIndexing = func(rel string) {
		h.broadcastEvent(SSEEvent{
			SessionID: sessionID,
			Event:     "md_indexing",
			Data:      TextDelta{Delta: rel},
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
	// A proxied remote request carries the originating desktop window's active
	// profile; apply it so buildAgentSession uses the same profile/keys the
	// user selected locally (the remote has no window-state.json).
	h.applyProxiedActiveProfile(r, windowID)

	model := req.Model
	if model == "" {
		model = h.effectiveSessionModel(req.SessionID)
	}

	sid := req.SessionID
	projectRoot := req.ProjectPath
	if projectRoot == "" {
		projectRoot = h.workDir
	}
	// Expand ~ in project paths so ~/x resolves to the server's own home.
	// Only a local project is expanded: a path registered as a remote project
	// on this server is stored verbatim and is expanded by the host that owns
	// $HOME (remote chat traffic is proxied there and never reaches this
	// handler). Expanding it here would rebind the turn to the wrong machine's
	// directory of the same name.
	if h.projectHostFor(projectRoot) == "" {
		expanded, err := projects.ExpandHome(projectRoot)
		if err != nil {
			log.Printf("chat: expand project path %q: %v", projectRoot, err)
			writeError(w, http.StatusBadRequest, fmt.Sprintf("expand project path: %v", err))
			return
		}
		projectRoot = expanded
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
		// Bind the session to its project root. An explicit project_path that
		// disagrees with the session's bound project is rejected (409): a
		// resident agent stays built for the old project while persistence
		// would move to the new one, so the next turn could execute in
		// project A but save into project B. An empty project_path keeps
		// the bound root (or the default root for unknown sessions).
		if req.ProjectPath != "" {
			snap, verr := h.sessions.BindNewOrVerify(sid, projectRoot, windowID)
			if verr != nil {
				writeError(w, http.StatusConflict, verr.Error())
				return
			}
			entry = h.sessions.Lookup(sid)
			if entry == nil {
				// Evicted between verify and lookup; re-register to the
				// verified snapshot root and continue.
				entry = h.sessions.RegisterWithWindow(sid, snap.ProjectRoot, windowID)
			}
		} else if entry = h.sessions.Lookup(sid); entry != nil {
			if windowID != "" {
				h.sessions.SetWindowID(sid, windowID)
				entry = h.sessions.Lookup(sid)
			}
		} else {
			var rerr error
			entry, rerr = h.sessions.Resolve(sid)
			if rerr != nil {
				// Session exists in no registered project. Today this
				// call site is lenient (a missing session silently starts
				// with empty history); preserve that by binding to the
				// default root and continuing.
				entry = h.sessions.RegisterWithWindow(sid, projectRoot, windowID)
			} else if windowID != "" {
				h.sessions.SetWindowID(sid, windowID)
				entry = h.sessions.Lookup(sid)
			}
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
		// Persist an explicitly-requested model and/or permission mode as this
		// session's overrides at creation time (before any turn), so a "new chat
		// with model X / yolo" pick survives resume/restart instead of silently
		// falling back to the global defaults. Later saves pass nil metadata and
		// the sqlite append preserves this row. Sessions created without an
		// explicit value get no override and keep following the config default.
		meta := map[string]any{}
		if req.Model != "" {
			meta["model"] = req.Model
		}
		if req.PermissionMode != "" {
			if mode, ok := normalizePermissionMode(req.PermissionMode); ok {
				meta[permissionModeMetadataKey] = string(mode)
			}
		}
		if len(meta) > 0 {
			if err := h.saveSession(sid, "", nil, meta); err != nil {
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
	// A close that arrived while this synchronous turn was running could not
	// release the agent mid-turn; drain the close-pending marker now that the
	// turn has unwound (runTurn set turnActive=false before returning).
	h.drainPendingClose(sid)
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
	// Cross-process change signal: the client records this token against the
	// transcript it just fetched, so a later out-of-process write is detected
	// by comparing it with the revision reported by /state.
	// Computed only for a stored session — a bridged/in-memory session has no
	// file to watch and reports nothing (mirrors HandleSessionState).
	revision := ""
	if entry.ProjectRoot != "" {
		if rev, rerr := session.StoredRevisionForDir(entry.ProjectRoot, id); rerr != nil {
			log.Printf("serve: session %s revision: %v", id, rerr)
		} else {
			revision = rev
		}
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
		Revision: revision,
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
	// A proxied remote request carries the originating desktop window's active
	// profile; apply it before the reconcile below rebuilds the resident agent
	// so this turn (and later ones) use the desktop's selected profile.
	h.applyProxiedActiveProfile(r, windowID)

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
	// A close that arrived while this synchronous turn was running could not
	// release the agent mid-turn; drain the close-pending marker now that the
	// turn has unwound (runTurn set turnActive=false before returning).
	h.drainPendingClose(id)
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
	// configured=true trims the registry to providers this server actually has
	// credentials/config for (see configuredProviderIDs). Opt-in so direct API
	// consumers can still enumerate the full registry.
	configuredParam := strings.TrimSpace(r.URL.Query().Get("configured"))
	configuredOnly := configuredParam == "1" || strings.EqualFold(configuredParam, "true")

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
		h.annotateModelFlags(h.workDir, modelsFiltered)
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
	// (equivalent to sorting the "provider/model" ids). With configured=true,
	// providers without credentials/config are dropped here — recents and
	// favorites above are always kept so a user's own picks never vanish.
	var configured map[string]bool
	if configuredOnly {
		configured = h.configuredProviderIDs()
		// Keep the currently configured model visible even if its provider has
		// no resolvable credential, so the active row is never missing.
		if currentModel != "" {
			if p, _, ok := splitModelID(currentModel); ok {
				configured[p] = true
			}
		}
	}
	rest := make([]string, 0, len(registry))
	for _, id := range registry {
		if shown[id] {
			continue
		}
		if configuredOnly {
			provider := providerOfModelID(id)
			if !configured[provider] && !agent.KeyOptionalProvider(provider) {
				continue
			}
		}
		rest = append(rest, id)
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

	h.annotateModelFlags(h.workDir, models)
	writeJSON(w, http.StatusOK, models)
}

// annotateModelFlags computes, for every listed model, whether an injectable
// model-specific custom prompt exists ({model}.OCODE.md via the root-anchored
// loader) and whether a force-injected Kaizen tuning directive is admitted for
// it in this project. Both checks are batched: the Kaizen stack is detected
// once and the *.OCODE.md search dirs are scanned once for the whole list, so
// annotating thousands of picker models does not perform a per-model directory
// scan. Root is the server's anchored workdir — never the process cwd (desktop
// boots with cwd "/").
func (h *Handler) annotateModelFlags(root string, models []ModelInfo) {
	if len(models) == 0 {
		return
	}
	ids := make([]string, 0, len(models))
	for _, m := range models {
		ids = append(ids, m.Name)
	}
	kaizen := skill.KaizenDigestAdmittedForModels(root, ids)
	promptKinds := agent.ModelContextKindsAt(root, ids)
	for i := range models {
		if kaizen[models[i].Name] {
			models[i].HasKaizen = true
		}
		if promptKinds[models[i].Name] != "" {
			models[i].HasModelPrompt = true
		}
	}
}
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

// providerOfModelID returns the provider prefix of a "provider/model" id, or
// "other" for an id without a slash (matching the ModelInfo.Provider fallback).
func providerOfModelID(id string) string {
	if provider, _, ok := splitModelID(id); ok {
		return provider
	}
	return "other"
}

// configuredProviderIDs returns the provider ids this server can actually call:
// any stored credential or OAuth token, a set provider env var, an explicit
// config `provider` block, or a keyless local provider. Used by
// HandleListModels?configured=true to drop the hundreds of registry providers
// the user has no account with. It deliberately does NOT resolve/refresh OAuth
// tokens (see auth.Get/auth.List) so a list request never triggers a network
// refresh.
func (h *Handler) configuredProviderIDs() map[string]bool {
	out := make(map[string]bool)
	h.mu.Lock()
	cfg := h.cfg
	h.mu.Unlock()
	if cfg != nil {
		for id := range cfg.Provider {
			if id != "" {
				out[id] = true
			}
		}
	}
	// Every stored credential counts, including provider ids that are not in
	// the auth registry (custom/gateway providers such as xiaomi-token-plan-sgp).
	for id := range auth.List() {
		if id != "" {
			out[id] = true
		}
	}
	for _, p := range auth.Providers {
		// A set env var (from the curated auth registry) counts as configured.
		if p.EnvVar != "" && os.Getenv(p.EnvVar) != "" {
			out[p.ID] = true
			continue
		}
		// Keyless/local provider (lmstudio) — usable without credentials.
		if p.EnvVar == "" && p.OAuthFlow == "" {
			out[p.ID] = true
		}
	}
	// The agent package's provider table is the authoritative env-var source and
	// covers keyed providers absent from auth.Providers (mistral, 302ai,
	// xiaomi-token-plan-*, chutes-coding, z.ai, …). Without this a user whose key
	// is env-only (no stored credential) would lose those providers from the
	// configured picker.
	for id, env := range agent.ProviderEnvVars() {
		if env != "" && os.Getenv(env) != "" {
			out[id] = true
		}
	}
	return out
}

func (h *Handler) HandleCompactSession(w http.ResponseWriter, r *http.Request, id string) {
	as, err := h.getOrCreateAgentSession(id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	// Optional focus steering the summary (the TUI's `/compact [focus]`). A
	// body is optional so the existing no-focus callers are unaffected.
	var body struct {
		Focus string `json:"focus"`
	}
	if r.Body != nil {
		_ = readBodyJSON(r, &body)
	}

	// Compaction is an LLM call. It runs under the per-session lock only —
	// holding h.mu across it would freeze every other session's turn for its
	// whole duration.
	as.mu.Lock()

	result, enabled := as.agent.CompactWithFocus(as.messages, body.Focus)
	if !enabled {
		as.mu.Unlock()
		writeError(w, http.StatusUnprocessableEntity, "compaction disabled in config")
		return
	}
	if !result.OK {
		as.mu.Unlock()
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
	as.mu.Unlock()

	// Refresh the per-session status snapshot so the web/desktop sidebar's
	// Context gauge reflects the compacted transcript immediately. Without
	// this the gauge kept the stale pre-compaction reading until the next turn.
	// Runs after the unlock: publishTurnStatusSnapshot resolves sessions and
	// broadcasts, and keeping it outside the as.mu region preserves the
	// as.mu → h.mu lock order. No-op when an RC bridge owns the status feed.
	h.publishTurnStatusSnapshot(id)

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
	if len(s.Messages) == 0 {
		writeError(w, http.StatusUnprocessableEntity, "session is empty")
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
		writeError(w, http.StatusUnprocessableEntity, "session is empty")
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

	// Context occupancy prefers the backend's provider-reported value: the
	// bridged TUI's live value for its session, else the session's live agent
	// LastInputTokens, then the agent's post-compaction estimate (LastInputTokens
	// is cleared by a /compact). Only when no live agent exists in this process
	// (restored/idle-evicted session) does it fall back to a chars/4 estimate
	// over the persisted transcript. This is the SAME chain applySessionContext
	// uses — the two entry points must agree (see the AGENTS.md rule).
	model := ""
	maxTokens := 0
	var current int64
	// currentSource records where the value came from so the token-budget
	// report does not label a chars/4 estimate "actual" (provider readings are
	// "actual"/"actual+tail"; the compacted heuristic is "estimated").
	currentSource := ""
	if rc := h.RCBridge(); rc != nil && rc.SessionID == id {
		if live := rc.TUIStatus(); live.ContextModel != "" {
			model = live.ContextModel
			maxTokens = live.ContextMaxTokens
			current = int64(live.ContextCurrentTokens)
			currentSource = "actual"
		}
	}
	if current == 0 {
		if as := h.lookupAgentSession(id); as != nil && as.agent != nil {
			current = as.agent.LastInputTokens()
			if current > 0 {
				currentSource = "actual"
			} else if current = as.agent.CompactedContextTokens(); current > 0 {
				currentSource = "estimated"
			}
		}
	}
	if current == 0 {
		current = estimateContextFromMessages(s.Messages)
		if current > 0 {
			currentSource = "estimated"
		}
	}
	if model == "" {
		model = h.effectiveSessionModel(id)
	}
	if maxTokens == 0 {
		maxTokens = int(agent.ModelWindow(model))
	}

	// Full token-budget breakdown: the same Report the TUI renders locally, so
	// the web/desktop `/context` shows identical sections and numbers. Only
	// available when a live agent exists and is not mid-turn (see
	// contextReportSource); otherwise the summary fields above are all we can
	// honestly report.
	resp := map[string]any{
		"session_id":     id,
		"message_count":  len(s.Messages),
		"current_tokens": current,
		"max_tokens":     maxTokens,
		"model":          model,
	}
	if ag, msgs, ok := h.contextReportSource(id); ok {
		h.mu.Lock()
		cfg := h.cfg
		h.mu.Unlock()
		in := contextbudget.Input{
			Agent:    ag,
			Messages: msgs,
			WorkDir:  entry.ProjectRoot,
			Config:   cfg,
		}
		// Override the report's derived context estimate with the same value
		// the summary carries, so the report's Context row and the summary
		// agree instead of the report re-deriving an estimate from message
		// text. The source follows the value: a provider reading is "actual",
		// a post-compaction / transcript chars/4 fallback is "estimated".
		if current > 0 {
			in.ContextTokens = current
			if currentSource != "" {
				in.ContextSource = currentSource
			} else {
				in.ContextSource = "actual"
			}
		}
		resp["report"] = contextbudget.Build(in)
	}
	writeJSON(w, http.StatusOK, resp)
}

// contextReportSource returns the live agent and a copy of its transcript for
// the /context breakdown, or ok=false when none is available.
//
// The read is deliberately conservative. The agent's tool maps are unsynchronised
// (discovery attaches tools mid-turn with no lock), so reading them from this
// HTTP goroutine while a turn runs can race the turn goroutine — and Go crashes
// outright on a concurrent map iteration + write. We therefore only read:
//   - a server-owned session while its per-session turn lock is free (TryLock,
//     so a running turn is never blocked — /context is user-initiated and can
//     simply fall back to the summary), and
//   - a bridged TUI session while the TUI is not mid-turn (the TUI owns its
//     agent; rc.GetMessages returns a safe copy).
func (h *Handler) contextReportSource(id string) (ag *agent.Agent, msgs []agent.Message, ok bool) {
	h.mu.Lock()
	rc := h.rc
	h.mu.Unlock()

	if rc != nil && rc.SessionID == id {
		if h.sessions.IsTurnActive(id) {
			return nil, nil, false
		}
		a := rc.Agent()
		if a == nil {
			return nil, nil, false
		}
		return a, rc.GetMessages(), true
	}

	as := h.lookupAgentSession(id)
	if as == nil || as.agent == nil {
		return nil, nil, false
	}
	if !as.mu.TryLock() {
		return nil, nil, false
	}
	defer as.mu.Unlock()
	return as.agent, append([]agent.Message(nil), as.messages...), true
}

// discoveryStatusDTO carries discovery configuration plus, when a live agent is
// reachable, the same runtime status the TUI's /discover status shows. The
// config fields mirror discoveryConfigDTO so /discover status can render both
// halves from one response; the runtime block is meaningful only when Live is
// true.
type discoveryStatusDTO struct {
	// Config (identical wire keys to discoveryConfigDTO).
	Enabled          bool     `json:"enabled"`
	EmbeddingModel   string   `json:"embedding_model"`
	EmbeddingBackend string   `json:"embedding_backend"`
	LocalModelStatus string   `json:"local_model_status"`
	LocalServerURL   string   `json:"local_server_url"`
	PinnedSkills     []string `json:"pinned_skills"`
	IgnorePaths      []string `json:"ignore_paths"`

	// Runtime status (agent.DiscoveryStatus). Live is false when no live agent
	// was reachable (idle/evicted session, or a turn is mid-flight and the
	// unsafe read was skipped) — every field below is then zero-valued.
	Live           bool     `json:"live"`
	Active         bool     `json:"active"`
	InitError      string   `json:"init_error,omitempty"`
	Judge          string   `json:"judge,omitempty"`
	JudgeVetoed    int      `json:"judge_vetoed"`
	MCPTotal       int      `json:"mcp_total"`
	SkillTotal     int      `json:"skill_total"`
	AttachedSkills []string `json:"attached_skills"`
	AttachedMCP    []string `json:"attached_mcp"`
	AttachedMD     []string `json:"attached_md"`
	AllSkills      []string `json:"all_skills"`
	AllMCP         []string `json:"all_mcp"`
	AllMD          []string `json:"all_md"`
	MDPending      int      `json:"md_pending"`
}

// HandleSessionDiscovery reports the discovery settings for a session plus its
// live runtime status (corpus sizes, attached skills/MCP/docs, judge vetoes)
// when an agent is reachable. It is the web counterpart to the TUI's
// `/discover status` (internal/tui/model.go:showDiscoverStatus) — the config
// half alone cannot tell a user whether discovery is actually working.
//
// The runtime read goes through contextReportSource on purpose: DiscoveryStatus
// touches the agent's unsynchronised tool maps, so reading it while a turn
// mutates those maps can race (and Go crashes on a concurrent map iteration +
// write). contextReportSource yields the agent only when that read is safe;
// otherwise the response carries config only with live=false, exactly like the
// /context breakdown degrading to its summary.
func (h *Handler) HandleSessionDiscovery(w http.ResponseWriter, r *http.Request, id string) {
	if _, err := h.sessions.Resolve(id); err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	h.mu.Lock()
	d := config.DiscoveryConfig{}
	if h.cfg != nil {
		d = h.cfg.Ocode.Discovery
	}
	h.mu.Unlock()

	resp := discoveryStatusDTO{
		Enabled: d.Enabled, EmbeddingModel: d.EmbeddingModel, EmbeddingBackend: d.EmbeddingBackend,
		LocalModelStatus: d.LocalModelStatus, LocalServerURL: d.LocalServerURL,
		PinnedSkills: d.PinnedSkills, IgnorePaths: d.IgnorePaths,
	}

	if ag, _, ok := h.contextReportSource(id); ok && ag != nil {
		st := ag.DiscoveryStatus()
		resp.Live = true
		resp.Active = st.Active
		resp.InitError = st.InitErr
		resp.Judge = st.Judge
		resp.JudgeVetoed = st.JudgeVetoed
		resp.MCPTotal = st.MCPTotal
		resp.SkillTotal = st.SkillTotal
		resp.AttachedSkills = nonNilStrings(st.AttachedSkills)
		resp.AttachedMCP = nonNilStrings(st.AttachedMCP)
		resp.AttachedMD = nonNilStrings(st.AttachedMD)
		resp.AllSkills = nonNilStrings(st.AllSkills)
		resp.AllMCP = nonNilStrings(st.AllMCP)
		resp.AllMD = nonNilStrings(st.AllMD)
		resp.MDPending = st.MDPending
	}
	writeJSON(w, http.StatusOK, resp)
}

// nonNilStrings returns s, or an empty slice when s is nil, so a JSON response
// serializes an absent list as [] rather than null. The web client reads
// `.length` on these arrays and would throw on null.
func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// HandleShellCommand executes a shell command and returns the output.
// This provides cross-platform shell execution for the web UI (! prefix commands).
//
// The actual spawn-and-capture work is delegated to internal/shell so the
// TUI agent loop and the server share one implementation (timeout,
// Setpgid, exit-code extraction, error-string policy). The handler is
// responsible for the HTTP-level concerns: input validation, response
// shape, and the workDir defaulting chain (request → server workDir → ".").
//
// A request may name a `host` (an ocode Remote project). The command then runs
// on that host instead of locally, through the host's own login shell — the
// same rule the interactive terminal follows in HandleTerminalWS. Running it
// locally would execute against the wrong filesystem, and using this machine's
// shell resolution to do it is what produced
// `fork/exec /bin/zsh: no such file or directory` on a Linux remote.
func (h *Handler) HandleShellCommand(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Command string `json:"command"`
		WorkDir string `json:"workDir,omitempty"`
		Host    string `json:"host,omitempty"`
		// Session is the opaque frontend tab key the persistent shell is
		// scoped to. Empty keeps the historical one-shot path (back-compat for
		// a client that has not been updated).
		Session string `json:"session,omitempty"`
		// Reset closes any existing shell for Session before running. Server
		// side only in v1: no client sends it yet.
		Reset bool `json:"reset,omitempty"`
	}
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Command == "" {
		writeError(w, http.StatusBadRequest, "command is required")
		return
	}

	if req.Host != "" {
		h.handleRemoteShellCommand(w, r, req.Host, req.WorkDir, req.Command)
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

	// Local `!` commands run in the session's persistent shell so the user's
	// env/aliases/functions are present and state persists. Any failure to
	// provide one (no pty, Windows, a racing close) degrades to the one-shot
	// run rather than failing the command.
	if req.Session != "" && h.shellSessions != nil {
		res, cwd, err := h.shellSessions.run(r.Context(), req.Session, workDir, req.Command, req.Reset)
		if err == nil {
			writeShellCommandResponse(w, res, cwd)
			return
		}
		log.Printf("server: persistent shell for session %q unavailable (%v); running one-shot", req.Session, err)
	}

	res := shellpkg.Run(req.Command, workDir)
	writeShellCommandResponse(w, res, workDir)
}

// writeShellCommandResponse is the single response shape for POST /api/shell:
// combined output, exit code, an error string, and the directory the command
// ran in. cwd is present on every path (persistent shell, one-shot fallback,
// remote) so the client never has to default a missing field.
func writeShellCommandResponse(w http.ResponseWriter, res shellpkg.Result, cwd string) {
	errMsg := ""
	if res.Err != nil {
		errMsg = res.Err.Error()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"output":   res.Output,
		"exitCode": res.ExitCode,
		"error":    errMsg,
		"cwd":      cwd,
	})
}

// handleRemoteShellCommand runs a `!` shell command on a registered remote
// project. The host/path pair must resolve through remoteWorkFor, which is the
// same admission check every other remote endpoint applies, so the request can
// never name an unregistered ssh target. The shell itself is resolved on the
// remote (remote.LoginShellScript), never from local config.
func (h *Handler) handleRemoteShellCommand(w http.ResponseWriter, r *http.Request, host, path, command string) {
	rw, err := h.remoteWorkFor(host, path)
	if err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	res := remoteShellRun(r.Context(), rw, command, shellpkg.DefaultTimeout)
	errMsg := ""
	if res.Err != nil {
		errMsg = res.Err.Error()
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"output":   res.Output,
		"exitCode": res.ExitCode,
		"error":    errMsg,
		// The remote command runs in `path` (resolved by remoteWorkFor); report
		// it so the response shape matches the local paths.
		"cwd": path,
	})
}
