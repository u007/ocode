package server

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/u007/ocode/internal/agent"
)

// bridgeAskResult runs a bridge asker on its own goroutine so a test can cancel
// the ctx while the park is in flight.
func bridgeAskResult(asker agent.BridgePermAsker, ctx context.Context, req agent.PermissionRequest) <-chan agent.PermissionResponse {
	out := make(chan agent.PermissionResponse, 1)
	go func() { out <- asker(ctx, req) }()
	return out
}

// TestBridgePermAskCtxCancelAutoDeniesAndRemovesDialog: a claude-code bridge ask
// parks like a sub-agent ask (visible to pending_asks), and ending its ctx — the
// CLI request ending — returns Deny promptly, drops the entry and 404s any later
// resolve for that request_id.
func TestBridgePermAskCtxCancelAutoDeniesAndRemovesDialog(t *testing.T) {
	harn := newChildAskHarness(t, 0)
	h, as, sessionID := harn.h, harn.as, harn.sessionID
	sub := h.subscribeHeadless()
	defer h.unsubscribeHeadless(sub)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	asker := h.newServerBridgePermAsker(sessionID, as)
	result := bridgeAskResult(asker, ctx, childAskRequest())
	t.Cleanup(func() {
		as.childAsks.denyAll()
	})

	waitForRegistryLen(t, as, 1)
	requestID := as.childAsks.list()[0].RequestID

	live := h.livePendingAsks(sessionID)
	if live == nil || len(live.Permissions) != 1 || live.Permissions[0].RequestID != requestID {
		t.Fatalf("pending_asks = %+v, want the bridge ask %q", live, requestID)
	}

	cancel()
	select {
	case resp := <-result:
		if resp.Level != agent.PermissionDeny {
			t.Fatalf("bridge received %v, want Deny when its ctx ends", resp.Level)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ctx cancel did not unblock the bridge park")
	}

	waitForRegistryLen(t, as, 0)
	if live := h.livePendingAsks(sessionID); live != nil && len(live.Permissions) != 0 {
		t.Fatalf("pending_asks after cancel = %+v, want none", live)
	}

	// The dialog must be dismissed by the frame, not left for a click that can
	// only fail.
	ev := waitForSSEEvent(t, sub, "permission_resolved", 3*time.Second)
	var payload struct {
		RequestID string `json:"request_id"`
	}
	eventPayload(t, ev, &payload)
	if payload.RequestID != requestID {
		t.Fatalf("permission_resolved request_id = %q, want %q", payload.RequestID, requestID)
	}

	rec := resolvePost(t, h, sessionID, requestID, PermDecisionAllow)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("late resolve status = %d (%s), want 404", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "no pending permission found for request_id") {
		t.Fatalf("late resolve body = %s, want the existing 404 message", rec.Body.String())
	}
}

// TestBridgePermAskAlreadyCancelledNeverParks: a ctx that is already gone must
// deny without surfacing a dialog nobody can answer.
func TestBridgePermAskAlreadyCancelledNeverParks(t *testing.T) {
	harn := newChildAskHarness(t, 0)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	got := harn.h.newServerBridgePermAsker(harn.sessionID, harn.as)(ctx, childAskRequest())
	if got.Level != agent.PermissionDeny {
		t.Fatalf("level = %v, want Deny for a cancelled ctx", got.Level)
	}
	if n := len(harn.as.childAsks.list()); n != 0 {
		t.Fatalf("registry has %d entries after a cancelled ask, want 0", n)
	}
}

// TestBridgePermAskResolveStillDelivers: the human answer path is unchanged for a
// bridge ask — a resolve while the ctx is live hands the decision back.
func TestBridgePermAskResolveStillDelivers(t *testing.T) {
	harn := newChildAskHarness(t, 0)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := bridgeAskResult(harn.h.newServerBridgePermAsker(harn.sessionID, harn.as), ctx, childAskRequest())
	t.Cleanup(func() { harn.as.childAsks.denyAll() })

	waitForRegistryLen(t, harn.as, 1)
	requestID := harn.as.childAsks.list()[0].RequestID
	if rec := harn.resolve(requestID, PermDecisionAllow); rec.Code != http.StatusOK {
		t.Fatalf("resolve status = %d (%s), want 200", rec.Code, rec.Body.String())
	}
	select {
	case resp := <-result:
		if resp.Level != agent.PermissionAllow {
			t.Fatalf("bridge received %v, want Allow", resp.Level)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("resolve did not unblock the bridge park")
	}
}
