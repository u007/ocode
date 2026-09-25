---
type: Decision
title: Zoho MCP OAuth Compatibility — Design Spec
description: 'Approved design spec for Zoho MCP OAuth compatibility: dual-schema auth storage, token attachment, RFC 9728 refresh, error surfacing, CLI status fix.'
tags:
  - mcp
  - oauth
  - zoho
  - design-spec
  - auth
timestamp: 2026-09-25T06:25:34Z
---
**Status:** Approved — implemented 2026-09-25
**Date:** 2026-09-25
**Scope:** Compatibility fix for remote MCP servers that arrive with authorization already stored by upstream OpenCode (exemplar: `zoho-books`).
**Implementation:** Shipped behavior is documented in `docs/concepts/remote-mcp-oauth-compat.md` (dual-schema auth storage, URL binding, discovery refresh, safe HTTP errors, unchanged per-chat toggle/cache semantics).

## 1. Purpose

Make enabled remote MCP servers work when the user has already authorized them through upstream OpenCode, without re-doing authorization from scratch, and surface real HTTP authentication failures instead of masking them.

Concretely:

- ocode must read the authorization upstream OpenCode already persisted, attach it to remote MCP requests, and refresh it when it expires.
- When the server rejects a request (e.g. HTTP 401), the user must see an actionable error — status code and safe response detail — not a silent no-token request, a JSON decode failure, or a bogus `ok` status.

## 2. Background / Evidence

- `zoho-books` is an enabled remote MCP server, but every request returns HTTP 401 with a `WWW-Authenticate` header carrying RFC 9728 resource metadata.
- Saved authorization exists in OpenCode format: an expired access token (expiry 2026-05-09), a refresh token, a client ID, and a server URL.
- ocode's current auth storage format is incompatible and reads none of that.
- The 401 response body is plain text; ocode currently decodes it as JSON, so the error path reports a decode failure instead of the real unauthorized condition.
- The MCP CLI `list` command can display a server as `ok` even when the probe failed with 401.

## 3. Root Cause

Two independent defects, plus one classification bug:

1. **Schema mismatch.** ocode expects stored authorization as:

   ```json
   { "tokens": { "<server-name>": { "access_token": "...", ... } } }
   ```

   Upstream OpenCode stores:

   ```json
   { "<server-name>": {
       "tokens": { "accessToken": "...", "refreshToken": "...", "expiresAt": ..., "scope": "..." },
       "clientInfo": { "clientId": "..." },
       "serverUrl": "..."
   } }
   ```

   Because the shapes differ, ocode finds no token for the server at all: no token is attached, the request is unauthenticated, and the server answers 401. The stored access token is expired, but a usable refresh token and client ID exist — the fix must refresh, not just attach.

2. **No dynamic discovery / wrong error decoding.** ocode has no RFC 9728/RFC 8414 discovery path, so it cannot react to the `WWW-Authenticate` resource metadata that Zoho returns. Separately, the plain-text 401 body is decoded as JSON, turning a diagnosable unauthorized error into a decode error.

3. **CLI status misclassification.** The MCP CLI `list` status rendering can show `ok` for a server whose probe actually failed, hiding the 401 entirely.

## 4. Approved Scope & Architecture

### 4.1 Auth storage adapter (dual-schema, merge-safe)

- Reads must accept **both** schemas: ocode's native `{tokens:{name:{access_token,...}}}` and upstream's `{name:{tokens:{accessToken,refreshToken,expiresAt,scope},clientInfo:{clientId,...},serverUrl}}`.
- Writes must be **merge-safe**: when persisting a token for one server, unknown/foreign entries in the file — including upstream-format entries for other servers — must be preserved verbatim. Never wipe or rewrite entries this code path does not own.

### 4.2 Token attachment without static `oauth` config

- Remote MCP requests must attach a stored compatible token **even when the server has no static `oauth` configuration**. Presence of stored authorization alone is sufficient to authenticate the request.
- Attachment must be URL-bound (see §5): the stored entry is only used for the server URL it was saved against.

### 4.3 Expired-token refresh via discovery

When the stored compatible token is expired, on the 401 response:

1. Parse the `WWW-Authenticate` header for RFC 9728 resource metadata (protected resource metadata URL).
2. Fetch the protected-resource metadata, then the authorization-server metadata (RFC 8414) it references.
3. Perform a refresh-token grant using the stored client ID and refresh token.
4. Persist the rotated token in the **upstream-compatible schema** (so upstream OpenCode and ocode share one representation), preserving all other entries. Reads remain dual-schema (§4.1), so files mixing both formats stay valid.

Refresh is attempted **once** per failure. If the refresh request itself is rejected, stop and surface the reauthorization requirement — do not loop (see §5).

### 4.4 Status-before-decode error handling

- Check the HTTP status code **before** attempting to JSON-decode the response body.
- Unauthorized (401/403) and refresh failures must produce actionable errors that include:
  - the HTTP status code, and
  - safe response detail (e.g. truncated, redacted body excerpt or the `WWW-Authenticate` challenge info).
- Errors must **never** include tokens, refresh tokens, client secrets, or the full secret server URL (see §5).

### 4.5 MCP CLI list status classification

- Fix status classification in the MCP CLI `list` command so a failed probe can never render `ok`. Probe failures must display the real failure state with the actionable error from §4.4.

## 5. Security & Error Rules

- **URL-bound stored entries:** a stored authorization entry is only ever attached to requests to the server URL it was saved for. No cross-server or cross-URL reuse.
- **Redaction:** credentials (access/refresh tokens, client secrets, authorization headers) are redacted in all logs and error messages. The full secret server URL is never echoed; use a sanitized/redacted form.
- **No empty catches:** every error path handles or surfaces the error. No bare `catch {}` that swallows failures silently.
- **Refresh once:** at most one refresh attempt per failure; no refresh retry storms.
- **Refresh rejected ⇒ reauthorization required:** if the server rejects the refresh (invalid/expired refresh token), fail with a clear message telling the user to reauthorize this server. This means *reporting* that reauthorization is needed and stopping — the patch does not build a new authorization flow (see §7). Do **not** loop, do not keep retrying, and do **not** attempt generic dynamic client registration to work around it.

## 6. Testing / TDD

**Process:** tests must be written and **observed failing** against current behavior **before** any implementation is written. Implementation proceeds only after the failing tests exist.

Required coverage:

| # | Case |
|---|------|
| 1 | Dual-schema read (native + upstream format) |
| 2 | Merge-safe write (foreign/upstream entries preserved) |
| 3 | Token attached without static `oauth` config |
| 4 | `WWW-Authenticate` metadata discovery + refresh + persist in upstream schema |
| 5 | Refresh rejected ⇒ actionable reauthorization error, no loop |
| 6 | 401 with plain-text body ⇒ status checked before decode, actionable error |
| 7 | CLI `list` shows failure (never `ok`) on probe failure |
| 8 | Race safety and secret redaction (no credentials in errors/logs under concurrency) |

**Verification gates after implementation:**

1. Focused Go tests for the touched packages.
2. `gofmt` clean on changed files.
3. `go vet` and `go build` clean.
4. A live `tools/list` probe against Zoho succeeding — performed **without exposing secrets** in output or logs.

## 7. Non-Goals (this patch)

- No generic dynamic client registration or generic browser authorization flow.
- No change to "enabled" semantics for MCP servers.
- No redesign of the per-chat MCP toggle (existing per-chat MCP cache/toggle behavior is documented and **remains unchanged** — see §8).
- No unrelated MCP work.
- If stored refresh credentials turn out to be invalid, the patch **fails clearly for later reauthorization** rather than expanding scope to fix the credentials.

## 8. Working-Tree & Documentation Safety

- **Working tree:** the main tree has extensive unrelated WIP. Modify **only** auth/MCP/CLI source files touched by this fix and this spec file. Do not disturb, stage, or revert unrelated changes.
- **Documentation alignment:**
  - Existing per-chat MCP cache/toggle behavior is documented and unchanged by this patch — no doc edit needed for it beyond confirming no conflict (there is none). *(Confirmed: no conflict; a cross-reference was added to `docs/concepts/per-chat-mcp-toggle.md` pointing at the implementation doc.)*
  - The later implementation PR must update the relevant MCP documentation and `CHANGES.md` if appropriate (auth-storage format compatibility, refresh behavior, error surfacing). *(MCP documentation update: `docs/concepts/remote-mcp-oauth-compat.md`.)*

## 9. Acceptance Criteria

- `zoho-books` (enabled, upstream-authorized, expired token) serves `tools/list` successfully after automatic refresh.
- Refresh persists a rotated token in the upstream-compatible schema without clobbering other entries.
- A hard 401 renders an actionable error with status and safe detail — no JSON decode failure, no `ok` CLI status, no token/URL leakage.
- All §6 tests green; gofmt/vet/build clean; live probe clean of secrets.