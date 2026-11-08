---
type: Concept
title: Artifact publishing / sharing (static snapshots, Mode A + Mode B client)
description: 'Phase 1 publish/share: immutable static project snapshots served at /p/{artifact}/{token}/... from a content-addressed blob store, with durable hashed capability tokens, allowlist invites, password gate, uniform 404, a computed blob_manifest_hash, access-log retention pruning, and tailscale serve/funnel exposure that MUST keep the /p upstream path. Also covers the Mode B (hub) CLIENT upload path: hosting_mode=hub publish, per-publish visitor access (public/password/allowlist) applied through the hub, hub_token persistence, presigned per-blob upload, and the 502 no-fallback rule; the hub-side API itself lives in the separate kakiit repo and is not in this tree.'
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
  - mode-b
  - hub
timestamp: 2026-11-08T04:32:53Z
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

**Phase 1 is static only.** No SSR, no containers, no per-app database. The default
shipped path is Mode A (self-hosted). Mode B (hub-hosted) now has a **client-side
upload path implemented in this tree** — a `hosting_mode=hub` publish uploads the
snapshot to the hub — but the **hub-side API it calls lives in the separate `kakiit`
repo and is not in this tree**, and nothing in this repo proves a hub is deployed. See
"Not implemented / do not assume" below.

## File map

| Path | Role |
|---|---|
| `internal/artifacts/types.go` | `Artifact`, `Version`, `File`, `AccessToken`, `AllowEntry`, `AccessLogEntry`. |
| `internal/artifacts/store.go` | SQLite metadata store: schema, migrations, cross-process flock, FK enforcement, CRUD, invites, access log (incl. retention prune), token prune, GC, hub-token accessors. |
| `internal/artifacts/blobstore.go` | Content-addressed blob store under `<dir>/blobs/<xx>/<sha256>`. |
| `internal/artifacts/manifest.go` | `ManifestHash(files []File)` — deterministic version manifest hash written to `artifact_versions.blob_manifest_hash`. |
| `internal/artifacts/hubclient.go` | Mode B HTTP client: `HubClient`, `EnsureArtifact`, `UploadBlob` (presigned PUT + dedupe), `PublishVersion`, `PublicURL`, `NewHubPublicToken`. |
| `internal/artifacts/hubclient_access.go` | Mode B visitor access + invites: `SetArtifactAccess`, `AddArtifactInvite`, `HubInvite`. |
| `internal/server/artifact_hub.go` | Mode B upload orchestration: `publishArtifactToHub` — resolves the bearer and base URL, persists `hub_token`, then runs EnsureArtifact → SetArtifactAccess → UploadBlob (per file) → PublishVersion → AddArtifactInvite. |
| `internal/server/artifact_tokens.go` | Durable hashed capability tokens, `artifactAuthMiddleware(Impl)`, password cookies, invite cookies, per-visitor rate limiting, startup retention prune. |
| `internal/server/handler_artifacts.go` | Owner CRUD, publish (incl. the Mode B hand-off and `parseArtifactAccess`), unpublish, delete, token mint/revoke, allowlist add/remove, access log, `artifactLogRetention()`. |
| `internal/server/handler_artifact_invite.go` | Invite accept (magic link) + password gate. |
| `internal/server/handler_artifact_serve.go` | Static serve from the blob store (Mode A public path), access-log write + throttled retention prune. |
| `internal/server/tailscale_share.go` | Tailscale `serve`/`funnel` exposure, `newArtifactTailscaleShare`, `authConfigured`. |
| `internal/server/server.go` | Route registration (`:834`-`:866`). |

## Data model and storage

Machine-global store, **not** per project: `artifactStoreDir()` is
`<GlobalDataDir>/artifacts` (`internal/server/artifact_tokens.go:225-229`). The design
spec's per-project layout was not adopted.

SQLite (`artifacts.db`) opens with WAL, `busy_timeout=5000`, and
**`foreign_keys(ON)`** (`internal/artifacts/store.go:75`); the FK clauses in the schema
are vacuous without that pragma, so do not remove it. `Migrate` and its tables
(`internal/artifacts/store.go:106-197`):

- `artifacts(id PK, project_host, project_path, name, created_at, created_by, status, hosting_mode, hub_token)`
- `artifact_versions(id PK, artifact_id FK, version, entrypoint, blob_manifest_hash, size_bytes, created_at, build_cmd, build_log_path, schema_version)`
- `artifact_files(version_id FK, path, blob_hash, size, mode, mime)`
- `access_tokens(id PK, artifact_id FK, token_hash, kind, expires_at, created_at, revoked_at, password_hash, label)`
- `allowlist(artifact_id FK, email, invited_at, accepted_at, invite_token_hash)`
- `access_log(artifact_id, token_id, ip, ua, path, status, ts)`

`hub_token` (`internal/artifacts/store.go:123`) is the artifact's **hub public
capability token**, generated client-side on the first hub publish and persisted so a
re-publish keeps the same link; the hub stores only its sha256. `Migrate` is additive:
SQLite has no `ADD COLUMN IF NOT EXISTS`, so it probes
`pragma_table_info('artifacts')` and runs a guarded `ALTER TABLE … ADD COLUMN` for
stores created before hub publishing existed (`internal/artifacts/store.go:184-194`).
`SetArtifactHubToken` (`:872`) updates the row and returns `ErrNotFound` when no row
matched; `GetArtifactHubToken` (`:890`) returns `""` for an artifact that has never
been hub-published and `ErrNotFound` for an unknown id. Both run under `withLock`.

Every store mutation runs under `withLock` — an in-process mutex plus a **cross-process
file flock** (`artifacts.lock`, 5s timeout), and contention is surfaced as `ErrLocked`
rather than silently running unlocked (`internal/artifacts/store.go:231-244`). Keep that
contract: a new write path that skips `withLock` reintroduces last-writer-wins.

Blobs are content-addressed: `Put` hashes with sha256 and stores at
`blobs/<first 2 hex>/<hash>`, deduping an existing file (`internal/artifacts/blobstore.go:62-82`).
`validateHash` rejects empty, separator-bearing and `..` hashes, and `pathFor`
re-checks containment (`:28-60`). `GCUnreferenced` deletes blobs not named by
`artifact_files` (`:121`); `Store.GC()` computes the referenced set and is invoked
after `DeleteArtifact` (`internal/server/handler_artifacts.go:671`).

Versions are immutable. `PublishVersion` inserts the version + its file rows in one
transaction and refuses an artifact that does not exist
(`internal/artifacts/store.go:350-400`). The serve path picks the newest version by an
**explicit sort key** (created_at, then version label, then id), never by trusting
`ListVersions` order (`latestArtifactVersion`, `internal/server/handler_artifact_serve.go:118-138`;
pinned by `TestLatestArtifactVersionPicksNewestExplicitly`).

**`blob_manifest_hash` is computed at publish time.** `ManifestHash`
(`internal/artifacts/manifest.go:14`) hashes the sorted `path + "\x00" + blob_hash`
pairs, newline-joined, and the publish handler writes the result
(`internal/server/handler_artifacts.go:552-570`). Size, mode and MIME are deliberately
excluded from the input, so a chmod or MIME re-detection alone does not change the
hash, and walk order is irrelevant.

**Access-log retention is wired.** `Store.PruneAccessLogOlderThan(maxAge)`
(`internal/artifacts/store.go:660-678`) deletes rows whose `ts` is older than the
cutoff and is a no-op for `maxAge <= 0` (keep forever).
`Store.MaybePruneAccessLog(maxAge)` (`internal/artifacts/store.go:684-699`) runs it at
most once per `accessLogPruneInterval` (1 hour), recording the **attempt** — success
or failure — in the atomic `lastAccessPrune` field (`internal/artifacts/store.go:64-68`)
before running, so concurrent callers do not stampede and a failed prune retries on the
next interval. It is invoked off the request path via `crashguard.Go` after
`LogAccess` (`internal/server/handler_artifact_serve.go:109-112`) and unconditionally at
store construction in `newArtifactTokenStore` (`internal/server/artifact_tokens.go:127-131`).

## Publish pipeline (what `POST /api/artifacts/{id}/publish` actually does)

`HandlePublish` (`internal/server/handler_artifacts.go:360`):

1. Remote project → the request is proxied to the owning host's
   `/api/artifacts/{id}/publish`; there is **no local fallback** (`:360-372`,
   `proxyArtifactToRemote`).
2. **Server-side secret scan** — the client flag is ignored (`_ = scanHit`); the
   handler walks the project for `.env`/`.env.*`, `*.pem`, `*.key`, `credentials`,
   `secrets.{json,yaml,yml}`. A hit returns 409 unless an `override_reason` is supplied,
   which is logged (`:401-434`).
3. `build_cmd` is **parsed and stored, never executed** (`:436`, and the `buildDir`
   block's "no run" comment). See deferrals.
4. `notify_on_publish` is read per request (`:437`); see below.
5. Walk the project, skipping: symlinks, any dot-prefixed path segment,
   `node_modules`/`vendor`/`.git`, `.env*`, and any extension outside
   `allowedArtifactExt` (`:456-545`, helper at `:970-979`: html/htm/css/js/mjs/cjs/json/map,
   images, fonts, txt/xml/csv/pdf, audio/video, wasm). Files that fail the allowlist are
   silently omitted — a publisher asking "why is my file missing" is usually here.
6. Enforce `artifactMaxBytes()` — default 100 MiB, overridable by
   `OCODE_ARTIFACT_MAX_BYTES` (`:546-550`, handler at `:981-989`) — then
   `PublishVersion` and `SetArtifactStatus("active")` (`:552-570`). A new publish
   therefore **re-livens an unpublished artifact**.
7. **Mode B hand-off.** If `strings.EqualFold(a.HostingMode, "hub")`, the handler calls
   `publishArtifactToHub` (`:577-586`). Success sets `hub_url` to the hub share URL; a
   failure writes **HTTP 502** with body `hub publish: <err>` and **nothing is uploaded
   to the hub**. There is deliberately **no local fallback**: a publish requested as
   hub-hosted never silently degrades to a self-hosted link that dies with this
   machine. Note the ordering — the local version and `active` status are written
   *before* this step, so a 502 means "the local snapshot exists, the hub copy does
   not"; it does not roll the local publish back.
8. If `notify`, email recipients are resolved **server-side**: the owner if
   `CreatedBy` looks like an email, plus every allowlist entry, deduped
   (`:590-619`). The flag is not persisted on the artifact, so "remember last value" is
   never true server-side.
9. **Mail delivery is best-effort and reported.** With no mailer configured
   `sendEmail` returns `errNoMailer`; the publish response carries
   `email_sent`/`notify_sent` reflecting actual delivery, the invite response carries
   `email_sent`, and failures are logged. The invite token is always returned, so a
   failed email degrades to manual link sharing, never to a lost invite.

The JSON response carries `version`, `artifact`, `notify_sent`, `email_sent` and
`hub_url` (`:621`). `hub_url` is `""` for a Mode A publish and the
`<hub>/p/<id>/<token>/` URL for a successful Mode B publish. The SPA type mirrors this
as an optional field (`web/src/api/artifacts.ts`, `PublishArtifactResponse.hub_url`).

## Visitor access on publish (`access_mode`)

A publish body may carry a visitor access mode. `parseArtifactAccess`
(`internal/server/handler_artifacts.go:171`) validates it **before** the snapshot walk, so
a bad request costs no blob work:

- `""` (absent) → leave the hub's current setting alone; `public` → no password;
  `password` → 8..200 chars; `allowlist` → 1..50 valid emails (`invite_emails`, a JSON
  array or a comma-separated string via `getStringSlice`, `:122`); anything else → 400.
  `normalizeInviteEmails` (`:147`) trims, lowercases, requires `@` and no whitespace, and
  dedupes.
- **Mode A never carries an access mode.** `password`/`allowlist` on a non-hub artifact is
  a 400 (`:393-397`) — local access is capability tokens plus the allowlist, and a request
  that cannot take effect is refused rather than silently ignored.
- **Mode B ordering:** `SetArtifactAccess` runs after `EnsureArtifact` and *before* any
  blob upload or version publish (`internal/server/artifact_hub.go:63-67`), so an access
  failure leaves **no live version** behind (the artifact row alone serves nothing).
  Invites are created only after the version is live (`:81-88`); a per-email failure is
  collected in `InviteErrors` and does not undo the publish.

Hub calls live in `internal/artifacts/hubclient_access.go`: `SetArtifactAccess` (`:14`,
`PUT /access`) and `AddArtifactInvite` (`:40`, `POST /invites`). The invite carries the
same public token the client already persists for the share link; the hub stores only its
sha256 and returns `inviteUrl` even with no mail provider. Neither the password nor the
token is logged or put in error text.

The publish response reports `access_mode`, `hub_invites` and `invite_errors`
(`handler_artifacts.go:621-630`). The SPA maps the access fields explicitly
(`web/src/api/artifacts.ts:258`, invites at `:261`) — **the existing `mode` field is a
legacy alias for `hosting_mode`, so the access mode must never travel as `mode`**
(`:253-257`). The dialog adds the select (`ArtifactPublishDialog.tsx:211`), the password
field (`:221`, client-side 8-char check at `:91`) and the invite textarea (`:236`, split
on commas/spaces/newlines, `:95`).

Pinned by `TestParseArtifactAccess`,
`TestPublishArtifactToHubAppliesAccessAndCollectsInviteErrors`, the handler 400 case, and
`TestHubClientSetArtifactAccess`/`TestHubClientAddArtifactInvite` plus the dialog tests.

## Mode B client upload path (`hosting_mode=hub`)

`publishArtifactToHub` (`internal/server/artifact_hub.go:32`) runs **after** the local
publish succeeds, and only for a hub-hosted artifact. It is the only place that talks
to the hub:

1. **Bearer.** `ocodesync.LoadToken()` (`internal/sync/sync.go:254`); a missing file is
   not an error — it means "not logged in", which surfaces as
   `hub: not connected; run the sync login flow first`. A hub publish therefore
   requires an active ocode sync login.
2. **Base URL.** `h.syncClient().BaseURL` — the **resolved `sync_url`** (per-machine
   config `OCODE.SyncURL`, then `OCODE_SYNC_URL`, then the production default;
   `internal/sync/client.go:12-20`). Never `backend_url`, and never
   `OCODE_ARTIFACT_HUB_URL` (still unread — see the env-var table).
3. **Public token, generated once.** `st.GetArtifactHubToken(a.ID)` is read first; a
   non-`ErrNotFound` error aborts the publish. An empty value means "never
   hub-published", so a fresh `artifacts.NewHubPublicToken()` (32 random bytes,
   base64url without padding, `internal/artifacts/hubclient.go:38`) is generated and
   persisted via `st.SetArtifactHubToken`. A re-publish reuses the stored token, so the
   share link is stable; the hub keeps only its sha256.
4. **`EnsureArtifact`** — `POST /api/ocode/artifacts` with `{id, name, token}`
   (`hubclient.go:96`). The id is ocode's own artifact id, which makes the call
   idempotent: the hub returns the existing artifact when the client-supplied id
   already belongs to this user. The public token is sent here, on creation only.
5. **`UploadBlob` per file** (`hubclient.go:120`) — POST `/:id/blobs` with `{hash, size}`,
   which answers `{exists, uploadUrl}`. When `exists` is true the bytes are **not**
   sent (content-addressed dedupe across every artifact on the hub); otherwise the
   blob is PUT directly to the presigned `uploadUrl` with
   `Content-Type: application/octet-stream`. Bytes come from `st.GetBlob(f.BlobHash)`,
   so the hub receives exactly the content-addressed bytes the local store holds.
6. **`PublishVersion`** (`hubclient.go:163`) — POST `/:id/versions` with `entrypoint`,
   `buildCmd` and the file list (`path`,`blobHash`,`size`,`mode`,`mime`), finalising an
   immutable version from blobs already uploaded.
7. **`PublicURL`** (`hubclient.go:62`) returns `<base>/p/<id>/<token>/` — the hub serves
   `/p` on its own origin, so a Mode B link is **not** on this machine.

**Error handling and secret hygiene.** Every non-2xx JSON-API response goes through
`statusError` (`hubclient.go:88`), which reads the body through a 4096-byte
`io.LimitReader` so the error is actionable without dumping an arbitrary response; the
presigned S3 PUT reports its status only. The sync bearer exists solely in the
`Authorization: Bearer …` header (`hubclient.go:66-86`): `HubClient` has no logger and
never serialises `Token`, and no artifact code path logs it. Keep it that way.

**Hub contract this consumes** (implemented in the separate `kakiit` repo, not in this
tree): `POST /api/ocode/artifacts` accepts `{id,name,slug?,token?}` and is idempotent
for the same user; the blob and version endpoints are `POST
/api/ocode/artifacts/:id/blobs` and the matching `/versions`; and the public route is
`GET /p/:id/:token/*` with a uniform 404. Treat that as an external contract: a change
breaks the client silently, because nothing in this repo exercises a live hub.

## Serving path and access enforcement (`/p/...`)

Routes (`internal/server/server.go:834-866`): owner routes under `authMiddleware`;
visitor routes under `artifactAuthMiddleware`:

- `GET /p/{id}/{token}/...` → `handleServeArtifact`
- `GET /p/{id}/invite/{inviteToken}` → `HandleInviteAccept`
- `POST /p/{id}/gate` → `HandlePasswordGate`

`artifactAuthMiddlewareImpl` (`internal/server/artifact_tokens.go:356-455`) enforces, in order:

1. **Rate limiter first**, before any token or Argon2 work — a blocked client is
   429'd without touching the token store (`:360-363`). `artifactRL` is a limiter
   **separate** from the owner API's `s.rl` (`internal/server/handler.go:436-438`), shared
   by the gate handler and the middleware.
2. **Uniform 404.** Missing artifact, missing token, bad hash, wrong artifact, revoked,
   expired, failed password and failed invite are all `http.NotFound` — the public path
   never distinguishes 401/403/404 and so leaks no existence oracle (`:304-335`,
   `:356-455`; `internal/server/handler_artifact_serve.go:21-29`). Tests assert 404
   explicitly: `TestArtifactEmailGateFailsClosed`, `TestArtifactUnpublishedRefused`
   (`internal/server/artifact_security_test.go:95,311`).
3. `invite`/`gate` sub-routes get `accessLevel 0` and bypass token parsing (`:375-380`).
4. Token validate: sha256 the presented value, look it up, then a
   **constant-time compare** (`subtle.ConstantTimeCompare`, `:318`), artifact binding
   (`:321`), revocation (`:324`), and expiry (`:327`). An expiry in the past fails
   closed; a zero `expires_at` means never expires.
5. **Password gate**: the derived cookie is checked first (cheap), then a presented
   `?password=` / `X-Artifact-Password` is verified under a 4-slot Argon2
   semaphore; a busy semaphore is 429 `Retry-After: 1`, and the attempt is counted
   against the key *before* hashing (`:140-157`, `:405-429`).
6. **Allowlist gate, fail-closed**: if the artifact has *any* allowlist row, a
   server-verified invite cookie for **this** artifact is required — a self-asserted
   email never passes, and a lookup error denies (`:435-448`). This applies even to a
   token of kind `public`: the allowlist is independent of token kind.

Serving itself (`internal/server/handler_artifact_serve.go`):

- Only a live (`status == "active"`) artifact is served (`:25-30`); unpublish revokes
  every token *and* sets status, so old links die immediately.
- Path handling rejects any whole `..` segment **before** `path.Clean` (checking after
  Clean is vacuous; a name like `a..b.js` is allowed) (`:44-52`), then resolves the
  request to an `artifact_files` row, falling back to `Version.Entrypoint` when the
  request is `/` or `index.html` (`:80-87`). No directory listing, no live-tree reads.
- **Blob-only**: bytes come from `st.GetBlob(target.BlobHash)` (`:92`); the project
  directory is never touched during serve.
- Response headers are set per file: `Content-Type` from the declared MIME,
  `X-Content-Type-Options: nosniff`, and
  `Content-Security-Policy: sandbox allow-scripts; default-src 'self'; script-src 'self'; frame-ancestors 'none'`
  (`:97-99`). The `sandbox` directive gives published pages an **opaque origin**, so
  even on the same origin as the owner they cannot read owner storage or call the
  same-origin authenticated API. Do not drop `sandbox` from that header.
- Successful serves are written to `access_log` (`:101-108`); the visitor key comes from
  `artifactClientKey`, which trusts `X-Forwarded-For` **only** from a loopback peer
  (tailscale serve/funnel arrives from loopback) and takes the right-most hop
  (`internal/server/artifact_tokens.go:205-223`).

## Tokens, passwords and invites (crypto invariants)

- Token value = **32 random bytes, base64url, no padding** (`newArtifactTokenValue`,
  `internal/server/artifact_tokens.go:232-238`), 256 bits.
- **Only the sha256 hex is stored** (`issue`, `:274-302`); there is no plaintext token
  column. A lost token cannot be recovered, only rotated (mint a new one).
- Passwords use **Argon2id** (3 iterations, 64 MiB, 4 lanes, 32-byte output) with a
  random 16-byte salt, encoded `argon2id$salt$hash` (`hashPassword`, `:241-251`), and
  verified with `subtle.ConstantTimeCompare` (`:271`). Verifications are bounded to 4
  concurrent (`artifactArgon2Concurrency`, `:105-108`) because each allocates 64 MiB.
- Password cookie = `tokenID + "." + HMAC-SHA256(key=PasswordHash, msg="artifactID|tokenID")`
  (`passwordCookieValue`, `:167-171`). The raw password is never stored client-side, and
  changing the password changes the key, so old cookies stop verifying
  (`TestPasswordCookieDiesWithPasswordChange`).
- Invite cookie: acceptance stores the raw invite token client-side
  (`artifact_access_<id>`, HttpOnly/Secure/SameSite=Lax, path-scoped to
  `/p/<id>/`, `internal/server/handler_artifact_invite.go:42-50`) while the DB keeps
  only its sha256; the serve path re-hashes the cookie and looks up an **accepted** row
  (`inviteAccessOK`, `artifact_tokens.go:190-204`). Accepting an artifact invite does
  **not** clear `invite_token_hash` (only `accepted_at` is set,
  `internal/artifacts/store.go:921-953`), which is why the cookie keeps working;
  deleting the allowlist row removes it (`DeleteAllowEntry`,
  `internal/artifacts/store.go:1029-1037`) and the lookup then fails closed
  (`GetAcceptedAllowEntryByInviteHash`, `:958-980`).
- **Invite TTL defaults to 7 days** (`const InviteTTL = 7 * 24 * time.Hour`,
  `internal/artifacts/store.go:33`) and `OCODE_ARTIFACT_INVITE_TTL_DAYS` overrides it
  through `inviteTTL()` (`store.go:38-48`); a missing, non-numeric or non-positive
  value falls back to the default, so a bad env var can never make invites permanent.
  Enforced on accept and on lookup (`store.go:611`, `store.go:940`).
- **No secret is ever logged.** `store.go` contains exactly one `log.Printf` — the
  startup access-log prune failure at `internal/artifacts/store.go:697` — and it
  carries only the error. The pre-existing artifact log lines record a secret-scan
  override reason (`handler_artifacts.go:433`) and mail-delivery failures
  (`handler_artifacts.go` publish/invite branches); none includes a token, password,
  invite hash, password cookie or the sync bearer. Keep it that way: a new log line
  must not print any of those values.
- `PruneExpiredTokens` deletes rows whose non-empty `expires_at` is past
  (`internal/artifacts/store.go:725-734`); `artifactTokenStore.pruneExpired` is the
  caller (`internal/server/artifact_tokens.go:344-349`).

## Mode A vs Mode B

- **Mode A (default, implemented): self-hosted.** Local ocode serves `/p/...`;
  optional tailscale `serve` (tailnet-only, default) or `funnel` (public, explicit
  confirmation). Visitor access there is capability tokens plus the allowlist; a
  publish-time `password`/`allowlist` is refused (see "Visitor access on publish").
- **Mode B (client implemented; hub-side API out of tree): hub-hosted** at the
  `sync_url` hub so artifacts stay up when the machine sleeps. A create request may
  pass `hosting_mode` (`handler_artifacts.go:256-259`, default `"self"`); the value is
  persisted and **is consulted at publish time** — only a case-insensitive `"hub"`
  takes the Mode B path (`handler_artifacts.go:577`), everything else is Mode A. The
  upload itself is described above; `hosting_mode=hub` with no sync login fails the
  publish with 502 rather than falling back to Mode A. Use **`sync_url`, never
  `backend_url`** (`backend_url` is local-dev-only;
  `concepts/backend-sync-url-split.md`).
- The remote path in `handler_artifacts.go` proxies artifact requests to a **saved
  remote ocode host** (by `project_host`), not to the hub. Unreachable → 502
  (`stage "remote-connect"`), still connecting → 503 (`stage "remote-connecting"`);
  never a local fallback (`proxyArtifactToRemote`, `:882+`).

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
`kind "full"` and is **refused with 403** unless `s.authConfigured()` is true (`:555-557`):

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

`.env.example:13-22` declares five `OCODE_ARTIFACT_*` vars, and **three are wired**:

| Var | Status |
|---|---|
| `OCODE_ARTIFACT_MAX_BYTES` | Wired. Default 100 MiB (`handler_artifacts.go:981-989`). |
| `OCODE_ARTIFACT_INVITE_TTL_DAYS` | Wired. Default 7 days; invalid or `<=0` falls back (`store.go:38-48`). |
| `OCODE_ARTIFACT_LOG_RETENTION_DAYS` | Wired. Read by `artifactLogRetention()` (`handler_artifacts.go:994-1008`) and applied by the store's throttled prune. Unset/invalid/negative → 30 days; `0` → keep forever. |
| `OCODE_ARTIFACT_ORIGIN` | **Declared only.** No artifact-origin plumbing. |
| `OCODE_ARTIFACT_HUB_URL` | **Declared only.** The hub client ignores it: the base URL is the resolved `sync_url` (`h.syncClient().BaseURL`), and `.env.example` says so ("defaults to sync_url; NOT read yet"). |

Do not set the last two expecting an effect. Note that `.env.example`'s header comment
still introduces the whole block as "Mode B (hub-hosted) is not implemented yet"; that
is now only true of the **hub side**, not of the client upload path above.

## Not implemented / do not assume

Verified absences in this tree (do not describe these as shipped):

- **Build execution.** `build_cmd` is recorded on the version; it is never run and no
  build log is captured. `BuildLogPath` stays empty. Implementing it must go through the
  shared shell spawn path under permission + sandbox gates, never a direct
  `exec.Command` (`TODO.md`, spec §13).
- **The hub itself, real end-to-end Mode B coverage, and hub deployment.** The
  **client upload path is implemented here and both halves are tested here**
  (`hosting_mode=hub` → `publishArtifactToHub` → `HubClient`):
  `internal/artifacts/hubclient_test.go` tests the client and
  `internal/server/artifact_hub_test.go` tests the server-level hand-off, both against
  a local `httptest` **hub stub**. What is **not** proven is the **real hub + S3** path
  end to end, or that the hub is deployed at all — the hub API it calls lives in the
  separate **`kakiit` repo** and is not in this tree. Do not describe Mode B as shipped
  end to end.
- **Explicit access-level field.** There is no `access_level` column and no
  private/allowlist/public enum on the artifact. What exists: a visitor always needs a
  capability token; allowlist *presence* forces a verified invite regardless of token
  kind; token `kind` maps to an `accessLevel` int (`public*` → 2, else 1) that is put in
  the request context (`artifact_tokens.go:451-453`) but **never read by the serve
  path** (`AccessLevelFromContext` has no non-test consumer). Model "private" as "no
  token shared", "public" as "token, no allowlist". On Mode B the visitor mode is a
  hub-side setting applied per publish (`access_mode`), not a local column.
- **Per-artifact subdomain / origin isolation.** Mode A is same-origin `/p/...`; the
  opaque-origin protection is the CSP `sandbox` directive, not a separate host. A Mode B
  link is on the hub's own origin, which is the hub's concern, not configured here.

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
8. Hub uploads use the resolved **`sync_url`** (`h.syncClient().BaseURL`), never
   `backend_url` and never `OCODE_ARTIFACT_HUB_URL`; a hub failure is a 502 with
   **no local fallback**.
9. Build-command execution stays deferred and, when added, goes through the permission +
   sandbox-gated shell path.
10. New store writes hold `withLock`; keep `foreign_keys(ON)` and the FK clauses.
11. `ManifestHash` covers `path + blob_hash` only — never size/mode/MIME — and is
    order-independent (sorted input). A metadata-only change must not invalidate the
    manifest hash.
12. The hub public token is generated **once** and persisted in `artifacts.hub_token`;
    regenerating it moves every published link. The sync bearer goes only in the
    `Authorization` header and is never logged.
13. The hub upload runs after the local publish and never rolls it back; a 502 means
    "local snapshot exists, hub copy does not".
14. `SetArtifactAccess` runs after `EnsureArtifact` and before any version publish, and
    invites run after the version is live; a per-email invite failure never undoes the
    publish. Mode A access stays capability tokens + allowlist — never silently accept a
    mode it cannot apply.

## Tests that pin this behaviour

`internal/server/artifact_tokens_test.go` (issue/validate, revoke, expiry, password,
wrong artifact, constant-time compare, prune), `artifact_security_test.go` (owner-only
exposure routes, fail-closed email gate, password gate + cookie, cookie dies with
password change, per-key rate limit, blocked-before-password, semaphore bound,
unpublished refused, newest version, `..` segments, JSON gate),
`handler_artifacts_exposure_test.go` (single mount, funnel only on confirmation, routes
registered, desktop mount not clobbered), `tailscale_artifact_upstream_test.go`
(upstream `/p`, funnel auth gate, desktop unchanged), `internal/artifacts/store_test.go`
(CRUD, pagination, publish, tokens, invite, access log, GC, and
`TestPruneAccessLogOlderThan` / `TestPruneAccessLogDisabled` /
`TestMaybePruneAccessLogThrottles` for retention + throttling) and `blobstore_test.go`
(content-addressed `Put`/`Get`, dedupe, GC). `internal/artifacts/manifest_test.go` pins
`ManifestHash`: deterministic and order-independent, changes on path/blob content,
ignores size/mode/MIME, and hashes an empty input to sha256(nil).
`internal/server/handler_artifacts_retention_test.go` pins `TestArtifactLogRetention`
(unset/`0`/positive/negative/non-numeric/padded env values).

Mode B:

- `internal/artifacts/hubclient_test.go` pins the client against an `httptest` hub —
  `TestHubClientPublishFlow` (bearer on every request, create body
  `{id,name,token}`, dedupe skips the PUT for an existing blob, the presigned PUT
  carries the bytes, the version payload, and the `PublicURL` shape),
  `TestHubClientSurfacesHubError` (a 502 becomes an error naming the status),
  `TestNewHubPublicToken` (32-byte base64url, two calls differ), and
  `TestHubClientSetArtifactAccess`/`TestHubClientAddArtifactInvite` (with/without
  password; invite success and error).
- `internal/server/artifact_hub_test.go` pins the **server-level** hand-off against a
  local `httptest` hub stub — `TestPublishArtifactToHubReusesTokenOnRepublish` (first
  publish generates and persists `hub_token`; the re-publish reuses the same token and
  returns the same URL), `TestPublishArtifactToHubSurfacesErrorWithoutFallback` (a hub
  502 surfaces as an error carrying the status, and no version is accepted),
  `TestPublishArtifactToHubRequiresSyncToken` (no stored sync token fails with "not
  connected"), `TestHandlePublishHubSuccessReturnsURL` (full `HandlePublish` with
  `hosting_mode=hub` returns 200 and a `hub_url` containing `/p/art_test/`), and
  `TestHandlePublishHubFailureReturns502` (full `HandlePublish` against a failing hub
  returns **502** and no version is accepted). Access: `TestParseArtifactAccess`,
  `TestPublishArtifactToHubAppliesAccessAndCollectsInviteErrors` (access applied before
  publish; an invite failure is collected), `TestHandlePublishSendsAccessFieldsToHub`,
  and `TestHandlePublishRejectsAccessFieldsForLocalArtifact` (the 400).
- `TestStoreHubToken` (`internal/artifacts/store_test.go:188`) pins `hub_token` end to
  end: empty before the first publish, set/get round-trip, idempotent `Migrate` after
  the additive column, and `ErrNotFound` for an unknown id.
- Web: `web/src/api/artifacts.publish.test.ts` pins the request-body mapping (access
  fields never sent as `mode`), and `ArtifactPublishDialog.test.tsx` pins the select,
  password floor and invite parsing.

## Related

- `concepts/backend-sync-url-split.md` — `sync_url` vs `backend_url`.
- `concepts/desktop-share-token.md` — the different whole-desktop share.
- `concepts/auto-share-on-start.md` — tailnet-only boot exposure (shares the mechanism).
- `docs/superpowers/specs/2026-11-07-publish-share-project-design.md` — full design.
