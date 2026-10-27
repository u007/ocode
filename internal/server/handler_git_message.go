package server

import (
	"net/http"
	"os"
	"strings"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/projects"
)

// maxCommitMessageBodyBytes bounds the POST body. The generator caps the diff it
// sends to the model, but the request is still rejected before a huge payload is
// decoded into memory.
const maxCommitMessageBodyBytes = 8 << 20

// commitMessageGenerator is agent.GenerateCommitMessage behind a package
// variable so handler tests can stub the LLM call.
var commitMessageGenerator = agent.GenerateCommitMessage

// HandleGitCommitMessage generates a git commit message from a client-supplied
// unified diff (POST /api/git/commit-message).
//
// The web/desktop Git tab sends the staged diff it already holds, so a remote
// project needs no host round-trip: generation runs on the server serving the
// UI, using the request's credential profile. Model policy (explicit
// commit_msg_model, else the small model, else the main model) is shared with
// the TUI via agent.GenerateCommitMessage.
func (h *Handler) HandleGitCommitMessage(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxCommitMessageBodyBytes)
	var req struct {
		Diff    string `json:"diff"`
		Project string `json:"project"`
	}
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	diff := strings.TrimSpace(req.Diff)
	if diff == "" {
		writeError(w, http.StatusBadRequest, "diff is required")
		return
	}

	h.mu.Lock()
	cfg := h.cfg
	h.mu.Unlock()
	if cfg == nil {
		writeError(w, http.StatusInternalServerError, "config not loaded")
		return
	}

	res, err := commitMessageGenerator(cfg, h.commitMessageProfile(r, req.Project), diff)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"message": res.Text,
		"model":   res.Model,
	})
}

// commitMessageProfile resolves the credential profile for a one-shot
// generation: OCODE_PROFILE > project binding > window binding > v1 fallback
// (the same order as resolveSessionProfile, minus the session registry).
func (h *Handler) commitMessageProfile(r *http.Request, project string) string {
	if v := os.Getenv("OCODE_PROFILE"); v != "" {
		return v
	}
	if h.projects != nil && project != "" {
		if p := h.projects.ProfileFor(projects.ProjectRef{Path: project}); p != "" {
			return p
		}
	}
	if id := strings.TrimSpace(r.Header.Get("X-Window-Id")); id != "" {
		if p := h.getWindowProfile(id); p != "" {
			return p
		}
	}
	return h.globalEffectiveProfile()
}
