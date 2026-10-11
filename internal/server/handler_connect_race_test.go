package server

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/u007/ocode/internal/auth"
)

// A cancelled flow must never store the credential it was cancelled to prevent.
// The Anthropic, Google and manual ChatGPT exchanges have no cancel func, so
// cancelling cannot stop them; the guarantee has to come from the completion
// path discarding a result that arrives after the cancel.
func TestConnectFlowFinishAfterCancelStoresNothing(t *testing.T) {
	preserveConnectCredential(t, "anthropic")
	_ = auth.Remove("anthropic")

	f := newConnectFlow("anthropic", "oauth_max", connectFlowPasteCode, connectFlowRunning)
	if !f.markCancelled() {
		t.Fatal("markCancelled() = false on a running flow, want true")
	}
	f.finish("anthropic", auth.Credential{Kind: auth.KindOAuth, AccessToken: "late-token"}, nil)

	if got := f.getState(); got != connectFlowCancelled {
		t.Errorf("state after a late result = %s, want cancelled (a late result must not overwrite the cancel)", got)
	}
	if cred, ok := auth.Get("anthropic"); ok {
		t.Errorf("a cancelled flow stored a credential: %+v", cred)
	}

	// A late FAILURE must not overwrite the cancelled state either.
	f.finish("anthropic", auth.Credential{}, fmt.Errorf("boom"))
	if got := f.getState(); got != connectFlowCancelled {
		t.Errorf("state after a late failure = %s, want cancelled", got)
	}
}

// Cancelling a flow that already finished leaves its outcome alone.
func TestConnectFlowCancelAfterCompleteKeepsOutcome(t *testing.T) {
	preserveConnectCredential(t, "anthropic")
	f := newConnectFlow("anthropic", "oauth_max", connectFlowPasteCode, connectFlowRunning)
	f.finish("anthropic", auth.Credential{Kind: auth.KindOAuth, AccessToken: "tok", Account: "me"}, nil)
	if f.markCancelled() {
		t.Error("markCancelled() = true on a completed flow, want false")
	}
	if got := f.getState(); got != connectFlowComplete {
		t.Errorf("state = %s, want complete", got)
	}
}

// Through the handlers: cancel while the exchange is in flight, then let the
// exchange succeed. Nothing may be stored.
func TestConnectCancelDuringExchangeStoresNothing(t *testing.T) {
	h := NewHandler()
	preserveConnectCredential(t, "anthropic")
	_ = auth.Remove("anthropic")
	flowID := startTestAnthropicFlow(t, h)
	f := connectFlows.get(flowID)

	started := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan struct{})
	stubConnectSeam(t, &anthropicExchangeFn, func(code, state, verifier string) (auth.Credential, error) {
		close(started)
		<-release
		defer close(finished)
		return auth.Credential{Kind: auth.KindOAuth, AccessToken: "late-token"}, nil
	})

	if _, code := connectDo(t, h.handleConnectFlowInput, "POST", "/api/auth/connect/flows/"+flowID+"/input",
		map[string]string{"flowId": flowID},
		map[string]string{"code": "https://claude.ai/oauth/callback?code=c&state=st-1"}); code != http.StatusOK {
		t.Fatalf("input: %d", code)
	}
	<-started
	if resp, code := connectDo(t, h.handleConnectFlowCancel, "DELETE", "/api/auth/connect/flows/"+flowID,
		map[string]string{"flowId": flowID}, nil); code != http.StatusOK || resp["state"] != "cancelled" {
		t.Fatalf("cancel: %d %v", code, resp)
	}
	close(release)
	<-finished
	// The exchange goroutine records its result right after the stub returns;
	// finish() takes the flow lock, so wait for it by taking the lock too.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		f.mu.Lock()
		f.mu.Unlock() //nolint:staticcheck // barrier only
		time.Sleep(10 * time.Millisecond)
	}
	if got := f.getState(); got != connectFlowCancelled {
		t.Errorf("state = %s, want cancelled", got)
	}
	if cred, ok := auth.Get("anthropic"); ok {
		t.Errorf("credential stored after cancel: %+v", cred)
	}
}

// A double submit must start exactly one exchange.
func TestConnectFlowInputDoubleSubmitRunsOneExchange(t *testing.T) {
	h := NewHandler()
	preserveConnectCredential(t, "anthropic")
	flowID := startTestAnthropicFlow(t, h)

	var calls atomic.Int32
	release := make(chan struct{})
	stubConnectSeam(t, &anthropicExchangeFn, func(code, state, verifier string) (auth.Credential, error) {
		calls.Add(1)
		<-release
		return auth.Credential{Kind: auth.KindOAuth, AccessToken: "tok"}, nil
	})

	const submitters = 12
	var wg sync.WaitGroup
	var ok200 atomic.Int32
	start := make(chan struct{})
	for i := 0; i < submitters; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, code := connectDo(t, h.handleConnectFlowInput, "POST", "/api/auth/connect/flows/"+flowID+"/input",
				map[string]string{"flowId": flowID},
				map[string]string{"code": "https://claude.ai/oauth/callback?code=c&state=st-1"})
			if code == http.StatusOK {
				ok200.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()
	close(release)
	awaitConnectFlow(t, h, flowID)

	if got := ok200.Load(); got != 1 {
		t.Errorf("%d submissions were accepted, want exactly 1", got)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("exchange ran %d times, want 1", got)
	}
}

func TestConnectFlowClaimInputIsExclusive(t *testing.T) {
	f := newConnectFlow("anthropic", "oauth_max", connectFlowPasteCode, connectFlowWaitingInput)
	if !f.claimInput(nil) {
		t.Fatal("first claim failed")
	}
	if f.claimInput(nil) {
		t.Error("second claim succeeded, want it refused")
	}
}

// An expired flow still owning background work (the ChatGPT auto flow's
// loopback listener) must be cancelled when the registry drops it.
func TestConnectFlowExpiryCancelsBackgroundWork(t *testing.T) {
	var cancelled atomic.Bool
	f := newConnectFlow("openai", "oauth", connectFlowLocalCallback, connectFlowWaitingBrowser)
	f.cancel = func() { cancelled.Store(true) }
	f.updatedAt = time.Now().Add(-2 * connectFlowTTL)
	connectFlows.add(f)
	t.Cleanup(func() { connectFlows.remove(f.id) })

	// Any registry access sweeps expired flows.
	if got := connectFlows.get("nonexistent"); got != nil {
		t.Fatalf("unexpected flow %v", got)
	}
	if !cancelled.Load() {
		t.Error("expired flow was dropped without cancelling its background work (listener leak)")
	}
	if connectFlows.get(f.id) != nil {
		t.Error("expired flow is still registered")
	}
}

// Starting a new auto ChatGPT flow must release a stale one's loopback port
// instead of failing with "address already in use".
func TestConnectOpenAIAutoStartSupersedesAbandonedFlow(t *testing.T) {
	h := NewHandler()
	preserveConnectCredential(t, "openai")

	var held atomic.Bool
	stubConnectSeam(t, &openaiStartFn, func(ctx context.Context) (string, func() (auth.Credential, error), error) {
		if !held.CompareAndSwap(false, true) {
			return "", nil, fmt.Errorf("bind localhost:1455: address already in use")
		}
		return "https://auth.openai.com/oauth/authorize?auto=1", func() (auth.Credential, error) {
			<-ctx.Done()
			held.Store(false) // the real finish() shuts its listener down here
			return auth.Credential{}, ctx.Err()
		}, nil
	})

	start := func() (map[string]any, int) {
		return connectDo(t, h.handleConnectOAuthStart, "POST", "/api/auth/connect/openai/oauth/start",
			map[string]string{"provider": "openai"}, map[string]string{"method": "oauth"})
	}
	first, code := start()
	if code != http.StatusOK {
		t.Fatalf("first start: %d %v", code, first)
	}
	firstID, _ := first["flowId"].(string)

	second, code := start()
	if code != http.StatusOK {
		t.Fatalf("retry after an abandoned flow: %d %v, want the old flow superseded", code, second)
	}
	if connectFlows.get(firstID) != nil {
		t.Error("the abandoned flow is still registered after being superseded")
	}
	secondID, _ := second["flowId"].(string)
	t.Cleanup(func() {
		connectDo(t, h.handleConnectFlowCancel, "DELETE", "/api/auth/connect/flows/"+secondID, map[string]string{"flowId": secondID}, nil)
	})
}
