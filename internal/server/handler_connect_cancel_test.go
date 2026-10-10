package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/u007/ocode/internal/auth"
)

// A cancelled connect flow must NOT persist its credential.
//
// handleConnectFlowCancel could only cancel a flow through its cancel func, and
// the Anthropic paste-code, Google token and manual-OpenAI exchanges had no
// cancel func at all — so cancelling one of those flows was a no-op on the
// network work, and completeConnectFlow then saved the credential anyway. A user
// who walked away from a connect could still find it connected minutes later.
//
// These three go through the CONTEXT gate: the cancel endpoint cancels the
// flow's context, so ctx.Err() is non-nil by the time the exchange returns.
// TestCompleteConnectFlowRefusesCancelledFlowWithLiveContext pins the other,
// independent gate.

func TestConnectFlowCancelDiscardsAnthropicCredential(t *testing.T) {
	h := NewHandler()
	preserveConnectCredential(t, "anthropic")
	_ = auth.Remove("anthropic")

	release := make(chan struct{})
	stubConnectSeam(t, &anthropicAuthorizeFn, func(mode string) (auth.AnthropicFlow, error) {
		return auth.AnthropicFlow{
			URL:      "https://console.anthropic.com/oauth/authorize",
			State:    "st-anthropic",
			Verifier: "ver-anthropic",
		}, nil
	})
	// The exchange blocks so the cancel lands while it is still in flight.
	exchanged := make(chan struct{})
	stubConnectSeam(t, &anthropicExchangeFn, func(code, state, verifier string) (auth.Credential, error) {
		close(exchanged)
		<-release
		return auth.Credential{Kind: auth.KindOAuth, AccessToken: "tok-should-not-persist"}, nil
	})

	resp, code := connectDo(t, h.handleConnectOAuthStart, "POST", "/api/auth/connect/anthropic/oauth/start",
		map[string]string{"provider": "anthropic"},
		map[string]string{"method": "oauth_max"})
	if code != http.StatusOK {
		t.Fatalf("start anthropic flow: %d %v", code, resp)
	}
	flowID, _ := resp["flowId"].(string)
	if flowID == "" {
		t.Fatalf("no flowId in %v", resp)
	}

	_, code = connectDo(t, h.handleConnectFlowInput, "POST", "/flows/"+flowID+"/input",
		map[string]string{"flowId": flowID},
		map[string]string{"code": "http://localhost:1455/callback?code=abc&state=st-anthropic"})
	if code != http.StatusOK {
		t.Fatalf("submit anthropic code: %d %v", code, resp)
	}
	select {
	case <-exchanged:
	case <-time.After(5 * time.Second):
		t.Fatal("anthropic exchange never started")
	}

	// Cancel while the exchange is still blocked.
	cancelResp, code := connectDo(t, h.handleConnectFlowCancel, "DELETE", "/flows/"+flowID,
		map[string]string{"flowId": flowID}, nil)
	if code != http.StatusOK {
		t.Fatalf("cancel: %d %v", code, cancelResp)
	}

	// Let the exchange finish. Its credential must be dropped.
	close(release)
	waitForNoCredential(t, "anthropic", "cancelled flow still persisted its credential")

	if got, ok := auth.Get("anthropic"); ok {
		t.Fatalf("cancelled Anthropic flow persisted a credential: %+v", got)
	}
}

func TestConnectFlowCancelDiscardsManualOpenAICredential(t *testing.T) {
	h := NewHandler()
	preserveConnectCredential(t, "openai")
	_ = auth.Remove("openai")

	release := make(chan struct{})
	stubConnectSeam(t, &openaiManualStartFn, func() (auth.OpenAIManualFlow, error) {
		return auth.OpenAIManualFlow{
			AuthURL:  "https://auth.openai.com/oauth/authorize?manual=1",
			State:    "st-openai",
			Verifier: "ver-openai",
		}, nil
	})
	exchanged := make(chan struct{})
	stubConnectSeam(t, &openaiManualExchangeFn, func(f auth.OpenAIManualFlow, pasted string) (auth.Credential, error) {
		close(exchanged)
		<-release
		return auth.Credential{Kind: auth.KindOAuth, AccessToken: "tok-openai-should-not-persist"}, nil
	})

	resp, code := connectDo(t, h.handleConnectOAuthStart, "POST", "/api/auth/connect/openai/oauth/start",
		map[string]string{"provider": "openai"},
		map[string]string{"method": "oauth", "mode": "manual"})
	if code != http.StatusOK {
		t.Fatalf("start openai manual flow: %d %v", code, resp)
	}
	flowID, _ := resp["flowId"].(string)
	if flowID == "" {
		t.Fatalf("no flowId in %v", resp)
	}

	_, code = connectDo(t, h.handleConnectFlowInput, "POST", "/flows/"+flowID+"/input",
		map[string]string{"flowId": flowID},
		map[string]string{"code": "http://localhost:1455/callback?code=abc&state=st-openai"})
	if code != http.StatusOK {
		t.Fatalf("submit openai paste: %d", code)
	}
	select {
	case <-exchanged:
	case <-time.After(5 * time.Second):
		t.Fatal("openai manual exchange never started")
	}

	if _, code := connectDo(t, h.handleConnectFlowCancel, "DELETE", "/flows/"+flowID,
		map[string]string{"flowId": flowID}, nil); code != http.StatusOK {
		t.Fatalf("cancel: %d", code)
	}

	close(release)
	waitForNoCredential(t, "openai", "cancelled manual-OpenAI flow still persisted its credential")
	if got, ok := auth.Get("openai"); ok {
		t.Fatalf("cancelled manual-OpenAI flow persisted a credential: %+v", got)
	}
}

func TestConnectFlowCancelDiscardsGoogleCredential(t *testing.T) {
	h := NewHandler()
	preserveConnectCredential(t, "google")
	_ = auth.Remove("google")

	release := make(chan struct{})
	stubConnectSeam(t, &googleStartFn, func() (string, func() (string, error), error) {
		return "https://accounts.google.com/o/oauth2/auth", func() (string, error) {
			<-release
			return "tok-google-should-not-persist", nil
		}, nil
	})

	resp, code := connectDo(t, h.handleConnectOAuthStart, "POST", "/api/auth/connect/google/oauth/start",
		map[string]string{"provider": "google"},
		map[string]string{"method": "oauth"})
	if code != http.StatusOK {
		t.Fatalf("start google flow: %d %v", code, resp)
	}
	flowID, _ := resp["flowId"].(string)
	if flowID == "" {
		t.Fatalf("no flowId in %v", resp)
	}

	// The Google flow never had a cancel func at all, so cancel was a no-op.
	if _, code := connectDo(t, h.handleConnectFlowCancel, "DELETE", "/flows/"+flowID,
		map[string]string{"flowId": flowID}, nil); code != http.StatusOK {
		t.Fatalf("cancel: %d", code)
	}

	close(release)
	waitForNoCredential(t, "google", "cancelled Google flow still persisted its credential")
	if got, ok := auth.Get("google"); ok {
		t.Fatalf("cancelled Google flow persisted a credential: %+v", got)
	}
}

// waitForNoCredential polls until the provider has no credential, so a test that
// releases a blocked exchange does not race the goroutine that saves it.
func waitForNoCredential(t *testing.T, providerID, msg string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := auth.Get(providerID); !ok {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Error(msg)
}

// Two concurrent POSTs to the input endpoint used to BOTH pass the
// waiting_input check — it was a separate lock acquisition from the
// setState(running) that followed — and both started an exchange. beginInput
// now performs the transition as one compare-and-set, so exactly one wins.
func TestConnectFlowInputDoubleSubmitStartsOneExchange(t *testing.T) {
	h := NewHandler()
	preserveConnectCredential(t, "anthropic")
	_ = auth.Remove("anthropic")

	var calls atomic.Int32
	stubConnectSeam(t, &anthropicAuthorizeFn, func(mode string) (auth.AnthropicFlow, error) {
		return auth.AnthropicFlow{
			URL: "https://console.anthropic.com/oauth/authorize", State: "st-dup", Verifier: "ver-dup",
		}, nil
	})
	stubConnectSeam(t, &anthropicExchangeFn, func(code, state, verifier string) (auth.Credential, error) {
		calls.Add(1)
		return auth.Credential{}, fmt.Errorf("no network in test")
	})

	resp, code := connectDo(t, h.handleConnectOAuthStart, "POST", "/api/auth/connect/anthropic/oauth/start",
		map[string]string{"provider": "anthropic"},
		map[string]string{"method": "oauth_max"})
	if code != http.StatusOK {
		t.Fatalf("start flow: %d %v", code, resp)
	}
	flowID, _ := resp["flowId"].(string)

	const racers = 8
	var wg sync.WaitGroup
	codes := make([]int, racers)
	wg.Add(racers)
	for i := 0; i < racers; i++ {
		go func(i int) {
			defer wg.Done()
			_, codes[i] = connectDo(t, h.handleConnectFlowInput, "POST", "/flows/"+flowID+"/input",
				map[string]string{"flowId": flowID},
				map[string]string{"code": "http://localhost:1455/callback?code=abc&state=st-dup"})
		}(i)
	}
	wg.Wait()

	accepted := 0
	for _, c := range codes {
		switch c {
		case http.StatusOK:
			accepted++
		case http.StatusConflict:
		default:
			t.Errorf("unexpected status %d (want 200 for the winner, 409 for the losers)", c)
		}
	}
	if accepted != 1 {
		t.Errorf("%d of %d concurrent input POSTs were accepted, want exactly 1", accepted, racers)
	}
	// The exchange count is the real assertion: two accepted POSTs means two
	// token exchanges against the provider.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && calls.Load() == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("anthropic exchange ran %d times, want exactly 1", got)
	}
}

// maskConnectCredential showed the first 4 AND last 4 characters of any key
// longer than 8, so a 9-character key rendered as "1234••••6789" — eight of its
// nine characters handed to the client.
func TestMaskConnectCredentialNeverRevealsTheHead(t *testing.T) {
	cases := []struct {
		key      string
		mustHide string
	}{
		{"123456789", "1234"},
		{"1234567890", "1234"},
		{"abcdefghijklmnop", "abcd"},
		{"123456789012345", "1234"},
	}
	for _, tc := range cases {
		got := maskConnectCredential(auth.Credential{Key: tc.key})
		// Scan every 4-char window EXCEPT the final one: the trailing 4 are
		// shown on purpose, so they are not a leak. Anything from earlier in the
		// key appearing in the mask is.
		for i := 0; i+4 < len(tc.key); i++ {
			if strings.Contains(got, tc.key[i:i+4]) {
				t.Errorf("mask(%q) = %q leaks the 4-char window %q from the head", tc.key, got, tc.key[i:i+4])
			}
		}
		if !strings.Contains(got, "•") {
			t.Errorf("mask(%q) = %q, want a masked marker", tc.key, got)
		}
	}
	// A long key may still show its tail, which is how a user recognises which
	// key is stored.
	if got := maskConnectCredential(auth.Credential{Key: "sk-abcdefghijklmnopqrstuv"}); !strings.Contains(got, "stuv") {
		t.Errorf("mask of a long key = %q, want the trailing 4 characters", got)
	}
	// Short keys must reveal nothing at all.
	for _, short := range []string{"", "a", "abc", "12345678", "123456789012345"} {
		if got := maskConnectCredential(auth.Credential{Key: short}); got != "••••" {
			t.Errorf("mask(%q) = %q, want a fully masked value", short, got)
		}
	}
}

// A cancel that lands after the flow claimed the right to save must be refused
// rather than allowed to interleave with auth.Set and report a state that is
// already a lie.
func TestConnectFlowCancelRefusedOnceCommitting(t *testing.T) {
	f := newConnectFlow("anthropic", "oauth", connectFlowPasteCode, connectFlowRunning)
	if !f.beginCommit() {
		t.Fatal("beginCommit from running must succeed")
	}
	if f.getState() != connectFlowCommitting {
		t.Fatalf("state = %q, want committing", f.getState())
	}
	if f.beginCommit() {
		t.Error("a second beginCommit must not be granted — two callers could both reach auth.Set")
	}
	h := NewHandler()
	connectFlows.add(f)
	t.Cleanup(func() { connectFlows.remove(f.id) })

	_, code := connectDo(t, h.handleConnectFlowCancel, "DELETE", "/flows/"+f.id,
		map[string]string{"flowId": f.id}, nil)
	if code != http.StatusConflict {
		t.Errorf("cancel during committing = %d, want %d", code, http.StatusConflict)
	}
	if got := f.getState(); got != connectFlowCommitting {
		t.Errorf("state after refused cancel = %q, want it left at committing", got)
	}
}

// beginCommit is the single gate on persistence, so it must refuse every state
// that is not one a background exchange can legitimately finish in.
func TestBeginCommitOnlyFromExchangeBackedStates(t *testing.T) {
	claimable := []connectFlowState{connectFlowRunning, connectFlowWaitingBrowser}
	for _, st := range claimable {
		f := newConnectFlow("p", "m", connectFlowPlugin, st)
		if !f.beginCommit() {
			t.Errorf("beginCommit from %q = false, want true", st)
		}
		if got := f.getState(); got != connectFlowCommitting {
			t.Errorf("after beginCommit from %q state = %q, want committing", st, got)
		}
	}
	refused := []connectFlowState{
		connectFlowWaitingInput, connectFlowCancelled,
		connectFlowComplete, connectFlowFailed, connectFlowCommitting,
	}
	for _, st := range refused {
		f := newConnectFlow("p", "m", connectFlowPlugin, st)
		if f.beginCommit() {
			t.Errorf("beginCommit from %q = true, want false (a cancelled or finished flow must not persist)", st)
		}
		if got := f.getState(); got != st {
			t.Errorf("a refused beginCommit changed state %q -> %q", st, got)
		}
	}
}

// A cancel that arrives before setCancel must not be lost: setCancel invokes the
// func immediately when the flow is already cancelled.
func TestSetCancelHonoursAnAlreadyCancelledFlow(t *testing.T) {
	f := newConnectFlow("p", "m", connectFlowPlugin, connectFlowWaitingBrowser)
	f.setState(connectFlowCancelled)
	cancelled := false
	f.setCancel(func() { cancelled = true })
	if !cancelled {
		t.Error("setCancel on an already-cancelled flow must invoke the func, or the cancel is lost")
	}
}

// takeCancel clears the func so a repeated cancel cannot call it twice.
func TestTakeCancelIsSingleUse(t *testing.T) {
	f := newConnectFlow("p", "m", connectFlowPlugin, connectFlowRunning)
	calls := 0
	f.setCancel(func() { calls++ })
	if c := f.takeCancel(); c == nil {
		t.Fatal("takeCancel returned nil for a flow with a cancel func")
	}
	if c := f.takeCancel(); c != nil {
		t.Error("takeCancel returned the func twice; a repeated cancel would call it again")
	}
}

// beginInput installs the cancel func under the same lock as the state
// transition, which is what closes the race with the cancel endpoint.
func TestBeginInputStoresCancelAtomically(t *testing.T) {
	f := newConnectFlow("p", "m", connectFlowPasteCode, connectFlowWaitingInput)
	installed := false
	if !f.beginInput(func() { installed = true }) {
		t.Fatal("beginInput from waiting_input must succeed")
	}
	if got := f.getState(); got != connectFlowRunning {
		t.Errorf("state = %q, want running", got)
	}
	if c := f.takeCancel(); c == nil {
		t.Fatal("beginInput must install the cancel func so cancel can reach the flow")
	}
	c := f.takeCancel()
	if c != nil {
		c()
		if !installed {
			t.Error("taking and invoking the installed cancel did not run it")
		}
	}
	if f.beginInput(func() {}) {
		t.Error("beginInput must refuse a second transition — that is the double-click bypass")
	}
}

// The Grok branch used to write f.cancel with no lock at all, racing the cancel
// handler's unlocked read. Run the whole pair concurrently under -race.
func TestConnectFlowCancelRacesInputHandler(t *testing.T) {
	h := NewHandler()
	preserveConnectCredential(t, "grok")
	stubConnectSeam(t, &grokSubscriptionLoginFn, func(ctx context.Context, authToken, ct0 string) (auth.Credential, error) {
		<-ctx.Done()
		return auth.Credential{}, ctx.Err()
	})

	for i := 0; i < 25; i++ {
		f := newConnectFlow("grok", "oauth", connectFlowCookies, connectFlowWaitingInput)
		connectFlows.add(f)
		t.Cleanup(func() { connectFlows.remove(f.id) })

		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			connectDo(t, h.handleConnectFlowInput, "POST", "/flows/"+f.id+"/input",
				map[string]string{"flowId": f.id},
				map[string]string{"authToken": "tok", "ct0": "ct0"})
		}()
		go func() {
			defer wg.Done()
			connectDo(t, h.handleConnectFlowCancel, "DELETE", "/flows/"+f.id,
				map[string]string{"flowId": f.id}, nil)
		}()
		wg.Wait()
	}
}

// Isolates the STATE gate from the context gate.
//
// handleConnectFlowCancel marks a flow cancelled even when takeCancel returned
// no func — a flow whose cancel func was never installed, or one already taken —
// so a cancelled flow can have a LIVE context. A late completion must still be
// discarded in that case. With only the ctx.Err() check in place, this saves the
// credential, so the test would not pass without beginCommit.
func TestCompleteConnectFlowRefusesCancelledFlowWithLiveContext(t *testing.T) {
	preserveConnectCredential(t, "anthropic")
	_ = auth.Remove("anthropic")

	f := newConnectFlow("anthropic", "oauth_max", connectFlowPasteCode, connectFlowRunning)
	// Cancelled, but deliberately NOT via its context.
	f.setState(connectFlowCancelled)
	ctx := context.Background()

	completeConnectFlow(ctx, f, "anthropic",
		auth.Credential{Kind: auth.KindOAuth, AccessToken: "tok-must-not-persist"}, nil)

	if got, ok := auth.Get("anthropic"); ok {
		t.Fatalf("a cancelled flow with a LIVE context still persisted its credential: %+v", got)
	}
	if got := f.getState(); got != connectFlowCancelled {
		t.Errorf("state = %q, want it left at cancelled", got)
	}
}
