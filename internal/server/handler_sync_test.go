package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/u007/ocode/internal/config"
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
	h.syncClientInst = ocodesync.NewClient(srv.URL)

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
	h.syncClientInst = ocodesync.NewClient(srv.URL)

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

// TestSyncClientConcurrentAccess verifies the lock-owning syncClient()
// accessor returns a consistent pointer under concurrent access. Before the
// refactor, syncClientLocked() required callers to hold h.syncMu manually — a
// forgotten Lock left the shared *sync.Client / syncConfiguredURL
// unprotected. With the lock now centralized inside syncClient(), concurrent
// callers must always observe the same pointer and never a torn client.
// Deterministic: seed syncClientInst + syncConfiguredURL consistently, then
// concurrently call h.syncClient() and assert every result is the same
// pointer. Run with -race to detect data races in the accessor.
func TestSyncClientConcurrentAccess(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	h := NewHandler()
	h.mu.Lock()
	h.cfg = &config.Config{Ocode: config.OcodeConfig{}}
	h.mu.Unlock()

	// Seed a client directly (BaseURL is never dereferenced by the accessor)
	// so syncClient() finds syncClientInst != nil and syncConfiguredURL ==
	// configured and returns it without rebuilding or any network call.
	seeded := ocodesync.NewClient("http://127.0.0.1:1")
	h.syncMu.Lock()
	h.syncClientInst = seeded
	h.syncConfiguredURL = "" // matches cfg.Ocode.SyncURL == ""
	h.syncMu.Unlock()

	const goroutines = 64
	results := make([]*ocodesync.Client, goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()
			results[i] = h.syncClient()
		}(i)
	}
	wg.Wait()

	for i, c := range results {
		if c != seeded {
			t.Fatalf("goroutine %d observed %p, want seeded %p (accessor returned inconsistent client)", i, c, seeded)
		}
	}
}

// TestSyncLogoutDuringLoginPollWatcherRace is the regression guard for the
// watcher-startup race: a login-poll's startSyncWatcher does a network-bound
// Pull BEFORE acquiring syncMu. If logout (HandleSyncLogout →
// detachSyncClientForLogout) runs while that Pull is in flight and detaches
// the client, startSyncWatcher must NOT resurrect the detached client's
// watcher when it finally acquires the lock.
//
// We force the race window open with an httptest handler blocked on a
// channel: startSyncWatcher stalls inside Pull; we detach the client; then we
// release the handler. The identity check (h.syncClientInst != client) must
// make startSyncWatcher a no-op, so no watcher is reinstated.
func TestSyncLogoutDuringLoginPollWatcherRace(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	h := NewHandler()
	h.mu.Lock()
	h.cfg = &config.Config{Ocode: config.OcodeConfig{}}
	h.mu.Unlock()

	// Block the handler on `release` until we have detached the client.
	// entered signals that the first Pull has reached the handler and is
	// parked; sync.Once because Pull is called twice (config + auth blobs).
	release := make(chan struct{})
	entered := make(chan struct{})
	var enteredOnce sync.Once
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		enteredOnce.Do(func() { close(entered) })
		<-release
		json.NewEncoder(w).Encode(map[string]interface{}{"version": 0, "blob": ""})
	}))
	defer srv.Close()

	client := ocodesync.NewClient(srv.URL)

	// Save a token so Pull actually issues the HTTP request (without a
	// token Pull returns early without calling the handler, and the test
	// could not observe the blocked-Pull race window).
	if err := ocodesync.SaveToken("test-token"); err != nil {
		t.Fatalf("SaveToken: %v", err)
	}

	// Seed the client as if a prior (still in-flight) login had built it.
	h.syncMu.Lock()
	h.syncClientInst = client
	h.syncMu.Unlock()

	// Kick off startSyncWatcher; it will block in Pull on <-release.
	done := make(chan struct{})
	go func() {
		h.startSyncWatcher(client)
		close(done)
	}()

	// Wait until the in-flight Pull has actually entered the handler and is
	// blocked on <-release. This opens the race window deterministically.
	<-entered

	// Logout detaches the client while Pull is blocked in the handler.
	detached := h.detachSyncClientForLogout()
	if detached != client {
		t.Fatalf("expected detachSyncClientForLogout to return the seeded client %v, got %v", client, detached)
	}

	// Release the blocked Pull so startSyncWatcher proceeds to acquire
	// syncMu and hit the identity check.
	close(release)
	<-done

	// The detached client's watcher must NOT be resurrected.
	h.syncMu.Lock()
	stopped := h.syncStop
	h.syncMu.Unlock()
	if stopped != nil {
		t.Fatal("startSyncWatcher resurrected a watcher for a detached client — logout was undone")
	}
}

// TestSyncLogoutClearsTokenAndStopsWatcher verifies the logout invariant:
// watcher stopped, server-side revoke called, token cleared locally.
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
	h.syncClientInst = ocodesync.NewClient(srv.URL)
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
