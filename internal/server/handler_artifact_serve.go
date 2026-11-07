package server

import (
	"net/http"
)

// handleServeArtifact is the static serve entry for an artifact behind
// artifactAuthMiddleware. Full implementation lands in Task 3; this stub keeps
// the route target compilable.
func (h *Handler) handleServeArtifact(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "not implemented", http.StatusNotImplemented)
}
