package auth

import (
	"strings"
	"testing"
)

// Manual (paste-back) completion is the only OAuth mode that works when the
// browser and the server are NOT on the same machine: `serve --remote` behind
// SSH/WSL, or a browser on a second device. The loopback listener binds on the
// server, so the browser's redirect to localhost lands on the wrong host. These
// tests pin the parsing and the state check, which are the parts that can be
// got wrong silently.

func TestExchangeOpenAIManualParsesPastedCallbackURL(t *testing.T) {
	var gotCode, gotVerifier string
	prev := openaiManualExchangeFn
	openaiManualExchangeFn = func(code, verifier string) (Credential, error) {
		gotCode, gotVerifier = code, verifier
		return Credential{Kind: KindOAuth, AccessToken: "at-manual"}, nil
	}
	t.Cleanup(func() { openaiManualExchangeFn = prev })

	flow := OpenAIManualFlow{AuthURL: "https://auth.openai.com/oauth/authorize?x=1", State: "st-manual", Verifier: "ver-manual"}
	cred, err := ExchangeOpenAIManual(flow, "http://localhost:1455/auth/callback?code=code-manual&state=st-manual")
	if err != nil {
		t.Fatalf("ExchangeOpenAIManual: %v", err)
	}
	if gotCode != "code-manual" {
		t.Errorf("exchanged code = %q, want code-manual", gotCode)
	}
	// The verifier is what the token endpoint checks the challenge against; it
	// must come from the flow, never from the pasted URL.
	if gotVerifier != "ver-manual" {
		t.Errorf("exchanged verifier = %q, want ver-manual", gotVerifier)
	}
	if cred.AccessToken != "at-manual" {
		t.Errorf("access token = %q, want at-manual", cred.AccessToken)
	}
}

// A bare code is REJECTED, deliberately. It carries no state, so accepting it
// would mean exchanging a code whose origin cannot be checked — which is
// precisely the attack the state parameter exists to stop: anyone who can get
// the user to paste a code of their choosing would bind their account to this
// machine. Being strict here costs the user one extra copy (the whole redirect
// URL) and closes the hole.
//
// This test started out asserting the opposite ("accept a bare code, providers
// show one"). The implementation refused, and on reflection the implementation
// was right — so the expectation was corrected rather than the check weakened.
func TestExchangeOpenAIManualRejectsBareCodeWithoutState(t *testing.T) {
	called := false
	prev := openaiManualExchangeFn
	openaiManualExchangeFn = func(code, verifier string) (Credential, error) {
		called = true
		return Credential{}, nil
	}
	t.Cleanup(func() { openaiManualExchangeFn = prev })

	flow := OpenAIManualFlow{State: "st-manual", Verifier: "ver-manual"}
	_, err := ExchangeOpenAIManual(flow, "bare-code-123")
	if err == nil {
		t.Fatal("expected an error for a bare code carrying no state")
	}
	if !strings.Contains(err.Error(), "state") {
		t.Errorf("error = %v, want it to explain the state requirement", err)
	}
	if called {
		t.Error("token exchange ran for an unverifiable bare code")
	}
}

// A state mismatch means the pasted URL came from a different sign-in attempt.
// It must NOT exchange, or an attacker who can make the user paste a URL of
// their choosing can bind their account to this machine.
func TestExchangeOpenAIManualRejectsStateMismatch(t *testing.T) {
	called := false
	prev := openaiManualExchangeFn
	openaiManualExchangeFn = func(code, verifier string) (Credential, error) {
		called = true
		return Credential{}, nil
	}
	t.Cleanup(func() { openaiManualExchangeFn = prev })

	flow := OpenAIManualFlow{State: "st-manual", Verifier: "ver-manual"}
	_, err := ExchangeOpenAIManual(flow, "http://localhost:1455/auth/callback?code=c&state=st-ATTACKER")
	if err == nil {
		t.Fatal("expected an error for a mismatched state")
	}
	if !strings.Contains(err.Error(), "state") {
		t.Errorf("error = %v, want it to mention state", err)
	}
	if called {
		t.Error("token exchange ran despite a state mismatch")
	}
}

// The provider reports user-facing failures in the redirect itself.
func TestExchangeOpenAIManualSurfacesProviderError(t *testing.T) {
	called := false
	prev := openaiManualExchangeFn
	openaiManualExchangeFn = func(code, verifier string) (Credential, error) {
		called = true
		return Credential{}, nil
	}
	t.Cleanup(func() { openaiManualExchangeFn = prev })

	flow := OpenAIManualFlow{State: "st-manual", Verifier: "ver-manual"}
	_, err := ExchangeOpenAIManual(flow, "http://localhost:1455/auth/callback?error=access_denied&error_description=User+declined")
	if err == nil {
		t.Fatal("expected an error for a provider error redirect")
	}
	if !strings.Contains(err.Error(), "access_denied") {
		t.Errorf("error = %v, want it to name access_denied", err)
	}
	if called {
		t.Error("token exchange ran despite a provider error")
	}
}

func TestExchangeOpenAIManualRejectsEmptyInput(t *testing.T) {
	flow := OpenAIManualFlow{State: "st-manual", Verifier: "ver-manual"}
	for _, in := range []string{"", "   ", "http://localhost:1455/auth/callback?state=st-manual"} {
		if _, err := ExchangeOpenAIManual(flow, in); err == nil {
			t.Errorf("input %q: expected an error, got nil", in)
		}
	}
}

// The manual start must not bind the loopback port — that is the whole point —
// and must still produce a usable authorize URL carrying a fresh state.
func TestStartOpenAIOAuthManualProducesURLWithoutBinding(t *testing.T) {
	flow, err := StartOpenAIOAuthManual()
	if err != nil {
		t.Fatalf("StartOpenAIOAuthManual: %v", err)
	}
	if !strings.HasPrefix(flow.AuthURL, "https://auth.openai.com/oauth/authorize") {
		t.Errorf("authorize URL = %q, want the OpenAI authorize endpoint", flow.AuthURL)
	}
	if flow.State == "" || flow.Verifier == "" {
		t.Fatalf("manual flow missing PKCE material: state=%q verifier=%q", flow.State, flow.Verifier)
	}
	if !strings.Contains(flow.AuthURL, "state="+flow.State) {
		t.Errorf("authorize URL does not carry the flow state: %q", flow.AuthURL)
	}

	other, err := StartOpenAIOAuthManual()
	if err != nil {
		t.Fatalf("second StartOpenAIOAuthManual: %v", err)
	}
	if other.State == flow.State || other.Verifier == flow.Verifier {
		t.Error("two manual flows reused the same PKCE material")
	}
}
