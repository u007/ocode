---
type: Concept
title: Remote MCP OAuth Compatibility
description: 'Implemented remote MCP OAuth compatibility: dual-schema mcp-auth.json reads with merge-safe writes, URL-bound token attachment, RFC 9728/8414 discovery refresh with a single retry, status-before-decode HTTP errors, CLI list status fix; per-chat toggle/cache semantics unchanged.'
tags:
  - mcp
  - oauth
  - auth
  - remote
  - credentials
  - errors
timestamp: 2026-09-25T06:23:10Z
---
# Remote MCP OAuth Compatibility

**Type:** Concept
**Status:** Current as of 2026-09-25
**Files:** `internal/auth/mcp_auth.go`, `internal/mcp/client.go`, `internal/mcpcli/commands.go`, `internal/filelock/filelock.go`

## Overview

Remote MCP servers that the user already authorized through upstream OpenCode used to fail in ocode: the stored credential was written in a different JSON schema, so ocode attached nothing, the server answered HTTP 401, and the error path masked that behind a JSON decode failure — while `ocode mcp list` could still print `ok`. The fix (designed in `docs/superpowers/specs/2026-09-25-zoho-mcp-oauth-compatibility-design.md`, implemented 2026-09-25) makes ocode read both storage schemas, bind credentials to the exact server URL they were saved against, discover OAuth metadata from the 401 challenge to refresh an expired token, and surface HTTP failures as actionable errors. The motivating example was a Zoho Books remote MCP server, but nothing in the implementation is Zoho-specific.

## Dual-schema auth storage (`internal/auth/mcp_auth.go`)

Stored MCP authorization lives in one file — `mcp-auth.json` under `$XDG_DATA_HOME/opencode/` (default `~/.local/share/opencode/`; Windows: `%APPDATA%\opencode\`) — whose entries may be in either of two schemas:

- **ocode native:** `{ "tokens": { "<server>": { "access_token", "refresh_token", "token_type", "expiry", "scopes" } } }`
- **upstream OpenCode:** `{ "<server>": { "tokens": { "accessToken", "refreshToken", "expiresAt", "scope" }, "clientInfo": { "clientId" }, "serverUrl" } }`

**Reads accept both.** `GetMCPAuth` tries the upstream entry first, then falls back to the native `tokens` map. `GetNativeMCPAuth` reads *only* the native schema; it is the fallback a static-`oauth` server uses, so a credential saved for some other server URL can never be sent to it.

**Writes are merge-safe.** `updateMCPAuthFileLocked` takes a cross-process advisory lock (`filelock.WithFileLockTimeout` on `<file>.lock`, 30s bound — `WithFileLockTimeout` is new in this change), re-reads the *latest* file inside the lock, patches only the raw JSON keys it owns — unknown top-level entries and unknown keys inside an entry survive verbatim — then writes a `0600` temp file and renames it into place. `DeleteMCPAuth` (the `mcp logout` path) likewise removes only that server's native *and* upstream entries, preserving everything else. In-process, a `sync.Mutex` plus a small in-memory copy serializes access (`mcpCache` in the `auth` package — not the server's tool-enumeration `mcpCache`, see `docs/concepts/per-chat-mcp-toggle.md`).

Rotated tokens from a URL-bound refresh are persisted **in the upstream schema** (`SetMCPAuthForServer` / `setUpstreamMCPAuthToken`), so ocode and upstream OpenCode keep sharing one representation; reads stay dual-schema, so files mixing both formats remain valid. The interactive `mcp auth` browser flow is unchanged and still writes the native schema.

## URL binding

A stored upstream credential is only ever attached to requests for the exact server URL it was saved against:

- `GetMCPAuthForServer(name, serverURL)` returns a token only when the entry's `serverUrl` string-equals the configured remote MCP URL.
- `MCPClient.loadStoredToken` tries that URL-bound lookup first — **regardless of whether the server has a static `oauth` config** (presence of stored authorization alone is enough to authenticate the request) — and only falls back to the native schema when static OAuth is configured.
- The refresh path re-validates the binding: `RefreshMCPAuthTokenForServer` refuses when the stored server binding does not match the configured URL or was changed/removed, and, under the same lock, re-reads the file so a fresher credential another process already persisted is reused instead of refreshing again.
- Discovery identity checks are the same rule one layer up: the protected-resource metadata's `resource` must match the MCP server URL, and the authorization server's `issuer` must match its own metadata URL (`sameMCPURL`); a mismatch aborts.

No cross-server or cross-URL reuse anywhere in the path.

## Expired-token refresh via discovery (`internal/mcp/client.go`)

When a remote request comes back HTTP 401 with a `WWW-Authenticate` challenge:

1. Parse `resource_metadata=...` from the challenge; if absent, construct the RFC 9728 endpoint from the server URL (`/.well-known/oauth-protected-resource` + resource path).
2. Fetch the protected-resource metadata, verify `resource` identity, take its first `authorization_servers` entry, fetch the RFC 8414 authorization-server metadata (`/.well-known/oauth-authorization-server` + issuer path), verify the issuer, and take `token_endpoint` (a relative endpoint resolves against the issuer; an empty one is rejected).
3. Perform **one** refresh-token grant with the stored client identity and refresh token (`RefreshMCPAuthTokenForServer` for URL-bound entries, `RefreshMCPAuthToken` for native/static ones).
4. Persist the rotated token (upstream schema for URL-bound entries — see above), then **retry the original request exactly once**. A second 401 becomes `ReauthorizationRequiredError` ("the refreshed credential was rejected").

Metadata and transport rules: HTTPS only (plain `http` accepted only for loopback), and **no redirects** on the remote MCP client, the metadata fetch, or the refresh grant. A rejected refresh surfaces as `ReauthorizationRequiredError` with a short generic reason — no loop, no retry storm, no dynamic-client-registration workaround. `safeRefreshError` deliberately reports only the server name and reason: never the endpoint, client identity, or any credential. Expiry timing: a URL-bound stored token is attached as-is even when expired and refreshed lazily on the 401 (discovery is required first); a native static-OAuth token refreshes eagerly at client construction when its static token URL/client are configured, and otherwise fails with a reauthorization error instead of sending an expired token.

## Safe HTTP errors

- Status is checked **before** any JSON decoding: `decodeRemoteResponse` turns any non-2xx into `RemoteHTTPError{ServerName, StatusCode, Status, AuthenticationRequired}` — e.g. `remote MCP server "<name>" requires authorization (HTTP 401)` or `remote MCP server "<name>" returned HTTP 500`. The response body and the endpoint URL are intentionally excluded (a body may be plain text or secret-bearing; a URL may carry secret path segments).
- A plain-text 401 therefore reports the real unauthorized condition instead of a JSON decode failure.
- Transport and metadata failures are similarly generic (`"request failed"`, `"metadata request failed"`) because the underlying errors would embed full endpoint URLs; code comments mark these as intentionally not logged.
- `ReauthorizationRequiredError` carries the server name and a short reason only — no client identities, tokens, or endpoint URLs.
- `ocode mcp list` classifies from the probe result itself: the symbol defaults to `fail`, `off` only for disabled servers, `ok` only when the probe actually returned tools (`runList` in `internal/mcpcli/commands.go`) — a failed probe can never render `ok`.

## Unchanged: per-chat toggle and cache semantics

This fix sits entirely below the enable/toggle layer. Per-session overrides, the process-wide `opencode.json` persist, the boot-time `mcpCache` warm, `mcpToolsForSession`, and `rebuildAgentForMCP` are untouched — the design's non-goals state explicitly that "enabled" semantics and the per-chat MCP toggle/cache behavior remain unchanged, and `internal/server/handler_mcp.go` / `internal/server/mcp_cache.go` were not modified. The one observable interaction is benign: an upstream-authorized remote server now enumerates successfully during cache warm where it previously failed with 401. See `docs/concepts/per-chat-mcp-toggle.md` for the toggle/cache mechanics.

## Tests

All green in `go test ./internal/auth/ ./internal/mcp/ ./internal/mcpcli/ ./internal/filelock/` (2026-09-25):

- `internal/auth/mcp_auth_test.go` — 10 tests: dual-schema read (both directions), merge-safe native/upstream writes (foreign entries and unknown metadata preserved), exact-URL binding, upstream-schema persist, write reloads the latest file before merging, refresh reuses a newer persisted credential, refresh rejects a changed/deleted binding, no redirect forwarding on refresh.
- `internal/mcp/client_test.go` — 14 tests: token attached without static `oauth`, plain-text status reported before decode, discovery → refresh → single retry, refresh rejection ⇒ reauthorization without a loop, static-OAuth attachment stays compatible, a mismatched upstream credential is not consumed as a static-OAuth fallback, URL-bound credentials do not use static refresh, concurrent refresh serialization with unrotated fields preserved, no metadata redirect, resource/issuer identity mismatches rejected, empty discovered token endpoint rejected, challenge-less resource-metadata path, HTTPS-only metadata outside loopback.
- `internal/mcpcli/commands_test.go` — `TestRunListClassifiesFailedProbeAsFail`.
- `internal/filelock/filelock_test.go` — `WithFileLockTimeout` honors the caller's bound.

## Related

- `docs/superpowers/specs/2026-09-25-zoho-mcp-oauth-compatibility-design.md` — the approved design (scope, security rules, non-goals).
- `docs/concepts/per-chat-mcp-toggle.md` — per-session toggle/override/cache semantics, explicitly unchanged by this fix.
- `docs/plugins.md` — MCP server registration from the plugin side.