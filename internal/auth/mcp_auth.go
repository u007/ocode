package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

type MCPAuthToken struct {
	AccessToken  string   `json:"access_token"`
	RefreshToken string   `json:"refresh_token,omitempty"`
	TokenType    string   `json:"token_type"`
	Expiry       int64    `json:"expiry"`
	Scopes       []string `json:"scopes,omitempty"`
}

type mcpAuthFile struct {
	Tokens map[string]MCPAuthToken `json:"tokens"`
}

var (
	mcpAuthMu sync.Mutex
	mcpCache  *mcpAuthFile
)

func mcpAuthPath() (string, error) {
	if runtime.GOOS == "windows" {
		appdata := os.Getenv("APPDATA")
		if appdata == "" {
			return "", fmt.Errorf("APPDATA not set")
		}
		return filepath.Join(appdata, "opencode", "mcp-auth.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "opencode", "mcp-auth.json"), nil
	}
	return filepath.Join(home, ".local", "share", "opencode", "mcp-auth.json"), nil
}

func loadMCPAuthLocked() error {
	if mcpCache != nil {
		return nil
	}
	path, err := mcpAuthPath()
	if err != nil {
		return fmt.Errorf("resolve mcp auth path: %w", err)
	}
	mcpCache = &mcpAuthFile{Tokens: map[string]MCPAuthToken{}}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read %s: %w", path, err)
	}
	if err := json.Unmarshal(data, mcpCache); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	if mcpCache.Tokens == nil {
		mcpCache.Tokens = map[string]MCPAuthToken{}
	}
	return nil
}

func persistMCPAuthLocked() error {
	path, err := mcpAuthPath()
	if err != nil {
		return fmt.Errorf("resolve mcp auth path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create mcp auth dir: %w", err)
	}
	data, err := json.MarshalIndent(mcpCache, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal mcp auth: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write mcp auth tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename mcp auth file: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("chmod mcp auth file: %w", err)
	}
	return nil
}

func GetMCPAuth(name string) (MCPAuthToken, bool) {
	mcpAuthMu.Lock()
	defer mcpAuthMu.Unlock()
	if err := loadMCPAuthLocked(); err != nil {
		return MCPAuthToken{}, false
	}
	t, ok := mcpCache.Tokens[name]
	return t, ok
}

func SetMCPAuth(name string, token MCPAuthToken) error {
	mcpAuthMu.Lock()
	defer mcpAuthMu.Unlock()
	if err := loadMCPAuthLocked(); err != nil {
		return err
	}
	mcpCache.Tokens[name] = token
	return persistMCPAuthLocked()
}

func DeleteMCPAuth(name string) error {
	mcpAuthMu.Lock()
	defer mcpAuthMu.Unlock()
	if err := loadMCPAuthLocked(); err != nil {
		return err
	}
	delete(mcpCache.Tokens, name)
	return persistMCPAuthLocked()
}

func (t MCPAuthToken) IsExpired() bool {
	return time.Now().Unix() >= t.Expiry
}

func (t MCPAuthToken) AuthorizationHeader() string {
	tokenType := t.TokenType
	if tokenType == "" {
		tokenType = "Bearer"
	}
	return tokenType + " " + t.AccessToken
}

func MCPAuthFlow(serverName, authURL, tokenURL, clientID string, scopes []string) error {
	pkce, err := NewPKCE()
	if err != nil {
		return fmt.Errorf("generate pkce: %w", err)
	}
	state, err := RandomState()
	if err != nil {
		return fmt.Errorf("generate state: %w", err)
	}

	redirectURL := "http://localhost:8085/mcp-callback"

	params := url.Values{}
	params.Set("client_id", clientID)
	params.Set("redirect_uri", redirectURL)
	params.Set("response_type", "code")
	params.Set("code_challenge", pkce.Challenge)
	params.Set("code_challenge_method", "S256")
	params.Set("state", state)
	if len(scopes) > 0 {
		params.Set("scope", strings.Join(scopes, " "))
	}
	fullAuthURL := authURL + "?" + params.Encode()

	codeChan := make(chan string, 1)
	errChan := make(chan error, 1)

	mux := http.NewServeMux()
	server := &http.Server{Addr: "127.0.0.1:8085", Handler: mux}

	mux.HandleFunc("/mcp-callback", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("state") != state {
			http.Error(w, "invalid state", http.StatusBadRequest)
			errChan <- fmt.Errorf("oauth state mismatch")
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" {
			desc := r.URL.Query().Get("error_description")
			if desc == "" {
				desc = r.URL.Query().Get("error")
			}
			http.Error(w, "no code received", http.StatusBadRequest)
			errChan <- fmt.Errorf("no auth code: %s", desc)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintln(w, "<html><body><p>MCP authentication successful! You can close this window.</p></body></html>")
		codeChan <- code
	})

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errChan <- fmt.Errorf("callback server error: %w", err)
		}
	}()

	fmt.Printf("Opening browser for MCP server %s authentication…\n", serverName)
	openBrowser(fullAuthURL)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	var code string
	select {
	case code = <-codeChan:
	case err = <-errChan:
		server.Shutdown(context.Background())
		return err
	case <-ctx.Done():
		server.Shutdown(context.Background())
		return fmt.Errorf("mcp auth timed out after 2 minutes")
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	server.Shutdown(shutdownCtx)

	token, err := exchangeMCPCodeForToken(clientID, code, pkce.Verifier, redirectURL, tokenURL)
	if err != nil {
		return fmt.Errorf("token exchange failed: %w", err)
	}

	return SetMCPAuth(serverName, token)
}

func exchangeMCPCodeForToken(clientID, code, verifier, redirectURL, tokenURL string) (MCPAuthToken, error) {
	form := url.Values{}
	form.Set("client_id", clientID)
	form.Set("code", code)
	form.Set("code_verifier", verifier)
	form.Set("grant_type", "authorization_code")
	form.Set("redirect_uri", redirectURL)

	httpClient := &http.Client{Timeout: 15 * time.Second}
	resp, err := httpClient.PostForm(tokenURL, form)
	if err != nil {
		return MCPAuthToken{}, fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return MCPAuthToken{}, fmt.Errorf("token endpoint returned %d", resp.StatusCode)
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int64  `json:"expires_in"`
		Scope        string `json:"scope"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return MCPAuthToken{}, fmt.Errorf("parse token response: %w", err)
	}

	var scopes []string
	if tokenResp.Scope != "" {
		scopes = strings.Split(tokenResp.Scope, " ")
	}

	expiresIn := tokenResp.ExpiresIn
	if expiresIn == 0 {
		expiresIn = 3600
	}

	return MCPAuthToken{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		TokenType:    tokenResp.TokenType,
		Expiry:       time.Now().Unix() + expiresIn,
		Scopes:       scopes,
	}, nil
}

func RefreshMCPAuthToken(serverName, tokenURL, clientID string, token MCPAuthToken) (MCPAuthToken, error) {
	if token.RefreshToken == "" {
		return MCPAuthToken{}, fmt.Errorf("no refresh token available")
	}

	form := url.Values{}
	form.Set("client_id", clientID)
	form.Set("refresh_token", token.RefreshToken)
	form.Set("grant_type", "refresh_token")

	httpClient := &http.Client{Timeout: 15 * time.Second}
	resp, err := httpClient.PostForm(tokenURL, form)
	if err != nil {
		return MCPAuthToken{}, fmt.Errorf("refresh request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return MCPAuthToken{}, fmt.Errorf("token refresh endpoint returned %d", resp.StatusCode)
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int64  `json:"expires_in"`
		Scope        string `json:"scope"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return MCPAuthToken{}, fmt.Errorf("parse refresh token response: %w", err)
	}

	var scopes []string
	if tokenResp.Scope != "" {
		scopes = strings.Split(tokenResp.Scope, " ")
	}

	expiresIn := tokenResp.ExpiresIn
	if expiresIn == 0 {
		expiresIn = 3600
	}

	newToken := MCPAuthToken{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		TokenType:    tokenResp.TokenType,
		Expiry:       time.Now().Unix() + expiresIn,
		Scopes:       scopes,
	}

	if err := SetMCPAuth(serverName, newToken); err != nil {
		return MCPAuthToken{}, fmt.Errorf("save refreshed token: %w", err)
	}

	return newToken, nil
}
