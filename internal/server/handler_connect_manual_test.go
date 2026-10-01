package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/auth"
)

// Manual completion is the only ChatGPT login that works when the browser and
// the server do NOT share a host — a `serve --remote` server behind SSH/WSL, or
// a browser on a second device. The auto flow binds 127.0.0.1:1455 and waits
// for the browser to be redirected there; from another machine that redirect
// never arrives. Manual mode binds nothing and takes the redirect as paste-back
// input instead.
//
// The mode is chosen by the CLIENT and sent as `mode`, because only the client
// knows whether the browser shares the server's host: `remoteMode` lives on
// *Server, and these routes are registered as s.handler.*, so the handler
// cannot see it. `auto` stays the default, preserving existing behaviour.

func TestConnectOAuthStartOpenAIManualModeWaitsForPastedCallback(t *testing.T) {
	h := NewHandler()
	preserveConnectCredential(t, "openai")

	stubConnectSeam(t, &openaiManualStartFn, func() (auth.OpenAIManualFlow, error) {
		return auth.OpenAIManualFlow{
			AuthURL:  "https://auth.openai.com/oauth/authorize?manual=1",
			State:    "st-openai-manual",
			Verifier: "ver-openai-manual",
		}, nil
	})
	// The auto seam must not be reached in manual mode: binding the loopback
	// port is the thing manual mode exists to avoid.
	autoCalled := false
	stubConnectSeam(t, &openaiStartFn, func(ctx context.Context) (string, func() (auth.Credential, error), error) {
		autoCalled = true
		return "", nil, fmt.Errorf("auto mode must not start in manual mode")
	})

	resp, code := connectDo(t, h.handleConnectOAuthStart, "POST", "/api/auth/connect/openai/oauth/start",
		map[string]string{"provider": "openai"},
		map[string]string{"method": "oauth", "mode": "manual"})
	if code != http.StatusOK {
		t.Fatalf("start manual flow: %d %v", code, resp)
	}
	if autoCalled {
		t.Error("manual mode still invoked the loopback-binding auto flow")
	}
	// The UI keys its paste box off these two fields.
	if resp["kind"] != string(connectFlowLocalCallback) {
		t.Errorf("kind = %v, want local-callback", resp["kind"])
	}
	if resp["state"] != string(connectFlowWaitingInput) {
		t.Errorf("state = %v, want waiting_input so the UI shows a paste box", resp["state"])
	}
	if resp["url"] != "https://auth.openai.com/oauth/authorize?manual=1" {
		t.Errorf("url = %v, want the authorize URL", resp["url"])
	}
	flowID, _ := resp["flowId"].(string)
	if flowID == "" {
		t.Fatalf("no flowId: %v", resp)
	}
	// The verifier must not travel; the leak guard covers the shape, this
	// covers the new path.
	if got := fmt.Sprint(resp); strings.Contains(got, "ver-openai-manual") {
		t.Errorf("manual start response leaks the PKCE verifier: %v", resp)
	}
}

func TestConnectFlowInputManualCompletesWithStoredCredential(t *testing.T) {
	h := NewHandler()
	preserveConnectCredential(t, "openai")
	flowID := startTestOpenAIManualFlow(t, h)

	stubConnectSeam(t, &openaiManualExchangeFn, func(f auth.OpenAIManualFlow, pasted string) (auth.Credential, error) {
		if f.State != "st-openai-manual" {
			return auth.Credential{}, fmt.Errorf("flow state = %q, want st-openai-manual", f.State)
		}
		if pasted != "http://localhost:1455/auth/callback?code=code-m&state=st-openai-manual" {
			return auth.Credential{}, fmt.Errorf("unexpected paste %q", pasted)
		}
		return auth.Credential{Kind: auth.KindOAuth, AccessToken: "at-openai", RefreshToken: "rt-openai", Account: "chat@example.com"}, nil
	})

	resp, code := connectDo(t, h.handleConnectFlowInput, "POST", "/api/auth/connect/flows/"+flowID+"/input",
		map[string]string{"flowId": flowID},
		map[string]string{"code": "http://localhost:1455/auth/callback?code=code-m&state=st-openai-manual"})
	if code != http.StatusOK {
		t.Fatalf("flow input: %d %v", code, resp)
	}
	if resp["state"] != string(connectFlowRunning) {
		t.Fatalf("input state = %v, want running", resp["state"])
	}

	final := awaitConnectFlow(t, h, flowID)
	if final["state"] != string(connectFlowComplete) {
		t.Fatalf("final state = %v, want complete (error=%v)", final["state"], final["error"])
	}
	cred, ok := auth.Get("openai")
	if !ok || cred.AccessToken != "at-openai" || cred.RefreshToken != "rt-openai" {
		t.Fatalf("stored credential = %+v (ok=%v), want the exchanged token", cred, ok)
	}
	if final["account"] != "chat@example.com" {
		t.Errorf("account = %v, want chat@example.com", final["account"])
	}
}

// A failed exchange must surface as a failed flow, not a silent 200 that the UI
// would render as success.
func TestConnectFlowInputManualExchangeFailureMarksFlowFailed(t *testing.T) {
	h := NewHandler()
	preserveConnectCredential(t, "openai")
	flowID := startTestOpenAIManualFlow(t, h)

	stubConnectSeam(t, &openaiManualExchangeFn, func(f auth.OpenAIManualFlow, pasted string) (auth.Credential, error) {
		return auth.Credential{}, fmt.Errorf("state mismatch")
	})

	resp, code := connectDo(t, h.handleConnectFlowInput, "POST", "/api/auth/connect/flows/"+flowID+"/input",
		map[string]string{"flowId": flowID},
		map[string]string{"code": "http://localhost:1455/auth/callback?code=c&state=WRONG"})
	if code != http.StatusOK {
		t.Fatalf("flow input: %d %v", code, resp)
	}
	final := awaitConnectFlow(t, h, flowID)
	if final["state"] != string(connectFlowFailed) {
		t.Fatalf("final state = %v, want failed", final["state"])
	}
	if _, ok := auth.Get("openai"); ok {
		t.Error("a failed exchange stored a credential")
	}
}

// An AUTO-mode local-callback flow must NOT accept pasted input: it owns a
// listener, and accepting a paste would let anyone drive that flow. It stays
// waiting_browser, which the input handler rejects before this switch.
func TestConnectFlowInputRejectsAutoModeCallback(t *testing.T) {
	h := NewHandler()
	preserveConnectCredential(t, "openai")

	// finish IS called by the auto flow's background goroutine — that is the
	// whole point of auto mode. It must simply block (or fail harmlessly) so
	// the flow stays waiting_browser; the assertion under test is that a PASTE
	// cannot drive it, not that finish is never reached.
	stubConnectSeam(t, &openaiStartFn, func(ctx context.Context) (string, func() (auth.Credential, error), error) {
		return "https://auth.openai.com/oauth/authorize?auto=1", func() (auth.Credential, error) {
			<-ctx.Done()
			return auth.Credential{}, ctx.Err()
		}, nil
	})
	resp, code := connectDo(t, h.handleConnectOAuthStart, "POST", "/api/auth/connect/openai/oauth/start",
		map[string]string{"provider": "openai"},
		map[string]string{"method": "oauth"})
	if code != http.StatusOK {
		t.Fatalf("start auto flow: %d %v", code, resp)
	}
	if resp["state"] != string(connectFlowWaitingBrowser) {
		t.Fatalf("state = %v, want waiting_browser (auto mode is the default)", resp["state"])
	}
	flowID, _ := resp["flowId"].(string)

	_, code = connectDo(t, h.handleConnectFlowInput, "POST", "/api/auth/connect/flows/"+flowID+"/input",
		map[string]string{"flowId": flowID},
		map[string]string{"code": "http://localhost:1455/auth/callback?code=c&state=s"})
	if code != http.StatusBadRequest {
		t.Errorf("pasted input to an auto flow returned %d, want 400", code)
	}
}

// startTestOpenAIManualFlow starts a manual-mode openai flow and returns its id.
func startTestOpenAIManualFlow(t *testing.T, h *Handler) string {
	t.Helper()
	stubConnectSeam(t, &openaiManualStartFn, func() (auth.OpenAIManualFlow, error) {
		return auth.OpenAIManualFlow{
			AuthURL:  "https://auth.openai.com/oauth/authorize?manual=1",
			State:    "st-openai-manual",
			Verifier: "ver-openai-manual",
		}, nil
	})
	resp, code := connectDo(t, h.handleConnectOAuthStart, "POST", "/api/auth/connect/openai/oauth/start",
		map[string]string{"provider": "openai"},
		map[string]string{"method": "oauth", "mode": "manual"})
	if code != http.StatusOK {
		t.Fatalf("start manual flow: %d %v", code, resp)
	}
	flowID, _ := resp["flowId"].(string)
	if flowID == "" {
		t.Fatalf("no flowId in response: %v", resp)
	}
	return flowID
}
