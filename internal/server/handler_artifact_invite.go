package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/u007/ocode/internal/artifacts"
)

func (h *Handler) HandleInviteAccept(w http.ResponseWriter, r *http.Request) {
	artifactID := ArtifactIDFromContext(r.Context())
	if artifactID == "" {
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")
		if len(parts) >= 2 {
			artifactID = parts[1]
		}
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")
	inviteToken := ""
	if len(parts) >= 4 {
		inviteToken = parts[3]
	}
	if inviteToken == "" {
		inviteToken = r.URL.Query().Get("token")
	}
	st := h.artifactStore()
	if st == nil {
		http.NotFound(w, r)
		return
	}
	sum := sha256Sum(inviteToken)
	_, err := st.AcceptInvite(sum)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "artifact_access_" + artifactID,
		Value:    inviteToken,
		Path:     "/p/" + artifactID + "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"accepted"}`))
}

func (h *Handler) HandlePasswordGate(w http.ResponseWriter, r *http.Request) {
	artifactID := ArtifactIDFromContext(r.Context())
	if artifactID == "" {
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")
		if len(parts) >= 2 {
			artifactID = parts[1]
		}
	}
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body == nil {
		body = map[string]any{}
	}
	password := getString(body, "password")
	_ = password
	_ = artifactID
	_ = artifacts.ErrNotFound
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}
