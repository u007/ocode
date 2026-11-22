package boxsession

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/u007/ocode/internal/browse/cdp"
	"github.com/u007/ocode/internal/upstashbox"
)

var mt0 = time.Date(2026, 11, 21, 10, 0, 0, 0, time.UTC)

// fakeClock is a settable clock.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// fakeAPI is a BoxAPI that records calls. Its CDPURL comes from a real
// upstashbox.Client aimed at an httptest server, so the URLs it returns are
// genuine CDPURL values with a token that changes on every call.
type fakeAPI struct {
	mu        sync.Mutex
	clock     *fakeClock
	cdpClient *upstashbox.Client
	seq       int
	creates   []upstashbox.CreateOptions
	// createErr is returned by Create. With createWithBox the box is returned alongside it.
	createErr     error
	createWithBox bool
	// createGate, when set, makes Create block until it is closed.
	createGate chan struct{}
	// createEntered, when set, receives a value as each Create starts.
	createEntered chan struct{}
	// afterCreated runs right after OnCreated, inside the create-and-wait window.
	afterCreated func(boxID string)
	// waitForCancel makes Create, after OnCreated, wait for ctx and then return
	// the box with ctx's error, as the real poll does when cancelled.
	waitForCancel bool
	// noExpiry makes the returned box carry no ExpiresAt.
	noExpiry  bool
	deletes   []string
	deleteErr map[string]error
	cdpErr    error
	cdpCalls  int
	// onCDP runs inside CDPURL, before it answers. The ordering tests use it.
	onCDP func(boxID string)
	// boxes is the server-side state the fake keeps per box id, for Get, Pause and
	// the resume that CDPURL performs on a paused box.
	boxes map[string]*fakeBox
	// pauseErr is returned by Pause, after the call is recorded. Tests set and clear it.
	pauseErr error
	pauses   []string
	resumes  int
}

// fakeBox is one box as the fake API stores it.
type fakeBox struct {
	status    string
	ephemeral bool
	createdAt time.Time
	expiresAt time.Time
	labels    []string
}

func newFakeAPI(t *testing.T, clock *fakeClock) *fakeAPI {
	t.Helper()
	var mu sync.Mutex
	tokens := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		tokens++
		n := tokens
		mu.Unlock()
		fmt.Fprintf(w, `{"cdp_url":"wss://box.example.test/devtools?token=tok-%d"}`, n)
	}))
	t.Cleanup(srv.Close)
	client, err := upstashbox.New("test-key", upstashbox.WithBaseURL(srv.URL))
	if err != nil {
		t.Fatalf("upstashbox.New: %v", err)
	}
	return &fakeAPI{clock: clock, cdpClient: client, deleteErr: map[string]error{}, boxes: map[string]*fakeBox{}}
}

// notFound is the API's answer for an id it does not know.
func notFound(method, path string) error {
	return &upstashbox.APIError{StatusCode: http.StatusNotFound, Method: method, Path: path, Message: "not found"}
}

// Create mirrors the real one's order: the POST assigns the id and OnCreated
// runs, then the wait (createGate, afterCreated, waitForCancel) and the result.
// Like the API, it returns an expiry only for an ephemeral box.
func (f *fakeAPI) Create(ctx context.Context, opts upstashbox.CreateOptions) (*upstashbox.Box, error) {
	f.mu.Lock()
	gate, entered := f.createGate, f.createEntered
	f.mu.Unlock()
	if entered != nil {
		entered <- struct{}{}
	}
	if gate != nil {
		<-gate
	}
	f.mu.Lock()
	f.creates = append(f.creates, opts)
	f.seq++
	now := f.clock.Now()
	box := &upstashbox.Box{
		ID:        fmt.Sprintf("box-%d", f.seq),
		Status:    "idle",
		Ephemeral: opts.Ephemeral,
		Browser:   true,
		Labels:    opts.Labels,
		Size:      opts.Size,
		CreatedAt: now,
	}
	if opts.Ephemeral && !f.noExpiry {
		box.ExpiresAt = now.Add(time.Duration(opts.TTLSeconds) * time.Second)
	}
	f.boxes[box.ID] = &fakeBox{status: "idle", ephemeral: opts.Ephemeral, createdAt: now, expiresAt: box.ExpiresAt, labels: opts.Labels}
	createErr, withBox := f.createErr, f.createWithBox
	afterCreated, waitForCancel := f.afterCreated, f.waitForCancel
	f.mu.Unlock()

	if createErr != nil && !withBox {
		return nil, createErr
	}
	if opts.OnCreated != nil {
		if err := opts.OnCreated(box.ID); err != nil {
			return box, err
		}
	}
	if afterCreated != nil {
		afterCreated(box.ID)
	}
	if waitForCancel {
		<-ctx.Done()
		return box, ctx.Err()
	}
	if createErr != nil {
		return box, createErr
	}
	return box, nil
}

// Get answers like the API: a deleted box is still readable, with status deleted,
// and an id the fake never created is a 404.
func (f *fakeAPI) Get(ctx context.Context, id string) (*upstashbox.Box, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.boxes[id]
	if !ok {
		return nil, notFound(http.MethodGet, "/v2/box/"+id)
	}
	return &upstashbox.Box{
		ID:        id,
		Status:    b.status,
		Ephemeral: b.ephemeral,
		Browser:   true,
		Labels:    b.labels,
		CreatedAt: b.createdAt,
		ExpiresAt: b.expiresAt,
	}, nil
}

// Pause refuses an ephemeral box with 400, as the API does, and pauses any other.
func (f *fakeAPI) Pause(ctx context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pauses = append(f.pauses, id)
	path := "/v2/box/" + id + "/pause"
	if f.pauseErr != nil {
		return f.pauseErr
	}
	b, ok := f.boxes[id]
	if !ok {
		// A sweep fixture journals a box that exists at the API but was never created
		// through this fake, so the pause adopts it as a running box, as the API would
		// find it. A deleted box is still a 404 below.
		b = &fakeBox{status: "idle", createdAt: f.clock.Now()}
		f.boxes[id] = b
	}
	if b.status == upstashbox.StatusDeleted {
		return notFound(http.MethodPost, path)
	}
	if b.ephemeral {
		return &upstashbox.APIError{StatusCode: http.StatusBadRequest, Method: http.MethodPost, Path: path, Message: "ephemeral boxes cannot be paused"}
	}
	b.status = upstashbox.StatusPaused
	return nil
}

func (f *fakeAPI) Delete(ctx context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deletes = append(f.deletes, id)
	if err := f.deleteErr[id]; err != nil {
		return err
	}
	if b, ok := f.boxes[id]; ok {
		b.status = upstashbox.StatusDeleted
	}
	return nil
}

// boxStatus returns the fake's status of a box, or "" for an unknown id.
func (f *fakeAPI) boxStatus(id string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if b, ok := f.boxes[id]; ok {
		return b.status
	}
	return ""
}

// pauseCalls returns the box ids Pause was asked for, in order.
func (f *fakeAPI) pauseCalls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.pauses...)
}

// resumeCount returns how many CDPURL calls found a paused box and resumed it.
func (f *fakeAPI) resumeCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.resumes
}

func (f *fakeAPI) setPauseErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pauseErr = err
}

// CDPURL answers for a box that is not deleted. On a paused box it resumes the box
// first, as the API does, so the box is idle again afterwards.
func (f *fakeAPI) CDPURL(ctx context.Context, id string) (upstashbox.CDPURL, error) {
	f.mu.Lock()
	f.cdpCalls++
	hook := f.onCDP
	cerr := f.cdpErr
	if b, ok := f.boxes[id]; ok && cerr == nil {
		switch b.status {
		case upstashbox.StatusDeleted:
			cerr = notFound(http.MethodPost, "/v2/box/"+id+"/browser/connect")
		case upstashbox.StatusPaused:
			b.status = "idle"
			f.resumes++
		}
	}
	f.mu.Unlock()
	if hook != nil {
		hook(id)
	}
	if cerr != nil {
		return upstashbox.CDPURL{}, cerr
	}
	return f.cdpClient.CDPURL(ctx, id)
}

func (f *fakeAPI) setDeleteErr(id string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleteErr[id] = err
}

func (f *fakeAPI) setCreateErr(err error, withBox bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.createErr = err
	f.createWithBox = withBox
}

func (f *fakeAPI) createCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.creates)
}

func (f *fakeAPI) deleted() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.deletes...)
}

func (f *fakeAPI) cdpCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.cdpCalls
}

// harness wires a Manager to the fakes.
type harness struct {
	t       *testing.T
	clock   *fakeClock
	api     *fakeAPI
	journal *Journal
	m       *Manager

	mu      sync.Mutex
	busy    map[string]bool
	dialed  []string
	account Account
	keys    map[string]string
	// alive lists the PIDs the fake liveness check reports as running. Any other PID is dead.
	alive map[int]bool
	// created is when each fake PID's process was created. A PID with no entry has an
	// unknown creation time, which the liveness rule reads as alive.
	created map[int]time.Time
}

// setCreated sets when the fake process pid was created. A lease whose heartbeat is
// earlier names a PID that was reused after its owner died.
func (h *harness) setCreated(pid int, at time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.created[pid] = at
}

func (h *harness) setAlive(pid int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.alive[pid] = true
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{
		t:       t,
		clock:   &fakeClock{t: mt0},
		busy:    map[string]bool{},
		alive:   map[int]bool{},
		created: map[int]time.Time{},
		keys:    map[string]string{"acc-1": "key-1", "acc-2": "key-2"},
		account: Account{
			ID:      "acc-1",
			APIKey:  "key-1",
			Size:    upstashbox.SizeSmall,
			Idle:    10 * time.Minute,
			HardCap: time.Hour,
		},
	}
	h.api = newFakeAPI(t, h.clock)
	h.journal = NewJournal(filepath.Join(t.TempDir(), "upstash-boxes.json"))
	h.m = New(Deps{
		Account: func() (Account, error) {
			h.mu.Lock()
			defer h.mu.Unlock()
			return h.account, nil
		},
		APIKeyFor: func(accountID string) (string, error) {
			h.mu.Lock()
			defer h.mu.Unlock()
			k, ok := h.keys[accountID]
			if !ok {
				return "", fmt.Errorf("no key for account %s", accountID)
			}
			return k, nil
		},
		NewAPI: func(apiKey string) (BoxAPI, error) {
			if apiKey == "" {
				return nil, errors.New("empty key")
			}
			return h.api, nil
		},
		Dial: func(ctx context.Context, rawURL string) (*cdp.Conn, error) {
			h.mu.Lock()
			h.dialed = append(h.dialed, rawURL)
			h.mu.Unlock()
			return nil, errors.New("fake dial refused")
		},
		Busy: func(sessionID string) bool {
			h.mu.Lock()
			defer h.mu.Unlock()
			return h.busy[sessionID]
		},
		Journal: h.journal,
		Label:   "ocode-test00aa",
		Now:     h.clock.Now,
		LeaseAlive: func(l Lease) bool {
			h.mu.Lock()
			defer h.mu.Unlock()
			return leaseAliveWith(l, func(pid int) (bool, time.Time, error) {
				return h.alive[pid], h.created[pid], nil
			})
		},
	})
	return h
}

func (h *harness) setBusy(session string, busy bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.busy[session] = busy
}

func (h *harness) setAccount(a Account) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.account = a
}

func (h *harness) dialedURLs() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.dialed...)
}

func (h *harness) journalIDs() []string {
	h.t.Helper()
	entries, err := h.journal.List()
	if err != nil {
		h.t.Fatalf("journal List: %v", err)
	}
	return ids(entries)
}

func mustStart(t *testing.T, m *Manager, session string) Status {
	t.Helper()
	st, err := m.Start(context.Background(), session, StartOptions{})
	if err != nil {
		t.Fatalf("Start(%s): %v", session, err)
	}
	return st
}

func TestStartCreatesOnceAndReuses(t *testing.T) {
	h := newHarness(t)
	first := mustStart(t, h.m, "ses-1")
	if first.Reused {
		t.Fatal("first Start reported Reused")
	}
	if first.Kind != KindEphemeral || first.BoxID == "" || first.AccountID != "acc-1" {
		t.Fatalf("first Start status = %+v", first)
	}
	second := mustStart(t, h.m, "ses-1")
	if !second.Reused || second.BoxID != first.BoxID {
		t.Fatalf("second Start = %+v, want Reused of %s", second, first.BoxID)
	}
	if n := h.api.createCount(); n != 1 {
		t.Fatalf("Create called %d times, want 1", n)
	}
}

func TestBrowserNeverCreates(t *testing.T) {
	h := newHarness(t)
	if _, err := h.m.Browser("ses-1"); !errors.Is(err, ErrNotStarted) {
		t.Fatalf("Browser before Start = %v, want ErrNotStarted", err)
	}
	if n := h.api.createCount(); n != 0 {
		t.Fatalf("Browser created %d boxes", n)
	}
	mustStart(t, h.m, "ses-1")
	cm, err := h.m.Browser("ses-1")
	if err != nil || cm == nil {
		t.Fatalf("Browser after Start = %v, %v", cm, err)
	}
	if _, err := h.m.Browser("ses-other"); !errors.Is(err, ErrNotStarted) {
		t.Fatalf("Browser for another session = %v, want ErrNotStarted", err)
	}
	if n := h.api.createCount(); n != 1 {
		t.Fatalf("Create called %d times, want 1", n)
	}
}

func TestCreateOptions(t *testing.T) {
	h := newHarness(t)
	mustStart(t, h.m, "0123456789abcdefXYZ")
	if len(h.api.creates) != 1 {
		t.Fatalf("creates = %d", len(h.api.creates))
	}
	o := h.api.creates[0]
	switch {
	case !o.Ephemeral:
		t.Error("Ephemeral = false")
	case !o.Browser:
		t.Error("Browser = false")
	case o.TTLSeconds != 3600:
		t.Errorf("TTLSeconds = %d, want 3600 (HardCap)", o.TTLSeconds)
	case o.Size != upstashbox.SizeSmall:
		t.Errorf("Size = %q", o.Size)
	case len(o.Labels) != 1 || o.Labels[0] != "ocode-test00aa":
		t.Errorf("Labels = %v", o.Labels)
	case o.Name != "ocode-0123456789ab":
		t.Errorf("Name = %q, want the first 12 session characters", o.Name)
	case o.NetworkPolicy != nil:
		t.Error("NetworkPolicy set; the denied-range policy is not applied")
	}
}

func TestCreateRejectsBadTiming(t *testing.T) {
	cases := map[string]Account{
		"hard cap over the ttl limit": {ID: "acc-1", APIKey: "key-1", Idle: time.Minute, HardCap: time.Duration(upstashbox.MaxTTLSeconds+1) * time.Second},
		"zero hard cap":               {ID: "acc-1", APIKey: "key-1", Idle: time.Minute, HardCap: 0},
		"idle over hard cap":          {ID: "acc-1", APIKey: "key-1", Idle: 2 * time.Hour, HardCap: time.Hour},
		"zero idle":                   {ID: "acc-1", APIKey: "key-1", Idle: 0, HardCap: time.Hour},
		"no account id":               {APIKey: "key-1", Idle: time.Minute, HardCap: time.Hour},
	}
	for name, acc := range cases {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			h.setAccount(acc)
			if _, err := h.m.Start(context.Background(), "ses-1", StartOptions{}); err == nil {
				t.Fatal("Start succeeded")
			}
			if n := h.api.createCount(); n != 0 {
				t.Fatalf("Create called %d times for invalid timing", n)
			}
		})
	}
}

func TestCreateErrorWithBoxDeletesIt(t *testing.T) {
	h := newHarness(t)
	h.api.setCreateErr(errors.New("create: poll timed out"), true)
	if _, err := h.m.Start(context.Background(), "ses-1", StartOptions{}); err == nil {
		t.Fatal("Start succeeded")
	}
	if got := h.api.deleted(); len(got) != 1 || got[0] != "box-1" {
		t.Fatalf("deleted = %v, want [box-1]", got)
	}
	if ids := h.journalIDs(); len(ids) != 0 {
		t.Fatalf("journal = %v, want empty", ids)
	}
	if _, err := h.m.Browser("ses-1"); !errors.Is(err, ErrNotStarted) {
		t.Fatalf("Browser after failed Start = %v", err)
	}
}

func TestCreateErrorWithoutBoxDeletesNothing(t *testing.T) {
	h := newHarness(t)
	h.api.setCreateErr(errors.New("create: 500"), false)
	if _, err := h.m.Start(context.Background(), "ses-1", StartOptions{}); err == nil {
		t.Fatal("Start succeeded")
	}
	if got := h.api.deleted(); len(got) != 0 {
		t.Fatalf("deleted = %v, want none", got)
	}
}

func TestJournalAddedBeforeCDPWork(t *testing.T) {
	h := newHarness(t)
	journaledAtCDP := -1
	h.api.onCDP = func(string) {
		journaledAtCDP = len(h.journalIDs())
	}
	mustStart(t, h.m, "ses-1")
	if journaledAtCDP != 1 {
		t.Fatalf("journal held %d rows during the CDP call, want 1", journaledAtCDP)
	}
}

func TestCDPFailureDeletesBoxAndJournalRow(t *testing.T) {
	h := newHarness(t)
	h.api.mu.Lock()
	h.api.cdpErr = errors.New("connect: browser did not start")
	h.api.mu.Unlock()
	if _, err := h.m.Start(context.Background(), "ses-1", StartOptions{}); err == nil {
		t.Fatal("Start succeeded")
	}
	if got := h.api.deleted(); len(got) != 1 || got[0] != "box-1" {
		t.Fatalf("deleted = %v, want [box-1]", got)
	}
	if ids := h.journalIDs(); len(ids) != 0 {
		t.Fatalf("journal = %v, want empty", ids)
	}
}

func TestDialFetchesFreshURLEveryTime(t *testing.T) {
	h := newHarness(t)
	mustStart(t, h.m, "ses-1")
	cm, err := h.m.Browser("ses-1")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	// The fake dial refuses, so each attach fails. What matters is the URL each dial receives.
	_, _, _ = cm.EnsureHeadless(ctx, "tab:a")
	_, _, _ = cm.EnsureHeadless(ctx, "tab:b")
	urls := h.dialedURLs()
	if len(urls) < 2 {
		t.Fatalf("dial ran %d times, want at least 2", len(urls))
	}
	if urls[0] == urls[1] {
		t.Fatalf("two dials received the same URL %q; each dial must ask for a fresh one", urls[0])
	}
	if !strings.Contains(urls[0], "token=") {
		t.Fatalf("dial did not receive the revealed URL: %q", urls[0])
	}
}

func TestConcurrentStartCreatesOneBox(t *testing.T) {
	h := newHarness(t)
	gate := make(chan struct{})
	h.api.mu.Lock()
	h.api.createGate = gate
	h.api.mu.Unlock()

	const n = 6
	var wg sync.WaitGroup
	ids := make([]string, n)
	errs := make([]error, n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			st, err := h.m.Start(context.Background(), "ses-race", StartOptions{})
			ids[i], errs[i] = st.BoxID, err
		}()
	}
	time.Sleep(50 * time.Millisecond)
	close(gate)
	wg.Wait()

	if n := h.api.createCount(); n != 1 {
		t.Fatalf("Create called %d times under concurrent Start, want 1", n)
	}
	for i := range n {
		if errs[i] != nil {
			t.Fatalf("Start %d: %v", i, errs[i])
		}
		if ids[i] != ids[0] {
			t.Fatalf("Start %d returned box %s, Start 0 returned %s", i, ids[i], ids[0])
		}
	}
}

func TestTerminateDeletesAndForgets(t *testing.T) {
	h := newHarness(t)
	st := mustStart(t, h.m, "ses-1")
	if err := h.m.Terminate(context.Background(), "ses-1"); err != nil {
		t.Fatalf("Terminate: %v", err)
	}
	if got := h.api.deleted(); len(got) != 1 || got[0] != st.BoxID {
		t.Fatalf("deleted = %v, want [%s]", got, st.BoxID)
	}
	if ids := h.journalIDs(); len(ids) != 0 {
		t.Fatalf("journal = %v, want empty", ids)
	}
	if _, err := h.m.Browser("ses-1"); !errors.Is(err, ErrNotStarted) {
		t.Fatalf("Browser after Terminate = %v, want ErrNotStarted", err)
	}
	if err := h.m.Terminate(context.Background(), "ses-1"); !errors.Is(err, ErrNotStarted) {
		t.Fatalf("second Terminate = %v, want ErrNotStarted", err)
	}
}

func TestTerminateNotStarted(t *testing.T) {
	h := newHarness(t)
	if err := h.m.Terminate(context.Background(), "ses-none"); !errors.Is(err, ErrNotStarted) {
		t.Fatalf("Terminate with no box = %v, want ErrNotStarted", err)
	}
}

func TestDelete404CountsAsDeleted(t *testing.T) {
	h := newHarness(t)
	st := mustStart(t, h.m, "ses-1")
	h.api.setDeleteErr(st.BoxID, &upstashbox.APIError{StatusCode: http.StatusNotFound, Method: "DELETE", Path: "/v2/box/" + st.BoxID, Message: "not found"})
	if err := h.m.Terminate(context.Background(), "ses-1"); err != nil {
		t.Fatalf("Terminate with 404 delete: %v", err)
	}
	if ids := h.journalIDs(); len(ids) != 0 {
		t.Fatalf("journal = %v, want empty", ids)
	}
}

func TestDeleteFailureKeepsJournalAndRetriesOnTick(t *testing.T) {
	h := newHarness(t)
	st := mustStart(t, h.m, "ses-1")
	h.api.setDeleteErr(st.BoxID, errors.New("network down"))
	if err := h.m.Terminate(context.Background(), "ses-1"); err == nil {
		t.Fatal("Terminate succeeded despite a failed delete")
	}
	if ids := h.journalIDs(); len(ids) != 1 || ids[0] != st.BoxID {
		t.Fatalf("journal = %v, want the failed box kept", ids)
	}
	if _, err := h.m.Browser("ses-1"); !errors.Is(err, ErrNotStarted) {
		t.Fatalf("Browser after failed Terminate = %v, want ErrNotStarted", err)
	}

	h.api.setDeleteErr(st.BoxID, nil)
	h.m.Tick(context.Background())
	if ids := h.journalIDs(); len(ids) != 0 {
		t.Fatalf("after retry, journal = %v, want empty", ids)
	}
}

func TestTickExpiryMarksSessionExpired(t *testing.T) {
	h := newHarness(t)
	st := mustStart(t, h.m, "ses-1")
	h.clock.Advance(time.Hour + time.Second)
	h.m.Tick(context.Background())

	if got := h.api.deleted(); len(got) != 1 || got[0] != st.BoxID {
		t.Fatalf("deleted = %v, want [%s]", got, st.BoxID)
	}
	if _, err := h.m.Browser("ses-1"); !errors.Is(err, ErrExpired) {
		t.Fatalf("Browser after expiry = %v, want ErrExpired", err)
	}
	if err := h.m.Terminate(context.Background(), "ses-1"); !errors.Is(err, ErrExpired) {
		t.Fatalf("Terminate after expiry = %v, want ErrExpired", err)
	}

	again := mustStart(t, h.m, "ses-1")
	if again.Reused || again.BoxID == st.BoxID {
		t.Fatalf("Start after expiry = %+v, want a new box", again)
	}
	if _, err := h.m.Browser("ses-1"); err != nil {
		t.Fatalf("Browser after restart: %v", err)
	}
}

func TestBrowserAfterTTLBeforeTickIsExpired(t *testing.T) {
	h := newHarness(t)
	mustStart(t, h.m, "ses-1")
	h.clock.Advance(time.Hour)
	if _, err := h.m.Browser("ses-1"); !errors.Is(err, ErrExpired) {
		t.Fatalf("Browser at the ttl = %v, want ErrExpired", err)
	}
}

func TestStartReplacesBoxPastTTLBeforeReaper(t *testing.T) {
	h := newHarness(t)
	old := mustStart(t, h.m, "ses-1")
	h.clock.Advance(time.Hour + time.Minute)
	fresh := mustStart(t, h.m, "ses-1")
	if fresh.Reused || fresh.BoxID == old.BoxID {
		t.Fatalf("Start past the ttl = %+v, want a new box", fresh)
	}
	if got := h.api.deleted(); len(got) != 1 || got[0] != old.BoxID {
		t.Fatalf("deleted = %v, want the old box %s", got, old.BoxID)
	}
	// The replacement must be the box the manager tracks, not a detached copy.
	if _, err := h.m.Browser("ses-1"); err != nil {
		t.Fatalf("Browser after the replacing Start: %v", err)
	}
	sts := h.m.Statuses()
	if len(sts) != 1 || sts[0].BoxID != fresh.BoxID {
		t.Fatalf("Statuses after the replacing Start = %+v, want exactly %s", sts, fresh.BoxID)
	}
	again := mustStart(t, h.m, "ses-1")
	if !again.Reused || again.BoxID != fresh.BoxID {
		t.Fatalf("Start after the replacement = %+v, want Reused of %s", again, fresh.BoxID)
	}
	if n := h.api.createCount(); n != 2 {
		t.Fatalf("Create called %d times, want 2", n)
	}
	h.m.Shutdown(context.Background())
	if got := h.api.deleted(); len(got) != 2 || got[1] != fresh.BoxID {
		t.Fatalf("deleted = %v, want the new box %s deleted by Shutdown", got, fresh.BoxID)
	}
	if ids := h.journalIDs(); len(ids) != 0 {
		t.Fatalf("journal after Shutdown = %v, want empty", ids)
	}
}

func TestConcurrentStartPastTTLCreatesOneReplacement(t *testing.T) {
	h := newHarness(t)
	mustStart(t, h.m, "ses-1")
	h.clock.Advance(time.Hour + time.Minute)

	gate := make(chan struct{})
	h.api.mu.Lock()
	h.api.createGate = gate
	h.api.mu.Unlock()

	const n = 2
	var wg sync.WaitGroup
	ids := make([]string, n)
	errs := make([]error, n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			st, err := h.m.Start(context.Background(), "ses-1", StartOptions{})
			ids[i], errs[i] = st.BoxID, err
		}()
	}
	close(gate)
	wg.Wait()

	for i := range n {
		if errs[i] != nil {
			t.Fatalf("Start %d: %v", i, errs[i])
		}
	}
	if ids[0] != ids[1] {
		t.Fatalf("concurrent Starts returned %s and %s, want one box", ids[0], ids[1])
	}
	// The original box plus exactly one replacement.
	if c := h.api.createCount(); c != 2 {
		t.Fatalf("Create called %d times, want 2", c)
	}
	if sts := h.m.Statuses(); len(sts) != 1 || sts[0].BoxID != ids[0] {
		t.Fatalf("Statuses = %+v, want only %s", sts, ids[0])
	}
}

func TestTickIdleTearsDownAndBrowserSaysNotStarted(t *testing.T) {
	h := newHarness(t)
	st := mustStart(t, h.m, "ses-1")
	h.clock.Advance(10 * time.Minute)
	h.m.Tick(context.Background())
	if got := h.api.deleted(); len(got) != 1 || got[0] != st.BoxID {
		t.Fatalf("deleted = %v, want [%s]", got, st.BoxID)
	}
	if _, err := h.m.Browser("ses-1"); !errors.Is(err, ErrNotStarted) {
		t.Fatalf("Browser after idle = %v, want ErrNotStarted", err)
	}
}

func TestIdleTimerPausesWhileBusy(t *testing.T) {
	h := newHarness(t)
	st := mustStart(t, h.m, "ses-1")
	h.clock.Advance(11 * time.Minute)
	h.setBusy("ses-1", true)
	h.m.Tick(context.Background())
	if got := h.api.deleted(); len(got) != 0 {
		t.Fatalf("busy session was torn down: %v", got)
	}

	h.setBusy("ses-1", false)
	h.clock.Advance(9 * time.Minute)
	h.m.Tick(context.Background())
	if got := h.api.deleted(); len(got) != 0 {
		t.Fatalf("idle clock ran during the busy stretch: deleted %v", got)
	}

	h.clock.Advance(time.Minute + time.Second)
	h.m.Tick(context.Background())
	if got := h.api.deleted(); len(got) != 1 || got[0] != st.BoxID {
		t.Fatalf("deleted = %v after the full idle period, want [%s]", got, st.BoxID)
	}
}

func TestBrowserUseResetsIdleTimer(t *testing.T) {
	h := newHarness(t)
	mustStart(t, h.m, "ses-1")
	h.clock.Advance(9 * time.Minute)
	if _, err := h.m.Browser("ses-1"); err != nil {
		t.Fatal(err)
	}
	h.clock.Advance(5 * time.Minute)
	h.m.Tick(context.Background())
	if got := h.api.deleted(); len(got) != 0 {
		t.Fatalf("recently used box was torn down: %v", got)
	}
}

func TestTickHeartbeatsOwnBoxes(t *testing.T) {
	h := newHarness(t)
	mustStart(t, h.m, "ses-1")
	h.clock.Advance(30 * time.Second)
	h.m.Tick(context.Background())
	entries, err := h.journal.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("journal = %d entries, want 1", len(entries))
	}
	e := entries[0]
	if e.Lease.PID != os.Getpid() || !e.Lease.Heartbeat.Equal(h.clock.Now()) {
		t.Fatalf("lease = %+v, want PID %d and heartbeat %v", e.Lease, os.Getpid(), h.clock.Now())
	}
}

func TestTickSweepsOrphanOfDeadProcess(t *testing.T) {
	h := newHarness(t)
	now := h.clock.Now()
	dead := Lease{PID: 4242, ProcessStart: now.Add(-3 * time.Hour), Heartbeat: now.Add(-time.Hour)}
	fresh := Lease{PID: 4343, ProcessStart: now.Add(-time.Hour), Heartbeat: now.Add(-10 * time.Second)}
	seed := []Entry{
		{BoxID: "orphan-1", AccountID: "acc-1", SessionID: "old", Kind: KindEphemeral, Label: "x", CreatedAt: now, Lease: dead},
		{BoxID: "retained-1", AccountID: "acc-1", SessionID: "old", Kind: KindRetained, LoginState: "work", Label: "x", CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour), Lease: dead},
		{BoxID: "live-1", AccountID: "acc-1", SessionID: "other", Kind: KindEphemeral, Label: "x", CreatedAt: now, Lease: fresh},
	}
	for _, e := range seed {
		if err := h.journal.Add(e); err != nil {
			t.Fatal(err)
		}
	}
	h.m.Tick(context.Background())
	if got := h.api.deleted(); len(got) != 1 || got[0] != "orphan-1" {
		t.Fatalf("deleted = %v, want only [orphan-1]", got)
	}
	left := h.journalIDs()
	if len(left) != 2 || left[0] != "live-1" || left[1] != "retained-1" {
		t.Fatalf("journal after sweep = %v, want [live-1 retained-1]", left)
	}
}

func TestSweepKeepsRowUntilDeleteSucceeds(t *testing.T) {
	h := newHarness(t)
	now := h.clock.Now()
	dead := Lease{PID: 4242, ProcessStart: now.Add(-3 * time.Hour), Heartbeat: now.Add(-time.Hour)}
	if err := h.journal.Add(Entry{BoxID: "orphan-x", AccountID: "acc-x", Kind: KindEphemeral, CreatedAt: now, Lease: dead}); err != nil {
		t.Fatal(err)
	}
	// No key for acc-x yet: the row must stay.
	h.m.Tick(context.Background())
	if ids := h.journalIDs(); len(ids) != 1 {
		t.Fatalf("row dropped without a key: %v", ids)
	}
	// The key appears and the delete fails once: the row still stays.
	h.mu.Lock()
	h.keys["acc-x"] = "key-x"
	h.mu.Unlock()
	h.api.setDeleteErr("orphan-x", errors.New("transient"))
	h.m.Tick(context.Background())
	if ids := h.journalIDs(); len(ids) != 1 {
		t.Fatalf("row dropped after a failed delete: %v", ids)
	}
	// The delete succeeds on the next pass.
	h.api.setDeleteErr("orphan-x", nil)
	h.m.Tick(context.Background())
	if ids := h.journalIDs(); len(ids) != 0 {
		t.Fatalf("row not removed after a successful delete: %v", ids)
	}
}

func TestRekeyMovesBoxAndJournal(t *testing.T) {
	h := newHarness(t)
	st := mustStart(t, h.m, "ses-1")
	if err := h.m.Rekey("ses-1", "ses-2"); err != nil {
		t.Fatalf("Rekey: %v", err)
	}
	if _, err := h.m.Browser("ses-1"); !errors.Is(err, ErrNotStarted) {
		t.Fatalf("Browser on the old id = %v, want ErrNotStarted", err)
	}
	if _, err := h.m.Browser("ses-2"); err != nil {
		t.Fatalf("Browser on the new id: %v", err)
	}
	sts := h.m.Statuses()
	if len(sts) != 1 || sts[0].SessionID != "ses-2" || sts[0].BoxID != st.BoxID {
		t.Fatalf("Statuses after Rekey = %+v", sts)
	}
	entries, err := h.journal.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].SessionID != "ses-2" {
		t.Fatalf("journal after Rekey = %+v", entries)
	}
	if err := h.m.Terminate(context.Background(), "ses-2"); err != nil {
		t.Fatalf("Terminate on the new id: %v", err)
	}
}

func TestRekeyMovesExpiredMarker(t *testing.T) {
	h := newHarness(t)
	mustStart(t, h.m, "ses-1")
	h.clock.Advance(time.Hour + time.Second)
	h.m.Tick(context.Background())
	if err := h.m.Rekey("ses-1", "ses-2"); err != nil {
		t.Fatalf("Rekey: %v", err)
	}
	if _, err := h.m.Browser("ses-2"); !errors.Is(err, ErrExpired) {
		t.Fatalf("Browser on the new id = %v, want ErrExpired", err)
	}
	if _, err := h.m.Browser("ses-1"); !errors.Is(err, ErrNotStarted) {
		t.Fatalf("Browser on the old id = %v, want ErrNotStarted", err)
	}
}

func TestRekeyRefusesSessionWithBox(t *testing.T) {
	h := newHarness(t)
	mustStart(t, h.m, "ses-1")
	mustStart(t, h.m, "ses-2")
	if err := h.m.Rekey("ses-1", "ses-2"); err == nil {
		t.Fatal("Rekey onto a session with a box succeeded")
	}
	if _, err := h.m.Browser("ses-1"); err != nil {
		t.Fatalf("refused Rekey disturbed the old session: %v", err)
	}
}

func TestStatusesSortedByCreatedAt(t *testing.T) {
	h := newHarness(t)
	mustStart(t, h.m, "ses-b")
	h.clock.Advance(time.Minute)
	mustStart(t, h.m, "ses-a")
	sts := h.m.Statuses()
	if len(sts) != 2 || sts[0].SessionID != "ses-b" || sts[1].SessionID != "ses-a" {
		t.Fatalf("Statuses order = %+v", sts)
	}
}

func TestShutdownDeletesEveryOwnBox(t *testing.T) {
	h := newHarness(t)
	mustStart(t, h.m, "ses-1")
	mustStart(t, h.m, "ses-2")
	h.m.Shutdown(context.Background())
	if got := h.api.deleted(); len(got) != 2 {
		t.Fatalf("deleted = %v, want both boxes", got)
	}
	if ids := h.journalIDs(); len(ids) != 0 {
		t.Fatalf("journal = %v, want empty", ids)
	}
	if sts := h.m.Statuses(); len(sts) != 0 {
		t.Fatalf("Statuses after Shutdown = %+v", sts)
	}
}

func TestMissingDepsReportedNotPanicked(t *testing.T) {
	m := New(Deps{})
	if _, err := m.Start(context.Background(), "ses-1", StartOptions{}); err == nil {
		t.Fatal("Start with empty Deps succeeded")
	}
	if _, err := m.Browser("ses-1"); err == nil {
		t.Fatal("Browser with empty Deps succeeded")
	}
	m.Tick(context.Background())
	m.Shutdown(context.Background())
}

func TestInstallationLabel(t *testing.T) {
	dir := t.TempDir()
	first, err := InstallationLabel(dir)
	if err != nil {
		t.Fatalf("InstallationLabel: %v", err)
	}
	if !regexp.MustCompile(`^ocode-[0-9a-f]{8}$`).MatchString(first) {
		t.Fatalf("label %q does not match ocode-<8 hex>", first)
	}
	second, err := InstallationLabel(dir)
	if err != nil {
		t.Fatalf("second InstallationLabel: %v", err)
	}
	if second != first {
		t.Fatalf("label changed between calls: %q then %q", first, second)
	}
}

func TestInstallationLabelConcurrentCallersAgree(t *testing.T) {
	dir := t.TempDir()
	const n = 8
	labels := make([]string, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			labels[i], errs[i] = InstallationLabel(dir)
		}()
	}
	wg.Wait()
	for i := range n {
		if errs[i] != nil {
			t.Fatalf("caller %d: %v", i, errs[i])
		}
		if labels[i] != labels[0] {
			t.Fatalf("caller %d got %q, caller 0 got %q", i, labels[i], labels[0])
		}
	}
}

func TestInstallationLabelRejectsCorruptID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "installation-id")
	if err := os.WriteFile(path, []byte("not-hex!\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := InstallationLabel(dir); err == nil {
		t.Fatal("corrupt installation id accepted")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "not-hex!\n" {
		t.Fatalf("corrupt installation id was overwritten: %q", data)
	}
}

// containsID reports whether id is in ids.
func containsID(ids []string, id string) bool {
	return slices.Contains(ids, id)
}

// waitFor polls cond until it holds or five seconds pass.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func TestTickKeepsStaleOrphanOfLivePeer(t *testing.T) {
	h := newHarness(t)
	now := h.clock.Now()
	h.setAlive(4242)
	// A peer that slept or stalled: its heartbeat is old, but its process is alive.
	peer := Lease{PID: 4242, ProcessStart: now.Add(-3 * time.Hour), Heartbeat: now.Add(-time.Hour)}
	if err := h.journal.Add(Entry{BoxID: "peer-1", AccountID: "acc-1", SessionID: "old", Kind: KindEphemeral, CreatedAt: now, Lease: peer}); err != nil {
		t.Fatal(err)
	}
	h.m.Tick(context.Background())
	if got := h.api.deleted(); len(got) != 0 {
		t.Fatalf("box of a live peer was deleted: %v", got)
	}
	if ids := h.journalIDs(); len(ids) != 1 || ids[0] != "peer-1" {
		t.Fatalf("journal = %v, want the live peer's row kept", ids)
	}
}

func TestTickSparesFreshOrphanOfDeadProcessForNow(t *testing.T) {
	h := newHarness(t)
	now := h.clock.Now()
	// The owner is dead, but its heartbeat is recent, so it is not an orphan yet.
	fresh := Lease{PID: 4343, ProcessStart: now.Add(-time.Hour), Heartbeat: now.Add(-10 * time.Second)}
	if err := h.journal.Add(Entry{BoxID: "fresh-1", AccountID: "acc-1", SessionID: "old", Kind: KindEphemeral, CreatedAt: now, Lease: fresh}); err != nil {
		t.Fatal(err)
	}
	h.m.Tick(context.Background())
	if got := h.api.deleted(); len(got) != 0 {
		t.Fatalf("fresh-heartbeat row was swept: %v", got)
	}
	if ids := h.journalIDs(); len(ids) != 1 {
		t.Fatalf("journal = %v, want the fresh row kept", ids)
	}
}

// TestBoxJournaledBeforeCreateWaitEnds checks the crash window: after the POST
// returns the id, the box must already be in the journal while Create polls.
func TestBoxJournaledBeforeCreateWaitEnds(t *testing.T) {
	h := newHarness(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h.api.mu.Lock()
	h.api.waitForCancel = true
	h.api.afterCreated = func(boxID string) {
		if !containsID(h.journalIDs(), boxID) {
			t.Errorf("box %s is not journaled while Create waits for it", boxID)
		}
		cancel()
	}
	h.api.mu.Unlock()

	if _, err := h.m.Start(ctx, "ses-1", StartOptions{}); err == nil {
		t.Fatal("Start succeeded although its context was cancelled during the wait")
	}
	inJournal := containsID(h.journalIDs(), "box-1")
	deleted := containsID(h.api.deleted(), "box-1")
	if !inJournal && !deleted {
		t.Fatal("box-1 is neither journaled nor deleted after a cancelled create")
	}
}

// TestCancelledCreateKeepsRowWhenDeleteFails: the row stays while the box may
// still exist, and the next Tick deletes the box and drops the row.
func TestCancelledCreateKeepsRowWhenDeleteFails(t *testing.T) {
	h := newHarness(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h.api.mu.Lock()
	h.api.waitForCancel = true
	h.api.afterCreated = func(string) { cancel() }
	h.api.mu.Unlock()
	h.api.setDeleteErr("box-1", errors.New("network down"))

	if _, err := h.m.Start(ctx, "ses-1", StartOptions{}); err == nil {
		t.Fatal("Start succeeded although its context was cancelled")
	}
	if ids := h.journalIDs(); len(ids) != 1 || ids[0] != "box-1" {
		t.Fatalf("journal = %v, want box-1 kept after the failed delete", ids)
	}
	h.api.setDeleteErr("box-1", nil)
	h.m.Tick(context.Background())
	if ids := h.journalIDs(); len(ids) != 0 {
		t.Fatalf("journal after the retry = %v, want empty", ids)
	}
}

// TestNoExpiryBoxIsDeletedAndRowRemovedOnlyIfDeleteSucceeds covers validation
// after journaling: a box with no expiry is deleted, and its row goes only with the delete.
func TestNoExpiryBoxIsDeletedAndRowRemovedOnlyIfDeleteSucceeds(t *testing.T) {
	t.Run("delete succeeds", func(t *testing.T) {
		h := newHarness(t)
		h.api.mu.Lock()
		h.api.noExpiry = true
		h.api.mu.Unlock()
		if _, err := h.m.Start(context.Background(), "ses-1", StartOptions{}); err == nil {
			t.Fatal("Start accepted a box with no expiry")
		}
		if got := h.api.deleted(); len(got) != 1 || got[0] != "box-1" {
			t.Fatalf("deleted = %v, want [box-1]", got)
		}
		if ids := h.journalIDs(); len(ids) != 0 {
			t.Fatalf("journal = %v, want empty after the delete", ids)
		}
	})
	t.Run("delete fails", func(t *testing.T) {
		h := newHarness(t)
		h.api.mu.Lock()
		h.api.noExpiry = true
		h.api.mu.Unlock()
		h.api.setDeleteErr("box-1", errors.New("network down"))
		if _, err := h.m.Start(context.Background(), "ses-1", StartOptions{}); err == nil {
			t.Fatal("Start accepted a box with no expiry")
		}
		if ids := h.journalIDs(); len(ids) != 1 || ids[0] != "box-1" {
			t.Fatalf("journal = %v, want box-1 kept after the failed delete", ids)
		}
	})
}

// TestJournalFailureInOnCreatedDeletesBox: the box exists but cannot be
// recorded. Create aborts, and the box is deleted.
func TestJournalFailureInOnCreatedDeletesBox(t *testing.T) {
	h := newHarness(t)
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	h.m.deps.Journal = NewJournal(filepath.Join(blocker, "upstash-boxes.json"))
	if _, err := h.m.Start(context.Background(), "ses-1", StartOptions{}); err == nil {
		t.Fatal("Start succeeded although the box could not be journaled")
	}
	if got := h.api.deleted(); len(got) != 1 || got[0] != "box-1" {
		t.Fatalf("deleted = %v, want [box-1]", got)
	}
}

// TestShutdownDuringCreateTearsNewBoxDown: Shutdown begins while a Start's
// create is in flight. The in-flight Start must not publish its box, and no
// other Start may create one.
func TestShutdownDuringCreateTearsNewBoxDown(t *testing.T) {
	h := newHarness(t)
	gate := make(chan struct{})
	entered := make(chan struct{}, 1)
	h.api.mu.Lock()
	h.api.createGate = gate
	h.api.createEntered = entered
	h.api.mu.Unlock()

	startErr := make(chan error, 1)
	go func() {
		_, err := h.m.Start(context.Background(), "ses-1", StartOptions{})
		startErr <- err
	}()
	<-entered

	shutDone := make(chan struct{})
	go func() {
		h.m.Shutdown(context.Background())
		close(shutDone)
	}()
	waitFor(t, "Shutdown to close the manager", h.m.isClosed)

	// closed is set, so a Start on another session is refused without a create.
	if _, err := h.m.Start(context.Background(), "ses-2", StartOptions{}); !errors.Is(err, errShutDown) {
		t.Fatalf("Start during Shutdown = %v, want errShutDown", err)
	}
	close(gate)

	if err := <-startErr; !errors.Is(err, errShutDown) {
		t.Fatalf("in-flight Start = %v, want errShutDown", err)
	}
	select {
	case <-shutDone:
	case <-time.After(5 * time.Second):
		t.Fatal("Shutdown did not return")
	}
	if n := h.api.createCount(); n != 1 {
		t.Fatalf("Create called %d times, want 1 (ses-2 must not create)", n)
	}
	if got := h.api.deleted(); len(got) != 1 || got[0] != "box-1" {
		t.Fatalf("deleted = %v, want the in-flight box [box-1] torn down", got)
	}
	if ids := h.journalIDs(); len(ids) != 0 {
		t.Fatalf("journal = %v, want empty", ids)
	}
	if sts := h.m.Statuses(); len(sts) != 0 {
		t.Fatalf("Statuses after Shutdown = %+v, want none", sts)
	}
}

func TestStartRefusedAfterShutdown(t *testing.T) {
	h := newHarness(t)
	h.m.Shutdown(context.Background())
	if _, err := h.m.Start(context.Background(), "ses-1", StartOptions{}); !errors.Is(err, errShutDown) {
		t.Fatalf("Start after Shutdown = %v, want errShutDown", err)
	}
	if n := h.api.createCount(); n != 0 {
		t.Fatalf("Create called %d times after Shutdown, want 0", n)
	}
}

// TestReapRechecksBusyUnderGate: the reaper decides on an idle box, then a turn
// starts before the reap takes the gate. The box must survive that turn.
func TestReapRechecksBusyUnderGate(t *testing.T) {
	h := newHarness(t)
	mustStart(t, h.m, "ses-1")
	h.clock.Advance(10 * time.Minute)

	var mu sync.Mutex
	calls := 0
	h.m.deps.Busy = func(string) bool {
		mu.Lock()
		defer mu.Unlock()
		calls++
		// The decision sees an idle session. The check under the gate sees a turn.
		return calls > 1
	}
	h.m.Tick(context.Background())

	mu.Lock()
	got := calls
	mu.Unlock()
	if got != 2 {
		t.Fatalf("Busy called %d times, want 2 (decision, then re-check under the gate)", got)
	}
	if d := h.api.deleted(); len(d) != 0 {
		t.Fatalf("box was torn down although a turn started before the reap: %v", d)
	}
	if _, err := h.m.Browser("ses-1"); err != nil {
		t.Fatalf("Browser after the kept box: %v", err)
	}
}

// TestHeartbeatKeepsBeatingWhileReapPassBlocks: a reap pass stuck in Busy (the
// stand-in for a slow network teardown) must not stop the lease heartbeat.
func TestHeartbeatKeepsBeatingWhileReapPassBlocks(t *testing.T) {
	h := newHarness(t)
	mustStart(t, h.m, "ses-1")

	// Run's two tickers are channels this test drives, so no tick comes from the
	// wall clock. The durations only tell the two apart.
	beat := make(chan time.Time)
	reap := make(chan time.Time)
	h.m.beatEvery = time.Second
	h.m.reapEvery = 2 * time.Second
	h.m.newTicker = func(d time.Duration) (<-chan time.Time, func()) {
		if d == h.m.beatEvery {
			return beat, func() {}
		}
		return reap, func() {}
	}

	entered := make(chan struct{})
	var enterOnce sync.Once
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseFn := func() { releaseOnce.Do(func() { close(release) }) }
	h.m.deps.Busy = func(string) bool {
		enterOnce.Do(func() { close(entered) })
		<-release
		return false
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		h.m.Run(ctx)
		close(done)
	}()
	defer func() {
		releaseFn()
		cancel()
	}()

	// One reap tick starts a pass, which heartbeats and then blocks in Busy.
	reap <- h.clock.Now()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the reap pass never reached Busy")
	}
	// The reap pass is blocked. Only the heartbeat goroutine can stamp the new time.
	h.clock.Advance(30 * time.Second)
	want := h.clock.Now()
	// The goroutine takes one beat at a time. The second send returns only after
	// the first beat's heartbeat has been written.
	beat <- h.clock.Now()
	beat <- h.clock.Now()
	entries, err := h.journal.List()
	if err != nil || len(entries) != 1 || !entries[0].Lease.Heartbeat.Equal(want) {
		t.Fatalf("while the reap pass is blocked, journal = %+v (err %v); want one box with heartbeat %v", entries, err, want)
	}

	releaseFn()
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after its context ended")
	}
}

// TestClockStripsMonotonicReading: a monotonic clock does not advance across
// machine sleep, so the stored times must not carry one.
func TestClockStripsMonotonicReading(t *testing.T) {
	h := newHarness(t)
	h.m.deps.Now = time.Now
	st := mustStart(t, h.m, "ses-1")
	if st.LastUsed != st.LastUsed.Round(0) {
		t.Fatalf("LastUsed %v carries a monotonic reading; idle timers would ignore machine sleep", st.LastUsed)
	}
}

func TestTerminateAfterExpiryClearsMarker(t *testing.T) {
	h := newHarness(t)
	mustStart(t, h.m, "ses-1")
	h.clock.Advance(time.Hour + time.Second)
	h.m.Tick(context.Background())

	if err := h.m.Terminate(context.Background(), "ses-1"); !errors.Is(err, ErrExpired) {
		t.Fatalf("Terminate after expiry = %v, want ErrExpired", err)
	}
	h.m.mu.Lock()
	markers, sessions := len(h.m.expired), len(h.m.sessions)
	h.m.mu.Unlock()
	if markers != 0 || sessions != 0 {
		t.Fatalf("after expiry and Terminate: %d expired markers and %d sessions left, want none", markers, sessions)
	}
	if err := h.m.Terminate(context.Background(), "ses-1"); !errors.Is(err, ErrNotStarted) {
		t.Fatalf("second Terminate = %v, want ErrNotStarted", err)
	}
}

// TestTickSweepsOrphanWhosePIDWasReused: a PID that is running now but was created
// after its owner's last heartbeat belongs to another process, so the owner is dead
// and its ephemeral box is swept. A PID created before the heartbeat is the owner's
// live peer, and its box is kept.
func TestTickSweepsOrphanWhosePIDWasReused(t *testing.T) {
	h := newHarness(t)
	now := h.clock.Now()
	h.setAlive(4242)
	h.setCreated(4242, now.Add(-10*time.Minute))
	reused := Lease{PID: 4242, ProcessStart: now.Add(-3 * time.Hour), Heartbeat: now.Add(-time.Hour)}
	h.setAlive(4343)
	h.setCreated(4343, now.Add(-2*time.Hour))
	peer := Lease{PID: 4343, ProcessStart: now.Add(-3 * time.Hour), Heartbeat: now.Add(-time.Hour)}
	for _, e := range []Entry{
		{BoxID: "reused-1", AccountID: "acc-1", SessionID: "old", Kind: KindEphemeral, CreatedAt: now, Lease: reused},
		{BoxID: "peer-1", AccountID: "acc-1", SessionID: "old", Kind: KindEphemeral, CreatedAt: now, Lease: peer},
	} {
		if err := h.journal.Add(e); err != nil {
			t.Fatal(err)
		}
	}
	h.m.Tick(context.Background())
	if got := h.api.deleted(); len(got) != 1 || got[0] != "reused-1" {
		t.Fatalf("deleted = %v, want only [reused-1]", got)
	}
	if ids := h.journalIDs(); len(ids) != 1 || ids[0] != "peer-1" {
		t.Fatalf("journal = %v, want the live peer's row kept", ids)
	}
}

// TestStrayPauseNotFoundRemovesRow: a stray whose pause the API answers with 404 is
// gone at the API. Its row is removed at once, and no later tick pauses it again.
func TestStrayPauseNotFoundRemovesRow(t *testing.T) {
	h := newHarness(t)
	now := h.clock.Now()
	dead := Lease{PID: 4242, ProcessStart: now.Add(-3 * time.Hour), Heartbeat: now.Add(-time.Hour)}
	if err := h.journal.Add(Entry{BoxID: "stray-gone", AccountID: "acc-1", SessionID: "old", Kind: KindRetained, LoginState: "work", CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour), Lease: dead}); err != nil {
		t.Fatal(err)
	}
	h.api.setPauseErr(notFound(http.MethodPost, "/v2/box/stray-gone/pause"))
	h.m.Tick(context.Background())
	if ids := h.journalIDs(); len(ids) != 0 {
		t.Fatalf("journal = %v after a 404 pause, want the row removed", ids)
	}
	h.m.Tick(context.Background())
	if got := h.api.pauseCalls(); len(got) != 1 {
		t.Fatalf("pause calls = %v, want one: a 404 is not retried", got)
	}
}

// TestOwnDeletingRowIsRetriedNextTick: when releasing a delete claim failed after a
// failed delete, the row stays Deleting under this process's own lease. That lease
// stays fresh while the process lives, so the next tick must still retry the delete.
func TestOwnDeletingRowIsRetriedNextTick(t *testing.T) {
	h := newHarness(t)
	now := h.clock.Now()
	own := Lease{PID: os.Getpid(), ProcessStart: h.m.lease.ProcessStart, Heartbeat: now}
	if err := h.journal.Add(Entry{BoxID: "stuck-1", AccountID: "acc-1", SessionID: "old", Kind: KindRetained, LoginState: "work", CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour), Deleting: true, Lease: own}); err != nil {
		t.Fatal(err)
	}
	h.m.Tick(context.Background())
	if got := h.api.deleted(); len(got) != 1 || got[0] != "stuck-1" {
		t.Fatalf("deleted = %v, want [stuck-1] on the next tick", got)
	}
	if ids := h.journalIDs(); len(ids) != 0 {
		t.Fatalf("journal = %v, want the row removed", ids)
	}
}
