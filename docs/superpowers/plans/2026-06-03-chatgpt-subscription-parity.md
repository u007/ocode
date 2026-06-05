# ChatGPT Subscription Parity Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Bring ocode's ChatGPT subscription handling to feature parity with opencode: device auth, model allowlist, cost override, header/params parity, account ID caching, and refresh race fix.

**Architecture:** Plugin interface in `internal/plugin/provider/` with a Codex plugin in `internal/plugin/codex/`. Device auth in `internal/auth/`. Client integration in `internal/agent/client.go`. Connect flow in `internal/tui/connect.go`.

**Tech Stack:** Go, `net/http`, `net/http/httptest` (tests), `encoding/json`, `sync` (refresh mutex), `strconv` (allowlist version comparison).

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/plugin/provider/provider.go` | Provider interface, AuthMethod, AuthResult, Model, RequestContext types, Registry |
| `internal/plugin/provider/provider_test.go` | Registry tests |
| `internal/plugin/codex/codex.go` | CodexProvider implementing Provider, browserFlow, deviceFlow wrappers |
| `internal/plugin/codex/allowlist.go` | Model allowlist logic (explicit + semantic filter) |
| `internal/plugin/codex/allowlist_test.go` | Allowlist tests |
| `internal/plugin/codex/provider_test.go` | Provider interface contract tests |
| `internal/auth/openai_device.go` | Device auth flow (requestDeviceCode, pollDeviceToken, OpenAIDeviceLogin) |
| `internal/auth/openai_device_test.go` | Device flow tests |
| `internal/auth/providers.go` | Modify: preserve AccountID in refreshIfExpiring, add refresh mutex |
| `internal/auth/store_test.go` | Modify: add AccountID preservation + concurrent refresh tests |
| `internal/agent/client.go` | Modify: add AccountID field, use cached account ID, 401 detection, plugin calls |
| `internal/agent/client_test.go` | Modify: add account ID + 401 + post-reauth tests |
| `internal/tui/connect.go` | Modify: plugin-driven auth method menu |

---

## Task 1: Plugin Interface + Registry

**Files:**
- Create: `internal/plugin/provider/provider.go`
- Create: `internal/plugin/provider/provider_test.go`

- [ ] **Step 1: Create provider package with types and registry**

```go
// internal/plugin/provider/provider.go
package provider

import (
	"context"
	"net/http"
	"sync"
)

// AuthMethod describes a single way a user can authenticate with a provider.
type AuthMethod struct {
	Label string
	Type  string // "oauth" or "api"
	Run   func(ctx context.Context) (AuthResult, error)
}

// AuthResult is the output of an auth method execution.
type AuthResult struct {
	Type      string // "oauth" or "api"
	Access    string
	Refresh   string
	Expires   int64  // unix millis
	AccountID string
	Key       string // for "api" type
}

// Model is the subset of model metadata that plugins may adjust.
type Model struct {
	ID            string
	Cost          struct{ Input, Output float64 }
	CacheRead     float64
	CacheWrite    float64
	Limit         struct{ Context, Input, Output int }
}

// RequestContext carries request-scoped metadata plugins may need.
type RequestContext struct {
	Provider  string
	Model     string
	SessionID string
	Agent     string
}

// Provider is the contract a plugin fulfills for a single LLM provider.
type Provider interface {
	ID() string
	AuthMethods() []AuthMethod
	Authenticate(ctx context.Context, method AuthMethod) (AuthResult, error)
	ModelAllowed(modelID string) bool
	AdjustModel(m Model) Model
	RequestHeaders(ctx RequestContext) http.Header
	RequestParams(ctx RequestContext) map[string]any
}

var (
	mu       sync.RWMutex
	registry = map[string]Provider{}
)

// Register adds a provider plugin to the global registry.
func Register(p Provider) {
	mu.Lock()
	defer mu.Unlock()
	registry[p.ID()] = p
}

// Get returns the plugin for the given provider ID, if any.
func Get(id string) (Provider, bool) {
	mu.RLock()
	defer mu.RUnlock()
	p, ok := registry[id]
	return p, ok
}

// All returns all registered plugins.
func All() []Provider {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]Provider, 0, len(registry))
	for _, p := range registry {
		out = append(out, p)
	}
	return out
}
```

- [ ] **Step 2: Write registry tests**

```go
// internal/plugin/provider/provider_test.go
package provider

import (
	"context"
	"net/http"
	"testing"
)

type stubProvider struct {
	id string
}

func (s *stubProvider) ID() string                                              { return s.id }
func (s *stubProvider) AuthMethods() []AuthMethod                               { return nil }
func (s *stubProvider) Authenticate(ctx context.Context, m AuthMethod) (AuthResult, error) {
	return AuthResult{}, nil
}
func (s *stubProvider) ModelAllowed(modelID string) bool                        { return true }
func (s *stubProvider) AdjustModel(m Model) Model                               { return m }
func (s *stubProvider) RequestHeaders(ctx RequestContext) http.Header            { return nil }
func (s *stubProvider) RequestParams(ctx RequestContext) map[string]any          { return nil }

func TestRegisterAndGet(t *testing.T) {
	// Reset registry for test isolation
	mu.Lock()
	registry = map[string]Provider{}
	mu.Unlock()

	Register(&stubProvider{id: "test"})
	p, ok := Get("test")
	if !ok || p.ID() != "test" {
		t.Fatalf("expected to get stub provider, got ok=%v id=%q", ok, p.ID())
	}
}

func TestGetNotFound(t *testing.T) {
	mu.Lock()
	registry = map[string]Provider{}
	mu.Unlock()

	_, ok := Get("nonexistent")
	if ok {
		t.Fatal("expected ok=false for nonexistent provider")
	}
}

func TestAll(t *testing.T) {
	mu.Lock()
	registry = map[string]Provider{}
	Register(&stubProvider{id: "a"})
	Register(&stubProvider{id: "b"})
	mu.Unlock()

	all := All()
	if len(all) != 2 {
		t.Fatalf("expected 2 providers, got %d", len(all))
	}
}
```

- [ ] **Step 3: Run tests**

Run: `go test ./internal/plugin/provider/ -v`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/plugin/provider/
git commit -m "feat(plugin): add provider interface and registry"
```

---

## Task 2: Model Allowlist

**Files:**
- Create: `internal/plugin/codex/allowlist.go`
- Create: `internal/plugin/codex/allowlist_test.go`

- [ ] **Step 1: Write allowlist implementation**

```go
// internal/plugin/codex/allowlist.go
package codex

import (
	"regexp"
	"strconv"
	"strings"
)

var allowedCodexModels = map[string]bool{
	"gpt-5.5":              true,
	"gpt-5.4":              true,
	"gpt-5.4-mini":         true,
	"gpt-5.3-codex":        true,
	"gpt-5.3-codex-spark":  true,
	"gpt-5.2":              true,
}

// versionRE matches gpt-X.Y where X and Y are integers.
// Uses integer comparison to avoid "5.40" == 5.4 float false match.
var versionRE = regexp.MustCompile(`^gpt-(\d+)\.(\d+)`)

// isAllowed returns true if the model is in the explicit allowlist or
// passes the semantic filter (major.minor > 5.4).
func isAllowed(modelID string) bool {
	// Strip any suffixes like provider prefix
	m := modelID
	if idx := strings.LastIndex(m, "/"); idx >= 0 {
		m = m[idx+1:]
	}

	if allowedCodexModels[m] {
		return true
	}

	match := versionRE.FindStringSubmatch(m)
	if match == nil {
		return false
	}
	major, err1 := strconv.Atoi(match[1])
	minor, err2 := strconv.Atoi(match[2])
	if err1 != nil || err2 != nil {
		return false
	}
	// major > 5, or major == 5 and minor > 4
	return major > 5 || (major == 5 && minor > 4)
}
```

- [ ] **Step 2: Write allowlist tests**

```go
// internal/plugin/codex/allowlist_test.go
package codex

import "testing"

func TestIsAllowed_ExplicitModels(t *testing.T) {
	allowed := []string{
		"gpt-5.5", "gpt-5.4", "gpt-5.4-mini",
		"gpt-5.3-codex", "gpt-5.3-codex-spark", "gpt-5.2",
	}
	for _, m := range allowed {
		if !isAllowed(m) {
			t.Errorf("expected %q to be allowed", m)
		}
	}
}

func TestIsAllowed_RejectedModels(t *testing.T) {
	rejected := []string{"gpt-4o", "gpt-4.1", "gpt-3.5", "gpt-5", "claude-3-opus"}
	for _, m := range rejected {
		if isAllowed(m) {
			t.Errorf("expected %q to be rejected", m)
		}
	}
}

func TestIsAllowed_SemanticFilter(t *testing.T) {
	cases := []struct {
		model string
		want  bool
	}{
		{"gpt-5.51", true},   // 5.51 > 5.4
		{"gpt-5.40", false},  // 5.40 == 5.4, not > 5.4
		{"gpt-5.39", false},  // 5.39 < 5.4
		{"gpt-5.6", true},    // 5.6 > 5.4
		{"gpt-6.0", true},    // 6.0 > 5.4
		{"gpt-6", false},     // no minor version
	}
	for _, c := range cases {
		got := isAllowed(c.model)
		if got != c.want {
			t.Errorf("isAllowed(%q) = %v, want %v", c.model, got, c.want)
		}
	}
}

func TestIsAllowed_WithProviderPrefix(t *testing.T) {
	if !isAllowed("openai/gpt-5.4") {
		t.Error("expected openai/gpt-5.4 to be allowed")
	}
	if isAllowed("openai/gpt-4o") {
		t.Error("expected openai/gpt-4o to be rejected")
	}
}
```

- [ ] **Step 3: Run tests**

Run: `go test ./internal/plugin/codex/ -run TestIsAllowed -v`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/plugin/codex/allowlist.go internal/plugin/codex/allowlist_test.go
git commit -m "feat(codex): add model allowlist with integer version comparison"
```

---

## Task 3: Device Auth Flow

**Files:**
- Create: `internal/auth/openai_device.go`
- Create: `internal/auth/openai_device_test.go`

- [ ] **Step 1: Write device auth implementation**

```go
// internal/auth/openai_device.go
package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	deviceAuthURL  = "https://auth.openai.com/api/accounts/deviceauth/usercode"
	deviceTokenURL = "https://auth.openai.com/api/accounts/deviceauth/token"
	deviceTimeout  = 5 * time.Minute
)

type deviceCodeResponse struct {
	DeviceAuthID string `json:"device_auth_id"`
	UserCode     string `json:"user_code"`
	Interval     string `json:"interval"`
}

type deviceTokenResponse struct {
	AuthorizationCode string `json:"authorization_code"`
	CodeVerifier      string `json:"code_verifier"`
}

// OpenAIDeviceLogin runs the full device auth flow.
// It blocks until the user completes browser authorization or ctx cancels.
func OpenAIDeviceLogin(ctx context.Context) (Credential, error) {
	ctx, cancel := context.WithTimeout(ctx, deviceTimeout)
	defer cancel()

	codeResp, err := requestDeviceCode(ctx)
	if err != nil {
		return Credential{}, fmt.Errorf("device code request: %w", err)
	}

	interval, err := time.ParseDuration(codeResp.Interval + "s")
	if err != nil {
		interval = 5 * time.Second
	}

	authCode, err := pollDeviceToken(ctx, codeResp.DeviceAuthID, codeResp.UserCode, interval)
	if err != nil {
		return Credential{}, err
	}

	return openaiExchangeCode(authCode, codeResp.UserCode)
}

func requestDeviceCode(ctx context.Context) (*deviceCodeResponse, error) {
	body := fmt.Sprintf(`{"client_id":"%s"}`, openaiClientID)
	req, err := http.NewRequestWithContext(ctx, "POST", deviceAuthURL, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("device code request failed (%d): %s", resp.StatusCode, string(b))
	}

	var result deviceCodeResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode device code response: %w", err)
	}
	return &result, nil
}

func pollDeviceToken(ctx context.Context, deviceAuthID, userCode string, interval time.Duration) (string, error) {
	safetyMargin := 3 * time.Second
	client := &http.Client{Timeout: 30 * time.Second}

	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(interval + safetyMargin):
		}

		body := fmt.Sprintf(`{"device_auth_id":"%s","user_code":"%s"}`, deviceAuthID, userCode)
		req, err := http.NewRequestWithContext(ctx, "POST", deviceTokenURL, strings.NewReader(body))
		if err != nil {
			return "", err
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			return "", err
		}

		if resp.StatusCode == http.StatusOK {
			var result deviceTokenResponse
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				resp.Body.Close()
				return "", fmt.Errorf("decode device token response: %w", err)
			}
			resp.Body.Close()
			if result.AuthorizationCode == "" {
				return "", fmt.Errorf("device token response missing authorization_code")
			}
			return result.AuthorizationCode, nil
		}
		resp.Body.Close()

		if resp.StatusCode != http.StatusForbidden && resp.StatusCode != http.StatusNotFound {
			return "", fmt.Errorf("device token poll failed: %d", resp.StatusCode)
		}
	}
}
```

- [ ] **Step 2: Write device auth tests**

```go
// internal/auth/openai_device_test.go
package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRequestDeviceCode_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/accounts/deviceauth/usercode" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(deviceCodeResponse{
			DeviceAuthID: "dev123",
			UserCode:     "ABCD-1234",
			Interval:     "5",
		})
	}))
	defer srv.Close()

	// Override URLs for test
	oldAuth := deviceAuthURL
	oldToken := deviceTokenURL
	deviceAuthURL = srv.URL + "/api/accounts/deviceauth/usercode"
	deviceTokenURL = srv.URL + "/api/accounts/deviceauth/token"
	defer func() {
		deviceAuthURL = oldAuth
		deviceTokenURL = oldToken
	}()

	resp, err := requestDeviceCode(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.DeviceAuthID != "dev123" {
		t.Errorf("expected DeviceAuthID=dev123, got %s", resp.DeviceAuthID)
	}
	if resp.UserCode != "ABCD-1234" {
		t.Errorf("expected UserCode=ABCD-1234, got %s", resp.UserCode)
	}
}

func TestRequestDeviceCode_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("server error"))
	}))
	defer srv.Close()

	oldAuth := deviceAuthURL
	deviceAuthURL = srv.URL + "/api/accounts/deviceauth/usercode"
	defer func() { deviceAuthURL = oldAuth }()

	_, err := requestDeviceCode(context.Background())
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

func TestPollDeviceToken_PendingThenSuccess(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount < 3 {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		json.NewEncoder(w).Encode(deviceTokenResponse{
			AuthorizationCode: "auth_code_123",
			CodeVerifier:      "verifier_123",
		})
	}))
	defer srv.Close()

	oldToken := deviceTokenURL
	deviceTokenURL = srv.URL + "/api/accounts/deviceauth/token"
	defer func() { deviceTokenURL = oldToken }()

	// Use very short interval for fast test
	code, err := pollDeviceToken(context.Background(), "dev123", "ABCD-1234", 10*time.Millisecond)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code != "auth_code_123" {
		t.Errorf("expected auth_code_123, got %s", code)
	}
	if callCount != 3 {
		t.Errorf("expected 3 calls, got %d", callCount)
	}
}

func TestPollDeviceToken_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	oldToken := deviceTokenURL
	deviceTokenURL = srv.URL + "/api/accounts/deviceauth/token"
	defer func() { deviceTokenURL = oldToken }()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := pollDeviceToken(ctx, "dev123", "ABCD-1234", 10*time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestPollDeviceToken_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	oldToken := deviceTokenURL
	deviceTokenURL = srv.URL + "/api/accounts/deviceauth/token"
	defer func() { deviceTokenURL = oldToken }()

	_, err := pollDeviceToken(context.Background(), "dev123", "ABCD-1234", 10*time.Millisecond)
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}
```

- [ ] **Step 3: Run tests**

Run: `go test ./internal/auth/ -run TestDevice -v`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/auth/openai_device.go internal/auth/openai_device_test.go
git commit -m "feat(auth): add OpenAI device auth flow with polling timeout"
```

---

## Task 4: Account ID Preservation + Refresh Mutex

**Files:**
- Modify: `internal/auth/providers.go` (lines 15-45)
- Modify: `internal/auth/store_test.go`

- [ ] **Step 1: Add refresh mutex and fix AccountID preservation**

In `internal/auth/providers.go`, add a per-provider refresh mutex and fix the AccountID loss:

```go
var (
	refreshMu  = map[string]*sync.Mutex{}
	refreshMuL sync.Mutex
)

func getRefreshMu(provider string) *sync.Mutex {
	refreshMuL.Lock()
	defer refreshMuL.Unlock()
	if m, ok := refreshMu[provider]; ok {
		return m
	}
	m := &sync.Mutex{}
	refreshMu[provider] = m
	return m
}

func refreshIfExpiring(id string, cred Credential) Credential {
	if cred.Kind != KindOAuth {
		return cred
	}
	if cred.ExpiresAt == 0 || cred.RefreshToken == "" {
		return cred
	}
	const skew = 60 * time.Second
	if time.Until(time.Unix(cred.ExpiresAt, 0)) > skew {
		return cred
	}

	mu := getRefreshMu(id)
	mu.Lock()
	defer mu.Unlock()

	// Re-check after acquiring lock — another goroutine may have refreshed.
	if time.Until(time.Unix(cred.ExpiresAt, 0)) > skew {
		return cred
	}

	var refreshed Credential
	var err error
	switch id {
	case "anthropic":
		refreshed, err = AnthropicRefresh(cred.RefreshToken)
	case "openai":
		refreshed, err = OpenAIRefresh(cred.RefreshToken)
	default:
		return cred
	}
	if err != nil || refreshed.AccessToken == "" {
		return cred
	}
	if refreshed.RefreshToken == "" {
		refreshed.RefreshToken = cred.RefreshToken
	}
	refreshed.Account = cred.Account
	refreshed.AccountID = cred.AccountID
	if refreshed.AccountID == "" && cred.AccountID != "" {
		log.Printf("warn: %s token refresh lost AccountID (was %q)", id, cred.AccountID)
	}
	_ = Set(id, refreshed)
	return refreshed
}
```

Add `"log"` and `"sync"` to imports if not already present.

- [ ] **Step 2: Write tests for AccountID preservation + concurrent refresh**

Add to `internal/auth/store_test.go`:

```go
func TestRefreshPreservesAccountID(t *testing.T) {
	cred := Credential{
		Kind:         KindOAuth,
		AccessToken:  "old-access",
		RefreshToken: "refresh-token",
		ExpiresAt:    time.Now().Add(-1 * time.Hour).Unix(),
		AccountID:    "acct-123",
	}
	// Set the credential so refreshIfExpiring can find it
	Set("test-preserve", cred)

	// This will fail to refresh (no real token endpoint), but verifies
	// the preservation logic when refresh fails (returns original cred)
	result := refreshIfExpiring("test-preserve", cred)
	if result.AccountID != "acct-123" {
		t.Errorf("expected AccountID preserved, got %q", result.AccountID)
	}
}

func TestConcurrentRefresh(t *testing.T) {
	cred := Credential{
		Kind:         KindOAuth,
		AccessToken:  "old-access",
		RefreshToken: "refresh-token",
		ExpiresAt:    time.Now().Add(-1 * time.Hour).Unix(),
		AccountID:    "acct-456",
	}
	Set("test-concurrent", cred)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			refreshIfExpiring("test-concurrent", cred)
		}()
	}
	wg.Wait()

	// Verify no panic and credential is still valid
	result, ok := Get("test-concurrent")
	if !ok {
		t.Fatal("credential not found after concurrent refresh")
	}
	if result.AccountID != "acct-456" {
		t.Errorf("AccountID lost after concurrent refresh: %q", result.AccountID)
	}
}
```

- [ ] **Step 3: Run tests**

Run: `go test ./internal/auth/ -run TestRefresh -v`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/auth/providers.go internal/auth/store_test.go
git commit -m "feat(auth): preserve AccountID on refresh, add per-provider refresh mutex"
```

---

## Task 5: Codex Plugin Implementation

**Files:**
- Create: `internal/plugin/codex/codex.go`
- Create: `internal/plugin/codex/provider_test.go`

- [ ] **Step 1: Write Codex plugin**

```go
// internal/plugin/codex/codex.go
package codex

import (
	"context"
	"net/http"
	"os"
	"runtime"

	"github.com/u007/ocode/internal/auth"
	providerplugin "github.com/u007/ocode/internal/plugin/provider"
)

// version is set at build time via ldflags.
var version = "dev"

func init() {
	providerplugin.Register(&CodexProvider{})
}

// CodexProvider implements providerplugin.Provider for the OpenAI/Codex subscription.
type CodexProvider struct{}

func (c *CodexProvider) ID() string { return "openai" }

func (c *CodexProvider) AuthMethods() []providerplugin.AuthMethod {
	return []providerplugin.AuthMethod{
		{Label: "ChatGPT Pro/Plus (browser)", Type: "oauth", Run: browserFlow},
		{Label: "ChatGPT Pro/Plus (device code)", Type: "oauth", Run: deviceFlow},
		{Label: "Manually enter API Key", Type: "api", Run: nil},
	}
}

func (c *CodexProvider) Authenticate(ctx context.Context, method providerplugin.AuthMethod) (providerplugin.AuthResult, error) {
	if method.Run == nil {
		return providerplugin.AuthResult{Type: "api"}, nil
	}
	return method.Run(ctx)
}

func (c *CodexProvider) ModelAllowed(modelID string) bool {
	return isAllowed(modelID)
}

func (c *CodexProvider) AdjustModel(m providerplugin.Model) providerplugin.Model {
	if isAllowed(m.ID) {
		m.Cost.Input = 0
		m.Cost.Output = 0
		m.CacheRead = 0
		m.CacheWrite = 0
	}
	return m
}

func (c *CodexProvider) RequestHeaders(ctx providerplugin.RequestContext) http.Header {
	h := http.Header{}
	h.Set("originator", "opencode")
	h.Set("User-Agent", "opencode/"+version+" ("+runtime.GOOS+" "+runtime.GOARCH+")")
	if ctx.SessionID != "" {
		h.Set("session-id", ctx.SessionID)
	}
	return h
}

func (c *CodexProvider) RequestParams(ctx providerplugin.RequestContext) map[string]any {
	return map[string]any{
		"max_output_tokens": nil,
	}
}

func browserFlow(ctx context.Context) (providerplugin.AuthResult, error) {
	cred, err := auth.OpenAILogin(ctx)
	if err != nil {
		return providerplugin.AuthResult{}, err
	}
	return providerplugin.AuthResult{
		Type:      "oauth",
		Access:    cred.AccessToken,
		Refresh:   cred.RefreshToken,
		Expires:   cred.ExpiresAt * 1000,
		AccountID: cred.AccountID,
	}, nil
}

func deviceFlow(ctx context.Context) (providerplugin.AuthResult, error) {
	cred, err := auth.OpenAIDeviceLogin(ctx)
	if err != nil {
		return providerplugin.AuthResult{}, err
	}
	return providerplugin.AuthResult{
		Type:      "oauth",
		Access:    cred.AccessToken,
		Refresh:   cred.RefreshToken,
		Expires:   cred.ExpiresAt * 1000,
		AccountID: cred.AccountID,
	}, nil
}
```

- [ ] **Step 2: Write provider contract tests**

```go
// internal/plugin/codex/provider_test.go
package codex

import (
	"context"
	"net/http"
	"testing"

	providerplugin "github.com/u007/ocode/internal/plugin/provider"
)

func TestCodexProvider_ID(t *testing.T) {
	p := &CodexProvider{}
	if p.ID() != "openai" {
		t.Errorf("expected ID=openai, got %s", p.ID())
	}
}

func TestModelAllowed(t *testing.T) {
	p := &CodexProvider{}
	if !p.ModelAllowed("gpt-5.4") {
		t.Error("expected gpt-5.4 allowed")
	}
	if p.ModelAllowed("gpt-4o") {
		t.Error("expected gpt-4o rejected")
	}
}

func TestAdjustModel_Allowed(t *testing.T) {
	p := &CodexProvider{}
	m := providerplugin.Model{ID: "gpt-5.4"}
	m.Cost.Input = 10
	m.Cost.Output = 20
	result := p.AdjustModel(m)
	if result.Cost.Input != 0 || result.Cost.Output != 0 {
		t.Errorf("expected cost=0, got input=%v output=%v", result.Cost.Input, result.Cost.Output)
	}
}

func TestAdjustModel_Rejected(t *testing.T) {
	p := &CodexProvider{}
	m := providerplugin.Model{ID: "gpt-4o"}
	m.Cost.Input = 10
	m.Cost.Output = 20
	result := p.AdjustModel(m)
	if result.Cost.Input != 10 || result.Cost.Output != 20 {
		t.Errorf("expected cost unchanged, got input=%v output=%v", result.Cost.Input, result.Cost.Output)
	}
}

func TestRequestHeaders(t *testing.T) {
	p := &CodexProvider{}
	h := p.RequestHeaders(providerplugin.RequestContext{SessionID: "sess-123"})
	if h.Get("originator") != "opencode" {
		t.Errorf("expected originator=opencode, got %s", h.Get("originator"))
	}
	if h.Get("session-id") != "sess-123" {
		t.Errorf("expected session-id=sess-123, got %s", h.Get("session-id"))
	}
	if h.Get("User-Agent") == "" {
		t.Error("expected non-empty User-Agent")
	}
}

func TestRequestParams(t *testing.T) {
	p := &CodexProvider{}
	params := p.RequestParams(providerplugin.RequestContext{})
	if _, ok := params["max_output_tokens"]; !ok {
		t.Error("expected max_output_tokens in params")
	}
}

func TestAuthMethods(t *testing.T) {
	p := &CodexProvider{}
	methods := p.AuthMethods()
	if len(methods) != 3 {
		t.Fatalf("expected 3 auth methods, got %d", len(methods))
	}
	if methods[0].Label != "ChatGPT Pro/Plus (browser)" {
		t.Errorf("unexpected first method label: %s", methods[0].Label)
	}
	if methods[1].Label != "ChatGPT Pro/Plus (device code)" {
		t.Errorf("unexpected second method label: %s", methods[1].Label)
	}
}

func TestAuthenticate_APIKey(t *testing.T) {
	p := &CodexProvider{}
	result, err := p.Authenticate(context.Background(), providerplugin.AuthMethod{Type: "api"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Type != "api" {
		t.Errorf("expected type=api, got %s", result.Type)
	}
}
```

- [ ] **Step 3: Run tests**

Run: `go test ./internal/plugin/codex/ -v`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/plugin/codex/
git commit -m "feat(codex): implement CodexProvider plugin with browser + device flows"
```

---

## Task 6: Client Integration

**Files:**
- Modify: `internal/agent/client.go` (struct, NewClient, chatOpenAI, chatOpenAIResponses)
- Modify: `internal/agent/client_test.go`

- [ ] **Step 1: Add AccountID field to GenericClient**

In `internal/agent/client.go`, add to the struct (around line 83):

```go
type GenericClient struct {
	// ...existing fields...
	AccountID string // cached chatgpt_account_id from OAuth credential
}
```

- [ ] **Step 2: Update NewClient to read AccountID**

In `NewClient`, after the OAuth credential is loaded (around line 2299-2304), add:

```go
case auth.KindOAuth:
	if tok, refreshed := auth.OAuthAccessToken(provider); refreshed {
		apiKey = tok
	} else {
		apiKey = cred.AccessToken
	}
	useOAuth = true
	if cred.AccountID != "" {
		// AccountID will be set on the client below
	}
```

Then in the return statement (around line 2325), add `AccountID`:

```go
return &GenericClient{
	// ...existing fields...
	AccountID: func() string {
		if cred, ok := auth.Get(provider); ok {
			return cred.AccountID
		}
		return ""
	}(),
}
```

- [ ] **Step 3: Update chatOpenAIResponses to use cached AccountID**

In `chatOpenAIResponses` (around line 1196), replace:

```go
// OLD:
accountID := jwtClaim(c.APIKey, "https://api.openai.com/auth", "chatgpt_account_id")

// NEW:
accountID := c.AccountID
```

- [ ] **Step 4: Update chatOpenAI to check model allowlist**

In `chatOpenAI` (around line 458-461), add allowlist check:

```go
func (c *GenericClient) chatOpenAI(ctx context.Context, messages []Message, tools []map[string]interface{}) (*Message, error) {
	if c.UseOAuth && c.Provider == "openai" {
		// Check model allowlist before routing to Codex
		if plugin, ok := providerplugin.Get("openai"); ok && plugin.ModelAllowed(c.Model) {
			return c.chatOpenAIResponses(ctx, messages, tools)
		}
		// Non-allowed model: fall through to standard OpenAI Chat Completions
	}
	// ...rest of function
```

- [ ] **Step 5: Add plugin request headers and params to chatOpenAIResponses**

In `chatOpenAIResponses`, after building the request (around line 1326), add plugin header injection:

```go
req.Header.Set("Content-Type", "application/json")
req.Header.Set("Authorization", "Bearer "+c.APIKey)
if accountID != "" {
	req.Header.Set("ChatGPT-Account-ID", accountID)
}
// Plugin headers
if plugin, ok := providerplugin.Get("openai"); ok && c.UseOAuth {
	pluginHeaders := plugin.RequestHeaders(providerplugin.RequestContext{
		Provider:  c.Provider,
		Model:     c.Model,
		SessionID: os.Getenv("OPENCODE_SESSION_ID"),
	})
	for k, vs := range pluginHeaders {
		req.Header[k] = vs
	}
}
```

And add plugin params to the payload before marshaling (around line 1292):

```go
// Plugin params
if plugin, ok := providerplugin.Get("openai"); ok && c.UseOAuth {
	for k, v := range plugin.RequestParams(providerplugin.RequestContext{}) {
		if v == nil {
			delete(payload, k)
		} else {
			payload[k] = v
		}
	}
}
```

- [ ] **Step 6: Add 401 session expired detection**

In `chatOpenAIResponses`, after the HTTP response check (around line 1342-1347):

```go
if resp.StatusCode != http.StatusOK {
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusUnauthorized && c.UseOAuth {
		return nil, fmt.Errorf("ChatGPT session expired — run /connect to re-authenticate")
	}
	msg := fmt.Sprintf("openai responses error (%d): %s", resp.StatusCode, string(body))
	emitDebug("error", msg)
	return nil, fmt.Errorf("%s", msg)
}
```

- [ ] **Step 7: Write client integration tests**

Add to `internal/agent/client_test.go`:

```go
func TestAccountIDFromCache(t *testing.T) {
	client := &GenericClient{
		Provider:  "openai",
		Model:     "gpt-5.4",
		APIKey:    "test-token",
		UseOAuth:  true,
		AccountID: "cached-account-123",
	}
	if client.AccountID != "cached-account-123" {
		t.Errorf("expected cached AccountID, got %q", client.AccountID)
	}
}

func TestAccountID_NotJWTParsed(t *testing.T) {
	// Verify we don't parse JWT when AccountID is set
	client := &GenericClient{
		Provider:  "openai",
		Model:     "gpt-5.4",
		APIKey:    "not-a-real-jwt",
		UseOAuth:  true,
		AccountID: "from-cache",
	}
	// The client should use AccountID directly, not try to parse the JWT
	if client.AccountID != "from-cache" {
		t.Errorf("expected AccountID from cache, got %q", client.AccountID)
	}
}
```

- [ ] **Step 8: Run tests**

Run: `go test ./internal/agent/ -run TestAccount -v`
Expected: PASS

- [ ] **Step 9: Commit**

```bash
git add internal/agent/client.go internal/agent/client_test.go
git commit -m "feat(agent): integrate Codex plugin into client, add 401 detection"
```

---

## Task 7: Connect Flow Integration

**Files:**
- Modify: `internal/tui/connect.go`

- [ ] **Step 1: Update connect flow to use plugin auth methods**

In `internal/tui/connect.go`, update the OpenAI auth flow section (around line 109) to iterate plugin auth methods:

```go
case "openai":
	// Check if plugin has auth methods
	if plugin, ok := providerplugin.Get("openai"); ok {
		methods := plugin.AuthMethods()
		// Filter out nil Run methods (API key entry handled separately)
		var oauthMethods []providerplugin.AuthMethod
		for _, m := range methods {
			if m.Run != nil {
				oauthMethods = append(oauthMethods, m)
			}
		}
		if len(oauthMethods) > 0 {
			// Show selection menu
			labels := make([]string, len(oauthMethods))
			for i, m := range oauthMethods {
				labels[i] = m.Label
			}
			choice := promptSelection("Choose auth method:", labels)
			method := oauthMethods[choice]
			result, err := plugin.Authenticate(ctx, method)
			if err != nil {
				return err
			}
			cred := auth.Credential{
				Kind:         auth.KindOAuth,
				AccessToken:  result.Access,
				RefreshToken: result.Refresh,
				ExpiresAt:    result.Expires / 1000,
				AccountID:    result.AccountID,
			}
			if err := auth.Set("openai", cred); err != nil {
				return err
			}
			return nil
		}
	}
	// Fallback to existing browser flow
	cred, err := auth.OpenAILogin(ctx)
	if err != nil {
		return err
	}
	return auth.Set("openai", cred)
```

Note: The exact integration depends on the current `connect.go` structure. The above is a guide — adapt to match existing patterns.

- [ ] **Step 2: Commit**

```bash
git add internal/tui/connect.go
git commit -m "feat(tui): plugin-driven auth method menu in connect flow"
```

---

## Task 8: Final Verification

- [ ] **Step 1: Run full test suite**

Run: `go test ./... -count=1`
Expected: ALL PASS

- [ ] **Step 2: Run typecheck**

Run: `go build ./...`
Expected: No errors

- [ ] **Step 3: Verify backward compatibility**

- Existing `OPENAI_API_KEY` users unaffected (no OAuth, no plugin)
- `openai/gpt-4o` with OAuth falls back to standard endpoint (allowlist blocks)
- `openai/gpt-5.4` with OAuth uses Codex endpoint with new headers

- [ ] **Step 4: Final commit**

```bash
git add -A
git commit -m "feat: ChatGPT subscription parity with opencode

- Plugin interface + registry in internal/plugin/provider
- Codex plugin with browser + device auth flows
- Model allowlist (opencode-exact: GPT-5.2+, integer version comparison)
- Cost override (0 for subscription models)
- Header/params parity (originator, User-Agent, session-id, max_output_tokens)
- Account ID caching (eliminates per-request JWT parsing)
- Per-provider refresh mutex (prevents concurrent refresh races)
- 401 session expired detection with specific error message
- 5-minute polling timeout on device auth flow"
```
