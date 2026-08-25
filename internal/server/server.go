package server

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/scheduler"
)

type rlEntry struct {
	failures    int
	lockedUntil time.Time
}

type rateLimiter struct {
	mu      sync.Mutex
	entries map[string]*rlEntry
}

func newRateLimiter() *rateLimiter {
	return &rateLimiter{entries: make(map[string]*rlEntry)}
}

func (rl *rateLimiter) isBlocked(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	e := rl.entries[ip]
	return e != nil && time.Now().Before(e.lockedUntil)
}

func (rl *rateLimiter) recordFailure(ip string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	e := rl.entries[ip]
	if e == nil {
		e = &rlEntry{}
		rl.entries[ip] = e
	}
	e.failures++
	if e.failures >= 5 {
		e.lockedUntil = time.Now().Add(time.Minute)
		e.failures = 0
	}
}

func (rl *rateLimiter) reset(ip string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	delete(rl.entries, ip)
}

type Server struct {
	addr             string
	username         string
	password         string
	rl               *rateLimiter
	mux              *http.ServeMux
	handler          *Handler
	webFS            fs.FS
	workDir          string
	scheduler        any               // optional *scheduler.Service; set via SetScheduler
	schedulerOutbox  *scheduler.Outbox // optional; set via SetScheduler
	schedulerRuns    *scheduler.RunHistory
	schedulerTargets *scheduler.Targets // optional; set via SetScheduler
	frontendStats    *frontendStatsRing
	startedAt        time.Time

	// ln and httpServer are populated by Serve so Shutdown can stop accepting
	// new connections and drain in-flight ones. Guarded by shutdownMu.
	shutdownMu sync.Mutex
	ln         net.Listener
	httpServer *http.Server
}

func New(addr, username, password string, webFS fs.FS) *Server {
	mux := http.NewServeMux()
	h := NewHandler()
	s := &Server{
		addr:          addr,
		username:      username,
		password:      password,
		rl:            newRateLimiter(),
		mux:           mux,
		handler:       h,
		frontendStats: newFrontendStatsRing(),
		webFS:         webFS,
		workDir:       ".",
		startedAt:     time.Now(),
	}
	h.SetTerminalAccessPolicy(username != "" || password != "", isLoopbackBind(addr))
	s.registerRoutes()
	return s
}

func isLoopbackBind(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (s *Server) registerRoutes() {
	s.mux.HandleFunc("POST /api/chat", s.authMiddleware(s.handleChat))
	s.mux.HandleFunc("GET /api/chat/stream", s.authMiddleware(s.handleChatStream))
	s.mux.HandleFunc("GET /api/chat/messages", s.authMiddleware(s.handleSessionMessages))
	// Unified tagged event bus (Part 02). The single SSE stream that replaces
	// the chat mirror, logs SSE, and per-session agent-runs SSE (those stay
	// functional until Part 06 removes them).
	s.mux.HandleFunc("GET /api/events", s.authMiddleware(s.handleEvents))
	s.mux.HandleFunc("GET /api/sessions", s.authMiddleware(s.handleListSessions))
	s.mux.HandleFunc("GET /api/sessions/{id}", s.authMiddleware(s.handleGetSession))
	s.mux.HandleFunc("GET /api/sessions/{id}/state", s.authMiddleware(s.handleSessionState))
	s.mux.HandleFunc("GET /api/sessions/{id}/status", s.authMiddleware(s.handleSessionStatus))
	s.mux.HandleFunc("POST /api/sessions/{id}/message", s.authMiddleware(s.handleSendMessage))
	s.mux.HandleFunc("GET /api/models", s.authMiddleware(s.handleListModels))
	s.mux.HandleFunc("GET /api/agents/runs", s.authMiddleware(s.handleListRuns))
	s.mux.HandleFunc("GET /api/agents/runs/stream", s.authMiddleware(s.handleRunsStream))
	s.mux.HandleFunc("GET /api/changes", s.authMiddleware(s.handleListChanges))
	s.mux.HandleFunc("GET /api/changes/diff", s.authMiddleware(s.handleChangesDiff))
	s.mux.HandleFunc("POST /api/changes/undo-file", s.authMiddleware(s.handleUndoChangeFile))
	s.mux.HandleFunc("POST /api/changes/undo-block", s.authMiddleware(s.handleUndoChangeBlock))
	s.mux.HandleFunc("GET /api/git/status", s.authMiddleware(s.handleGitStatus))
	s.mux.HandleFunc("GET /api/git/diff", s.authMiddleware(s.handleGitDiff))
	s.mux.HandleFunc("GET /api/theme", s.authMiddleware(s.handleGetTheme))
	s.mux.HandleFunc("GET /api/themes", s.authMiddleware(s.handleListThemes))
	s.mux.HandleFunc("GET /api/files/tree", s.authMiddleware(s.handleFileTree))
	s.mux.HandleFunc("GET /api/files/content", s.authMiddleware(s.handleFileContent))
	s.mux.HandleFunc("PUT /api/files/content", s.authMiddleware(s.handleSaveFileContent))
	s.mux.HandleFunc("POST /api/files/open", s.authMiddleware(s.handleOpenFile))

	// TUI status (consolidated snapshot for the web UI status bar)
	s.mux.HandleFunc("GET /api/tui-status", s.authMiddleware(s.handleGetTUIStatus))
	s.mux.HandleFunc("GET /api/paths", s.authMiddleware(s.handleGetPathsInfo))
	s.mux.HandleFunc("GET /api/memory/status", s.authMiddleware(s.handleGetMemoryStatus))
	s.mux.HandleFunc("GET /api/docs/status", s.authMiddleware(s.handleDocsStatus))
	s.mux.HandleFunc("POST /api/docs/init", s.authMiddleware(s.handleDocsInit))
	s.mux.HandleFunc("POST /api/docs/update", s.authMiddleware(s.handleDocsUpdate))
	s.mux.HandleFunc("POST /api/docs/cleanup", s.authMiddleware(s.handleDocsCleanup))
	s.mux.HandleFunc("POST /api/auth/connect", s.authMiddleware(s.handleConnectProvider))
	s.mux.HandleFunc("GET /api/spending", s.authMiddleware(s.handleGetSpending))
	s.mux.HandleFunc("GET /api/lsp/statuses", s.authMiddleware(s.handleGetLSPStatuses))
	s.mux.HandleFunc("GET /api/files/modified", s.authMiddleware(s.handleGetModifiedFiles))
	s.mux.HandleFunc("POST /api/debug/frontend-stats", s.authMiddleware(s.handlePostFrontendStats))
	s.mux.HandleFunc("GET /api/debug/frontend-stats", s.authMiddleware(s.handleGetFrontendStats))

	// Session operations
	s.mux.HandleFunc("POST /api/sessions/{id}/compact", s.authMiddleware(s.handleCompactSession))
	s.mux.HandleFunc("GET /api/sessions/{id}/recap", s.authMiddleware(s.handleRecapSession))
	s.mux.HandleFunc("GET /api/sessions/{id}/export", s.authMiddleware(s.handleExportSession))
	s.mux.HandleFunc("GET /api/sessions/{id}/export-claude", s.authMiddleware(s.handleExportClaudeSession))
	s.mux.HandleFunc("GET /api/sessions/{id}/share", s.authMiddleware(s.handleShareSession))
	s.mux.HandleFunc("POST /api/sessions/{id}/btw", s.authMiddleware(s.handleBtw))
	s.mux.HandleFunc("PUT /api/sessions/{id}/title", s.authMiddleware(s.handleSetSessionTitle))
	s.mux.HandleFunc("POST /api/sessions/{id}/title/generate", s.authMiddleware(s.handleGenerateSessionTitle))
	s.mux.HandleFunc("GET /api/sessions/{id}/context", s.authMiddleware(s.handleSessionContext))

	// Files
	s.mux.HandleFunc("POST /api/files/undo", s.authMiddleware(s.handleUndo))
	s.mux.HandleFunc("POST /api/files/redo", s.authMiddleware(s.handleRedo))

	// Shell execution
	s.mux.HandleFunc("POST /api/shell", s.authMiddleware(s.handleShellCommand))

	// Config
	s.mux.HandleFunc("GET /api/config/model", s.authMiddleware(s.handleGetModel))
	s.mux.HandleFunc("PUT /api/config/model", s.authMiddleware(s.handleSetModel))
	s.mux.HandleFunc("GET /api/config/thinking-budget", s.authMiddleware(s.handleGetThinkingBudget))
	s.mux.HandleFunc("PUT /api/config/thinking-budget", s.authMiddleware(s.handleSetThinkingBudget))
	s.mux.HandleFunc("GET /api/config/small-model", s.authMiddleware(s.handleGetSmallModel))
	s.mux.HandleFunc("PUT /api/config/small-model", s.authMiddleware(s.handleSetSmallModel))
	s.mux.HandleFunc("GET /api/config/permission-model", s.authMiddleware(s.handleGetPermissionModel))
	s.mux.HandleFunc("PUT /api/config/permission-model", s.authMiddleware(s.handleSetPermissionModel))
	s.mux.HandleFunc("GET /api/config/terminal", s.authMiddleware(s.handleGetTerminalConfig))
	s.mux.HandleFunc("PUT /api/config/terminal", s.authMiddleware(s.handleSetTerminalConfig))
	s.mux.HandleFunc("GET /api/config/ocode/recap", s.authMiddleware(s.handleGetRecapConfig))
	s.mux.HandleFunc("PUT /api/config/ocode/recap", s.authMiddleware(s.handleSetRecapConfig))
	s.mux.HandleFunc("GET /api/config/ocode/commit-msg", s.authMiddleware(s.handleGetCommitMsgConfig))
	s.mux.HandleFunc("PUT /api/config/ocode/commit-msg", s.authMiddleware(s.handleSetCommitMsgConfig))
	s.mux.HandleFunc("GET /api/config/ocode/compact", s.authMiddleware(s.handleGetCompactConfig))
	s.mux.HandleFunc("PUT /api/config/ocode/compact", s.authMiddleware(s.handleSetCompactConfig))
	s.mux.HandleFunc("GET /api/config/ocode/permissions-auto", s.authMiddleware(s.handleGetAutoPermissionConfig))
	s.mux.HandleFunc("PUT /api/config/ocode/permissions-auto", s.authMiddleware(s.handleSetAutoPermissionConfig))
	s.mux.HandleFunc("GET /api/config/ocode/discovery", s.authMiddleware(s.handleGetDiscoveryConfig))
	s.mux.HandleFunc("PUT /api/config/ocode/discovery", s.authMiddleware(s.handleSetDiscoveryConfig))
	s.mux.HandleFunc("GET /api/config/ocode/tui", s.authMiddleware(s.handleGetTUIConfigSection))
	s.mux.HandleFunc("PUT /api/config/ocode/tui", s.authMiddleware(s.handleSetTUIConfigSection))
	s.mux.HandleFunc("GET /api/config/ocode/editor", s.authMiddleware(s.handleGetEditorConfig))
	s.mux.HandleFunc("PUT /api/config/ocode/editor", s.authMiddleware(s.handleSetEditorConfig))
	s.mux.HandleFunc("GET /api/config/ocode/imagegen", s.authMiddleware(s.handleGetImageGenConfig))
	s.mux.HandleFunc("PUT /api/config/ocode/imagegen", s.authMiddleware(s.handleSetImageGenConfig))
	s.mux.HandleFunc("GET /api/config/ocode/paths", s.authMiddleware(s.handleGetPathsConfig))
	s.mux.HandleFunc("PUT /api/config/ocode/paths", s.authMiddleware(s.handleSetPathsConfig))
	s.mux.HandleFunc("GET /api/config/ocode/autocontinue", s.authMiddleware(s.handleGetAutoContinue))
	s.mux.HandleFunc("PUT /api/config/ocode/autocontinue", s.authMiddleware(s.handleSetAutoContinue))
	s.mux.HandleFunc("GET /api/config/ocode/limits", s.authMiddleware(s.handleGetLimitsConfig))
	s.mux.HandleFunc("PUT /api/config/ocode/limits", s.authMiddleware(s.handleSetLimitsConfig))
	s.mux.HandleFunc("GET /api/config/ocode/features", s.authMiddleware(s.handleGetFeaturesConfig))
	s.mux.HandleFunc("PUT /api/config/ocode/features", s.authMiddleware(s.handleSetFeaturesConfig))
	s.mux.HandleFunc("GET /api/config/ocode/profile-debug", s.authMiddleware(s.handleGetProfileDebugConfig))
	s.mux.HandleFunc("PUT /api/config/ocode/profile-debug", s.authMiddleware(s.handleSetProfileDebugConfig))
	s.mux.HandleFunc("GET /api/config/ocode/plugins-enabled", s.authMiddleware(s.handleGetPluginsEnabledConfig))
	s.mux.HandleFunc("PUT /api/config/ocode/plugins-enabled", s.authMiddleware(s.handleSetPluginsEnabledConfig))
	s.mux.HandleFunc("GET /api/config/ocode/local-models", s.authMiddleware(s.handleGetLocalModelsConfig))
	s.mux.HandleFunc("PUT /api/config/ocode/local-models", s.authMiddleware(s.handleSetLocalModelsConfig))
	// Interactive pty terminal (always enabled). The browser cannot set an
	// Authorization header on a WebSocket, so this relies on
	// authMiddleware's ?token= support.
	s.mux.HandleFunc("GET /api/terminal/ws", s.authMiddleware(s.handleTerminalWS))
	s.mux.HandleFunc("GET /api/terminal/processes", s.authMiddleware(s.handleTerminalProcesses))
	s.mux.HandleFunc("GET /api/config/advisor", s.authMiddleware(s.handleGetAdvisor))
	s.mux.HandleFunc("PUT /api/config/advisor", s.authMiddleware(s.handleSetAdvisor))
	s.mux.HandleFunc("GET /api/config/advisor-enabled", s.authMiddleware(s.handleGetAdvisorEnabled))
	s.mux.HandleFunc("PUT /api/config/advisor-enabled", s.authMiddleware(s.handleSetAdvisorEnabled))
	s.mux.HandleFunc("GET /api/config/ocr-enabled", s.authMiddleware(s.handleGetOcrEnabled))
	s.mux.HandleFunc("PUT /api/config/ocr-enabled", s.authMiddleware(s.handleSetOcrEnabled))
	s.mux.HandleFunc("PUT /api/config/ocr-model", s.authMiddleware(s.handleSetOcrModel))
	s.mux.HandleFunc("GET /api/config/ocr", s.authMiddleware(s.handleGetOcrConfig))
	s.mux.HandleFunc("PUT /api/config/ocr", s.authMiddleware(s.handleSetOcrConfig))
	s.mux.HandleFunc("GET /api/ocr/models", s.authMiddleware(s.handleGetOcrModels))
	// Mask (secret redaction) config
	s.mux.HandleFunc("GET /api/config/mask", s.authMiddleware(s.handleGetMaskConfig))
	s.mux.HandleFunc("PUT /api/config/mask/enabled", s.authMiddleware(s.handleSetMaskEnabled))
	s.mux.HandleFunc("PUT /api/config/mask/mode", s.authMiddleware(s.handleSetMaskMode))
	s.mux.HandleFunc("PUT /api/config/mask/model", s.authMiddleware(s.handleSetMaskModel))
	s.mux.HandleFunc("PUT /api/config/mask/advanced", s.authMiddleware(s.handleSetMaskAdvanced))
	s.mux.HandleFunc("GET /api/config/agents", s.authMiddleware(s.handleListAgents))
	s.mux.HandleFunc("PUT /api/config/agent", s.authMiddleware(s.handleSetAgent))

	// Account sync (web/desktop equivalent of the TUI's /login, /logout)
	s.mux.HandleFunc("POST /api/sync/login/start", s.authMiddleware(s.handleSyncLoginStart))
	s.mux.HandleFunc("POST /api/sync/login/poll", s.authMiddleware(s.handleSyncLoginPoll))
	s.mux.HandleFunc("GET /api/sync/status", s.authMiddleware(s.handleSyncStatus))
	s.mux.HandleFunc("POST /api/sync/logout", s.authMiddleware(s.handleSyncLogout))

	// Permissions
	s.mux.HandleFunc("GET /api/permissions", s.authMiddleware(s.handleGetPermissions))
	s.mux.HandleFunc("POST /api/permissions", s.authMiddleware(s.handleSetPermission))
	s.mux.HandleFunc("POST /api/permissions/bash-rule", s.authMiddleware(s.handleSetBashRule))
	s.mux.HandleFunc("POST /api/questions", s.authMiddleware(s.handleAnswerQuestion))
	s.mux.HandleFunc("POST /api/permissions/resolve", s.authMiddleware(s.handleResolvePermission))
	s.mux.HandleFunc("GET /api/permissions/yolo", s.authMiddleware(s.handleGetYolo))
	s.mux.HandleFunc("PUT /api/permissions/yolo", s.authMiddleware(s.handleSetYolo))
	// Remote-control (external client, e.g. Telegram) permission/question
	// resolution. These target the live TUI agent via the RC bridge.
	s.mux.HandleFunc("POST /api/rc/permission/resolve", s.authMiddleware(s.handler.handleRCPermissionResolve))
	s.mux.HandleFunc("POST /api/rc/question/answer", s.authMiddleware(s.handler.handleRCQuestionAnswer))

	// MCP
	s.mux.HandleFunc("GET /api/mcp", s.authMiddleware(s.handleListMCP))
	s.mux.HandleFunc("PUT /api/mcp/{name}/enable", s.authMiddleware(s.handleEnableMCP))
	s.mux.HandleFunc("PUT /api/mcp/{name}/disable", s.authMiddleware(s.handleDisableMCP))

	// Plugins
	s.mux.HandleFunc("GET /api/plugins", s.authMiddleware(s.handleListPlugins))
	s.mux.HandleFunc("GET /api/plugins/{name}", s.authMiddleware(s.handleGetPlugin))
	s.mux.HandleFunc("PUT /api/plugins/{name}/enable", s.authMiddleware(s.handleEnablePlugin))
	s.mux.HandleFunc("PUT /api/plugins/{name}/disable", s.authMiddleware(s.handleDisablePlugin))
	s.mux.HandleFunc("POST /api/plugins", s.authMiddleware(s.handleInstallPlugin))
	s.mux.HandleFunc("DELETE /api/plugins/{name}", s.authMiddleware(s.handleRemovePlugin))

	// Usage
	s.mux.HandleFunc("GET /api/usage", s.authMiddleware(s.handleGetUsage))

	// Logs
	s.mux.HandleFunc("GET /api/logs", s.authMiddleware(s.handleGetLogs))
	s.mux.HandleFunc("GET /api/logs/stream", s.authMiddleware(s.handleLogStream))
	s.mux.HandleFunc("DELETE /api/logs", s.authMiddleware(s.handleClearLogs))

	// Info
	s.mux.HandleFunc("GET /api/skills", s.authMiddleware(s.handleListSkills))
	s.mux.HandleFunc("GET /api/commands", s.authMiddleware(s.handleListCommands))
	s.mux.HandleFunc("GET /api/command-context/{name}", s.authMiddleware(s.handleCommandContext))
	s.mux.HandleFunc("GET /api/github/pr/{owner}/{repo}/{number}", s.authMiddleware(s.handleGitHubPR))
	s.mux.HandleFunc("GET /api/github/issues/{owner}/{repo}", s.authMiddleware(s.handleGitHubIssues))
	s.mux.HandleFunc("POST /api/init", s.authMiddleware(s.handleInit))

	// Projects (multi-project desktop UI)
	s.mux.HandleFunc("GET /api/projects", s.authMiddleware(s.handleListProjects))
	s.mux.HandleFunc("GET /api/projects/current", s.authMiddleware(s.handleGetCurrentProject))
	s.mux.HandleFunc("POST /api/projects", s.authMiddleware(s.handleAddProject))
	s.mux.HandleFunc("DELETE /api/projects/{path...}", s.authMiddleware(s.handleRemoveProject))
	s.mux.HandleFunc("GET /api/projects/sessions", s.authMiddleware(s.handleListProjectSessions))
	s.mux.HandleFunc("POST /api/projects/rename", s.authMiddleware(s.handleRenameProject))
	s.mux.HandleFunc("POST /api/projects/reorder", s.authMiddleware(s.handleReorderProjects))
	s.mux.HandleFunc("POST /api/projects/group", s.authMiddleware(s.handleSetProjectGroup))
	s.mux.HandleFunc("GET /api/projects/groups", s.authMiddleware(s.handleListGroups))
	s.mux.HandleFunc("POST /api/projects/groups", s.authMiddleware(s.handleCreateGroup))
	s.mux.HandleFunc("DELETE /api/projects/groups/{name}", s.authMiddleware(s.handleDeleteGroup))
	s.mux.HandleFunc("POST /api/projects/groups/rename", s.authMiddleware(s.handleRenameGroup))
	s.mux.HandleFunc("POST /api/projects/groups/reorder", s.authMiddleware(s.handleReorderGroups))
	s.mux.HandleFunc("POST /api/projects/groups/collapse", s.authMiddleware(s.handleSetGroupCollapsed))

	// Profiles — desktop per-window sparse overlays
	s.mux.HandleFunc("GET /api/profiles", s.authMiddleware(s.handler.handleListProfiles))
	s.mux.HandleFunc("POST /api/profiles", s.authMiddleware(s.handler.handleCreateProfile))
	s.mux.HandleFunc("GET /api/profiles/{name}", s.authMiddleware(s.handler.handleGetProfile))
	s.mux.HandleFunc("GET /api/profiles/{name}/effective", s.authMiddleware(s.handler.handleGetProfileEffective))
	s.mux.HandleFunc("DELETE /api/profiles/{name}", s.authMiddleware(s.handler.handleDeleteProfile))
	s.mux.HandleFunc("POST /api/profiles/{name}/rename", s.authMiddleware(s.handler.handleRenameProfile))
	s.mux.HandleFunc("GET /api/profiles/{name}/auth", s.authMiddleware(s.handler.handleGetProfileAuth))
	s.mux.HandleFunc("PUT /api/profiles/{name}/auth/{provider}", s.authMiddleware(s.handler.handleSetProfileAuth))
	s.mux.HandleFunc("DELETE /api/profiles/{name}/auth/{provider}", s.authMiddleware(s.handler.handleDeleteProfileAuth))
	s.mux.HandleFunc("DELETE /api/profiles/{name}/overrides/{field}", s.authMiddleware(s.handler.handleResetProfileField))
	s.mux.HandleFunc("GET /api/window/{id}/activeProfile", s.authMiddleware(s.handler.handleGetWindowActiveProfile))
	s.mux.HandleFunc("PUT /api/window/{id}/activeProfile", s.authMiddleware(s.handler.handleSetWindowActiveProfile))

	// Open-session tab state (server-side persistence; survives desktop restarts)
	s.mux.HandleFunc("GET /api/tabs", s.authMiddleware(s.handleGetTabs))
	s.mux.HandleFunc("PUT /api/tabs", s.authMiddleware(s.handleSetTabs))

	// Directory browser for the project sidebar folder picker.
	s.mux.HandleFunc("GET /api/browse", s.authMiddleware(s.handleBrowseDirectory))

	// Monaco editor settings and extensions
	s.mux.HandleFunc("GET /api/monaco/settings", s.authMiddleware(s.handleGetMonacoSettings))
	s.mux.HandleFunc("PUT /api/monaco/settings", s.authMiddleware(s.handleSetMonacoSettings))
	s.mux.HandleFunc("GET /api/monaco/extensions", s.authMiddleware(s.handleListMonacoExtensions))
	s.mux.HandleFunc("PUT /api/monaco/extensions/{name}/toggle", s.authMiddleware(s.handleToggleMonacoExtension))

	// Uploads (assets)
	s.mux.HandleFunc("/api/uploads", s.authMiddleware(s.handleUploads))
	s.mux.HandleFunc("/api/uploads/file", s.authMiddleware(s.handleUploadFile))

	// Runtime diagnostics: Go heap/goroutine stats and net/http/pprof profiles,
	// gated behind the same auth as everything else so it's never exposed
	// unauthenticated even on a non-loopback bind.
	s.mux.HandleFunc("GET /api/debug/runtime", s.authMiddleware(s.handleDebugRuntime))
	s.mux.HandleFunc("/debug/pprof/", s.authMiddleware(pprof.Index))
	s.mux.HandleFunc("/debug/pprof/cmdline", s.authMiddleware(pprof.Cmdline))
	s.mux.HandleFunc("/debug/pprof/profile", s.authMiddleware(pprof.Profile))
	s.mux.HandleFunc("/debug/pprof/symbol", s.authMiddleware(pprof.Symbol))
	s.mux.HandleFunc("/debug/pprof/trace", s.authMiddleware(pprof.Trace))

	// Serve embedded web UI for non-API routes
	s.mux.Handle("/", spaHandler(s.webFS))
}

func realIP(r *http.Request) string {
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	if host != "" {
		return host
	}
	if r.RemoteAddr != "" {
		return r.RemoteAddr
	}
	return "unknown"
}

func (s *Server) checkAuth(r *http.Request) bool {
	// Bearer token header (used by frontend fetch calls)
	if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
		return auth[7:] == s.password
	}
	// ?token= query param (used by EventSource which can't set headers)
	if tok := r.URL.Query().Get("token"); tok != "" {
		return tok == s.password
	}
	// HTTP Basic Auth
	user, pass, ok := r.BasicAuth()
	if ok {
		return (s.username == "" || user == s.username) && pass == s.password
	}
	return false
}

func (s *Server) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	if s.username == "" && s.password == "" {
		return next
	}
	return func(w http.ResponseWriter, r *http.Request) {
		ip := realIP(r)
		if s.rl.isBlocked(ip) {
			http.Error(w, "too many requests", http.StatusTooManyRequests)
			return
		}
		if !s.checkAuth(r) {
			s.rl.recordFailure(ip)
			w.Header().Set("WWW-Authenticate", `Basic realm="ocode"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		s.rl.reset(ip)
		next(w, r)
	}
}

func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleChat(w, r)
}

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleListSessions(w, r)
}

func (s *Server) handleGetSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.handler.HandleGetSession(w, r, id)
}

func (s *Server) handleSessionState(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.handler.HandleSessionState(w, r, id)
}

func (s *Server) handleSessionStatus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.handler.HandleSessionStatus(w, r, id)
}

func (s *Server) handleSendMessage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.handler.HandleSendMessage(w, r, id)
}

func (s *Server) handleChatStream(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleChatStream(w, r)
}

func (s *Server) handleSessionMessages(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleSessionMessages(w, r)
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleEvents(w, r)
}

func (s *Server) handleListModels(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleListModels(w, r)
}

func (s *Server) handleGitStatus(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleGitStatus(w, r)
}

func (s *Server) handleGitDiff(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleGitDiff(w, r)
}

func (s *Server) handleGetTheme(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleGetTheme(w, r)
}

// debugRuntimeStats reports process-wide Go runtime memory/goroutine stats.
// This is process totals, not per-session — attributing heap usage to an
// individual session would require per-session profiling, which pprof (also
// wired at /debug/pprof/) provides instead.
type debugRuntimeStats struct {
	HeapAllocBytes uint64 `json:"heap_alloc_bytes"`
	HeapSysBytes   uint64 `json:"heap_sys_bytes"`
	SysBytes       uint64 `json:"sys_bytes"`
	NumGoroutine   int    `json:"num_goroutine"`
	NumGC          uint32 `json:"num_gc"`
	Uptime         string `json:"uptime"`
}

func (s *Server) handleDebugRuntime(w http.ResponseWriter, r *http.Request) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	writeJSON(w, http.StatusOK, debugRuntimeStats{
		HeapAllocBytes: m.HeapAlloc,
		HeapSysBytes:   m.HeapSys,
		SysBytes:       m.Sys,
		NumGoroutine:   runtime.NumGoroutine(),
		NumGC:          m.NumGC,
		Uptime:         time.Since(s.startedAt).String(),
	})
}

func (s *Server) handleSyncLoginStart(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleSyncLoginStart(w, r)
}

func (s *Server) handleSyncLoginPoll(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleSyncLoginPoll(w, r)
}

func (s *Server) handleSyncStatus(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleSyncStatus(w, r)
}

func (s *Server) handleSyncLogout(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleSyncLogout(w, r)
}

func (s *Server) handleListThemes(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleListThemes(w, r)
}

func (s *Server) handleFileTree(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleFileTree(w, r)
}

func (s *Server) handleFileContent(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleFileContent(w, r)
}

func (s *Server) handleSaveFileContent(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleSaveFileContent(w, r)
}

func (s *Server) handleOpenFile(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleOpenFile(w, r)
}

func (s *Server) handleUploads(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleUploads(w, r)
}

func (s *Server) handleUploadFile(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleUploadFile(w, r)
}

// Listen binds a TCP listener for the server. If the requested port is already
// in use it walks forward to the next free port (up to maxPortAttempts) and
// updates s.addr to the address actually bound, so callers can read the real
// port back from s.addr afterwards.
func (s *Server) Listen() (net.Listener, error) {
	const maxPortAttempts = 20

	host, portStr, err := net.SplitHostPort(s.addr)
	if err != nil {
		return nil, fmt.Errorf("invalid listen address %q: %w", s.addr, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, fmt.Errorf("invalid port %q: %w", portStr, err)
	}

	for i := 0; i < maxPortAttempts; i++ {
		candidate := net.JoinHostPort(host, strconv.Itoa(port+i))
		ln, err := net.Listen("tcp", candidate)
		if err != nil {
			if errors.Is(err, syscall.EADDRINUSE) {
				log.Printf("serve: port %d in use, trying %d", port+i, port+i+1)
				continue
			}
			return nil, err
		}
		s.addr = candidate
		return ln, nil
	}
	return nil, fmt.Errorf("no free port found in range %d-%d", port, port+maxPortAttempts-1)
}

func (s *Server) Start() error {
	ln, err := s.Listen()
	if err != nil {
		return err
	}
	return s.Serve(ln)
}

// Addr returns the actual listen address after Listen succeeds.
func (s *Server) Addr() string {
	return s.addr
}

// SetWorkDir configures the working directory used to resolve relative paths
// (currently only the upload directory fallback under <workDir>/.ocode/uploads).
// The handler is updated too so its UploadDir helper sees the same root.
func (s *Server) SetWorkDir(dir string) {
	s.workDir = dir
	if s.handler != nil {
		s.handler.SetWorkDir(dir)
	}
}

// Serve serves requests on an already-bound listener.
func (s *Server) Serve(ln net.Listener) error {
	// Populate the live model caches (OpenRouter, Novita) in the background so
	// the model-list endpoint can enrich the embedded snapshot with live models
	// without ever blocking a request on a network fetch. The Preload* helpers
	// are idempotent and degrade gracefully when the network is unavailable.
	go agent.PreloadOpenRouterModels()
	go agent.PreloadNovitaModels()
	log.Printf("serving on %s", s.addr)
	// Periodically release idle built agents so agent/plugin processes do not
	// accumulate as projects accumulate. The registry entry and on-disk
	// session survive; the agent rebuilds on the next message.
	stop := make(chan struct{})
	defer close(stop)
	go s.handler.evictIdleLoop(stop)
	// Forward new debug-log entries onto the unified event bus exactly once,
	// process-wide (never per SSE connection).
	go s.handler.logBusForwardLoop(stop)

	// Track the listener + an *http.Server so Shutdown can stop accepting new
	// connections and drain in-flight ones (graceful shutdown for desktop quit
	// and any caller that wires it up).
	s.shutdownMu.Lock()
	s.ln = ln
	s.httpServer = &http.Server{Handler: s.mux}
	s.shutdownMu.Unlock()

	return s.httpServer.Serve(ln)
}

// Shutdown gracefully stops the server within ctx. It first tears down agent
// sessions and running terminals (so in-flight work and interactive shells get
// a chance to flush/exit), then stops accepting new HTTP connections and drains
// in-flight ones. It always returns by the time ctx expires — callers pass a
// TTL context so a desktop quit never hangs.
func (s *Server) Shutdown(ctx context.Context) error {
	if s.handler != nil {
		s.handler.Shutdown(ctx)
	}
	s.shutdownMu.Lock()
	hs := s.httpServer
	ln := s.ln
	s.shutdownMu.Unlock()
	if hs != nil {
		// Stop accepting new connections and wait for in-flight requests to
		// finish (or ctx to expire). Long-lived SSE connections are released on
		// ctx expiry, after which we close the listener to force any stragglers.
		_ = hs.Shutdown(ctx)
	}
	if ln != nil {
		_ = ln.Close()
	}
	return nil
}

// evictIdleLoop runs the session-registry idle-agent eviction pass on a
// fixed interval until stop is closed (Serve return). Eviction never touches
// sessions with an active turn.
func (h *Handler) evictIdleLoop(stop <-chan struct{}) {
	if h.sessions == nil {
		return
	}
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			h.sessions.EvictIdle()
		}
	}
}

// RegisterExternalSession registers an existing TUI session with the web server
// so the web UI can stream and interact with it. Instead of creating a new agent,
// the server forwards requests through the rcCh channel to the TUI's Update loop.
// Returns the bridge so the caller can push messages into it.
//
// Part 06: the bridged TUI session becomes a first-class SessionManager entry
// (bound to projectRoot — the TUI's own workdir, passed explicitly so the
// binding never depends on the handler's process-level workdir), so the
// per-session state/status endpoints and the uniform send path resolve it
// like any other session. The bridge also re-broadcasts every frame onto the
// unified event bus, tagged at source with the real session id and project.
// An empty projectRoot falls back to the handler workdir.
func (s *Server) RegisterExternalSession(sessionID, model, projectRoot string, rcCh chan RCRequest, resolveCh chan RCResolution, token string) *RCBridge {
	s.handler.mu.Lock()
	defer s.handler.mu.Unlock()

	bridge := &RCBridge{
		RcCh:      rcCh,
		ResolveCh: resolveCh,
		Token:     token,
		SessionID: sessionID,
		Model:     model,
		publish: func(ev SSEEvent) {
			project := ""
			if e := s.handler.sessions.Lookup(ev.SessionID); e != nil {
				project = e.ProjectRoot
			}
			s.handler.bus.Publish(ev.Event, project, ev.SessionID, ev.Data)
			s.handler.sessions.SetLastSeq(ev.SessionID, s.handler.bus.LastSeq())
		},
	}
	if projectRoot == "" {
		projectRoot = s.handler.workDir
	}
	s.handler.sessions.Register(sessionID, projectRoot)
	s.handler.rc = bridge
	return bridge
}

// Run is the entry point for the `serve`/`web` subcommands. It starts an HTTP
// server bound to addr (host:port) and blocks until the listener closes.
//
// setup is an optional hook invoked once, after the Server is constructed and
// the listener is bound, but before http.Serve begins. It receives the live
// *Server so the caller can attach extensions (e.g. a scheduler.Service).
// setup may be nil.
func Run(args []string, webFS fs.FS, setup func(srv *Server) error) error {
	// Check for help flag before parsing
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			printServeUsage()
			return nil
		}
	}

	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	port := fs.Int("port", 4096, "Port to listen on")
	host := fs.String("host", "0.0.0.0", "Host to bind to")
	openBrowser := fs.Bool("open", false, "Open browser after starting")
	fs.Parse(args)

	addr := fmt.Sprintf("%s:%d", *host, *port)
	username := os.Getenv("OPENCODE_SERVER_USERNAME")
	password := os.Getenv("OPENCODE_SERVER_PASSWORD")

	srv := New(addr, username, password, webFS)
	// Anchor the server to the process working directory so relative-path
	// endpoints (file tree, file content, git, uploads) resolve against the
	// project root regardless of where the process was launched from. The
	// desktop shell passes its own workDir explicitly before serving.
	if wd, err := os.Getwd(); err == nil {
		srv.SetWorkDir(wd)
	}

	// Bind before opening the browser so the URL reflects the port actually
	// bound (Listen falls forward to a free port if the requested one is busy).
	ln, err := srv.Listen()
	if err != nil {
		return err
	}

	if setup != nil {
		if err := setup(srv); err != nil {
			return fmt.Errorf("server setup: %w", err)
		}
	}

	if *openBrowser {
		_, boundPort, _ := net.SplitHostPort(srv.addr)
		go func() {
			openURL(fmt.Sprintf("http://localhost:%s", boundPort))
		}()
	}

	log.Printf("serving on %s", srv.addr)
	return http.Serve(ln, srv.mux)
}

func openURL(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		return
	}
	cmd.Stdout = nil
	cmd.Stderr = nil
	_ = cmd.Start()
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	log.Printf("serve error: %s", msg)
	writeJSON(w, status, map[string]string{"error": msg})
}

func readBodyJSON(r *http.Request, v interface{}) error {
	return json.NewDecoder(r.Body).Decode(v)
}

func corsMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next(w, r)
	}
}

func (s *Server) WithCORS() *Server {
	original := s.mux
	wrapped := http.NewServeMux()
	wrapped.HandleFunc("/", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		original.ServeHTTP(w, r)
	}))
	s.mux = wrapped
	return s
}

// Session shims
func (s *Server) handleCompactSession(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleCompactSession(w, r, r.PathValue("id"))
}
func (s *Server) handleRecapSession(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleRecapSession(w, r, r.PathValue("id"))
}
func (s *Server) handleExportSession(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleExportSession(w, r, r.PathValue("id"))
}
func (s *Server) handleExportClaudeSession(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleExportClaudeSession(w, r, r.PathValue("id"))
}
func (s *Server) handleShareSession(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleShareSession(w, r, r.PathValue("id"))
}
func (s *Server) handleBtw(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleBtw(w, r, r.PathValue("id"))
}
func (s *Server) handleSetSessionTitle(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleSetSessionTitle(w, r, r.PathValue("id"))
}
func (s *Server) handleGenerateSessionTitle(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleGenerateSessionTitle(w, r, r.PathValue("id"))
}
func (s *Server) handleSessionContext(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleSessionContext(w, r, r.PathValue("id"))
}

// File shims
func (s *Server) handleUndo(w http.ResponseWriter, r *http.Request) { s.handler.HandleUndo(w, r) }
func (s *Server) handleRedo(w http.ResponseWriter, r *http.Request) { s.handler.HandleRedo(w, r) }
func (s *Server) handleShellCommand(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleShellCommand(w, r)
}

// TUI status shims. These read from the live RCBridge (when the web is
// attached to a TUI session) or fall back to the local handler's config when
// the server is running headless.
func (s *Server) handleGetTUIStatus(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleGetTUIStatus(w, r, s.rc())
}
func (s *Server) handleGetSpending(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleGetSpending(w, r)
}
func (s *Server) handleGetLSPStatuses(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleGetLSPStatuses(w, r, s.rc())
}
func (s *Server) handleGetModifiedFiles(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleGetModifiedFiles(w, r, s.rc())
}

// rc safely returns the handler's RCBridge (may be nil for headless /api/serve).
func (s *Server) rc() *RCBridge {
	if s.handler == nil {
		return nil
	}
	return s.handler.RCBridge()
}

// Config shims
func (s *Server) handleGetModel(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleGetModel(w, r)
}
func (s *Server) handleSetModel(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleSetModel(w, r)
}
func (s *Server) handleGetThinkingBudget(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleGetThinkingBudget(w, r)
}
func (s *Server) handleSetThinkingBudget(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleSetThinkingBudget(w, r)
}
func (s *Server) handleGetSmallModel(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleGetSmallModel(w, r)
}
func (s *Server) handleSetSmallModel(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleSetSmallModel(w, r)
}
func (s *Server) handleGetPermissionModel(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleGetPermissionModel(w, r)
}
func (s *Server) handleSetPermissionModel(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleSetPermissionModel(w, r)
}
func (s *Server) handleGetTerminalConfig(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleGetTerminalConfig(w, r)
}
func (s *Server) handleSetTerminalConfig(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleSetTerminalConfig(w, r)
}
func (s *Server) handleGetRecapConfig(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleGetRecapConfig(w, r)
}
func (s *Server) handleSetRecapConfig(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleSetRecapConfig(w, r)
}
func (s *Server) handleGetCommitMsgConfig(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleGetCommitMsgConfig(w, r)
}
func (s *Server) handleSetCommitMsgConfig(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleSetCommitMsgConfig(w, r)
}
func (s *Server) handleGetCompactConfig(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleGetCompactConfig(w, r)
}
func (s *Server) handleSetCompactConfig(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleSetCompactConfig(w, r)
}
func (s *Server) handleGetAutoPermissionConfig(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleGetAutoPermissionConfig(w, r)
}
func (s *Server) handleSetAutoPermissionConfig(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleSetAutoPermissionConfig(w, r)
}
func (s *Server) handleGetDiscoveryConfig(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleGetDiscoveryConfig(w, r)
}
func (s *Server) handleSetDiscoveryConfig(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleSetDiscoveryConfig(w, r)
}
func (s *Server) handleGetTUIConfigSection(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleGetTUIConfigSection(w, r)
}
func (s *Server) handleSetTUIConfigSection(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleSetTUIConfigSection(w, r)
}
func (s *Server) handleGetEditorConfig(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleGetEditorConfig(w, r)
}
func (s *Server) handleSetEditorConfig(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleSetEditorConfig(w, r)
}
func (s *Server) handleGetImageGenConfig(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleGetImageGenConfig(w, r)
}
func (s *Server) handleSetImageGenConfig(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleSetImageGenConfig(w, r)
}
func (s *Server) handleGetPathsConfig(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleGetPathsConfig(w, r)
}
func (s *Server) handleSetPathsConfig(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleSetPathsConfig(w, r)
}
func (s *Server) handleGetPathsInfo(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleGetPathsInfo(w, r)
}
func (s *Server) handleGetMemoryStatus(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleGetMemoryStatus(w, r)
}
func (s *Server) handleDocsStatus(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleDocsStatus(w, r)
}
func (s *Server) handleDocsInit(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleDocsInit(w, r)
}
func (s *Server) handleDocsUpdate(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleDocsUpdate(w, r)
}
func (s *Server) handleDocsCleanup(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleDocsCleanup(w, r)
}
func (s *Server) handleConnectProvider(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleConnectProvider(w, r)
}
func (s *Server) handleGetAutoContinue(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleGetAutoContinue(w, r)
}
func (s *Server) handleSetAutoContinue(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleSetAutoContinue(w, r)
}
func (s *Server) handleSetBashRule(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleSetBashRule(w, r)
}
func (s *Server) handleGetLimitsConfig(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleGetLimitsConfig(w, r)
}
func (s *Server) handleSetLimitsConfig(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleSetLimitsConfig(w, r)
}
func (s *Server) handleGetFeaturesConfig(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleGetFeaturesConfig(w, r)
}
func (s *Server) handleSetFeaturesConfig(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleSetFeaturesConfig(w, r)
}
func (s *Server) handleGetProfileDebugConfig(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleGetProfileDebugConfig(w, r)
}
func (s *Server) handleSetProfileDebugConfig(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleSetProfileDebugConfig(w, r)
}
func (s *Server) handleGetPluginsEnabledConfig(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleGetPluginsEnabledConfig(w, r)
}
func (s *Server) handleSetPluginsEnabledConfig(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleSetPluginsEnabledConfig(w, r)
}
func (s *Server) handleGetLocalModelsConfig(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleGetLocalModelsConfig(w, r)
}
func (s *Server) handleSetLocalModelsConfig(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleSetLocalModelsConfig(w, r)
}
func (s *Server) handleTerminalWS(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleTerminalWS(w, r)
}
func (s *Server) handleTerminalProcesses(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleTerminalProcesses(w, r)
}
func (s *Server) handleGetAdvisor(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleGetAdvisor(w, r)
}
func (s *Server) handleSetAdvisor(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleSetAdvisor(w, r)
}
func (s *Server) handleGetAdvisorEnabled(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleGetAdvisorEnabled(w, r)
}
func (s *Server) handleSetAdvisorEnabled(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleSetAdvisorEnabled(w, r)
}
func (s *Server) handleGetOcrEnabled(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleGetOcrEnabled(w, r)
}
func (s *Server) handleSetOcrEnabled(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleSetOcrEnabled(w, r)
}
func (s *Server) handleSetOcrModel(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleSetOcrModel(w, r)
}
func (s *Server) handleGetOcrConfig(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleGetOcrConfig(w, r)
}
func (s *Server) handleSetOcrConfig(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleSetOcrConfig(w, r)
}
func (s *Server) handleGetOcrModels(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleGetOcrModels(w, r)
}

func (s *Server) handleGetMaskConfig(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleGetMaskConfig(w, r)
}
func (s *Server) handleSetMaskEnabled(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleSetMaskEnabled(w, r)
}
func (s *Server) handleSetMaskMode(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleSetMaskMode(w, r)
}
func (s *Server) handleSetMaskModel(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleSetMaskModel(w, r)
}
func (s *Server) handleSetMaskAdvanced(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleSetMaskAdvanced(w, r)
}
func (s *Server) handleListAgents(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleListAgents(w, r)
}
func (s *Server) handleSetAgent(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleSetAgent(w, r)
}

// Permissions shims
func (s *Server) handleGetPermissions(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleGetPermissions(w, r)
}
func (s *Server) handleSetPermission(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleSetPermission(w, r)
}
func (s *Server) handleGetYolo(w http.ResponseWriter, r *http.Request) { s.handler.HandleGetYolo(w, r) }
func (s *Server) handleSetYolo(w http.ResponseWriter, r *http.Request) { s.handler.HandleSetYolo(w, r) }
func (s *Server) handleAnswerQuestion(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleAnswerQuestion(w, r)
}
func (s *Server) handleResolvePermission(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleResolvePermission(w, r)
}

// MCP shims
func (s *Server) handleListMCP(w http.ResponseWriter, r *http.Request) { s.handler.HandleListMCP(w, r) }
func (s *Server) handleEnableMCP(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleSetMCPEnabled(w, r, r.PathValue("name"), true)
}
func (s *Server) handleDisableMCP(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleSetMCPEnabled(w, r, r.PathValue("name"), false)
}

// Plugin shims
func (s *Server) handleListPlugins(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleListPlugins(w, r)
}
func (s *Server) handleGetPlugin(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleGetPlugin(w, r, r.PathValue("name"))
}
func (s *Server) handleEnablePlugin(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleSetPluginEnabled(w, r, r.PathValue("name"), true)
}
func (s *Server) handleDisablePlugin(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleSetPluginEnabled(w, r, r.PathValue("name"), false)
}
func (s *Server) handleInstallPlugin(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleInstallPlugin(w, r)
}
func (s *Server) handleRemovePlugin(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleRemovePlugin(w, r, r.PathValue("name"))
}

// Usage shims
func (s *Server) handleGetUsage(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleGetUsage(w, r)
}

// Agent run shims
func (s *Server) handleListRuns(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleListRuns(w, r)
}
func (s *Server) handleRunsStream(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleRunsStream(w, r)
}

// Changes tab shims
func (s *Server) handleListChanges(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleListChanges(w, r)
}
func (s *Server) handleChangesDiff(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleChangesDiff(w, r)
}
func (s *Server) handleUndoChangeFile(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleUndoChangeFile(w, r)
}
func (s *Server) handleUndoChangeBlock(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleUndoChangeBlock(w, r)
}

// Log shims
func (s *Server) handleGetLogs(w http.ResponseWriter, r *http.Request) { s.handler.HandleGetLogs(w, r) }
func (s *Server) handleLogStream(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleLogStream(w, r)
}
func (s *Server) handleClearLogs(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleClearLogs(w, r)
}

// Info shims
func (s *Server) handleListSkills(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleListSkills(w, r)
}
func (s *Server) handleListCommands(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleListCommands(w, r)
}
func (s *Server) handleCommandContext(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleCommandContext(w, r, r.PathValue("name"))
}
func (s *Server) handleGitHubPR(w http.ResponseWriter, r *http.Request) {
	owner, repo, number, ok := parseGitHubPRRoute(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid github PR path")
		return
	}
	s.handler.HandleGitHubPR(w, r, owner, repo, number)
}
func (s *Server) handleGitHubIssues(w http.ResponseWriter, r *http.Request) {
	owner := r.PathValue("owner")
	repo := r.PathValue("repo")
	if owner == "" || repo == "" {
		writeError(w, http.StatusBadRequest, "owner and repo are required")
		return
	}
	s.handler.HandleGitHubIssues(w, r, owner, repo)
}
func (s *Server) handleInit(w http.ResponseWriter, r *http.Request) { s.handler.HandleInit(w, r) }

func (s *Server) handleListProjects(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleListProjects(w, r)
}
func (s *Server) handleGetCurrentProject(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleGetCurrentProject(w, r)
}
func (s *Server) handleAddProject(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleAddProject(w, r)
}
func (s *Server) handleRemoveProject(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleRemoveProject(w, r)
}
func (s *Server) handleListProjectSessions(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleListProjectSessions(w, r)
}
func (s *Server) handleRenameProject(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleRenameProject(w, r)
}
func (s *Server) handleReorderProjects(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleReorderProjects(w, r)
}
func (s *Server) handleSetProjectGroup(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleSetProjectGroup(w, r)
}
func (s *Server) handleListGroups(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleListGroups(w, r)
}
func (s *Server) handleCreateGroup(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleCreateGroup(w, r)
}
func (s *Server) handleDeleteGroup(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleDeleteGroup(w, r)
}
func (s *Server) handleRenameGroup(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleRenameGroup(w, r)
}
func (s *Server) handleReorderGroups(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleReorderGroups(w, r)
}
func (s *Server) handleSetGroupCollapsed(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleSetGroupCollapsed(w, r)
}
func (s *Server) handleGetTabs(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleGetTabs(w, r)
}
func (s *Server) handleSetTabs(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleSetTabs(w, r)
}
func (s *Server) handleBrowseDirectory(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleBrowseDirectory(w, r)
}

func (s *Server) handleGetMonacoSettings(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleGetMonacoSettings(w, r)
}
func (s *Server) handleSetMonacoSettings(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleSetMonacoSettings(w, r)
}
func (s *Server) handleListMonacoExtensions(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleListMonacoExtensions(w, r)
}
func (s *Server) handleToggleMonacoExtension(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleToggleMonacoExtension(w, r)
}

type ChatRequest struct {
	Content   string `json:"content"`
	SessionID string `json:"sessionId,omitempty"`
	Model     string `json:"model,omitempty"`
	RequestID string `json:"request_id,omitempty"`
	// ProjectPath binds the session to a project root (multi-project). Empty
	// falls back to the server's own workdir.
	ProjectPath string `json:"project_path,omitempty"`
	// WindowID binds the session to a desktop window for per-window profile
	// isolation. First value wins; later non-empty values update the binding.
	WindowID string `json:"windowId,omitempty"`
	// Async, when set, makes the endpoint acknowledge with 202 as soon as the
	// turn is dispatched instead of holding the HTTP connection open until the
	// agent finishes. The web UI sets it: a browser allows only six concurrent
	// connections per origin over HTTP/1.1, so a connection pinned for every
	// running turn starves the other sessions' requests and makes them look
	// stuck. All turn output reaches the browser over the SSE mirror.
	Async bool `json:"async,omitempty"`
}

type ChatResponse struct {
	Content   string `json:"content"`
	SessionID string `json:"sessionId"`
	Model     string `json:"model"`
}

type SessionInfo struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type SessionListResponse struct {
	Sessions []SessionInfo `json:"sessions"`
	Total    int           `json:"total"`
}

type SessionDetail struct {
	SessionInfo
	Messages []agent.Message `json:"messages"`
	Total    int             `json:"total"`
}

type ModelInfo struct {
	Name     string `json:"name"`
	Model    string `json:"model"`
	Provider string `json:"provider"`
	Active   bool   `json:"active"`
	// DisplayName is the human-readable name from the models.dev registry
	// (e.g. "Ox Alpha Free" for the zen codename "opencode/x-preview-f-free").
	// Empty when the registry doesn't know the model — UIs fall back to the
	// raw id in that case.
	DisplayName string `json:"display_name,omitempty"`
	// Favorite and Recent mirror the TUI model picker's priority sections
	// (★ Favorites / Recently Used): the web and desktop selectors surface
	// these first, sorted exactly like the TUI.
	Favorite bool `json:"favorite,omitempty"`
	Recent   bool `json:"recent,omitempty"`
}

func printServeUsage() {
	fmt.Println("Usage: ocode serve [options]")
	fmt.Println()
	fmt.Println("Start the HTTP server with web UI.")
	fmt.Println()
	fmt.Println("Options:")
	fmt.Println("  -port <port>    Port to listen on (default: 4096)")
	fmt.Println("  -host <host>    Host to bind to (default: 0.0.0.0)")
	fmt.Println("  -open           Open browser after starting")
	fmt.Println("  -h, --help      Show this help message")
	fmt.Println()
	fmt.Println("Environment Variables:")
	fmt.Println("  OPENCODE_SERVER_USERNAME    Basic auth username")
	fmt.Println("  OPENCODE_SERVER_PASSWORD    Basic auth password")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  ocode serve")
	fmt.Println("  ocode serve -port 8080 -open")
	fmt.Println("  ocode serve -host 127.0.0.1 -port 3000")
}
