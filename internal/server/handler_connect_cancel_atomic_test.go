package server

import (
	"context"
	"net/http"
	"testing"
	"time"
)

// A cancel that arrives as a flow starts committing must not be able to report
// "cancelled" while the credential it was meant to stop is written to disk.
//
// handleConnectFlowCancel used to check the state and act on it in four separate
// lock acquisitions (isTerminal, getState==committing, takeCancel,
// setState(cancelled)). beginCommit could win the gap between the committing
// check and the take: it had already been authorised, so it went on to auth.Set,
// while the handler — having passed its check — replied state:cancelled. The
// user was told the connect was cancelled and found themselves connected
// anyway, which is the one outcome this endpoint's own comment promises never to
// produce.

// A commit that becomes due while a cancel is mid-claim must be REFUSED, and the
// flow must end up cancelled — never both.
//
// This is tested through connectFlowCancelHook, which parks claimCancel between
// its state check and its take with the flow mutex HELD. A probabilistic two-
// goroutine race cannot test this: that window is a few instructions wide, so
// the old check-then-act version survived thousands of racing iterations without
// ever landing inside it. Parking the claim there makes the interleaving exact.
//
// The old version released the mutex before taking the cancel func, so the
// beginCommit below completed while the claim was parked — the flow would then be
// authorised to call auth.Set while the handler went on to report "cancelled".
func TestConnectFlowCommitIsRefusedWhileACancelIsMidClaim(t *testing.T) {
	f := newConnectFlow("anthropic", "oauth_max", connectFlowPasteCode, connectFlowRunning)

	inClaim := make(chan struct{})
	release := make(chan struct{})
	connectFlowCancelHook = func() {
		close(inClaim)
		<-release
	}
	t.Cleanup(func() { connectFlowCancelHook = nil })

	type cancelResult struct {
		outcome cancelOutcome
	}
	claimed := make(chan cancelResult, 1)
	go func() {
		_, outcome := f.claimCancel()
		claimed <- cancelResult{outcome}
	}()

	<-inClaim // the claim is now parked mid-flight, holding f.mu

	commitResult := make(chan bool, 1)
	go func() { commitResult <- f.beginCommit() }()

	// The commit must not be able to proceed: it needs the same mutex the parked
	// claim is holding. A short bound is unavoidable here — the assertion is
	// that it has NOT completed yet, not that it never will.
	select {
	case granted := <-commitResult:
		close(release)
		<-claimed
		t.Fatalf("beginCommit succeeded (%t) while a cancel was mid-claim — the credential "+
			"would be written for a flow reported as cancelled", granted)
	case <-time.After(150 * time.Millisecond):
	}

	close(release)
	if got := <-claimed; got.outcome != cancelClaimed {
		t.Fatalf("claim outcome = %v, want claimed", got.outcome)
	}
	if granted := <-commitResult; granted {
		t.Fatal("beginCommit was granted after the cancel already claimed the flow")
	}
	if state := f.getState(); state != connectFlowCancelled {
		t.Fatalf("state = %q, want cancelled", state)
	}
}

// A flow in committing has already been authorised to write its credential, so a
// cancel must be refused with 409 rather than acknowledged. This is the
// behaviour the atomic claim preserves for the sequential case.
func TestConnectFlowCancelRefusedWhileCommitting(t *testing.T) {
	h := NewHandler()
	f := newConnectFlow("anthropic", "oauth_max", connectFlowPasteCode, connectFlowRunning)
	connectFlows.add(f)

	if !f.beginCommit() {
		t.Fatal("beginCommit refused from running")
	}

	resp, code := connectDo(t, h.handleConnectFlowCancel, "DELETE", "/flows/"+f.id,
		map[string]string{"flowId": f.id}, nil)
	if code != http.StatusConflict {
		t.Fatalf("cancel during commit = %d %v, want 409", code, resp)
	}
	// The state must be untouched: a refused cancel that still flipped the state
	// would leave the committer writing a credential into a cancelled flow.
	if got := f.getState(); got != connectFlowCommitting {
		t.Fatalf("state after a refused cancel = %q, want %q", got, connectFlowCommitting)
	}
}

// A terminal flow reports its terminal state and stays there — cancel is a
// no-op, not a rewrite.
func TestConnectFlowCancelOnTerminalFlowReportsItsState(t *testing.T) {
	for _, terminal := range []connectFlowState{connectFlowComplete, connectFlowFailed, connectFlowCancelled} {
		t.Run(string(terminal), func(t *testing.T) {
			h := NewHandler()
			f := newConnectFlow("anthropic", "oauth_max", connectFlowPasteCode, connectFlowRunning)
			f.setState(terminal)
			connectFlows.add(f)

			resp, code := connectDo(t, h.handleConnectFlowCancel, "DELETE", "/flows/"+f.id,
				map[string]string{"flowId": f.id}, nil)
			if code != http.StatusOK {
				t.Fatalf("cancel on %s = %d %v, want 200", terminal, code, resp)
			}
			if got, _ := resp["state"].(string); got != string(terminal) {
				t.Fatalf("state = %q, want %q", got, terminal)
			}
		})
	}
}

// A claimed cancel still runs the flow's cancel func exactly once, so the
// in-flight network exchange actually stops.
func TestConnectFlowCancelRunsTheFlowCancelFunc(t *testing.T) {
	h := NewHandler()
	f := newConnectFlow("google", "token", connectFlowLocalCallback, connectFlowWaitingBrowser)
	ctx, cancel := context.WithCancel(context.Background())
	f.cancel = cancel
	connectFlows.add(f)

	resp, code := connectDo(t, h.handleConnectFlowCancel, "DELETE", "/flows/"+f.id,
		map[string]string{"flowId": f.id}, nil)
	if code != http.StatusOK {
		t.Fatalf("cancel = %d %v", code, resp)
	}
	select {
	case <-ctx.Done():
	default:
		t.Fatal("the flow's context was not cancelled")
	}
	if got := f.getState(); got != connectFlowCancelled {
		t.Fatalf("state = %q, want cancelled", got)
	}
	// A successful cancel also drops the flow from the registry, so a repeat
	// cancel is a 404 rather than a second invocation of the (already nil'd)
	// cancel func.
	if _, code := connectDo(t, h.handleConnectFlowCancel, "DELETE", "/flows/"+f.id,
		map[string]string{"flowId": f.id}, nil); code != http.StatusNotFound {
		t.Fatalf("second cancel = %d, want 404 (the flow is dropped once cancelled)", code)
	}
}
