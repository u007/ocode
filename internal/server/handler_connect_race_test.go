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
	// The cancel runs off the sweeping goroutine (it can wait on a listener).
	deadline := time.Now().Add(2 * time.Second)
	for !cancelled.Load() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
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
