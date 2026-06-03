# ChatGPT Subscription Parity for ocode

**Date:** 2026-06-03  
**Status:** Approved  
**Approach:** Pluggable interface (B)

## Goal

Bring ocode's ChatGPT subscription handling to feature parity with opencode (TypeScript). Four gaps: headless device auth, model allowlist + cost override, header/params parity, account ID caching. Plus a bug fix for account ID loss on token refresh, and a TUI warning on refresh failure.

## Architecture

### Plugin Interface (`internal/plugin/provider/provider.go`)

Introduce a `Provider` interface and global registry. Plugins self-register via `init()`.

```go
type AuthMethod struct {
    Label string
    Type  string         // "oauth" or "api"
    Run   func(ctx context.Context) (AuthResult, error)
}

type AuthResult struct {
    Type      string // "oauth" or "api"
    Access    string
    Refresh   string
    Expires   int64  // unix millis
    AccountID string
    Key       string // for "api" type
}

type Model struct {
    ID                           string
    Cost                         struct{ Input, Output float64 }
    CacheRead, CacheWrite        float64
    Limit                        struct{ Context, Input, Output int }
}

type RequestContext struct {
    Provider  string
    Model     string
    SessionID string
    Agent     string
}

type Provider interface {
    ID() string
    AuthMethods() []AuthMethod
    Authenticate(ctx context.Context, method AuthMethod) (AuthResult, error)
    ModelAllowed(modelID string) bool
    AdjustModel(m Model) Model
    RequestHeaders(ctx RequestContext) http.Header
    RequestParams(ctx RequestContext) map[string]any
}
```

Registry: `Register(p Provider)`, `Get(id) (Provider, bool)`, `All() []Provider`.

### Codex Plugin (`internal/plugin/codex/codex.go`)

First registered plugin. `ID() = "openai"`. Registers itself in `init()`.

**Provider name mapping:** The plugin registers under `"openai"` but ocode also has a `"codex"` provider entry in `providers.go` (line 78) that shares the same `OAuthFlow: "openai"` and env var. Both `"openai"` and `"codex"` resolve to the same Codex plugin — the plugin's `ID()` returns `"openai"`, and the connect flow looks up the plugin by the provider's `OAuthFlow` value (`"openai"`), not by provider ID. This means `codex` users get the same plugin behavior automatically.

## Feature 1: Device Auth Flow

New file: `internal/auth/openai_device.go`

Protocol (OAuth 2.0 Device Authorization Grant):

1. `POST https://auth.openai.com/api/accounts/deviceauth/usercode`  
   Body: `{ "client_id": "app_EMoamEEZ73f0CkXaXp7hrann" }`  
   Response: `{ "device_auth_id", "user_code", "interval" }`

2. Print URL and user code to TUI

3. Poll `POST https://auth.openai.com/api/accounts/deviceauth/token`  
   Body: `{ "device_auth_id", "user_code" }`  
   403/404 → continue polling at `interval` + safety margin  
   200 → `{ "authorization_code", "code_verifier" }`

4. Exchange code via existing `openaiExchangeCode(code, codeVerifier)`

5. Return `AuthResult` with `AccountID` from JWT

**Polling timeout:** Cap at 5 minutes (matching opencode's timeout). Use `context.WithTimeout(ctx, 5*time.Minute)` on the polling loop. If the user abandons auth, the context cancels and the goroutine exits cleanly.

**TUI integration:** `connect.go` iterates `plugin.AuthMethods()` and renders each as a selectable menu item. User picks one; selected method's `Run` is called.

## Feature 2: Model Allowlist

New file: `internal/plugin/codex/allowlist.go`

Exact opencode mirror:

- Explicit allowlist: `gpt-5.5`, `gpt-5.4`, `gpt-5.4-mini`, `gpt-5.3-codex`, `gpt-5.3-codex-spark`, `gpt-5.2`
- Semantic filter: regex `^gpt-(\d+)\.(\d+)`, integer compare major.minor > 5.4. Use `strconv.Atoi` on split parts, not `ParseFloat` — avoids `"5.40"` == `5.4` false match. Explicit allowlist entries bypass the semantic filter (OR logic).

Called from `client.go` in `chatOpenAI()`:
- `UseOAuth && provider == "openai"` AND `plugin.ModelAllowed(model)` → route to `chatOpenAIResponses`
- Otherwise → standard `chatOpenAI` path (non-codex endpoint)

Non-allowed GPT models fall back to `api.openai.com/chat/completions` — no error.

## Feature 3: Cost Override

`AdjustModel()` returns cost=0 for allowed models:

```go
func (c *CodexProvider) AdjustModel(m providerplugin.Model) providerplugin.Model {
    if isAllowed(m.ID) {
        m.Cost.Input = 0
        m.Cost.Output = 0
        m.CacheRead = 0
        m.CacheWrite = 0
    }
    return m
}
```

Called in model resolution (after models.dev lookup, before client construction). `usage.Spend(model)` returns $0 for Codex-routed models.

## Feature 4: Header/Params Parity

**Headers** (`RequestHeaders`):
- `originator: opencode`
- `User-Agent: opencode/{version} ({platform} {arch})`
- `session-id: {sessionID}`

Merged into `chatOpenAIResponses` at request build time.

**Params** (`RequestParams`):
- `max_output_tokens: nil` → omitted from JSON, matches opencode's `maxOutputTokens = undefined`

## Feature 5: Account ID Caching

### Changes

1. `internal/auth/store.go` — `Credential` gains `AccountID string` field (already exists at line 33, already serialized in JSON)

2. `internal/auth/providers.go` — `refreshIfExpiring` preserves `AccountID` (BUG FIX):
```go
refreshed.Account = cred.Account
refreshed.AccountID = cred.AccountID // NEW — fixes loss on refresh
if refreshed.AccountID == "" && cred.AccountID != "" {
    log.Printf("warn: openai token refresh lost AccountID (was %q)", cred.AccountID)
}
```

**Concurrent refresh race guard:** Add a `sync.Mutex` per-provider in the store to serialize refresh calls. Two goroutines hitting an expired token simultaneously must not both refresh — the mutex ensures only one refreshes while the other waits and reads the updated credential. The existing `storeMu` protects the cache map; this new mutex protects the refresh operation itself. Implement via a `refreshMu map[string]*sync.Mutex` in the store, created on first use per provider.

3. `internal/agent/client.go` — `NewClient` reads `AccountID` from credential, stores on `GenericClient`

4. `internal/agent/client.go` — `chatOpenAIResponses` uses `c.AccountID` instead of JWT parsing:
```go
// OLD: accountID := jwtClaim(c.APIKey, "https://api.openai.com/auth", "chatgpt_account_id")
// NEW:
accountID := c.AccountID
```

5. `GenericClient` struct gains `AccountID string` field

6. Plugin's `browserFlow` / `deviceFlow` extract and return `AccountID` in `AuthResult`

### TUI Warning on Refresh Failure

When `OAuthAccessToken()` returns a credential that failed to refresh (expired but no new token), `NewClient` logs a warning via `emitDebug("auth", ...)` so the user sees it in the debug pane.

Additionally, in `chatOpenAIResponses`, if the first request returns 401 and `UseOAuth` is true, return a specific error: `"ChatGPT session expired — run /connect to re-authenticate"` instead of the generic retry loop.

**Post-reauth credential re-read:** After the user runs `/connect` and re-authenticates, the next `NewClient` call must read the fresh credential from `auth.Get("openai")`, not from any in-memory cache on the old `GenericClient`. This is already the case — `NewClient` is called per-conversation or per-agent, and the store is the source of truth. Verify with a test that after `auth.Set("openai", newCred)`, a fresh `NewClient` picks up the new token and AccountID.

## Integration Flow

### Client Construction

```
NewClient(cfg, "openai/gpt-5.4")
  ├─ resolveKeyWithConfig → auth.Get("openai")
  ├─ plugin.Get("openai") → CodexProvider
  ├─ if cred.Kind == oauth:
  │    ├─ apiKey = cred.AccessToken
  │    ├─ useOAuth = true
  │    └─ accountID = cred.AccountID
  └─ return &GenericClient{...AccountID: accountID...}
```

### Request Flow

```
ChatWithContext()
  └─ chatOpenAI()
       ├─ UseOAuth && provider == "openai"
       ├─ plugin.ModelAllowed(model) → "gpt-5.4" passes
       └─ chatOpenAIResponses()
            ├─ accountID = c.AccountID
            ├─ URL = "https://chatgpt.com/backend-api/codex/responses"
            ├─ plugin.RequestParams() → merge nil max_output_tokens
            ├─ headers: Authorization, ChatGPT-Account-ID, originator, User-Agent, session-id
            └─ parseResponsesSSE()
```

### Connect Flow

```
connect.go picks "openai" provider
  ├─ plugin.AuthMethods() → browser, device code, API key
  ├─ user picks device code
  ├─ selectedMethod.Run(ctx) → AuthResult{AccountID: "xyz"}
  └─ auth.Set("openai", Credential{..., AccountID: "xyz"})
```

## Files Changed

| File | Change |
|------|--------|
| `internal/plugin/provider/provider.go` | **NEW** — interface + registry |
| `internal/plugin/codex/codex.go` | **NEW** — plugin implementation |
| `internal/plugin/codex/allowlist.go` | **NEW** — model allowlist logic |
| `internal/auth/openai_device.go` | **NEW** — device auth flow |
| `internal/agent/client.go` | Add `AccountID` field to `GenericClient`, use cached account ID in `chatOpenAIResponses`, add 401 auth-expired detection, integrate plugin calls |
| `internal/auth/providers.go` | Preserve `AccountID` in `refreshIfExpiring`, add per-provider refresh mutex |
| `internal/tui/connect.go` | Plugin-driven auth method menu |
| `internal/plugin/provider/provider_test.go` | **NEW** — registry tests |
| `internal/plugin/codex/allowlist_test.go` | **NEW** — allowlist tests |
| `internal/plugin/codex/provider_test.go` | **NEW** — interface contract tests |
| `internal/auth/openai_device_test.go` | **NEW** — device flow tests |
| `internal/auth/store_test.go` | Updated — AccountID preservation, concurrent refresh tests |
| `internal/agent/client_test.go` | Updated — account ID caching tests, 401 error tests, post-reauth test |

## Backward Compatibility

| Scenario | Before | After |
|---|---|---|
| `OPENAI_API_KEY` set, no OAuth | `chatOpenAI` → `api.openai.com/chat/completions` | **No change** |
| `openai/gpt-4o` with OAuth | Codex endpoint (backend may reject) | Falls back to `api.openai.com` (allowlist blocks) |
| `openai/gpt-5.4` with OAuth | Same endpoint | Same, now with headers + cost=0 + cached account ID |
| Token refresh | AccountID lost | AccountID preserved |
| Refresh failure | Silent 401 retry | 401 → "session expired, run /connect" |
| Non-OpenAI providers | Unaffected | No plugin registered — zero changes |
| `codex` provider entry | `OPENAI_API_KEY` → `api.openai.com/v1` | Unaffected |

## Testing

### Unit Tests

**Allowlist** (`allowlist_test.go`): 100% coverage
- Known allowed models pass
- Known rejected models fail
- Semantic filter: gpt-5.51 passes, gpt-5.39 fails, gpt-5.40 does NOT pass (integer comparison, not float)
- Unknown future IDs (gpt-5.6, gpt-6) pass
- Explicit allowlist entries bypass semantic filter

**Provider** (`provider_test.go`): 100% coverage
- `ModelAllowed` delegates to `isAllowed`
- `AdjustModel` — allowed → cost=0, disallowed → unchanged
- `RequestHeaders` — returns correct headers
- `RequestParams` — returns nil for max_output_tokens

**Device flow** (`openai_device_test.go`): >=90% coverage
- Mock httptest server for all endpoints
- `TestRequestDeviceCode_Success`, `TestRequestDeviceCode_Error`
- `TestPollDeviceToken_Pending`, `_Success`, `_Error`
- `TestPollDeviceToken_Timeout` — context expires after 5 min, goroutine exits cleanly

**Auth store** (`store_test.go`): 
- `TestRefreshPreservesAccountID` — refresh preserves AccountID
- `TestRefreshLogsWarningOnEmptyAccountID` — warns if AccountID lost
- `TestConcurrentRefresh` — two goroutines refresh simultaneously, only one hits token endpoint

**Client integration** (`client_test.go`):
- `TestChatOpenAIResponses_UsesCodexURL`
- `TestChatOpenAIResponses_NonAllowedModel_Fallback`
- `TestAccountIDFromCache`
- `TestAccountID_NotJWTParsed`
- `Test401_SessionExpired_ReturnsSpecificError`
- `TestPostReauth_PicksUpNewCredential` — after `auth.Set`, fresh `NewClient` uses new token + AccountID

### Integration Test (manual/CI)

Device flow end-to-end: request code → poll → exchange → credential persisted with AccountID.
