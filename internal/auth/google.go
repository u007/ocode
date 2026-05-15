package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// LoginWithGoogle performs an OAuth2 Authorization Code flow with PKCE.
// It starts a local callback server, opens the browser, waits up to 2 minutes
// for the user to complete login, then shuts down the server.
// The returned token is the real access token from Google, not a placeholder.
func LoginWithGoogle() (string, error) {
	clientID := os.Getenv("GOOGLE_CLIENT_ID")
	if clientID == "" {
		return "", fmt.Errorf("GOOGLE_CLIENT_ID environment variable not set")
	}

	// Generate PKCE verifier and challenge.
	verifier, err := generateCodeVerifier()
	if err != nil {
		return "", fmt.Errorf("failed to generate PKCE verifier: %w", err)
	}
	challenge := codeChallenge(verifier)

	state, err := generateState()
	if err != nil {
		return "", fmt.Errorf("failed to generate state: %w", err)
	}

	redirectURL := "http://localhost:8080/callback"

	params := url.Values{}
	params.Set("client_id", clientID)
	params.Set("redirect_uri", redirectURL)
	params.Set("response_type", "code")
	params.Set("scope", "https://www.googleapis.com/auth/userinfo.email openid")
	params.Set("code_challenge", challenge)
	params.Set("code_challenge_method", "S256")
	params.Set("state", state)
	authURL := "https://accounts.google.com/o/oauth2/v2/auth?" + params.Encode()

	tokenChan := make(chan string, 1)
	errChan := make(chan error, 1)

	mux := http.NewServeMux()
	server := &http.Server{Addr: ":8080", Handler: mux}

	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("state") != state {
			http.Error(w, "invalid state", http.StatusBadRequest)
			errChan <- fmt.Errorf("OAuth state mismatch — possible CSRF")
			return
		}

		code := r.URL.Query().Get("code")
		if code == "" {
			desc := r.URL.Query().Get("error_description")
			if desc == "" {
				desc = r.URL.Query().Get("error")
			}
			http.Error(w, "no code received", http.StatusBadRequest)
			errChan <- fmt.Errorf("no authorization code received: %s", desc)
			return
		}

		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintln(w, "<html><body><p>Authentication successful! You can close this window.</p></body></html>")

		tokenChan <- code
	})

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errChan <- fmt.Errorf("callback server error: %w", err)
		}
	}()

	fmt.Printf("Opening browser for Google login…\n")
	openBrowser(authURL)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	var code string
	select {
	case code = <-tokenChan:
	case err = <-errChan:
		server.Shutdown(context.Background()) //nolint:errcheck
		return "", err
	case <-ctx.Done():
		server.Shutdown(context.Background()) //nolint:errcheck
		return "", fmt.Errorf("login timed out after 2 minutes")
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	server.Shutdown(shutdownCtx) //nolint:errcheck

	// Exchange the authorization code for a token.
	// The caller must provide GOOGLE_CLIENT_SECRET for a confidential client,
	// or use a public client that accepts PKCE without a secret.
	clientSecret := os.Getenv("GOOGLE_CLIENT_SECRET")
	token, err := exchangeCodeForToken(clientID, clientSecret, code, verifier, redirectURL)
	if err != nil {
		return "", fmt.Errorf("token exchange failed: %w", err)
	}
	return token, nil
}

func exchangeCodeForToken(clientID, clientSecret, code, verifier, redirectURL string) (string, error) {
	form := url.Values{}
	form.Set("client_id", clientID)
	form.Set("code", code)
	form.Set("code_verifier", verifier)
	form.Set("grant_type", "authorization_code")
	form.Set("redirect_uri", redirectURL)
	if clientSecret != "" {
		form.Set("client_secret", clientSecret)
	}

	httpClient := &http.Client{Timeout: 15 * time.Second}
	resp, err := httpClient.PostForm("https://oauth2.googleapis.com/token", form)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token endpoint returned %d", resp.StatusCode)
	}

	// Parse just the access_token field without importing encoding/json
	// to keep dependencies minimal. Use a simple struct with json.
	buf := make([]byte, 4096)
	n, _ := resp.Body.Read(buf)
	body := string(buf[:n])

	// Extract access_token from JSON body using basic string search.
	const marker = `"access_token":"`
	idx := strings.Index(body, marker)
	if idx == -1 {
		return "", fmt.Errorf("access_token not found in token response")
	}
	rest := body[idx+len(marker):]
	end := strings.Index(rest, `"`)
	if end == -1 {
		return "", fmt.Errorf("malformed access_token in token response")
	}
	return rest[:end], nil
}

func generateCodeVerifier() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func codeChallenge(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

func generateState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// OpenURL is the exported alias of openBrowser for use by other auth flows.
func OpenURL(u string) { openBrowser(u) }

func openBrowser(u string) {
	var err error
	switch runtime.GOOS {
	case "linux":
		err = exec.Command("xdg-open", u).Start()
	case "windows":
		err = exec.Command("rundll32", "url.dll,FileProtocolHandler", u).Start()
	case "darwin":
		err = exec.Command("open", u).Start()
	default:
		err = fmt.Errorf("unsupported platform")
	}
	if err != nil {
		fmt.Printf("Please open this URL in your browser: %s\n", u)
	}
}
