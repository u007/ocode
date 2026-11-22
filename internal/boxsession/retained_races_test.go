package boxsession

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/u007/ocode/internal/upstashbox"
)

// apiCtl steers the box API a rig hands the manager: it can fail deletes, and run a
// hook after each pause returns.
type apiCtl struct {
	mu         sync.Mutex
	deleteErr  error
	afterPause func()
}

func (c *apiCtl) setDeleteErr(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.deleteErr = err
}

func (c *apiCtl) setAfterPause(f func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.afterPause = f
}

// steeredAPI routes a box API through an apiCtl.
type steeredAPI struct {
	BoxAPI
	ctl *apiCtl
}

func (a steeredAPI) Delete(ctx context.Context, id string) error {
	a.ctl.mu.Lock()
	err := a.ctl.deleteErr
	a.ctl.mu.Unlock()
	if err != nil {
		return err
	}
	return a.BoxAPI.Delete(ctx, id)
}

func (a steeredAPI) Pause(ctx context.Context, id string) error {
	err := a.BoxAPI.Pause(ctx, id)
	a.ctl.mu.Lock()
	after := a.ctl.afterPause
	a.ctl.mu.Unlock()
	if after != nil {
		after()
	}
	return err
}

// steerAPI makes every box API of the rig's manager go through the returned apiCtl.
func steerAPI(r *loginRig) *apiCtl {
	ctl := &apiCtl{}
	orig := r.h.m.deps.NewAPI
	r.h.m.deps.NewAPI = func(apiKey string) (BoxAPI, error) {
		api, err := orig(apiKey)
		if err != nil {
			return nil, err
		}
		return steeredAPI{BoxAPI: api, ctl: ctl}, nil
	}
	return ctl
}

// TestLeftoverRetainedBoxOfFailedCreateIsNotDeletedUnderNewSession: a fresh retained
// box whose attach failed and whose discard delete failed stays queued for a delete
// retry. A later session that resumes that box must not have the retry delete it
// once that session has released it.
func TestLeftoverRetainedBoxOfFailedCreateIsNotDeletedUnderNewSession(t *testing.T) {
	r := newRetainedRig(t)
	ctl := steerAPI(r)
	r.setAssign("ses-1", "work")
	ctl.setDeleteErr(errors.New("delete down"))
	r.h.api.cdpErr = errors.New("connect refused")
	if _, err := r.h.m.Start(context.Background(), "ses-1", StartOptions{Retain: true}); err == nil {
		t.Fatal("Start with a refused connect: want an error")
	}
	ids := r.h.journalIDs()
	if len(ids) != 1 {
		t.Fatalf("journal = %v, want the leftover retained row", ids)
	}
	box := ids[0]
	r.h.m.mu.Lock()
	_, queued := r.h.m.undeleted[box]
	r.h.m.mu.Unlock()
	if !queued {
		t.Fatal("precondition: the failed discard should queue the box for a delete retry")
	}

	r.h.api.cdpErr = nil
	ctl.setDeleteErr(nil)
	r.setAssign("ses-2", "work")
	st := startRetained(t, r.h.m, "ses-2")
	if !st.Resumed || st.BoxID != box {
		t.Fatalf("Status = %+v, want the leftover box %s resumed", st, box)
	}
	if err := r.h.m.Terminate(context.Background(), "ses-2"); err != nil {
		t.Fatalf("Terminate: %v", err)
	}
	r.h.m.Tick(context.Background())

	if got := r.h.api.deleted(); len(got) != 0 {
		t.Fatalf("deleted = %v, want the resumed retained box kept (paused, not deleted)", got)
	}
	if s := r.h.api.boxStatus(box); s != upstashbox.StatusPaused {
		t.Fatalf("box status = %q, want paused", s)
	}
}

// TestSweepSnapshotOfExpiredRowLosesToResume: the sweep snapshots a paused row past
// its delete-at, and a Start resumes that box before the sweep claims it. The sweep
// must neither delete the box nor touch its row.
func TestSweepSnapshotOfExpiredRowLosesToResume(t *testing.T) {
	r := newRetainedRig(t)
	r.setAssign("ses-1", "work")
	first := startRetained(t, r.h.m, "ses-1")
	r.h.clock.Advance(10 * time.Minute)
	r.h.m.Tick(context.Background())
	r.h.clock.Advance(retainPeriod + time.Minute)

	r.setAssign("ses-2", "work")
	fired := false
	r.h.m.beforeClaim = func(id string) {
		if id != first.BoxID || fired {
			return
		}
		fired = true
		if _, err := r.h.m.Start(context.Background(), "ses-2", StartOptions{Retain: true}); err != nil {
			t.Errorf("Start inside the sweep window: %v", err)
		}
	}
	r.h.m.Tick(context.Background())

	if !fired {
		t.Fatal("the seam never fired: the paused row was not in the sweep snapshot")
	}
	if got := r.h.api.deleted(); len(got) != 0 {
		t.Fatalf("deleted = %v, want none: the box was resumed by ses-2", got)
	}
	if got := r.h.api.pauseCalls(); !slices.Equal(got, []string{first.BoxID}) {
		t.Fatalf("pause calls = %v, want only the idle pause", got)
	}
	row := retainedRowOf(t, r, "work")
	if row.Paused || row.SessionID != "ses-2" {
		t.Fatalf("row = %+v, want unpaused and held by ses-2", row)
	}
	if got := r.holder("work"); got != "ses-2" {
		t.Fatalf("login state held by %q, want ses-2", got)
	}
}

// TestStraySnapshotLosesToResumeAndKeepsFreshValues: the sweep snapshots an unpaused
// row of a dead process, and a Start claims that box before the sweep pauses it. The
// pause must not run, and the row keeps the claim's last-use time and delete-at.
func TestStraySnapshotLosesToResumeAndKeepsFreshValues(t *testing.T) {
	r := newRetainedRig(t)
	ctx := context.Background()
	now := r.h.clock.Now()
	stray, err := r.h.api.Create(ctx, upstashbox.CreateOptions{Name: "stray", Browser: true, Labels: []string{"ocode-test00aa"}})
	if err != nil {
		t.Fatalf("Create stray: %v", err)
	}
	deadLease := Lease{PID: 4242, ProcessStart: now.Add(-3 * time.Hour), Heartbeat: now.Add(-time.Hour)}
	if err := r.h.journal.Add(Entry{BoxID: stray.ID, AccountID: "acc-1", SessionID: "old", Kind: KindRetained, LoginState: "work", CreatedAt: now, ExpiresAt: now.Add(retainPeriod), LastUsed: now.Add(-time.Hour), Lease: deadLease}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	r.setAssign("ses-2", "work")
	fired := false
	r.h.m.beforeClaim = func(id string) {
		if id != stray.ID || fired {
			return
		}
		fired = true
		if _, err := r.h.m.Start(ctx, "ses-2", StartOptions{Retain: true}); err != nil {
			t.Errorf("Start inside the sweep window: %v", err)
		}
	}
	resumedAt := r.h.clock.Now()
	r.h.m.Tick(ctx)

	if !fired {
		t.Fatal("the seam never fired: the stray row was not in the sweep snapshot")
	}
	if got := r.h.api.pauseCalls(); len(got) != 0 {
		t.Fatalf("pause calls = %v, want none: the box was resumed by ses-2", got)
	}
	row := retainedRowOf(t, r, "work")
	if row.Paused || row.SessionID != "ses-2" {
		t.Fatalf("row = %+v, want unpaused and held by ses-2", row)
	}
	if !row.LastUsed.Equal(resumedAt) || !row.ExpiresAt.Equal(resumedAt.Add(retainPeriod)) {
		t.Fatalf("row last-use %v and delete-at %v, want the claim's %v and %v", row.LastUsed, row.ExpiresAt, resumedAt, resumedAt.Add(retainPeriod))
	}
}

// TestQueuedPauseLosesToResumeAfterListing: a failed pause leaves a box queued. A
// Start then resumes that box, and the retry, which listed the box before the
// Start, must not pause it under the session that now holds it.
func TestQueuedPauseLosesToResumeAfterListing(t *testing.T) {
	r := newRetainedRig(t)
	r.setAssign("ses-1", "work")
	st := startRetained(t, r.h.m, "ses-1")
	r.h.api.setPauseErr(errors.New("network down"))
	if err := r.h.m.Terminate(context.Background(), "ses-1"); err == nil {
		t.Fatal("Terminate with a failing pause: want the error returned")
	}
	r.h.api.setPauseErr(nil)

	r.setAssign("ses-2", "work")
	fired := false
	r.h.m.beforeClaim = func(id string) {
		if id != st.BoxID || fired {
			return
		}
		fired = true
		if _, err := r.h.m.Start(context.Background(), "ses-2", StartOptions{Retain: true}); err != nil {
			t.Errorf("Start inside the retry window: %v", err)
		}
	}
	before := len(r.h.api.pauseCalls())
	r.h.m.Tick(context.Background())

	if !fired {
		t.Fatal("the seam never fired: the box was not queued for a pause retry")
	}
	if got := r.h.api.pauseCalls(); len(got) != before {
		t.Fatalf("pause calls = %v, want no pause of the box ses-2 resumed", got)
	}
	row := retainedRowOf(t, r, "work")
	if row.Paused || row.SessionID != "ses-2" {
		t.Fatalf("row = %+v, want unpaused and held by ses-2", row)
	}
	r.h.m.mu.Lock()
	_, still := r.h.m.unpaused[st.BoxID]
	r.h.m.mu.Unlock()
	if still {
		t.Fatal("the resumed box is still queued for a pause")
	}
}

// TestTickDuringResumeLeavesResumedBoxAlone: a Tick that runs while a resume is in
// its attach must neither pause nor delete the box the resume is bringing up.
func TestTickDuringResumeLeavesResumedBoxAlone(t *testing.T) {
	r := newRetainedRig(t)
	r.setAssign("ses-1", "work")
	first := startRetained(t, r.h.m, "ses-1")
	r.h.clock.Advance(10 * time.Minute)
	r.h.m.Tick(context.Background())

	r.setAssign("ses-2", "work")
	fired := false
	r.h.api.onCDP = func(id string) {
		if id != first.BoxID || fired {
			return
		}
		fired = true
		r.h.m.Tick(context.Background())
	}
	if _, err := r.h.m.Start(context.Background(), "ses-2", StartOptions{Retain: true}); err != nil {
		t.Fatalf("Start(ses-2, retain): %v", err)
	}
	if !fired {
		t.Fatal("the CDP hook never ran")
	}
	if got := r.h.api.pauseCalls(); !slices.Equal(got, []string{first.BoxID}) {
		t.Fatalf("pause calls = %v, want only the idle pause", got)
	}
	if got := r.h.api.deleted(); len(got) != 0 {
		t.Fatalf("deleted = %v, want none", got)
	}
}

// TestPauseNotFoundRemovesRetainedRow: a pause the API answers with 404 means the
// box is gone. Teardown then returns no error and the row is removed.
func TestPauseNotFoundRemovesRetainedRow(t *testing.T) {
	r := newRetainedRig(t)
	r.setAssign("ses-1", "work")
	st := startRetained(t, r.h.m, "ses-1")
	if err := r.h.api.Delete(context.Background(), st.BoxID); err != nil {
		t.Fatalf("Delete out of band: %v", err)
	}
	if err := r.h.m.Terminate(context.Background(), "ses-1"); err != nil {
		t.Fatalf("Terminate of a box deleted outside ocode: %v", err)
	}
	if got := r.h.journalIDs(); len(got) != 0 {
		t.Fatalf("journal = %v, want the row removed", got)
	}
	if _, err := r.h.m.Browser("ses-1"); !errors.Is(err, ErrNotStarted) {
		t.Fatalf("Browser after Terminate = %v, want ErrNotStarted", err)
	}
}

// TestQueuedPauseOfBoxDeletedOutsideOcodeIsDropped: a queued pause whose box is
// then deleted outside ocode is dropped on the retry, not retried forever.
func TestQueuedPauseOfBoxDeletedOutsideOcodeIsDropped(t *testing.T) {
	r := newRetainedRig(t)
	r.setAssign("ses-1", "work")
	st := startRetained(t, r.h.m, "ses-1")
	r.h.api.setPauseErr(errors.New("network down"))
	if err := r.h.m.Terminate(context.Background(), "ses-1"); err == nil {
		t.Fatal("Terminate with a failing pause: want the error returned")
	}
	r.h.api.setPauseErr(nil)
	if err := r.h.api.Delete(context.Background(), st.BoxID); err != nil {
		t.Fatalf("Delete out of band: %v", err)
	}
	before := len(r.h.api.pauseCalls())

	r.h.m.Tick(context.Background())
	r.h.m.Tick(context.Background())

	if got := len(r.h.api.pauseCalls()); got != before+1 {
		t.Fatalf("pause calls = %d, want one 404 retry and then no more", got-before)
	}
	if got := r.h.journalIDs(); len(got) != 0 {
		t.Fatalf("journal = %v, want the row removed", got)
	}
	r.h.m.mu.Lock()
	_, still := r.h.m.unpaused[st.BoxID]
	r.h.m.mu.Unlock()
	if still {
		t.Fatal("the 404 box is still queued for a pause")
	}
}

// TestShutdownSaveFloorKeepsRetainedSaveBeforePause: on a deadline that is already
// gone, a retained box's final save still runs, and it runs before the pause that
// would otherwise be the box's last state.
func TestShutdownSaveFloorKeepsRetainedSaveBeforePause(t *testing.T) {
	r := newRetainedRig(t)
	r.setAssign("ses-r", "work")
	r.browser.cookies = []json.RawMessage{testCookie("sid", "v1")}
	ret := startRetained(t, r.h.m, "ses-r")

	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	time.Sleep(5 * time.Millisecond)
	r.h.m.Shutdown(ctx)

	i, j := r.ev.index("read-cookies"), r.ev.index("pause-start")
	if i < 0 || j <= i {
		t.Fatalf("event order %v, want the final save before the pause", r.ev.all())
	}
	if got := r.h.api.pauseCalls(); !slices.Equal(got, []string{ret.BoxID}) {
		t.Fatalf("pause calls = %v, want [%s]", got, ret.BoxID)
	}
	if got := cookieNames(t, r.savedState("work").Cookies); !slices.Equal(got, []string{"sid"}) {
		t.Fatalf("saved cookies = %v, want [sid] from the final save", got)
	}
}

// TestRekeyCarriesPausedMarker: a session whose retained box the reaper paused keeps
// ErrPaused under its new id, and the old id reports ErrNotStarted.
func TestRekeyCarriesPausedMarker(t *testing.T) {
	r := newRetainedRig(t)
	r.setAssign("ses-1", "work")
	startRetained(t, r.h.m, "ses-1")
	r.h.clock.Advance(10 * time.Minute)
	r.h.m.Tick(context.Background())

	if err := r.h.m.Rekey("ses-1", "ses-9"); err != nil {
		t.Fatalf("Rekey: %v", err)
	}
	if _, err := r.h.m.Browser("ses-9"); !errors.Is(err, ErrPaused) {
		t.Fatalf("Browser(ses-9) = %v, want ErrPaused", err)
	}
	if _, err := r.h.m.Browser("ses-1"); !errors.Is(err, ErrNotStarted) {
		t.Fatalf("Browser(ses-1) = %v, want ErrNotStarted", err)
	}
}

// TestJournalWriteFailureAfterPauseRetriesWithoutSecondPause: the pause succeeds but
// the journal write after it fails. The Tick retry writes the row without pausing
// the box again.
func TestJournalWriteFailureAfterPauseRetriesWithoutSecondPause(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions, so the journal write cannot be made to fail")
	}
	r := newRetainedRig(t)
	ctl := steerAPI(r)
	r.setAssign("ses-1", "work")
	startRetained(t, r.h.m, "ses-1")

	dir := filepath.Dir(r.h.journal.path)
	ctl.setAfterPause(func() {
		if err := os.Chmod(dir, 0o500); err != nil {
			t.Errorf("chmod journal dir: %v", err)
		}
	})
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	if err := r.h.m.Terminate(context.Background(), "ses-1"); err == nil {
		t.Fatal("Terminate with a failing journal write: want the error returned")
	}
	ctl.setAfterPause(nil)
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatalf("restore journal dir: %v", err)
	}
	before := len(r.h.api.pauseCalls())
	if row := retainedRowOf(t, r, "work"); row.Paused {
		t.Fatalf("row = %+v, want unpaused while the journal write is pending", row)
	}

	r.h.m.Tick(context.Background())

	if got := len(r.h.api.pauseCalls()); got != before {
		t.Fatalf("the retry paused the box again (%d more pauses), want the journal write only", got-before)
	}
	if !retainedRowOf(t, r, "work").Paused {
		t.Fatal("row not paused after the retry")
	}
}

// TestFailedClaimedDeleteOfDeadOwnersRowIsRetriedNextTick: a delete claimed for a
// dead owner's expired unpaused row fails. The claim is given back, and the next Tick
// in the same process deletes the box.
func TestFailedClaimedDeleteOfDeadOwnersRowIsRetriedNextTick(t *testing.T) {
	r := newRetainedRig(t)
	ctl := steerAPI(r)
	ctx := context.Background()
	now := r.h.clock.Now()
	box, err := r.h.api.Create(ctx, upstashbox.CreateOptions{Name: "orphan", Browser: true, Labels: []string{"ocode-test00aa"}})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	deadLease := Lease{PID: 4242, ProcessStart: now.Add(-3 * time.Hour), Heartbeat: now.Add(-time.Hour)}
	if err := r.h.journal.Add(Entry{BoxID: box.ID, AccountID: "acc-1", SessionID: "old", Kind: KindRetained, LoginState: "work", CreatedAt: now, ExpiresAt: now.Add(-time.Minute), LastUsed: now.Add(-retainPeriod), Lease: deadLease}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	ctl.setDeleteErr(errors.New("delete down"))
	r.h.m.Tick(ctx)
	if got := r.h.journalIDs(); !slices.Equal(got, []string{box.ID}) {
		t.Fatalf("journal = %v, want the row kept after the failed delete", got)
	}
	entries, err := r.h.journal.List()
	if err != nil || len(entries) != 1 || entries[0].Deleting || entries[0].Lease.PID != 0 {
		t.Fatalf("row after the failed delete = %+v (err %v), want the claim given back", entries, err)
	}

	ctl.setDeleteErr(nil)
	r.h.m.Tick(ctx)
	if got := r.h.journalIDs(); len(got) != 0 {
		t.Fatalf("journal = %v, want the row removed by the retry", got)
	}
	if s := r.h.api.boxStatus(box.ID); s != upstashbox.StatusDeleted {
		t.Fatalf("box status = %q, want deleted by the retry", s)
	}
}

// TestFailedStrayPauseIsRetriedBySweepNextTick: a stray pause that fails gives the
// claim back, and the next Tick pauses the box.
func TestFailedStrayPauseIsRetriedBySweepNextTick(t *testing.T) {
	r := newRetainedRig(t)
	ctx := context.Background()
	now := r.h.clock.Now()
	stray, err := r.h.api.Create(ctx, upstashbox.CreateOptions{Name: "stray", Browser: true, Labels: []string{"ocode-test00aa"}})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	deadLease := Lease{PID: 4242, ProcessStart: now.Add(-3 * time.Hour), Heartbeat: now.Add(-time.Hour)}
	if err := r.h.journal.Add(Entry{BoxID: stray.ID, AccountID: "acc-1", SessionID: "old", Kind: KindRetained, LoginState: "work", CreatedAt: now, ExpiresAt: now.Add(retainPeriod), LastUsed: now.Add(-time.Hour), Lease: deadLease}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	r.h.api.setPauseErr(errors.New("network down"))
	r.h.m.Tick(ctx)
	if row := retainedRowOf(t, r, "work"); row.Paused || row.Lease.PID != 0 {
		t.Fatalf("row = %+v, want unpaused with the claim given back", row)
	}
	r.h.api.setPauseErr(nil)
	r.h.m.Tick(ctx)
	if !retainedRowOf(t, r, "work").Paused {
		t.Fatal("row not paused by the next Tick")
	}
	if got := r.h.api.pauseCalls(); !slices.Equal(got, []string{stray.ID, stray.ID}) {
		t.Fatalf("pause calls = %v, want the failed pause then the retry", got)
	}
}

// TestSetPausedZeroTimesKeepRowValues: the zero time means keep, so a pause that
// names no time leaves the row's last use and delete-at alone.
func TestSetPausedZeroTimesKeepRowValues(t *testing.T) {
	j, _ := newTestJournal(t)
	lastUsed := jt0.Add(-time.Hour)
	expires := jt0.Add(time.Hour)
	if err := j.Add(Entry{BoxID: "box-1", AccountID: "acc-1", SessionID: "s", Kind: KindRetained, LoginState: "work", CreatedAt: jt0, ExpiresAt: expires, LastUsed: lastUsed, Lease: ownerLease(1, jt0, jt0)}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := j.SetPaused("box-1", time.Time{}, time.Time{}); err != nil {
		t.Fatalf("SetPaused: %v", err)
	}
	row, _, err := j.Retained("acc-1", "work")
	if err != nil || !row.Paused || !row.LastUsed.Equal(lastUsed) || !row.ExpiresAt.Equal(expires) {
		t.Fatalf("row = %+v (err %v), want paused with the values kept", row, err)
	}
}

// TestClaimDueAndResumeAreMutuallyExclusive: whichever of a delete claim and a
// resume lands first, the other is refused.
func TestClaimDueAndResumeAreMutuallyExclusive(t *testing.T) {
	now := jt0
	alive := func(l Lease) bool { return l.PID == 4343 }
	dead := Lease{PID: 4242, ProcessStart: now.Add(-3 * time.Hour), Heartbeat: now.Add(-time.Hour)}
	claimer := Lease{PID: 9001, ProcessStart: now, Heartbeat: now}
	peer := Lease{PID: 4343, ProcessStart: now.Add(-time.Hour), Heartbeat: now}
	expired := now.Add(-time.Minute)

	t.Run("claim wins, resume loses", func(t *testing.T) {
		j, _ := newTestJournal(t)
		j.bindAlive(alive)
		if err := j.Add(Entry{BoxID: "box-1", AccountID: "acc-1", SessionID: "old", Kind: KindRetained, LoginState: "work", CreatedAt: now, ExpiresAt: expired, Paused: true, Lease: dead}); err != nil {
			t.Fatalf("Add: %v", err)
		}
		action, ok, err := j.ClaimDue("box-1", now, claimer, leaseTTL, alive)
		if err != nil || !ok || action != "delete" {
			t.Fatalf("ClaimDue = %q, %v, %v; want a delete claim", action, ok, err)
		}
		if err := j.Resume("box-1", "ses-2", peer, now, now.Add(time.Hour)); !errors.Is(err, ErrRetainedBusy) {
			t.Fatalf("Resume of a claimed row = %v, want ErrRetainedBusy", err)
		}
	})

	t.Run("resume wins, claim loses", func(t *testing.T) {
		j, _ := newTestJournal(t)
		j.bindAlive(alive)
		if err := j.Add(Entry{BoxID: "box-2", AccountID: "acc-1", SessionID: "old", Kind: KindRetained, LoginState: "work", CreatedAt: now, ExpiresAt: expired, Paused: true, Lease: dead}); err != nil {
			t.Fatalf("Add: %v", err)
		}
		if err := j.Resume("box-2", "ses-2", peer, now, now.Add(time.Hour)); err != nil {
			t.Fatalf("Resume: %v", err)
		}
		_, ok, err := j.ClaimDue("box-2", now, claimer, leaseTTL, alive)
		if err != nil || ok {
			t.Fatalf("ClaimDue of a resumed row = ok %v, err %v; want refused", ok, err)
		}
	})

	t.Run("live peer's running row is refused", func(t *testing.T) {
		j, _ := newTestJournal(t)
		j.bindAlive(alive)
		if err := j.Add(Entry{BoxID: "box-3", AccountID: "acc-1", SessionID: "other", Kind: KindRetained, LoginState: "work", CreatedAt: now, ExpiresAt: now.Add(time.Hour), Lease: peer}); err != nil {
			t.Fatalf("Add: %v", err)
		}
		if err := j.Resume("box-3", "ses-2", claimer, now, now.Add(time.Hour)); !errors.Is(err, ErrRetainedBusy) {
			t.Fatalf("Resume of a live peer's row = %v, want ErrRetainedBusy", err)
		}
	})
}

// TestDeletingRowOfDeadClaimerIsClaimable: a claimer that dies mid-delete leaves a
// Deleting row that another process can claim, and a live claimer's row cannot be
// taken.
func TestDeletingRowOfDeadClaimerIsClaimable(t *testing.T) {
	j, _ := newTestJournal(t)
	now := jt0
	dead := Lease{PID: 4242, ProcessStart: now.Add(-3 * time.Hour), Heartbeat: now.Add(-time.Hour)}
	firstClaimer := Lease{PID: 4343, ProcessStart: now.Add(-time.Hour), Heartbeat: now}
	if err := j.Add(Entry{BoxID: "box-1", AccountID: "acc-1", SessionID: "old", Kind: KindRetained, LoginState: "work", CreatedAt: now, ExpiresAt: now.Add(-time.Minute), Paused: true, Lease: dead}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	aliveFirst := func(l Lease) bool { return l.PID == 4343 }
	if action, ok, err := j.ClaimDue("box-1", now, firstClaimer, leaseTTL, aliveFirst); err != nil || !ok || action != "delete" {
		t.Fatalf("first ClaimDue = %q, %v, %v; want a delete claim", action, ok, err)
	}

	other := Lease{PID: 5555, ProcessStart: now, Heartbeat: now}
	if _, ok, err := j.ClaimDue("box-1", now, other, leaseTTL, aliveFirst); err != nil || ok {
		t.Fatalf("ClaimDue while the first claimer is alive = ok %v, err %v; want refused", ok, err)
	}

	later := now.Add(2 * leaseTTL)
	noneAlive := func(Lease) bool { return false }
	due, strays, err := j.RetainedDue(later, other, leaseTTL, noneAlive)
	if err != nil || len(strays) != 0 || len(due) != 1 || due[0].BoxID != "box-1" {
		t.Fatalf("RetainedDue of a dead claimer's row = due %v strays %v err %v; want box-1 due", ids(due), ids(strays), err)
	}
	claimer := Lease{PID: 5555, ProcessStart: later, Heartbeat: later}
	if action, ok, err := j.ClaimDue("box-1", later, claimer, leaseTTL, noneAlive); err != nil || !ok || action != "delete" {
		t.Fatalf("ClaimDue of a dead claimer's row = %q, %v, %v; want a delete claim", action, ok, err)
	}
	if err := j.Resume("box-1", "ses-2", claimer, later, later.Add(time.Hour)); !errors.Is(err, ErrRetainedBusy) {
		t.Fatalf("Resume of a row being deleted = %v, want ErrRetainedBusy", err)
	}
}

// TestRekeyUndoTouchesOnlyMovedRows: the undo of a rekey moves back the rows that
// rekey moved, and leaves a row the new session already had where it was.
func TestRekeyUndoTouchesOnlyMovedRows(t *testing.T) {
	j, _ := newTestJournal(t)
	for _, e := range []Entry{
		{BoxID: "box-1", SessionID: "old", Kind: KindEphemeral, CreatedAt: jt0},
		{BoxID: "box-2", SessionID: "new", Kind: KindEphemeral, CreatedAt: jt0},
	} {
		if err := j.Add(e); err != nil {
			t.Fatal(err)
		}
	}
	moved, err := j.rekeyMoved("old", "new")
	if err != nil || !slices.Equal(moved, []string{"box-1"}) {
		t.Fatalf("rekeyMoved = %v, %v; want [box-1]", moved, err)
	}
	if err := j.restoreSession(moved, "old"); err != nil {
		t.Fatalf("restoreSession: %v", err)
	}
	got := map[string]string{}
	for _, e := range mustList(t, j) {
		got[e.BoxID] = e.SessionID
	}
	if got["box-1"] != "old" || got["box-2"] != "new" {
		t.Fatalf("sessions after the undo = %v, want box-1 old and box-2 new", got)
	}
}
