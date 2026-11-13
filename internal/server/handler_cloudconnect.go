package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/u007/ocode/internal/cloudconnect"
)

func (h *Handler) cloudManager() *cloudconnect.Manager {
	if h == nil {
		return nil
	}
	return h.cloudconnect
}

// handleCloudConnectors manages connector instances: list, register, remove.
// GET  /api/cloud/connectors            -> list
// POST /api/cloud/connectors            -> register {id, provider, root_path, secrets...}
// DELETE /api/cloud/connectors?id=...  -> remove
func (h *Handler) handleCloudConnectors(w http.ResponseWriter, r *http.Request) {
	mgr := h.cloudManager()
	if mgr == nil {
		http.Error(w, "cloud connectors not initialized", http.StatusServiceUnavailable)
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, mgr.List())
	case http.MethodPost:
		var in struct {
			ID            string                    `json:"id"`
			Provider      string                    `json:"provider"`
			RootPath      string                    `json:"root_path"`
			ProjectSlug   string                    `json:"project_slug"`
			MaxCacheBytes int64                     `json:"max_cache_bytes"`
			Secrets       cloudconnect.SecretBundle `json:"secrets"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if in.ID == "" || in.Provider == "" {
			http.Error(w, "id and provider required", http.StatusBadRequest)
			return
		}
		cfg := cloudconnect.Config{
			ID:            in.ID,
			Provider:      in.Provider,
			RootPath:      in.RootPath,
			ProjectSlug:   in.ProjectSlug,
			MaxCacheBytes: in.MaxCacheBytes,
		}
		if err := mgr.Register(cfg, in.Secrets); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	case http.MethodDelete:
		id := r.URL.Query().Get("id")
		if id == "" {
			http.Error(w, "id required", http.StatusBadRequest)
			return
		}
		if err := mgr.Remove(id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleCloudConnectBrowse lists entries at a path for a connector.
// GET /api/cloud/browse?id=...&path=...
func (h *Handler) handleCloudConnectBrowse(w http.ResponseWriter, r *http.Request) {
	mgr := h.cloudManager()
	if mgr == nil {
		http.Error(w, "cloud connectors not initialized", http.StatusServiceUnavailable)
		return
	}
	id := r.URL.Query().Get("id")
	path := r.URL.Query().Get("path")
	entries, err := mgr.Browse(r.Context(), id, path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, entries)
}

// handleCloudConnectRead downloads a file's content.
// GET /api/cloud/read?id=...&path=...
func (h *Handler) handleCloudConnectRead(w http.ResponseWriter, r *http.Request) {
	mgr := h.cloudManager()
	if mgr == nil {
		http.Error(w, "cloud connectors not initialized", http.StatusServiceUnavailable)
		return
	}
	id := r.URL.Query().Get("id")
	path := r.URL.Query().Get("path")
	rc, err := mgr.Read(r.Context(), id, path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer rc.Close()
	// cache to temp file then serve? For Phase 1, stream directly.
	w.Header().Set("Content-Type", "application/octet-stream")
	// derive name from last path segment
	name := path
	if i := strings.LastIndex(path, "/"); i >= 0 {
		name = path[i+1:]
	}
	if name == "" {
		name = "download"
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
	io.Copy(w, rc)
}

// handleCloudConnectMetadata returns metadata for a single entry.
// GET /api/cloud/metadata?id=...&path=...
func (h *Handler) handleCloudConnectMetadata(w http.ResponseWriter, r *http.Request) {
	mgr := h.cloudManager()
	if mgr == nil {
		http.Error(w, "cloud connectors not initialized", http.StatusServiceUnavailable)
		return
	}
	id := r.URL.Query().Get("id")
	path := r.URL.Query().Get("path")
	e, err := mgr.Metadata(r.Context(), id, path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, e)
}
