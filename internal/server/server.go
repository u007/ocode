package server

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/u007/ocode/internal/agent"
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
	addr     string
	username string
	password string
	rl       *rateLimiter
	mux      *http.ServeMux
	handler  *Handler
	webFS    fs.FS
}

func New(addr, username, password string, webFS fs.FS) *Server {
	mux := http.NewServeMux()
	h := NewHandler()
	s := &Server{
		addr:     addr,
		username: username,
		password: password,
		rl:       newRateLimiter(),
		mux:      mux,
		handler:  h,
		webFS:    webFS,
	}
	s.registerRoutes()
	return s
}

func (s *Server) registerRoutes() {
	s.mux.HandleFunc("POST /api/chat", s.authMiddleware(s.handleChat))
	s.mux.HandleFunc("GET /api/chat/stream", s.authMiddleware(s.handleChatStream))
	s.mux.HandleFunc("GET /api/chat/messages", s.authMiddleware(s.handleSessionMessages))
	s.mux.HandleFunc("GET /api/sessions", s.authMiddleware(s.handleListSessions))
	s.mux.HandleFunc("GET /api/sessions/{id}", s.authMiddleware(s.handleGetSession))
	s.mux.HandleFunc("POST /api/sessions/{id}/message", s.authMiddleware(s.handleSendMessage))
	s.mux.HandleFunc("GET /api/models", s.authMiddleware(s.handleListModels))
	s.mux.HandleFunc("GET /api/agents/runs", s.authMiddleware(s.handleListRuns))
	s.mux.HandleFunc("GET /api/agents/runs/stream", s.authMiddleware(s.handleRunsStream))
	s.mux.HandleFunc("GET /api/git/status", s.authMiddleware(s.handleGitStatus))
	s.mux.HandleFunc("GET /api/git/diff", s.authMiddleware(s.handleGitDiff))
	s.mux.HandleFunc("GET /api/theme", s.authMiddleware(s.handleGetTheme))
	s.mux.HandleFunc("GET /api/files/tree", s.authMiddleware(s.handleFileTree))
	s.mux.HandleFunc("GET /api/files/content", s.authMiddleware(s.handleFileContent))
	s.mux.HandleFunc("POST /api/files/open", s.authMiddleware(s.handleOpenFile))

	// Session operations
	s.mux.HandleFunc("POST /api/sessions/{id}/compact", s.authMiddleware(s.handleCompactSession))
	s.mux.HandleFunc("GET /api/sessions/{id}/recap", s.authMiddleware(s.handleRecapSession))
	s.mux.HandleFunc("GET /api/sessions/{id}/export", s.authMiddleware(s.handleExportSession))
	s.mux.HandleFunc("GET /api/sessions/{id}/export-claude", s.authMiddleware(s.handleExportClaudeSession))
	s.mux.HandleFunc("GET /api/sessions/{id}/share", s.authMiddleware(s.handleShareSession))
	s.mux.HandleFunc("PUT /api/sessions/{id}/title", s.authMiddleware(s.handleSetSessionTitle))
	s.mux.HandleFunc("GET /api/sessions/{id}/context", s.authMiddleware(s.handleSessionContext))

	// Files
	s.mux.HandleFunc("POST /api/files/undo", s.authMiddleware(s.handleUndo))
	s.mux.HandleFunc("POST /api/files/redo", s.authMiddleware(s.handleRedo))

	// Config
	s.mux.HandleFunc("GET /api/config/model", s.authMiddleware(s.handleGetModel))
	s.mux.HandleFunc("PUT /api/config/model", s.authMiddleware(s.handleSetModel))
	s.mux.HandleFunc("GET /api/config/small-model", s.authMiddleware(s.handleGetSmallModel))
	s.mux.HandleFunc("PUT /api/config/small-model", s.authMiddleware(s.handleSetSmallModel))
	s.mux.HandleFunc("GET /api/config/advisor", s.authMiddleware(s.handleGetAdvisor))
	s.mux.HandleFunc("PUT /api/config/advisor", s.authMiddleware(s.handleSetAdvisor))
	s.mux.HandleFunc("GET /api/config/advisor-enabled", s.authMiddleware(s.handleGetAdvisorEnabled))
	s.mux.HandleFunc("PUT /api/config/advisor-enabled", s.authMiddleware(s.handleSetAdvisorEnabled))
	s.mux.HandleFunc("GET /api/config/agents", s.authMiddleware(s.handleListAgents))
	s.mux.HandleFunc("PUT /api/config/agent", s.authMiddleware(s.handleSetAgent))

	// Permissions
	s.mux.HandleFunc("GET /api/permissions", s.authMiddleware(s.handleGetPermissions))
	s.mux.HandleFunc("POST /api/permissions", s.authMiddleware(s.handleSetPermission))
	s.mux.HandleFunc("GET /api/permissions/yolo", s.authMiddleware(s.handleGetYolo))
	s.mux.HandleFunc("PUT /api/permissions/yolo", s.authMiddleware(s.handleSetYolo))

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
	s.mux.HandleFunc("GET /api/github/pr/{owner}/{repo}/{number}", s.authMiddleware(s.handleGitHubPR))
	s.mux.HandleFunc("GET /api/github/issues/{owner}/{repo}", s.authMiddleware(s.handleGitHubIssues))
	s.mux.HandleFunc("POST /api/init", s.authMiddleware(s.handleInit))

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

func (s *Server) handleFileTree(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleFileTree(w, r)
}

func (s *Server) handleFileContent(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleFileContent(w, r)
}

func (s *Server) handleOpenFile(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleOpenFile(w, r)
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

// Serve serves requests on an already-bound listener.
func (s *Server) Serve(ln net.Listener) error {
	log.Printf("serving on %s", s.addr)
	return http.Serve(ln, s.mux)
}

// RegisterExternalSession registers an existing TUI session with the web server
// so the web UI can stream and interact with it. Instead of creating a new agent,
// the server forwards requests through the rcCh channel to the TUI's Update loop.
// Returns the bridge so the caller can push messages into it.
func (s *Server) RegisterExternalSession(sessionID, model string, rcCh chan RCRequest) *RCBridge {
	s.handler.mu.Lock()
	defer s.handler.mu.Unlock()

	bridge := &RCBridge{
		RcCh:      rcCh,
		SessionID: sessionID,
		Model:     model,
	}
	s.handler.rc = bridge
	return bridge
}

func Run(args []string, webFS fs.FS) error {
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

	// Bind before opening the browser so the URL reflects the port actually
	// bound (Listen falls forward to a free port if the requested one is busy).
	ln, err := srv.Listen()
	if err != nil {
		return err
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
func (s *Server) handleSetSessionTitle(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleSetSessionTitle(w, r, r.PathValue("id"))
}
func (s *Server) handleSessionContext(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleSessionContext(w, r, r.PathValue("id"))
}

// File shims
func (s *Server) handleUndo(w http.ResponseWriter, r *http.Request) { s.handler.HandleUndo(w, r) }
func (s *Server) handleRedo(w http.ResponseWriter, r *http.Request) { s.handler.HandleRedo(w, r) }

// Config shims
func (s *Server) handleGetModel(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleGetModel(w, r)
}
func (s *Server) handleSetModel(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleSetModel(w, r)
}
func (s *Server) handleGetSmallModel(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleGetSmallModel(w, r)
}
func (s *Server) handleSetSmallModel(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleSetSmallModel(w, r)
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

type ChatRequest struct {
	Content   string `json:"content"`
	SessionID string `json:"sessionId,omitempty"`
	Model     string `json:"model,omitempty"`
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

type SessionDetail struct {
	SessionInfo
	Messages []agent.Message `json:"messages"`
}

type ModelInfo struct {
	Name     string `json:"name"`
	Model    string `json:"model"`
	Provider string `json:"provider"`
	Active   bool   `json:"active"`
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
