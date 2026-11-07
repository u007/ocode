# Publish / Share Project as a Browser Artifact — Design

Date: 2026-11-07
Status: Draft (decisions captured from user; advisor review pending)
Owner: TBD
Worktree: `.worktrees/publish-share` (branch `publish-share`)

## 1. Goal

Let a user turn a local (or remote) project into a **browser-accessible artifact** that
**stays up when the user's machine is off**, at a stable shareable URL, with per-artifact
access control (private / allow-list of emails / public), email invitations, and
persistent storage — so the published thing is usable, not a frozen screenshot.

This is a **new subsystem**, distinct from:
- "Share Entire Desktop" (`docs/concepts/desktop-share-token.md`,
  `internal/server/tailscale_share.go`): exposes the whole ocode server behind one
  all-powerful token.
- Embedded-browser proxy (`concepts/browse-per-tab-origin.md`): proxies a *live* dev
  server, not durable.

## 2. Scope & non-goals

In scope (phased — see §3):
- Publish a project (local or remote) as an immutable, versioned artifact.
- Serve that artifact at a shareable URL with a strict static handler.
- Access control: `private` / `allowlist` (emails) / `public`.
- Email invitations for allow-list members.
- Per-artifact capability token: mint, revoke, rotate, optional expiry, optional
  password gate.
- Persistence of artifact metadata + blobs (hub-side, so it survives laptop sleep).
- Optional bundled SQLite for the published app (Phase 1); Postgres in Phase 3.

Non-goals (v1): SSR/containers, multi-tenant billing, custom domains.

## 3. Phasing (per advisor)

- **Phase 1 — Static snapshots (this spec's implementation target).** Immutable static
  output. Visibility private / invite-by-email / public. Opaque capability tokens.
  Email via magic links. Hosted on the hub so it stays up. No containers.
- **Phase 2 — Server-side apps (separate, larger effort).** Each app isolated in a
  Podman container with its own volume, per-app DB credentials, backup/restore.
- **Phase 3 — Databases & "production ready" (separate, larger effort).** Plain
  statement: "production ready" for apps that use a database is NOT delivered by
  Phase 1. Phase 1 supports bundling a read-only or read-write SQLite file; Postgres/
  MySQL integration is Phase 3.

## 3.5 Hosting modes (decided)

The user requires the published artifact to **stay up when the machine is off**, so
Phase 1's primary target is **hub-hosted**:

- **Mode B (primary): hub-hosted.** Publish uploads the artifact (metadata + blobs) to
  the hub (`sync_url`, see §5). The hub serves `/p/...` (or a subdomain) and keeps it
  up. The local ocode instance is the authoring tool only.
- **Mode A (secondary/dev): self-hosted.** Local ocode serves `/p/...` via tailscale
  serve/funnel. Dies with the machine. Useful for dev/preview and tailnet-only sharing.

Routing rule: publishing goes through `sync_url` (the production hub), **never**
`backend_url` (`backend_url` is local-dev-only; see
`docs/concepts/backend-sync-url-split.md`). The hub today is config/auth sync only,
so Phase 1 requires new hub endpoints for artifact storage/serving (hub-side work).

**No silent fallback:** if the hub is unreachable, publish fails loudly (502
`remote-connect`-style error). Mode A (self-hosted) is an explicit, user-chosen
mode, never an automatic fallback from a failed hub publish.

## 4. Decisions captured from user answers

| # | Question | Decision |
|---|----------|----------|
| Q1 | Static vs live app | **Static snapshots** (Phase 1); live apps deferred to Phase 2. |
| Q2 | Survive restart / stay up | **Yes, hub-hosted** (Mode B) — stays up when laptop sleeps. |
| Q3 | Tailnet vs public | Both, chosen at publish time: `serve` (tailnet) default, `funnel` (public) opt-in with explicit confirmation. Auto-publish never funnels. |
| Q4 | "DB persistence" meaning | (a) artifact metadata store (hub-side SQLite/DB); (b) optional bundled SQLite for the published app (Phase 1); Postgres scoped connection (Phase 3). |
| Q5 | Origin isolation | **Separate subdomain per artifact** (see §10) — recommended by advisor for cookie/storage isolation. |
| Q6 | Build step | **Optional**, arbitrary user build command, run under existing permission + sandbox gates, opt-in per publish. |
| Q7 | Hosting location | **Hub-hosted (hub.mercstudio.com)** — stays up. (decided) |
| Q8 | Domain | **Subdomain per artifact** (see §10). Subdirectory form documented as fallback with same-origin limitation. |
| Q9 | Email | **Invitations to allow-list members via magic link** (confirmed). Whether to also **notify on publish** is still open (see §19). |
| Q10 | Build command allowlist | Arbitrary command, opt-in per publish (decided). |

## 5. References to existing ocode pieces (do not reinvent)

- Auth: `Server.checkAuth` / `authMiddleware` / `mediaAuthMiddleware`
  (`internal/server/server.go:879,919,945`). Master credential is Bearer / `?token=`
  (forbidden in `--remote`) / WS subprotocol / Basic. Remote mode accepts Bearer only.
- Capability precedent: `mediaTokenStore` (`internal/server/media_tokens.go`) — 256-bit
  random, single file+root+host, 6h TTL, in-memory, dies on restart. Copy this shape
  for per-artifact capabilities, but make them durable (hashed, on-disk/hub) per the
  share-token doc's "persist before handing out" rule.
- Share token: `internal/server/share_token.go`, `internal/desktop/share_token.go`
  (durable second credential, rotate-only revocation). Artifact tokens differ:
  per-artifact, immediate revoke via row delete.
- Project scoping: `allowedProjectRoots()` (`internal/server/handler.go:875`) — workdir
  + saved **local** projects only; remote excluded on purpose. Never widen.
- Handler project resolution: `h.workDir` is default only; sessions carry their own
  root. New directory-bound endpoints take explicit `?project=` validated against
  `allowedProjectRoots()`. Never `os.Getwd()` in a handler.
- Remote: `internal/server/remote_hosts.go` (connect cooldown, background connect with
  2s wait bound, drop no-op mid-connect, 503 `remote-connecting` vs 502). Remote
  projects publish via host's server at `/api/remote/{host}/api/artifacts/...`; a
  remote failure is 502 `remote-connect`, never a local fallback. `~` expanded only
  by owning machine.
- Tailscale: `internal/tailscale/tailscale.go` (`Expose`, `StartExposeWithKind`,
  `StartServeExpose`); `internal/server/tailscale_share.go`. One global mount per
  node; `serve` tailnet-only, `funnel` public on port 8443. Auto-share serve-only,
  fail-safe-off. (Mode A / dev only.)
- Hub/sync: `internal/sync` resolves via `sync.ResolveBaseURL` (default
  `https://hub.mercstudio.com`). Today config/auth sync only. Publishing routes
  through `sync_url` (hub), not `backend_url`. Phase 1 requires new hub endpoints.
- Storage: `paths.GlobalDataDir()` (`internal/paths/paths.go`), project slug = SHA-256
  of git root. Sessions at `<slug>/sessions/`. Logs via `paths.LogsDir()`. Tabs merge
  under cross-process lock. Per-project flock + skip-on-contention pattern from
  `md_discovery.go` / `discovery/cache.go`.
- Secret scan: `/api/secret/scan` (`handler_secret.go:85`) — use as pre-publish guard.
- Editor grants: `internal/server/handler_file_grants.go` +
  `concepts/file-editor-grants.md` (capability must never widen agent scope).
- Sandbox: `concepts/sandbox-permission-mode.md` — integrity-only, NOT a
  confidentiality/exfil boundary. Cannot be relied on for publish safety.
- Permissions: `internal/agent/permissions*` — new tools acting outside the agent's
  own files must set `ask`.

## 6. Architecture overview

```
Owner (ocode SPA) --authMiddleware--> /api/artifacts/*   (CRUD, publish, tokens, invites)
Anonymous visitor --artifactAuthMiddleware--> /p/{id}/{tok}/...   (static serve, Mode A)
Hub (sync_url) serves Mode B artifacts at <slug>.hub.mercstudio.com (or subdir fallback)
```

Invariants carried over:
- `Handler.mu` is a map lock, never a work lock: build/publish run outside it.
- Git subprocesses use `gitexec.Env()` (optional locks off).
- New routes registered in `Server.registerRoutes()` behind a deliberately chosen
  middleware.
- Artifact metadata never in `ocodeconfig.json`.
- Remote: host serves; no local fallback.
- No master `?token=` in URLs; capabilities are single-purpose, revocable.

## 7. Data model

New package `internal/artifacts`. Storage:
- Mode A (self-hosted): `<GlobalDataDir>/project/<slug>/artifacts/`.
- Mode B (hub): hub-side store (hub owns persistence; local client keeps a manifest
  cache).

SQLite `artifacts.db` (WAL, per-project flock, skip-on-contention):
- `artifacts(id PK, project_host, project_path, name, created_at, created_by, status, hosting_mode)`
- `artifact_versions(id PK, artifact_id FK, version, entrypoint, blob_manifest_hash, size_bytes, created_at, build_cmd, build_log_path, schema_version)`
- `artifact_files(version_id FK, path, blob_hash, size, mode, mime)`
- `access_tokens(id PK, artifact_id FK, token_hash, kind, expires_at, created_at, revoked_at, password_hash nullable, label)`
- `allowlist(artifact_id FK, email, invited_at, accepted_at, invite_token_hash)`
- `access_log(artifact_id, token_id, ip, ua, path, status, ts)` (rotating)

Blobs: content-addressed (`<xx>/<hash>` sha256), shared across versions within scope;
GC removes unreferenced.

## 8. Auth model (artifact capabilities)

New `internal/server/artifact_tokens.go` (durable, hashed), mirroring media-token
shape but durable:
- Token = 32 bytes base64url (256 bits), stored **hashed** (sha256); constant-time
  compare.
- Bound to exactly one `artifact_id`; never the master credential.
- Optional `expires_at`; revoke = set `revoked_at` (immediate).
- Optional Argon2id `password_hash` extra gate.
- Separate rate limiter (anonymous visitors must not poison owner's API limiter).
- `artifactAuthMiddleware`: resolves `{artifactId, token}` from path; checks hash,
  expiry, revocation, allow-list membership (email via magic-link cookie), password.
  Sets request context with `artifactID` + `accessLevel`. Returns 401/403/404
  uniformly (no existence oracle for private artifacts).

Magic-link invite: allow-list entry carries hashed single-use `invite_token_hash`
(TTL). Acceptance sets artifact-scoped HttpOnly/Secure/SameSite=Lax cookie.

## 9. Routes

Owner (behind `authMiddleware`):
- `POST   /api/artifacts`                    create from project
- `GET    /api/artifacts`                    list (paginated, sorted)
- `GET    /api/artifacts/{id}`               metadata + versions
- `POST   /api/artifacts/{id}/publish`       new version (optional build cmd)
- `POST   /api/artifacts/{id}/unpublish`     revoke all tokens
- `DELETE /api/artifacts/{id}`               delete + schedule GC
- `POST   /api/artifacts/{id}/tokens`        mint capability link
- `DELETE /api/artifacts/{id}/tokens/{tid}`  revoke one
- `POST   /api/artifacts/{id}/allowlist`     add email (+ send invite)
- `DELETE /api/artifacts/{id}/allowlist/{email}`
- `GET    /api/artifacts/{id}/access-log`    paginated

Visitor (behind `artifactAuthMiddleware`, separate limiter):
- `GET /p/{id}/{token}/` and `GET /p/{id}/{token}/*`   static serve (Mode A)
- `GET /p/{id}/invite/{inviteToken}`                   magic-link accept
- `POST /p/{id}/gate`                                   password check

Hub (Mode B): equivalent endpoints on hub under `sync_url` base, served from a
per-artifact subdomain.

All registered in `Server.registerRoutes()`.

## 10. URL scheme & origin isolation (decided)

- **Recommended: subdomain per artifact** — `<slug>.hub.mercstudio.com` (or a
  configurable origin via `OCODE_ARTIFACT_ORIGIN`). Separate origin gives cookie/
  storage isolation: published HTML/JS cannot read hub cookies or storage, and
  each artifact's cookies/storage are isolated from the others.
- Cost: wildcard DNS + wildcard cert; Cloudflare is the natural host (wrangler/
  Workers static assets or R2 + Worker). Pin dependency majors.
- **Fallback: subdirectory** `hub.mercstudio.com/p/{id}/{token}/...` — shares origin
  with the hub; documented same-origin cookie limitation (published content could
  read hub cookies). Only if subdomain provisioning is unavailable.

**Subdomain isolation requirements (must be specified in implementation):**
- Wildcard TLS cert for `*.hub.mercstudio.com` plus a reserved-slug list so a
  published artifact cannot claim `api`, `www`, `hub`, `auth`, `admin`, etc.
- Cookie scope: hub session cookies must NOT be sent to artifact subdomains
  (host-only or parent domain with explicit exclusion); artifact cookies scoped to
  the artifact subdomain only.
- CORS: hub API does not accept origins from artifact subdomains.
- Artifact id = 12-char base36 (not guessable alone; token is the real secret).
- Mode A (self-hosted): `https://<host>/p/{id}/{token}/...` via tailscale.

## 11. Publish flow

1. Owner picks project (validated via `allowedProjectRoots()` for local; remote
   routed to host).
2. Secret scan (`/api/secret/scan`) — block on hits unless overridden with a
   recorded reason.
3. File allowlist check — only allowed file types; exclude `.env`, secrets,
   gitignored files. Require explicit confirmation of the file list.
4. Optional build: run `buildCmd` via existing shell tool under permission +
   sandbox gates; capture stdout/stderr to `build_log_path`. Build failure aborts
   publish (no partial version).
5. Snapshot working tree / build output into content-addressed blob set.
6. Write `artifact_versions` + `artifact_files` rows in one transaction; compute
   manifest hash.
7. Mint initial capability token per access level (private: none; allowlist: one
   invite per email hashed single-use TTL; public: one long-lived rotatable token).
8. Upload to hub (Mode B) or store locally (Mode A).
9. Return URL(s); for allowlist send invites (§12).
10. Optional tailscale exposure (Mode A only): offer `serve` (tailnet) or `funnel`
    (public, explicit confirmation). Never funnel unattended.

All long work outside `Handler.mu`, under per-artifact/per-project lock, through
`crashguard.Go` for any goroutine. No fallbacks, no upsert-on-conflict.

## 12. Email invitations

- **Confirmed use:** invitations to allow-list members via magic link (email is the
  access gate for `allowlist` level).

**Interpretation of "email also" (user answer Q9):** the confirmed use is
invitation/access gating only. Whether the hub also **notifies** the owner or
allow-list when a new version goes live is a separate, still-open product
decision (see §19 item 1). This spec does not assume notify-on-publish.
- **Open:** whether to also **notify on publish** (email the owner/allow-list that
  a new version is live). Decide in §19.
- v1: ocode emits the invite URL + copyable message (Mode A) OR hub sends email
  (Mode B, preferred — server-side sending).
- v1.1 (optional): SMTP/Resend provider config in new `internal/mailer`, API key
  stored encrypted (like dbconnect), never plaintext in ocodeconfig.
- Invite token = 32 bytes, hashed, single-use, TTL (default 7 days).

## 13. Security must-haves

- **Separate origin/subdomain** for serving published content (§10) so it cannot
  reach ocode's session/API cookies.
- **Revoke, expiry, rate limits** on every capability token (§8).
- **File allowlist + secret scan + confirmation**: never publish `.env`, secrets,
  gitignored files, or `auth.json`. Explicit allowlist of file types; block on scan
  hits unless overridden with a recorded reason.
- **Never widen `allowedProjectRoots`**: publish resolves source via the existing
  boundary; artifact serving reads only from the blob store, never live tree.
- Path-containment check on every serve request (resolve + prefix, no `..`, no
  symlink escape).
- MIME allowlist + `Content-Type` from declared type; default
  `application/octet-stream` with `X-Content-Type-Options: nosniff`.
- Strict CSP: `default-src 'self'; script-src 'self'; frame-ancestors 'none'`.
- No directory listing; entrypoint resolution only.
- **Abuse of always-up host**: per-artifact rate limits, size quotas, expiry defaults,
  and unpublish/delete by owner. Hub enforces quotas.

**Build command (arbitrary, opt-in per publish):**
- Executed through the shared shell spawn path under the existing permission +
  sandbox gates (not a direct `exec.Command`).
- Sandbox is write-integrity only and does NOT prevent exfiltration; the build
  command can read `.env`/secrets and network. Mitigations: secret scan before
  publish (§11), file allowlist, and an explicit per-publish opt-in with a recorded
  reason. Publishing untrusted build output is the user's responsibility.
- **Who can publish/unpublish**: owner (master auth) only; allow-list members can
  view only.
- Remote: host serves; local never fetches artifact bytes for remote project.

## 14. DB persistence (Phase 1 scope)

- (a) Artifact metadata store: hub-side DB (§7).
- (b) Published app's DB: v1 supports bundling a SQLite file (read-only at serve
  time, or read-write persisted under the artifact dir with a documented backup
  path). Migrations via `migrations/` dir; record `schema_version` in
  `artifact_versions`; run pending migrations on serve (idempotent, transactional).
- Phase 3 (not this spec): Postgres/MySQL via scoped write-capable connection.

## 15. Lifecycle / GC / unpublish

- Unpublish: set all tokens `revoked_at` (instant), then async GC.
- Delete artifact: mark `status=deleted`, remove tokens, schedule GC of version
  rows + unreferenced blobs.
- GC goroutine: `crashguard.Go`, must not hold `Handler.mu`, per-project flock,
  idempotent, logs to `LogsDir()`.
- Access log rotation: cap rows/age (e.g. 30 days).

## 16. New files / functions (names only, no code)

Package `internal/artifacts`:
- `store.go`: `Store`, `NewStore`, `CreateArtifact`, `PublishVersion`, `GetArtifact`,
  `ListArtifacts`, `RevokeToken`, `MintToken`, `GC`, `Migrate`.
- `types.go`: `Artifact`, `Version`, `File`, `AccessToken`, `AllowEntry`.
- `blobstore.go`: `BlobStore`, `Put`, `Get`, `Has`, `GCUnreferenced`.

Server:
- `internal/server/handler_artifacts.go`: `HandleCreateArtifact`, `HandleListArtifacts`,
  `HandleGetArtifact`, `HandlePublish`, `HandleUnpublish`, `HandleDeleteArtifact`,
  `HandleMintToken`, `HandleRevokeToken`, `HandleAllowlistAdd/Remove`,
  `HandleAccessLog`.
- `internal/server/handler_artifact_serve.go`: `handleServeArtifact`.
- `internal/server/artifact_tokens.go`: `artifactTokenStore`, `artifactAuthMiddleware`,
  `issueArtifactToken`, `revokeArtifactToken`.
- `internal/server/handler_artifact_invite.go`: magic-link accept + cookie.

Hub-side (Mode B): equivalent endpoints on hub under `sync_url` base.

Web:
- `web/src/components/Artifacts/ArtifactPublishDialog.tsx`
- `web/src/components/Artifacts/ArtifactList.tsx`
- `web/src/api/artifacts.ts`

Config/env (add to `.env.example`):
- `OCODE_ARTIFACT_MAX_BYTES`, `OCODE_ARTIFACT_INVITE_TTL_DAYS`,
  `OCODE_ARTIFACT_LOG_RETENTION_DAYS`, `OCODE_ARTIFACT_ORIGIN` (separate origin),
  `OCODE_ARTIFACT_HUB_URL` (override; defaults to `sync_url`).

## 17. Process rules

- New env vars go in `.env.example`.
- Deferred items (Phase 2/3) go in `TODO.md`.
- Pin dependency majors (Argon2id, Cloudflare SDK, etc.).
- No fallbacks, no upsert-on-conflict (per CLAUDE.md).
- Bundle docs (`docs/concepts/*`, `docs/gotchas/*`) are written via
  `okf:context-writer`, not direct edits. This spec lives in
  `docs/superpowers/specs/` (outside the OKF bundle) so direct write is fine.
- Worktree: `.worktrees/publish-share` (branch `publish-share`).

## 18. Integration with existing code

- `Server.registerRoutes()`: owner routes behind `authMiddleware`; visitor routes
  behind `artifactAuthMiddleware` (new middleware, own limiter).
- `allowedProjectRoots()` reused to validate source project (local).
- Remote routing: reuse `/api/remote/{host}/api/...`; host serves `/p/...`.
- Tailscale: reuse `Expose`/`StartServeExpose`/funnel:8443; single global mount
  `/p/*` dispatched by artifact id (never N mounts) — Mode A only.
- `crashguard.Go` for GC goroutine; `paths.GlobalDataDir()` + slug for storage.
- Migrations: reuse SQLite migration pattern from `internal/session/sqlitestore.go`.
- Sync: publishing uses `sync.ResolveBaseURL` / `sync_url`, not `backend_url`.

## 19. Open questions (need user answers before code)

Q7–Q9 hosting/domain/email choices are now **decided** (see §3.5, §10, §12).
Remaining open:

1. **Notify on publish (email)** — should the hub email the owner/allow-list when a
   new version goes live, or is invitation-only enough? (Invitations themselves are
   confirmed.)
2. **Hub-side email sender** — no mail-sending code exists in this repo (grep found no
   SMTP/Resend/SendGrid/Mailgun). Mode B therefore requires either the hub to provide
   email sending, or ocode to ship an `internal/mailer`. Discoverable at implementation
   time; not a product decision.
3. **Quota / max artifact size defaults** — propose `OCODE_ARTIFACT_MAX_BYTES`
   default (e.g. 100 MB) and per-user artifact count cap.
4. **Subdomain provisioning** — no DNS automation exists in this repo; whether the hub
   (Cloudflare or other) provisions `*.hub.mercstudio.com` records is a hub-side
   capability. If unavailable, v1 uses the subdirectory fallback with the documented
   same-origin limitation. Discoverable at implementation time.

## 20. Next step

Confirm §19 items, then invoke `writing-plans` to produce the implementation plan.
No code or tests yet. Phase 2/3 are separate specs.
