package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/projects"
	"github.com/u007/ocode/internal/redact"
)

// maxCommitMessageBodyBytes bounds the POST body. The generator caps the diff it
// sends to the model, but the request is still rejected before a huge payload is
// decoded into memory.
const maxCommitMessageBodyBytes = 8 << 20

// commitMessageTimeout bounds one commit-message provider call. Without it a
// stalled provider holds the request, and the Git tab's spinner, open forever.
const commitMessageTimeout = 60 * time.Second

// commitMessageDetailMaxRunes caps the provider detail echoed in a failure body.
const commitMessageDetailMaxRunes = 200

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
// the TUI via agent.GenerateCommitMessage, which never falls back to another model.
//
// 409 means no model is configured. 500 means the model was configured but
// generation failed or timed out; the body carries the provider error as one
// redacted line (see commitMessageErrorDetail), and the full error is logged.
// Not 502: the SPA reads 502 as the remote host being unreachable.
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

	ctx, cancel := context.WithTimeout(r.Context(), commitMessageTimeout)
	defer cancel()
	res, err := commitMessageGenerator(ctx, cfg, h.commitMessageProfile(r, req.Project), diff)
	if err != nil {
		slog.Error("commit message generation failed", "project", req.Project, "err", err)
		if errors.Is(err, agent.ErrCommitMessageNotConfigured) {
			writeError(w, http.StatusConflict, "no LLM configured for commit messages; set a model in settings")
			return
		}
		writeError(w, http.StatusInternalServerError, "commit message generation failed: "+commitMessageErrorDetail(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"message": res.Text,
		"model":   res.Model,
	})
}

// commitMessageErrorDetail renders a provider error as one short line that is
// safe to return to the client. Secret-looking spans (API keys in URLs, bearer
// tokens, key=value pairs) are masked by the tier-1 detector before anything is
// truncated, so a cut can never expose a partial secret. Whitespace, including
// newlines, collapses to single spaces.
func commitMessageErrorDetail(err error) string {
	line := strings.Join(strings.Fields(err.Error()), " ")
	spans := redact.Detect(line, nil, redact.DetectOpts{})
	var b strings.Builder
	cursor := 0
	for _, s := range spans {
		if s.Start < cursor {
			continue
		}
		b.WriteString(line[cursor:s.Start])
		fmt.Fprintf(&b, "[REDACTED:%s]", s.Kind)
		cursor = s.End
	}
	b.WriteString(line[cursor:])
	runes := []rune(b.String())
	if len(runes) > commitMessageDetailMaxRunes {
		return string(runes[:commitMessageDetailMaxRunes]) + "..."
	}
	return string(runes)
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
