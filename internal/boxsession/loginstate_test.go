package boxsession

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func newTestLoginStore(t *testing.T) (*LoginStateStore, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "login-states")
	s := NewLoginStateStore(dir)
	s.alive = func(int) bool { return true }
	return s, dir
}

func testLoginState(name string) LoginState {
	return LoginState{
		Name:    name,
		SavedAt: time.Date(2026, 11, 22, 9, 30, 0, 0, time.UTC),
		Cookies: []json.RawMessage{
			json.RawMessage(`{"name":"sid","value":"s3cret","domain":"example.com"}`),
		},
		LocalStorage: map[string]map[string]string{
			"https://example.com": {"token": "t0ken"},
		},
	}
}

func TestLoginStateRoundTrip(t *testing.T) {
	s, _ := newTestLoginStore(t)
	want := testLoginState("work")
	if err := s.Save(want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, ok, err := s.Load("work")
	if err != nil || !ok {
		t.Fatalf("Load = ok %v, err %v", ok, err)
	}
	if got.Name != want.Name || !got.SavedAt.Equal(want.SavedAt) {
		t.Fatalf("Load header = %+v, want %+v", got, want)
	}
	// The file is indented, so the raw bytes differ; the decoded cookie must not.
	if len(got.Cookies) != 1 {
		t.Fatalf("cookies round trip = %d, want 1", len(got.Cookies))
	}
	var gotCookie, wantCookie map[string]string
	if err := json.Unmarshal(got.Cookies[0], &gotCookie); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(want.Cookies[0], &wantCookie); err != nil {
		t.Fatal(err)
	}
	if gotCookie["value"] != wantCookie["value"] || gotCookie["domain"] != wantCookie["domain"] {
		t.Fatalf("cookie round trip = %v, want %v", gotCookie, wantCookie)
	}
	if got.LocalStorage["https://example.com"]["token"] != "t0ken" {
		t.Fatalf("local storage round trip = %v", got.LocalStorage)
	}
}

func TestLoginStateMissingIsNotAnError(t *testing.T) {
	s, _ := newTestLoginStore(t)
	_, ok, err := s.Load("nothing")
	if err != nil || ok {
		t.Fatalf("Load missing = ok %v, err %v; want false, nil", ok, err)
	}
	if err := s.Delete("nothing"); err != nil {
		t.Fatalf("Delete missing: %v", err)
	}
}

func TestLoginStateSaveLeavesNoTempFile(t *testing.T) {
	s, dir := newTestLoginStore(t)
	if err := s.Save(testLoginState("work")); err != nil {
		t.Fatalf("Save: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("temp file left behind: %s", e.Name())
		}
	}
	if len(entries) != 1 || entries[0].Name() != "work.json" {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("dir entries = %v, want only work.json", names)
	}
}

func TestLoginStatePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits do not apply on windows")
	}
	s, dir := newTestLoginStore(t)
	if err := s.Save(testLoginState("work")); err != nil {
		t.Fatalf("Save: %v", err)
	}
	di, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := di.Mode().Perm(); got != 0o700 {
		t.Fatalf("dir perm = %o, want 700", got)
	}
	fi, err := os.Stat(filepath.Join(dir, "work.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got := fi.Mode().Perm(); got != 0o600 {
		t.Fatalf("file perm = %o, want 600", got)
	}
}

func TestLoginStateInvalidNamesRefused(t *testing.T) {
	s, dir := newTestLoginStore(t)
	bad := []string{"", "none", "../escape", "a/b", "Work", "x.lock", "-x", strings.Repeat("a", 41)}
	for _, name := range bad {
		if _, _, err := s.Load(name); err == nil {
			t.Errorf("Load(%q) accepted an invalid name", name)
		}
		if err := s.Save(LoginState{Name: name}); err == nil {
			t.Errorf("Save(%q) accepted an invalid name", name)
		}
		if err := s.Delete(name); err == nil {
			t.Errorf("Delete(%q) accepted an invalid name", name)
		}
		if _, err := s.Acquire(name, "sess"); err == nil {
			t.Errorf("Acquire(%q) accepted an invalid name", name)
		}
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a refused name created the directory: %v", err)
	}
}

func TestLoginStateListSortedAndCounted(t *testing.T) {
	s, _ := newTestLoginStore(t)
	if got, err := s.List(); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("List on empty store = %v, %v; want empty non-nil", got, err)
	}
	for _, name := range []string{"zeta", "alpha", "mid-1"} {
		if err := s.Save(testLoginState(name)); err != nil {
			t.Fatal(err)
		}
	}
	// A holder file and a lock file in the same directory are not states.
	if _, err := s.Acquire("mid-1", "sess-1"); err != nil {
		t.Fatal(err)
	}
	infos, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	var names []string
	for _, in := range infos {
		names = append(names, in.Name)
	}
	if strings.Join(names, ",") != "alpha,mid-1,zeta" {
		t.Fatalf("List names = %v, want sorted alpha,mid-1,zeta", names)
	}
	if infos[0].Cookies != 1 || infos[0].Origins != 1 || infos[0].Bytes <= 0 {
		t.Fatalf("counts = %+v, want 1 cookie, 1 origin, bytes > 0", infos[0])
	}
	if !infos[0].SavedAt.Equal(testLoginState("alpha").SavedAt) {
		t.Fatalf("SavedAt = %v", infos[0].SavedAt)
	}
}

func TestLoginStateAcquireConflictNamesSession(t *testing.T) {
	s, _ := newTestLoginStore(t)
	release, err := s.Acquire("work", "sess-owner")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer release()

	_, err = s.Acquire("work", "sess-other")
	var inUse ErrLoginStateInUse
	if !errors.As(err, &inUse) {
		t.Fatalf("second Acquire = %v, want ErrLoginStateInUse", err)
	}
	if inUse.SessionID != "sess-owner" || inUse.Name != "work" {
		t.Fatalf("ErrLoginStateInUse = %+v", inUse)
	}
	if !strings.Contains(err.Error(), "sess-owner") {
		t.Fatalf("error text %q does not name the holding session", err.Error())
	}

	// Delete refuses a state a live session holds.
	if err := s.Delete("work"); !errors.As(err, &inUse) {
		t.Fatalf("Delete of an in-use state = %v, want ErrLoginStateInUse", err)
	}
	sid, held, err := s.InUseBy("work")
	if err != nil || !held || sid != "sess-owner" {
		t.Fatalf("InUseBy = %q %v %v, want sess-owner held", sid, held, err)
	}
}

func TestLoginStateSameSessionReacquireSucceeds(t *testing.T) {
	s, _ := newTestLoginStore(t)
	if _, err := s.Acquire("work", "sess-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Acquire("work", "sess-1"); err != nil {
		t.Fatalf("same-session re-acquire in the same process: %v", err)
	}
}

// A holder written by another process (different pid) is in use even when the
// session id matches: the session is not the same process.
func TestLoginStateOtherProcessSameSessionIsInUse(t *testing.T) {
	s, dir := newTestLoginStore(t)
	writeHolderForTest(t, dir, "work", loginHolder{SessionID: "sess-1", PID: os.Getpid() + 1, AcquiredAt: time.Now()})
	if _, err := s.Acquire("work", "sess-1"); err == nil {
		t.Fatal("same session from another pid was granted the lock")
	}
}

func TestLoginStateDeadHolderIsReplaced(t *testing.T) {
	s, dir := newTestLoginStore(t)
	writeHolderForTest(t, dir, "work", loginHolder{SessionID: "sess-dead", PID: 4242, AcquiredAt: time.Now()})
	s.alive = func(pid int) bool { return pid != 4242 }

	sid, held, err := s.InUseBy("work")
	if err != nil || held || sid != "" {
		t.Fatalf("InUseBy with dead holder = %q %v %v, want not held", sid, held, err)
	}
	release, err := s.Acquire("work", "sess-new")
	if err != nil {
		t.Fatalf("Acquire over a dead holder: %v", err)
	}
	if err := release(); err != nil {
		t.Fatalf("release: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "work.lock.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("holder file survived its release: %v", err)
	}
}

func TestLoginStateReleaseNotOursIsNoOp(t *testing.T) {
	s, dir := newTestLoginStore(t)
	releaseA, err := s.Acquire("work", "sess-a")
	if err != nil {
		t.Fatal(err)
	}
	// The holder becomes another session's: sess-a's record is now stale.
	writeHolderForTest(t, dir, "work", loginHolder{SessionID: "sess-b", PID: os.Getpid(), AcquiredAt: time.Now()})

	if err := releaseA(); err != nil {
		t.Fatalf("release of a superseded holder: %v", err)
	}
	sid, held, err := s.InUseBy("work")
	if err != nil || !held || sid != "sess-b" {
		t.Fatalf("holder after foreign release = %q %v %v, want sess-b still held", sid, held, err)
	}
}

func TestLoginStateCorruptFileIsAnError(t *testing.T) {
	s, dir := newTestLoginStore(t)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	// The bytes contain a secret-looking value. No error may echo it back.
	secret := "s3cret-value"
	if err := os.WriteFile(filepath.Join(dir, "work.json"), []byte(`{"cookies": [`+secret+``), 0o600); err != nil {
		t.Fatal(err)
	}
	_, ok, err := s.Load("work")
	if err == nil || ok {
		t.Fatalf("Load of a corrupt state = ok %v, err %v; want an error", ok, err)
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("corrupt-file error echoes stored bytes: %v", err)
	}
	infos, err := s.List()
	if err != nil {
		t.Fatalf("List failed on one corrupt file: %v", err)
	}
	if len(infos) != 1 || infos[0].Name != "work" || !infos[0].Corrupt || infos[0].Cookies != 0 || infos[0].Origins != 0 {
		t.Fatalf("List of a corrupt state = %+v, want one Corrupt entry named work with zero counts", infos)
	}
	// A corrupt state is not reset by a delete that cannot see it either.
	if err := os.WriteFile(filepath.Join(dir, "work.json"), []byte(`{"name":"other"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Load("work"); err == nil {
		t.Fatal("a state naming another file was accepted")
	}
}

func TestLoginStateCorruptHolderIsAnError(t *testing.T) {
	s, dir := newTestLoginStore(t)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "work.lock.json"), []byte("garbage"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Acquire("work", "sess"); err == nil {
		t.Fatal("Acquire replaced a corrupt holder")
	}
	if _, _, err := s.InUseBy("work"); err == nil {
		t.Fatal("InUseBy read a corrupt holder as free")
	}

	if err := os.WriteFile(filepath.Join(dir, "work.lock.json"), []byte(`{"session_id":"","pid":0}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Acquire("work", "sess"); err == nil {
		t.Fatal("Acquire accepted a holder with no session and no pid")
	}
}

func TestLoginStateDeleteRemovesStateAndStaleHolder(t *testing.T) {
	s, dir := newTestLoginStore(t)
	if err := s.Save(testLoginState("work")); err != nil {
		t.Fatal(err)
	}
	writeHolderForTest(t, dir, "work", loginHolder{SessionID: "sess-dead", PID: 4242, AcquiredAt: time.Now()})
	s.alive = func(int) bool { return false }
	if err := s.Delete("work"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok, err := s.Load("work"); err != nil || ok {
		t.Fatalf("state after Delete = ok %v, err %v", ok, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "work.lock.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale holder survived Delete: %v", err)
	}
}

func writeHolderForTest(t *testing.T, dir, name string, h loginHolder) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(h)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+".lock.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestLoginStateTransferMovesHoldWithoutGap: the hold moves from one session to
// another with no moment when a third session could take the name.
func TestLoginStateTransferMovesHoldWithoutGap(t *testing.T) {
	s, _ := newTestLoginStore(t)
	oldRelease, err := s.Acquire("work", "sess-a")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	newRelease, err := s.Transfer("work", "sess-a", "sess-b")
	if err != nil {
		t.Fatalf("Transfer: %v", err)
	}
	if sid, held, err := s.InUseBy("work"); err != nil || !held || sid != "sess-b" {
		t.Fatalf("InUseBy after Transfer = %q %v %v, want sess-b held", sid, held, err)
	}
	var inUse ErrLoginStateInUse
	if _, err := s.Acquire("work", "sess-c"); !errors.As(err, &inUse) || inUse.SessionID != "sess-b" {
		t.Fatalf("third session Acquire = %v, want ErrLoginStateInUse naming sess-b", err)
	}

	// The old session's release no longer frees the name.
	if err := oldRelease(); err != nil {
		t.Fatalf("old release: %v", err)
	}
	if sid, held, _ := s.InUseBy("work"); !held || sid != "sess-b" {
		t.Fatalf("old release freed the name: InUseBy = %q %v, want sess-b held", sid, held)
	}

	if err := newRelease(); err != nil {
		t.Fatalf("new release: %v", err)
	}
	if _, held, _ := s.InUseBy("work"); held {
		t.Fatal("name still held after the new holder released it")
	}
}

// TestLoginStateTransferRefusesNonHolder: only the session that holds the name can hand it over.
func TestLoginStateTransferRefusesNonHolder(t *testing.T) {
	s, _ := newTestLoginStore(t)
	release, err := s.Acquire("work", "sess-a")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer release()

	var inUse ErrLoginStateInUse
	if _, err := s.Transfer("work", "sess-x", "sess-b"); !errors.As(err, &inUse) || inUse.SessionID != "sess-a" {
		t.Fatalf("Transfer by a non-holder = %v, want ErrLoginStateInUse naming sess-a", err)
	}
	if sid, held, _ := s.InUseBy("work"); !held || sid != "sess-a" {
		t.Fatalf("refused Transfer changed the holder to %q", sid)
	}
	if _, err := s.Transfer("unheld", "sess-a", "sess-b"); err == nil {
		t.Fatal("Transfer of a name with no holder succeeded")
	}
}

// TestLoginStateDeleteRemovesCorruptFileAndLeftovers: a corrupt state can still be
// deleted, and the name's holder and lock files go with it.
func TestLoginStateDeleteRemovesCorruptFileAndLeftovers(t *testing.T) {
	s, dir := newTestLoginStore(t)
	writeHolderForTest(t, dir, "work", loginHolder{SessionID: "gone", PID: 4242, AcquiredAt: time.Now()})
	s.alive = func(int) bool { return false }
	if err := os.WriteFile(filepath.Join(dir, "work.json"), []byte(`{"cookies": [s3cret`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.lockPath("work"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete("work"); err != nil {
		t.Fatalf("Delete of a corrupt state = %v, want it removed", err)
	}
	for _, p := range []string{s.statePath("work"), s.holderPath("work"), s.lockPath("work")} {
		if _, err := os.Stat(p); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("after Delete, %s still exists (stat err %v)", filepath.Base(p), err)
		}
	}
}

// TestLoginStateHolderFromEarlierProcessWithOurPidIsStale: a holder that names our
// pid but a different process start came from an earlier process whose pid was
// reused (a container restart). It is stale, and Acquire replaces it.
func TestLoginStateHolderFromEarlierProcessWithOurPidIsStale(t *testing.T) {
	s, dir := newTestLoginStore(t)
	s.processStart = time.Date(2026, 11, 22, 8, 0, 0, 0, time.UTC)
	writeHolderForTest(t, dir, "work", loginHolder{
		SessionID: "old", PID: os.Getpid(), AcquiredAt: time.Now(),
		ProcessStart: s.processStart.Add(-time.Hour),
	})
	if _, inUse, err := s.InUseBy("work"); err != nil || inUse {
		t.Fatalf("InUseBy of a stale holder = inUse %v, err %v; want not in use", inUse, err)
	}
	release, err := s.Acquire("work", "new")
	if err != nil {
		t.Fatalf("Acquire over a stale holder: %v", err)
	}
	defer release()
	h, found, err := s.readHolder("work")
	if err != nil || !found || h.SessionID != "new" || !h.ProcessStart.Equal(s.processStart) {
		t.Fatalf("holder after Acquire = %+v (found %v, err %v), want new with our process start", h, found, err)
	}
}

// TestLoginStateHolderWithOurPidAndSameStartIsLive: our own process's holder counts.
func TestLoginStateHolderWithOurPidAndSameStartIsLive(t *testing.T) {
	s, dir := newTestLoginStore(t)
	writeHolderForTest(t, dir, "work", loginHolder{
		SessionID: "mine", PID: os.Getpid(), AcquiredAt: time.Now(), ProcessStart: s.processStart,
	})
	sid, inUse, err := s.InUseBy("work")
	if err != nil || !inUse || sid != "mine" {
		t.Fatalf("InUseBy = %q, %v, %v; want mine in use", sid, inUse, err)
	}
}

// TestLoginStateHolderWithoutStartIsJudgedByPid: a holder written before the start
// field existed has no ProcessStart, so pid liveness alone decides.
func TestLoginStateHolderWithoutStartIsJudgedByPid(t *testing.T) {
	s, dir := newTestLoginStore(t)
	writeHolderForTest(t, dir, "work", loginHolder{SessionID: "legacy", PID: os.Getpid(), AcquiredAt: time.Now()})
	if sid, inUse, err := s.InUseBy("work"); err != nil || !inUse || sid != "legacy" {
		t.Fatalf("InUseBy of a start-less holder with our pid = %q, %v, %v; want in use", sid, inUse, err)
	}
}

// TestLoginStateHolderOfOtherPidIgnoresStart: a holder of another live process is
// live whatever its recorded start.
func TestLoginStateHolderOfOtherPidIgnoresStart(t *testing.T) {
	s, dir := newTestLoginStore(t)
	writeHolderForTest(t, dir, "work", loginHolder{
		SessionID: "other", PID: os.Getpid() + 1, AcquiredAt: time.Now(),
		ProcessStart: s.processStart.Add(-time.Hour),
	})
	if sid, inUse, err := s.InUseBy("work"); err != nil || !inUse || sid != "other" {
		t.Fatalf("InUseBy = %q, %v, %v; want the other live process to hold it", sid, inUse, err)
	}
}

// TestLoginStateSweepsStaleTempFiles: a crash leftover older than an hour is removed
// when its name's lock is taken, and any name's leftovers are removed by List. A
// fresh temp file is left alone.
func TestLoginStateSweepsStaleTempFiles(t *testing.T) {
	s, dir := newTestLoginStore(t)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * time.Hour)
	files := map[string]time.Time{
		".work.json.111.tmp":      old,
		".work.lock.json.222.tmp": old,
		".work.json.333.tmp":      time.Now(),
		".other.json.444.tmp":     old,
	}
	for name, mt := range files {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("partial"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, mt, mt); err != nil {
			t.Fatal(err)
		}
	}
	release, err := s.Acquire("work", "ses-1")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if err := release(); err != nil {
		t.Fatalf("release: %v", err)
	}
	exists := func(name string) bool {
		_, err := os.Stat(filepath.Join(dir, name))
		return err == nil
	}
	if exists(".work.json.111.tmp") || exists(".work.lock.json.222.tmp") {
		t.Fatal("the name lock did not remove its stale temp files")
	}
	if !exists(".work.json.333.tmp") {
		t.Fatal("the name lock removed a fresh temp file, which may be a live write")
	}
	if !exists(".other.json.444.tmp") {
		t.Fatal("the name lock removed another name's temp file")
	}
	if _, err := s.List(); err != nil {
		t.Fatalf("List: %v", err)
	}
	if exists(".other.json.444.tmp") {
		t.Fatal("List did not remove a stale temp file")
	}
	if !exists(".work.json.333.tmp") {
		t.Fatal("List removed a fresh temp file")
	}
}
