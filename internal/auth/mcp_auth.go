package auth

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/u007/ocode/internal/filelock"
)

type MCPAuthToken struct {
	AccessToken  string   `json:"access_token"`
	RefreshToken string   `json:"refresh_token,omitempty"`
	TokenType    string   `json:"token_type"`
	Expiry       int64    `json:"expiry"`
	Scopes       []string `json:"scopes,omitempty"`
	ClientID     string   `json:"-"`
	ServerURL    string   `json:"-"`
}

type mcpAuthFile map[string]json.RawMessage

var (
	mcpAuthMu sync.Mutex
	mcpCache  mcpAuthFile
)

const (
	mcpAuthLockTimeout      = 30 * time.Second
	nativeMCPAuthStorageKey = "tokens"
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
	loaded, err := readMCPAuthFile(path)
	if err != nil {
		return err
	}
	mcpCache = loaded
	return nil
}

func readMCPAuthFile(path string) (mcpAuthFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return make(mcpAuthFile), nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var authFile mcpAuthFile
	if err := json.Unmarshal(data, &authFile); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if authFile == nil {
		authFile = make(mcpAuthFile)
	}
	return authFile, nil
}

func updateMCPAuthFileLocked(update func(mcpAuthFile) error) error {
	path, err := mcpAuthPath()
	if err != nil {
		return fmt.Errorf("resolve mcp auth path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create mcp auth dir: %w", err)
	}
	if err := withMCPAuthFileLock(path, func() error {
		latest, err := readMCPAuthFile(path)
		if err != nil {
			return err
		}
		if err := update(latest); err != nil {
			return err
		}
		if err := writeMCPAuthFile(path, latest); err != nil {
			return err
		}
		mcpCache = latest
		return nil
	}); err != nil {
		return err
	}
	return nil
}

func withMCPAuthFileLock(path string, fn func() error) error {
	return filelock.WithFileLockTimeout(path+".lock", mcpAuthLockTimeout, fn)
}

func writeMCPAuthFile(path string, authFile mcpAuthFile) error {
	data, err := json.MarshalIndent(authFile, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal mcp auth: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".mcp-auth-*.tmp")
	if err != nil {
		return fmt.Errorf("create mcp auth temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod mcp auth temp file: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write mcp auth temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync mcp auth temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close mcp auth temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
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
	if token, ok := upstreamMCPAuthToken(mcpCache[name]); ok {
		return token, true
	}
	nativeTokens, ok := rawObject(mcpCache[nativeMCPAuthStorageKey])
	if !ok {
		return MCPAuthToken{}, false
	}
	return nativeMCPAuthToken(nativeTokens[name])
}

// GetNativeMCPAuth reads only ocode's legacy native token schema. Callers that
// have a configured remote URL use this instead of GetMCPAuth so an upstream
// OpenCode credential bound to a different server can never be sent as a
// static-OAuth fallback.
func GetNativeMCPAuth(name string) (MCPAuthToken, bool) {
	mcpAuthMu.Lock()
	defer mcpAuthMu.Unlock()
	if err := loadMCPAuthLocked(); err != nil {
		return MCPAuthToken{}, false
	}
	nativeTokens, ok := rawObject(mcpCache[nativeMCPAuthStorageKey])
	if !ok {
		return MCPAuthToken{}, false
	}
	return nativeMCPAuthToken(nativeTokens[name])
}

func SetMCPAuth(name string, token MCPAuthToken) error {
	mcpAuthMu.Lock()
	defer mcpAuthMu.Unlock()
	return updateMCPAuthFileLocked(func(latest mcpAuthFile) error {
		return setNativeMCPAuthToken(latest, name, token)
	})
}

// GetMCPAuthForServer loads an upstream OpenCode credential only when its
// saved server URL exactly matches the configured remote MCP URL.
func GetMCPAuthForServer(name, serverURL string) (MCPAuthToken, bool) {
	if serverURL == "" || name == nativeMCPAuthStorageKey {
		return MCPAuthToken{}, false
	}

	mcpAuthMu.Lock()
	defer mcpAuthMu.Unlock()
	if err := loadMCPAuthLocked(); err != nil {
		return MCPAuthToken{}, false
	}
	token, ok := upstreamMCPAuthToken(mcpCache[name])
	return token, ok && token.ServerURL == serverURL
}

// SetMCPAuthForServer stores a credential in the upstream OpenCode schema,
// preserving unrelated top-level entries and unknown metadata in the entry.
func SetMCPAuthForServer(name, serverURL string, token MCPAuthToken) error {
	if name == "" || serverURL == "" {
		return fmt.Errorf("MCP auth server name and URL are required")
	}
	if name == nativeMCPAuthStorageKey {
		return fmt.Errorf("MCP auth server name %q is reserved", name)
	}

	mcpAuthMu.Lock()
	defer mcpAuthMu.Unlock()
	return updateMCPAuthFileLocked(func(latest mcpAuthFile) error {
		return setUpstreamMCPAuthToken(latest, name, serverURL, token)
	})
}

func DeleteMCPAuth(name string) error {
	mcpAuthMu.Lock()
	defer mcpAuthMu.Unlock()
	return updateMCPAuthFileLocked(func(latest mcpAuthFile) error {
		if name != nativeMCPAuthStorageKey {
			delete(latest, name)
		}
		nativeTokens, ok := rawObject(latest[nativeMCPAuthStorageKey])
		if !ok {
			return nil
		}
		delete(nativeTokens, name)
		if len(nativeTokens) == 0 {
			delete(latest, nativeMCPAuthStorageKey)
			return nil
		}
		return setRawJSON(latest, nativeMCPAuthStorageKey, nativeTokens)
	})
}

func setNativeMCPAuthToken(authFile mcpAuthFile, name string, token MCPAuthToken) error {
	nativeTokens, ok := rawObject(authFile[nativeMCPAuthStorageKey])
	if !ok {
		nativeTokens = make(map[string]json.RawMessage)
	}
	encoded, err := json.Marshal(token)
	if err != nil {
		return fmt.Errorf("marshal native MCP auth token: %w", err)
	}
	nativeTokens[name] = encoded
	encodedTokens, err := json.Marshal(nativeTokens)
	if err != nil {
		return fmt.Errorf("marshal native MCP auth tokens: %w", err)
	}
	authFile[nativeMCPAuthStorageKey] = encodedTokens
	return nil
}

func setUpstreamMCPAuthToken(authFile mcpAuthFile, name, serverURL string, token MCPAuthToken) error {
	if name == nativeMCPAuthStorageKey {
		return fmt.Errorf("MCP auth server name %q is reserved", name)
	}
	entry, ok := rawObject(authFile[name])
	if !ok {
		entry = make(map[string]json.RawMessage)
	}
	tokens, ok := rawObject(entry[nativeMCPAuthStorageKey])
	if !ok {
		tokens = make(map[string]json.RawMessage)
	}
	if err := setRawJSON(tokens, "accessToken", token.AccessToken); err != nil {
		return err
	}
	if err := setRawJSON(tokens, "refreshToken", token.RefreshToken); err != nil {
		return err
	}
	if err := setRawJSON(tokens, "tokenType", cmp.Or(token.TokenType, "Bearer")); err != nil {
		return err
	}
	if err := setRawJSON(tokens, "expiresAt", token.Expiry); err != nil {
		return err
	}
	if err := setRawJSON(tokens, "scope", strings.Join(token.Scopes, " ")); err != nil {
		return err
	}

	clientInfo, ok := rawObject(entry["clientInfo"])
	if !ok {
		clientInfo = make(map[string]json.RawMessage)
	}
	if err := setRawJSON(clientInfo, "clientId", token.ClientID); err != nil {
		return err
	}
	if err := setRawJSON(entry, nativeMCPAuthStorageKey, tokens); err != nil {
		return err
	}
	if err := setRawJSON(entry, "clientInfo", clientInfo); err != nil {
		return err
	}
	if err := setRawJSON(entry, "serverUrl", serverURL); err != nil {
		return err
	}
	return setRawJSON(authFile, name, entry)
}

func rawObject(raw json.RawMessage) (map[string]json.RawMessage, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil || object == nil {
		return nil, false
	}
	return object, true
}

func setRawJSON[T any](object map[string]json.RawMessage, key string, value T) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal MCP auth field %q: %w", key, err)
	}
	object[key] = encoded
	return nil
}

func upstreamMCPAuthToken(entryRaw json.RawMessage) (MCPAuthToken, bool) {
	entry, ok := rawObject(entryRaw)
	if !ok {
		return MCPAuthToken{}, false
	}
	tokens, ok := rawObject(entry["tokens"])
	if !ok {
		return MCPAuthToken{}, false
	}
	token, ok := upstreamTokenFromJSON(tokens)
	if !ok {
		return MCPAuthToken{}, false
	}
	if clientInfo, ok := rawObject(entry["clientInfo"]); ok {
		token.ClientID = rawString(clientInfo["clientId"])
	}
	token.ServerURL = rawString(entry["serverUrl"])
	return token, true
}

func nativeMCPAuthToken(raw json.RawMessage) (MCPAuthToken, bool) {
	if _, ok := rawObject(raw); !ok {
		return MCPAuthToken{}, false
	}
	var token MCPAuthToken
	if err := json.Unmarshal(raw, &token); err != nil {
		return MCPAuthToken{}, false
	}
	return token, true
}

func upstreamTokenFromJSON(tokens map[string]json.RawMessage) (MCPAuthToken, bool) {
	accessToken := rawString(tokens["accessToken"])
	refreshToken := rawString(tokens["refreshToken"])
	if accessToken == "" && refreshToken == "" {
		return MCPAuthToken{}, false
	}
	scopes := strings.Fields(rawString(tokens["scope"]))
	return MCPAuthToken{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		TokenType:    cmp.Or(rawString(tokens["tokenType"]), "Bearer"),
		Expiry:       rawInt64(tokens["expiresAt"]),
		Scopes:       scopes,
	}, true
}

func rawString(raw json.RawMessage) string {
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return ""
	}
	return value
}

func rawInt64(raw json.RawMessage) int64 {
	text := strings.Trim(strings.TrimSpace(string(raw)), `"`)
	if text == "" {
		return 0
	}
	if value, err := strconv.ParseInt(text, 10, 64); err == nil {
		return value
	}
	value, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return 0
	}
	return int64(value)
}

func (t MCPAuthToken) IsExpired() bool {
	return time.Now().Unix() >= t.Expiry
}

func (t MCPAuthToken) AuthorizationHeader() string {
	if t.AccessToken == "" {
		return ""
	}
	tokenType := t.TokenType
	if tokenType == "" {
		tokenType = "Bearer"
	}
	return tokenType + " " + t.AccessToken
}

func MCPAuthFlow(serverName, authURL, tokenURL, clientID string, scopes []string) error {
	if err := validateMCPOAuthEndpoint(authURL); err != nil {
		return fmt.Errorf("mcp authorization endpoint is invalid: %w", err)
	}
	if err := validateMCPOAuthEndpoint(tokenURL); err != nil {
		return fmt.Errorf("mcp token endpoint is invalid: %w", err)
	}
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

	log.Printf("Opening browser for MCP server %s authentication…", serverName)
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
	if err := validateMCPOAuthEndpoint(tokenURL); err != nil {
		return MCPAuthToken{}, fmt.Errorf("token endpoint is invalid: %w", err)
	}
	form := url.Values{}
	form.Set("client_id", clientID)
	form.Set("code", code)
	form.Set("code_verifier", verifier)
	form.Set("grant_type", "authorization_code")
	form.Set("redirect_uri", redirectURL)

	req, err := http.NewRequest(http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return MCPAuthToken{}, fmt.Errorf("create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	httpClient := &http.Client{
		Timeout: 15 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return fmt.Errorf("redirects are not allowed for MCP OAuth code exchange")
		},
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		if resp != nil {
			_ = resp.Body.Close()
		}
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
	newToken, err := requestMCPRefresh(tokenURL, clientID, token)
	if err != nil {
		return MCPAuthToken{}, err
	}
	if err := SetMCPAuth(serverName, newToken); err != nil {
		return MCPAuthToken{}, fmt.Errorf("save refreshed token: %w", err)
	}
	return newToken, nil
}

// MCPRefreshError reports a refresh failure without including the endpoint,
// client identifier, or any credential material in its message.
type MCPRefreshError struct {
	StatusCode int
	Reason     string
}

func (e *MCPRefreshError) Error() string {
	if e.StatusCode > 0 {
		return fmt.Sprintf("MCP token refresh failed (HTTP %d): %s", e.StatusCode, e.Reason)
	}
	return "MCP token refresh failed: " + e.Reason
}

// RefreshMCPAuthTokenForServer refreshes and persists a URL-bound upstream
// credential without replacing unrelated mcp-auth.json entries.
func RefreshMCPAuthTokenForServer(serverName, serverURL, tokenURL, clientID string, token MCPAuthToken) (MCPAuthToken, error) {
	if token.ServerURL != serverURL {
		return MCPAuthToken{}, &MCPRefreshError{Reason: "stored server binding does not match configured URL"}
	}

	mcpAuthMu.Lock()
	defer mcpAuthMu.Unlock()
	path, err := mcpAuthPath()
	if err != nil {
		return MCPAuthToken{}, &MCPRefreshError{Reason: "auth storage path is unavailable"}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return MCPAuthToken{}, &MCPRefreshError{Reason: "auth storage directory is unavailable"}
	}

	var refreshed MCPAuthToken
	err = withMCPAuthFileLock(path, func() error {
		latest, err := readMCPAuthFile(path)
		if err != nil {
			return &MCPRefreshError{Reason: "latest auth storage could not be read"}
		}
		current, hasCurrent := upstreamMCPAuthToken(latest[serverName])
		if !hasCurrent || current.ServerURL != serverURL {
			return &MCPRefreshError{Reason: "stored server binding changed or was removed"}
		}
		if !current.IsExpired() {
			refreshed = current
			mcpCache = latest
			return nil
		}
		token = current
		clientID = cmp.Or(current.ClientID, clientID)
		if token.RefreshToken == "" {
			return &MCPRefreshError{Reason: "stored refresh token is unavailable"}
		}
		if clientID == "" {
			return &MCPRefreshError{Reason: "stored client identity is unavailable"}
		}

		newToken, err := requestMCPRefresh(tokenURL, clientID, token)
		if err != nil {
			return err
		}
		newToken.ClientID = clientID
		newToken.ServerURL = serverURL
		if err := setUpstreamMCPAuthToken(latest, serverName, serverURL, newToken); err != nil {
			return fmt.Errorf("save refreshed token: %w", err)
		}
		if err := writeMCPAuthFile(path, latest); err != nil {
			return fmt.Errorf("save refreshed token: %w", err)
		}
		mcpCache = latest
		refreshed = newToken
		return nil
	})
	if err != nil {
		return MCPAuthToken{}, err
	}
	return refreshed, nil
}

func requestMCPRefresh(tokenURL, clientID string, previous MCPAuthToken) (MCPAuthToken, error) {
	if previous.RefreshToken == "" {
		return MCPAuthToken{}, &MCPRefreshError{Reason: "stored refresh token is unavailable"}
	}
	if err := validateMCPOAuthEndpoint(tokenURL); err != nil {
		return MCPAuthToken{}, &MCPRefreshError{Reason: "refresh endpoint must use HTTPS unless it targets loopback"}
	}
	form := url.Values{}
	form.Set("client_id", clientID)
	form.Set("refresh_token", previous.RefreshToken)
	form.Set("grant_type", "refresh_token")

	req, err := http.NewRequest(http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		// intentionally not logged: the underlying URL may contain credentials.
		return MCPAuthToken{}, &MCPRefreshError{Reason: "refresh endpoint is invalid"}
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	httpClient := &http.Client{
		Timeout: 15 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return fmt.Errorf("redirects are not allowed for MCP OAuth refresh")
		},
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		// intentionally not logged: transport errors include the full endpoint URL.
		if resp != nil {
			_ = resp.Body.Close()
		}
		return MCPAuthToken{}, &MCPRefreshError{Reason: "refresh request failed"}
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return MCPAuthToken{}, &MCPRefreshError{StatusCode: resp.StatusCode, Reason: "refresh credentials were rejected"}
	}

	var response struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int64  `json:"expires_in"`
		Scope        string `json:"scope"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return MCPAuthToken{}, &MCPRefreshError{Reason: "refresh response was not valid JSON"}
	}
	if response.AccessToken == "" {
		return MCPAuthToken{}, &MCPRefreshError{Reason: "refresh response contained no access token"}
	}
	refreshToken := response.RefreshToken
	if refreshToken == "" {
		refreshToken = previous.RefreshToken
	}
	scopes := strings.Fields(response.Scope)
	if len(scopes) == 0 {
		scopes = previous.Scopes
	}
	tokenType := response.TokenType
	if tokenType == "" {
		tokenType = previous.TokenType
	}
	if tokenType == "" {
		tokenType = "Bearer"
	}
	expiresIn := response.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 3600
	}
	return MCPAuthToken{
		AccessToken:  response.AccessToken,
		RefreshToken: refreshToken,
		TokenType:    tokenType,
		Expiry:       time.Now().Unix() + expiresIn,
		Scopes:       scopes,
	}, nil
}

func validateMCPOAuthEndpoint(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil || !parsed.IsAbs() || parsed.Hostname() == "" {
		return fmt.Errorf("OAuth endpoint URL is invalid")
	}
	if parsed.Scheme == "https" {
		return nil
	}
	if parsed.Scheme == "http" {
		host := strings.ToLower(parsed.Hostname())
		if host == "localhost" {
			return nil
		}
		ip := net.ParseIP(parsed.Hostname())
		if ip != nil && ip.IsLoopback() {
			return nil
		}
	}
	return fmt.Errorf("OAuth endpoint must use HTTPS unless it targets loopback")
}
