package server

import (
	"context"
	"net/http"
	"time"

	"github.com/u007/ocode/internal/config"
	ocodesync "github.com/u007/ocode/internal/sync"
)

// SyncLoginStartResponse is the response for POST /api/sync/login/start.
type SyncLoginStartResponse struct {
	DeviceCode string `json:"deviceCode"`
	UserCode   string `json:"userCode"`
	VerifyURL  string `json:"verifyUrl"`
	ExpiresIn  int    `json:"expiresIn"`
}

// SyncLoginPollRequest is the request body for POST /api/sync/login/poll.
type SyncLoginPollRequest struct {
	DeviceCode string `json:"deviceCode"`
}

// SyncLoginPollResponse is the response for POST /api/sync/login/poll.
type SyncLoginPollResponse struct {
	Status string `json:"status"` // "pending" | "approved" | "expired"
}

// SyncBlobStatus reports the last-synced state of one blob type.
type SyncBlobStatus struct {
	Version  int       `json:"version"`
	SyncedAt time.Time `json:"syncedAt"`
	Synced   bool      `json:"synced"`
}

// SyncStatusResponse is the response for GET /api/sync/status.
type SyncStatusResponse struct {
	LoggedIn bool           `json:"loggedIn"`
	Config   SyncBlobStatus `json:"config"`
	Auth     SyncBlobStatus `json:"auth"`
}

// syncClient returns h.syncClient, creating it on first use and rebuilding
// it if the configured sync URL (Settings > Backend > Sync server, or
// ocodeconfig.json's sync_url) has changed since the client was last built.
// Comparing against the last-seen *setting* (syncConfiguredURL) rather than
// the client's own BaseURL means a client built out-of-band (e.g. tests
// pointing it at a mock server) is left alone as long as the setting itself
// hasn't changed.
//
// Unlike the old syncClientLocked (which required callers to hold
// syncMu), this method OWNs the h.syncMu lock
// internally — callers must NOT hold h.syncMu. This centralizes the lock
// contract in one place so a future caller cannot forget the Lock/Unlock
// dance and race on the shared *sync.Client / syncConfiguredURL. The brief
// critical section (read cfg under h.mu, compare, maybe rebuild under
// syncMu) never makes a network call; the returned client is used AFTER the
// lock is released. Lock order: syncMu -> mu (never the reverse).
func (h *Handler) syncClient() *ocodesync.Client {
	h.syncMu.Lock()
	h.mu.Lock()
	configured := ""
	if h.cfg != nil {
		configured = h.cfg.Ocode.SyncURL
	}
	h.mu.Unlock()
	if h.syncClientInst == nil || h.syncConfiguredURL != configured {
		resolved, src := ocodesync.ResolveBaseURLWithSource(configured)
		h.syncClientInst = ocodesync.NewClient(resolved)
		h.syncConfiguredURL = configured
		ocodesync.LogBaseURLNotice(resolved, src)
	}
	client := h.syncClientInst
	h.syncMu.Unlock()
	return client
}

// detachSyncClientForLogout stops the background watcher and clears the
// in-memory client pointer under h.syncMu, then returns the now-detached
// client for the caller to use (e.g. to revoke the token) AFTER releasing
// the lock. Centralizing the stop+detach here preserves the logout invariant
// (watcher stopped exactly once, client pointer cleared before revoke) and
// keeps the lock contract in one place. Returns nil if there is no client.
func (h *Handler) detachSyncClientForLogout() *ocodesync.Client {
	h.syncMu.Lock()
	if h.syncStop != nil {
		h.syncStop()
		h.syncStop = nil
	}
	client := h.syncClientInst
	h.syncClientInst = nil
	h.syncMu.Unlock()
	return client
}

// SyncURLConfigResponse is the response for GET/PUT /api/config/ocode/sync-url.
type SyncURLConfigResponse struct {
	// SyncURL is the persisted override, empty when unset.
	SyncURL string `json:"sync_url"`
	// ResolvedURL is what the sync client actually uses right now: SyncURL
	// if set, else OCODE_SYNC_URL, else the production default.
	ResolvedURL string `json:"resolved_url"`
}

// HandleGetSyncURLConfig reports the configured (if any) and effective sync
// server URL.
func (h *Handler) HandleGetSyncURLConfig(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	configured := ""
	if h.cfg != nil {
		configured = h.cfg.Ocode.SyncURL
	}
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, SyncURLConfigResponse{
		SyncURL:     configured,
		ResolvedURL: ocodesync.ResolveBaseURL(configured),
	})
}

// HandleSetSyncURLConfig persists a sync server URL override. Changing the
// URL stops the background watcher and drops the cached client immediately:
// the old watcher holds a client bound to the previous endpoint, and the
// config-save event from this very change would otherwise trigger a push of
// (possibly secret-bearing) blobs to that old server. Sync stays suspended
// until the next login against the new endpoint rebuilds the client and
// restarts the watcher; the stored token is left untouched since it is tied
// to whichever server issued it.
func (h *Handler) HandleSetSyncURLConfig(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SyncURL string `json:"sync_url"`
	}
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	normalized, err := config.NormalizeSyncURL(req.SyncURL)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := config.SaveSyncURL(normalized); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save config: "+err.Error())
		return
	}
	h.mu.Lock()
	if h.cfg != nil {
		h.cfg.Ocode.SyncURL = normalized
	}
	h.mu.Unlock()
	// Invalidate under syncMu (lock order: syncMu -> mu, same as syncClient):
	// stop the old-endpoint watcher and drop the cached client so the next
	// syncClient() call rebuilds against the new URL. A watcher started later
	// by startSyncWatcher carries an identity check, so it cannot resurrect
	// this detached client.
	h.syncMu.Lock()
	if h.syncStop != nil {
		h.syncStop()
		h.syncStop = nil
	}
	h.syncClientInst = nil
	h.syncMu.Unlock()
	writeJSON(w, http.StatusOK, SyncURLConfigResponse{
		SyncURL:     normalized,
		ResolvedURL: ocodesync.ResolveBaseURL(normalized),
	})
}

// HandleSyncLoginStart begins a device-code flow and returns the code/URL
// for the web UI to display, without blocking on approval.
func (h *Handler) HandleSyncLoginStart(w http.ResponseWriter, r *http.Request) {
	client := h.syncClient()

	result, err := client.StartDevice(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, "start device flow: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, SyncLoginStartResponse{
		DeviceCode: result.DeviceCode,
		UserCode:   result.UserCode,
		VerifyURL:  result.VerifyURL,
		ExpiresIn:  result.ExpiresIn,
	})
}

// HandleSyncLoginPoll checks a device code once. On approval it saves the
// token, pulls both blobs to bootstrap local state, and (re)starts the
// background sync watcher.
func (h *Handler) HandleSyncLoginPoll(w http.ResponseWriter, r *http.Request) {
	var req SyncLoginPollRequest
	if err := readBodyJSON(r, &req); err != nil || req.DeviceCode == "" {
		writeError(w, http.StatusBadRequest, "deviceCode is required")
		return
	}

	client := h.syncClient()

	result, err := client.PollDevice(r.Context(), req.DeviceCode)
	if err != nil {
		writeError(w, http.StatusBadGateway, "poll device flow: "+err.Error())
		return
	}

	if result.Status == "approved" {
		if err := ocodesync.SaveToken(result.Token); err != nil {
			writeError(w, http.StatusInternalServerError, "save sync token: "+err.Error())
			return
		}
		h.startSyncWatcher(client)
	}

	writeJSON(w, http.StatusOK, SyncLoginPollResponse{Status: result.Status})
}

// HandleSyncStatus reports whether the machine is logged in and the
// last-synced version/time for each blob.
func (h *Handler) HandleSyncStatus(w http.ResponseWriter, r *http.Request) {
	_, loggedIn, err := ocodesync.LoadToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "load sync token: "+err.Error())
		return
	}

	resp := SyncStatusResponse{LoggedIn: loggedIn}
	if v, t, ok := ocodesync.SnapshotInfo(ocodesync.BlobTypeConfig); ok {
		resp.Config = SyncBlobStatus{Version: v, SyncedAt: t, Synced: true}
	}
	if v, t, ok := ocodesync.SnapshotInfo(ocodesync.BlobTypeAuth); ok {
		resp.Auth = SyncBlobStatus{Version: v, SyncedAt: t, Synced: true}
	}
	writeJSON(w, http.StatusOK, resp)
}

// HandleSyncLogout stops the background watcher, revokes the token
// server-side (best-effort), and clears it locally. The watcher is stopped
// and the client pointer is detached under syncMu (via
// detachSyncClientForLogout); the actual revoke happens AFTER the lock is
// released so the blocking network call never sits inside a critical
// section. Clearing the token is intentionally the last step so a failed
// revoke does not leave the machine logged-out locally while the token is
// still live server-side.
func (h *Handler) HandleSyncLogout(w http.ResponseWriter, r *http.Request) {
	token, ok, err := ocodesync.LoadToken()
	if err != nil || !ok {
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return
	}

	client := h.detachSyncClientForLogout()

	if client != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		_ = client.Revoke(ctx, token) // best-effort; token is cleared locally either way
	}

	if err := ocodesync.ClearToken(); err != nil {
		writeError(w, http.StatusInternalServerError, "clear sync token: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// startSyncWatcher pulls both blobs and (re)starts the background
// watcher. Stops any previously running watcher first so repeated logins
// (or a login after a prior process-lifetime login) don't double-wire the
// config/auth save hooks.
//
// The client identity is captured BEFORE the (network-bound) Pull so that,
// after acquiring syncMu, we can verify this client is still the current one.
// If logout (or a reconfigured syncClient) replaced h.syncStop/h.syncClient
// while Pull was in flight, installing a watcher here would silently undo the
// logout. The identity check makes startSyncWatcher a no-op in that case —
// the already-detached client's watcher is not resurrected.
func (h *Handler) startSyncWatcher(client *ocodesync.Client) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = client.Pull(ctx, ocodesync.BlobTypeConfig) // best-effort; background sync will retry
	_ = client.Pull(ctx, ocodesync.BlobTypeAuth)

	h.syncMu.Lock()
	defer h.syncMu.Unlock()
	// If the client that initiated this Pull is no longer the current client
	// (logout or reconfigured URL replaced it while we were pulling), do NOT
	// install a watcher — that would resurrect a detached/stopped client and
	// undo the logout.
	if h.syncClientInst != client {
		return
	}
	if h.syncStop != nil {
		h.syncStop()
	}
	h.syncStop = ocodesync.StartWatcher(client)
}
