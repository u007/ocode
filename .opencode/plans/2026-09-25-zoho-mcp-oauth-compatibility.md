# Zoho MCP OAuth compatibility implementation plan

Approved design: `docs/superpowers/specs/2026-09-25-zoho-mcp-oauth-compatibility-design.md` (commit `f7ffd747`).

## User expectation checklist

- [ ] An enabled upstream-OpenCode-compatible remote MCP credential is readable by ocode without static `oauth` config.
- [ ] Existing foreign/upstream auth entries survive ocode credential writes; no credential or metadata is silently dropped.
- [ ] A stored access token is URL-bound and attached as `Authorization: Bearer …` when valid.
- [ ] An expired compatible token refreshes once through Zoho's discovered OAuth metadata using the stored client ID and refresh token, and the rotated token is persisted safely.
- [ ] A rejected refresh fails clearly and does not loop; this patch does not add a new browser authorization flow.
- [ ] HTTP 401/plain-text and other non-2xx responses are reported as HTTP/auth failures, not JSON decoder errors, with secrets and the full server URL redacted.
- [ ] `ocode mcp list` never labels a failed probe `ok`.
- [ ] The live `zoho-books` endpoint enumerates tools and one harmless read/list tool call succeeds, or a precise external blocker is reported.
- [ ] Existing local/static-OAuth MCP behavior and per-chat enabled/cache behavior remain unchanged.

## Files expected to change

- `internal/auth/mcp_auth.go` — dual-schema storage model, URL-bound lookup, merge-safe writes, client metadata needed for refresh.
- `internal/auth/mcp_auth_test.go` — new TDD coverage.
- `internal/mcp/client.go` — stored-token loading, OAuth metadata discovery, refresh, status-before-decode, safe errors.
- `internal/mcp/client_test.go` — new TDD coverage using `httptest` only.
- `internal/mcpcli/commands.go` — correct list status classification and possibly probe formatting.
- `internal/mcpcli/commands_test.go` — new regression coverage for status classification.
- `CHANGES.md` and the existing curated MCP concept/gotcha documentation if the implementation changes documented behavior.

Do not touch unrelated files or the user's global/project MCP config as part of source implementation.

## TDD sequence

1. Add auth storage tests for upstream-schema token/client/serverURL loading, native-schema compatibility, URL mismatch rejection, and merge-safe persistence. Run them and record the expected failures.
2. Implement the auth storage adapter and rerun until green.
3. Add remote-client tests for:
   - valid stored token attached without static OAuth;
   - HTTP 401 plain text returning a typed/actionable error;
   - `WWW-Authenticate` resource metadata discovery and authorization-server discovery;
   - expired-token refresh using stored client ID/refresh token, token rotation persistence, and one retry;
   - refresh rejection returning a reauthorization-required error without retry loops;
   - no token/URL leakage in errors;
   - static OAuth behavior remaining intact.
   Run them against old code and record the expected failures.
4. Implement the remote-client changes until those tests pass.
5. Add a focused `mcpcli` regression proving failed probes render `fail`, run it failing first, then fix classification and rerun.
6. Update relevant documentation/CHANGES only after behavior is green.
7. Run focused tests, race-sensitive tests where practical, `gofmt`, `go vet` on changed packages, `go test` on changed packages, `go build ./...`, and a redacted live Zoho probe.
8. Re-check the working tree and confirm only intended files changed relative to the pre-implementation snapshot.

## Implementation constraints

- Follow Go 1.27 modern guidance returned by `use-modern-go`, especially typed errors, `any`, error wrapping, and current HTTP patterns.
- Write a failing regression test before touching implementation.
- Never log or return access tokens, refresh tokens, client secrets, full secret-bearing server URLs, or metadata containing credentials.
- No empty catches. Catch-and-rethrow paths must use the project logger when they are server/tool paths, with inline suppression comments only for known-benign cases.
- URL-bound credentials only: reject entries whose saved `serverUrl` differs from the configured remote URL.
- Refresh at most once per request; use a mutex/singleflight equivalent so concurrent tool enumeration cannot launch duplicate refreshes.
- Preserve the existing per-chat MCP cache/toggle semantics; do not redesign them.
- Use a real failing-test demonstration before implementation and keep the tests as regression guards.
