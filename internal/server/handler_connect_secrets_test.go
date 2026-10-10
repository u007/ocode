package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The flow-status endpoint is polled by an unauthenticated-ish browser UI and
// its payload is what a client renders. A flow holds live OAuth material — the
// PKCE verifier and the state parameter — which together are enough to redeem
// an intercepted authorization code. None of it may cross the wire.
//
// Today the endpoint is safe BY CONSTRUCTION rather than by filtering: the flow
// struct keeps both fields unexported and the handler returns an explicit DTO
// built by snapshot(). These tests are the guard for that property, because the
// failure mode it prevents is invisible: adding a json tag to a struct field, or
// switching the handler to marshal the flow directly, would start leaking a
// live credential while every other flow test still passed.

// connectRawBody invokes a connect endpoint and returns the RAW response body,
// which connectDo cannot do because it decodes into a map.
func connectRawBody(t *testing.T, hf connectHandlerFunc, method, path string, pathValues map[string]string, body interface{}) (string, int) {
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
	return rec.Body.String(), rec.Code
}

func TestConnectFlowStatusNeverLeaksOAuthSecrets(t *testing.T) {
	h := NewHandler()
	preserveConnectCredential(t, "anthropic")
	flowID := startTestAnthropicFlow(t, h)

	// startTestAnthropicFlow pins state "st-1" and verifier "ver-1".
	raw, code := connectRawBody(t, h.handleConnectFlowStatus, "GET",
		"/api/auth/connect/flows/"+flowID, map[string]string{"flowId": flowID}, nil)
	if code != http.StatusOK {
		t.Fatalf("flow status: %d %s", code, raw)
	}

	for _, secret := range []string{"ver-1", "st-1"} {
		if strings.Contains(raw, secret) {
			t.Errorf("flow-status payload leaks %q: %s", secret, raw)
		}
	}

	// And the payload still carries what the UI actually needs.
	var got map[string]any
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("payload is not JSON: %v (%s)", err, raw)
	}
	for _, key := range []string{"flowId", "provider", "method", "kind", "state"} {
		if _, ok := got[key]; !ok {
			t.Errorf("flow-status payload is missing %q, which the UI needs: %s", key, raw)
		}
	}
}

// The start response is rendered directly by the browser, so it gets the same
// treatment. It carries the authorize URL (which contains the challenge and the
// state, both of which are meant to travel to the provider) but must not carry
// the verifier, which is what the provider's token endpoint checks against.
func TestConnectOAuthStartNeverLeaksVerifier(t *testing.T) {
	h := NewHandler()
	preserveConnectCredential(t, "anthropic")
	flowID := startTestAnthropicFlow(t, h)

	raw, code := connectRawBody(t, h.handleConnectOAuthStart, "POST",
		"/api/auth/connect/anthropic/oauth/start", map[string]string{"provider": "anthropic"},
		map[string]string{"method": "oauth_max"})
	if code != http.StatusOK {
		t.Fatalf("start flow: %d %s", code, raw)
	}
	if strings.Contains(raw, "ver-1") {
		t.Errorf("oauth/start response leaks the PKCE verifier: %s", raw)
	}
	// Sanity: the response is the flow we just asked for, not a stale one.
	var got map[string]any
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("payload is not JSON: %v", err)
	}
	if got["flowId"] != flowID && got["flowId"] == nil {
		t.Errorf("start response has no flowId: %s", raw)
	}
}
