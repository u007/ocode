package server

import (
	"net/http"
	"path"
	"strings"

	"github.com/u007/ocode/internal/artifacts"
)

func (h *Handler) handleServeArtifact(w http.ResponseWriter, r *http.Request) {
	artifactID := ArtifactIDFromContext(r.Context())
	if artifactID == "" {
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")
		if len(parts) >= 2 {
			artifactID = parts[1]
		}
	}
	st := h.artifactStore()
	if st == nil {
		http.NotFound(w, r)
		return
	}
	if _, err := st.GetArtifact(artifactID); err != nil {
		http.NotFound(w, r)
		return
	}
	tokenID := TokenIDFromContext(r.Context())
	if tokenID == "" {
		http.NotFound(w, r)
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")
	relPath := ""
	if len(parts) >= 4 {
		relPath = strings.Join(parts[3:], "/")
	}
	if relPath == "" {
		relPath = "index.html"
	}
	relPath = path.Clean("/" + relPath)
	relPath = strings.TrimPrefix(relPath, "/")
	if relPath == "." || relPath == "" {
		relPath = "index.html"
	}
	if strings.Contains(relPath, "..") {
		http.NotFound(w, r)
		return
	}
	versions, err := st.ListVersions(artifactID)
	if err != nil || len(versions) == 0 {
		http.NotFound(w, r)
		return
	}
	v := versions[len(versions)-1]
	files, err := st.GetVersionFiles(v.ID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	var target *artifacts.File
	for i := range files {
		if files[i].Path == relPath {
			target = &files[i]
			break
		}
	}
	if target == nil && (relPath == "" || relPath == "index.html") {
		for i := range files {
			if files[i].Path == v.Entrypoint {
				target = &files[i]
				break
			}
		}
	}
	if target == nil {
		http.NotFound(w, r)
		return
	}
	data, err := st.GetBlob(target.BlobHash)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", target.Mime)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; frame-ancestors 'none'")
	_, _ = w.Write(data)
	_ = st.LogAccess(artifacts.AccessLogEntry{
		ArtifactID: artifactID,
		TokenID:    tokenID,
		IP:         realIP(r),
		UA:         r.UserAgent(),
		Path:       relPath,
		Status:     http.StatusOK,
	})
}
