package server

import (
	"context"
	"net/http"
	"time"

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

// syncClientLocked returns h.syncClient, creating it on first use. Callers
// must hold h.syncMu.
func (h *Handler) syncClientLocked() *ocodesync.Client {
	if h.syncClient == nil {
		h.syncClient = ocodesync.NewClient(ocodesync.DefaultBaseURL())
	}
	return h.syncClient
}

// HandleSyncLoginStart begins a device-code flow and returns the code/URL
// for the web UI to display, without blocking on approval.
func (h *Handler) HandleSyncLoginStart(w http.ResponseWriter, r *http.Request) {
	h.syncMu.Lock()
	client := h.syncClientLocked()
	h.syncMu.Unlock()

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

	h.syncMu.Lock()
	client := h.syncClientLocked()
	h.syncMu.Unlock()

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
// server-side (best-effort), and clears it locally.
func (h *Handler) HandleSyncLogout(w http.ResponseWriter, r *http.Request) {
	token, ok, err := ocodesync.LoadToken()
	if err != nil || !ok {
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return
	}

	h.syncMu.Lock()
	if h.syncStop != nil {
		h.syncStop()
		h.syncStop = nil
	}
	client := h.syncClientLocked()
	h.syncMu.Unlock()

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	_ = client.Revoke(ctx, token) // best-effort; token is cleared locally either way

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
func (h *Handler) startSyncWatcher(client *ocodesync.Client) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = client.Pull(ctx, ocodesync.BlobTypeConfig) // best-effort; background sync will retry
	_ = client.Pull(ctx, ocodesync.BlobTypeAuth)

	h.syncMu.Lock()
	defer h.syncMu.Unlock()
	if h.syncStop != nil {
		h.syncStop()
	}
	h.syncStop = ocodesync.StartWatcher(client)
}
