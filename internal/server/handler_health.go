package server

import (
	"net/http"

	"github.com/u007/ocode/internal/version"
)

// handleHealth answers GET /api/health unauthenticated (deliberately not
// wrapped in authMiddleware): it exists so a --remote server's reuse check
// (internal/remote's ServerAlive, run as a short-lived exec probe on the
// remote host itself, before any tunnel or token has been established) can
// tell "process alive and this ocode's HTTP stack is actually serving" from
// "process alive but wedged/still booting", without needing the token.
// Never expose anything beyond the version here.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"version": version.Version})
}
