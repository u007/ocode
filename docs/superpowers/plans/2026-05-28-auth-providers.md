# Auth Providers Implementation Plan (Cloudflare Workers AI, Cloudflare AI Gateway, OpenAI Codex OAuth)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add three new auth providers — Cloudflare Workers AI (API key + account ID), Cloudflare AI Gateway (custom gateway URL), and OpenAI Codex (OAuth reusing the existing OpenAI flow) — so users can connect to them via `/connect`.

**Architecture:** Each provider follows the existing pattern: (1) add an entry to `auth.Providers`, (2) store credentials via the existing store (with `AccountID string` added to `Credential` for Cloudflare), (3) wire routing in `NewClient`. The `BaseURL` field on `Credential` already feeds into `NewClient` via `auth.GetBaseURL` — Cloudflare Workers AI constructs and stores the full endpoint URL in `BaseURL` at save time, so no parsing is needed later. Cloudflare AI Gateway strips `max_tokens` for o-series models using the existing `isReasoningOnlyModel` helper.

**Tech Stack:** Go, existing `internal/auth` credential store, existing `internal/agent/client.go` provider routing.

---

## File Map

| File | Action | Responsibility |
|------|--------|----------------|
| `internal/auth/store.go` | Modify | Add `AccountID string` to `Credential` struct |
| `internal/auth/cloudflare.go` | Create | `CloudflareWorkersBaseURL` helper; no parsing needed |
| `internal/auth/providers.go` | Modify | Add `cloudflare-workers`, `cloudflare-gateway`, `codex` entries |
| `internal/agent/client.go` | Modify | Add `cloudflare-gateway` provider entry in `providers` map; strip `max_tokens` for gateway o-series in `chatOpenAI` |
| `internal/tui/connect.go` | Modify | Prompt for account ID when connecting Cloudflare Workers AI |
| `internal/auth/cloudflare_test.go` | Create | Tests for `CloudflareWorkersBaseURL` |
| `internal/agent/client_cloudflare_test.go` | Create | Tests for o-series stripping on gateway |

---

### Task 1: Add `AccountID` to `Credential`

The `Credential` struct lives in `internal/auth/store.go`. Adding `AccountID` is the clean way to store the Cloudflare account ID separately from `BaseURL`.

**Files:**
- Modify: `internal/auth/store.go`

- [ ] **Step 1: Add `AccountID` field**

In `internal/auth/store.go`, extend `Credential` (currently line 22) with one new field after `BaseURL`:

```go
type Credential struct {
	Kind         CredentialKind `json:"kind"`
	Key          string         `json:"key,omitempty"`
	AccessToken  string         `json:"access_token,omitempty"`
	RefreshToken string         `json:"refresh_token,omitempty"`
	ExpiresAt    int64          `json:"expires_at,omitempty"`
	Account      string         `json:"account,omitempty"`
	BaseURL      string         `json:"base_url,omitempty"`
	AccountID    string         `json:"account_id,omitempty"` // Cloudflare account ID
}
```

- [ ] **Step 2: Build**

```
cd /Users/james/www/ocode && go build ./... 2>&1
```

Expected: no errors. Existing JSON is forward-compatible — old auth.json files without `account_id` will deserialise with an empty string.

- [ ] **Step 3: Commit**

```bash
git add internal/auth/store.go
git commit -m "feat(auth): add AccountID field to Credential"
```

---

### Task 2: Cloudflare Workers AI helper

**Files:**
- Create: `internal/auth/cloudflare.go`
- Create: `internal/auth/cloudflare_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/auth/cloudflare_test.go
package auth

import "testing"

func TestCloudflareWorkersBaseURL(t *testing.T) {
	got := CloudflareWorkersBaseURL("abc123")
	want := "https://api.cloudflare.com/client/v4/accounts/abc123/ai/v1"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
```

- [ ] **Step 2: Run to confirm failure**

```
cd /Users/james/www/ocode && go test ./internal/auth/... -run TestCloudflare -v 2>&1
```

Expected: compile error.

- [ ] **Step 3: Create `internal/auth/cloudflare.go`**

```go
package auth

import "fmt"

// CloudflareWorkersBaseURL builds the Workers AI endpoint URL for a given
// account ID. Store this in Credential.BaseURL at save time so NewClient picks
// it up via auth.GetBaseURL without needing to parse it back.
func CloudflareWorkersBaseURL(accountID string) string {
	return fmt.Sprintf("https://api.cloudflare.com/client/v4/accounts/%s/ai/v1", accountID)
}
```

- [ ] **Step 4: Run tests**

```
cd /Users/james/www/ocode && go test ./internal/auth/... -run TestCloudflare -v 2>&1
```

Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add internal/auth/cloudflare.go internal/auth/cloudflare_test.go
git commit -m "feat(auth): add CloudflareWorkersBaseURL helper"
```

---

### Task 3: Add providers to registry

**Files:**
- Modify: `internal/auth/providers.go`

- [ ] **Step 1: Add three entries to the `Providers` slice**

In `internal/auth/providers.go`, after the `lmstudio` entry (line ~73):

```go
{ID: "cloudflare-workers", Label: "Cloudflare Workers AI", EnvVar: "CLOUDFLARE_API_KEY"},
{ID: "cloudflare-gateway", Label: "Cloudflare AI Gateway", EnvVar: "CLOUDFLARE_GATEWAY_KEY"},
{ID: "codex",              Label: "OpenAI Codex",          EnvVar: "OPENAI_API_KEY", OAuthFlow: "openai"},
```

`codex` reuses `OAuthFlow: "openai"` — the existing OpenAI OAuth flow handles the token acquisition. `NewClient` will route it to the Codex endpoint (wired in Task 4).

- [ ] **Step 2: Build**

```
cd /Users/james/www/ocode && go build ./... 2>&1
```

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add internal/auth/providers.go
git commit -m "feat(auth): register cloudflare-workers, cloudflare-gateway, codex providers"
```

---

### Task 4: Wire provider routing in `NewClient` and strip `max_tokens` on gateway

`NewClient` in `internal/agent/client.go` (line 1830) resolves `baseURL` per provider. The internal `providers` map (searched near line 1873) holds per-provider defaults. We also need to strip `max_tokens` for o-series models when using `cloudflare-gateway`.

**Files:**
- Modify: `internal/agent/client.go`
- Create: `internal/agent/client_cloudflare_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// internal/agent/client_cloudflare_test.go
package agent

import (
	"testing"
)

func TestStripMaxTokensForCloudflareGatewayOSeries(t *testing.T) {
	// o-series model on cloudflare-gateway must lose max_tokens.
	payload := map[string]interface{}{
		"model":       "o1",
		"max_tokens":  4096,
		"temperature": 0.7,
	}
	maybeStripMaxTokensForGateway("cloudflare-gateway", "o1", payload)
	if _, ok := payload["max_tokens"]; ok {
		t.Error("max_tokens should be stripped for o-series on cloudflare-gateway")
	}
	if _, ok := payload["temperature"]; !ok {
		t.Error("temperature should be preserved")
	}
}

func TestStripMaxTokensPreservesNonOSeries(t *testing.T) {
	payload := map[string]interface{}{
		"model":      "@cf/meta/llama-3",
		"max_tokens": 4096,
	}
	maybeStripMaxTokensForGateway("cloudflare-gateway", "@cf/meta/llama-3", payload)
	if _, ok := payload["max_tokens"]; !ok {
		t.Error("max_tokens should be preserved for non-o-series")
	}
}

func TestStripMaxTokensPreservesNonGateway(t *testing.T) {
	payload := map[string]interface{}{
		"model":      "o1",
		"max_tokens": 4096,
	}
	maybeStripMaxTokensForGateway("openai", "o1", payload)
	// openai provider: max_tokens already handled by isReasoningOnlyModel via
	// applyGenerationParams; this helper must not touch non-gateway providers.
	if _, ok := payload["max_tokens"]; !ok {
		t.Error("max_tokens should be untouched for non-gateway providers")
	}
}
```

- [ ] **Step 2: Run to confirm failure**

```
cd /Users/james/www/ocode && go test ./internal/agent/... -run TestStripMaxTokens -v 2>&1
```

Expected: compile error.

- [ ] **Step 3: Add `maybeStripMaxTokensForGateway` helper near `isReasoningOnlyModel` (line ~184)**

```go
// maybeStripMaxTokensForGateway removes max_tokens from an outgoing payload
// when the provider is cloudflare-gateway and the model is an o-series reasoning
// model. Cloudflare's gateway forwards to OpenAI, which rejects max_tokens for
// o-series; it accepts only max_completion_tokens (not yet supported here).
// isReasoningOnlyModel (defined above) is reused for the model check.
func maybeStripMaxTokensForGateway(provider, model string, payload map[string]interface{}) {
	if provider != "cloudflare-gateway" {
		return
	}
	if isReasoningOnlyModel(model) {
		delete(payload, "max_tokens")
	}
}
```

- [ ] **Step 4: Add `cloudflare-gateway` to the internal `providers` map in `NewClient`**

Search for the `providers` map literal in `client.go` (it holds per-provider `envKey` and `baseURL` defaults). Add:

```
grep -n "\"lmstudio\"\|providers\[" /Users/james/www/ocode/internal/agent/client.go | head -20
```

In that map, add (after `lmstudio`):

```go
"cloudflare-gateway": {envKey: "CLOUDFLARE_GATEWAY_KEY", baseURL: ""},
```

`cloudflare-workers` doesn't need an entry here — its `baseURL` comes from `auth.GetBaseURL("cloudflare-workers")` which reads `Credential.BaseURL` set during `/connect`. `cloudflare-gateway` similarly; the user's gateway URL is stored as `Credential.BaseURL`.

For `codex`, add:

```go
"codex": {envKey: "OPENAI_API_KEY", baseURL: "https://api.openai.com/v1"},
```

- [ ] **Step 5: Wire `maybeStripMaxTokensForGateway` into `chatOpenAI`**

`chatOpenAI` starts at line 331. The `payload` map is built at line 342:

```go
payload := map[string]interface{}{
    "model":    c.Model,
    "messages": openAIMessages,
    "stream":   true,
}
c.applyGenerationParams(payload)
```

After `c.applyGenerationParams(payload)` (line ~347), add:

```go
maybeStripMaxTokensForGateway(c.Provider, c.Model, payload)
```

- [ ] **Step 6: Run tests**

```
cd /Users/james/www/ocode && go test ./internal/agent/... -run TestStripMaxTokens -v 2>&1
```

Expected: all pass.

- [ ] **Step 7: Commit**

```bash
git add internal/agent/client.go internal/agent/client_cloudflare_test.go
git commit -m "feat(agent): add cloudflare-gateway/codex providers, strip max_tokens for gateway o-series"
```

---

### Task 5: Connect dialog — Cloudflare Workers AI account ID prompt

**Files:**
- Modify: `internal/tui/connect.go`

- [ ] **Step 1: Locate where API key credentials are saved**

```
grep -n "KindAPIKey\|auth.Store\|auth.Save\|Store(\|storeCred" /Users/james/www/ocode/internal/tui/connect.go | head -20
```

- [ ] **Step 2: Find the save path and add account ID handling**

When the selected provider is `"cloudflare-workers"` and an account ID has been collected, build the full endpoint URL and store it in `Credential.BaseURL`, and store the raw account ID in `Credential.AccountID`:

```go
cred := auth.Credential{
    Kind: auth.KindAPIKey,
    Key:  apiKey,
}
if providerID == "cloudflare-workers" && accountID != "" {
    cred.AccountID = accountID
    cred.BaseURL = auth.CloudflareWorkersBaseURL(accountID)
}
auth.Store(providerID, cred)
```

The `auth.GetBaseURL("cloudflare-workers")` call in `NewClient` (line 1900) will then return the constructed URL automatically — no extra routing code needed.

- [ ] **Step 3: Add account ID input step to the dialog state machine for `cloudflare-workers`**

The connect dialog has a state machine (stages: select provider → enter key → confirm or similar). Add a stage that fires only when `providerID == "cloudflare-workers"` to collect the account ID string before the key is saved.

Read `connect.go` fully to identify the exact state type and transitions, then insert a new state variant (e.g. `stateAccountID`) that appears between provider selection and API key input for this provider. The text prompt should read: `"Cloudflare Account ID (from dash.cloudflare.com):"`.

- [ ] **Step 4: Build**

```
cd /Users/james/www/ocode && go build ./... 2>&1
```

Expected: no errors.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/connect.go
git commit -m "feat(tui): prompt for account ID when connecting Cloudflare Workers AI"
```

---

## Self-Review

**All advisor issues addressed:**
- `AccountID` on `Credential` (not BaseURL encoding/parsing) → Task 1 ✓
- `ExtractCloudflareAccountID` removed entirely — not needed since `BaseURL` is stored constructed → Task 2 ✓
- No dead code: `GetBaseURL("cloudflare-workers")` in `NewClient` line 1900 picks up the stored URL automatically ✓
- `isReasoningOnlyModel` reused (not duplicated) for gateway stripping → Task 4 `maybeStripMaxTokensForGateway` ✓
- Placeholder "inspect surrounding code" steps replaced with concrete line numbers (331, 342, 347, 1900) ✓
- Task 5 still has one "read connect.go fully" step — the dialog state machine varies; this is the minimum necessary orientation before editing, not a placeholder ✓

**Type consistency:**
- `auth.CloudflareWorkersBaseURL(accountID string) string` — defined Task 2, used Task 5 ✓
- `auth.Credential.AccountID string` — defined Task 1, set in Task 5 ✓
- `maybeStripMaxTokensForGateway(provider, model string, payload map[string]interface{})` — defined and used in Task 4 ✓
