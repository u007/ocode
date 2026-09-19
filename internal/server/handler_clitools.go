package server

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"runtime"
	"sync"
	"time"

	"github.com/u007/ocode/internal/plugins/clitools"
)

// The CLI-tools endpoints back the web/desktop `/tools` command (the TUI's
// `/tools`, alias `/tool`). Detection is a cheap PATH probe and runs
// synchronously; installation shells out to the platform package manager and
// can legitimately block for minutes (see clitools.installTimeout), so it runs
// in the background and the client polls a job id. `clitools.Install` exposes
// no progress callback, so a polled status ("running" → "done"/"error") is the
// honest contract; there is nothing finer to stream.
//
// Remote projects need no special casing: the SPA routes these through
// /api/remote/<host>/api/cli-tools*, so the probe and install run on the
// remote's own ocode server against the remote host's PATH — which is exactly
// where the binaries are needed.

// Test seams. The real functions shell out to the platform package manager
// (install can take minutes and mutate the host), so tests swap these instead
// of running brew/apt. Package-level vars mirror the codebase's existing seam
// pattern (e.g. prepareLocalBuildFn, notifyGitAction).
var (
	cliToolDetectAllFn = clitools.DetectAll
	cliToolManagerFn   = clitools.Manager
	cliToolInstallFn   = clitools.Install
)

// cliToolsResponse is the payload for GET /api/cli-tools.
type cliToolsResponse struct {
	Platform       string `json:"platform"`
	PackageManager string `json:"package_manager"`
	// ManagerHint is the install-a-package-manager remediation shown only when
	// no supported manager is on PATH (PackageManager == "").
	ManagerHint string          `json:"manager_hint,omitempty"`
	Tools       []cliToolStatus `json:"tools"`
}

// cliToolStatus is one catalog entry's probe result. The fields mirror
// clitools.ToolStatus but are shaped here so the wire contract is explicit
// (clitools.Tool's install recipes are unexported and must not leak).
type cliToolStatus struct {
	Name        string   `json:"name"`
	Aliases     []string `json:"aliases,omitempty"`
	Description string   `json:"description"`
	Project     string   `json:"project,omitempty"`
	Found       bool     `json:"found"`
	// Command is the resolved binary name (Name or an alias); empty when
	// Found is false.
	Command string `json:"command,omitempty"`
}

// cliToolInstallStartResponse is returned by POST /api/cli-tools/install.
type cliToolInstallStartResponse struct {
	JobID  string `json:"job_id"`
	Tool   string `json:"tool"`
	Status string `json:"status"` // always "running" on 202
}

// cliToolInstallStatusResponse is returned by GET /api/cli-tools/install/{id}.
type cliToolInstallStatusResponse struct {
	JobID     string `json:"job_id"`
	Tool      string `json:"tool"`
	Status    string `json:"status"` // running | done | error
	Manager   string `json:"manager,omitempty"`
	Output    string `json:"output,omitempty"`
	Error     string `json:"error,omitempty"`
	NoManager bool   `json:"no_manager,omitempty"`
	// Hint is the package-manager remediation when NoManager is true.
	Hint string `json:"hint,omitempty"`
}

// cliToolJobManager tracks background installs. Terminal jobs are pruned on
// access so a long-lived process cannot accumulate them.
type cliToolJobManager struct {
	mu   sync.Mutex
	jobs map[string]*cliToolJob
}

const (
	// cliToolJobTTL is how long a finished job stays queryable. Clients poll
	// until terminal and stop, so a few minutes is ample; the bound keeps the
	// map from growing without limit.
	cliToolJobTTL = 10 * time.Minute
	// cliToolJobMax bounds the map even if a client never polls a started job.
	cliToolJobMax = 64
)

type cliToolJob struct {
	id        string
	tool      string
	started   time.Time
	completed time.Time // zero while running
	// status is one of "running", "done", "error".
	status    string
	manager   string
	output    string
	errText   string
	noManager bool
}

func newCLIToolJobManager() *cliToolJobManager {
	return &cliToolJobManager{jobs: make(map[string]*cliToolJob)}
}

func newCLIToolJobID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// pruneLocked drops terminal jobs past their TTL, then, if the map is still at
// its cap, the oldest terminal jobs — never a running one. Caller holds m.mu.
func (m *cliToolJobManager) pruneLocked(now time.Time) {
	for id, j := range m.jobs {
		if j.status != "running" && !j.completed.IsZero() && now.Sub(j.completed) > cliToolJobTTL {
			delete(m.jobs, id)
		}
	}
	for len(m.jobs) >= cliToolJobMax {
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
			// Everything is running; refuse to evict live work.
			return
		}
		delete(m.jobs, oldestID)
	}
}

// start launches the install for toolName in the background and returns its
// job id. The bool reports whether toolName is in the catalog at all, so the
// caller can answer 404 without racing the job.
func (m *cliToolJobManager) start(toolName string) (string, bool) {
	if _, ok := clitools.FindTool(toolName); !ok {
		return "", false
	}
	m.mu.Lock()
	m.pruneLocked(time.Now())
	id := newCLIToolJobID()
	job := &cliToolJob{id: id, tool: toolName, started: time.Now(), status: "running"}
	m.jobs[id] = job
	m.mu.Unlock()

	go func() {
		res := cliToolInstallFn(toolName)
		m.mu.Lock()
		// pruned while we were installing: the job is no longer addressable,
		// so publish nothing (a client polling it already got 404 and stopped).
		if _, live := m.jobs[id]; !live {
			m.mu.Unlock()
			return
		}
		job.manager = string(res.Manager)
		job.output = res.Output
		job.noManager = res.NoManager
		if res.Err != nil {
			job.errText = res.Err.Error()
			job.status = "error"
		} else {
			job.status = "done"
		}
		job.completed = time.Now()
		m.mu.Unlock()
	}()
	return id, true
}

// get returns a consistent copy of the job's state, or nil when unknown
// (never started, or already pruned).
func (m *cliToolJobManager) get(id string) *cliToolInstallStatusResponse {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pruneLocked(time.Now())
	j, ok := m.jobs[id]
	if !ok {
		return nil
	}
	resp := &cliToolInstallStatusResponse{
		JobID:     j.id,
		Tool:      j.tool,
		Status:    j.status,
		Manager:   j.manager,
		Output:    j.output,
		Error:     j.errText,
		NoManager: j.noManager,
	}
	if j.noManager {
		resp.Hint = clitools.MissingManagerHint()
	}
	return resp
}

// HandleListCliTools handles GET /api/cli-tools: probe every catalog tool on
// this server's PATH and report the platform + package manager.
func (h *Handler) HandleListCliTools(w http.ResponseWriter, r *http.Request) {
	statuses := cliToolDetectAllFn()
	tools := make([]cliToolStatus, 0, len(statuses))
	for _, st := range statuses {
		tools = append(tools, cliToolStatus{
			Name:        st.Tool.Name,
			Aliases:     st.Tool.Aliases,
			Description: st.Tool.Description,
			Project:     st.Tool.Project,
			Found:       st.Found,
			Command:     st.Command,
		})
	}
	resp := cliToolsResponse{
		Platform:       runtime.GOOS,
		PackageManager: string(cliToolManagerFn()),
		Tools:          tools,
	}
	if resp.PackageManager == "" {
		resp.ManagerHint = clitools.MissingManagerHint()
	}
	writeJSON(w, http.StatusOK, resp)
}

// HandleStartCliToolsInstall handles POST /api/cli-tools/install with body
// {"tool":"rg"}. Unknown tools 404; an install already running for the same
// tool is reported as such.
func (h *Handler) HandleStartCliToolsInstall(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Tool string `json:"tool"`
	}
	if err := readBodyJSON(r, &req); err != nil || req.Tool == "" {
		writeError(w, http.StatusBadRequest, "tool is required")
		return
	}
	if h.cliToolJobs == nil {
		writeError(w, http.StatusServiceUnavailable, "cli tool installer not available")
		return
	}
	jobID, ok := h.cliToolJobs.start(req.Tool)
	if !ok {
		writeError(w, http.StatusNotFound, "unknown tool")
		return
	}
	writeJSON(w, http.StatusAccepted, cliToolInstallStartResponse{
		JobID:  jobID,
		Tool:   req.Tool,
		Status: "running",
	})
}

// HandleGetCliToolsInstallStatus handles GET /api/cli-tools/install/{id}.
func (h *Handler) HandleGetCliToolsInstallStatus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if h.cliToolJobs == nil {
		writeError(w, http.StatusServiceUnavailable, "cli tool installer not available")
		return
	}
	resp := h.cliToolJobs.get(id)
	if resp == nil {
		writeError(w, http.StatusNotFound, "unknown install job")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}
