package agent

import (
	"strings"
	"sync"
	"testing"
	"time"
)

func TestAgentRunRegistryLifecycle(t *testing.T) {
	r := NewAgentRunRegistry()
	run := r.New("explore")
	if run.ID == "" || run.Name != "explore" {
		t.Fatalf("bad run: %+v", run)
	}
	if run.statusValue() != RunRunning {
		t.Fatalf("new run status = %s, want running", run.statusValue())
	}
	run.appendTranscript(Message{Role: "assistant", Content: "line one\nline two"})
	run.finishOK("final result")
	if run.statusValue() != RunDone {
		t.Fatalf("status = %s, want done", run.statusValue())
	}
	got, ok := r.Get(run.ID)
	if !ok {
		t.Fatal("run not found by ID")
	}
	if got.Result != "final result" {
		t.Fatalf("result = %q", got.Result)
	}
}

func TestAgentRunTranscriptCap(t *testing.T) {
	run := &AgentRun{ID: "agent-1", Name: "x", Status: RunRunning}
	for i := 0; i < transcriptCap+50; i++ {
		run.appendTranscript(Message{Role: "assistant", Content: "msg"})
	}
	if n := len(run.transcriptCopy()); n > transcriptCap {
		t.Fatalf("transcript not capped: %d", n)
	}
}

func TestAgentRunLastLines(t *testing.T) {
	run := &AgentRun{ID: "agent-1", Name: "x", Status: RunRunning}
	run.appendTranscript(Message{Role: "assistant", Content: "alpha\nbeta\ngamma"})
	lines := run.LastLines(2)
	if len(lines) != 2 || lines[0] != "beta" || lines[1] != "gamma" {
		t.Fatalf("LastLines = %v", lines)
	}
}

func TestAgentRunRegistryUnknown(t *testing.T) {
	r := NewAgentRunRegistry()
	if _, ok := r.Get("agent-999"); ok {
		t.Fatal("expected miss for unknown run")
	}
	_ = strings.TrimSpace("") // keep strings import used
}

func TestAgentRunSnapshot(t *testing.T) {
	r := NewAgentRunRegistry()
	r.New("explore")
	r.New("scout")
	snap := r.Snapshot()
	if len(snap) != 2 {
		t.Fatalf("snapshot len = %d, want 2", len(snap))
	}
}

func TestAgentRunRunningCount(t *testing.T) {
	r := NewAgentRunRegistry()
	r1 := r.New("explore")
	r2 := r.New("scout")
	r2.finishOK("done")
	if c := r.RunningCount(); c != 1 {
		t.Fatalf("running count = %d, want 1", c)
	}
	_ = r1
}

func TestAgentRunRegistryCancelAll(t *testing.T) {
	r := NewAgentRunRegistry()
	run := r.New("explore")
	cancelled := false
	run.Sub = NewAgent(nil, nil, nil, nil)
	run.Cancel = func() { cancelled = true }
	r.CancelAll()
	if !cancelled {
		t.Fatal("CancelAll did not invoke run cancel")
	}
}

func TestAgentRun_ModelLabel(t *testing.T) {
	t.Run("returns provider/model when subagent has both", func(t *testing.T) {
		run := &AgentRun{
			Sub: &Agent{client: &GenericClient{Provider: "opencode-go", Model: "deepseek-v4-flash"}},
		}
		got := run.ModelLabel()
		if got != "opencode-go/deepseek-v4-flash" {
			t.Fatalf("ModelLabel = %q, want %q", got, "opencode-go/deepseek-v4-flash")
		}
	})

	t.Run("returns model only when no provider", func(t *testing.T) {
		run := &AgentRun{
			Sub: &Agent{client: &GenericClient{Provider: "", Model: "gpt-4o"}},
		}
		got := run.ModelLabel()
		if got != "gpt-4o" {
			t.Fatalf("ModelLabel = %q, want %q", got, "gpt-4o")
		}
	})

	t.Run("returns empty string when Sub is nil", func(t *testing.T) {
		run := &AgentRun{Sub: nil}
		if got := run.ModelLabel(); got != "" {
			t.Fatalf("ModelLabel = %q, want empty", got)
		}
	})

	t.Run("returns empty string when Sub client is nil", func(t *testing.T) {
		run := &AgentRun{Sub: &Agent{client: nil}}
		if got := run.ModelLabel(); got != "" {
			t.Fatalf("ModelLabel = %q, want empty", got)
		}
	})
}

func TestAgentRunTerminalStateIsStickyAfterCancel(t *testing.T) {
	r := NewAgentRunRegistry()
	run := r.New("explore")
	r.CancelAll()

	run.finishOK("late success")
	if run.statusValue() != RunCancelled {
		t.Fatalf("status after late finishOK = %s, want %s", run.statusValue(), RunCancelled)
	}
	if run.Result != "" {
		t.Fatalf("late finishOK should not set result, got %q", run.Result)
	}
}

func TestCancelOwnedUnknownID(t *testing.T) {
	r := NewAgentRunRegistry()
	r.New("explore")
	err := r.CancelOwned("agent-run-999", "build")
	if err == nil {
		t.Fatal("expected error for unknown task id")
	}
	if !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("error = %q, want 'unknown'", err)
	}
}

func TestCancelOwnedWrongDispatcher(t *testing.T) {
	r := NewAgentRunRegistry()
	run := r.New("explore")
	run.Dispatcher = "build"
	err := r.CancelOwned(run.ID, "attacker")
	if err == nil {
		t.Fatal("expected error for wrong dispatcher")
	}
	if !strings.Contains(err.Error(), "not owned by") {
		t.Fatalf("error = %q, want 'not owned by'", err)
	}
}

func TestCancelOwnedCorrectDispatcher(t *testing.T) {
	r := NewAgentRunRegistry()
	run := r.New("explore")
	run.Dispatcher = "build"
	cancelled := false
	run.Cancel = func() { cancelled = true }

	err := r.CancelOwned(run.ID, "build")
	if err != nil {
		t.Fatalf("CancelOwned err: %v", err)
	}
	if !cancelled {
		t.Fatal("CancelOwned did not invoke Cancel func")
	}
	if run.statusValue() != RunCancelled {
		t.Fatalf("status = %s, want %s", run.statusValue(), RunCancelled)
	}
	if run.Err != "cancelled" {
		t.Fatalf("Err = %q, want 'cancelled'", run.Err)
	}
}

// TestCancelOwnedForceReleasesSlotForHungRun reproduces the "cancel a run
// stuck inside a plain Tool.Execute call" scenario: Tool.Execute takes no
// context, so Cancel() closing stopCh does not make a hung tool call return,
// and the dispatch goroutine's own deferred releaseOwnSlot() never runs.
// CancelOwned must force-release the slot itself so a run queued behind the
// hung one can proceed immediately, instead of waiting forever for a
// goroutine that will never come back.
func TestCancelOwnedForceReleasesSlotForHungRun(t *testing.T) {
	r := NewAgentRunRegistry()
	r.SetMaxConcurrent(1)

	run1 := r.New("hung")
	run1.Dispatcher = "build"
	run1.Cancel = func() {} // stopCh close has no effect on the "hung" tool call
	release, err := r.AcquireForRun(run1, nil)
	if err != nil {
		t.Fatalf("run1 Acquire err: %v", err)
	}
	run1.Sub = &Agent{}
	run1.Sub.setOwnSlot(release)
	// Deliberately never call run1.Sub.releaseOwnSlot() here — this stands in
	// for the goroutine being permanently blocked inside Tool.Execute.

	run2 := r.New("queued")
	run2.markQueued()
	run2.Cancel = func() {}
	errCh := make(chan error, 1)
	go func() {
		_, aerr := r.AcquireForRun(run2, nil)
		errCh <- aerr
	}()
	waitForQueued(t, r)

	if cerr := r.CancelOwned(run1.ID, "build"); cerr != nil {
		t.Fatalf("CancelOwned err: %v", cerr)
	}

	select {
	case aerr := <-errCh:
		if aerr != nil {
			t.Fatalf("run2 AcquireForRun err = %v, want nil (slot should have been force-released)", aerr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("run2 stayed queued after run1 was cancelled — slot was not force-released")
	}
}

func TestCancelOwnedAlreadyDoneIsNoOp(t *testing.T) {
	r := NewAgentRunRegistry()
	run := r.New("explore")
	run.Dispatcher = "build"
	run.finishOK("already done")

	err := r.CancelOwned(run.ID, "build")
	if err != nil {
		t.Fatalf("CancelOwned err: %v", err)
	}
	if run.statusValue() != RunDone {
		t.Fatalf("status = %s, want %s", run.statusValue(), RunDone)
	}
	if run.Result != "already done" {
		t.Fatalf("result changed to %q", run.Result)
	}
}

func TestCancelAllMarksRunCancelled(t *testing.T) {
	r := NewAgentRunRegistry()
	run := r.New("explore")
	run.Sub = NewAgent(nil, nil, nil, nil)
	run.Cancel = func() {}
	r.CancelAll()

	if run.statusValue() != RunCancelled {
		t.Fatalf("CancelAll status = %s, want %s", run.statusValue(), RunCancelled)
	}
	if run.Err != "cancelled" {
		t.Fatalf("CancelAll Err = %q, want 'cancelled'", run.Err)
	}
}

func TestAgentRunRegistryAcquireUnlimited(t *testing.T) {
	r := NewAgentRunRegistry()
	release, err := r.Acquire(nil)
	if err != nil {
		t.Fatalf("Acquire err: %v", err)
	}
	release()
	if r.QueuedCount() != 0 {
		t.Fatalf("QueuedCount = %d, want 0", r.QueuedCount())
	}
}

func TestAgentRunRegistryAcquireUnlimitedHonorsCancellation(t *testing.T) {
	r := NewAgentRunRegistry()
	stopCh := make(chan struct{})
	close(stopCh)
	if _, err := r.Acquire(stopCh); err != ErrAgentQueueCancelled {
		t.Fatalf("unlimited Acquire err = %v, want ErrAgentQueueCancelled", err)
	}
}

func TestAgentRunRegistryAcquireQueues(t *testing.T) {
	r := NewAgentRunRegistry()
	r.SetMaxConcurrent(1)

	release1, err := r.Acquire(nil)
	if err != nil {
		t.Fatalf("first Acquire err: %v", err)
	}

	acquired := make(chan struct{})
	go func() {
		release2, err := r.Acquire(nil)
		if err != nil {
			t.Errorf("second Acquire err: %v", err)
			return
		}
		close(acquired)
		release2()
	}()

	// Give the second Acquire time to start blocking behind the held slot.
	for i := 0; i < 100 && r.QueuedCount() == 0; i++ {
		time.Sleep(time.Millisecond)
	}
	if r.QueuedCount() != 1 {
		t.Fatalf("QueuedCount while blocked = %d, want 1", r.QueuedCount())
	}
	select {
	case <-acquired:
		t.Fatal("second Acquire returned before the first slot was released")
	default:
	}

	release1()

	select {
	case <-acquired:
	case <-time.After(2 * time.Second):
		t.Fatal("second Acquire never unblocked after release")
	}
}

func TestAgentRunRegistryAcquireCancelledByStopCh(t *testing.T) {
	r := NewAgentRunRegistry()
	r.SetMaxConcurrent(1)
	release1, err := r.Acquire(nil)
	if err != nil {
		t.Fatalf("first Acquire err: %v", err)
	}
	defer release1()

	stopCh := make(chan struct{})
	close(stopCh)
	_, err = r.Acquire(stopCh)
	if err != ErrAgentQueueCancelled {
		t.Fatalf("Acquire err = %v, want ErrAgentQueueCancelled", err)
	}
}

func TestAgentRunRegistryResizingPreservesHoldersAndWakesWaiters(t *testing.T) {
	r := NewAgentRunRegistry()
	r.SetMaxConcurrent(1)
	release1, err := r.Acquire(nil)
	if err != nil {
		t.Fatalf("first Acquire err: %v", err)
	}

	acquired := make(chan struct{})
	go func() {
		release, err := r.Acquire(nil)
		if err != nil {
			t.Errorf("waiting Acquire err: %v", err)
			return
		}
		close(acquired)
		release()
	}()
	waitForQueued(t, r)

	// Increasing the limit must wake the waiter even though the first holder
	// was acquired before the resize.
	r.SetMaxConcurrent(2)
	select {
	case <-acquired:
	case <-time.After(2 * time.Second):
		t.Fatal("waiter was stranded after increasing the limit")
	}
	release1()
}

func TestAgentRunRegistryResizingAccountsExistingHolders(t *testing.T) {
	r := NewAgentRunRegistry()
	r.SetMaxConcurrent(2)
	release1, err := r.Acquire(nil)
	if err != nil {
		t.Fatalf("first Acquire err: %v", err)
	}
	release2, err := r.Acquire(nil)
	if err != nil {
		t.Fatalf("second Acquire err: %v", err)
	}
	defer release2()

	r.SetMaxConcurrent(1)
	acquired := make(chan struct{})
	go func() {
		release, err := r.Acquire(nil)
		if err != nil {
			t.Errorf("waiting Acquire err: %v", err)
			return
		}
		close(acquired)
		release()
	}()
	waitForQueued(t, r)
	select {
	case <-acquired:
		t.Fatal("waiter exceeded the reduced limit while two holders remained")
	default:
	}
	release1()
	select {
	case <-acquired:
		t.Fatal("waiter exceeded the reduced limit while one existing holder remained")
	case <-time.After(50 * time.Millisecond):
	}
	release2()
	select {
	case <-acquired:
	case <-time.After(2 * time.Second):
		t.Fatal("waiter was stranded after existing holders released")
	}
}

func TestAgentRunRegistryUnlimitedToFiniteCountsExistingHolders(t *testing.T) {
	r := NewAgentRunRegistry()
	release1, err := r.Acquire(nil)
	if err != nil {
		t.Fatalf("first Acquire err: %v", err)
	}
	release2, err := r.Acquire(nil)
	if err != nil {
		t.Fatalf("second Acquire err: %v", err)
	}
	r.SetMaxConcurrent(1)

	acquired := make(chan struct{})
	go func() {
		release, err := r.Acquire(nil)
		if err != nil {
			t.Errorf("waiting Acquire err: %v", err)
			return
		}
		close(acquired)
		release()
	}()
	waitForQueued(t, r)
	release1()
	select {
	case <-acquired:
		t.Fatal("waiter ignored the second existing holder after unlimited-to-finite resize")
	case <-time.After(50 * time.Millisecond):
	}
	release2()
	select {
	case <-acquired:
	case <-time.After(2 * time.Second):
		t.Fatal("waiter was stranded after unlimited-to-finite resize")
	}
}

func TestAgentRunRegistryCancelAllCancelsQueued(t *testing.T) {
	r := NewAgentRunRegistry()
	r.SetMaxConcurrent(1)
	run := r.New("queued")
	run.markQueued()
	cancelled := false
	run.Cancel = func() { cancelled = true }
	r.CancelAll()
	if !cancelled {
		t.Fatal("CancelAll did not invoke queued run cancel")
	}
	if got := run.CurrentStatus(); got != RunCancelled {
		t.Fatalf("queued status = %s, want %s", got, RunCancelled)
	}
}

func TestAgentRunRegistryAcquireForRunWakesOnOwnCancellation(t *testing.T) {
	r := NewAgentRunRegistry()
	r.SetMaxConcurrent(1)
	release1, err := r.Acquire(nil)
	if err != nil {
		t.Fatalf("first Acquire err: %v", err)
	}
	defer release1()

	run := r.New("queued")
	run.markQueued()
	run.Cancel = func() {}

	errCh := make(chan error, 1)
	go func() {
		_, err := r.AcquireForRun(run, nil)
		errCh <- err
	}()
	waitForQueued(t, r)

	// Cancelling this specific queued run must wake its own AcquireForRun
	// immediately, without waiting for the unrelated held slot to free up.
	if cerr := r.CancelOwned(run.ID, run.Dispatcher); cerr != nil {
		t.Fatalf("CancelOwned err: %v", cerr)
	}

	select {
	case err := <-errCh:
		if err != ErrAgentQueueCancelled {
			t.Fatalf("AcquireForRun err = %v, want ErrAgentQueueCancelled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("AcquireForRun stayed blocked after its own run was cancelled")
	}
}

func TestAgentRunRegistryDoesNotPruneQueuedRuns(t *testing.T) {
	r := NewAgentRunRegistry()
	queued := r.New("queued")
	queued.markQueued()
	for i := 0; i < 31; i++ {
		run := r.New("done")
		run.finishOK("done")
	}
	if removed := r.PruneCompleted(30); removed != 1 {
		t.Fatalf("PruneCompleted removed %d runs, want 1", removed)
	}
	if _, ok := r.Get(queued.ID); !ok {
		t.Fatal("queued run was pruned while still active")
	}
}

func waitForQueued(t *testing.T, r *AgentRunRegistry) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for r.QueuedCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if r.QueuedCount() == 0 {
		t.Fatal("waiter did not enter the limiter queue")
	}
}

func TestCancelOwnedEmptyDispatcherOnRun(t *testing.T) {
	r := NewAgentRunRegistry()
	run := r.New("explore")
	// Dispatcher is empty string -- only a caller with empty dispatcher can cancel.
	run.Dispatcher = ""
	err := r.CancelOwned(run.ID, "")
	if err != nil {
		t.Fatalf("CancelOwned err: %v", err)
	}
	if run.statusValue() != RunCancelled {
		t.Fatalf("status = %s, want %s", run.statusValue(), RunCancelled)
	}
}

func TestBeginResumeFromCancelled(t *testing.T) {
	r := NewAgentRunRegistry()
	run := r.New("explore")
	run.tryFinishCancelled()
	if run.statusValue() != RunCancelled {
		t.Fatalf("setup: status = %s, want cancelled", run.statusValue())
	}

	if ok := run.beginResume(); !ok {
		t.Fatal("beginResume() = false, want true for a cancelled run")
	}
	if run.statusValue() != RunRunning {
		t.Fatalf("status = %s, want running", run.statusValue())
	}
	if !run.EndedAt.IsZero() {
		t.Fatalf("EndedAt = %v, want zero", run.EndedAt)
	}
}

func TestBeginResumeFromDone(t *testing.T) {
	r := NewAgentRunRegistry()
	run := r.New("explore")
	run.finishOK("original result")
	if run.statusValue() != RunDone {
		t.Fatalf("setup: status = %s, want done", run.statusValue())
	}

	if ok := run.beginResume(); !ok {
		t.Fatal("beginResume() = false, want true for a done run")
	}
	if run.statusValue() != RunRunning {
		t.Fatalf("status = %s, want running", run.statusValue())
	}
}

func TestBeginResumeRejectsRunning(t *testing.T) {
	r := NewAgentRunRegistry()
	run := r.New("explore") // New() leaves the run in RunRunning

	if ok := run.beginResume(); ok {
		t.Fatal("beginResume() = true, want false for a running run")
	}
	if run.statusValue() != RunRunning {
		t.Fatalf("status changed to %s, want unchanged running", run.statusValue())
	}
}

func TestBeginResumeRejectsFailed(t *testing.T) {
	r := NewAgentRunRegistry()
	run := r.New("explore")
	run.finishErr("boom")

	if ok := run.beginResume(); ok {
		t.Fatal("beginResume() = true, want false for a failed run")
	}
	if run.statusValue() != RunFailed {
		t.Fatalf("status changed to %s, want unchanged failed", run.statusValue())
	}
}

func TestBeginResumeRejectsQueued(t *testing.T) {
	r := NewAgentRunRegistry()
	run := r.New("explore")
	run.markQueued()
	if run.statusValue() != RunQueued {
		t.Fatalf("setup: status = %s, want queued", run.statusValue())
	}

	if ok := run.beginResume(); ok {
		t.Fatal("beginResume() = true, want false for a queued run")
	}
}

// TestAwaitTeardownBlocksUntilMarkTeardownDone proves the resume race-guard
// primitive actually synchronizes: awaitTeardown must not return before
// markTeardownDone is called on the same run, and must return promptly once
// it is. Run with -race: this exercises exactly the concurrent access
// pattern (concurrent read of run.teardownDone / concurrent close) that
// RearmMaintenance's real caller (executeResume) depends on being race-free.
func TestAwaitTeardownBlocksUntilMarkTeardownDone(t *testing.T) {
	r := NewAgentRunRegistry()
	run := r.New("explore")

	release := make(chan struct{})
	go func() {
		<-release
		run.markTeardownDone()
	}()

	waitReturned := make(chan struct{})
	go func() {
		run.awaitTeardown(2 * time.Second)
		close(waitReturned)
	}()

	select {
	case <-waitReturned:
		t.Fatal("awaitTeardown returned before markTeardownDone was called")
	case <-time.After(50 * time.Millisecond):
	}

	close(release)
	select {
	case <-waitReturned:
	case <-time.After(2 * time.Second):
		t.Fatal("awaitTeardown did not return after markTeardownDone")
	}
}

// TestAwaitTeardownTimesOutWithoutSignal proves awaitTeardown does not hang
// forever if markTeardownDone is never called (e.g. a dispatch whose
// shutdownTransient somehow never completes) — it must give up after the
// timeout rather than deadlocking the resume path.
func TestAwaitTeardownTimesOutWithoutSignal(t *testing.T) {
	r := NewAgentRunRegistry()
	run := r.New("explore")

	start := time.Now()
	run.awaitTeardown(50 * time.Millisecond)
	if elapsed := time.Since(start); elapsed < 50*time.Millisecond {
		t.Fatalf("awaitTeardown returned early after %v, want >= 50ms", elapsed)
	}
}

// TestBeginResumeRearmsTeardownAndDoneChannels proves beginResume replaces
// both teardownDone and done with fresh, open channels for the new dispatch
// cycle — otherwise a caller that resumes twice would find markTeardownDone
// (from the second dispatch) closing an already-closed channel, and Done()
// would report the run as finished the instant it was resumed.
func TestBeginResumeRearmsTeardownAndDoneChannels(t *testing.T) {
	r := NewAgentRunRegistry()
	run := r.New("explore")
	run.finishOK("first result")
	run.markTeardownDone()

	oldDone := run.Done()
	select {
	case <-oldDone:
	default:
		t.Fatal("setup: original done channel should be closed after finishOK")
	}

	if ok := run.beginResume(); !ok {
		t.Fatal("beginResume() = false, want true")
	}

	newDone := run.Done()
	select {
	case <-newDone:
		t.Fatal("Done() still closed immediately after beginResume — not re-armed")
	default:
	}

	// The new teardownDone must also be open (not still closed from the
	// prior markTeardownDone call above) and must not panic when closed again.
	run.markTeardownDone()

	run.finishOK("second result")
	select {
	case <-newDone:
	default:
		t.Fatal("Done() (post-resume) did not close after the resumed run's finishOK")
	}
}

// TestAgentRunQueuedLifecycleRaceStress hammers the queued-run lifecycle —
// AcquireForRun admission, CancelOwned on queued runs, PruneCompleted, and
// live SetMaxConcurrent resizes — from many goroutines at once. It exists to
// be run under `go test -race`: it proves the limiter accounting, the
// queued-run registry entries, and the Done()-channel wakeups are all
// synchronized, not just functionally correct in the single-threaded tests.
func TestAgentRunQueuedLifecycleRaceStress(t *testing.T) {
	r := NewAgentRunRegistry()
	r.SetMaxConcurrent(2)

	const workers = 24
	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Dispatch workers: each creates a run, queues it, and acquires a slot
	// (or observes cancellation while queued), then finishes and releases.
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				run := r.New("stress")
				run.markQueued()
				release, err := r.AcquireForRun(run, nil)
				if err != nil {
					run.tryFinishCancelled()
					r.notifyDone(run)
					continue
				}
				if !run.beginExecution() {
					release()
					r.notifyDone(run)
					continue
				}
				// Simulate the sub-agent goroutine doing a little work while
				// holding the slot, then finishing like runBackgroundDispatch.
				time.Sleep(time.Millisecond)
				run.finishOK("ok")
				release()
				r.notifyDone(run)
			}
		}()
	}

	// Canceller: randomly cancel runs that are currently queued (only ones
	// this goroutine "owns" via the empty dispatcher match).
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			for _, run := range r.Snapshot() {
				if run.statusValue() == RunQueued {
					_ = r.CancelOwned(run.ID, "")
				}
			}
			time.Sleep(2 * time.Millisecond)
		}
	}()

	// Pruner: keep the registry bounded, exactly like the agent loop does.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			r.PruneCompleted(30)
			time.Sleep(2 * time.Millisecond)
		}
	}()

	// Resizer: churn the live limit between 1, 2, and 0 (unlimited), which
	// exercises setMax's broadcast wakeups against in-flight admissions.
	wg.Add(1)
	go func() {
		defer wg.Done()
		limits := []int{1, 2, 0}
		i := 0
		for {
			select {
			case <-stop:
				return
			default:
			}
			r.SetMaxConcurrent(limits[i%len(limits)])
			i++
			time.Sleep(3 * time.Millisecond)
		}
	}()

	time.Sleep(500 * time.Millisecond)
	close(stop)
	wg.Wait()

	// Accounting must be consistent after quiescence: no negative queued
	// count, and the limiter must admit new work (max may be 0 = unlimited
	// or 1/2; either way an Acquire must not hang forever).
	r.SetMaxConcurrent(2)
	release, err := r.Acquire(nil)
	if err != nil {
		t.Fatalf("Acquire after stress failed: %v", err)
	}
	release()
	if q := r.QueuedCount(); q < 0 {
		t.Fatalf("QueuedCount() = %d after stress, want >= 0", q)
	}
	for _, run := range r.Snapshot() {
		if run.CurrentStatus().IsActive() {
			t.Fatalf("run %s remains active after stress: %s", run.ID, run.CurrentStatus())
		}
	}
	r.PruneCompleted(0)
	if runs := r.Snapshot(); len(runs) != 0 {
		t.Fatalf("PruneCompleted(0) left %d terminal runs", len(runs))
	}
}
