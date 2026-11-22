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
	setHomeTree(t, t.TempDir())

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
	setHomeTree(t, t.TempDir())

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
	setHomeTree(t, t.TempDir())

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
	setHomeTree(t, t.TempDir())

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
	setHomeTree(t, t.TempDir())

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
	setHomeTree(t, t.TempDir())

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

// TestSyncNowPullsBothBlobs verifies POST /api/sync/sync pulls config and auth
// from the sync server and reports refreshed (synced) status. A blob is
// "synced" once its snapshot file exists on disk; Pull writes it when the
// server has a blob, PushLocal writes it after uploading the local blob.
func TestSyncNowPullsBothBlobs(t *testing.T) {
	setHomeTree(t, t.TempDir())

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && (r.URL.Path == "/api/ocode/sync/ocodeconfig" || r.URL.Path == "/api/ocode/sync/authsecrets"):
			json.NewEncoder(w).Encode(map[string]interface{}{"version": 1, "blob": "{}"})
		case r.Method == http.MethodPut && (r.URL.Path == "/api/ocode/sync/ocodeconfig" || r.URL.Path == "/api/ocode/sync/authsecrets"):
			json.NewEncoder(w).Encode(map[string]bool{"ok": true})
		default:
			http.Error(w, "unexpected path", http.StatusNotFound)
		}
	}))
	defer srv.Close()

	if err := ocodesync.SaveToken("tok-abc"); err != nil {
		t.Fatalf("SaveToken: %v", err)
	}

	h := NewHandler()
	h.syncClientInst = ocodesync.NewClient(srv.URL)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/sync/sync", nil)
	h.HandleSyncNow(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var result SyncStatusResponse
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !result.LoggedIn {
		t.Error("expected loggedIn=true")
	}
	if !result.Config.Synced || !result.Auth.Synced {
		t.Errorf("expected both blobs synced, got %+v", result)
	}
}

// TestSyncNowNeverSyncedReachesSynced verifies that a machine which has never
// synced (server has no blob, so Pull writes no snapshot) still reaches
// "synced" by pushing its local blob up.
func TestSyncNowNeverSyncedReachesSynced(t *testing.T) {
	setHomeTree(t, t.TempDir())

	var pulls, puts []string
	mu := &sync.Mutex{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		if r.Method == http.MethodGet {
			pulls = append(pulls, r.URL.Path)
		} else {
			puts = append(puts, r.URL.Path)
		}
		mu.Unlock()
		switch {
		case r.Method == http.MethodGet && (r.URL.Path == "/api/ocode/sync/ocodeconfig" || r.URL.Path == "/api/ocode/sync/authsecrets"):
			// Server has no blob for this user yet.
			json.NewEncoder(w).Encode(map[string]interface{}{"version": 0, "blob": ""})
		case r.Method == http.MethodPut && (r.URL.Path == "/api/ocode/sync/ocodeconfig" || r.URL.Path == "/api/ocode/sync/authsecrets"):
			json.NewEncoder(w).Encode(map[string]bool{"ok": true})
		default:
			http.Error(w, "unexpected path", http.StatusNotFound)
		}
	}))
	defer srv.Close()

	if err := ocodesync.SaveToken("tok-abc"); err != nil {
		t.Fatalf("SaveToken: %v", err)
	}

	h := NewHandler()
	h.syncClientInst = ocodesync.NewClient(srv.URL)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/sync/sync", nil)
	h.HandleSyncNow(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var result SyncStatusResponse
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !result.Config.Synced || !result.Auth.Synced {
		t.Errorf("expected both blobs synced after push, got %+v", result)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(pulls) != 2 || len(puts) != 2 {
		t.Errorf("expected 2 pulls and 2 pushes, got pulls=%v puts=%v", pulls, puts)
	}
}

// TestSyncNowInFlightGuardSkipsPull verifies a concurrent sync-now call does
// not start a second overlapping Pull: with syncNowInFlight set it reports the
// current status without hitting the sync server.
func TestSyncNowInFlightGuardSkipsPull(t *testing.T) {
	setHomeTree(t, t.TempDir())

	if err := ocodesync.SaveToken("tok-abc"); err != nil {
		t.Fatalf("SaveToken: %v", err)
	}

	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		json.NewEncoder(w).Encode(map[string]interface{}{"version": 1, "blob": "{}"})
	}))
	defer srv.Close()

	h := NewHandler()
	h.syncClientInst = ocodesync.NewClient(srv.URL)

	// Simulate a sync already in flight.
	h.syncMu.Lock()
	h.syncNowInFlight = true
	h.syncMu.Unlock()

	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/sync/sync", nil)
	h.HandleSyncNow(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if hits != 0 {
		t.Errorf("expected no Pull while in flight, got %d hits", hits)
	}
}

// TestSyncNowFlagResetsAfterPullError verifies the in-flight flag resets after
// a failed sync so a subsequent call is not permanently blocked.
func TestSyncNowFlagResetsAfterPullError(t *testing.T) {
	setHomeTree(t, t.TempDir())

	if err := ocodesync.SaveToken("tok-abc"); err != nil {
		t.Fatalf("SaveToken: %v", err)
	}

	// First mock: every pull returns 500, so the sync fails.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	h := NewHandler()
	h.syncClientInst = ocodesync.NewClient(srv.URL)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/sync/sync", nil)
	h.HandleSyncNow(w, r)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 on sync failure, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "sync failed") {
		t.Fatalf("expected error body naming the failure, got %s", w.Body.String())
	}

	// syncBlobBoth runs synchronously, so by now the flag must be reset.
	h.syncMu.Lock()
	inFlight := h.syncNowInFlight
	h.syncMu.Unlock()
	if inFlight {
		t.Error("expected syncNowInFlight to reset after a failed sync")
	}

	// A subsequent call must reach the server (not be blocked by a stale flag).
	hits := 0
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		json.NewEncoder(w).Encode(map[string]interface{}{"version": 1, "blob": "{}"})
	}))
	defer srv2.Close()
	h.syncClientInst = ocodesync.NewClient(srv2.URL)

	w2 := httptest.NewRecorder()
	r2 := httptest.NewRequest("POST", "/api/sync/sync", nil)
	h.HandleSyncNow(w2, r2)

	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200 on retry, got %d: %s", w2.Code, w2.Body.String())
	}
	if hits == 0 {
		t.Error("expected subsequent sync to reach the server after flag reset")
	}
}

// TestSyncNowNotLoggedInReturnsConflict verifies Sync now refuses to run
// without a stored token and contacts no sync server.
func TestSyncNowNotLoggedInReturnsConflict(t *testing.T) {
	setHomeTree(t, t.TempDir())

	var mu sync.Mutex
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()
		json.NewEncoder(w).Encode(map[string]interface{}{"version": 1, "blob": "{}"})
	}))
	defer srv.Close()

	h := NewHandler()
	h.syncClientInst = ocodesync.NewClient(srv.URL)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/sync/sync", nil)
	h.HandleSyncNow(w, r)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409 when not logged in, got %d: %s", w.Code, w.Body.String())
	}
	mu.Lock()
	defer mu.Unlock()
	if hits != 0 {
		t.Errorf("expected no requests to the sync server, got %d", hits)
	}
}

// TestSetSyncURLChangeSignsOutAndSuspendsSync verifies that changing the sync
// URL revokes and clears the token issued by the old server, so a following
// Sync now sends nothing to the new server.
func TestSetSyncURLChangeSignsOutAndSuspendsSync(t *testing.T) {
	setHomeTree(t, t.TempDir())

	var mu sync.Mutex
	var oldPaths []string
	oldHub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		oldPaths = append(oldPaths, r.URL.Path)
		mu.Unlock()
		json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}))
	defer oldHub.Close()
	newHits := 0
	newHub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		newHits++
		mu.Unlock()
		json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}))
	defer newHub.Close()

	if err := ocodesync.SaveToken("tok-old"); err != nil {
		t.Fatalf("SaveToken: %v", err)
	}

	h := NewHandler()
	h.mu.Lock()
	h.cfg = &config.Config{Ocode: config.OcodeConfig{SyncURL: oldHub.URL}}
	h.mu.Unlock()
	h.syncMu.Lock()
	h.syncClientInst = ocodesync.NewClient(oldHub.URL)
	h.syncConfiguredURL = oldHub.URL
	h.syncMu.Unlock()

	w := httptest.NewRecorder()
	r := httptest.NewRequest("PUT", "/api/config/ocode/sync-url", strings.NewReader(`{"sync_url":"`+newHub.URL+`"}`))
	h.HandleSetSyncURLConfig(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on URL change, got %d: %s", w.Code, w.Body.String())
	}
	if _, ok, _ := ocodesync.LoadToken(); ok {
		t.Fatal("expected token cleared after sync URL change")
	}
	mu.Lock()
	revokedOnOld := false
	for _, p := range oldPaths {
		if p == "/api/ocode/device/revoke" {
			revokedOnOld = true
		}
	}
	mu.Unlock()
	if !revokedOnOld {
		t.Error("expected the old token to be revoked on the old server")
	}

	w2 := httptest.NewRecorder()
	r2 := httptest.NewRequest("POST", "/api/sync/sync", nil)
	h.HandleSyncNow(w2, r2)
	if w2.Code != http.StatusConflict {
		t.Fatalf("expected 409 for Sync now after URL change, got %d: %s", w2.Code, w2.Body.String())
	}
	mu.Lock()
	defer mu.Unlock()
	if newHits != 0 {
		t.Errorf("expected nothing sent to the new server, got %d requests", newHits)
	}
}

// TestSetSyncURLSameURLKeepsToken verifies that saving the sync URL already in
// effect leaves the stored token and the client alone.
func TestSetSyncURLSameURLKeepsToken(t *testing.T) {
	setHomeTree(t, t.TempDir())

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request to sync server: %s %s", r.Method, r.URL.Path)
	}))
	defer srv.Close()

	if err := ocodesync.SaveToken("tok-keep"); err != nil {
		t.Fatalf("SaveToken: %v", err)
	}

	h := NewHandler()
	h.mu.Lock()
	h.cfg = &config.Config{Ocode: config.OcodeConfig{SyncURL: srv.URL}}
	h.mu.Unlock()
	seeded := ocodesync.NewClient(srv.URL)
	h.syncMu.Lock()
	h.syncClientInst = seeded
	h.syncConfiguredURL = srv.URL
	h.syncMu.Unlock()

	w := httptest.NewRecorder()
	r := httptest.NewRequest("PUT", "/api/config/ocode/sync-url", strings.NewReader(`{"sync_url":"`+srv.URL+`"}`))
	h.HandleSetSyncURLConfig(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	token, ok, err := ocodesync.LoadToken()
	if err != nil || !ok || token != "tok-keep" {
		t.Errorf("expected token tok-keep kept, got %q ok=%v err=%v", token, ok, err)
	}
	h.syncMu.Lock()
	defer h.syncMu.Unlock()
	if h.syncClientInst != seeded {
		t.Error("expected the sync client to be kept when the URL is unchanged")
	}
}
