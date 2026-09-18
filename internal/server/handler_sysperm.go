package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"runtime"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/sysperm"
)

// systemPermissionsConfig returns the persisted sub-tree without holding the
// handler lock across any OS call.
func (h *Handler) systemPermissionsConfig() sysperm.Config {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cfg == nil {
		return sysperm.DefaultConfig()
	}
	return h.cfg.Ocode.SystemPermissions
}

// systemPermissionsCatalog builds the live entry list. Tests override the
// catalog so the suite never runs an OS probe.
func (h *Handler) systemPermissionsCatalog(ctx context.Context) []sysperm.Entry {
	if h.systemPermissionCatalog != nil {
		return h.systemPermissionCatalog(ctx)
	}
	cfg := h.systemPermissionsConfig()
	return sysperm.Catalog(ctx, cfg, sysperm.Options{DiscoveredPaths: h.allowedProjectRoots()})
}

// syspermRequest fires one OS request, honoring a test override.
func (h *Handler) syspermRequest(ctx context.Context, e sysperm.Entry) sysperm.RequestResult {
	if h.requestSystemPermission != nil {
		return h.requestSystemPermission(ctx, e)
	}
	return sysperm.Request(ctx, e)
}

// systemPermissionsPayload is the shared GET/PUT/DELETE/POST response body.
func (h *Handler) systemPermissionsPayload(ctx context.Context) map[string]any {
	return map[string]any{
		"platform":  runtime.GOOS,
		"supported": sysperm.PlatformSupported(),
		"entries":   h.systemPermissionsCatalog(ctx),
	}
}

// mutateSystemPermissions loads the persisted sub-tree, applies fn, saves it,
// and refreshes the in-memory copy. The caller must not hold h.mu.
func (h *Handler) mutateSystemPermissions(fn func(cfg *sysperm.Config)) error {
	h.sysPermMu.Lock()
	defer h.sysPermMu.Unlock()

	// Load the on-disk config so a concurrent ocode instance's change is not
	// clobbered by a stale in-memory copy.
	current := h.systemPermissionsConfig()
	if ocode, err := config.LoadOcodeConfigCopy(); err == nil && ocode != nil {
		current = ocode.SystemPermissions
	}
	if current.Entries == nil {
		current.Entries = map[string]sysperm.EntryConfig{}
	}
	fn(&current)
	if err := config.SaveSystemPermissionsConfig(current); err != nil {
		return err
	}
	h.mu.Lock()
	if h.cfg != nil {
		h.cfg.Ocode.SystemPermissions = current
	}
	h.mu.Unlock()
	return nil
}

// markSystemPermissionsRequested records that the user asked for these grants,
// which un-gates prompt-triggering folder probes on later reads.
func (h *Handler) markSystemPermissionsRequested(ids []string) {
	if len(ids) == 0 {
		return
	}
	_ = h.mutateSystemPermissions(func(cfg *sysperm.Config) {
		for _, id := range ids {
			ec := cfg.Get(id)
			ec.Requested = true
			cfg.Set(id, ec)
		}
	})
}

// HandleGetSystemPermissions returns the live catalog with per-entry status.
func (h *Handler) HandleGetSystemPermissions(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.systemPermissionsPayload(r.Context()))
}

// systemPermissionPut is the PUT body. A nil Enabled with a non-empty Path
// adds (or re-enables) a custom path; Remove deletes the row.
type systemPermissionPut struct {
	ID      string `json:"id"`
	Enabled *bool  `json:"enabled"`
	Path    string `json:"path"`
	Label   string `json:"label"`
	Remove  bool   `json:"remove"`
}

// HandleSetSystemPermission toggles one entry (or adds/removes a custom path).
// Enabling an entry triggers its OS request and returns the result.
func (h *Handler) HandleSetSystemPermission(w http.ResponseWriter, r *http.Request) {
	decoder := json.NewDecoder(r.Body)
	var payload systemPermissionPut
	if err := decoder.Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	id := payload.ID
	if id == "" && payload.Path != "" {
		id = sysperm.PathID(payload.Path)
	}
	if id == "" {
		writeError(w, http.StatusBadRequest, "id or path is required")
		return
	}

	var enable bool
	if err := h.mutateSystemPermissions(func(cfg *sysperm.Config) {
		if payload.Remove {
			cfg.Delete(id)
			return
		}
		ec := cfg.Get(id)
		if payload.Enabled != nil {
			ec.Enabled = *payload.Enabled
			enable = *payload.Enabled
		}
		if payload.Path != "" {
			ec.Path = payload.Path
			enable = ec.Enabled
		}
		if payload.Label != "" {
			ec.Label = payload.Label
		}
		if enable || payload.Path != "" {
			ec.Requested = true
		}
		cfg.Set(id, ec)
	}); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	resp := h.systemPermissionsPayload(r.Context())
	if enable {
		for _, e := range resp["entries"].([]sysperm.Entry) {
			if e.ID == id {
				resp["result"] = h.syspermRequest(r.Context(), e)
				break
			}
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// HandleDeleteSystemPermission removes one entry (used for custom paths).
func (h *Handler) HandleDeleteSystemPermission(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "id is required")
		return
	}
	if err := h.mutateSystemPermissions(func(cfg *sysperm.Config) { cfg.Delete(id) }); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, h.systemPermissionsPayload(r.Context()))
}

// HandleRequestSystemPermissions requests one entry ({"id": "..."}) or, when
// the id is empty, every enabled-but-not-granted entry (the startup reconcile).
func (h *Handler) HandleRequestSystemPermissions(w http.ResponseWriter, r *http.Request) {
	decoder := json.NewDecoder(r.Body)
	var payload struct {
		ID string `json:"id"`
	}
	// An empty body means "reconcile all", so only reject malformed JSON.
	if err := decoder.Decode(&payload); err != nil && err != io.EOF {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	entries := h.systemPermissionsCatalog(r.Context())
	var results []sysperm.RequestResult
	if payload.ID != "" {
		var target *sysperm.Entry
		for i := range entries {
			if entries[i].ID == payload.ID {
				target = &entries[i]
				break
			}
		}
		if target == nil {
			writeError(w, http.StatusNotFound, "unknown permission id")
			return
		}
		results = []sysperm.RequestResult{h.syspermRequest(r.Context(), *target)}
	} else {
		results = sysperm.Reconcile(r.Context(), entries, h.syspermRequest)
	}

	ids := make([]string, 0, len(results))
	for _, res := range results {
		ids = append(ids, res.ID)
	}
	h.markSystemPermissionsRequested(ids)

	resp := h.systemPermissionsPayload(r.Context())
	resp["results"] = results
	writeJSON(w, http.StatusOK, resp)
}

// ReconcileSystemPermissions is the desktop startup entry point: it re-requests
// every enabled-but-not-granted entry after a rebuild invalidated the TCC
// grants. It is safe on every platform (a no-op off macOS).
func (h *Handler) ReconcileSystemPermissions(ctx context.Context) []sysperm.RequestResult {
	if ctx == nil {
		ctx = context.Background()
	}
	entries := h.systemPermissionsCatalog(ctx)
	results := sysperm.Reconcile(ctx, entries, h.syspermRequest)
	ids := make([]string, 0, len(results))
	for _, res := range results {
		ids = append(ids, res.ID)
	}
	h.markSystemPermissionsRequested(ids)
	return results
}
