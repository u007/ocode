package server

import (
	"context"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/u007/ocode/internal/auth"
)

// TestConnectOAuthStartOpenAIDefaultsToAuto pins the fallback: anything that is
// not literally "manual" gets the loopback-binding auto flow.
//
// This is the compatibility contract. Every client that predates `mode` sends no
// field at all, and a client that sends garbage (a stale build, a typo, a
// hand-rolled curl) must not silently lose its flow — the safe reading of an
// unknown value is the historical one, not "manual", because manual is the mode
// that changes the flow's SHAPE. A typo would otherwise hand every user a
// paste-back box they did not ask for.
func TestConnectOAuthStartOpenAIDefaultsToAuto(t *testing.T) {
	cases := []struct {
		name string
		body map[string]string
	}{
		{"mode field absent", map[string]string{"method": "oauth"}},
		{"mode empty", map[string]string{"method": "oauth", "mode": ""}},
		{"mode unrecognised", map[string]string{"method": "oauth", "mode": "bogus"}},
		{"mode wrong case", map[string]string{"method": "oauth", "mode": "Manual"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := NewHandler()
			preserveConnectCredential(t, "openai")

			manualCalled := false
			stubConnectSeam(t, &openaiManualStartFn, func() (auth.OpenAIManualFlow, error) {
				manualCalled = true
				return auth.OpenAIManualFlow{}, fmt.Errorf("manual starter must not run")
			})
			autoCalled := false
			release := make(chan struct{})
			t.Cleanup(func() { close(release) })
			stubConnectSeam(t, &openaiStartFn, func(ctx context.Context) (string, func() (auth.Credential, error), error) {
				autoCalled = true
				// The finish func blocks until the test ends: an auto flow waits
				// on a real browser redirect, and returning immediately would
				// complete the flow before the assertions read it.
				return "https://auth.openai.com/oauth/authorize?auto=1", func() (auth.Credential, error) {
					<-release
					return auth.Credential{}, fmt.Errorf("released")
				}, nil
			})

			resp, code := connectDo(t, h.handleConnectOAuthStart, "POST", "/api/auth/connect/openai/oauth/start",
				map[string]string{"provider": "openai"}, tc.body)
			if code != http.StatusOK {
				t.Fatalf("start: %d %v", code, resp)
			}
			if !autoCalled {
				t.Error("auto starter was not invoked")
			}
			if manualCalled {
				t.Error("manual starter was invoked for a non-manual mode")
			}
			if resp["state"] != string(connectFlowWaitingBrowser) {
				t.Errorf("state = %v, want waiting_browser (auto)", resp["state"])
			}
		})
	}
}

// A manual paste is single-use. The code is exchanged exactly once per flow, so
// a replayed paste cannot drive a second exchange against a credential that is
// already being written.
//
// The exchange is held open so the second paste lands while the flow is
// `running` — that is the state a replay actually exploits. Asserting after
// completion would pass for the wrong reason (the flow would be terminal either
// way) and would not pin single-use at all.
func TestConnectFlowInputManualIsSingleUse(t *testing.T) {
	h := NewHandler()
	preserveConnectCredential(t, "openai")
	flowID := startTestOpenAIManualFlow(t, h)

	var exchanges atomic.Int32
	release := make(chan struct{})
	entered := make(chan struct{}, 1)
	t.Cleanup(func() {
		// Unblock the exchange so the flow goroutine can exit even if an
		// assertion above already failed.
		select {
		case <-release:
		default:
			close(release)
		}
	})
	stubConnectSeam(t, &openaiManualExchangeFn, func(f auth.OpenAIManualFlow, pasted string) (auth.Credential, error) {
		exchanges.Add(1)
		select {
		case entered <- struct{}{}:
		default:
		}
		<-release
		return auth.Credential{Kind: auth.KindOAuth, AccessToken: "at-openai"}, nil
	})

	paste := "http://localhost:1455/auth/callback?code=code-m&state=st-openai-manual"
	resp, code := connectDo(t, h.handleConnectFlowInput, "POST", "/api/auth/connect/flows/"+flowID+"/input",
		map[string]string{"flowId": flowID}, map[string]string{"code": paste})
	if code != http.StatusOK {
		t.Fatalf("first paste: %d %v", code, resp)
	}

	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("exchange never started")
	}

	resp, code = connectDo(t, h.handleConnectFlowInput, "POST", "/api/auth/connect/flows/"+flowID+"/input",
		map[string]string{"flowId": flowID}, map[string]string{"code": paste})
	if code != http.StatusConflict {
		t.Fatalf("replayed paste: %d %v, want 409 (the flow already started)", code, resp)
	}
	if got := exchanges.Load(); got != 1 {
		t.Fatalf("exchanges = %d, want exactly 1 — the replay reached the exchange", got)
	}

	close(release)
	awaitConnectFlow(t, h, flowID)
}
