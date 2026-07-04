package server

import (
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/u007/ocode/internal/usage"
)

// HandleGetTUIStatus returns the latest TUI status snapshot pushed by the TUI
// whenever any tracked field changes. When no TUI is attached (server is
// running headless), it returns a snapshot built from the local handler config
// and a zero session ID so the web UI can still render a meaningful page.
func (h *Handler) HandleGetTUIStatus(w http.ResponseWriter, r *http.Request, rc *RCBridge) {
	if rc != nil {
		writeJSON(w, http.StatusOK, rc.TUIStatus())
		return
	}
	// Headless fallback — populate the fields that don't need a live session.
	snap := TUIStatus{
		AdvisorEnabled: h.advisorEnabled,
		OcrBackend:     "openai-compat",
		UpdatedAt:      time.Now().UTC().Format(time.RFC3339Nano),
	}
	if h.cfg != nil {
		snap.MainModel = h.cfg.Model
		snap.SmallModel = h.cfg.Ocode.SmallModel
		snap.SmallModelOn = h.cfg.Ocode.SmallModelEnabled
		snap.AdvisorModel = h.cfg.Ocode.Advisor.Model
		snap.ExtraAllowedPaths = h.cfg.Ocode.ExtraAllowedPaths
		snap.OcrBackend = h.cfg.Ocode.Ocr.Backend
		if snap.OcrBackend == "" {
			snap.OcrBackend = "openai-compat"
		}
		switch snap.OcrBackend {
		case "paddle":
			snap.OcrModel = h.cfg.Ocode.Ocr.Paddle.Variant
		default:
			snap.OcrModel = h.cfg.Ocode.Ocr.OpenAI.Model
		}
		snap.OcrEnabled = h.cfg.Ocode.Ocr.Enabled
	}
	if cwd, err := os.Getwd(); err == nil {
		snap.CWD = cwd
	}
	writeJSON(w, http.StatusOK, snap)
}

// HandleGetSpending returns the cumulative USD cost for today, sourced from the
// usage records. The web displays this next to the model in the status bar so
// the user can see spend accumulate as the model runs.
func (h *Handler) HandleGetSpending(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	from := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	recs, err := usage.Query(from, now)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var total float64
	for _, rec := range recs {
		total += rec.Spend
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"spending_usd": total,
		"records":      len(recs),
		"from":         from.Format(time.RFC3339),
		"to":           now.Format(time.RFC3339),
	})
}

// HandleGetLSPStatuses returns the current set of running LSP servers, derived
// from the TUI's bridge snapshot (when attached) or an empty list (headless).
func (h *Handler) HandleGetLSPStatuses(w http.ResponseWriter, r *http.Request, rc *RCBridge) {
	if rc == nil {
		writeJSON(w, http.StatusOK, map[string]any{"lsp_servers": []LSPStatus{}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"lsp_servers": rc.TUIStatus().LSPServers,
	})
}

// HandleGetModifiedFiles returns the list of files the TUI knows are modified
// in this session, with a one-character git status code when known. The TUI
// pushes these via the status snapshot; we expose them on a dedicated endpoint
// so the web can refresh just the file list without re-fetching the whole
// payload.
func (h *Handler) HandleGetModifiedFiles(w http.ResponseWriter, r *http.Request, rc *RCBridge) {
	if rc == nil {
		writeJSON(w, http.StatusOK, map[string]any{"modified_files": []FileStatus{}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"modified_files": rc.TUIStatus().ModifiedFiles,
	})
}

// absPath is a small helper that returns the absolute, cleaned form of p. It
// is used by the TUI when building the modified-files list so paths in the
// web UI match what the user sees in the TUI sidebar.
func absPath(p string) string {
	if p == "" {
		return p
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return filepath.Clean(p)
	}
	return abs
}
