package server

import (
	"encoding/json"
	"net/http"
	"strings"
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
	if artifactID == "" || inviteToken == "" || len(inviteToken) > artifactTokenMaxLen {
		http.NotFound(w, r)
		return
	}
	// Scoped to this artifact: an invite minted for another artifact never
	// accepts here. The cookie carries the raw invite token; the allow-list
	// row keeps its hash, and the serve path verifies the cookie against it.
	sum := sha256Sum(inviteToken)
	if _, err := st.AcceptArtifactInvite(artifactID, sum); err != nil {
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
	// The gate route carries no capability token in its path, so the body
	// names the token whose password is being checked.
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body == nil {
		body = map[string]any{}
	}
	token := getString(body, "token")
	password := getString(body, "password")
	if h.artifactTokens == nil || artifactID == "" {
		http.NotFound(w, r)
		return
	}
	rl := h.artifactRL
	key := artifactClientKey(r)
	if rl != nil && rl.isBlocked(key) {
		http.Error(w, "too many requests", http.StatusTooManyRequests)
		return
	}
	at, _, err := h.artifactTokens.validate(token, artifactID)
	if err != nil {
		if rl != nil {
			rl.recordFailure(key)
		}
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if at.PasswordHash == "" || passwordCookieOK(r, artifactID, at) {
		_, _ = w.Write([]byte(`{"status":"ok"}`))
		return
	}
	if password == "" {
		if rl != nil {
			rl.recordFailure(key)
		}
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"status":"unauthorized"}`))
		return
	}
	ok, busy := h.artifactTokens.checkPassword(rl, key, at, password)
	if busy {
		w.Header().Set("Retry-After", "1")
		http.Error(w, "too many password checks in progress", http.StatusTooManyRequests)
		return
	}
	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"status":"unauthorized"}`))
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     passwordCookieName(artifactID),
		Value:    passwordCookieValue(artifactID, at),
		Path:     "/p/" + artifactID + "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}
