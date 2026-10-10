package auth

import (
	"fmt"
	"net/url"
	"strings"
)

// Manual (paste-back) OAuth completion for ChatGPT.
//
// StartOpenAIOAuth binds a loopback listener on the machine running ocode and
// waits for the provider to redirect the BROWSER to localhost:1455. That only
// works when the browser and ocode share a host — so it fails against a
// `serve --remote` server behind SSH/WSL, and from a browser on a second device,
// where the redirect lands on the browser's own localhost and never reaches
// ocode. Manual completion removes the listener entirely: the user opens the
// authorize URL, finishes signing in, and pastes back whatever the browser was
// redirected to (or just the code). Nothing has to listen on any port.
//
// This file is deliberately separate from openai_oauth.go: the auto mode and
// the manual mode share the URL builder and the token exchange, but only the
// manual mode can be driven from a server that is not on the user's machine.

// OpenAIManualFlow carries everything a pasted-callback exchange needs. The
// State and Verifier stay server-side: the browser is handed AuthURL only, so
// it can never learn the PKCE verifier that the token endpoint checks.
type OpenAIManualFlow struct {
	// AuthURL is what the user opens to sign in.
	AuthURL string
	// State is the CSRF value the provider echoes back on the redirect.
	State string
	// Verifier is the PKCE secret exchanged for the token.
	Verifier string
}

// openaiManualExchangeFn is a seam so tests can complete a flow without
// contacting the token endpoint. Same pattern as the connect handlers' seams.
var openaiManualExchangeFn = openaiExchangeCode

// StartOpenAIOAuthManual builds the ChatGPT authorize URL WITHOUT binding a
// port. The caller opens AuthURL, then hands the pasted redirect to
// ExchangeOpenAIManual. Nothing listens, so the browser and ocode need not
// share a host.
func StartOpenAIOAuthManual() (OpenAIManualFlow, error) {
	pkce, err := NewPKCE()
	if err != nil {
		return OpenAIManualFlow{}, err
	}
	state, err := RandomState()
	if err != nil {
		return OpenAIManualFlow{}, err
	}
	authURL, err := buildOpenAIAuthorizeURL(pkce.Challenge, state)
	if err != nil {
		return OpenAIManualFlow{}, err
	}
	return OpenAIManualFlow{AuthURL: authURL, State: state, Verifier: pkce.Verifier}, nil
}

// ExchangeOpenAIManual completes a manual flow from the value the user pasted.
//
// pasted is the full redirect URL the browser was sent to, including a
// localhost origin it never actually reached. A bare authorization code is
// refused: it carries no state, so there is nothing to validate it against.
//
// The state check is load-bearing: without it, anyone who can get the user to
// paste a URL of their choosing could bind the attacker's account to this
// machine. A mismatch returns an error and never contacts the token endpoint.
func ExchangeOpenAIManual(f OpenAIManualFlow, pasted string) (Credential, error) {
	code, state, err := parsePastedCallback(pasted)
	if err != nil {
		return Credential{}, err
	}
	if state == "" || state != f.State {
		return Credential{}, fmt.Errorf("openai oauth state mismatch: the pasted value has no state (paste the whole redirect URL, not just the code), or it came from a different sign-in attempt")
	}
	if code == "" {
		return Credential{}, fmt.Errorf("openai oauth missing code in the pasted value")
	}
	return openaiManualExchangeFn(code, f.Verifier)
}

// parsePastedCallback extracts (code, state) from a pasted redirect URL or a
// bare code. A provider-reported error is returned as an error so the UI shows
// the real reason instead of a confusing "missing code".
func parsePastedCallback(pasted string) (code, state string, err error) {
	trimmed := strings.TrimSpace(pasted)
	if trimmed == "" {
		return "", "", fmt.Errorf("nothing pasted: copy the URL your browser was redirected to")
	}

	if !strings.HasPrefix(trimmed, "http://") && !strings.HasPrefix(trimmed, "https://") {
		// A bare code. It carries no state, so it cannot be validated against
		// this flow and ExchangeOpenAIManual rejects it — accepting it would
		// let an attacker-supplied code be redeemed on this machine.
		return trimmed, "", nil
	}

	u, parseErr := url.Parse(trimmed)
	if parseErr != nil {
		return "", "", fmt.Errorf("pasted value is not a URL: %w", parseErr)
	}
	q := u.Query()
	if errParam := q.Get("error"); errParam != "" {
		if desc := q.Get("error_description"); desc != "" {
			return "", "", fmt.Errorf("openai oauth error: %s — %s", errParam, desc)
		}
		return "", "", fmt.Errorf("openai oauth error: %s", errParam)
	}
	return q.Get("code"), q.Get("state"), nil
}
