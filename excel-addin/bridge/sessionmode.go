package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"slices"
)

// The pane needs one chat's permission mode, and ocode's status route (GET /api/sessions/{id}/status) returns the whole
// TUIStatus: cwd, extra_allowed_paths, lsp_servers with launch commands, model_prompt. Its GET also starts the project's
// LSP warm-up. So the raw route is not on the /api/ allowlist. The bridge calls it itself and returns only the mode.

// getSessionMode answers GET /bridge/session-mode/{id} with {"permission_mode": "<mode>"}, one of ocodePermissionModes.
// Nothing is defaulted: a missing or unrecognised mode, or any ocode failure, is a 502 (404 when ocode has no such chat).
// The answer is a fresh object built here, so no other field of ocode's body reaches the pane. ocode's own error text is
// logged, never echoed.
func (s *srv) getSessionMode(w http.ResponseWriter, r *http.Request) {
	sid := r.PathValue("id")
	if !sessionIDPattern.MatchString(sid) {
		bridgeRouteError(w, http.StatusBadRequest, errors.New("that is not a valid chat id"))
		return
	}
	mode, err := s.sessionPermissionMode(r.Context(), sid)
	if err != nil {
		status, short := http.StatusBadGateway, "ocode did not report a usable permission mode for this chat"
		var httpErr *ocodeHTTPError
		if errors.As(err, &httpErr) && httpErr.code == http.StatusNotFound {
			status, short = http.StatusNotFound, "ocode has no such chat"
		}
		log.Printf("session mode for %s (HTTP %d): %v", sid, status, err)
		writeJSONStatus(w, status, map[string]string{"error": short})
		return
	}
	writeJSONStatus(w, http.StatusOK, map[string]string{"permission_mode": mode})
}

// sessionPermissionMode reads one chat's permission_mode from ocode and checks it is one of ocodePermissionModes.
func (s *srv) sessionPermissionMode(ctx context.Context, sid string) (string, error) {
	raw, err := s.ocodeCall(ctx, http.MethodGet, "/api/sessions/"+sid+"/status", nil)
	if err != nil {
		return "", err
	}
	mode, err := permissionModeOf(sid, raw)
	if err != nil {
		return "", err
	}
	if !slices.Contains(ocodePermissionModes, mode) {
		return "", fmt.Errorf("ocode reported permission mode %q for chat %s, which is not one of %v", mode, sid, ocodePermissionModes)
	}
	return mode, nil
}
