package reminders

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/u007/ocode/internal/scheduler"
)

// fixedNow is a deterministic clock anchor for tests.
var fixedNow = time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)

// --- fakes ---------------------------------------------------------------

// fakeRunner is a stand-in AgentRunner. It records what it was asked to run and
// can be told to block, so a test can hold a firing open while the main
// goroutine changes the item's status underneath it.
type fakeRunner struct {
	mu      sync.Mutex
	calls   []*Item
	out     string
	err     error
	entered chan struct{} // closed on the first call
	release chan struct{} // a call blocks on this when non-nil
	once    sync.Once
}

func newFakeRunner() *fakeRunner {
	return &fakeRunner{out: "agent said hi", entered: make(chan struct{})}
}

func (f *fakeRunner) RunReminder(_ context.Context, it *Item) (string, error) {
	f.once.Do(func() { close(f.entered) })
	f.mu.Lock()
	f.calls = append(f.calls, it)
	release := f.release
	out, err := f.out, f.err
	f.mu.Unlock()
	if release != nil {
		<-release
	}
	return out, err
}

func (f *fakeRunner) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// fakeNotifier records notifications.
type fakeNotifier struct {
	mu     sync.Mutex
	titles []string
	bodies []string
	err    error
}

func (f *fakeNotifier) NotifyReminder(title, body string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.titles = append(f.titles, title)
	f.bodies = append(f.bodies, body)
	return nil
}

func (f *fakeNotifier) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.titles)
}

// fakeOutbox records deliveries in memory.
type fakeOutbox struct {
	mu   sync.Mutex
	recs []scheduler.Delivery
}

func (f *fakeOutbox) Append(d scheduler.Delivery) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recs = append(f.recs, d)
	return nil
}

func (f *fakeOutbox) all() []scheduler.Delivery {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]scheduler.Delivery(nil), f.recs...)
}

// fakeRuns records run history in memory.
type fakeRuns struct {
	mu   sync.Mutex
	recs []scheduler.RunRecord
}

func (f *fakeRuns) Append(rec scheduler.RunRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recs = append(f.recs, rec)
	return nil
}

func (f *fakeRuns) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.recs)
}

// harness bundles a service with its fakes and a settable clock.
type harness struct {
	t   *testing.T
	svc *Service
	now time.Time

	runner   *fakeRunner
	notifier *fakeNotifier
	outbox   *fakeOutbox
	runs     *fakeRuns
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{
		t:        t,
		now:      fixedNow,
		runner:   newFakeRunner(),
		notifier: &fakeNotifier{},
		outbox:   &fakeOutbox{},
		runs:     &fakeRuns{},
	}
	h.svc = NewService(filepath.Join(t.TempDir(), "reminders.json"))
	h.svc.SetClock(func() time.Time { return h.now })
	h.svc.SetAgentRunner(h.runner)
	h.svc.SetNotifier(h.notifier)
	h.svc.SetOutbox(h.outbox)
	h.svc.SetRunHistory(h.runs)
	return h
}

// start runs the BACKGROUND loop. Items are added before Start so nothing
// fires on load.
//
// Do NOT call this in a test that also calls fireDue() to drive one firing:
// the loop sees a past-due item, computes a zero delay, and fires it itself —
// so there are two racing firing paths and the test stops being a test of the
// sequence it sets up. Tests that need to hold a firing open at a precise point
// (the race tests) therefore drive tick() directly and never start the loop.
func (h *harness) start() {
	h.t.Helper()
	if err := h.svc.Start(); err != nil {
		h.t.Fatalf("Start: %v", err)
	}
	h.t.Cleanup(h.svc.Stop)
}

// fireDue calls the internal tick directly, which is deterministic: it does not
// wait on a timer. The loop's own scheduling is exercised by TestNextDelay.
func (h *harness) fireDue() { h.svc.tick() }

func (h *harness) addReminder(title string, dueMs int64) Item {
	h.t.Helper()
	id, err := h.svc.Add(Item{Kind: KindReminder, Title: title, Message: title + " body", DueAtMs: dueMs})
	if err != nil {
		h.t.Fatalf("add reminder %q: %v", title, err)
	}
	it, err := h.svc.Get(id)
	if err != nil {
		h.t.Fatalf("get reminder %q: %v", title, err)
	}
	return it
}

func (h *harness) addTask(title string, dueMs int64, action Action, autoComplete bool) Item {
	h.t.Helper()
	id, err := h.svc.Add(Item{Kind: KindTask, Title: title, Message: title + " prompt", DueAtMs: dueMs, Action: action, AutoComplete: autoComplete})
	if err != nil {
		h.t.Fatalf("add task %q: %v", title, err)
	}
	it, err := h.svc.Get(id)
	if err != nil {
		h.t.Fatalf("get task %q: %v", title, err)
	}
	return it
}

// --- transition state machine -------------------------------------------

// TestTransitionMatrix is the exhaustive table over the accepted status set.
// It is exhaustive by CONSTRUCTION — it iterates Statuses(), not a hand-written
// list — so adding a status extends the coverage instead of silently leaving a
// hole in it.
func TestTransitionMatrix(t *testing.T) {
	all := Statuses()
	// wantLegal is the machine from allowedTransitions, restated here on
	// purpose. If the table above is edited without updating this, the test
	// fails and the two can never be quietly "corrected" to match each other.
	wantLegal := map[[2]Status]bool{
		{StatusPending, StatusInProgress}:   true,
		{StatusPending, StatusCompleted}:    true,
		{StatusPending, StatusCancelled}:    true,
		{StatusInProgress, StatusCompleted}: true,
		{StatusInProgress, StatusCancelled}: true,
		{StatusInProgress, StatusPending}:   true,
		{StatusCompleted, StatusPending}:    true,
		{StatusCancelled, StatusPending}:    true,
	}
	for _, from := range all {
		for _, to := range all {
			key := [2]Status{from, to}
			legal := wantLegal[key]
			if got := CanTransition(from, to); got != legal {
				t.Errorf("CanTransition(%q,%q) = %v, want %v", from, to, got, legal)
			}
			err := Transition(from, to)
			if legal && err != nil {
				t.Errorf("Transition(%q,%q) = %v, want nil", from, to, err)
			}
			if !legal {
				if err == nil {
					t.Errorf("Transition(%q,%q) = nil, want an error", from, to)
					continue
				}
				// Every rejection must be classifiable, so the HTTP layer can
				// answer 409 without matching on message text.
				if !errors.Is(err, ErrTransition) {
					t.Errorf("Transition(%q,%q) error does not wrap ErrTransition: %v", from, to, err)
				}
			}
		}
	}
}

// TestTransitionRejectsUnknownStatus pins the "no silent coercion" rule: a value
// outside the registry is an error from both entry points, never a default.
func TestTransitionRejectsUnknownStatus(t *testing.T) {
	for _, bad := range []Status{"done", "PENDING", "Pending", "archived", ""} {
		if ValidStatus(bad) {
			t.Errorf("ValidStatus(%q) = true, want false", bad)
		}
		if CanTransition(StatusPending, bad) {
			t.Errorf("CanTransition(pending, %q) = true, want false", bad)
		}
		if err := Transition(StatusPending, bad); err == nil {
			t.Errorf("Transition(pending, %q) = nil, want an error", bad)
		} else if !errors.Is(err, ErrTransition) {
			t.Errorf("Transition(pending, %q) error does not wrap ErrTransition: %v", bad, err)
		}
	}
}

// TestSetStatusRejectsIllegalTransition is the service-level half of the
// matrix: the guard must hold through the mutex, not just in the pure helper.
func TestSetStatusRejectsIllegalTransition(t *testing.T) {
	h := newHarness(t)
	it := h.addTask("ship it", 0, ActionNotify, false)
	if _, err := h.svc.SetStatus(it.ID, StatusCompleted); err != nil {
		t.Fatalf("pending -> completed: %v", err)
	}
	// completed -> cancelled is not a legal single step.
	_, err := h.svc.SetStatus(it.ID, StatusCancelled)
	if err == nil {
		t.Fatal("completed -> cancelled: got nil, want an error")
	}
	if !errors.Is(err, ErrTransition) {
		t.Fatalf("completed -> cancelled error does not wrap ErrTransition: %v", err)
	}
	// And the item must be untouched by the rejected call.
	got, err := h.svc.Get(it.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != StatusCompleted {
		t.Fatalf("status after rejected transition = %q, want %q", got.Status, StatusCompleted)
	}
}

// TestSetStatusUnmarkIsIdempotent covers the "mark or unmark" round trip and the
// retry safety that makes a double-clicked button harmless.
func TestSetStatusUnmarkIsIdempotent(t *testing.T) {
	h := newHarness(t)
	it := h.addTask("round trip", 0, ActionNotify, false)

	steps := []Status{StatusInProgress, StatusCompleted, StatusPending, StatusCancelled, StatusPending}
	for _, want := range steps {
		got, err := h.svc.SetStatus(it.ID, want)
		if err != nil {
			t.Fatalf("set %q: %v", want, err)
		}
		if got.Status != want {
			t.Fatalf("status = %q, want %q", got.Status, want)
		}
		// Re-sending the same status must be a no-op success, not a 409: a
		// retried PATCH or a double click is not a user error.
		again, err := h.svc.SetStatus(it.ID, want)
		if err != nil {
			t.Fatalf("re-set %q (idempotent): %v", want, err)
		}
		if again.Status != want {
			t.Fatalf("status after idempotent set = %q, want %q", again.Status, want)
		}
	}
}

// TestSetStatusBackToPendingRearms is the subtle half of "unmark": a one-shot
// item is gated on FiredAtMs == 0, so moving back to pending must clear it or
// an unmarked reminder could never ring again.
func TestSetStatusBackToPendingRearms(t *testing.T) {
	h := newHarness(t)
	h.now = fixedNow
	h.addReminder("already rang", fixedNow.Add(-time.Minute).UnixMilli()) // Deliberately NOT h.start(): the background run loop would fire this due item
	// itself, in parallel with the manual fireDue() below, so the assertions would
	// depend on which path won. (Same rule as the sibling race tests.)
	h.fireDue()

	items, _ := h.svc.List(ListFilter{Kind: KindReminder})
	if len(items) != 1 {
		t.Fatalf("want 1 reminder, got %d", len(items))
	}
	if items[0].FiredAtMs == 0 {
		t.Fatal("FiredAtMs = 0 after a firing; the one-shot gate is not being set")
	}
	if items[0].Status != StatusCompleted {
		t.Fatalf("status after firing = %q, want %q", items[0].Status, StatusCompleted)
	}

	// Unmark it. Without the re-arm, the second tick would deliver nothing.
	if _, err := h.svc.SetStatus(items[0].ID, StatusPending); err != nil {
		t.Fatalf("unmark: %v", err)
	}
	after, err := h.svc.Get(items[0].ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if after.FiredAtMs != 0 {
		t.Fatalf("FiredAtMs = %d after unmark, want 0 (re-armed)", after.FiredAtMs)
	}
	h.fireDue()
	if got := len(h.outbox.all()); got != 2 {
		t.Fatalf("deliveries after re-arm = %d, want 2", got)
	}
}

// TestSetStatusBackToPendingClearsStaleError guards the UI: a reopened item must
// not still show the previous run's failure.
func TestSetStatusBackToPendingClearsStaleError(t *testing.T) {
	h := newHarness(t)
	h.runner.err = errors.New("boom")
	h.addTask("fails", fixedNow.Add(-time.Minute).UnixMilli(), ActionAgent, false) // Deliberately NOT h.start(): the background run loop would fire this due item
	// itself, in parallel with the manual fireDue() below, so the assertions would
	// depend on which path won. (Same rule as the sibling race tests.)
	h.fireDue()

	items, _ := h.svc.List(ListFilter{Kind: KindTask})
	if items[0].LastError == "" {
		t.Fatal("want a recorded LastError after a failed firing")
	}
	if _, err := h.svc.SetStatus(items[0].ID, StatusPending); err != nil {
		t.Fatalf("unmark: %v", err)
	}
	after, _ := h.svc.Get(items[0].ID)
	if after.LastError != "" || after.LastStatus != "" {
		t.Fatalf("after unmark LastStatus=%q LastError=%q, want both cleared", after.LastStatus, after.LastError)
	}
}

// --- firing --------------------------------------------------------------

// TestReminderFiresOnceThenCompletes is the core one-shot contract: exactly one
// delivery, then the reminder settles on completed.
func TestReminderFiresOnceThenCompletes(t *testing.T) {
	h := newHarness(t)
	h.addReminder("stand up", fixedNow.Add(-time.Second).UnixMilli()) // Deliberately NOT h.start(): the background run loop would fire this due item
	// itself, in parallel with the manual fireDue() below, so the assertions would
	// depend on which path won. (Same rule as the sibling race tests.)
	h.fireDue()
	h.fireDue()
	h.fireDue()

	deliveries := h.outbox.all()
	if len(deliveries) != 1 {
		t.Fatalf("deliveries = %d, want 1 (a reminder must ring exactly once)", len(deliveries))
	}
	if deliveries[0].JobID != "reminder:"+deliveries[0].JobID[len("reminder:"):] {
		t.Fatalf("delivery JobID = %q, want the reminder-prefixed form", deliveries[0].JobID)
	}
	if h.notifier.count() != 1 {
		t.Fatalf("notifications = %d, want 1", h.notifier.count())
	}
	items, _ := h.svc.List(ListFilter{Kind: KindReminder})
	if items[0].Status != StatusCompleted {
		t.Fatalf("status = %q, want %q", items[0].Status, StatusCompleted)
	}
	if items[0].Runs != 1 {
		t.Fatalf("Runs = %d, want 1", items[0].Runs)
	}
	if h.runner.count() != 0 {
		t.Fatalf("agent runs = %d, want 0 for a notify-only reminder", h.runner.count())
	}
}

// TestReminderNotDueYetDoesNotFire is the negative half: a future due time is
// not due, and 0 (no due date) is not due either.
func TestReminderNotDueYetDoesNotFire(t *testing.T) {
	h := newHarness(t)
	h.addReminder("later", fixedNow.Add(time.Hour).UnixMilli())
	h.addTask("no due date", 0, ActionNotify, false) // Deliberately NOT h.start(): the background run loop would fire this due item
	// itself, in parallel with the manual fireDue() below, so the assertions would
	// depend on which path won. (Same rule as the sibling race tests.)
	h.fireDue()
	if got := len(h.outbox.all()); got != 0 {
		t.Fatalf("deliveries = %d, want 0", got)
	}
}

// TestTaskDoesNotAutoCompleteWithoutTheFlag pins the opt-in: a notify firing
// leaves the task pending so the user still decides, even though it "fired".
func TestTaskDoesNotAutoCompleteWithoutTheFlag(t *testing.T) {
	h := newHarness(t)
	it := h.addTask("check the logs", fixedNow.Add(-time.Second).UnixMilli(), ActionNotify, false) // Deliberately NOT h.start(): the background run loop would fire this due item
	// itself, in parallel with the manual fireDue() below, so the assertions would
	// depend on which path won. (Same rule as the sibling race tests.)
	h.fireDue()

	after, _ := h.svc.Get(it.ID)
	if after.Status != StatusPending {
		t.Fatalf("status = %q, want %q (auto_complete is off)", after.Status, StatusPending)
	}
	if after.FiredAtMs == 0 {
		t.Fatal("FiredAtMs = 0; the task would fire again on the next tick")
	}
	// And it really must not fire again.
	h.fireDue()
	if got := len(h.outbox.all()); got != 1 {
		t.Fatalf("deliveries = %d, want 1", got)
	}
}

// TestTaskAutoCompletesOnSuccessfulAgentRun is the "fires an agent and
// autocompletes" half of the request.
func TestTaskAutoCompletesOnSuccessfulAgentRun(t *testing.T) {
	h := newHarness(t)
	it := h.addTask("fix the build", fixedNow.Add(-time.Second).UnixMilli(), ActionAgent, true) // Deliberately NOT h.start(): the background run loop would fire this due item
	// itself, in parallel with the manual fireDue() below, so the assertions would
	// depend on which path won. (Same rule as the sibling race tests.)
	h.fireDue()

	after, _ := h.svc.Get(it.ID)
	if after.Status != StatusCompleted {
		t.Fatalf("status = %q, want %q", after.Status, StatusCompleted)
	}
	if h.runner.count() != 1 {
		t.Fatalf("agent runs = %d, want 1", h.runner.count())
	}
	if got := h.outbox.all(); len(got) != 1 || got[0].Result != "agent said hi" {
		t.Fatalf("deliveries = %+v, want one carrying the agent output", got)
	}
}

// TestTaskStaysPendingWhenAgentFails: a failed turn must NOT auto-complete,
// or a broken task silently looks done.
func TestTaskStaysPendingWhenAgentFails(t *testing.T) {
	h := newHarness(t)
	h.runner.err = errors.New("model unavailable")
	it := h.addTask("fix the build", fixedNow.Add(-time.Second).UnixMilli(), ActionAgent, true) // Deliberately NOT h.start(): the background run loop would fire this due item
	// itself, in parallel with the manual fireDue() below, so the assertions would
	// depend on which path won. (Same rule as the sibling race tests.)
	h.fireDue()

	after, _ := h.svc.Get(it.ID)
	if after.Status != StatusPending {
		t.Fatalf("status = %q, want %q after a failed agent turn", after.Status, StatusPending)
	}
	if after.LastStatus != "error" {
		t.Fatalf("LastStatus = %q, want %q", after.LastStatus, "error")
	}
	if after.LastError != "model unavailable" {
		t.Fatalf("LastError = %q, want the runner error", after.LastError)
	}
}

// TestAgentActionWithoutRunnerRecordsError: a mis-wired host must surface an
// error on the item, not a silent success.
func TestAgentActionWithoutRunnerRecordsError(t *testing.T) {
	h := newHarness(t)
	// Detach the runner the way a host that never wired one would be.
	h.svc.SetAgentRunner(nil)
	it := h.addTask("needs an agent", fixedNow.Add(-time.Second).UnixMilli(), ActionAgent, false) // Deliberately NOT h.start(): the background run loop would fire this due item
	// itself, in parallel with the manual fireDue() below, so the assertions would
	// depend on which path won. (Same rule as the sibling race tests.)
	h.fireDue()

	after, _ := h.svc.Get(it.ID)
	if after.LastStatus != "error" {
		t.Fatalf("LastStatus = %q, want %q", after.LastStatus, "error")
	}
	if after.LastError == "" {
		t.Fatal("LastError is empty; a mis-wired host was reported as success")
	}
	// A failed firing must not notify, and must not complete a task.
	if h.notifier.count() != 0 {
		t.Fatalf("notifications = %d, want 0 for a failed turn", h.notifier.count())
	}
	if after.Status != StatusPending {
		t.Fatalf("status = %q, want %q", after.Status, StatusPending)
	}
}

// TestNotifierFailureDoesNotBlockCompletion: the notification is a side channel.
// If the OS notification service is down the item must still settle and the
// run must still be recorded — otherwise a dead notifier wedges every reminder.
func TestNotifierFailureDoesNotBlockCompletion(t *testing.T) {
	h := newHarness(t)
	h.notifier.err = errors.New("no notification service")
	h.addReminder("notify fails", fixedNow.Add(-time.Second).UnixMilli()) // Deliberately NOT h.start(): the background run loop would fire this due item
	// itself, in parallel with the manual fireDue() below, so the assertions would
	// depend on which path won. (Same rule as the sibling race tests.)
	h.fireDue()

	items, _ := h.svc.List(ListFilter{Kind: KindReminder})
	if items[0].Status != StatusCompleted {
		t.Fatalf("status = %q, want %q despite the notifier failing", items[0].Status, StatusCompleted)
	}
	if len(h.outbox.all()) != 1 {
		t.Fatal("the delivery log must still record the firing")
	}
	if h.runs.count() != 1 {
		t.Fatal("the run history must still record the firing")
	}
}

// TestResetToPendingDuringAgentRunIsNotOverwritten is the race the
// compare-and-set in commitFire exists for.
//
// The obvious version of this test — cancel during the turn — is NOT enough:
// the transition table already refuses cancelled -> completed, so the test
// would pass even with the compare-and-set deleted. The case only the
// compare-and-set catches is a task that was in_progress when the turn started
// and that the user reset to PENDING while it ran. completed IS reachable from
// pending, so without the `live.Status == item.Status` guard the turn's
// completion would silently mark a task done that the user had just reopened.
func TestResetToPendingDuringAgentRunIsNotOverwritten(t *testing.T) {
	h := newHarness(t)
	release := make(chan struct{})
	h.runner.release = release
	it := h.addTask("long job", fixedNow.Add(-time.Second).UnixMilli(), ActionAgent, true)
	if _, err := h.svc.SetStatus(it.ID, StatusInProgress); err != nil {
		t.Fatalf("mark in progress: %v", err)
	}
	// Deliberately NOT h.start(): the background loop would fire this due item
	// itself, in parallel with the tick() below, and the test would no longer
	// control the ordering it is asserting about.
	// h.t.Cleanup is unnecessary too: no goroutine is started.

	fired := make(chan struct{})
	go func() {
		h.fireDue()
		close(fired)
	}()
	<-h.runner.entered
	if _, err := h.svc.SetStatus(it.ID, StatusPending); err != nil {
		t.Fatalf("reset to pending: %v", err)
	}
	close(release)
	<-fired

	after, err := h.svc.Get(it.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if after.Status != StatusPending {
		t.Fatalf("status = %q, want %q — the in-flight completion overwrote the user's reset", after.Status, StatusPending)
	}
	// The run bookkeeping must still be honest: the agent really did run.
	if after.Runs != 1 {
		t.Fatalf("Runs = %d, want 1", after.Runs)
	}
	if after.LastStatus != "ok" {
		t.Fatalf("LastStatus = %q, want %q", after.LastStatus, "ok")
	}
	// And the reset re-armed it, so the still-past due time will fire again.
	if after.FiredAtMs != 0 {
		t.Fatalf("FiredAtMs = %d after a reset to pending, want 0 (re-armed)", after.FiredAtMs)
	}
}

// TestCancelDuringAgentRunIsNotOverwritten covers the sibling race. The
// transition table alone would block this one, which is exactly why the test
// above exists in the in_progress -> pending shape.
func TestCancelDuringAgentRunIsNotOverwritten(t *testing.T) {
	h := newHarness(t)
	release := make(chan struct{})
	h.runner.release = release
	it := h.addTask("long job", fixedNow.Add(-time.Second).UnixMilli(), ActionAgent, true)
	// Deliberately NOT h.start(): see the sibling test above.
	fired := make(chan struct{})
	go func() {
		h.fireDue()
		close(fired)
	}()

	// Wait until the runner is actually inside the turn, then cancel.
	<-h.runner.entered
	if _, err := h.svc.SetStatus(it.ID, StatusCancelled); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	close(release)
	<-fired

	after, err := h.svc.Get(it.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if after.Status != StatusCancelled {
		t.Fatalf("status = %q, want %q — the in-flight completion overwrote the cancel", after.Status, StatusCancelled)
	}
	// The run bookkeeping must still be honest: the agent really did run.
	if after.Runs != 1 {
		t.Fatalf("Runs = %d, want 1", after.Runs)
	}
	if after.LastStatus != "ok" {
		t.Fatalf("LastStatus = %q, want %q", after.LastStatus, "ok")
	}
}

// TestFireOnceAbandonsUnarmedSnapshot pins fireOnce's own guard. tick() takes
// snapshots and then calls fireOnce, so an item can stop being armed in between
// — and only fireOnce's re-check stops the claim, the delivery, and the agent
// turn from happening for an item the user has already dealt with. This calls
// fireOnce directly with a stale snapshot, because the window is not
// reachable deterministically through the loop.
func TestFireOnceAbandonsUnarmedSnapshot(t *testing.T) {
	h := newHarness(t)
	it := h.addReminder("stale snapshot", fixedNow.Add(-time.Second).UnixMilli())
	// Deliberately NOT h.start(): see TestResetToPendingDuringAgentRunIsNotOverwritten.
	// The background loop would fire this due item itself, in parallel with the
	// fireOnce below, and the delivery this test asserts against would arrive from
	// the WRONG firing path — intermittently, which is worse than not testing it.

	// Take a snapshot while the item is genuinely due...
	stale := it
	// ...then take it out of the running before the firing happens.
	if _, err := h.svc.SetStatus(it.ID, StatusCancelled); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	h.svc.fireOnce(&stale)

	if h.runner.count() != 0 {
		t.Fatalf("agent runs = %d for a cancelled item, want 0", h.runner.count())
	}
	if got := len(h.outbox.all()); got != 0 {
		t.Fatalf("deliveries = %d for a cancelled item, want 0", got)
	}
	after, _ := h.svc.Get(it.ID)
	if after.Runs != 0 {
		t.Fatalf("Runs = %d for a cancelled item, want 0 (the claim must not happen)", after.Runs)
	}
	if after.FiredAtMs != 0 {
		t.Fatalf("FiredAtMs = %d for a cancelled item, want 0", after.FiredAtMs)
	}
}

// TestNextDelayAgreesWithDue is a LIVENESS test, not a cosmetic one.
//
// nextDelayLocked returning 0 tells run() to tick immediately, and tick can
// only progress by claiming something Due(). If the two predicates disagree
// about which statuses are live, an item can be permanently "due" to the delay
// computation and permanently refused by the firing gate: the delay never
// becomes positive, and run() loops tick/continue at 100% CPU forever.
//
// So the test asserts they can never disagree: for every status, whether
// nextDelayLocked considers the item a candidate must equal whether Due()
// considers it fireable. A divergence (here, narrowing Due() to pending only
// while in_progress stays a delay candidate) is caught here instead of hanging
// a CI worker.
func TestNextDelayAgreesWithDue(t *testing.T) {
	h := newHarness(t)
	// One due item per status, all sharing a past due time so the delay is
	// driven purely by liveness, not by the clock.
	for _, st := range Statuses() {
		it := mustAdd(t, h, Item{Kind: KindTask, Title: "task " + string(st), DueAtMs: fixedNow.Add(-time.Hour).UnixMilli()})
		if _, err := h.svc.SetStatus(it.ID, st); err != nil {
			t.Fatalf("set %s: %v", st, err)
		}
	}
	h.svc.mu.Lock()
	dueDelay := h.svc.nextDelayLocked()
	h.svc.mu.Unlock()
	if dueDelay != 0 {
		t.Fatalf("nextDelay = %v for past-due items, want 0 (fire immediately)", dueDelay)
	}

	for _, st := range Statuses() {
		it := mustAdd(t, h, Item{Kind: KindTask, Title: "probe " + string(st), DueAtMs: fixedNow.Add(-time.Hour).UnixMilli()})
		if _, err := h.svc.SetStatus(it.ID, st); err != nil {
			t.Fatalf("set %s: %v", st, err)
		}
		probe, err := h.svc.Get(it.ID)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		want := probe.Active()
		if got := probe.Due(h.now.UnixMilli()); got != want {
			t.Errorf("status %q: Due() = %v but Active() = %v; the firing gate and the run loop now disagree, which busy-spins the loop", st, got, want)
		}
	}
}

// TestRunLoopDoesNotSpinWhenDueItemsAreFired is the direct guard for the same
// hazard: after the loop has claimed everything due, nextDelayLocked must
// become positive. A zero delay with nothing left to fire is the spin.
func TestRunLoopDoesNotSpinWhenDueItemsAreFired(t *testing.T) {
	h := newHarness(t)
	h.addReminder("one shot", fixedNow.Add(-time.Minute).UnixMilli()) // Deliberately NOT h.start(): the background run loop would fire this due item
	// itself, in parallel with the manual fireDue() below, so the assertions would
	// depend on which path won. (Same rule as the sibling race tests.)
	h.fireDue()

	h.svc.mu.Lock()
	d := h.svc.nextDelayLocked()
	h.svc.mu.Unlock()
	if d <= 0 {
		t.Fatalf("nextDelay = %v after everything due was claimed, want a positive delay; the loop would spin", d)
	}
	if d != idleDelay {
		t.Fatalf("nextDelay = %v with nothing armed, want the %v idle delay", d, idleDelay)
	}
}

// TestInProgressItemStillFires: an in_progress item is still "active" for
// firing purposes. A task that is due AND started is precisely the one worth
// nudging, so excluding in_progress from the firing gate would silently drop
// the reminders a user most wants to see.
func TestInProgressItemStillFires(t *testing.T) {
	h := newHarness(t)
	it := h.addTask("started but not finished", fixedNow.Add(-time.Second).UnixMilli(), ActionNotify, false)
	if _, err := h.svc.SetStatus(it.ID, StatusInProgress); err != nil {
		t.Fatalf("mark in progress: %v", err)
	} // Deliberately NOT h.start(): the background run loop would fire this due item
	// itself, in parallel with the manual fireDue() below, so the assertions would
	// depend on which path won. (Same rule as the sibling race tests.)
	h.fireDue()

	if got := len(h.outbox.all()); got != 1 {
		t.Fatalf("deliveries = %d for an in_progress due item, want 1", got)
	}
	after, _ := h.svc.Get(it.ID)
	if after.Status != StatusInProgress {
		t.Fatalf("status = %q, want %q to survive a notify firing", after.Status, StatusInProgress)
	}
}

// TestFiringSkipsItemDeletedMidFlight: a delete that lands while the turn runs
// must not be undone or panic.
func TestFiringSkipsItemDeletedMidFlight(t *testing.T) {
	h := newHarness(t)
	release := make(chan struct{})
	h.runner.release = release
	it := h.addTask("doomed", fixedNow.Add(-time.Second).UnixMilli(), ActionAgent, false)
	// Deliberately NOT h.start(): see TestResetToPendingDuringAgentRunIsNotOverwritten.
	fired := make(chan struct{})
	go func() {
		h.fireDue()
		close(fired)
	}()
	<-h.runner.entered
	if err := h.svc.Remove(it.ID); err != nil {
		t.Fatalf("remove: %v", err)
	}
	close(release)
	<-fired

	if _, err := h.svc.Get(it.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get after delete = %v, want ErrNotFound", err)
	}
}

// TestFireNowBypassesDueGate covers the "run now" button, including the refusal
// to fire a terminal item — a cancelled reminder must not ring.
func TestFireNowBypassesDueGate(t *testing.T) {
	h := newHarness(t)
	it := h.addReminder("not for an hour", fixedNow.Add(time.Hour).UnixMilli())
	h.start()

	if _, err := h.svc.FireNow(it.ID); err != nil {
		t.Fatalf("FireNow: %v", err)
	}
	if got := len(h.outbox.all()); got != 1 {
		t.Fatalf("deliveries = %d, want 1", got)
	}

	cancelled := h.addTask("cancelled", fixedNow.Add(time.Hour).UnixMilli(), ActionNotify, false)
	if _, err := h.svc.SetStatus(cancelled.ID, StatusCancelled); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if _, err := h.svc.FireNow(cancelled.ID); err == nil {
		t.Fatal("FireNow on a cancelled item: got nil, want an error")
	}
	if got := len(h.outbox.all()); got != 1 {
		t.Fatalf("deliveries = %d after a refused FireNow, want 1", got)
	}
}

// --- validation ----------------------------------------------------------

// TestValidation pins the normalise/reject rules, including the two the UI
// depends on: a reminder REQUIRES a due time, and auto_complete is meaningless
// outside a task whose firing runs an agent.
func TestValidation(t *testing.T) {
	cases := []struct {
		name    string
		item    Item
		wantErr string
		check   func(t *testing.T, it Item)
	}{
		{
			name:    "reminder without a due time is rejected",
			item:    Item{Kind: KindReminder, Title: "x"},
			wantErr: "needs a due time",
		},
		{
			name:  "task without a due time is allowed",
			item:  Item{Kind: KindTask, Title: "x"},
			check: func(t *testing.T, it Item) {},
		},
		{
			name:    "blank title is rejected",
			item:    Item{Kind: KindTask, Title: "   "},
			wantErr: "title is required",
		},
		{
			name:    "unknown kind is rejected",
			item:    Item{Kind: "alert", Title: "x"},
			wantErr: "kind must be",
		},
		{
			name:    "unknown action is rejected",
			item:    Item{Kind: KindTask, Title: "x", Action: "sms"},
			wantErr: "action must be",
		},
		{
			name: "blank status and action take their documented defaults",
			item: Item{Kind: KindTask, Title: "x"},
			check: func(t *testing.T, it Item) {
				if it.Status != StatusPending {
					t.Errorf("Status = %q, want %q", it.Status, StatusPending)
				}
				if it.Action != ActionNotify {
					t.Errorf("Action = %q, want %q", it.Action, ActionNotify)
				}
			},
		},
		{
			name: "auto_complete is cleared for a notify firing",
			item: Item{Kind: KindTask, Title: "x", Action: ActionNotify, AutoComplete: true},
			check: func(t *testing.T, it Item) {
				if it.AutoComplete {
					t.Error("AutoComplete survived on a notify task; the flag would silently do nothing")
				}
			},
		},
		{
			name: "auto_complete is cleared for a reminder",
			item: Item{Kind: KindReminder, Title: "x", DueAtMs: 1, AutoComplete: true},
			check: func(t *testing.T, it Item) {
				if it.AutoComplete {
					t.Error("AutoComplete survived on a reminder")
				}
			},
		},
		{
			name: "auto_complete is kept for a task that runs an agent",
			item: Item{Kind: KindTask, Title: "x", Action: ActionAgent, AutoComplete: true},
			check: func(t *testing.T, it Item) {
				if !it.AutoComplete {
					t.Error("AutoComplete was cleared on the one shape where it means something")
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.item.validate()
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("validate() = nil, want an error containing %q", tc.wantErr)
				}
				if !contains(err.Error(), tc.wantErr) {
					t.Fatalf("validate() = %q, want it to contain %q", err.Error(), tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("validate() = %v, want nil", err)
			}
			if tc.check != nil {
				tc.check(t, tc.item)
			}
		})
	}
}

// TestAddForcesPending: creating an already-done item is meaningless and would
// make "create" and "mark done" two ways to say the same thing.
func TestAddForcesPending(t *testing.T) {
	h := newHarness(t)
	id, err := h.svc.Add(Item{Kind: KindTask, Title: "done at birth", Status: StatusCompleted})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	it, _ := h.svc.Get(id)
	if it.Status != StatusPending {
		t.Fatalf("Status = %q, want %q", it.Status, StatusPending)
	}
}

// TestAddRejectsOverLongTitle guards the store from an unbounded label.
func TestAddRejectsOverLongTitle(t *testing.T) {
	h := newHarness(t)
	long := make([]rune, maxTitleLen+1)
	for i := range long {
		long[i] = 'x'
	}
	if _, err := h.svc.Add(Item{Kind: KindTask, Title: string(long)}); err == nil {
		t.Fatal("add with an over-long title: got nil, want an error")
	}
}

// --- list ordering and paging -------------------------------------------

// TestListSortsDueFirstThenUndated pins the half of the order that is
// deterministic: dated items come first, in due order, with the soonest (and so
// possibly overdue) at the very top; undated checklist items come last, because
// an item with no deadline must not outrank something that is about to fire.
//
// The tie-break half of the order is checked separately, with distinct created
// times, in TestListBreaksTiesDeterministically.
func TestListSortsDueFirstThenUndated(t *testing.T) {
	h := newHarness(t)
	// Added out of due order on purpose.
	mustAdd(t, h, Item{Kind: KindTask, Title: "undated"})
	mustAdd(t, h, Item{Kind: KindTask, Title: "due later", DueAtMs: fixedNow.Add(2 * time.Hour).UnixMilli()})
	mustAdd(t, h, Item{Kind: KindTask, Title: "overdue", DueAtMs: fixedNow.Add(-time.Hour).UnixMilli()})
	mustAdd(t, h, Item{Kind: KindTask, Title: "due sooner", DueAtMs: fixedNow.Add(time.Hour).UnixMilli()})

	items, total := h.svc.List(ListFilter{Kind: KindTask})
	if total != 4 {
		t.Fatalf("total = %d, want 4", total)
	}
	got := make([]string, 0, len(items))
	for _, it := range items {
		got = append(got, it.Title)
	}
	want := []string{"overdue", "due sooner", "due later", "undated"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}

// TestListBreaksTiesDeterministically: two items sharing a due time (or both
// undated) must still have a TOTAL order, or paging can repeat or skip an item
// between two polls.
func TestListBreaksTiesDeterministically(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "reminders.json")
	svc := NewService(path)

	// A moving clock gives the items distinct created_at_ms, the first
	// tie-break, without sleeping.
	base := fixedNow
	svc.SetClock(func() time.Time {
		base = base.Add(time.Millisecond)
		return base
	})
	first, err := svc.Add(Item{Kind: KindTask, Title: "created first", DueAtMs: fixedNow.Add(time.Hour).UnixMilli()})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	second, err := svc.Add(Item{Kind: KindTask, Title: "created second", DueAtMs: fixedNow.Add(time.Hour).UnixMilli()})
	if err != nil {
		t.Fatalf("add: %v", err)
	}

	firstPage, _ := svc.List(ListFilter{Kind: KindTask, Limit: 1, Offset: 0})
	secondPage, _ := svc.List(ListFilter{Kind: KindTask, Limit: 1, Offset: 1})
	if len(firstPage) != 1 || len(secondPage) != 1 {
		t.Fatalf("pages = %d/%d, want 1/1", len(firstPage), len(secondPage))
	}
	if firstPage[0].ID != first || secondPage[0].ID != second {
		t.Fatalf("tie-break order = %q then %q, want %q then %q",
			firstPage[0].ID, secondPage[0].ID, first, second)
	}
}

// TestListPaginatesAfterSorting proves paging is applied to the SORTED list,
// not to insertion order — otherwise page 2 can repeat or skip an item.
func TestListPaginatesAfterSorting(t *testing.T) {
	h := newHarness(t)
	for i := 0; i < 7; i++ {
		mustAdd(t, h, Item{
			Kind:    KindTask,
			Title:   fmt.Sprintf("t%d", i),
			DueAtMs: fixedNow.Add(time.Duration(i) * time.Hour).UnixMilli(),
		})
	}
	all, total := h.svc.List(ListFilter{Kind: KindTask, Limit: 100})
	if total != 7 || len(all) != 7 {
		t.Fatalf("full list: len=%d total=%d, want 7/7", len(all), total)
	}
	page1, _ := h.svc.List(ListFilter{Kind: KindTask, Limit: 3, Offset: 0})
	page2, _ := h.svc.List(ListFilter{Kind: KindTask, Limit: 3, Offset: 3})
	last, _ := h.svc.List(ListFilter{Kind: KindTask, Limit: 3, Offset: 6})
	if len(page1) != 3 || len(page2) != 3 || len(last) != 1 {
		t.Fatalf("page sizes = %d/%d/%d, want 3/3/1", len(page1), len(page2), len(last))
	}
	for i, it := range all {
		switch {
		case i < 3 && it.ID != page1[i].ID:
			t.Fatalf("page1[%d] = %q, want %q", i, it.ID, page1[i].ID)
		case i >= 3 && i < 6 && it.ID != page2[i-3].ID:
			t.Fatalf("page2[%d] = %q, want %q", i-3, it.ID, page2[i-3].ID)
		case i == 6 && it.ID != last[0].ID:
			t.Fatalf("last[0] = %q, want %q", last[0].ID, it.ID)
		}
	}
	// An offset past the end is an empty page, not a panic and not a wrap.
	empty, totalBeyond := h.svc.List(ListFilter{Kind: KindTask, Limit: 3, Offset: 99})
	if len(empty) != 0 {
		t.Fatalf("page past the end = %d items, want 0", len(empty))
	}
	if totalBeyond != 7 {
		t.Fatalf("total past the end = %d, want 7 (the filter total is independent of paging)", totalBeyond)
	}
}

// TestListFiltersByKindAndStatus proves the two kinds never bleed into each
// other's view — the property the /api/reminders and /api/tasks routes rest on.
func TestListFiltersByKindAndStatus(t *testing.T) {
	h := newHarness(t)
	rem := mustAdd(t, h, Item{Kind: KindReminder, Title: "r", DueAtMs: fixedNow.UnixMilli()})
	task := mustAdd(t, h, Item{Kind: KindTask, Title: "t"})
	if _, err := h.svc.SetStatus(task.ID, StatusCompleted); err != nil {
		t.Fatalf("complete task: %v", err)
	}

	remindersOnly, total := h.svc.List(ListFilter{Kind: KindReminder})
	if total != 1 || len(remindersOnly) != 1 || remindersOnly[0].ID != rem.ID {
		t.Fatalf("reminder view = %+v total=%d, want just %s", remindersOnly, total, rem.ID)
	}
	pending, total := h.svc.List(ListFilter{Kind: KindTask, Status: StatusPending})
	if total != 0 || len(pending) != 0 {
		t.Fatalf("pending tasks = %+v total=%d, want none (the only task is completed)", pending, total)
	}
	completed, total := h.svc.List(ListFilter{Kind: KindTask, Status: StatusCompleted})
	if total != 1 || completed[0].ID != task.ID {
		t.Fatalf("completed tasks = %+v total=%d", completed, total)
	}
}

// --- persistence ---------------------------------------------------------

// TestPersistsAcrossRestart: the whole point of a separate store is that the
// list survives the process.
func TestPersistsAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "reminders.json")

	first := NewService(path)
	first.SetClock(func() time.Time { return fixedNow })
	remID, err := first.Add(Item{Kind: KindReminder, Title: "persisted reminder", DueAtMs: fixedNow.Add(time.Hour).UnixMilli()})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	taskID, err := first.Add(Item{Kind: KindTask, Title: "persisted task"})
	if err != nil {
		t.Fatalf("add task: %v", err)
	}
	if _, err := first.SetStatus(taskID, StatusInProgress); err != nil {
		t.Fatalf("set status: %v", err)
	}

	second := NewService(path)
	second.SetClock(func() time.Time { return fixedNow })
	if err := second.Start(); err != nil {
		t.Fatalf("restart: %v", err)
	}
	t.Cleanup(second.Stop)

	rem, err := second.Get(remID)
	if err != nil {
		t.Fatalf("reminder gone after restart: %v", err)
	}
	if rem.Title != "persisted reminder" {
		t.Fatalf("title = %q, want %q", rem.Title, "persisted reminder")
	}
	task, err := second.Get(taskID)
	if err != nil {
		t.Fatalf("task gone after restart: %v", err)
	}
	if task.Status != StatusInProgress {
		t.Fatalf("task status = %q after restart, want %q", task.Status, StatusInProgress)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("store file missing: %v", err)
	}
}

// TestLoadRefusesUnknownStatus: a store this build cannot interpret is an
// error, never a silent reset. Resetting would destroy the user's list, which
// is the one thing a reminder file must never do.
func TestLoadRefusesUnknownStatus(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reminders.json")
	raw := `{"version":1,"items":[{"id":"aaaaaaaa","kind":"task","title":"t","status":"archived"}]}`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatalf("seed store: %v", err)
	}
	svc := NewService(path)
	err := svc.Start()
	if err == nil {
		svc.Stop()
		t.Fatal("Start on an unknown status: got nil, want an error")
	}
	if !contains(err.Error(), "archived") {
		t.Fatalf("error = %q, want it to name the offending status", err.Error())
	}
	// The loop must not be running, so nothing can overwrite the file.
	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatalf("store file was removed by the failed load: %v", statErr)
	}
}

// TestLoadRefusesCorruptStore: same contract, for unparseable JSON.
func TestLoadRefusesCorruptStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reminders.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatalf("seed store: %v", err)
	}
	svc := NewService(path)
	if err := svc.Start(); err == nil {
		svc.Stop()
		t.Fatal("Start on a corrupt store: got nil, want an error")
	}
}

// TestUpdateLeavesItemUnchangedOnRejection: a rejected patch must not partially
// apply, or the user fixes one field and silently loses another.
func TestUpdateLeavesItemUnchangedOnRejection(t *testing.T) {
	h := newHarness(t)
	id, err := h.svc.Add(Item{Kind: KindTask, Title: "keep me", Message: "original body"})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	// Blanking the title is invalid; renaming the item at the same time is
	// valid. The whole patch must be refused.
	blank := "   "
	good := "renamed"
	if _, err := h.svc.Update(id, ItemPatch{Title: &blank, Message: &good}); err == nil {
		t.Fatal("patch with a blank title: got nil, want an error")
	}
	it, _ := h.svc.Get(id)
	if it.Title != "keep me" {
		t.Fatalf("title = %q after a rejected patch, want %q", it.Title, "keep me")
	}
	if it.Message != "original body" {
		t.Fatalf("message = %q after a rejected patch; the valid half of the patch leaked through", it.Message)
	}
}

// TestPatchOnlyTouchesProvidedFields: a PATCH of one field must not reset the
// others, which is the whole reason ItemPatch uses pointers.
func TestPatchOnlyTouchesProvidedFields(t *testing.T) {
	h := newHarness(t)
	it := h.addTask("original", fixedNow.Add(time.Hour).UnixMilli(), ActionAgent, true)
	newTitle := "renamed"
	got, err := h.svc.Update(it.ID, ItemPatch{Title: &newTitle})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if got.Title != "renamed" {
		t.Fatalf("title = %q, want %q", got.Title, "renamed")
	}
	if got.Action != ActionAgent || !got.AutoComplete || got.DueAtMs != it.DueAtMs || got.Message != it.Message {
		t.Fatalf("a one-field patch reset other fields: %+v", got)
	}
}

// TestUpdateWakesTheLoop: moving a due time into the past must be noticed
// promptly, not at the next unrelated fire.
func TestUpdateWakesTheLoop(t *testing.T) {
	h := newHarness(t)
	it := h.addReminder("moved", fixedNow.Add(48*time.Hour).UnixMilli())
	past := fixedNow.Add(-time.Minute).UnixMilli()
	if _, err := h.svc.Update(it.ID, ItemPatch{DueAtMs: &past}); err != nil {
		t.Fatalf("update due: %v", err)
	}
	h.start()
	// The loop is live; give it a moment to observe the wake.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(h.outbox.all()) > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("moving a due time into the past did not fire within 2s; the loop was not woken")
}

// TestNextDelayIsCapped: a far-future due time must not park the loop for
// years, or a sleep/wake or clock change would skip the fire.
func TestNextDelayIsCapped(t *testing.T) {
	h := newHarness(t)
	h.addReminder("far future", fixedNow.AddDate(4, 0, 0).UnixMilli())
	h.svc.mu.Lock()
	d := h.svc.nextDelayLocked()
	h.svc.mu.Unlock()
	if d != maxPollLeg {
		t.Fatalf("nextDelay = %v, want the %v safety cap", d, maxPollLeg)
	}
}

// --- helpers -------------------------------------------------------------

func mustAdd(t *testing.T, h *harness, it Item) Item {
	t.Helper()
	id, err := h.svc.Add(it)
	if err != nil {
		t.Fatalf("add %q: %v", it.Title, err)
	}
	got, err := h.svc.Get(id)
	if err != nil {
		t.Fatalf("get %q: %v", it.Title, err)
	}
	return got
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
