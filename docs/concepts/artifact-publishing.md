---
type: Concept
title: Artifact publishing / sharing (static snapshots, Mode A)
description: 'Phase 1 publish/share: immutable static project snapshots served at /p/{artifact}/{token}/... from a content-addressed blob store, with durable hashed capability tokens, allowlist invites, password gate, uniform 404, and tailscale serve/funnel exposure that MUST keep the /p upstream path. Covers the data model, publish pipeline, access enforcement, and what is still deferred (Mode B hub, build execution, TTL/retention env vars).'
resource: docs/superpowers/specs/2026-11-07-publish-share-project-design.md
tags:
  - artifacts
  - publish
  - share
  - tailscale
  - security
  - tokens
  - blobstore
  - sqlite
  - static-hosting
  - mode-a
timestamp: 2026-11-07T10:54:53Z
---
# Artifact publishing / sharing (Phase 1: static snapshots)

The "publish/share project" subsystem turns a project into an immutable **static**
snapshot served at `/p/{artifactID}/{token}/...`. It is a new subsystem, distinct
from the two things it is often confused with:

- **Share Entire Desktop** (`concepts/desktop-share-token.md`, `internal/server/tailscale_share.go`)
  exposes the whole ocode server behind one all-powerful token. Do not reuse that model here.
- **Embedded browser proxy** (`concepts/browse-per-tab-origin.md`) proxies a *live*
  dev server; it is not durable and is not this.

Full design, decisions and phase plan:
`docs/superpowers/specs/2026-11-07-publish-share-project-design.md`. Deferrals:
`TODO.md` ("Deferred: publish/share project artifact (Phase 2/3)" and "Open: publish/share
artifact build command execution deferred").

**Phase 1 is static only.** No SSR, no containers, no per-app database. Mode B
(hub-hosted) is the *intended* production target but is **not implemented** yet — see
"Not implemented / do not assume" below. The shipped path is Mode A (self-hosted).

## File map

| Path | Role |
|---|---|
| `internal/artifacts/types.go` | `Artifact`, `Version`, `File`, `AccessToken`, `AllowEntry`, `AccessLogEntry`. |
| `internal/artifacts/store.go` | SQLite metadata store: schema, migrations, cross-process flock, FK enforcement, CRUD, invites, access log, token prune, GC. |
| `internal/artifacts/blobstore.go` | Content-addressed blob store under `<dir>/blobs/<xx>/<sha256>`. |
| `internal/server/artifact_tokens.go` | Durable hashed capability tokens, `artifactAuthMiddleware(Impl)`, password cookies, invite cookies, per-visitor rate limiting. |
| `internal/server/handler_artifacts.go` | Owner CRUD, publish, unpublish, delete, token mint/revoke, allowlist add/remove, access log. |
| `internal/server/handler_artifact_invite.go` | Invite accept (magic link) + password gate. |
| `internal/server/handler_artifact_serve.go` | Static serve from the blob store (Mode A public path). |
| `internal/server/tailscale_share.go` | Tailscale `serve`/`funnel` exposure, `newArtifactTailscaleShare`, `authConfigured`. |
| `internal/server/server.go` | Route registration (`:834`-`:866`). |

## Data model and storage

Machine-global store, **not** per project: `artifactStoreDir()` is
`<GlobalDataDir>/artifacts` (`internal/server/artifact_tokens.go:220-225`). The design
spec's per-project layout was not adopted.

SQLite (`artifacts.db`) opens with WAL, `busy_timeout=5000`, and
**`foreign_keys(ON)`** (`internal/artifacts/store.go:49`); the FK clauses in the schema
are vacuous without that pragma, so do not remove it. Tables
(`internal/artifacts/store.go:88-150`):

- `artifacts(id PK, project_host, project_path, name, created_at, created_by, status, hosting_mode)`
- `artifact_versions(id PK, artifact_id FK, version, entrypoint, blob_manifest_hash, size_bytes, created_at, build_cmd, build_log_path, schema_version)`
- `artifact_files(version_id FK, path, blob_hash, size, mode, mime)`
- `access_tokens(id PK, artifact_id FK, token_hash, kind, expires_at, created_at, revoked_at, password_hash, label)`
- `allowlist(artifact_id FK, email, invited_at, accepted_at, invite_token_hash)`
- `access_log(artifact_id, token_id, ip, ua, path, status, ts)`

Every store mutation runs under `withLock` — an in-process mutex plus a **cross-process
file flock** (`artifacts.lock`, 5s timeout), and contention is surfaced as `ErrLocked`
rather than silently running unlocked (`internal/artifacts/store.go:191-204`). Keep that
contract: a new write path that skips `withLock` reintroduces last-writer-wins.

Blobs are content-addressed: `Put` hashes with sha256 and stores at
`blobs/<first 2 hex>/<hash>`, deduping an existing file (`internal/artifacts/blobstore.go:42-82`).
`validateHash` rejects empty, separator-bearing and `..` hashes, and `pathFor`
re-checks containment (`:28-60`). `GCUnreferenced` deletes blobs not named by
`artifact_files` (`:121`); `Store.GC()` computes the referenced set and is invoked
after `DeleteArtifact` (`internal/server/handler_artifacts.go:539`).

Versions are immutable. `PublishVersion` inserts the version + its file rows in one
transaction and refuses an artifact that does not exist
(`internal/artifacts/store.go:312-341`). The serve path picks the newest version by an
**explicit sort key** (created_at, then version label, then id), never by trusting
`ListVersions` order (`latestArtifactVersion`, `internal/server/handler_artifact_serve.go:113-132`;
pinned by `TestLatestArtifactVersionPicksNewestExplicitly`).

## Publish pipeline (what `POST /api/artifacts/{id}/publish` actually does)

`HandlePublish` (`internal/server/handler_artifacts.go:266`):

1. Remote project → the request is proxied to the owning host's
   `/api/artifacts/{id}/publish`; there is **no local fallback** (`:266-283`,
   `proxyArtifactToRemote`).
2. **Server-side secret scan** — the client flag is ignored (`_ = scanHit`); the
   handler walks the project for `.env`/`.env.*`, `*.pem`, `*.key`, `credentials`,
   `secrets.{json,yaml,yml}`. A hit returns 409 unless an `override_reason` is supplied,
   which is logged (`:286-329`).
3. `build_cmd` is **parsed and stored, never executed** (`:330`, and the `buildDir`
   block's "no run" comment). See deferrals.
4. `notify_on_publish` is read per request (`:331`); see below.
5. Walk the project, skipping: symlinks, any dot-prefixed path segment,
   `node_modules`/`vendor`/`.git`, `.env*`, and any extension outside
   `allowedArtifactExt` (`:398`, helper at `:834-843`: html/htm/css/js/mjs/cjs/json/map,
   images, fonts, txt/xml/csv/pdf, audio/video, wasm). Files that fail the allowlist are
   silently omitted — a publisher asking "why is my file missing" is usually here.
6. Enforce `artifactMaxBytes()` — default 100 MiB, overridable by
   `OCODE_ARTIFACT_MAX_BYTES` (`:440`, handler at `:845-853`) — then
   `PublishVersion` and `SetArtifactStatus("active")` (`:446-462`). A new publish
   therefore **re-livens an unpublished artifact**.
7. If `notify`, email recipients are resolved **server-side**: the owner if
   `CreatedBy` looks like an email, plus every allowlist entry, deduped
   (`:466-491`). The flag is not persisted on the artifact, so "remember last value" is
   never true server-side.

## Serving path and access enforcement (`/p/...`)

Routes (`internal/server/server.go:834-866`): owner routes under `authMiddleware`;
visitor routes under `artifactAuthMiddleware`:

- `GET /p/{id}/{token}/...` → `handleServeArtifact`
- `GET /p/{id}/invite/{inviteToken}` → `HandleInviteAccept`
- `POST /p/{id}/gate` → `HandlePasswordGate`

`artifactAuthMiddlewareImpl` (`internal/server/artifact_tokens.go:351-452`) enforces, in order:

1. **Rate limiter first**, before any token or Argon2 work — a blocked client is
   429'd without touching the token store (`:354-357`). `artifactRL` is a limiter
   **separate** from the owner API's `s.rl` (`internal/server/handler.go:436-438`), shared
   by the gate handler and the middleware.
2. **Uniform 404.** Missing artifact, missing token, bad hash, wrong artifact, revoked,
   expired, failed password and failed invite are all `http.NotFound` — the public path
   never distinguishes 401/403/404 and so leaks no existence oracle (`:299-331`,
   `:358-451`; `internal/server/handler_artifact_serve.go:21-29`). Tests assert 404
   explicitly: `TestArtifactEmailGateFailsClosed`, `TestArtifactUnpublishedRefused`
   (`internal/server/artifact_security_test.go:95,311`).
3. `invite`/`gate` sub-routes get `accessLevel 0` and bypass token parsing (`:370-375`).
4. Token validate: sha256 the presented value, look it up, then a
   **constant-time compare** (`subtle.ConstantTimeCompare`, `:313`), artifact binding
   (`:316`), revocation (`:320`), and expiry (`:323`). An expiry in the past fails
   closed; a zero `expires_at` means never expires.
5. **Password gate**: the derived cookie is checked first (cheap), then a presented
   `?password=` / `X-Artifact-Password` is verified under a 4-slot Argon2
   semaphore; a busy semaphore is 429 `Retry-After: 1`, and the attempt is counted
   against the key *before* hashing (`:135-150`, `:397-424`).
6. **Allowlist gate, fail-closed**: if the artifact has *any* allowlist row, a
   server-verified invite cookie for **this** artifact is required — a self-asserted
   email never passes, and a lookup error denies (`:430-438`). This applies even to a
   token of kind `public`: the allowlist is independent of token kind.

Serving itself (`internal/server/handler_artifact_serve.go`):

- Only a live (`status == "active"`) artifact is served (`:25-29`); unpublish revokes
  every token *and* sets status, so old links die immediately.
- Path handling rejects any whole `..` segment **before** `path.Clean` (checking after
  Clean is vacuous; a name like `a..b.js` is allowed) (`:43-49`), then resolves the
  request to an `artifact_files` row, falling back to `Version.Entrypoint` when the
  request is `/` or `index.html` (`:76-84`). No directory listing, no live-tree reads.
- **Blob-only**: bytes come from `st.GetBlob(target.BlobHash)` (`:91`); the project
  directory is never touched during serve.
- Response headers are set per file: `Content-Type` from the declared MIME,
  `X-Content-Type-Options: nosniff`, and
  `Content-Security-Policy: sandbox allow-scripts; default-src 'self'; script-src 'self'; frame-ancestors 'none'`
  (`:97-98`). The `sandbox` directive gives published pages an **opaque origin**, so
  even on the same origin as the owner they cannot read owner storage or call the
  same-origin authenticated API. Do not drop `sandbox` from that header.
- Successful serves are written to `access_log` (`:99-107`); the visitor key comes from
  `artifactClientKey`, which trusts `X-Forwarded-For` **only** from a loopback peer
  (tailscale serve/funnel arrives from loopback) and takes the right-most hop
  (`internal/server/artifact_tokens.go:197-219`).

## Tokens, passwords and invites (crypto invariants)

- Token value = **32 random bytes, base64url, no padding** (`newArtifactTokenValue`,
  `internal/server/artifact_tokens.go:227-233`), 256 bits.
- **Only the sha256 hex is stored** (`issue`, `:269-292`); there is no plaintext token
  column. A lost token cannot be recovered, only rotated (mint a new one).
- Passwords use **Argon2id** (3 iterations, 64 MiB, 4 lanes, 32-byte output) with a
  random 16-byte salt, encoded `argon2id$salt$hash` (`hashPassword`, `:236-247`), and
  verified with `subtle.ConstantTimeCompare` (`:266`). Verifications are bounded to 4
  concurrent (`artifactArgon2Concurrency`, `:104-107`) because each allocates 64 MiB.
- Password cookie = `tokenID + "." + HMAC-SHA256(key=PasswordHash, msg="artifactID|tokenID")`
  (`passwordCookieValue`, `:162-166`). The raw password is never stored client-side, and
  changing the password changes the key, so old cookies stop verifying
  (`TestPasswordCookieDiesWithPasswordChange`).
- Invite cookie: acceptance stores the raw invite token client-side
  (`artifact_access_<id>`, HttpOnly/Secure/SameSite=Lax, path-scoped to
  `/p/<id>/`, `internal/server/handler_artifact_invite.go:42-50`) while the DB keeps
  only its sha256; the serve path re-hashes the cookie and looks up an **accepted** row
  (`inviteAccessOK`, `artifact_tokens.go:185-195`). Invites are single-use at accept
  time but the hash is deliberately retained so the cookie keeps working; removing the
  allowlist row revokes it (`internal/artifacts/store.go:816-859`).
- **Invite TTL defaults to 7 days** (`const InviteTTL = 7 * 24 * time.Hour`,
  `internal/artifacts/store.go:31`) and `OCODE_ARTIFACT_INVITE_TTL_DAYS` overrides it
  through `inviteTTL()` (`store.go:36-45`); a missing, non-numeric or non-positive
  value falls back to the default, so a bad env var can never make invites permanent.
  Enforced on accept and on lookup (`store.go:591`, `store.go:837`).
- **Tokens are never logged** — there is no `log`/`emitDebug` call anywhere in
  `artifact_tokens.go`, `handler_artifact_serve.go`, `handler_artifact_invite.go` or
  `internal/artifacts/*.go` (verified by grep). The only artifact log line records a
  secret-scan override reason (`handler_artifacts.go:327`). Keep it that way: never log
  a token, password, invite hash or password cookie.
- `PruneExpiredTokens` deletes rows whose non-empty `expires_at` is past
  (`internal/artifacts/store.go:638-650`); `artifactTokenStore.pruneExpired` is the
  caller (`internal/server/artifact_tokens.go:339-344`).

## Mode A vs Mode B

- **Mode A (implemented): self-hosted.** Local ocode serves `/p/...`; optional tailscale
  `serve` (tailnet-only, default) or `funnel` (public, explicit confirmation).
- **Mode B (intended primary, NOT implemented): hub-hosted** at the `sync_url` hub so
  artifacts stay up when the machine sleeps. Use **`sync_url`, never `backend_url`**
  (`backend_url` is local-dev-only; `concepts/backend-sync-url-split.md`). The spec
  states Phase 1 needs new hub-side storage/serving endpoints, and none exist in this
  tree. `Artifact.HostingMode` is accepted on create (`handler_artifacts.go:162-184`),
  persisted, and then **never consulted** — create defaults it to `"self"`. Treat
  `hosting_mode` as metadata-only until routing exists.
- The remote path in `handler_artifacts.go` proxies artifact requests to a **saved
  remote ocode host** (by `project_host`), not to the hub. Unreachable → 502
  (`stage "remote-connect"`), still connecting → 503 (`stage "remote-connecting"`);
  never a local fallback (`proxyArtifactToRemote`, `:744+`).

## DANGER: the tailscale upstream path must stay `/p`

Every artifact exposure instance **MUST** be built by `newArtifactTailscaleShare()`
(`internal/server/tailscale_share.go:124-130`). It is the only place that sets
`upstreamPath = "/p"`, and `upstreamTarget` appends it so tailscale proxies to
`localhost:<port>/p` (`:132-141`).

Why: `tailscale serve --set-path /p` **strips** the mount prefix before forwarding. A bare
`&tailscaleShare{mount: artifactTailscalePath}` (no `upstreamPath`) therefore forwards a
public `/p/api/...` request to the server **root** `/api/...`, i.e. to the owner's
authenticated API. The `upstreamPath` is what keeps the public mount confined to `/p/*`
artifact routes.

There are **four** alias fields on `Server` — `artifactShare`,
`artifactTailscaleShare`, `tsArtifactShare`, `artifactsShare` — and all four must be
constructed via the helper (`internal/server/server.go:204-207`). The upstream trap is
pinned by `TestArtifactShareUpstreamKeepsPathPrefix` (iterates all four),
`TestArtifactShareFunnelTargetKeepsPathPrefix`, and `TestUpstreamTargetTable`
(`internal/server/tailscale_artifact_upstream_test.go:35,54,92`). The desktop share is
deliberately different: empty `upstreamPath`, bare root target
(`TestDesktopShareTargetUnchanged`).

Also: the mount is a **single global `/p/*`**, so a second expose reuses the live mount
instead of re-mounting (re-mounting would orphan the first and its routes) —
`TestArtifactExposureMountsOnce`, and it must never clobber `/desktop`
(`TestArtifactExposureDoesNotClobberDesktopMount`).

## Funnel (public) requires auth

`handleStartArtifactTailscaleShare` (`internal/server/tailscale_share.go:536-561`)
defaults to tailnet-only `serve`. A public request (`?funnel=1` / `?public=1`) becomes
`kind "full"` and is **refused with 403** unless `s.authConfigured()` is true (`:554-558`):

```go
func (s *Server) authConfigured() bool {
	return s.remoteMode || s.username != "" || s.password != ""
}
```

(`:529-534`.) Use **that one predicate** for the funnel gate and for
`authMiddleware`/`mediaAuthMiddleware`; never re-derive `remoteMode || username || password`
inline, or the two can disagree and a public mount lands on an unauthenticated server.
Pinned by `TestArtifactShareFunnelRefusedWithoutAuth` and
`TestArtifactShareFunnelAllowedWithAuth` (`tailscale_artifact_upstream_test.go:115,150`).
An unattended expose must never funnel (`TestArtifactExposureFunnelOnlyOnExplicitConfirmation`).
All exposure routes are owner-only (`authMiddleware`), so an unauthenticated caller
cannot start one (`TestArtifactTailscaleRoutesRequireAuth`).

## Environment variables (verified status)

`.env.example:11-15` declares five `OCODE_ARTIFACT_*` vars, but **only two are wired**:

| Var | Status |
|---|---|
| `OCODE_ARTIFACT_MAX_BYTES` | Wired. Default 100 MiB (`handler_artifacts.go:845-853`). |
| `OCODE_ARTIFACT_INVITE_TTL_DAYS` | Wired. Default 7 days; invalid or `<=0` falls back (`store.go:36-45`). |
| `OCODE_ARTIFACT_LOG_RETENTION_DAYS` | **Declared only.** No access-log pruning exists. |
| `OCODE_ARTIFACT_ORIGIN` | **Declared only.** No artifact-origin plumbing. |
| `OCODE_ARTIFACT_HUB_URL` | **Declared only.** No hub client. |

## Not implemented / do not assume

Verified absences in this tree (do not describe these as shipped):

- **Build execution.** `build_cmd` is recorded on the version; it is never run and no
  build log is captured. `BuildLogPath` stays empty. Implementing it must go through the
  shared shell spawn path under permission + sandbox gates, never a direct
  `exec.Command` (`TODO.md`, spec §13).
- **Mode B / hub hosting.** No hub endpoints, no `sync_url` routing; `hosting_mode` is
  stored but inert.
- **Explicit access-level field.** There is no `access_level` column and no
  private/allowlist/public enum on the artifact. What exists: a visitor always needs a
  capability token; allowlist *presence* forces a verified invite regardless of token
  kind; token `kind` maps to an `accessLevel` int (`public*` → 2, else 1) that is put in
  the request context (`artifact_tokens.go:325-330`) but **never read by the serve
  path** (`AccessLevelFromContext` has no non-test consumer). Model "private" as "no
  token shared", "public" as "token, no allowlist".
- **Per-artifact subdomain / origin isolation.** Mode A is same-origin `/p/...`; the
  opaque-origin protection is the CSP `sandbox` directive, not a separate host.
- **`artifact_versions.blob_manifest_hash`** is written as `""` by the publish handler
  (`handler_artifacts.go:450`) and is not computed.
- **Access-log retention/rotation** — the table grows unbounded.

## Invariants to preserve (checklist before touching the feature)

1. Build every artifact `tailscaleShare` with `newArtifactTailscaleShare()`; never a
   bare struct literal. (`server.go:204-207`, `tailscale_share.go:124-130`)
2. Funnel start is 403 unless `authConfigured()`; one predicate for funnel + auth.
3. Missing/unauthorized visitor requests are **uniform 404** — no existence oracle.
4. Server only from the blob store; keep the CSP `sandbox` + `nosniff` headers.
5. Tokens sha256-at-rest, 32-byte values, Argon2id passwords, constant-time compares,
   expiry fails closed, separate `artifactRL`, no secrets in logs.
6. `InviteTTL` defaults to 7 days, is overridable by `OCODE_ARTIFACT_INVITE_TTL_DAYS`
   (invalid/`<=0` falls back), and is enforced on accept *and* lookup.
7. `notify_on_publish` defaults false, is per-request, and recipients come from the
   server-side allowlist.
8. Hub routing, when built, uses `sync_url`, never `backend_url`; no silent fallback.
9. Build-command execution stays deferred and, when added, goes through the permission +
   sandbox-gated shell path.
10. New store writes hold `withLock`; keep `foreign_keys(ON)` and the FK clauses.

## Tests that pin this behaviour

`internal/server/artifact_tokens_test.go` (issue/validate, revoke, expiry, password,
wrong artifact, constant-time compare, prune), `artifact_security_test.go` (owner-only
exposure routes, fail-closed email gate, password gate + cookie, cookie dies with
password change, per-key rate limit, blocked-before-password, semaphore bound,
unpublished refused, newest version, `..` segments, JSON gate),
`handler_artifacts_exposure_test.go` (single mount, funnel only on confirmation, routes
registered, desktop mount not clobbered), `tailscale_artifact_upstream_test.go`
(upstream `/p`, funnel auth gate, desktop unchanged), `internal/artifacts/store_test.go`
and `blobstore_test.go` (CRUD, pagination, publish, tokens, invite, access log, GC).

## Related

- `concepts/backend-sync-url-split.md` — `sync_url` vs `backend_url`.
- `concepts/desktop-share-token.md` — the different whole-desktop share.
- `concepts/auto-share-on-start.md` — tailnet-only boot exposure (shares the mechanism).
- `docs/superpowers/specs/2026-11-07-publish-share-project-design.md` — full design.
