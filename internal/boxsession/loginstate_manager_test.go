package boxsession

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/u007/ocode/internal/browse/cdp"
	"github.com/u007/ocode/internal/upstashbox"
)

// eventLog is an ordered record of the calls the fakes receive. The fake browser
// and the wrapped box API share one log, so the order of a save and a delete can
// be asserted.
type eventLog struct {
	mu     sync.Mutex
	events []string
}

func (l *eventLog) add(s string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, s)
}

func (l *eventLog) all() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.events)
}

func (l *eventLog) count(s string) int {
	n := 0
	for _, e := range l.all() {
		if e == s {
			n++
		}
	}
	return n
}

// index returns the position of the first event equal to s, or -1.
func (l *eventLog) index(s string) int {
	return slices.Index(l.all(), s)
}

// fakeBrowser is a boxBrowser that records its calls and returns what the test sets.
type fakeBrowser struct {
	ev *eventLog

	mu            sync.Mutex
	cookies       []json.RawMessage
	cookieErr     error
	snap          map[string]map[string]string
	snapErr       error
	setCookiesErr error
	// setCookiesRejected and seedSkipped are what the browser reports back from a restore.
	setCookiesRejected int
	seedSkipped        []string
	seedErr            error
	closeErr           error
	// gotCookies and gotSeed are the last arguments the restore passed in.
	gotCookies []json.RawMessage
	gotSeed    map[string]map[string]string
	// onRead, when set, runs first in StorageCookies and may block. A non-nil
	// error it returns is the read's error.
	onRead func(ctx context.Context) error
}

func (f *fakeBrowser) Close(ctx context.Context) error {
	f.ev.add("close-browser")
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closeErr
}

func (f *fakeBrowser) StorageCookies(ctx context.Context) ([]json.RawMessage, error) {
	f.ev.add("read-cookies")
	if f.onRead != nil {
		if err := f.onRead(ctx); err != nil {
			return nil, err
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.cookieErr != nil {
		return nil, f.cookieErr
	}
	return slices.Clone(f.cookies), nil
}

func (f *fakeBrowser) SetStorageCookies(ctx context.Context, cookies []json.RawMessage) (int, error) {
	f.ev.add("set-cookies")
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gotCookies = slices.Clone(cookies)
	return f.setCookiesRejected, f.setCookiesErr
}

func (f *fakeBrowser) LocalStorageSnapshot(ctx context.Context) (map[string]map[string]string, error) {
	f.ev.add("read-storage")
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make(map[string]map[string]string, len(f.snap))
	for o, items := range f.snap {
		out[o] = maps.Clone(items)
	}
	return out, f.snapErr
}

func (f *fakeBrowser) SetLocalStorageSeed(ctx context.Context, seed map[string]map[string]string) ([]string, error) {
	f.ev.add("set-seed")
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gotSeed = seed
	return f.seedSkipped, f.seedErr
}

// fakeTimers records the checkpoint timers the manager arms. Tests fire them
// explicitly, so nothing waits on the wall clock.
type fakeTimers struct {
	mu   sync.Mutex
	list []*fakeTimer
}

func (ft *fakeTimers) afterFunc(d time.Duration, f func()) checkpointTimer {
	t := &fakeTimer{d: d, f: f}
	ft.mu.Lock()
	defer ft.mu.Unlock()
	ft.list = append(ft.list, t)
	return t
}

func (ft *fakeTimers) all() []*fakeTimer {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	return slices.Clone(ft.list)
}

type fakeTimer struct {
	d time.Duration
	f func()

	mu      sync.Mutex
	stopped bool
}

func (t *fakeTimer) Stop() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	was := !t.stopped
	t.stopped = true
	return was
}

// fire runs the callback unless Stop came first, as time.AfterFunc does.
func (t *fakeTimer) fire() {
	t.mu.Lock()
	stopped := t.stopped
	t.mu.Unlock()
	if !stopped {
		t.f()
	}
}

// fireLate runs the callback even after Stop. It models a timer that fired just
// before Stop was called, so its callback is already on its way.
func (t *fakeTimer) fireLate() {
	t.f()
}

// loginRig is a harness with login states switched on. The box API is wrapped so
// Create and Delete land in the shared event log, and Delete can be held open.
type loginRig struct {
	t       *testing.T
	h       *harness
	store   *LoginStateStore
	ev      *eventLog
	browser *fakeBrowser
	timers  *fakeTimers

	mu        sync.Mutex
	assign    map[string]string
	lookupErr error

	// onCreate runs at the start of each box Create, before it is recorded.
	onCreate func()
	// deleteEntered receives a value when a Delete starts, and deleteGate, when
	// set, holds the Delete until it is closed.
	deleteEntered chan struct{}
	deleteGate    chan struct{}
}

// rigAPI wraps the harness box API. It reads its hooks from the rig at call time.
type rigAPI struct {
	BoxAPI
	r *loginRig
}

func (a rigAPI) Create(ctx context.Context, opts upstashbox.CreateOptions) (*upstashbox.Box, error) {
	if a.r.onCreate != nil {
		a.r.onCreate()
	}
	a.r.ev.add("create")
	return a.BoxAPI.Create(ctx, opts)
}

func (a rigAPI) Delete(ctx context.Context, id string) error {
	a.r.ev.add("delete-start")
	if a.r.deleteEntered != nil {
		a.r.deleteEntered <- struct{}{}
	}
	if a.r.deleteGate != nil {
		<-a.r.deleteGate
	}
	err := a.BoxAPI.Delete(ctx, id)
	a.r.ev.add("delete-done")
	return err
}

// Pause logs its start and end in the shared event log, so a test can check that
// a final save comes before the pause.
func (a rigAPI) Pause(ctx context.Context, id string) error {
	a.r.ev.add("pause-start")
	err := a.BoxAPI.Pause(ctx, id)
	a.r.ev.add("pause-done")
	return err
}

func newLoginRig(t *testing.T) *loginRig {
	t.Helper()
	h := newHarness(t)
	r := &loginRig{
		t:      t,
		h:      h,
		store:  NewLoginStateStore(filepath.Join(t.TempDir(), "login-states")),
		ev:     &eventLog{},
		timers: &fakeTimers{},
		assign: map[string]string{},
	}
	r.store.alive = func(int) bool { return true }
	r.browser = &fakeBrowser{ev: r.ev}
	h.m.deps.LoginStates = r.store
	h.m.deps.LoginStateFor = r.loginStateFor
	h.m.deps.NewAPI = func(apiKey string) (BoxAPI, error) {
		if apiKey == "" {
			return nil, errors.New("empty key")
		}
		return rigAPI{BoxAPI: h.api, r: r}, nil
	}
	h.m.browserOf = func(*cdp.Manager) boxBrowser { return r.browser }
	h.m.afterFunc = r.timers.afterFunc
	h.m.checkpointDelay = loginCheckpointDelay
	return r
}

func (r *loginRig) loginStateFor(sessionID string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.lookupErr != nil {
		return "", r.lookupErr
	}
	return r.assign[sessionID], nil
}

func (r *loginRig) setAssign(sessionID, name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.assign[sessionID] = name
}

// holder returns the session holding the named login state, or "".
func (r *loginRig) holder(name string) string {
	r.t.Helper()
	sid, held, err := r.store.InUseBy(name)
	if err != nil {
		r.t.Fatalf("InUseBy(%s): %v", name, err)
	}
	if !held {
		return ""
	}
	return sid
}

// savedState loads the named state, failing the test when it is missing.
func (r *loginRig) savedState(name string) LoginState {
	r.t.Helper()
	st, found, err := r.store.Load(name)
	if err != nil || !found {
		r.t.Fatalf("Load(%s) = found %v, err %v; want a saved state", name, found, err)
	}
	return st
}

func testCookie(name, value string) json.RawMessage {
	return json.RawMessage(`{"name":"` + name + `","value":"` + value + `","domain":"example.com","path":"/"}`)
}

func cookieNames(t *testing.T, cookies []json.RawMessage) []string {
	t.Helper()
	var names []string
	for _, raw := range cookies {
		var c struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(raw, &c); err != nil {
			t.Fatalf("cookie is not JSON: %v", err)
		}
		names = append(names, c.Name)
	}
	return names
}

func TestLoginStateDepsMustBePaired(t *testing.T) {
	h := newHarness(t)
	d := h.m.deps
	d.LoginStates = NewLoginStateStore(t.TempDir())
	d.LoginStateFor = nil
	if err := d.validate(); err == nil || !strings.Contains(err.Error(), "both be set") {
		t.Fatalf("validate with only LoginStates = %v, want a pairing error", err)
	}
	m := New(d)
	if _, err := m.Start(context.Background(), "ses-1", StartOptions{}); err == nil {
		t.Fatal("Start with unpaired login-state deps succeeded")
	}
	if n := h.api.createCount(); n != 0 {
		t.Fatalf("a misconfigured manager created %d box(es)", n)
	}
}

func TestStartRestoresSavedStateBeforeBoxIsPublished(t *testing.T) {
	r := newLoginRig(t)
	seed := LoginState{
		Name:         "work",
		Cookies:      []json.RawMessage{testCookie("sid", "v1")},
		LocalStorage: map[string]map[string]string{"https://example.com": {"k": "v"}},
	}
	if err := r.store.Save(seed); err != nil {
		t.Fatalf("Save: %v", err)
	}
	r.setAssign("ses-1", "work")
	r.onCreate = func() {
		if r.holder("work") != "ses-1" {
			r.ev.add("create-without-hold")
		}
	}

	st := mustStart(t, r.h.m, "ses-1")
	if st.LoginState != "work" {
		t.Fatalf("Status.LoginState = %q, want work", st.LoginState)
	}
	create, cookies, seedAt := r.ev.index("create"), r.ev.index("set-cookies"), r.ev.index("set-seed")
	if create < 0 || cookies <= create || seedAt <= cookies {
		t.Fatalf("event order %v, want create, then set-cookies, then set-seed", r.ev.all())
	}
	if r.ev.index("create-without-hold") >= 0 {
		t.Fatal("the box was created before the login-state hold was taken")
	}
	if got := cookieNames(t, r.browser.gotCookies); !slices.Equal(got, []string{"sid"}) {
		t.Fatalf("restored cookies = %v, want [sid]", got)
	}
	if r.browser.gotSeed["https://example.com"]["k"] != "v" {
		t.Fatalf("restored seed = %v, want the saved origin", r.browser.gotSeed)
	}
}

func TestStartWithoutSavedStateStartsClean(t *testing.T) {
	r := newLoginRig(t)
	r.setAssign("ses-1", "fresh")
	st := mustStart(t, r.h.m, "ses-1")
	if st.LoginState != "fresh" {
		t.Fatalf("Status.LoginState = %q, want fresh", st.LoginState)
	}
	if r.ev.index("set-cookies") >= 0 || r.ev.index("set-seed") >= 0 {
		t.Fatalf("a name with no saved file was restored: %v", r.ev.all())
	}
	if got := r.holder("fresh"); got != "ses-1" {
		t.Fatalf("holder = %q, want ses-1", got)
	}
	if _, found, _ := r.store.Load("fresh"); found {
		t.Fatal("a state file exists before any checkpoint or teardown")
	}
}

func TestStatusesReportLoginStateAndNone(t *testing.T) {
	r := newLoginRig(t)
	r.setAssign("ses-a", "work")
	mustStart(t, r.h.m, "ses-a")
	mustStart(t, r.h.m, "ses-b")
	got := map[string]string{}
	for _, st := range r.h.m.Statuses() {
		got[st.SessionID] = st.LoginState
	}
	if got["ses-a"] != "work" || got["ses-b"] != "" {
		t.Fatalf("Statuses login states = %v, want ses-a=work and ses-b empty", got)
	}
}

func TestStartInUseCreatesNoBox(t *testing.T) {
	r := newLoginRig(t)
	r.setAssign("ses-a", "work")
	r.setAssign("ses-b", "work")
	mustStart(t, r.h.m, "ses-a")

	_, err := r.h.m.Start(context.Background(), "ses-b", StartOptions{})
	inUse, ok := err.(ErrLoginStateInUse)
	if !ok {
		t.Fatalf("Start while in use = %v (%T), want ErrLoginStateInUse returned as-is", err, err)
	}
	if inUse.SessionID != "ses-a" || inUse.Name != "work" {
		t.Fatalf("ErrLoginStateInUse = %+v, want the holder ses-a", inUse)
	}
	if n := r.h.api.createCount(); n != 1 {
		t.Fatalf("creates = %d, want 1: the refused start must not create a box", n)
	}
	if got := len(r.h.m.Statuses()); got != 1 {
		t.Fatalf("running boxes = %d, want only ses-a's", got)
	}
}

func TestStartFailureReleasesHold(t *testing.T) {
	r := newLoginRig(t)
	if err := r.store.Save(testLoginState("work")); err != nil {
		t.Fatalf("Save: %v", err)
	}
	r.setAssign("ses-a", "work")
	r.setAssign("ses-b", "work")
	r.browser.setCookiesErr = errors.New("browser refused the cookies")

	_, err := r.h.m.Start(context.Background(), "ses-a", StartOptions{})
	if err == nil || !strings.Contains(err.Error(), "restore cookies") {
		t.Fatalf("Start with a failing restore = %v, want a restore error", err)
	}
	if got := r.holder("work"); got != "" {
		t.Fatalf("holder after a failed start = %q, want none", got)
	}
	if n := r.h.api.createCount(); n != 1 {
		t.Fatalf("creates = %d, want 1", n)
	}
	if got := len(r.h.api.deleted()); got != 1 {
		t.Fatalf("deletes = %d, want the failed box discarded", got)
	}
	if r.ev.count("close-browser") != 1 {
		t.Fatalf("browser closes = %d, want the failed browser closed", r.ev.count("close-browser"))
	}

	r.browser.setCookiesErr = nil
	mustStart(t, r.h.m, "ses-b")
	if got := r.holder("work"); got != "ses-b" {
		t.Fatalf("holder after a later start = %q, want ses-b", got)
	}
}

func TestStartFailsBeforeCreateWhenLookupOrLoadFails(t *testing.T) {
	t.Run("lookup", func(t *testing.T) {
		r := newLoginRig(t)
		r.lookupErr = errors.New("config unreadable")
		if _, err := r.h.m.Start(context.Background(), "ses-1", StartOptions{}); err == nil {
			t.Fatal("Start with a failing login-state lookup succeeded")
		}
		if n := r.h.api.createCount(); n != 0 {
			t.Fatalf("creates = %d, want 0", n)
		}
	})
	t.Run("corrupt file", func(t *testing.T) {
		r := newLoginRig(t)
		if err := os.MkdirAll(r.store.dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(r.store.statePath("work"), []byte("{not json"), 0o600); err != nil {
			t.Fatal(err)
		}
		r.setAssign("ses-1", "work")
		if _, err := r.h.m.Start(context.Background(), "ses-1", StartOptions{}); err == nil {
			t.Fatal("Start with a corrupt login state succeeded")
		}
		if n := r.h.api.createCount(); n != 0 {
			t.Fatalf("creates = %d, want 0: a corrupt state must stop the start", n)
		}
		if got := r.holder("work"); got != "" {
			t.Fatalf("holder = %q after the failed start, want none", got)
		}
	})
}

func TestCheckpointDebounceCoalescesBrowserCalls(t *testing.T) {
	r := newLoginRig(t)
	r.setAssign("ses-1", "work")
	mustStart(t, r.h.m, "ses-1")

	for range 5 {
		if _, err := r.h.m.Browser("ses-1"); err != nil {
			t.Fatalf("Browser: %v", err)
		}
	}
	timers := r.timers.all()
	if len(timers) != 5 {
		t.Fatalf("armed timers = %d, want one per browser call", len(timers))
	}
	for _, tm := range timers {
		if tm.d != 10*time.Second {
			t.Fatalf("checkpoint delay = %s, want 10s", tm.d)
		}
	}
	for _, tm := range timers[:4] {
		tm.fire()
	}
	if n := r.ev.count("read-cookies"); n != 0 {
		t.Fatalf("superseded timers saved %d time(s), want none", n)
	}
	timers[4].fire()
	if n := r.ev.count("read-cookies"); n != 1 {
		t.Fatalf("checkpoints = %d, want exactly one for five browser calls", n)
	}
	if st := r.savedState("work"); st.SavedAt.IsZero() {
		t.Fatal("the checkpoint saved a state without a SavedAt")
	}

	// A session with no login state arms nothing.
	mustStart(t, r.h.m, "ses-plain")
	if _, err := r.h.m.Browser("ses-plain"); err != nil {
		t.Fatalf("Browser: %v", err)
	}
	if got := len(r.timers.all()); got != 5 {
		t.Fatalf("a session with no login state armed a checkpoint (%d timers)", got)
	}
}

func TestCheckpointAfterTeardownOrRestartIsNoOp(t *testing.T) {
	r := newLoginRig(t)
	r.setAssign("ses-1", "work")
	mustStart(t, r.h.m, "ses-1")
	if _, err := r.h.m.Browser("ses-1"); err != nil {
		t.Fatalf("Browser: %v", err)
	}
	old := r.timers.all()[0]
	if err := r.h.m.Terminate(context.Background(), "ses-1"); err != nil {
		t.Fatalf("Terminate: %v", err)
	}
	before := r.ev.count("read-cookies")
	old.fireLate()
	if n := r.ev.count("read-cookies"); n != before {
		t.Fatalf("a checkpoint that fired after the teardown read the box (%d -> %d)", before, n)
	}

	// A new box for the same session must not be read by the old token either.
	mustStart(t, r.h.m, "ses-1")
	old.fireLate()
	if n := r.ev.count("read-cookies"); n != before {
		t.Fatalf("a stale checkpoint read the new box (%d -> %d)", before, n)
	}
}

// TestTeardownSavesThenDeletesWithoutCheckpointAfterDeleteStarts: a checkpoint
// that fires while the teardown is held inside the box delete must not read the
// box. The final save comes first, and nothing reads the box once the delete starts.
func TestTeardownSavesThenDeletesWithoutCheckpointAfterDeleteStarts(t *testing.T) {
	r := newLoginRig(t)
	r.setAssign("ses-1", "work")
	r.deleteEntered = make(chan struct{}, 4)
	r.deleteGate = make(chan struct{})
	mustStart(t, r.h.m, "ses-1")
	if _, err := r.h.m.Browser("ses-1"); err != nil {
		t.Fatalf("Browser: %v", err)
	}
	pending := r.timers.all()[0]

	termErr := make(chan error, 1)
	go func() { termErr <- r.h.m.Terminate(context.Background(), "ses-1") }()
	select {
	case <-r.deleteEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("the teardown never reached the box delete")
	}

	// The teardown is inside the delete. The checkpoint fires now and must wait.
	cpDone := make(chan struct{})
	go func() {
		defer close(cpDone)
		pending.fireLate()
	}()
	close(r.deleteGate)
	if err := <-termErr; err != nil {
		t.Fatalf("Terminate: %v", err)
	}
	<-cpDone

	deleteAt := r.ev.index("delete-start")
	if deleteAt < 0 {
		t.Fatalf("no delete recorded: %v", r.ev.all())
	}
	for i, e := range r.ev.all() {
		if i > deleteAt && (strings.HasPrefix(e, "read-") || e == "set-cookies" || e == "set-seed") {
			t.Fatalf("the box was read after its delete started: %v", r.ev.all())
		}
	}
	final := r.ev.index("read-cookies")
	if final < 0 || final > r.ev.index("close-browser") || r.ev.index("close-browser") > deleteAt {
		t.Fatalf("event order %v, want the final save, then close, then delete", r.ev.all())
	}
	if got := r.holder("work"); got != "" {
		t.Fatalf("holder after teardown = %q, want released", got)
	}
	r.savedState("work")
}

func TestCookieReadFailureKeepsPreviousFile(t *testing.T) {
	r := newLoginRig(t)
	if err := r.store.Save(testLoginState("work")); err != nil {
		t.Fatalf("Save: %v", err)
	}
	before := r.savedState("work")
	r.setAssign("ses-1", "work")
	mustStart(t, r.h.m, "ses-1")
	r.browser.cookieErr = errors.New("cookie store unavailable")

	if err := r.h.m.Terminate(context.Background(), "ses-1"); err != nil {
		t.Fatalf("Terminate with a failed cookie read = %v, want the teardown to finish", err)
	}
	after := r.savedState("work")
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("a failed cookie read changed the saved state:\nbefore %+v\nafter  %+v", before, after)
	}
	if got := len(r.h.api.deleted()); got != 1 {
		t.Fatalf("deletes = %d, want the box still deleted", got)
	}
}

func TestSnapshotMergeReplacesVisitedOriginsAndKeepsOthers(t *testing.T) {
	r := newLoginRig(t)
	if err := r.store.Save(LoginState{
		Name:    "work",
		Cookies: []json.RawMessage{testCookie("old", "1")},
		LocalStorage: map[string]map[string]string{
			"https://a.test": {"stale": "gone", "k": "old"},
			"https://b.test": {"keep": "yes"},
			"https://d.test": {"d": "1"},
		},
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	r.setAssign("ses-1", "work")
	mustStart(t, r.h.m, "ses-1")

	// The snapshot reports a.test (replaces its stored items) and c.test (new). It
	// omits b.test and d.test, as it would for tabs that are closed or whose read
	// failed. The error marks the snapshot as partial, and it is still used.
	r.browser.cookies = []json.RawMessage{testCookie("new", "2")}
	r.browser.snap = map[string]map[string]string{
		"https://a.test": {"k": "new"},
		"https://c.test": {"x": "2"},
	}
	r.browser.snapErr = errors.Join(errors.New("tab one failed"), errors.New("tab two failed"))

	if err := r.h.m.Terminate(context.Background(), "ses-1"); err != nil {
		t.Fatalf("Terminate: %v", err)
	}
	st := r.savedState("work")
	want := map[string]map[string]string{
		"https://a.test": {"k": "new"},
		"https://b.test": {"keep": "yes"},
		"https://c.test": {"x": "2"},
		"https://d.test": {"d": "1"},
	}
	if !reflect.DeepEqual(st.LocalStorage, want) {
		t.Fatalf("merged local storage = %v, want %v", st.LocalStorage, want)
	}
	if got := cookieNames(t, st.Cookies); !slices.Equal(got, []string{"new"}) {
		t.Fatalf("cookies = %v, want the read replacing the old set wholesale", got)
	}
}

// TestHolderReleasedOnEveryTeardownPath: each way a box is torn down frees the
// login-state name, so another session can take it.
func TestHolderReleasedOnEveryTeardownPath(t *testing.T) {
	cases := map[string]func(t *testing.T, r *loginRig){
		"terminate": func(t *testing.T, r *loginRig) {
			if err := r.h.m.Terminate(context.Background(), "ses-a"); err != nil {
				t.Fatalf("Terminate: %v", err)
			}
		},
		"idle": func(t *testing.T, r *loginRig) {
			r.h.clock.Advance(11 * time.Minute)
			r.h.m.Tick(context.Background())
		},
		"hard cap": func(t *testing.T, r *loginRig) {
			r.h.clock.Advance(time.Hour + time.Second)
			r.h.m.Tick(context.Background())
		},
		"shutdown": func(t *testing.T, r *loginRig) {
			r.h.m.Shutdown(context.Background())
		},
	}
	for name, tear := range cases {
		t.Run(name, func(t *testing.T) {
			r := newLoginRig(t)
			r.setAssign("ses-a", "work")
			r.setAssign("ses-b", "work")
			mustStart(t, r.h.m, "ses-a")
			tear(t, r)
			if got := r.holder("work"); got != "" {
				t.Fatalf("holder after %s = %q, want released", name, got)
			}
			if name == "shutdown" {
				return
			}
			mustStart(t, r.h.m, "ses-b")
			if got := r.holder("work"); got != "ses-b" {
				t.Fatalf("after %s, holder = %q, want ses-b", name, got)
			}
		})
	}
}

// TestReplaceAfterTTLSavesThenReleasesThenHoldsAgain: a start that replaces an
// expired box saves the old box first, and the replacement holds the name again.
func TestReplaceAfterTTLSavesThenReleasesThenHoldsAgain(t *testing.T) {
	r := newLoginRig(t)
	r.setAssign("ses-a", "work")
	first := mustStart(t, r.h.m, "ses-a")
	r.h.clock.Advance(time.Hour + time.Second)

	second := mustStart(t, r.h.m, "ses-a")
	if second.BoxID == first.BoxID || second.Reused {
		t.Fatalf("replacement = %+v, want a new box", second)
	}
	if second.LoginState != "work" {
		t.Fatalf("replacement LoginState = %q, want work", second.LoginState)
	}
	if got := r.holder("work"); got != "ses-a" {
		t.Fatalf("holder after replacement = %q, want ses-a", got)
	}
	if !slices.Contains(r.h.api.deleted(), first.BoxID) {
		t.Fatalf("the expired box %s was not deleted", first.BoxID)
	}
	if r.ev.index("read-cookies") < 0 || r.ev.index("read-cookies") > r.ev.index("delete-start") {
		t.Fatalf("event order %v, want the old box saved before its delete", r.ev.all())
	}
}

func TestRekeyKeepsLoginHold(t *testing.T) {
	r := newLoginRig(t)
	r.setAssign("ses-a", "work")
	r.setAssign("ses-b", "work")
	mustStart(t, r.h.m, "ses-a")

	if err := r.h.m.Rekey("ses-a", "ses-a2"); err != nil {
		t.Fatalf("Rekey: %v", err)
	}
	if got := r.holder("work"); got != "ses-a2" {
		t.Fatalf("holder after Rekey = %q, want ses-a2", got)
	}
	_, err := r.h.m.Start(context.Background(), "ses-b", StartOptions{})
	if inUse, ok := err.(ErrLoginStateInUse); !ok || inUse.SessionID != "ses-a2" {
		t.Fatalf("Start by a third session = %v, want ErrLoginStateInUse naming ses-a2", err)
	}

	// The moved box still checkpoints and tears down under the new id.
	if _, err := r.h.m.Browser("ses-a2"); err != nil {
		t.Fatalf("Browser after Rekey: %v", err)
	}
	if err := r.h.m.Terminate(context.Background(), "ses-a2"); err != nil {
		t.Fatalf("Terminate after Rekey: %v", err)
	}
	if got := r.holder("work"); got != "" {
		t.Fatalf("holder after the new session's teardown = %q, want released", got)
	}
}

// TestRekeyHoldMoveFailureUndoesJournal: when the login-state hold cannot follow
// the box, the rekey fails whole, and the journal keeps the old session id.
func TestRekeyHoldMoveFailureUndoesJournal(t *testing.T) {
	r := newLoginRig(t)
	r.setAssign("ses-a", "work")
	mustStart(t, r.h.m, "ses-a")

	// Another holder appears in the holder file, so the move is refused.
	writeHolderForTest(t, r.store.dir, "work", loginHolder{SessionID: "someone-else", PID: os.Getpid(), AcquiredAt: time.Now()})
	if err := r.h.m.Rekey("ses-a", "ses-a2"); err == nil {
		t.Fatal("Rekey succeeded although the login-state hold could not move")
	}
	entries, err := r.h.journal.List()
	if err != nil || len(entries) != 1 || entries[0].SessionID != "ses-a" {
		t.Fatalf("journal after the failed rekey = %+v (err %v), want the old session id", entries, err)
	}
	if _, err := r.h.m.Browser("ses-a"); err != nil {
		t.Fatalf("the box is no longer reachable under its old session: %v", err)
	}
}

// TestStartReportsRestoreNotes: a restore the browser partly refused says so in the
// box's status, in words, and the status of a reused box carries the same notes.
func TestStartReportsRestoreNotes(t *testing.T) {
	r := newLoginRig(t)
	if err := r.store.Save(testLoginState("work")); err != nil {
		t.Fatalf("Save: %v", err)
	}
	r.browser.setCookiesRejected = 3
	r.browser.seedSkipped = []string{"https://big.test"}
	r.setAssign("ses-1", "work")
	st := mustStart(t, r.h.m, "ses-1")
	want := []string{
		"3 cookies were rejected by the browser",
		"site storage for https://big.test was too large and was not restored",
	}
	if !slices.Equal(st.LoginStateNotes, want) {
		t.Fatalf("Start LoginStateNotes = %q, want %q", st.LoginStateNotes, want)
	}
	for _, s := range r.h.m.Statuses() {
		if s.SessionID == "ses-1" && !slices.Equal(s.LoginStateNotes, want) {
			t.Fatalf("Statuses LoginStateNotes = %q, want %q", s.LoginStateNotes, want)
		}
	}
}

// TestRestoreNotesWording pins the sentence for each count.
func TestRestoreNotesWording(t *testing.T) {
	cases := []struct {
		rejected int
		skipped  []string
		want     []string
	}{
		{0, nil, nil},
		{1, nil, []string{"1 cookie was rejected by the browser"}},
		{2, []string{"https://x.test"}, []string{
			"2 cookies were rejected by the browser",
			"site storage for https://x.test was too large and was not restored",
		}},
	}
	for _, c := range cases {
		if got := restoreNotes(c.rejected, c.skipped); !slices.Equal(got, c.want) {
			t.Fatalf("restoreNotes(%d, %v) = %q, want %q", c.rejected, c.skipped, got, c.want)
		}
	}
}

// TestContextRestoreLoadsLatestSavedFile: a replacement browser is filled from the
// newest saved file, not the state the box started with.
func TestContextRestoreLoadsLatestSavedFile(t *testing.T) {
	r := newLoginRig(t)
	if err := r.store.Save(testLoginState("work")); err != nil {
		t.Fatalf("Save: %v", err)
	}
	r.setAssign("ses-1", "work")
	mustStart(t, r.h.m, "ses-1")
	// A checkpoint after the start wrote a newer cookie.
	if err := r.store.Save(LoginState{Name: "work", Cookies: []json.RawMessage{testCookie("newer", "2")}}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	r.h.m.mu.Lock()
	hold := r.h.m.sessions["ses-1"].box.hold
	r.h.m.mu.Unlock()

	cookies, _, err := r.h.m.contextRestoreFor(hold)(context.Background())
	if err != nil {
		t.Fatalf("context restore: %v", err)
	}
	if got := cookieNames(t, cookies); !slices.Equal(got, []string{"newer"}) {
		t.Fatalf("context restore cookies = %v, want the newer saved file", got)
	}
}

// TestContextRestoreForBoxWithoutLoginStateIsEmpty: a clean box's replacement
// browser is cleared, not refused.
func TestContextRestoreForBoxWithoutLoginStateIsEmpty(t *testing.T) {
	r := newLoginRig(t)
	cookies, seed, err := r.h.m.contextRestoreFor(nil)(context.Background())
	if err != nil {
		t.Fatalf("context restore of a clean box = %v, want an empty restore", err)
	}
	if cookies == nil || len(cookies) != 0 || seed == nil || len(seed) != 0 {
		t.Fatalf("context restore of a clean box = %v, %v; want empty non-nil values", cookies, seed)
	}
}

// TestCheckpointMaxWaitRunsOneWhileCallsKeepComing: calls every 5 s never let the
// 10 s debounce fire, so once 60 s have gone without a save, the next call's
// checkpoint runs at once.
func TestCheckpointMaxWaitRunsOneWhileCallsKeepComing(t *testing.T) {
	r := newLoginRig(t)
	r.setAssign("ses-1", "work")
	mustStart(t, r.h.m, "ses-1")

	var last *fakeTimer
	for i := 1; i <= 12; i++ {
		r.h.clock.Advance(5 * time.Second)
		if _, err := r.h.m.Browser("ses-1"); err != nil {
			t.Fatalf("Browser: %v", err)
		}
		timers := r.timers.all()
		last = timers[len(timers)-1]
		want := loginCheckpointDelay
		if i == 12 {
			want = 0
		}
		if last.d != want {
			t.Fatalf("call %d (t=%ds): checkpoint delay %s, want %s", i, i*5, last.d, want)
		}
	}
	last.fire()
	if n := r.ev.count("read-cookies"); n != 1 {
		t.Fatalf("checkpoints after 60s of calls = %d, want one", n)
	}
	// The save restarts the max-wait clock: the next call is debounced again.
	r.h.clock.Advance(5 * time.Second)
	if _, err := r.h.m.Browser("ses-1"); err != nil {
		t.Fatalf("Browser: %v", err)
	}
	timers := r.timers.all()
	if d := timers[len(timers)-1].d; d != loginCheckpointDelay {
		t.Fatalf("call after a save: delay %s, want the debounce %s", d, loginCheckpointDelay)
	}
}

// TestPreExpiryCheckpointSavesOnceWithinNinetySeconds: a box with a login state
// saves once when it comes within 90 s of its expiry, and not before.
func TestPreExpiryCheckpointSavesOnceWithinNinetySeconds(t *testing.T) {
	r := newLoginRig(t)
	r.setAssign("ses-1", "work")
	st := mustStart(t, r.h.m, "ses-1")
	keepAlive := func() {
		t.Helper()
		if _, err := r.h.m.Browser("ses-1"); err != nil {
			t.Fatalf("Browser: %v", err)
		}
	}

	r.h.clock.Advance(st.ExpiresAt.Sub(r.h.clock.Now()) - 120*time.Second)
	keepAlive()
	r.h.m.Tick(context.Background())
	if n := r.ev.count("read-cookies"); n != 0 {
		t.Fatalf("a save 120s before expiry = %d, want none", n)
	}

	r.h.clock.Advance(31 * time.Second) // 89 s before expiry
	keepAlive()
	r.h.m.Tick(context.Background())
	if n := r.ev.count("read-cookies"); n != 1 {
		t.Fatalf("saves within 90s of expiry = %d, want one", n)
	}
	r.h.clock.Advance(10 * time.Second)
	keepAlive()
	r.h.m.Tick(context.Background())
	if n := r.ev.count("read-cookies"); n != 1 {
		t.Fatalf("a second Tick saved again (%d saves), want one per box", n)
	}
	if got := len(r.h.m.Statuses()); got != 1 {
		t.Fatalf("the box was torn down by the pre-expiry save (statuses %d)", got)
	}
}

// TestPreExpiryCheckpointSkipsBoxWithoutLoginState: a clean box is not saved.
func TestPreExpiryCheckpointSkipsBoxWithoutLoginState(t *testing.T) {
	r := newLoginRig(t)
	st := mustStart(t, r.h.m, "ses-1")
	r.h.clock.Advance(st.ExpiresAt.Sub(r.h.clock.Now()) - 60*time.Second)
	if _, err := r.h.m.Browser("ses-1"); err != nil {
		t.Fatalf("Browser: %v", err)
	}
	r.h.m.Tick(context.Background())
	if n := r.ev.count("read-cookies") + r.ev.count("read-storage"); n != 0 {
		t.Fatalf("a box with no login state was read %d time(s) near expiry", n)
	}
}

// TestCheckpointArmedBeforeRekeyFollowsTheBox: a checkpoint armed under the old
// session id still saves the box after Rekey moved it.
func TestCheckpointArmedBeforeRekeyFollowsTheBox(t *testing.T) {
	r := newLoginRig(t)
	r.setAssign("ses-a", "work")
	mustStart(t, r.h.m, "ses-a")
	if _, err := r.h.m.Browser("ses-a"); err != nil {
		t.Fatalf("Browser: %v", err)
	}
	pending := r.timers.all()[0]
	if err := r.h.m.Rekey("ses-a", "ses-a2"); err != nil {
		t.Fatalf("Rekey: %v", err)
	}
	pending.fire()
	if n := r.ev.count("read-cookies"); n != 1 {
		t.Fatalf("a checkpoint armed before Rekey saved %d time(s), want 1", n)
	}
	r.savedState("work")
}

// TestStartPanicInCreateReleasesLoginHold: a panic inside create must not leave the
// login state held by a box that was never published.
func TestStartPanicInCreateReleasesLoginHold(t *testing.T) {
	r := newLoginRig(t)
	r.setAssign("ses-a", "work")
	r.setAssign("ses-b", "work")
	r.onCreate = func() { panic("create panicked") }
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("Start did not propagate the panic")
			}
		}()
		_, _ = r.h.m.Start(context.Background(), "ses-a", StartOptions{})
	}()
	if got := r.holder("work"); got != "" {
		t.Fatalf("holder after a panicked start = %q, want released", got)
	}
	r.onCreate = nil
	mustStart(t, r.h.m, "ses-b")
	if got := r.holder("work"); got != "ses-b" {
		t.Fatalf("holder after a later start = %q, want ses-b", got)
	}
}

// TestSnapshotEmptyOriginIsRemovedFromFile: an origin the snapshot reports with no
// items was cleared in the browser, so it leaves the file. An origin the snapshot
// does not report keeps its stored items.
func TestSnapshotEmptyOriginIsRemovedFromFile(t *testing.T) {
	r := newLoginRig(t)
	if err := r.store.Save(LoginState{
		Name:    "work",
		Cookies: []json.RawMessage{testCookie("sid", "1")},
		LocalStorage: map[string]map[string]string{
			"https://gone.test": {"k": "v"},
			"https://big.test":  {"x": "1"},
		},
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	r.setAssign("ses-1", "work")
	mustStart(t, r.h.m, "ses-1")
	r.browser.snap = map[string]map[string]string{"https://gone.test": {}}

	if err := r.h.m.Terminate(context.Background(), "ses-1"); err != nil {
		t.Fatalf("Terminate: %v", err)
	}
	want := map[string]map[string]string{"https://big.test": {"x": "1"}}
	if got := r.savedState("work").LocalStorage; !reflect.DeepEqual(got, want) {
		t.Fatalf("local storage after a cleared origin = %v, want %v", got, want)
	}
}

// TestAbandonedCaptureNeverWrites: once a shutdown abandons a capture, its late
// write is refused and the file keeps its previous state. A capture that was not
// abandoned writes.
func TestAbandonedCaptureNeverWrites(t *testing.T) {
	r := newLoginRig(t)
	if err := r.store.Save(testLoginState("work")); err != nil {
		t.Fatalf("Save: %v", err)
	}
	before := r.savedState("work")

	abandoned := &saveTicket{}
	if finished := abandoned.abandon(); finished {
		t.Fatal("abandon of an unfinished capture reported it finished")
	}
	err := r.h.m.commitCapture("work", []json.RawMessage{testCookie("late", "9")}, map[string]map[string]string{"https://late.test": {"k": "v"}}, abandoned)
	if !errors.Is(err, errSaveAbandoned) {
		t.Fatalf("commit of an abandoned capture = %v, want errSaveAbandoned", err)
	}
	if after := r.savedState("work"); !reflect.DeepEqual(after, before) {
		t.Fatalf("an abandoned capture changed the file:\nbefore %+v\nafter  %+v", before, after)
	}

	live := &saveTicket{}
	if err := r.h.m.commitCapture("work", []json.RawMessage{testCookie("fresh", "3")}, nil, live); err != nil {
		t.Fatalf("commit of a live capture: %v", err)
	}
	if got := cookieNames(t, r.savedState("work").Cookies); !slices.Equal(got, []string{"fresh"}) {
		t.Fatalf("live capture cookies = %v, want [fresh]", got)
	}
	if !live.abandon() {
		t.Fatal("a written capture was reported unfinished")
	}
}

// TestShutdownSaveBudgetLeavesDeleteReserve: the final saves get the shorter of
// their own cap and the caller's deadline less the delete reserve.
func TestShutdownSaveBudgetLeavesDeleteReserve(t *testing.T) {
	r := newLoginRig(t)
	if got := r.h.m.shutdownSaveBudget(context.Background()); got != r.h.m.shutdownSaveCap {
		t.Fatalf("budget with no deadline = %s, want the cap %s", got, r.h.m.shutdownSaveCap)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	got := r.h.m.shutdownSaveBudget(ctx)
	if got > 8*time.Second-shutdownDeleteReserve || got < 8*time.Second-shutdownDeleteReserve-time.Second {
		t.Fatalf("budget under an 8s deadline = %s, want about %s", got, 8*time.Second-shutdownDeleteReserve)
	}
}

// TestShutdownSavesRunConcurrentlyBeforeAnyDelete: the final saves of several boxes
// run at once, and every save is read before the first delete starts. Sequential
// saves would stall the first read, so the barrier would time out and fail.
func TestShutdownSavesRunConcurrentlyBeforeAnyDelete(t *testing.T) {
	r := newLoginRig(t)
	const n = 3
	var arrived sync.WaitGroup
	arrived.Add(n)
	allIn := make(chan struct{})
	go func() {
		arrived.Wait()
		close(allIn)
	}()
	var queue []*fakeBrowser
	for range n {
		queue = append(queue, &fakeBrowser{ev: r.ev, onRead: func(ctx context.Context) error {
			arrived.Done()
			select {
			case <-allIn:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(5 * time.Second):
				return errors.New("the saves ran one at a time")
			}
		}})
	}
	r.h.m.browserOf = func(*cdp.Manager) boxBrowser {
		b := queue[0]
		queue = queue[1:]
		return b
	}
	for i := 1; i <= n; i++ {
		sid := fmt.Sprintf("ses-%d", i)
		r.setAssign(sid, fmt.Sprintf("work%d", i))
		mustStart(t, r.h.m, sid)
	}

	done := make(chan struct{})
	go func() {
		r.h.m.Shutdown(context.Background())
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("Shutdown did not finish")
	}

	if got := len(r.h.api.deleted()); got != n {
		t.Fatalf("deletes = %d, want %d", got, n)
	}
	lastRead, firstDelete := -1, -1
	for i, e := range r.ev.all() {
		if e == "read-cookies" {
			lastRead = i
		}
		if e == "delete-start" && firstDelete < 0 {
			firstDelete = i
		}
	}
	if r.ev.count("read-cookies") != n || lastRead > firstDelete {
		t.Fatalf("event order %v, want all %d saves before the first delete", r.ev.all(), n)
	}
	for i := 1; i <= n; i++ {
		r.savedState(fmt.Sprintf("work%d", i))
	}
}

// TestShutdownBudgetKeepsPreviousFileOfUnfinishedSave: a box whose save is still
// running when the shutdown deadline passes keeps its previous file, and the other
// boxes' saves and deletes still complete.
func TestShutdownBudgetKeepsPreviousFileOfUnfinishedSave(t *testing.T) {
	r := newLoginRig(t)
	prior := LoginState{
		Name:         "work1",
		Cookies:      []json.RawMessage{testCookie("old", "1")},
		LocalStorage: map[string]map[string]string{"https://a.test": {"k": "old"}},
	}
	if err := r.store.Save(prior); err != nil {
		t.Fatalf("Save: %v", err)
	}
	// Compare against what the file holds, not the struct: the file is re-indented.
	prior = r.savedState("work1")
	// The three boxes are created in this order, so the browsers pair with sessions 1..3.
	stuck := &fakeBrowser{ev: r.ev, onRead: func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}}
	committed := make(chan string, 2)
	others := []*fakeBrowser{
		{ev: r.ev, snap: map[string]map[string]string{"https://b.test": {"x": "1"}}},
		{ev: r.ev, snap: map[string]map[string]string{"https://c.test": {"y": "2"}}},
	}
	queue := []*fakeBrowser{stuck, others[0], others[1]}
	r.h.m.browserOf = func(*cdp.Manager) boxBrowser {
		b := queue[0]
		queue = queue[1:]
		return b
	}
	r.h.m.onCommit = func(name string) { committed <- name }
	for i := 1; i <= 3; i++ {
		sid := fmt.Sprintf("ses-%d", i)
		r.setAssign(sid, fmt.Sprintf("work%d", i))
		mustStart(t, r.h.m, sid)
	}

	// The shutdown's context is cancelled once the two other saves have committed.
	// The stuck save is still reading then, and the deadline ends it.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		r.h.m.Shutdown(ctx)
		close(done)
	}()
	for range 2 {
		select {
		case <-committed:
		case <-time.After(5 * time.Second):
			t.Fatal("the two other saves did not commit")
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("Shutdown did not finish after its deadline")
	}

	after := r.savedState("work1")
	if !reflect.DeepEqual(after, prior) {
		t.Fatalf("the unfinished save changed its file:\nbefore %+v\nafter  %+v", prior, after)
	}
	if _, ok := r.savedState("work2").LocalStorage["https://b.test"]; !ok {
		t.Fatal("a save that finished in time was not written")
	}
	if _, ok := r.savedState("work3").LocalStorage["https://c.test"]; !ok {
		t.Fatal("a save that finished in time was not written")
	}
	if got := len(r.h.api.deleted()); got != 3 {
		t.Fatalf("deletes = %d, want all three boxes deleted", got)
	}
}
