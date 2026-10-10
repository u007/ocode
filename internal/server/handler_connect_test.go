package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/u007/ocode/internal/auth"
	providerplugin "github.com/u007/ocode/internal/plugin/provider"
)

// connectHandlerFunc matches every connect endpoint's signature so the
// tests can drive them the same way the profile-auth tests drive
// their handlers (directly, without the mux).
type connectHandlerFunc func(w http.ResponseWriter, r *http.Request)

// connectDo invokes a connect endpoint and returns the decoded JSON
// body plus the HTTP status.
func connectDo(t *testing.T, hf connectHandlerFunc, method, path string, pathValues map[string]string, body interface{}) (map[string]any, int) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	for k, v := range pathValues {
		req.SetPathValue(k, v)
	}
	rec := httptest.NewRecorder()
	hf(rec, req)
	out := map[string]any{}
	if rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("%s %s: response is not JSON: %s", method, path, rec.Body.String())
		}
	}
	return out, rec.Code
}

// preserveConnectCredential saves and restores a provider's base
// credential and env var around a test, so tests can mutate the
// process-global auth store without leaking into sibling tests.
func preserveConnectCredential(t *testing.T, providerID string) {
	t.Helper()
	prevCred, hadPrev := auth.Get(providerID)
	t.Cleanup(func() {
		if hadPrev {
			_ = auth.Set(providerID, prevCred)
		} else {
			_ = auth.Remove(providerID)
		}
	})
	p := auth.FindProvider(providerID)
	if p == nil || p.EnvVar == "" {
		return
	}
	prevEnv, hadEnv := os.LookupEnv(p.EnvVar)
	t.Cleanup(func() {
		if hadEnv {
			_ = os.Setenv(p.EnvVar, prevEnv)
		} else {
			_ = os.Unsetenv(p.EnvVar)
		}
	})
}

// stubConnectSeam swaps a connect flow seam for the test and restores
// it afterwards (the notifyGitAction seam pattern).
func stubConnectSeam[T any](t *testing.T, slot *T, stub T) {
	t.Helper()
	orig := *slot
	*slot = stub
	t.Cleanup(func() { *slot = orig })
}

// awaitConnectFlow polls a flow's status until it reaches a terminal
// state or the deadline passes.
func awaitConnectFlow(t *testing.T, h *Handler, flowID string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp, code := connectDo(t, h.handleConnectFlowStatus, "GET", "/api/auth/connect/flows/"+flowID, map[string]string{"flowId": flowID}, nil)
		if code != http.StatusOK {
			t.Fatalf("flow status: %d %v", code, resp)
		}
		switch resp["state"] {
		case "complete", "failed", "cancelled":
			return resp
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("flow did not reach a terminal state in time")
	return nil
}

// startTestAnthropicFlow starts a paste-code flow for anthropic with
// stubbed authorize/exchange seams and returns the flow id.
func startTestAnthropicFlow(t *testing.T, h *Handler) string {
	t.Helper()
	stubConnectSeam(t, &anthropicAuthorizeFn, func(mode string) (auth.AnthropicFlow, error) {
		return auth.AnthropicFlow{URL: "https://claude.ai/authorize?x=1", State: "st-1", Verifier: "ver-1", Mode: mode}, nil
	})
	resp, code := connectDo(t, h.handleConnectOAuthStart, "POST", "/api/auth/connect/anthropic/oauth/start", map[string]string{"provider": "anthropic"}, map[string]string{"method": "oauth_max"})
	if code != http.StatusOK {
		t.Fatalf("start anthropic flow: %d %v", code, resp)
	}
	flowID, _ := resp["flowId"].(string)
	if flowID == "" {
		t.Fatalf("no flowId in response: %v", resp)
	}
	return flowID
}

// stubConnectPlugin registers a provider plugin stub for the test. The
// plugin registry has no unregister, so stubs are only registered by
// tests in this file, whose assertions never depend on another test's
// plugin state.
func stubConnectPlugin(t *testing.T, providerID string, methods []providerplugin.AuthMethod) {
	t.Helper()
	providerplugin.Register(&stubConnectProvider{providerID: providerID, methods: methods})
}

type stubConnectProvider struct {
	providerID string
	methods    []providerplugin.AuthMethod
}

func (s *stubConnectProvider) ID() string { return s.providerID }
func (s *stubConnectProvider) AuthMethods() []providerplugin.AuthMethod {
	return s.methods
}
func (s *stubConnectProvider) Authenticate(ctx context.Context, method providerplugin.AuthMethod) (providerplugin.AuthResult, error) {
	return providerplugin.AuthResult{}, nil
}
func (s *stubConnectProvider) ModelAllowed(modelID string) bool { return true }
func (s *stubConnectProvider) AdjustModel(m providerplugin.Model) providerplugin.Model {
	return m
}
func (s *stubConnectProvider) RequestHeaders(ctx providerplugin.RequestContext) http.Header {
	return nil
}
func (s *stubConnectProvider) RequestParams(ctx providerplugin.RequestContext) map[string]any {
	return nil
}

func TestConnectListMasksStoredKeys(t *testing.T) {
	h := NewHandler()
	preserveConnectCredential(t, "deepseek")
	if err := auth.Set("deepseek", auth.Credential{Kind: auth.KindAPIKey, Key: "sk-abcdefghijklmnop"}); err != nil {
		t.Fatal(err)
	}

	resp, code := connectDo(t, h.handleConnectList, "GET", "/api/auth/connect", nil, nil)
	if code != http.StatusOK {
		t.Fatalf("list: %d %v", code, resp)
	}
	providers, ok := resp["providers"].([]any)
	if !ok || len(providers) == 0 {
		t.Fatalf("no providers listed: %v", resp)
	}
	// The catalog order is the display order.
	first, _ := providers[0].(map[string]any)
	if first["id"] != "openai" {
		t.Fatalf("first provider = %v, want openai (catalog order)", first["id"])
	}
	var row map[string]any
	for _, p := range providers {
		m, _ := p.(map[string]any)
		if m["id"] == "deepseek" {
			row = m
			break
		}
	}
	if row == nil {
		t.Fatal("deepseek missing from list")
	}
	if row["status"] != "✓" || row["statusDetail"] != "api key" {
		t.Fatalf("deepseek status = %v/%v, want ✓/api key", row["status"], row["statusDetail"])
	}
	if row["hasCredential"] != true {
		t.Fatalf("deepseek hasCredential = %v, want true", row["hasCredential"])
	}
	if row["kind"] != "api" {
		t.Fatalf("deepseek kind = %v, want api", row["kind"])
	}
	// Only the trailing 4 characters are ever shown, and only for a key of at
	// least 16 characters. The head used to be revealed too, which handed a
	// client 8 of a 9-character key.
	if row["masked"] != "••••••••••••mnop" {
		t.Fatalf("deepseek masked = %v, want ••••••••••••mnop", row["masked"])
	}
	if strings.Contains(fmt.Sprint(row["masked"]), "sk-a") {
		t.Fatalf("deepseek masked = %v leaks the head of the key", row["masked"])
	}
	methods, _ := row["methods"].([]any)
	hasAPIKey, hasRemove := false, false
	for _, m := range methods {
		mm, _ := m.(map[string]any)
		switch mm["id"] {
		case "apikey":
			hasAPIKey = true
		case "remove":
			hasRemove = true
		}
	}
	if !hasAPIKey || !hasRemove {
		t.Fatalf("deepseek methods missing apikey/remove: %v", row["methods"])
	}
}

func TestConnectSetSavesAPIKeyAndEnvVar(t *testing.T) {
	h := NewHandler()
	preserveConnectCredential(t, "deepseek")

	resp, code := connectDo(t, h.handleConnectSet, "PUT", "/api/auth/connect/deepseek", map[string]string{"provider": "deepseek"}, map[string]string{"apiKey": "sk-test-12345678"})
	if code != http.StatusOK {
		t.Fatalf("set: %d %v", code, resp)
	}
	if resp["ok"] != true {
		t.Fatalf("set ok = %v, want true", resp["ok"])
	}
	cred, ok := auth.Get("deepseek")
	if !ok || cred.Kind != auth.KindAPIKey || cred.Key != "sk-test-12345678" {
		t.Fatalf("stored credential = %+v (ok=%v), want the API key", cred, ok)
	}
	if os.Getenv("DEEPSEEK_API_KEY") != "sk-test-12345678" {
		t.Fatalf("DEEPSEEK_API_KEY = %q, want the saved key (env outranks the store)", os.Getenv("DEEPSEEK_API_KEY"))
	}
	prov, _ := resp["provider"].(map[string]any)
	// "sk-test-12345678" is 16 characters, so it clears the disclosure floor and
	// shows only its trailing 4. Never the head.
	if prov["masked"] != "••••••••••••5678" {
		t.Fatalf("provider masked = %v, want ••••••••••••5678", prov["masked"])
	}
	if strings.Contains(fmt.Sprint(prov["masked"]), "sk-t") {
		t.Fatalf("provider masked = %v leaks the head of the key", prov["masked"])
	}
}

func TestConnectSetRequiresKey(t *testing.T) {
	h := NewHandler()
	preserveConnectCredential(t, "deepseek")

	resp, code := connectDo(t, h.handleConnectSet, "PUT", "/api/auth/connect/deepseek", map[string]string{"provider": "deepseek"}, map[string]string{})
	if code != http.StatusBadRequest {
		t.Fatalf("set without key: %d %v", code, resp)
	}
	if resp["error"] != "apiKey or key required" {
		t.Fatalf("error = %v, want apiKey or key required", resp["error"])
	}
}

func TestConnectSetCloudflareWorkersExtras(t *testing.T) {
	h := NewHandler()
	preserveConnectCredential(t, "cloudflare-workers")

	resp, code := connectDo(t, h.handleConnectSet, "PUT", "/api/auth/connect/cloudflare-workers", map[string]string{"provider": "cloudflare-workers"}, map[string]string{"apiKey": "cf-key-12345678"})
	if code != http.StatusBadRequest {
		t.Fatalf("set without accountId: %d %v", code, resp)
	}
	if !strings.Contains(fmt.Sprint(resp["error"]), "accountId required") {
		t.Fatalf("error = %v, want accountId required", resp["error"])
	}

	resp, code = connectDo(t, h.handleConnectSet, "PUT", "/api/auth/connect/cloudflare-workers", map[string]string{"provider": "cloudflare-workers"}, map[string]string{"apiKey": "cf-key-12345678", "accountId": "acct-1"})
	if code != http.StatusOK {
		t.Fatalf("set with accountId: %d %v", code, resp)
	}
	cred, ok := auth.Get("cloudflare-workers")
	if !ok || cred.AccountID != "acct-1" {
		t.Fatalf("stored AccountID = %q (ok=%v), want acct-1", cred.AccountID, ok)
	}
	if cred.BaseURL != auth.CloudflareWorkersBaseURL("acct-1") {
		t.Fatalf("stored BaseURL = %q, want %q", cred.BaseURL, auth.CloudflareWorkersBaseURL("acct-1"))
	}
}

func TestConnectRemoveDeletesCredential(t *testing.T) {
	h := NewHandler()
	preserveConnectCredential(t, "deepseek")
	if err := auth.Set("deepseek", auth.Credential{Kind: auth.KindAPIKey, Key: "sk-abcdefghijklmnop"}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEEPSEEK_API_KEY", "sk-abcdefghijklmnop")

	resp, code := connectDo(t, h.handleConnectRemove, "DELETE", "/api/auth/connect/deepseek", map[string]string{"provider": "deepseek"}, nil)
	if code != http.StatusOK {
		t.Fatalf("remove: %d %v", code, resp)
	}
	if resp["ok"] != true {
		t.Fatalf("remove ok = %v, want true", resp["ok"])
	}
	if _, ok := auth.Get("deepseek"); ok {
		t.Fatal("credential still stored after remove")
	}
	if os.Getenv("DEEPSEEK_API_KEY") != "" {
		t.Fatalf("DEEPSEEK_API_KEY = %q, want unset after remove", os.Getenv("DEEPSEEK_API_KEY"))
	}
}

func TestConnectOAuthStartAnthropicPasteCode(t *testing.T) {
	h := NewHandler()
	preserveConnectCredential(t, "anthropic")
	flowID := startTestAnthropicFlow(t, h)

	resp, code := connectDo(t, h.handleConnectFlowStatus, "GET", "/api/auth/connect/flows/"+flowID, map[string]string{"flowId": flowID}, nil)
	if code != http.StatusOK {
		t.Fatalf("flow status: %d %v", code, resp)
	}
	if resp["state"] != "waiting_input" {
		t.Fatalf("state = %v, want waiting_input", resp["state"])
	}
	if resp["kind"] != "paste-code" {
		t.Fatalf("kind = %v, want paste-code", resp["kind"])
	}
	if resp["provider"] != "anthropic" {
		t.Fatalf("provider = %v, want anthropic", resp["provider"])
	}
	if resp["url"] != "https://claude.ai/authorize?x=1" {
		t.Fatalf("url = %v, want the authorize URL", resp["url"])
	}
}

func TestConnectFlowInputPasteCodeCompletes(t *testing.T) {
	h := NewHandler()
	preserveConnectCredential(t, "anthropic")
	flowID := startTestAnthropicFlow(t, h)

	stubConnectSeam(t, &anthropicExchangeFn, func(code, state, verifier string) (auth.Credential, error) {
		if code != "auth-code-1" || state != "st-1" || verifier != "ver-1" {
			return auth.Credential{}, fmt.Errorf("unexpected exchange args %q %q %q", code, state, verifier)
		}
		return auth.Credential{Kind: auth.KindOAuth, AccessToken: "at-1", RefreshToken: "rt-1", Account: "Claude User"}, nil
	})

	resp, code := connectDo(t, h.handleConnectFlowInput, "POST", "/api/auth/connect/flows/"+flowID+"/input", map[string]string{"flowId": flowID}, map[string]string{"code": "https://claude.ai/oauth/callback?code=auth-code-1&state=st-1"})
	if code != http.StatusOK {
		t.Fatalf("flow input: %d %v", code, resp)
	}
	if resp["state"] != "running" {
		t.Fatalf("input state = %v, want running", resp["state"])
	}

	final := awaitConnectFlow(t, h, flowID)
	if final["state"] != "complete" {
		t.Fatalf("final state = %v, want complete (error=%v)", final["state"], final["error"])
	}
	if final["account"] != "Claude User" {
		t.Fatalf("final account = %v, want Claude User", final["account"])
	}
	cred, ok := auth.Get("anthropic")
	if !ok || cred.Kind != auth.KindOAuth || cred.AccessToken != "at-1" || cred.RefreshToken != "rt-1" {
		t.Fatalf("stored credential = %+v (ok=%v), want the exchanged OAuth token", cred, ok)
	}
}

func TestConnectFlowInputStateMismatch(t *testing.T) {
	h := NewHandler()
	preserveConnectCredential(t, "anthropic")
	flowID := startTestAnthropicFlow(t, h)

	resp, code := connectDo(t, h.handleConnectFlowInput, "POST", "/api/auth/connect/flows/"+flowID+"/input", map[string]string{"flowId": flowID}, map[string]string{"code": "https://claude.ai/oauth/callback?code=auth-code-1&state=evil"})
	if code != http.StatusBadRequest {
		t.Fatalf("state mismatch: %d %v", code, resp)
	}
	if !strings.Contains(fmt.Sprint(resp["error"]), "state mismatch") {
		t.Fatalf("error = %v, want state mismatch", resp["error"])
	}
}

func TestConnectFlowInputUnparseableCode(t *testing.T) {
	h := NewHandler()
	preserveConnectCredential(t, "anthropic")
	flowID := startTestAnthropicFlow(t, h)

	resp, code := connectDo(t, h.handleConnectFlowInput, "POST", "/api/auth/connect/flows/"+flowID+"/input", map[string]string{"flowId": flowID}, map[string]string{"code": "not a callback url"})
	if code != http.StatusBadRequest {
		t.Fatalf("unparseable code: %d %v", code, resp)
	}
	if !strings.Contains(fmt.Sprint(resp["error"]), "could not parse") {
		t.Fatalf("error = %v, want could not parse", resp["error"])
	}
}

func TestConnectOAuthStartCopilotDeviceFlow(t *testing.T) {
	h := NewHandler()
	preserveConnectCredential(t, "copilot")
	accountFetched := make(chan string, 1)

	stubConnectSeam(t, &copilotStartFn, func() (auth.CopilotDevice, error) {
		return auth.CopilotDevice{DeviceCode: "dc-1", UserCode: "ABCD-EFGH", VerificationURI: "https://github.com/login/device", Interval: 1}, nil
	})
	stubConnectSeam(t, &copilotPollFn, func(ctx context.Context, dev auth.CopilotDevice) (auth.Credential, error) {
		if dev.DeviceCode != "dc-1" {
			return auth.Credential{}, fmt.Errorf("wrong device code %q", dev.DeviceCode)
		}
		return auth.Credential{Kind: auth.KindOAuth, AccessToken: "gh-at-1"}, nil
	})
	stubConnectSeam(t, &copilotFetchAccountFn, func(ghToken string) string {
		select {
		case accountFetched <- ghToken:
		default:
		}
		return "copilot-user"
	})

	resp, code := connectDo(t, h.handleConnectOAuthStart, "POST", "/api/auth/connect/copilot/oauth/start", map[string]string{"provider": "copilot"}, map[string]string{"method": "oauth"})
	if code != http.StatusOK {
		t.Fatalf("start copilot flow: %d %v", code, resp)
	}
	if resp["kind"] != "device-code" {
		t.Fatalf("kind = %v, want device-code", resp["kind"])
	}
	if resp["userCode"] != "ABCD-EFGH" {
		t.Fatalf("userCode = %v, want ABCD-EFGH", resp["userCode"])
	}
	if resp["verificationUri"] != "https://github.com/login/device" {
		t.Fatalf("verificationUri = %v", resp["verificationUri"])
	}
	flowID, _ := resp["flowId"].(string)

	final := awaitConnectFlow(t, h, flowID)
	if final["state"] != "complete" {
		t.Fatalf("final state = %v, want complete (error=%v)", final["state"], final["error"])
	}
	if final["account"] != "copilot-user" {
		t.Fatalf("final account = %v, want copilot-user", final["account"])
	}
	cred, ok := auth.Get("copilot")
	if !ok || cred.AccessToken != "gh-at-1" || cred.Account != "copilot-user" {
		t.Fatalf("stored credential = %+v (ok=%v), want the polled token and account", cred, ok)
	}
	select {
	case got := <-accountFetched:
		if got != "gh-at-1" {
			t.Fatalf("account fetched for token %q, want gh-at-1", got)
		}
	default:
		t.Fatal("account was never fetched from the access token")
	}
}

func TestConnectFlowCancel(t *testing.T) {
	h := NewHandler()
	preserveConnectCredential(t, "copilot")

	stubConnectSeam(t, &copilotStartFn, func() (auth.CopilotDevice, error) {
		return auth.CopilotDevice{DeviceCode: "dc-1", UserCode: "ABCD-EFGH", VerificationURI: "https://github.com/login/device"}, nil
	})
	drained := make(chan struct{})
	stubConnectSeam(t, &copilotPollFn, func(ctx context.Context, dev auth.CopilotDevice) (auth.Credential, error) {
		defer close(drained)
		<-ctx.Done()
		return auth.Credential{}, ctx.Err()
	})

	resp, code := connectDo(t, h.handleConnectOAuthStart, "POST", "/api/auth/connect/copilot/oauth/start", map[string]string{"provider": "copilot"}, map[string]string{"method": "oauth"})
	if code != http.StatusOK {
		t.Fatalf("start copilot flow: %d %v", code, resp)
	}
	flowID, _ := resp["flowId"].(string)
	t.Cleanup(func() { cancelAndDrainConnectFlow(t, h, flowID, drained) })

	resp, code = connectDo(t, h.handleConnectFlowCancel, "DELETE", "/api/auth/connect/flows/"+flowID, map[string]string{"flowId": flowID}, nil)
	if code != http.StatusOK {
		t.Fatalf("cancel: %d %v", code, resp)
	}
	if resp["state"] != "cancelled" {
		t.Fatalf("cancel state = %v, want cancelled", resp["state"])
	}

	// A cancelled flow is dropped from the registry, so its status is
	// gone rather than reporting a stale state.
	_, code = connectDo(t, h.handleConnectFlowStatus, "GET", "/api/auth/connect/flows/"+flowID, map[string]string{"flowId": flowID}, nil)
	if code != http.StatusNotFound {
		t.Fatalf("status after cancel: %d, want 404", code)
	}
}

func TestConnectOAuthStartRejectsBadMethods(t *testing.T) {
	h := NewHandler()

	resp, code := connectDo(t, h.handleConnectOAuthStart, "POST", "/api/auth/connect/deepseek/oauth/start", map[string]string{"provider": "deepseek"}, map[string]string{"method": "bogus"})
	if code != http.StatusBadRequest {
		t.Fatalf("unknown method: %d %v", code, resp)
	}
	if !strings.Contains(fmt.Sprint(resp["error"]), "not offered") {
		t.Fatalf("error = %v, want not offered", resp["error"])
	}

	// The API-key method is offered but is not an OAuth flow.
	resp, code = connectDo(t, h.handleConnectOAuthStart, "POST", "/api/auth/connect/deepseek/oauth/start", map[string]string{"provider": "deepseek"}, map[string]string{"method": "apikey"})
	if code != http.StatusBadRequest {
		t.Fatalf("apikey method: %d %v", code, resp)
	}
	if !strings.Contains(fmt.Sprint(resp["error"]), "not an OAuth flow") {
		t.Fatalf("error = %v, want not an OAuth flow", resp["error"])
	}

	resp, code = connectDo(t, h.handleConnectOAuthStart, "POST", "/api/auth/connect/nope/oauth/start", map[string]string{"provider": "nope"}, map[string]string{"method": "oauth"})
	if code != http.StatusBadRequest {
		t.Fatalf("unknown provider: %d %v", code, resp)
	}
}

func TestConnectTestProvider(t *testing.T) {
	h := NewHandler()

	stubConnectSeam(t, &testCredentialFn, func(ctx context.Context, id string) error {
		if id != "deepseek" {
			return fmt.Errorf("unexpected provider %q", id)
		}
		return nil
	})
	resp, code := connectDo(t, h.handleConnectTest, "POST", "/api/auth/connect/deepseek/test", map[string]string{"provider": "deepseek"}, nil)
	if code != http.StatusOK {
		t.Fatalf("test: %d %v", code, resp)
	}
	if resp["ok"] != true {
		t.Fatalf("test ok = %v, want true", resp["ok"])
	}

	stubConnectSeam(t, &testCredentialFn, func(ctx context.Context, id string) error {
		return errors.New("boom")
	})
	resp, code = connectDo(t, h.handleConnectTest, "POST", "/api/auth/connect/deepseek/test", map[string]string{"provider": "deepseek"}, nil)
	if code != http.StatusOK {
		t.Fatalf("test (failing): %d %v", code, resp)
	}
	if resp["ok"] != false {
		t.Fatalf("test ok = %v, want false", resp["ok"])
	}
	if resp["error"] != "boom" {
		t.Fatalf("test error = %v, want boom", resp["error"])
	}
}

func TestConnectGrokSubscriptionCookiesFlow(t *testing.T) {
	h := NewHandler()
	preserveConnectCredential(t, "grok")
	stubConnectPlugin(t, "grok", []providerplugin.AuthMethod{
		{Label: "Grok Subscription", Type: "oauth", Run: func(ctx context.Context) (providerplugin.AuthResult, error) {
			return providerplugin.AuthResult{}, errors.New("unused: the cookie exchange seam runs instead")
		}},
	})

	// With the grok plugin registered, the subscription method is
	// offered under its dedicated id.
	resp, code := connectDo(t, h.handleConnectOAuthStart, "POST", "/api/auth/connect/grok/oauth/start", map[string]string{"provider": "grok"}, map[string]string{"method": "grok_subscription"})
	if code != http.StatusOK {
		t.Fatalf("start grok flow: %d %v", code, resp)
	}
	if resp["kind"] != "cookies" {
		t.Fatalf("kind = %v, want cookies", resp["kind"])
	}
	flowID, _ := resp["flowId"].(string)

	stubConnectSeam(t, &grokSubscriptionLoginFn, func(ctx context.Context, authToken, ct0 string) (auth.Credential, error) {
		if authToken != "at-cookie" || ct0 != "ct0-cookie" {
			return auth.Credential{}, fmt.Errorf("unexpected cookies %q/%q", authToken, ct0)
		}
		return auth.Credential{Kind: auth.KindOAuth, AccessToken: "grok-sso", Account: "grok-user"}, nil
	})
	resp, code = connectDo(t, h.handleConnectFlowInput, "POST", "/api/auth/connect/flows/"+flowID+"/input", map[string]string{"flowId": flowID}, map[string]string{"authToken": "at-cookie", "ct0": "ct0-cookie"})
	if code != http.StatusOK {
		t.Fatalf("grok cookies input: %d %v", code, resp)
	}

	final := awaitConnectFlow(t, h, flowID)
	if final["state"] != "complete" {
		t.Fatalf("final state = %v, want complete (error=%v)", final["state"], final["error"])
	}
	if final["account"] != "grok-user" {
		t.Fatalf("final account = %v, want grok-user", final["account"])
	}
	cred, ok := auth.Get("grok")
	if !ok || cred.AccessToken != "grok-sso" || cred.Account != "grok-user" {
		t.Fatalf("stored credential = %+v (ok=%v), want the SSO token", cred, ok)
	}

	// A second flow rejects missing cookies.
	resp, code = connectDo(t, h.handleConnectOAuthStart, "POST", "/api/auth/connect/grok/oauth/start", map[string]string{"provider": "grok"}, map[string]string{"method": "grok_subscription"})
	if code != http.StatusOK {
		t.Fatalf("start second grok flow: %d %v", code, resp)
	}
	flowID2, _ := resp["flowId"].(string)
	resp, code = connectDo(t, h.handleConnectFlowInput, "POST", "/api/auth/connect/flows/"+flowID2+"/input", map[string]string{"flowId": flowID2}, map[string]string{"authToken": "", "ct0": ""})
	if code != http.StatusBadRequest {
		t.Fatalf("missing cookies: %d %v", code, resp)
	}
	if !strings.Contains(fmt.Sprint(resp["error"]), "Both x.com cookies") {
		t.Fatalf("error = %v, want the both-cookies requirement", resp["error"])
	}
}

func TestConnectPluginAuthFlow(t *testing.T) {
	h := NewHandler()
	preserveConnectCredential(t, "opencode")
	stubConnectPlugin(t, "opencode", []providerplugin.AuthMethod{
		{Label: "Service Key", Type: "api", Run: func(ctx context.Context) (providerplugin.AuthResult, error) {
			return providerplugin.AuthResult{Type: "api", Key: "plug-key-12345678", AccountID: "acct-9"}, nil
		}},
	})

	resp, code := connectDo(t, h.handleConnectOAuthStart, "POST", "/api/auth/connect/opencode/oauth/start", map[string]string{"provider": "opencode"}, map[string]string{"method": "plugin_Service Key"})
	if code != http.StatusOK {
		t.Fatalf("start plugin flow: %d %v", code, resp)
	}
	if resp["kind"] != "plugin" {
		t.Fatalf("kind = %v, want plugin", resp["kind"])
	}
	flowID, _ := resp["flowId"].(string)

	final := awaitConnectFlow(t, h, flowID)
	if final["state"] != "complete" {
		t.Fatalf("final state = %v, want complete (error=%v)", final["state"], final["error"])
	}
	cred, ok := auth.Get("opencode")
	if !ok || cred.Kind != auth.KindAPIKey || cred.Key != "plug-key-12345678" || cred.AccountID != "acct-9" {
		t.Fatalf("stored credential = %+v (ok=%v), want the plugin's API key", cred, ok)
	}
}

// cancelAndDrainConnectFlow cancels a flow and waits for its background
// exchange goroutine to exit.
//
// Tests that stub a blocking poll seam (one that waits on ctx.Done) otherwise
// leave that goroutine running past the end of the test, and the seam's
// t.Cleanup then restores the package var WHILE the goroutine is still reading
// it — a data race under -race. Draining here keeps the suite honest.
func cancelAndDrainConnectFlow(t *testing.T, h *Handler, flowID string, drained <-chan struct{}) {
	t.Helper()
	// 404 is fine: a test that already cancelled its flow had it dropped from
	// the registry. The wait below is what matters either way.
	if _, code := connectDo(t, h.handleConnectFlowCancel, "DELETE", "/api/auth/connect/flows/"+flowID,
		map[string]string{"flowId": flowID}, nil); code != http.StatusOK && code != http.StatusNotFound {
		t.Fatalf("cancel flow %s: %d", flowID, code)
	}
	select {
	case <-drained:
	case <-time.After(5 * time.Second):
		t.Fatalf("connect flow %s: background exchange never returned after cancel", flowID)
	}
}

func TestConnectFlowInputWrongKind(t *testing.T) {
	h := NewHandler()
	preserveConnectCredential(t, "copilot")

	stubConnectSeam(t, &copilotStartFn, func() (auth.CopilotDevice, error) {
		return auth.CopilotDevice{DeviceCode: "dc-1", UserCode: "ABCD-EFGH", VerificationURI: "https://github.com/login/device"}, nil
	})
	drained := make(chan struct{})
	stubConnectSeam(t, &copilotPollFn, func(ctx context.Context, dev auth.CopilotDevice) (auth.Credential, error) {
		defer close(drained)
		<-ctx.Done()
		return auth.Credential{}, ctx.Err()
	})
	resp, code := connectDo(t, h.handleConnectOAuthStart, "POST", "/api/auth/connect/copilot/oauth/start", map[string]string{"provider": "copilot"}, map[string]string{"method": "oauth"})
	if code != http.StatusOK {
		t.Fatalf("start copilot flow: %d %v", code, resp)
	}
	flowID, _ := resp["flowId"].(string)
	t.Cleanup(func() { cancelAndDrainConnectFlow(t, h, flowID, drained) })

	// A device-code flow has no pasted-input stage.
	resp, code = connectDo(t, h.handleConnectFlowInput, "POST", "/api/auth/connect/flows/"+flowID+"/input", map[string]string{"flowId": flowID}, map[string]string{"code": "anything"})
	if code != http.StatusBadRequest {
		t.Fatalf("input on device-code flow: %d %v", code, resp)
	}
	// The message is kind-accurate now: the endpoint has no up-front
	// waiting_input check (it was the TOCTOU half of the double-click bug), so
	// a device-code flow falls through to the "does not accept pasted input"
	// branch. The rejection itself is unchanged and is what this pins.
	if !strings.Contains(fmt.Sprint(resp["error"]), "does not accept pasted input") {
		t.Fatalf("error = %v, want does not accept pasted input", resp["error"])
	}

	// Unknown flow ids are 404s.
	_, code = connectDo(t, h.handleConnectFlowStatus, "GET", "/api/auth/connect/flows/does-not-exist", map[string]string{"flowId": "does-not-exist"}, nil)
	if code != http.StatusNotFound {
		t.Fatalf("unknown flow status: %d, want 404", code)
	}
	_, code = connectDo(t, h.handleConnectFlowInput, "POST", "/api/auth/connect/flows/does-not-exist/input", map[string]string{"flowId": "does-not-exist"}, map[string]string{"code": "x"})
	if code != http.StatusNotFound {
		t.Fatalf("unknown flow input: %d, want 404", code)
	}
	_, code = connectDo(t, h.handleConnectFlowCancel, "DELETE", "/api/auth/connect/flows/does-not-exist", map[string]string{"flowId": "does-not-exist"}, nil)
	if code != http.StatusNotFound {
		t.Fatalf("unknown flow cancel: %d, want 404", code)
	}
}
