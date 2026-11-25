package server

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/u007/ocode/internal/config"
)

// HandleListAutoGrants returns the durable interpreter-exact grants
// (whitelisted scripts) currently loaded, for the Settings UI.
func (h *Handler) HandleListAutoGrants(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.cfg == nil {
		writeError(w, http.StatusInternalServerError, "config not loaded")
		return
	}

	type grantView struct {
		Index             int    `json:"index"`
		Kind              string `json:"kind"`
		Language          string `json:"language"`
		SourceMode        string `json:"source_mode"`
		EntrypointPath    string `json:"entrypoint_path"`
		EntrypointSHA256  string `json:"entrypoint_sha256"`
		NormalizedCommand string `json:"normalized_command"`
		CWD               string `json:"cwd"`
		Destructive       bool   `json:"destructive"`
	}

	grants := []config.AutoGrant{}
	if h.cfg.Ocode.Permissions.Auto != nil {
		grants = h.cfg.Ocode.Permissions.Auto.Grants
	}
	out := make([]grantView, 0, len(grants))
	for i, g := range grants {
		sha := g.EntrypointSHA256
		if len(sha) > 12 {
			sha = sha[:12]
		}
		out = append(out, grantView{
			Index:             i,
			Kind:              g.Kind,
			Language:          g.Language,
			SourceMode:        g.SourceMode,
			EntrypointPath:    g.EntrypointPath,
			EntrypointSHA256:  sha,
			NormalizedCommand: g.NormalizedCommand,
			CWD:               g.CWD,
			Destructive:       g.Destructive,
		})
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"grants": out})
}

// HandleRevokeAutoGrant removes one durable interpreter grant identified by
// ?index=N (or a JSON body {"index":N}) and refreshes every live agent so
// the revocation takes effect immediately.
func (h *Handler) HandleRevokeAutoGrant(w http.ResponseWriter, r *http.Request) {
	index := -1
	if v := strings.TrimSpace(r.URL.Query().Get("index")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			writeError(w, http.StatusBadRequest, "index must be a non-negative integer")
			return
		}
		index = n
	}
	if index < 0 {
		var body struct {
			Index int `json:"index"`
		}
		if err := readBodyJSON(r, &body); err == nil {
			index = body.Index
		}
	}
	if index < 0 {
		writeError(w, http.StatusBadRequest, "index is required")
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	if h.cfg == nil {
		writeError(w, http.StatusInternalServerError, "config not loaded")
		return
	}

	grants := []config.AutoGrant{}
	if h.cfg.Ocode.Permissions.Auto != nil {
		grants = h.cfg.Ocode.Permissions.Auto.Grants
	}
	if index >= len(grants) {
		writeError(w, http.StatusNotFound, "grant index out of range")
		return
	}
	target := grants[index]

	if err := config.RemoveAutoGrant(target); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if h.cfg.Ocode.Permissions.Auto != nil {
		kept := make([]config.AutoGrant, 0, len(h.cfg.Ocode.Permissions.Auto.Grants))
		for _, g := range h.cfg.Ocode.Permissions.Auto.Grants {
			if config.AutoGrantKeyEqual(g, target) {
				continue
			}
			kept = append(kept, g)
		}
		h.cfg.Ocode.Permissions.Auto.Grants = kept
	}
	for _, a := range h.allAgents() {
		if pm := a.Permissions(); pm != nil {
			cur := pm.AutoPermissionConfig()
			if cur != nil {
				for i, g := range cur.Grants {
					if config.AutoGrantKeyEqual(g, target) {
						pm.RemoveAutoGrant(i)
						break
					}
				}
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"revoked": true, "index": index})
}
