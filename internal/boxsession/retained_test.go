package boxsession

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/u007/ocode/internal/upstashbox"
)

// retainPeriod is the retain period the retained tests give their account.
const retainPeriod = 14 * 24 * time.Hour

// newRetainedRig is a login-state rig whose account keeps retained boxes for retainPeriod.
func newRetainedRig(t *testing.T) *loginRig {
	t.Helper()
	r := newLoginRig(t)
	acc := r.h.account
	acc.Retain = retainPeriod
	r.h.setAccount(acc)
	return r
}

// startRetained starts a retained box for session and fails the test on error.
func startRetained(t *testing.T, m *Manager, session string) Status {
	t.Helper()
	st, err := m.Start(context.Background(), session, StartOptions{Retain: true})
	if err != nil {
		t.Fatalf("Start(%s, retain): %v", session, err)
	}
	return st
}

// retainedRowOf returns the retained journal row of the rig's account and login state.
func retainedRowOf(t *testing.T, r *loginRig, loginState string) Entry {
	t.Helper()
	row, found, err := r.h.journal.Retained("acc-1", loginState)
	if err != nil || !found {
		t.Fatalf("Retained(%s) = found %v, err %v; want a row", loginState, found, err)
	}
	return row
}

func TestRetainNeedsLoginStateCreatesNothing(t *testing.T) {
	r := newRetainedRig(t)
	_, err := r.h.m.Start(context.Background(), "ses-1", StartOptions{Retain: true})
	if !errors.Is(err, ErrRetainNeedsLoginState) {
		t.Fatalf("Start(retain) = %v, want ErrRetainNeedsLoginState", err)
	}
	if n := r.h.api.createCount(); n != 0 {
		t.Fatalf("creates = %d, want none", n)
	}
	if got := r.h.journalIDs(); len(got) != 0 {
		t.Fatalf("journal = %v, want empty", got)
	}
}

func TestRetainCreatesPersistentBoxKeyedByLoginState(t *testing.T) {
	r := newRetainedRig(t)
	r.setAssign("ses-1", "work")
	st := startRetained(t, r.h.m, "ses-1")

	if st.Kind != KindRetained || st.LoginState != "work" || st.Resumed || st.Reused {
		t.Fatalf("Status = %+v, want a created retained box for login state work", st)
	}
	want := mt0.Add(retainPeriod)
	if !st.ExpiresAt.Equal(want) {
		t.Fatalf("Status.ExpiresAt = %v, want the delete-at %v", st.ExpiresAt, want)
	}
	opts := r.h.api.creates[0]
	if opts.Ephemeral || opts.TTLSeconds != 0 || !opts.Browser {
		t.Fatalf("create options = %+v, want a persistent browser box with no ttl", opts)
	}
	row := retainedRowOf(t, r, "work")
	if row.Kind != KindRetained || row.Paused || row.SessionID != "ses-1" || row.BoxID != st.BoxID {
		t.Fatalf("row = %+v, want an unpaused retained row of ses-1", row)
	}
	if !row.ExpiresAt.Equal(want) || row.Lease.PID != os.Getpid() {
		t.Fatalf("row expiry %v lease %+v, want %v and this process", row.ExpiresAt, row.Lease, want)
	}

	// A second retained start reuses the live box, and a plain start does too.
	again := startRetained(t, r.h.m, "ses-1")
	if !again.Reused || again.BoxID != st.BoxID || r.h.api.createCount() != 1 {
		t.Fatalf("second retained Start = %+v with %d creates, want reuse of %s", again, r.h.api.createCount(), st.BoxID)
	}
	plain := mustStart(t, r.h.m, "ses-1")
	if !plain.Reused || plain.Kind != KindRetained {
		t.Fatalf("plain Start = %+v, want the retained box reused", plain)
	}
}

func TestRetainWithZeroRetainRefusedBeforeLiveBoxIsTouched(t *testing.T) {
	r := newRetainedRig(t)
	acc := r.h.account
	acc.Retain = 0
	r.h.setAccount(acc)
	r.setAssign("ses-1", "work")
	mustStart(t, r.h.m, "ses-1")

	if _, err := r.h.m.Start(context.Background(), "ses-1", StartOptions{Retain: true}); err == nil {
		t.Fatal("Start(retain) with no retain period: want an error")
	}
	if got := r.h.api.deleted(); len(got) != 0 {
		t.Fatalf("deleted = %v, want the live ephemeral box untouched", got)
	}
	if _, err := r.h.m.Browser("ses-1"); err != nil {
		t.Fatalf("Browser after the refused retain = %v, want the ephemeral box still running", err)
	}
}

func TestIdleRetainedBoxIsPausedNotDeleted(t *testing.T) {
	r := newRetainedRig(t)
	r.setAssign("ses-1", "work")
	r.browser.cookies = []json.RawMessage{testCookie("sid", "v1")}
	st := startRetained(t, r.h.m, "ses-1")

	r.h.clock.Advance(10 * time.Minute)
	r.h.m.Tick(context.Background())

	if got := r.h.api.deleted(); len(got) != 0 {
		t.Fatalf("deleted = %v, want a retained box paused, never deleted", got)
	}
	if got := r.h.api.pauseCalls(); !slices.Equal(got, []string{st.BoxID}) {
		t.Fatalf("pause calls = %v, want [%s]", got, st.BoxID)
	}
	if s := r.h.api.boxStatus(st.BoxID); s != upstashbox.StatusPaused {
		t.Fatalf("box status = %q, want paused", s)
	}
	if i, j := r.ev.index("read-cookies"), r.ev.index("pause-start"); i < 0 || j <= i {
		t.Fatalf("event order %v, want the final save before the pause", r.ev.all())
	}
	if h := r.holder("work"); h != "" {
		t.Fatalf("login state held by %q after the pause, want released", h)
	}
	if row := retainedRowOf(t, r, "work"); !row.Paused {
		t.Fatalf("row = %+v, want paused", row)
	}
	if got := cookieNames(t, r.savedState("work").Cookies); !slices.Equal(got, []string{"sid"}) {
		t.Fatalf("saved cookies = %v, want [sid] from the final save", got)
	}
	// ErrPaused is not cleared by Browser: it keeps answering until Start or Terminate.
	for range 2 {
		if _, err := r.h.m.Browser("ses-1"); !errors.Is(err, ErrPaused) {
			t.Fatalf("Browser after idle = %v, want ErrPaused", err)
		}
	}
}

func TestStartResumesPausedBoxInAnotherSession(t *testing.T) {
	r := newRetainedRig(t)
	r.setAssign("ses-1", "work")
	r.browser.cookies = []json.RawMessage{testCookie("sid", "v1")}
	first := startRetained(t, r.h.m, "ses-1")
	r.h.clock.Advance(10 * time.Minute)
	r.h.m.Tick(context.Background())

	resumedAt := r.h.clock.Now()
	r.setAssign("ses-2", "work")
	r.browser.gotCookies = nil
	st, err := r.h.m.Start(context.Background(), "ses-2", StartOptions{Retain: true})
	if err != nil {
		t.Fatalf("Start(ses-2, retain): %v", err)
	}
	if !st.Resumed || st.Reused || st.BoxID != first.BoxID || st.Kind != KindRetained {
		t.Fatalf("Status = %+v, want the paused box %s resumed", st, first.BoxID)
	}
	if n := r.h.api.createCount(); n != 1 {
		t.Fatalf("creates = %d, want the resume to create nothing", n)
	}
	if n := r.h.api.resumeCount(); n != 1 {
		t.Fatalf("resumes = %d, want the CDP connect to resume the paused box once", n)
	}
	if got := cookieNames(t, r.browser.gotCookies); !slices.Equal(got, []string{"sid"}) {
		t.Fatalf("restored cookies = %v, want [sid] from the login state", got)
	}
	if want := resumedAt.Add(retainPeriod); !st.ExpiresAt.Equal(want) {
		t.Fatalf("Status.ExpiresAt = %v, want now plus retain %v", st.ExpiresAt, want)
	}
	row := retainedRowOf(t, r, "work")
	if row.Paused || row.SessionID != "ses-2" || row.Lease.PID != os.Getpid() {
		t.Fatalf("row = %+v, want unpaused, held by ses-2, owned by this process", row)
	}
	if got := r.holder("work"); got != "ses-2" {
		t.Fatalf("login state held by %q, want ses-2", got)
	}
}

func TestResumeOfBoxDeletedOutsideOcodeCreatesNewOne(t *testing.T) {
	r := newRetainedRig(t)
	r.setAssign("ses-1", "work")
	first := startRetained(t, r.h.m, "ses-1")
	r.h.clock.Advance(10 * time.Minute)
	r.h.m.Tick(context.Background())
	if err := r.h.api.Delete(context.Background(), first.BoxID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	r.setAssign("ses-2", "work")
	st := startRetained(t, r.h.m, "ses-2")
	if st.BoxID == first.BoxID || st.Resumed {
		t.Fatalf("Status = %+v, want a new box in place of %s", st, first.BoxID)
	}
	if n := r.h.api.createCount(); n != 2 {
		t.Fatalf("creates = %d, want a second box", n)
	}
	if got := r.h.journalIDs(); !slices.Equal(got, []string{st.BoxID}) {
		t.Fatalf("journal = %v, want only the new box %s", got, st.BoxID)
	}
	if row := retainedRowOf(t, r, "work"); row.SessionID != "ses-2" {
		t.Fatalf("row = %+v, want it held by ses-2", row)
	}
}

func TestTerminatePausesRetainedAndDeletesEphemeral(t *testing.T) {
	r := newRetainedRig(t)
	r.setAssign("ses-r", "work")
	ret := startRetained(t, r.h.m, "ses-r")
	eph := mustStart(t, r.h.m, "ses-e")

	if err := r.h.m.Terminate(context.Background(), "ses-r"); err != nil {
		t.Fatalf("Terminate(retained): %v", err)
	}
	if err := r.h.m.Terminate(context.Background(), "ses-e"); err != nil {
		t.Fatalf("Terminate(ephemeral): %v", err)
	}
	if got := r.h.api.deleted(); !slices.Equal(got, []string{eph.BoxID}) {
		t.Fatalf("deleted = %v, want only the ephemeral box %s", got, eph.BoxID)
	}
	if s := r.h.api.boxStatus(ret.BoxID); s != upstashbox.StatusPaused {
		t.Fatalf("retained box status = %q, want paused", s)
	}
	if !retainedRowOf(t, r, "work").Paused {
		t.Fatal("retained row is not paused after Terminate")
	}
	if _, err := r.h.m.Browser("ses-r"); !errors.Is(err, ErrNotStarted) {
		t.Fatalf("Browser after Terminate = %v, want ErrNotStarted (Terminate sets no paused marker)", err)
	}
}

func TestShutdownPausesRetainedAndDeletesEphemeral(t *testing.T) {
	r := newRetainedRig(t)
	r.setAssign("ses-r", "work")
	ret := startRetained(t, r.h.m, "ses-r")
	eph := mustStart(t, r.h.m, "ses-e")

	r.h.m.Shutdown(context.Background())

	if got := r.h.api.deleted(); !slices.Equal(got, []string{eph.BoxID}) {
		t.Fatalf("deleted = %v, want only the ephemeral box %s", got, eph.BoxID)
	}
	if got := r.h.api.pauseCalls(); !slices.Equal(got, []string{ret.BoxID}) {
		t.Fatalf("pause calls = %v, want [%s]", got, ret.BoxID)
	}
	if !retainedRowOf(t, r, "work").Paused {
		t.Fatal("retained row is not paused after Shutdown")
	}
	if _, err := r.h.m.Start(context.Background(), "ses-x", StartOptions{}); !errors.Is(err, errShutDown) {
		t.Fatalf("Start after Shutdown = %v, want errShutDown", err)
	}
}

func TestRetainOverEphemeralDeletesItWithFinalSaveFirst(t *testing.T) {
	r := newRetainedRig(t)
	r.setAssign("ses-1", "work")
	eph := mustStart(t, r.h.m, "ses-1")
	ret := startRetained(t, r.h.m, "ses-1")

	if ret.BoxID == eph.BoxID || ret.Kind != KindRetained {
		t.Fatalf("retained Status = %+v, want a new retained box beside %s", ret, eph.BoxID)
	}
	if got := r.h.api.deleted(); !slices.Equal(got, []string{eph.BoxID}) {
		t.Fatalf("deleted = %v, want the ephemeral box %s", got, eph.BoxID)
	}
	save, del := r.ev.index("read-cookies"), r.ev.index("delete-start")
	if save < 0 || del <= save {
		t.Fatalf("event order %v, want the final save before the ephemeral delete", r.ev.all())
	}
	if r.ev.count("create") != 2 {
		t.Fatalf("event order %v, want two creates", r.ev.all())
	}
	if got := r.holder("work"); got != "ses-1" {
		t.Fatalf("login state held by %q, want ses-1 again", got)
	}
	if got := r.h.journalIDs(); !slices.Equal(got, []string{ret.BoxID}) {
		t.Fatalf("journal = %v, want only the retained box", got)
	}
}

func TestLiveRetainedBoxIsNotReapedAtHardCap(t *testing.T) {
	r := newRetainedRig(t)
	r.setAssign("ses-1", "work")
	st := startRetained(t, r.h.m, "ses-1")

	r.h.setBusy("ses-1", true)
	r.h.clock.Advance(2 * time.Hour) // well past the one-hour hard cap
	r.h.m.Tick(context.Background())

	if _, err := r.h.m.Browser("ses-1"); err != nil {
		t.Fatalf("Browser past the hard cap = %v, want the retained box still live", err)
	}
	if got := r.h.api.deleted(); len(got) != 0 {
		t.Fatalf("deleted = %v, want none", got)
	}
	if got := r.h.api.pauseCalls(); len(got) != 0 {
		t.Fatalf("pause calls = %v, want none while busy", got)
	}
	statuses := r.h.m.Statuses()
	if len(statuses) != 1 || statuses[0].BoxID != st.BoxID || statuses[0].Kind != KindRetained {
		t.Fatalf("Statuses = %+v, want the live retained box", statuses)
	}
}

func TestTickDeletesPausedRetainedRowPastDeleteAt(t *testing.T) {
	r := newRetainedRig(t)
	r.setAssign("ses-1", "work")
	st := startRetained(t, r.h.m, "ses-1")
	r.h.clock.Advance(10 * time.Minute)
	r.h.m.Tick(context.Background())
	if got := r.h.api.deleted(); len(got) != 0 {
		t.Fatalf("deleted = %v before the delete-at", got)
	}

	r.h.clock.Advance(retainPeriod + time.Minute)
	r.h.m.Tick(context.Background())

	if got := r.h.api.deleted(); !slices.Equal(got, []string{st.BoxID}) {
		t.Fatalf("deleted = %v, want [%s] once past its delete-at", got, st.BoxID)
	}
	if got := r.h.journalIDs(); len(got) != 0 {
		t.Fatalf("journal = %v, want the row removed with the box", got)
	}
}

func TestTickPausesStrayOfDeadProcessAndSparesLivePeer(t *testing.T) {
	r := newRetainedRig(t)
	now := r.h.clock.Now()
	ctx := context.Background()
	stray, err := r.h.api.Create(ctx, upstashbox.CreateOptions{Name: "stray", Browser: true, Labels: []string{"ocode-test00aa"}})
	if err != nil {
		t.Fatalf("Create stray: %v", err)
	}
	peer, err := r.h.api.Create(ctx, upstashbox.CreateOptions{Name: "peer", Browser: true, Labels: []string{"ocode-test00aa"}})
	if err != nil {
		t.Fatalf("Create peer: %v", err)
	}
	deadLease := Lease{PID: 4242, ProcessStart: now.Add(-3 * time.Hour), Heartbeat: now.Add(-time.Hour)}
	peerLease := Lease{PID: 4343, ProcessStart: now.Add(-time.Hour), Heartbeat: now.Add(-10 * time.Second)}
	seed := []Entry{
		{BoxID: stray.ID, AccountID: "acc-1", SessionID: "old", Kind: KindRetained, LoginState: "a", CreatedAt: now, ExpiresAt: now.Add(retainPeriod), LastUsed: now.Add(-time.Hour), Lease: deadLease},
		{BoxID: peer.ID, AccountID: "acc-1", SessionID: "other", Kind: KindRetained, LoginState: "b", CreatedAt: now, ExpiresAt: now.Add(retainPeriod), LastUsed: now.Add(-time.Hour), Lease: peerLease},
	}
	for _, e := range seed {
		if err := r.h.journal.Add(e); err != nil {
			t.Fatalf("Add: %v", err)
		}
	}
	r.h.setAlive(4343)

	r.h.m.Tick(ctx)

	if got := r.h.api.pauseCalls(); !slices.Equal(got, []string{stray.ID}) {
		t.Fatalf("pause calls = %v, want only the stray %s", got, stray.ID)
	}
	if row, _, _ := r.h.journal.Retained("acc-1", "a"); !row.Paused {
		t.Fatalf("stray row = %+v, want paused", row)
	}
	row, _, _ := r.h.journal.Retained("acc-1", "b")
	if row.Paused || row.SessionID != "other" || r.h.api.boxStatus(peer.ID) == upstashbox.StatusPaused {
		t.Fatalf("live peer row = %+v, status %q; want it untouched", row, r.h.api.boxStatus(peer.ID))
	}
}

func TestFailedPauseIsRetriedOnNextTick(t *testing.T) {
	r := newRetainedRig(t)
	r.setAssign("ses-1", "work")
	st := startRetained(t, r.h.m, "ses-1")

	r.h.api.setPauseErr(errors.New("network down"))
	if err := r.h.m.Terminate(context.Background(), "ses-1"); err == nil {
		t.Fatal("Terminate with a failing pause: want the error returned")
	}
	if h := r.holder("work"); h != "" {
		t.Fatalf("login state held by %q after a failed pause, want released", h)
	}
	row := retainedRowOf(t, r, "work")
	if row.Paused || row.Lease.PID != os.Getpid() {
		t.Fatalf("row = %+v, want unpaused and still this process's while the pause is pending", row)
	}

	r.h.api.setPauseErr(nil)
	r.h.m.Tick(context.Background())

	if got := r.h.api.pauseCalls(); !slices.Equal(got, []string{st.BoxID, st.BoxID}) {
		t.Fatalf("pause calls = %v, want the failed pause then the retry", got)
	}
	if !retainedRowOf(t, r, "work").Paused {
		t.Fatal("row not paused after the retry")
	}
	if got := r.h.api.deleted(); len(got) != 0 {
		t.Fatalf("deleted = %v, want the retained box never deleted by the retry", got)
	}
}

func TestJournalRejectsSecondRetainedRowForSameLoginState(t *testing.T) {
	j := NewJournal(filepath.Join(t.TempDir(), "upstash-boxes.json"))
	retained := func(boxID, account, login string) Entry {
		return Entry{BoxID: boxID, AccountID: account, SessionID: "s", Kind: KindRetained, LoginState: login, CreatedAt: jt0, ExpiresAt: jt0.Add(time.Hour)}
	}
	if err := j.Add(retained("box-1", "acc-1", "work")); err != nil {
		t.Fatalf("first retained row: %v", err)
	}
	if err := j.Add(retained("box-2", "acc-1", "work")); err == nil {
		t.Fatal("second retained row for the same account and login state: want an error")
	}
	for _, e := range []Entry{
		retained("box-3", "acc-1", "other"),
		retained("box-4", "acc-2", "work"),
	} {
		if err := j.Add(e); err != nil {
			t.Fatalf("Add(%s): %v", e.BoxID, err)
		}
	}
	if err := j.Add(retained("box-5", "acc-1", "")); err == nil {
		t.Fatal("retained row with no login state: want an error")
	}
	entries, err := j.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if got := ids(entries); !slices.Equal(got, []string{"box-1", "box-3", "box-4"}) {
		t.Fatalf("journal = %v, want box-1, box-3 and box-4", got)
	}
}

func TestJournalRetainedDueClassifiesRows(t *testing.T) {
	j := NewJournal(filepath.Join(t.TempDir(), "upstash-boxes.json"))
	now := jt0
	owner := Lease{PID: 1, ProcessStart: now.Add(-time.Hour), Heartbeat: now}
	dead := Lease{PID: 4242, ProcessStart: now.Add(-3 * time.Hour), Heartbeat: now.Add(-time.Hour)}
	peer := Lease{PID: 4343, ProcessStart: now.Add(-time.Hour), Heartbeat: now.Add(-2 * time.Hour)}
	alive := func(l Lease) bool { return l.PID == 4343 }
	rows := []Entry{
		{BoxID: "paused-past", AccountID: "acc-1", Kind: KindRetained, LoginState: "a", Paused: true, ExpiresAt: now.Add(-time.Minute), Lease: dead},
		{BoxID: "paused-future", AccountID: "acc-1", Kind: KindRetained, LoginState: "b", Paused: true, ExpiresAt: now.Add(time.Hour), Lease: dead},
		{BoxID: "dead-past", AccountID: "acc-1", Kind: KindRetained, LoginState: "c", ExpiresAt: now.Add(-time.Minute), Lease: dead},
		{BoxID: "dead-future", AccountID: "acc-1", Kind: KindRetained, LoginState: "d", ExpiresAt: now.Add(time.Hour), Lease: dead},
		{BoxID: "peer-live", AccountID: "acc-1", Kind: KindRetained, LoginState: "e", ExpiresAt: now.Add(-time.Minute), Lease: peer},
		{BoxID: "own", AccountID: "acc-1", Kind: KindRetained, LoginState: "f", ExpiresAt: now.Add(-time.Minute), Lease: owner},
	}
	for _, e := range rows {
		if err := j.Add(e); err != nil {
			t.Fatalf("Add(%s): %v", e.BoxID, err)
		}
	}

	expired, strays, err := j.RetainedDue(now, owner, leaseTTL, alive)
	if err != nil {
		t.Fatalf("RetainedDue: %v", err)
	}
	if got := ids(expired); !slices.Equal(got, []string{"dead-past", "paused-past"}) {
		t.Fatalf("expired = %v, want dead-past and paused-past", got)
	}
	if got := ids(strays); !slices.Equal(got, []string{"dead-future"}) {
		t.Fatalf("strays = %v, want dead-future", got)
	}
}

func TestJournalResumeMovesRowToSessionAndLease(t *testing.T) {
	j := NewJournal(filepath.Join(t.TempDir(), "upstash-boxes.json"))
	old := Lease{PID: 4242, ProcessStart: jt0.Add(-time.Hour), Heartbeat: jt0.Add(-time.Hour)}
	if err := j.Add(Entry{BoxID: "box-1", AccountID: "acc-1", SessionID: "ses-1", Kind: KindRetained, LoginState: "work", CreatedAt: jt0, ExpiresAt: jt0, Lease: old, Paused: true}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	owner := Lease{PID: 7, ProcessStart: jt0, Heartbeat: jt0}
	expires := jt0.Add(retainPeriod)
	if err := j.Resume("box-1", "ses-2", owner, jt0, expires); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	row, found, err := j.Retained("acc-1", "work")
	if err != nil || !found {
		t.Fatalf("Retained = found %v, err %v", found, err)
	}
	if row.Paused || row.SessionID != "ses-2" || row.Lease.PID != 7 || !row.ExpiresAt.Equal(expires) {
		t.Fatalf("row = %+v, want unpaused, ses-2, lease 7, expiry %v", row, expires)
	}
	if err := j.SetPaused("missing", jt0, expires); err == nil {
		t.Fatal("SetPaused of a missing row: want an error")
	}
}

func TestForgetBoxDropsRowAndLiveSideWithoutAPICall(t *testing.T) {
	r := newRetainedRig(t)
	r.setAssign("ses-1", "work")
	st := startRetained(t, r.h.m, "ses-1")

	if err := r.h.m.ForgetBox(st.BoxID); err != nil {
		t.Fatalf("ForgetBox: %v", err)
	}
	if got := r.h.api.deleted(); len(got) != 0 {
		t.Fatalf("deleted = %v, want no API call", got)
	}
	if got := r.h.api.pauseCalls(); len(got) != 0 {
		t.Fatalf("pause calls = %v, want no API call", got)
	}
	if i := r.ev.index("read-cookies"); i >= 0 {
		t.Fatalf("event order %v, want no final save of a box deleted outside ocode", r.ev.all())
	}
	if _, err := r.h.m.Browser("ses-1"); !errors.Is(err, ErrNotStarted) {
		t.Fatalf("Browser after ForgetBox = %v, want ErrNotStarted", err)
	}
	if h := r.holder("work"); h != "" {
		t.Fatalf("login state held by %q, want released", h)
	}
	if got := r.h.journalIDs(); len(got) != 0 {
		t.Fatalf("journal = %v, want the row dropped", got)
	}
}

// TestPauseBoxDetachesBeforeItPausesAndKeepsRetainPeriod: a pause of a box a live
// session holds runs the final save and the browser close before the API pause, and
// no connect is made after it. The session is then paused, and the row carries the
// account's retain period from the pause.
func TestPauseBoxDetachesBeforeItPausesAndKeepsRetainPeriod(t *testing.T) {
	r := newRetainedRig(t)
	r.setAssign("ses-1", "work")
	ctl := steerAPI(r)
	ctl.setAfterPause(func() { r.ev.add("pause") })
	st := startRetained(t, r.h.m, "ses-1")
	r.h.api.onCDP = func(string) { r.ev.add("cdp") }

	r.h.clock.Advance(time.Hour)
	handled, err := r.h.m.PauseBox(context.Background(), st.BoxID)
	if err != nil || !handled {
		t.Fatalf("PauseBox = handled %v, err %v; want handled and no error", handled, err)
	}
	pause := r.ev.index("pause")
	if pause < 0 {
		t.Fatalf("event order %v: the box was never paused", r.ev.all())
	}
	if save := r.ev.index("read-cookies"); save < 0 || save > pause {
		t.Fatalf("event order %v: want the final save before the pause", r.ev.all())
	}
	if cl := r.ev.index("close-browser"); cl < 0 || cl > pause {
		t.Fatalf("event order %v: want the browser closed before the pause", r.ev.all())
	}
	for i, e := range r.ev.all() {
		if e == "cdp" && i > pause {
			t.Fatalf("event order %v: a connect after the pause would resume the box", r.ev.all())
		}
	}
	if _, err := r.h.m.Browser("ses-1"); !errors.Is(err, ErrPaused) {
		t.Fatalf("Browser after PauseBox = %v, want ErrPaused", err)
	}
	if h := r.holder("work"); h != "" {
		t.Fatalf("login state held by %q, want released", h)
	}
	row := retainedRowOf(t, r, "work")
	if !row.Paused || !row.ExpiresAt.Equal(r.h.clock.Now().Add(retainPeriod)) {
		t.Fatalf("row = %+v, want paused with delete-at now plus the retain period", row)
	}
	if got := r.h.api.deleted(); len(got) != 0 {
		t.Fatalf("deleted = %v, want none", got)
	}
}

// TestPauseBoxFailedPauseIsQueuedForTick: a pause the API refuses leaves the box and
// its row running, with the box queued. The next tick pauses it and marks the row.
func TestPauseBoxFailedPauseIsQueuedForTick(t *testing.T) {
	r := newRetainedRig(t)
	r.setAssign("ses-1", "work")
	st := startRetained(t, r.h.m, "ses-1")

	r.h.api.setPauseErr(errors.New("transient"))
	handled, err := r.h.m.PauseBox(context.Background(), st.BoxID)
	if !handled || err == nil {
		t.Fatalf("PauseBox = handled %v, err %v; want handled with the pause error", handled, err)
	}
	if row := retainedRowOf(t, r, "work"); row.Paused {
		t.Fatal("the row is paused although the pause failed")
	}
	r.h.m.mu.Lock()
	_, queued := r.h.m.unpaused[st.BoxID]
	r.h.m.mu.Unlock()
	if !queued {
		t.Fatal("the failed pause is not queued for Tick")
	}

	r.h.api.setPauseErr(nil)
	r.h.m.Tick(context.Background())
	if row := retainedRowOf(t, r, "work"); !row.Paused {
		t.Fatalf("row = %+v after the retry, want paused", row)
	}
}

// TestPauseBoxOfUnheldRowPausesThroughAPIKeepingItsTimes: a journaled retained row
// no live session holds is paused through the API, and its last-use and delete-at
// are kept.
func TestPauseBoxOfUnheldRowPausesThroughAPIKeepingItsTimes(t *testing.T) {
	r := newRetainedRig(t)
	r.setAssign("ses-1", "work")
	st := startRetained(t, r.h.m, "ses-1")
	before := retainedRowOf(t, r, "work")
	if err := r.h.m.forgetLive(context.Background(), st.BoxID); err != nil {
		t.Fatalf("forgetLive: %v", err)
	}

	r.h.clock.Advance(time.Hour)
	handled, err := r.h.m.PauseBox(context.Background(), st.BoxID)
	if err != nil || !handled {
		t.Fatalf("PauseBox = handled %v, err %v; want handled and no error", handled, err)
	}
	if got := r.h.api.pauseCalls(); !slices.Equal(got, []string{st.BoxID}) {
		t.Fatalf("pause calls = %v, want one pause of %s", got, st.BoxID)
	}
	row := retainedRowOf(t, r, "work")
	if !row.Paused || !row.LastUsed.Equal(before.LastUsed) || !row.ExpiresAt.Equal(before.ExpiresAt) {
		t.Fatalf("row = %+v, want paused with last use %v and delete-at %v kept", row, before.LastUsed, before.ExpiresAt)
	}
}

// TestPauseBoxRefusesRowOfLivePeer: a retained row another live process runs is
// refused, and no pause is sent for it.
func TestPauseBoxRefusesRowOfLivePeer(t *testing.T) {
	r := newRetainedRig(t)
	now := r.h.clock.Now()
	r.h.setAlive(4343)
	peer := Lease{PID: 4343, ProcessStart: now.Add(-time.Hour), Heartbeat: now}
	if err := r.h.journal.Add(Entry{BoxID: "box-peer", AccountID: "acc-1", SessionID: "other", Kind: KindRetained, LoginState: "work", CreatedAt: now, ExpiresAt: now.Add(retainPeriod), LastUsed: now, Lease: peer}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	handled, err := r.h.m.PauseBox(context.Background(), "box-peer")
	if !handled || !errors.Is(err, ErrRetainedBusy) {
		t.Fatalf("PauseBox = handled %v, err %v; want handled and ErrRetainedBusy", handled, err)
	}
	if got := r.h.api.pauseCalls(); len(got) != 0 {
		t.Fatalf("pause calls = %v, want none for a peer's row", got)
	}
}

// TestPauseBoxOfUnjournaledBoxIsNotHandled: a box with no retained row is left to the
// caller, and nothing is touched.
func TestPauseBoxOfUnjournaledBoxIsNotHandled(t *testing.T) {
	r := newRetainedRig(t)
	handled, err := r.h.m.PauseBox(context.Background(), "box-foreign")
	if handled || err != nil {
		t.Fatalf("PauseBox = handled %v, err %v; want not handled and no error", handled, err)
	}
	if got := r.h.api.pauseCalls(); len(got) != 0 {
		t.Fatalf("pause calls = %v, want none", got)
	}
}

// TestFailedConnectOnResumeParksBoxAndRetriesPause: a resume whose CDP connect
// fails may still have resumed the box server-side, so the box is parked the way a
// failed start parks it: a pause is sent, and the row is left running under this
// process. When that pause fails too, the box stays queued, and the next Tick
// pauses it.
func TestFailedConnectOnResumeParksBoxAndRetriesPause(t *testing.T) {
	r := newRetainedRig(t)
	r.setAssign("ses-1", "work")
	first := startRetained(t, r.h.m, "ses-1")
	r.h.clock.Advance(10 * time.Minute)
	r.h.m.Tick(context.Background())

	r.h.api.cdpErr = errors.New("connect refused")
	r.h.api.setPauseErr(errors.New("network down"))
	r.setAssign("ses-2", "work")
	if _, err := r.h.m.Start(context.Background(), "ses-2", StartOptions{Retain: true}); err == nil {
		t.Fatal("Start with a refused connect: want an error")
	}
	if got := r.h.api.pauseCalls(); !slices.Equal(got, []string{first.BoxID, first.BoxID}) {
		t.Fatalf("pause calls = %v, want the idle pause then the park of the resumed box", got)
	}
	if row := retainedRowOf(t, r, "work"); row.Paused || row.Lease.PID != os.Getpid() {
		t.Fatalf("row = %+v, want unpaused and this process's while the park's pause is pending", row)
	}
	if h := r.holder("work"); h != "" {
		t.Fatalf("login state held by %q after the failed resume, want released", h)
	}

	r.h.api.setPauseErr(nil)
	r.h.m.Tick(context.Background())
	if got := r.h.api.pauseCalls(); !slices.Equal(got, []string{first.BoxID, first.BoxID, first.BoxID}) {
		t.Fatalf("pause calls = %v, want the Tick to retry the failed park", got)
	}
	if row := retainedRowOf(t, r, "work"); !row.Paused {
		t.Fatalf("row = %+v, want paused after the retry", row)
	}

	r.h.api.cdpErr = nil
	st := startRetained(t, r.h.m, "ses-2")
	if !st.Resumed || st.BoxID != first.BoxID {
		t.Fatalf("Start after the connect recovered = %+v, want the same box resumed", st)
	}
}

func TestFailedRestoreAfterResumeParksBox(t *testing.T) {
	r := newRetainedRig(t)
	r.setAssign("ses-1", "work")
	r.browser.cookies = []json.RawMessage{testCookie("sid", "v1")}
	first := startRetained(t, r.h.m, "ses-1")
	r.h.clock.Advance(10 * time.Minute)
	r.h.m.Tick(context.Background())

	r.browser.setCookiesErr = errors.New("cookie restore refused")
	r.setAssign("ses-2", "work")
	if _, err := r.h.m.Start(context.Background(), "ses-2", StartOptions{Retain: true}); err == nil {
		t.Fatal("Start with a failing restore: want an error")
	}
	if got := r.h.api.pauseCalls(); !slices.Equal(got, []string{first.BoxID, first.BoxID}) {
		t.Fatalf("pause calls = %v, want the idle pause then the park after the failed restore", got)
	}
	if s := r.h.api.boxStatus(first.BoxID); s != upstashbox.StatusPaused {
		t.Fatalf("box status = %q, want paused", s)
	}
	if row := retainedRowOf(t, r, "work"); !row.Paused {
		t.Fatalf("row = %+v, want paused", row)
	}
	if h := r.holder("work"); h != "" {
		t.Fatalf("login state held by %q, want released", h)
	}

	r.browser.setCookiesErr = nil
	st := startRetained(t, r.h.m, "ses-2")
	if !st.Resumed || st.BoxID != first.BoxID {
		t.Fatalf("Start after the restore recovered = %+v, want the same box resumed", st)
	}
}

func TestStartRefusesRowOfLivePeerProcess(t *testing.T) {
	r := newRetainedRig(t)
	now := r.h.clock.Now()
	peerBox, err := r.h.api.Create(context.Background(), upstashbox.CreateOptions{Name: "peer", Browser: true, Labels: []string{"ocode-test00aa"}})
	if err != nil {
		t.Fatalf("Create peer box: %v", err)
	}
	peer := Lease{PID: 4343, ProcessStart: now.Add(-time.Hour), Heartbeat: now.Add(-10 * time.Second)}
	if err := r.h.journal.Add(Entry{BoxID: peerBox.ID, AccountID: "acc-1", SessionID: "other", Kind: KindRetained, LoginState: "work", CreatedAt: now, ExpiresAt: now.Add(retainPeriod), LastUsed: now, Lease: peer}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	r.h.setAlive(4343)
	r.setAssign("ses-1", "work")

	_, err = r.h.m.Start(context.Background(), "ses-1", StartOptions{Retain: true})
	if err == nil || !strings.Contains(err.Error(), "in use by another ocode process") {
		t.Fatalf("Start = %v, want the in-use refusal", err)
	}
	if n := r.h.api.createCount(); n != 1 {
		t.Fatalf("creates = %d, want only the peer's box", n)
	}
	if got := r.h.api.resumeCount(); got != 0 {
		t.Fatalf("resumes = %d, want the peer's box untouched", got)
	}
	if row := retainedRowOf(t, r, "work"); row.SessionID != "other" || row.Paused {
		t.Fatalf("row = %+v, want the peer's row unchanged", row)
	}
}

// TestRetainedStartReplacesBoxWhenAssignmentChanged: a retained box carries the
// login state it was created with, so reassigning the project must replace it
// rather than hand the caller the previous identity's cookies.
func TestRetainedStartReplacesBoxWhenAssignmentChanged(t *testing.T) {
	r := newRetainedRig(t)
	r.setAssign("ses-1", "work")
	first := startRetained(t, r.h.m, "ses-1")

	r.setAssign("ses-1", "personal")
	second := startRetained(t, r.h.m, "ses-1")

	if second.Reused || second.BoxID == first.BoxID {
		t.Fatalf("Start after reassignment = %+v, want a new box, not reuse of %s", second, first.BoxID)
	}
	if second.LoginState != "personal" {
		t.Fatalf("LoginState = %q, want personal", second.LoginState)
	}
	if n := r.h.api.createCount(); n != 2 {
		t.Fatalf("creates = %d, want 2", n)
	}
	if got := r.h.api.pauseCalls(); !slices.Contains(got, first.BoxID) {
		t.Fatalf("pause calls = %v, want the old retained box %s paused", got, first.BoxID)
	}
	if h := r.holder("work"); h != "" {
		t.Fatalf("login state work still held by %q", h)
	}
	if row := retainedRowOf(t, r, "work"); !row.Paused {
		t.Fatalf("row = %+v, want the old retained row paused", row)
	}
}

// TestSlowCreateDoesNotBackdateTheNewBox: lastUsed and the checkpoint clock must
// be read when the box is published, not when Start was entered. A create that
// outlives the idle timeout must not hand the caller a box the next Tick reaps.
func TestSlowCreateDoesNotBackdateTheNewBox(t *testing.T) {
	r := newLoginRig(t)
	r.onCreate = func() { r.h.clock.Advance(2 * r.h.account.Idle) }
	st := mustStart(t, r.h.m, "ses-1")
	if st.Reused {
		t.Fatalf("Start = %+v, want a created box", st)
	}

	r.h.m.Tick(context.Background())

	if _, err := r.h.m.Browser("ses-1"); err != nil {
		t.Fatalf("the box was reaped right after a create slower than the idle timeout: %v", err)
	}
}
