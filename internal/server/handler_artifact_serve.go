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
	// Only a live artifact is served. Unpublish sets a non-"active" status.
	art, err := st.GetArtifact(artifactID)
	if err != nil || art.Status != "active" {
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
	// Reject any ".." path segment in the raw request path BEFORE cleaning.
	// After path.Clean("/"+...) no ".." segment can remain, so checking later
	// would be vacuous. Only whole segments are refused: "a..b.js" is a name.
	for _, seg := range strings.Split(relPath, "/") {
		if seg == ".." {
			http.NotFound(w, r)
			return
		}
	}
	relPath = path.Clean("/" + relPath)
	relPath = strings.TrimPrefix(relPath, "/")
	if relPath == "." || relPath == "" {
		relPath = "index.html"
	}
	versions, err := st.ListVersions(artifactID)
	if err != nil || len(versions) == 0 {
		http.NotFound(w, r)
		return
	}
	v, ok := latestArtifactVersion(versions)
	if !ok {
		http.NotFound(w, r)
		return
	}
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
		IP:         artifactClientKey(r),
		UA:         r.UserAgent(),
		Path:       relPath,
		Status:     http.StatusOK,
	})
}

// latestArtifactVersion picks the newest version by an explicit sort key
// (created_at, then version label, then id) instead of trusting the order
// ListVersions returns. It reports false for an empty slice.
func latestArtifactVersion(vs []artifacts.Version) (artifacts.Version, bool) {
	if len(vs) == 0 {
		return artifacts.Version{}, false
	}
	best := vs[0]
	for _, v := range vs[1:] {
		switch {
		case !v.CreatedAt.Equal(best.CreatedAt):
			if v.CreatedAt.After(best.CreatedAt) {
				best = v
			}
		case v.Version != best.Version:
			if v.Version > best.Version {
				best = v
			}
		case v.ID > best.ID:
			best = v
		}
	}
	return best, true
}
