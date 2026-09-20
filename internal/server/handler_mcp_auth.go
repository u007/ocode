package server

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/u007/ocode/internal/auth"
	"github.com/u007/ocode/internal/config"
)

// The MCP-auth endpoint backs the web/desktop `/mcp-auth <server>` command.
//
// Why it exists: `/mcp-auth` used to be documented as TUI-only on the grounds
// that the OAuth flow needs a system-browser launch plus a localhost callback
// (127.0.0.1:8085). That reasoning is wrong for the DESKTOP app, whose server
// runs on the same machine — the browser launches fine and the callback lands.
// It is only genuinely impossible for a REMOTE web client, whose server is
// another host: the browser would open on the CLIENT machine and the callback
// would go to the client's own 8085, never the server's.
//
// Consequently the endpoint is gated to loopback callers. A request arriving
// through the remote proxy (or any non-loopback client) is refused, because
// the flow would either hang for two minutes or send the user's browser to a
// callback the server never receives.
//
// MCPAuthFlow blocks for up to its own 2-minute timeout, so this mirrors the
// `/api/cli-tools` shape: start returns a job id, the client polls status.

// mcpAuthJobManager tracks background auth flows. Terminal jobs are pruned on
// access so a long-lived process cannot accumulate them.
type mcpAuthJobManager struct {
	mu   sync.Mutex
	jobs map[string]*mcpAuthJob
}

const (
	// mcpAuthJobTTL is how long a finished job stays queryable. The flow's own
	// timeout is 2 minutes; a few minutes of retention lets a slow client
	// observe the outcome.
	mcpAuthJobTTL = 5 * time.Minute
	// mcpAuthJobMax bounds the map even if a client never polls.
	mcpAuthJobMax = 16
)

type mcpAuthJob struct {
	id        string
	server    string
	started   time.Time
	completed time.Time // zero while running
	status    string    // running | done | error
	errText   string
}

func newMCPAuthJobManager() *mcpAuthJobManager {
	return &mcpAuthJobManager{jobs: make(map[string]*mcpAuthJob)}
}

func newMCPAuthJobID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// pruneLocked drops terminal jobs past their TTL, then, if the map is still at
// its cap, the oldest terminal jobs — never a running one. Caller holds m.mu.
func (m *mcpAuthJobManager) pruneLocked(now time.Time) {
	for id, j := range m.jobs {
		if j.status != "running" && !j.completed.IsZero() && now.Sub(j.completed) > mcpAuthJobTTL {
			delete(m.jobs, id)
		}
	}
	for len(m.jobs) >= mcpAuthJobMax {
		oldestID := ""
		var oldest time.Time
		for id, j := range m.jobs {
			if j.status == "running" {
				continue
			}
			if oldestID == "" || j.started.Before(oldest) {
				oldestID, oldest = id, j.started
			}
		}
		if oldestID == "" {
			return // everything is running; refuse to evict live work
		}
		delete(m.jobs, oldestID)
	}
}

// start launches the OAuth flow for serverName in the background and returns
// its job id. It cannot fail: the caller validates that the server exists and
// has a usable OAuth config (answering 4xx) before calling, and any error from
// the flow itself is published on the job's status, not returned here.
func (m *mcpAuthJobManager) start(serverName string, oauth *mcpAuthConfig) string {
	m.mu.Lock()
	m.pruneLocked(time.Now())
	id := newMCPAuthJobID()
	job := &mcpAuthJob{id: id, server: serverName, started: time.Now(), status: "running"}
	m.jobs[id] = job
	m.mu.Unlock()

	// A remote MCP OAuth flow can also be rejected because another server's
	// listener already holds 8085 (MCPAuthFlow binds a fixed port). Surface
	// that as a job error rather than swallowing it.
	go func() {
		err := auth.MCPAuthFlow(serverName, oauth.AuthorizationURL, oauth.TokenURL, oauth.ClientID, oauth.Scopes)
		m.mu.Lock()
		defer m.mu.Unlock()
		// Pruned while we were authenticating: not addressable, publish nothing.
		if _, live := m.jobs[id]; !live {
			return
		}
		if err != nil {
			job.errText = err.Error()
			job.status = "error"
		} else {
			job.status = "done"
		}
		job.completed = time.Now()
	}()
	return id
}

func (m *mcpAuthJobManager) get(id string) *mcpAuthJob {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pruneLocked(time.Now())
	j, ok := m.jobs[id]
	if !ok {
		return nil
	}
	// Copy so the caller reads a stable snapshot after the lock is released.
	cp := *j
	return &cp
}

// mcpAuthConfig is the subset of config.MCPOAuthConfig the flow needs. Declared
// here so this file does not leak the config type into the wire contract.
type mcpAuthConfig struct {
	AuthorizationURL string
	TokenURL         string
	ClientID         string
	Scopes           []string
}

// mcpAuthStartResponse is returned by POST /api/mcp/{name}/auth.
type mcpAuthStartResponse struct {
	JobID    string `json:"job_id"`
	Server   string `json:"server"`
	Status   string `json:"status"` // always "running" on 202
	Browsers string `json:"browser_note,omitempty"`
}

// mcpAuthStatusResponse is returned by GET /api/mcp/auth/{id}.
type mcpAuthStatusResponse struct {
	JobID  string `json:"job_id"`
	Server string `json:"server"`
	Status string `json:"status"` // running | done | error
	Error  string `json:"error,omitempty"`
}

// isLoopbackRequest reports whether the request's peer is loopback.
//
// Deliberately checks RemoteAddr rather than the Host header or X-Forwarded-*
// headers: a client on another machine must not be able to claim loopback.
// Note this only covers requests that reach THIS server directly — a request
// proxied to a remote host is refused earlier (see HandleRemoteProxy), so the
// remote server's loopback check is a backstop, not the primary guard.
func isLoopbackRequest(r *http.Request) bool {
	return isLoopbackName(realIP(r))
}

// HandleStartMCPAuth serves POST /api/mcp/{name}/auth. It validates the server
// and its OAuth config, then starts the flow in the background.
func (h *Handler) HandleStartMCPAuth(w http.ResponseWriter, r *http.Request, name string) {
	if !isLoopbackRequest(r) {
		writeError(w, http.StatusForbidden,
			"MCP OAuth must run where the browser can reach the callback (127.0.0.1:8085). "+
				"Open the ocode desktop app, or run /mcp-auth in the TUI on this host.")
		return
	}
	if h.mcpAuthJobs == nil {
		writeError(w, http.StatusServiceUnavailable, "mcp auth not available")
		return
	}

	h.mu.Lock()
	var cfg *mcpAuthConfig
	var why string
	if h.cfg == nil {
		why = "config not loaded"
	} else if mc, ok := h.cfg.MCP[name]; !ok {
		why = fmt.Sprintf("mcp server %q not found", name)
	} else if mc.Type != "" && mc.Type != "remote" {
		why = fmt.Sprintf("OAuth only supported for remote servers (%q is %s)", name, mc.Type)
	} else if !mcpOAuthEnabled(mc.OAuth) {
		why = fmt.Sprintf("OAuth not configured for server %q", name)
	} else {
		cfg = &mcpAuthConfig{
			AuthorizationURL: mc.OAuth.AuthorizationURL,
			TokenURL:         mc.OAuth.TokenURL,
			ClientID:         mc.OAuth.ClientID,
			Scopes:           mc.OAuth.Scopes,
		}
	}
	h.mu.Unlock()

	if cfg == nil {
		writeError(w, http.StatusBadRequest, why)
		return
	}

	jobID := h.mcpAuthJobs.start(name, cfg)
	writeJSON(w, http.StatusAccepted, mcpAuthStartResponse{
		JobID:    jobID,
		Server:   name,
		Status:   "running",
		Browsers: "A browser window will open on the machine running ocode.",
	})
}

// HandleGetMCPAuthStatus serves GET /api/mcp/auth/{id}.
//
// Gated to loopback callers like HandleStartMCPAuth: a flow can only be started
// from loopback (or proxied WSL), so a non-loopback caller has no legitimate
// job to poll. Job ids are 16 random bytes and the route is authenticated, but
// matching the start gate keeps the whole flow reachable from exactly the same
// peers and closes the mismatch the review flagged.
func (h *Handler) HandleGetMCPAuthStatus(w http.ResponseWriter, r *http.Request) {
	if !isLoopbackRequest(r) {
		writeError(w, http.StatusForbidden,
			"MCP OAuth must run where the browser can reach the callback (127.0.0.1:8085). "+
				"Open the ocode desktop app, or run /mcp-auth in the TUI on this host.")
		return
	}
	id := r.PathValue("id")
	if h.mcpAuthJobs == nil {
		writeError(w, http.StatusServiceUnavailable, "mcp auth not available")
		return
	}
	job := h.mcpAuthJobs.get(id)
	if job == nil {
		writeError(w, http.StatusNotFound, "unknown auth job")
		return
	}
	writeJSON(w, http.StatusOK, mcpAuthStatusResponse{
		JobID:  job.id,
		Server: job.server,
		Status: job.status,
		Error:  job.errText,
	})
}

// mcpOAuthEnabled mirrors mcpcli.isOAuthEnabled for the server package: when the
// config has no explicit `enabled` flag, OAuth is considered configured only if
// all three required endpoints are present.
func mcpOAuthEnabled(o *config.MCPOAuthConfig) bool {
	if o == nil {
		return false
	}
	if o.Enabled != nil {
		return *o.Enabled
	}
	return o.AuthorizationURL != "" && o.TokenURL != "" && o.ClientID != ""
}
