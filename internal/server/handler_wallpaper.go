package server

import (
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/wallpaper"
)

// wallpaperUploadResponse is the response for POST /api/wallpaper/upload.
type wallpaperUploadResponse struct {
	Meta wallpaper.WallpaperMeta `json:"meta"`
}

// HandleListWallpapers returns all available wallpapers (built-in + uploaded).
func (h *Handler) HandleListWallpapers(w http.ResponseWriter, r *http.Request) {
	metas, err := wallpaper.ListWallpapers()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list wallpapers: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"wallpapers": metas,
	})
}

// HandleGetWallpaper serves the raw image bytes for a wallpaper ID.
func (h *Handler) HandleGetWallpaper(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/wallpaper/")
	if id == "" {
		writeError(w, http.StatusBadRequest, "wallpaper ID required")
		return
	}
	data, err := wallpaper.ReadWallpaperFile(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "wallpaper not found")
		return
	}
	w.Header().Set("Content-Type", "image/svg+xml")
	// Uploaded SVGs are user content served on the API origin. Uploads are
	// sanitized at registration; this CSP is the second layer so a direct
	// navigation to the file can never run script or load remote resources.
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'none'; style-src 'unsafe-inline'; img-src data:")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
	w.Write(data)
}

// HandleUploadWallpaper accepts a multipart form upload and stores the
// wallpaper under the user upload directory.
func (h *Handler) HandleUploadWallpaper(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		writeError(w, http.StatusBadRequest, "file too large (max 10MB)")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing file field 'file'")
		return
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, maxUploadSize+1))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read file")
		return
	}
	if len(data) > maxUploadSize {
		writeError(w, http.StatusBadRequest, "file too large (max 10MB)")
		return
	}

	// Validate extension
	ext := strings.ToLower(filepath.Ext(header.Filename))
	allowedExts := map[string]bool{".svg": true, ".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true}
	if !allowedExts[ext] {
		writeError(w, http.StatusBadRequest, "unsupported file type: "+ext)
		return
	}

	meta, err := wallpaper.RegisterUserUpload(header.Filename, data)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to register upload: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, wallpaperUploadResponse{Meta: meta})
}

// HandleGetWallpaperConfig returns the current wallpaper configuration.
func (h *Handler) HandleGetWallpaperConfig(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	cfg := wallpaper.DefaultWallpaperConfig()
	if h.cfg != nil && h.cfg.Ocode.Wallpaper.Enabled {
		cfg = h.cfg.Ocode.Wallpaper
	}
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, cfg)
}

// HandleSetWallpaperConfig persists the wallpaper configuration.
func (h *Handler) HandleSetWallpaperConfig(w http.ResponseWriter, r *http.Request) {
	var req wallpaper.WallpaperConfig
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Mode != "" {
		valid := false
		for _, m := range wallpaper.ValidModes {
			if req.Mode == m {
				valid = true
				break
			}
		}
		if !valid {
			writeError(w, http.StatusBadRequest, "invalid mode, must be one of: "+strings.Join(wallpaper.ValidModes, ", "))
			return
		}
	}
	if err := config.SaveOcodeWallpaperConfig(req); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save wallpaper config: "+err.Error())
		return
	}
	h.mu.Lock()
	if h.cfg != nil {
		h.cfg.Ocode.Wallpaper = req
	}
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, req)
}
