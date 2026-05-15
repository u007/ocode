package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// OpenAI ChatGPT (Codex) OAuth constants — ported from the openai/codex CLI.
const (
	openaiClientID     = "app_EMoamEEZ73f0CkXaXp7hrann"
	openaiAuthorizeURL = "https://auth.openai.com/oauth/authorize"
	openaiTokenURL     = "https://auth.openai.com/oauth/token"
	openaiRedirectURI  = "http://localhost:1455/auth/callback"
	openaiScope        = "openid profile email offline_access api.connectors.read api.connectors.invoke"
	openaiLoopbackPort = 1455
)

// OpenAILogin runs the full OAuth flow: spins up a localhost callback server,
// opens the browser, blocks until the user completes login (or `ctx` cancels),
// then exchanges the code for tokens.
func OpenAILogin(ctx context.Context) (Credential, error) {
	pkce, err := NewPKCE()
	if err != nil {
		return Credential{}, err
	}
	state, err := RandomState()
	if err != nil {
		return Credential{}, err
	}

	authURL, err := buildOpenAIAuthorizeURL(pkce.Challenge, state)
	if err != nil {
		return Credential{}, err
	}

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)

	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", openaiLoopbackPort))
	if err != nil {
		return Credential{}, fmt.Errorf("bind localhost:%d for openai callback: %w", openaiLoopbackPort, err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/auth/callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if errParam := q.Get("error"); errParam != "" {
			errCh <- fmt.Errorf("openai oauth error: %s — %s", errParam, q.Get("error_description"))
			http.Error(w, "auth failed", http.StatusBadRequest)
			return
		}
		gotState := q.Get("state")
		code := q.Get("code")
		if gotState != state {
			errCh <- fmt.Errorf("openai oauth state mismatch")
			http.Error(w, "state mismatch", http.StatusBadRequest)
			return
		}
		if code == "" {
			errCh <- fmt.Errorf("openai oauth missing code")
			http.Error(w, "missing code", http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`<!doctype html><html><body style="font-family:system-ui;padding:40px"><h2>Signed in to ocode ✓</h2><p>You can close this tab.</p></body></html>`))
		codeCh <- code
	})

	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(listener) }()
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	openBrowser(authURL)

	select {
	case <-ctx.Done():
		return Credential{}, ctx.Err()
	case err := <-errCh:
		return Credential{}, err
	case code := <-codeCh:
		return openaiExchangeCode(code, pkce.Verifier)
	}
}

func buildOpenAIAuthorizeURL(challenge, state string) (string, error) {
	u, err := url.Parse(openaiAuthorizeURL)
	if err != nil {
		return "", fmt.Errorf("parse openai authorize url: %w", err)
	}
	q := u.Query()
	q.Set("response_type", "code")
	q.Set("client_id", openaiClientID)
	q.Set("redirect_uri", openaiRedirectURI)
	q.Set("scope", openaiScope)
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	q.Set("state", state)
	q.Set("id_token_add_organizations", "true")
	q.Set("codex_cli_simplified_flow", "true")
	q.Set("originator", "codex_cli_rs")
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func openaiExchangeCode(code, verifier string) (Credential, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("client_id", openaiClientID)
	form.Set("code", code)
	form.Set("code_verifier", verifier)
	form.Set("redirect_uri", openaiRedirectURI)
	return openaiTokenRequest(form)
}

// OpenAIRefresh swaps a refresh token for a fresh access token.
func OpenAIRefresh(refresh string) (Credential, error) {
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("client_id", openaiClientID)
	form.Set("refresh_token", refresh)
	return openaiTokenRequest(form)
}

func openaiTokenRequest(form url.Values) (Credential, error) {
	req, err := http.NewRequest("POST", openaiTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return Credential{}, fmt.Errorf("build openai token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return Credential{}, fmt.Errorf("openai token request: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return Credential{}, fmt.Errorf("openai token exchange failed: %d %s", resp.StatusCode, string(body))
	}
	var parsed struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return Credential{}, fmt.Errorf("decode openai token response: %w", err)
	}
	if parsed.AccessToken == "" {
		return Credential{}, fmt.Errorf("openai token response missing access_token")
	}
	return Credential{
		Kind:         KindOAuth,
		AccessToken:  parsed.AccessToken,
		RefreshToken: parsed.RefreshToken,
		ExpiresAt:    time.Now().Unix() + parsed.ExpiresIn,
	}, nil
}
