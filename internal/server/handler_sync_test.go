package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	ocodesync "github.com/u007/ocode/internal/sync"
)

func TestSyncStatusLoggedOutByDefault(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	h := NewHandler()
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/sync/status", nil)
	h.HandleSyncStatus(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var result SyncStatusResponse
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result.LoggedIn {
		t.Error("expected loggedIn=false with no stored token")
	}
	if result.Config.Synced || result.Auth.Synced {
		t.Error("expected no blobs synced yet")
	}
}

func TestSyncLoginStartReturnsDeviceCode(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"deviceCode": "dc-1", "userCode": "AAAA-BBBB",
			"verifyUrl": "https://kakiit.test/device", "expiresIn": 600,
		})
	}))
	defer srv.Close()

	h := NewHandler()
	h.syncClient = ocodesync.NewClient(srv.URL)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/sync/login/start", nil)
	h.HandleSyncLoginStart(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var result SyncLoginStartResponse
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result.UserCode != "AAAA-BBBB" || result.DeviceCode != "dc-1" {
		t.Errorf("unexpected response: %+v", result)
	}
}

func TestSyncLoginPollApprovedSavesTokenAndUpdatesStatus(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/ocode/device/token":
			json.NewEncoder(w).Encode(map[string]string{"status": "approved", "token": "tok-123"})
		case "/api/ocode/sync/ocodeconfig", "/api/ocode/sync/authsecrets":
			json.NewEncoder(w).Encode(map[string]interface{}{"version": 0, "blob": ""})
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer srv.Close()

	h := NewHandler()
	h.syncClient = ocodesync.NewClient(srv.URL)

	w := httptest.NewRecorder()
	body := `{"deviceCode":"dc-1"}`
	r := httptest.NewRequest("POST", "/api/sync/login/poll", strings.NewReader(body))
	h.HandleSyncLoginPoll(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var result SyncLoginPollResponse
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result.Status != "approved" {
		t.Fatalf("expected approved, got %q", result.Status)
	}

	token, ok, err := ocodesync.LoadToken()
	if err != nil || !ok || token != "tok-123" {
		t.Errorf("expected saved token tok-123, got %q ok=%v err=%v", token, ok, err)
	}

	if h.syncStop == nil {
		t.Error("expected background watcher to be started after approved login")
	}
	h.syncStop() // cleanup: stop the watcher goroutines started by this test
}

func TestSyncLogoutClearsTokenAndStopsWatcher(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	revoked := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/ocode/device/revoke" {
			revoked = true
		}
		json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}))
	defer srv.Close()

	if err := ocodesync.SaveToken("tok-abc"); err != nil {
		t.Fatalf("SaveToken: %v", err)
	}

	h := NewHandler()
	h.syncClient = ocodesync.NewClient(srv.URL)
	stopped := false
	h.syncStop = func() { stopped = true }

	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/sync/logout", nil)
	h.HandleSyncLogout(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !revoked {
		t.Error("expected server-side revoke to be called")
	}
	if !stopped {
		t.Error("expected background watcher to be stopped")
	}
	if _, ok, _ := ocodesync.LoadToken(); ok {
		t.Error("expected token to be cleared locally")
	}
}
