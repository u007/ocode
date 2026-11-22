package server

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/auth"
	"github.com/u007/ocode/internal/boxsession"
	"github.com/u007/ocode/internal/browse/cdp"
	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/session"
	"github.com/u007/ocode/internal/tool"
	"github.com/u007/ocode/internal/upstashbox"
)

// testBoxAccount is the account id the box tests configure.
const testBoxAccount = "ub_test"

// boxTestHome points HOME at a fresh tree and reloads the auth store from it, so
// keys from earlier tests cannot leak in. The reload is registered before
// setHomeTree, so it runs after HOME is restored (LIFO), as upstashTestServer does.
func boxTestHome(t *testing.T) {
	t.Helper()
	prevVersion := auth.CredentialVersion()
	t.Cleanup(func() {
		auth.SetCredentialVersionForTest(prevVersion)
		_ = auth.LoadStore()
	})
	setHomeTree(t, t.TempDir())
	if err := auth.LoadStore(); err != nil {
		t.Fatalf("auth.LoadStore: %v", err)
	}
}

// fakeBoxAPI is a boxsession.BoxAPI that records every delete and never touches
// the network. Create makes box-1, box-2, ... with the labels it was given.
type fakeBoxAPI struct {
	created   int
	deleted   []string
	deleteErr error
	// gate, when non-nil, holds every Delete until it is closed. entered receives
	// each box id as its Delete starts, and released receives it after the id is
	// recorded in deleted. A test waits on these channels instead of sleeping.
	gate     chan struct{}
	entered  chan string
	released chan string
}

var _ boxsession.BoxAPI = (*fakeBoxAPI)(nil)

func (f *fakeBoxAPI) Create(_ context.Context, opts upstashbox.CreateOptions) (*upstashbox.Box, error) {
	f.created++
	now := time.Now().UTC()
	return &upstashbox.Box{
		ID:        fmt.Sprintf("box-%d", f.created),
		Labels:    opts.Labels,
		Status:    "running",
		Ephemeral: true,
		CreatedAt: now,
		ExpiresAt: now.Add(time.Hour),
	}, nil
}

func (f *fakeBoxAPI) Delete(_ context.Context, id string) error {
	if f.entered != nil {
		f.entered <- id
	}
	if f.gate != nil {
		<-f.gate
	}
	f.deleted = append(f.deleted, id)
	if f.released != nil {
		f.released <- id
	}
	return f.deleteErr
}

func (f *fakeBoxAPI) CDPURL(_ context.Context, _ string) (upstashbox.CDPURL, error) {
	return upstashbox.CDPURL{}, nil
}

// Get and Pause are for retained boxes, which these tests never start. They fail
// loudly if a retained path reaches them.
func (f *fakeBoxAPI) Get(_ context.Context, id string) (*upstashbox.Box, error) {
	return nil, fmt.Errorf("fakeBoxAPI: Get(%s): no retained box is expected in this test", id)
}

func (f *fakeBoxAPI) Pause(_ context.Context, id string) error {
	return fmt.Errorf("fakeBoxAPI: Pause(%s): no retained box is expected in this test", id)
}

// newTestBoxManager builds a manager over api, journaled at the test home's journal.
func newTestBoxManager(t *testing.T, api *fakeBoxAPI, label string) *boxsession.Manager {
	t.Helper()
	path, err := upstashBoxJournalPath()
	if err != nil {
		t.Fatal(err)
	}
	return boxsession.New(boxsession.Deps{
		Account: func() (boxsession.Account, error) {
			return boxsession.Account{ID: testBoxAccount, APIKey: "ubx-test-key", Idle: time.Hour, HardCap: 2 * time.Hour}, nil
		},
		APIKeyFor: func(string) (string, error) { return "ubx-test-key", nil },
		NewAPI:    func(string) (boxsession.BoxAPI, error) { return api, nil },
		Dial: func(context.Context, string) (*cdp.Conn, error) {
			return nil, errors.New("tests do not dial a browser")
		},
		Busy:    func(string) bool { return false },
		Journal: boxsession.NewJournal(path),
		Label:   label,
	})
}

// fakeBoxChrome records the surface calls a boxSurfaces makes.
type fakeBoxChrome struct {
	calls []string
	err   error
}

func (f *fakeBoxChrome) EnsureHeadless(_ context.Context, key string) (*cdp.Target, bool, error) {
	f.calls = append(f.calls, "ensure:"+key)
	return nil, false, f.err
}

func (f *fakeBoxChrome) OpenHeadless(context.Context) (*cdp.Target, string, error) {
	f.calls = append(f.calls, "open")
	return nil, "tab:new", f.err
}

func (f *fakeBoxChrome) CloseTarget(_ context.Context, key string) error {
	f.calls = append(f.calls, "close:"+key)
	return f.err
}

// fakeBoxPage is a boxPage whose navigation and readyState are scripted.
type fakeBoxPage struct {
	navErr   error
	state    string
	stateErr error
}

func (p *fakeBoxPage) Navigate(context.Context, string) error { return p.navErr }

func (p *fakeBoxPage) ReadyState(context.Context) (string, error) { return p.state, p.stateErr }

func TestBoxSurfacesDiscardsAFailedNavigation(t *testing.T) {
	chrome := &fakeBoxChrome{}
	s := boxSurfaces{chrome: chrome}
	_, err := s.loadAttached(context.Background(), "tab:a", "https://example.com/", &fakeBoxPage{navErr: errors.New("net down")})
	if err == nil {
		t.Fatal("a failed navigation must be an error")
	}
	if len(chrome.calls) != 1 || chrome.calls[0] != "close:tab:a" {
		t.Fatalf("the attached target must be discarded, calls = %v", chrome.calls)
	}
}

func TestBoxSurfacesDiscardsAFailedWait(t *testing.T) {
	chrome := &fakeBoxChrome{}
	s := boxSurfaces{chrome: chrome}
	_, err := s.loadAttached(context.Background(), "tab:b", "https://example.com/", &fakeBoxPage{state: "", stateErr: errors.New("cdp gone")})
	if err == nil {
		t.Fatal("a wait that fails must be an error")
	}
	if len(chrome.calls) != 1 || chrome.calls[0] != "close:tab:b" {
		t.Fatalf("the attached target must be discarded, calls = %v", chrome.calls)
	}
}

func TestBoxSurfacesKeepsALoadedTarget(t *testing.T) {
	chrome := &fakeBoxChrome{}
	s := boxSurfaces{chrome: chrome}
	note, err := s.loadAttached(context.Background(), "tab:c", "https://example.com/", &fakeBoxPage{state: "complete"})
	if err != nil || note != "" {
		t.Fatalf("a loaded page: note %q err %v, want neither", note, err)
	}
	if len(chrome.calls) != 0 {
		t.Fatalf("a loaded target must not be closed, calls = %v", chrome.calls)
	}
}

func TestBoxSurfacesOpenAndCloseUseTheBoxChrome(t *testing.T) {
	chrome := &fakeBoxChrome{}
	s := boxSurfaces{chrome: chrome}
	key, err := s.Open(context.Background(), "")
	if err != nil || key != "tab:new" {
		t.Fatalf("Open = %q, %v; want tab:new, nil", key, err)
	}
	if err := s.Close(context.Background(), "tab:new"); err != nil {
		t.Fatal(err)
	}
	if strings.Join(chrome.calls, ",") != "open,close:tab:new" {
		t.Fatalf("calls = %v", chrome.calls)
	}
}

// TestHandlerBoxesNeverBuildsAManagerForAnAbsentBox pins that a backend=upstash
// call on a process with no box manager reports ErrNotStarted and builds nothing.
func TestHandlerBoxesNeverBuildsAManagerForAnAbsentBox(t *testing.T) {
	boxTestHome(t)
	h := NewHandler()
	b := handlerBoxes{h: h}
	if _, _, err := b.Surfaces(context.Background(), "ses_x"); !errors.Is(err, boxsession.ErrNotStarted) {
		t.Fatalf("Surfaces = %v, want ErrNotStarted", err)
	}
	if _, err := b.Terminate(context.Background(), "ses_x"); !errors.Is(err, boxsession.ErrNotStarted) {
		t.Fatalf("Terminate = %v, want ErrNotStarted", err)
	}
	if h.boxManagerIfBuilt() != nil {
		t.Fatal("a Surfaces or Terminate call built a box manager")
	}
}

// TestHandlerBoxesStartSurfacesTerminateThroughTheManager walks one box through
// its lifecycle on a fake API: start, reuse, surfaces, terminate.
func TestHandlerBoxesStartSurfacesTerminateThroughTheManager(t *testing.T) {
	boxTestHome(t)
	api := &fakeBoxAPI{}
	h := NewHandler()
	h.boxes = newTestBoxManager(t, api, "ocode-aaaaaaaa")
	b := handlerBoxes{h: h}
	ctx := context.Background()

	st, err := b.Start(ctx, "ses_one", false)
	if err != nil || st.BoxID != "box-1" || st.Reused || st.Kind != boxsession.KindEphemeral {
		t.Fatalf("Start = %+v, %v; want an ephemeral box-1 created", st, err)
	}
	again, err := b.Start(ctx, "ses_one", false)
	if err != nil || again.BoxID != "box-1" || !again.Reused {
		t.Fatalf("second Start = %+v, %v; want box-1 reused", again, err)
	}
	if _, _, err := b.Surfaces(ctx, "ses_one"); err != nil {
		t.Fatalf("Surfaces of a running box: %v", err)
	}
	kind, err := b.Terminate(ctx, "ses_one")
	if err != nil {
		t.Fatalf("Terminate: %v", err)
	}
	if kind != boxsession.KindEphemeral {
		t.Fatalf("Terminate reported kind %q, want %q", kind, boxsession.KindEphemeral)
	}
	if len(api.deleted) != 1 || api.deleted[0] != "box-1" {
		t.Fatalf("the box was not deleted through the API: %v", api.deleted)
	}
	if _, _, err := b.Surfaces(ctx, "ses_one"); !errors.Is(err, boxsession.ErrNotStarted) {
		t.Fatalf("Surfaces after terminate = %v, want ErrNotStarted", err)
	}
}

// TestBoxManagerBuildNeedsNoAccount pins that building the manager is free of
// account checks: the missing account is reported by Start, with the user's message.
func TestBoxManagerBuildNeedsNoAccount(t *testing.T) {
	boxTestHome(t)
	h := NewHandler()
	t.Cleanup(func() { h.shutdownBoxes(context.Background()) })
	mgr, err := h.boxManager()
	if err != nil {
		t.Fatalf("boxManager with no account: %v", err)
	}
	if h.boxManagerIfBuilt() != mgr {
		t.Fatal("boxManagerIfBuilt does not return the built manager")
	}
	// A session that starts a box is always registered. Start resolves its login
	// state before the account, so the session must be known for the account error to surface.
	h.sessions.Register("ses_none", t.TempDir())
	_, err = handlerBoxes{h: h}.Start(context.Background(), "ses_none", false)
	if err == nil || !strings.Contains(err.Error(), "no Upstash Box account is configured") {
		t.Fatalf("Start with no account = %v, want the add-an-account message", err)
	}
}

// TestBoxSweepStartsOnlyWithJournalRows pins the startup rule: the sweep runs when
// the journal holds rows, and an absent or empty journal starts nothing.
func TestBoxSweepStartsOnlyWithJournalRows(t *testing.T) {
	boxTestHome(t)
	path, err := upstashBoxJournalPath()
	if err != nil {
		t.Fatal(err)
	}

	empty := NewHandler()
	empty.resumeBoxSweepIfJournaled()
	if empty.boxManagerIfBuilt() != nil {
		t.Fatal("an absent journal built a manager")
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	emptyFile := NewHandler()
	emptyFile.resumeBoxSweepIfJournaled()
	if emptyFile.boxManagerIfBuilt() != nil {
		t.Fatal("an empty journal built a manager")
	}

	j := boxsession.NewJournal(path)
	if err := j.Add(boxsession.Entry{BoxID: "box-old", AccountID: testBoxAccount, SessionID: "ses_old", Kind: boxsession.KindEphemeral, Label: "ocode-aaaaaaaa", CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	swept := NewHandler()
	t.Cleanup(func() { swept.shutdownBoxes(context.Background()) })
	swept.resumeBoxSweepIfJournaled()
	if swept.boxManagerIfBuilt() == nil {
		t.Fatal("a journal with rows did not start the sweep")
	}
}

// TestHandlerBoxesDefaultBackendFollowsTheConfig pins the default backend read.
func TestHandlerBoxesDefaultBackendFollowsTheConfig(t *testing.T) {
	boxTestHome(t)
	b := handlerBoxes{h: NewHandler()}
	if got, err := b.DefaultBackend(); err != nil || got != browserBackendLocal {
		t.Fatalf("no config: DefaultBackend = %q, %v; want local", got, err)
	}
	addTestBoxAccount(t, testBoxAccount, "main")
	upstash := "upstash"
	if err := config.MutateUpstashBox(func(c *config.UpstashBoxConfig) error {
		return c.SetSettings(&upstash, nil)
	}); err != nil {
		t.Fatal(err)
	}
	if got, err := b.DefaultBackend(); err != nil || got != browserBackendUpstash {
		t.Fatalf("upstash configured: DefaultBackend = %q, %v; want upstash", got, err)
	}
}

// TestHandlerBoxesDefaultBackendErrorsOnAnUnreadableConfig pins that an unreadable
// config is an error, never a guessed backend: a guess to local would silently
// replace the box the user chose, and a guess to upstash would bill a box they did
// not ask for.
func TestHandlerBoxesDefaultBackendErrorsOnAnUnreadableConfig(t *testing.T) {
	boxTestHome(t)
	path, err := config.ActiveOcodeConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := (handlerBoxes{h: NewHandler()}).DefaultBackend()
	if err == nil {
		t.Fatalf("unreadable config: DefaultBackend = %q with no error, want an error", got)
	}
	if got != "" {
		t.Fatalf("unreadable config: DefaultBackend = %q alongside the error, want the empty string", got)
	}
}

// TestHandlerBoxesSurfacesCarryTheBoxIDAcrossAReplacement pins the real adapter's
// box id: it is the running box's id, and a new start after a terminate reports the
// new box's id, so the tool can tell the two apart.
func TestHandlerBoxesSurfacesCarryTheBoxIDAcrossAReplacement(t *testing.T) {
	boxTestHome(t)
	api := &fakeBoxAPI{}
	h := NewHandler()
	h.boxes = newTestBoxManager(t, api, "ocode-aaaaaaaa")
	b := handlerBoxes{h: h}
	ctx := context.Background()

	if _, err := b.Start(ctx, "ses_swap", false); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if _, id, err := b.Surfaces(ctx, "ses_swap"); err != nil || id != "box-1" {
		t.Fatalf("Surfaces before the replacement = id %q, %v; want box-1", id, err)
	}
	if _, err := b.Terminate(ctx, "ses_swap"); err != nil {
		t.Fatalf("Terminate: %v", err)
	}
	if _, err := b.Start(ctx, "ses_swap", false); err != nil {
		t.Fatalf("second Start: %v", err)
	}
	if _, id, err := b.Surfaces(ctx, "ses_swap"); err != nil || id != "box-2" {
		t.Fatalf("Surfaces after the replacement = id %q, %v; want box-2", id, err)
	}
	if _, _, err := b.Surfaces(ctx, "ses_other"); !errors.Is(err, boxsession.ErrNotStarted) {
		t.Fatalf("Surfaces of a session with no box = %v, want ErrNotStarted", err)
	}
}

// addTestBoxAccount adds an account with a key to the test home.
func addTestBoxAccount(t *testing.T, id, name string) {
	t.Helper()
	if err := config.MutateUpstashBox(func(c *config.UpstashBoxConfig) error {
		return c.AddAccount(config.UpstashBoxAccount{
			ID: id, Name: name, Size: config.UpstashBoxSizeSmall,
			IdleMinutes: 15, HardCapMinutes: 120, RetainDays: 14,
		})
	}); err != nil {
		t.Fatal(err)
	}
	if err := auth.SetUpstashBoxAPIKey(id, "ubx-test-key"); err != nil {
		t.Fatal(err)
	}
}

// TestUpstashBoxAtCreationFollowsAccounts pins the creation flag's rule: on iff an
// account exists at that moment.
func TestUpstashBoxAtCreationFollowsAccounts(t *testing.T) {
	boxTestHome(t)
	on, err := upstashBrowserAtCreation()
	if err != nil || on {
		t.Fatalf("no account: flag %v err %v, want false", on, err)
	}
	addTestBoxAccount(t, testBoxAccount, "main")
	on, err = upstashBrowserAtCreation()
	if err != nil || !on {
		t.Fatalf("one account: flag %v err %v, want true", on, err)
	}
}

// TestUpstashBrowserFromMetadataRejectsANonBool pins the parse: absent is false, a
// bool is its value, and anything else is an error that fails the build.
func TestUpstashBrowserFromMetadataRejectsANonBool(t *testing.T) {
	if on, err := upstashBrowserFromMetadata(map[string]any{}); err != nil || on {
		t.Fatalf("absent: %v, %v", on, err)
	}
	if on, err := upstashBrowserFromMetadata(map[string]any{upstashBrowserMetadataKey: true}); err != nil || !on {
		t.Fatalf("true: %v, %v", on, err)
	}
	if _, err := upstashBrowserFromMetadata(map[string]any{upstashBrowserMetadataKey: "yes"}); err == nil {
		t.Fatal("a non-bool flag must be an error")
	}
}

// TestBoxBusyReportsTurnsAndAsks pins Busy: a turn in flight, a held session lock,
// or a pending permission ask is busy; an idle session is not.
func TestBoxBusyReportsTurnsAndAsks(t *testing.T) {
	boxTestHome(t)
	h := NewHandler()
	const sid = "ses_busy"
	if h.boxBusy(sid) {
		t.Fatal("an unknown session is busy")
	}

	h.cancelMu.Lock()
	h.turnInFlight = map[string]int{sid: 1}
	h.cancelMu.Unlock()
	if !h.boxBusy(sid) {
		t.Fatal("a turn in flight is not busy")
	}
	h.cancelMu.Lock()
	h.turnInFlight = map[string]int{}
	h.cancelMu.Unlock()

	as := &agentSession{}
	h.mu.Lock()
	h.agents[agentKey(sid)] = as
	h.mu.Unlock()
	as.mu.Lock()
	if !h.boxBusy(sid) {
		t.Fatal("a session whose lock is held is not busy")
	}
	as.mu.Unlock()

	as.messages = []agent.Message{{Role: "tool", Content: tool.SentinelPermissionAsk + " pending"}}
	if !h.boxBusy(sid) {
		t.Fatal("a pending permission ask is not busy")
	}
	as.messages = nil
	if h.boxBusy(sid) {
		t.Fatal("an idle session is busy")
	}
}

// boxTestWait bounds a wait on a background box teardown. It is a failure deadline,
// not a sleep: a teardown that finishes returns at once.
const boxTestWait = 20 * time.Second

// TestCloseSessionTerminatesItsBox pins the close hook: the session's box is deleted
// in the background, and the close still succeeds. The test waits for the delete
// on the fake's channel, not by sleeping.
func TestCloseSessionTerminatesItsBox(t *testing.T) {
	boxTestHome(t)
	api := &fakeBoxAPI{released: make(chan string, 1)}
	h := NewHandler()
	// The teardown goes on after the delete (the journal row is removed then), so
	// the test waits for it before its temp home is removed.
	t.Cleanup(h.boxCloses.Wait)
	proj := t.TempDir()
	const sid = "ses_close"
	if _, err := h.sessions.BindNewOrVerify(sid, proj, ""); err != nil {
		t.Fatal(err)
	}
	mgr := newTestBoxManager(t, api, "ocode-aaaaaaaa")
	h.boxes = mgr
	if _, err := mgr.Start(context.Background(), sid, boxsession.StartOptions{}); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	h.HandleCloseSession(rec, httptest.NewRequest(http.MethodPost, "/api/sessions/"+sid+"/close", nil), sid)
	if rec.Code != http.StatusOK {
		t.Fatalf("close status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	select {
	case id := <-api.released:
		if id != "box-1" {
			t.Fatalf("close deleted box %q, want box-1", id)
		}
	case <-time.After(boxTestWait):
		t.Fatal("close never deleted the session's box")
	}
	if len(api.deleted) != 1 || api.deleted[0] != "box-1" {
		t.Fatalf("close did not delete the session's box: %v", api.deleted)
	}
}

// TestCloseSessionDoesNotWaitForTheBoxDelete pins that the close request returns
// while the box delete is still in flight. The fake holds the delete until the test
// releases it, so a close that waited for the delete never returns and the test fails
// at the bound.
func TestCloseSessionDoesNotWaitForTheBoxDelete(t *testing.T) {
	boxTestHome(t)
	gate := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(gate) }) }
	api := &fakeBoxAPI{gate: gate, entered: make(chan string, 1), released: make(chan string, 1)}
	h := NewHandler()
	// Never leave the held delete blocked, even when the test fails first, and wait
	// out the teardown before the temp home is removed. Cleanups run last-in first-out.
	t.Cleanup(func() {
		release()
		h.boxCloses.Wait()
	})
	proj := t.TempDir()
	const sid = "ses_close_held"
	if _, err := h.sessions.BindNewOrVerify(sid, proj, ""); err != nil {
		t.Fatal(err)
	}
	mgr := newTestBoxManager(t, api, "ocode-aaaaaaaa")
	h.boxes = mgr
	if _, err := mgr.Start(context.Background(), sid, boxsession.StartOptions{}); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	returned := make(chan struct{})
	go func() {
		defer close(returned)
		h.HandleCloseSession(rec, httptest.NewRequest(http.MethodPost, "/api/sessions/"+sid+"/close", nil), sid)
	}()
	select {
	case <-returned:
	case <-time.After(boxTestWait):
		t.Fatal("close did not return while the box delete was held: the request waits for the teardown")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("close status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	select {
	case id := <-api.entered:
		if id != "box-1" {
			t.Fatalf("the background delete started for %q, want box-1", id)
		}
	case <-time.After(boxTestWait):
		t.Fatal("close never started the background box delete")
	}
	release()
	select {
	case <-api.released:
	case <-time.After(boxTestWait):
		t.Fatal("the released box delete never finished")
	}
}

// TestTerminateSessionBoxEndsWithTheTeardown pins that the done channel of the
// background teardown closes only after the box is deleted and no longer running.
func TestTerminateSessionBoxEndsWithTheTeardown(t *testing.T) {
	boxTestHome(t)
	api := &fakeBoxAPI{}
	h := NewHandler()
	const sid = "ses_teardown_done"
	mgr := newTestBoxManager(t, api, "ocode-aaaaaaaa")
	h.boxes = mgr
	if _, err := mgr.Start(context.Background(), sid, boxsession.StartOptions{}); err != nil {
		t.Fatal(err)
	}

	done := h.terminateSessionBox(sid)
	select {
	case <-done:
	case <-time.After(boxTestWait):
		t.Fatal("the background teardown never ended")
	}
	if len(api.deleted) != 1 || api.deleted[0] != "box-1" {
		t.Fatalf("the teardown did not delete the box: %v", api.deleted)
	}
	if got := mgr.Statuses(); len(got) != 0 {
		t.Fatalf("a box is still running after the teardown: %+v", got)
	}
}

// TestShutdownDeletesOwnedBoxes pins the shutdown hook.
func TestShutdownDeletesOwnedBoxes(t *testing.T) {
	boxTestHome(t)
	api := &fakeBoxAPI{}
	h := NewHandler()
	mgr := newTestBoxManager(t, api, "ocode-aaaaaaaa")
	h.boxes = mgr
	if _, err := mgr.Start(context.Background(), "ses_shut", boxsession.StartOptions{}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	h.Shutdown(ctx)
	if len(api.deleted) != 1 || api.deleted[0] != "box-1" {
		t.Fatalf("shutdown did not delete the owned box: %v", api.deleted)
	}
}

// TestResetSessionIDMovesItsBox pins the reset-id hook: the box follows the new id.
func TestResetSessionIDMovesItsBox(t *testing.T) {
	boxTestHome(t)
	h := NewHandler()
	proj := t.TempDir()
	h.projects = newTestProjectStore(t, proj)
	h.SetWorkDir(proj)
	session.SetWorkDir(proj)
	t.Cleanup(func() { session.SetWorkDir("") })

	id := session.NewSessionID()
	if err := session.Save(id, "Box chat", []agent.Message{{Role: "user", Content: "hi"}}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := h.sessions.BindNewOrVerify(id, proj, ""); err != nil {
		t.Fatal(err)
	}
	mgr := newTestBoxManager(t, &fakeBoxAPI{}, "ocode-aaaaaaaa")
	h.boxes = mgr
	if _, err := mgr.Start(context.Background(), id, boxsession.StartOptions{}); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	h.HandleResetSessionID(rec, httptest.NewRequest(http.MethodPost, "/api/sessions/"+id+"/reset-id", nil), id)
	if rec.Code != http.StatusOK {
		t.Fatalf("reset-id status = %d (%s)", rec.Code, rec.Body.String())
	}
	var out map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	st := mgr.Statuses()
	if len(st) != 1 || st[0].SessionID != out["new_id"] {
		t.Fatalf("the box did not follow the new id %q: %+v", out["new_id"], st)
	}
}

// TestBuildAgentSessionBoxBackendFollowsTheCreationFlag is the tools-array
// stability test. A session with the flag gets the box backend; a session without
// it keeps the byte-identical local definition, and changing the accounts later
// changes neither.
func TestBuildAgentSessionBoxBackendFollowsTheCreationFlag(t *testing.T) {
	boxTestHome(t)
	h := NewHandler()
	h.browseSurfaces = &fakeSurfaces{}
	proj := t.TempDir()

	plain := session.NewSessionID()
	if err := session.SaveForDir(proj, plain, "", nil, nil); err != nil {
		t.Fatal(err)
	}
	withBoxes := session.NewSessionID()
	if err := session.SaveForDir(proj, withBoxes, "", nil, map[string]any{upstashBrowserMetadataKey: true}); err != nil {
		t.Fatal(err)
	}

	build := func(id string) *agentSession {
		t.Helper()
		as, stage, err := h.buildAgentSession(id, "opencode-go/deepseek-v4-flash", nil, proj)
		if err != nil {
			t.Fatalf("build %s: %v (stage %s)", id, err, stage)
		}
		t.Cleanup(as.agent.Shutdown)
		return as
	}
	browserDef := func(as *agentSession) map[string]any {
		t.Helper()
		bt, ok := as.agent.GetTool("browser")
		if !ok {
			t.Fatal("the session has no browser tool")
		}
		return bt.Definition()
	}
	assertLocal := func(label string, def map[string]any) {
		t.Helper()
		raw, err := json.Marshal(def)
		if err != nil {
			t.Fatal(err)
		}
		if got := fmt.Sprintf("%x", sha256.Sum256(raw)); got != browserDefinitionSHA {
			t.Fatalf("%s: browser definition is not the local one: sha256 %s, want %s", label, got, browserDefinitionSHA)
		}
	}

	assertLocal("no account, no flag", browserDef(build(plain)))

	addTestBoxAccount(t, testBoxAccount, "main")
	assertLocal("account added later, no flag", browserDef(build(plain)))

	boxed := browserDef(build(withBoxes))
	if raw, _ := json.Marshal(boxed); !strings.Contains(string(raw), `"backend"`) {
		t.Fatalf("a flagged session has no backend property: %s", raw)
	}

	if err := config.MutateUpstashBox(func(c *config.UpstashBoxConfig) error {
		return c.RemoveAccount(testBoxAccount)
	}); err != nil {
		t.Fatal(err)
	}
	if raw, _ := json.Marshal(browserDef(build(withBoxes))); !strings.Contains(string(raw), `"backend"`) {
		t.Fatalf("removing the account dropped a flagged session's backend: %s", raw)
	}
}

// TestBuildAgentSessionRefusesAnUnreadableFlag pins that a malformed flag fails the
// build at the flag's stage.
func TestBuildAgentSessionRefusesAnUnreadableFlag(t *testing.T) {
	boxTestHome(t)
	h := NewHandler()
	h.browseSurfaces = &fakeSurfaces{}
	proj := t.TempDir()
	id := session.NewSessionID()
	if err := session.SaveForDir(proj, id, "", nil, map[string]any{upstashBrowserMetadataKey: "yes"}); err != nil {
		t.Fatal(err)
	}
	_, stage, err := h.buildAgentSession(id, "opencode-go/deepseek-v4-flash", nil, proj)
	if err == nil || stage != "upstash-browser" {
		t.Fatalf("malformed flag: stage %q err %v, want the upstash-browser stage failing", stage, err)
	}
}

// TestBuildAgentSessionRefusesAnUnreadableSessionFile pins that a session whose
// transcript exists but cannot be read fails the build at the session-load stage. The
// flag must not read as absent: that would drop a box backend the session was
// created with, and change its tools array mid-session.
func TestBuildAgentSessionRefusesAnUnreadableSessionFile(t *testing.T) {
	boxTestHome(t)
	h := NewHandler()
	h.browseSurfaces = &fakeSurfaces{}
	proj := t.TempDir()
	id := session.NewSessionID()
	if err := session.SaveForDir(proj, id, "", nil, map[string]any{upstashBrowserMetadataKey: true}); err != nil {
		t.Fatal(err)
	}
	dir, err := session.GetStorageDirForPath(proj)
	if err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(filepath.Join(dir, id+"*"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no transcript file for %s in %s: %v", id, dir, err)
	}
	for _, f := range files {
		if err := os.WriteFile(f, []byte("not a session"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	as, stage, err := h.buildAgentSession(id, "opencode-go/deepseek-v4-flash", nil, proj)
	if err == nil || stage != "session-load" {
		if as != nil {
			as.agent.Shutdown()
		}
		t.Fatalf("unreadable transcript: stage %q err %v, want the session-load stage failing", stage, err)
	}
}

// TestReconcileBoxesDeletesOnlyOldOrphans covers the crash window between a box
// create and its journal rename: a labelled box older than the grace with no
// journal row is deleted, while a journaled box and a just-created one are kept.
func TestReconcileBoxesDeletesOnlyOldOrphans(t *testing.T) {
	mux, h := upstashTestServer(t)
	id := upstashCreate(t, mux, "Work", "ubx-k-reconcile123456")
	fake := &fakeUpstashBoxAPI{}
	wireUpstashBoxClient(t, h, fake)
	label, err := h.boxInstallationLabel()
	if err != nil {
		t.Fatalf("boxInstallationLabel: %v", err)
	}
	now := time.Now()
	fake.boxes = []map[string]any{
		wireBox("orphan-old", now.Add(-time.Hour), true, []string{label}),
		wireBox("known", now.Add(-time.Hour), true, []string{label}),
		wireBox("orphan-young", now, true, []string{label}),
	}
	journal, err := upstashBoxJournal()
	if err != nil {
		t.Fatal(err)
	}
	if err := journal.Add(boxsession.Entry{
		BoxID: "known", AccountID: id, Kind: boxsession.KindEphemeral, SessionID: "ses-1",
	}); err != nil {
		t.Fatalf("journal.Add: %v", err)
	}

	h.reconcileOrphanBoxes(context.Background())

	var deleted []string
	for _, c := range fake.calls {
		if strings.HasPrefix(c, "DELETE /v2/box/") {
			deleted = append(deleted, strings.TrimPrefix(c, "DELETE /v2/box/"))
		}
	}
	if len(deleted) != 1 || deleted[0] != "orphan-old" {
		t.Fatalf("deleted = %v, want [orphan-old]", deleted)
	}
}
