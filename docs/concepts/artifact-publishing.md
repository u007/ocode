---
type: Concept
title: Artifact publishing / sharing (static snapshots, Mode A + Mode B client)
description: 'Phase 1 publish/share: immutable static project snapshots served at /p/{artifact}/{token}/... from a content-addressed blob store, with durable hashed capability tokens, allowlist invites, password gate, uniform 404, a computed blob_manifest_hash, access-log retention pruning, and tailscale serve/funnel exposure that MUST keep the /p upstream path. Also covers the Mode B (hub) CLIENT upload path: hosting_mode=hub publish, per-publish visitor access (public/password/allowlist) applied through the hub, hub_token persistence, presigned per-blob upload, and the 409-not-connected / 502 no-fallback rules; the hub-side API itself lives in the separate kakiit repo and is not in this tree. Also covers the owner API''s computed share_url and the project-level Artifacts top tab (client-side project scoping, reload-on-activation, per-row publish/unpublish/copy/delete behind a confirm guard, and a read-only per-row versions expander).'
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
timestamp: 2026-11-09T07:45:57Z
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
| `internal/artifacts/types.go` | `Artifact` (incl. the computed, never-stored `ShareURL`), `Version`, `File`, `AccessToken`, `AllowEntry`, `AccessLogEntry`. |
| `internal/artifacts/store.go` | SQLite metadata store: schema, migrations, cross-process flock, FK enforcement, CRUD, invites, access log (incl. retention prune), token prune, GC, hub-token accessors (incl. the batched `GetArtifactHubTokens`). |
| `internal/artifacts/blobstore.go` | Content-addressed blob store under `<dir>/blobs/<xx>/<sha256>`. |
| `internal/artifacts/manifest.go` | `ManifestHash(files []File)` — deterministic version manifest hash written to `artifact_versions.blob_manifest_hash`. |
| `internal/artifacts/hubclient.go` | Mode B HTTP client: `HubClient`, `EnsureArtifact`, `UploadBlob` (presigned PUT + dedupe), `PublishVersion`, `UnpublishArtifact`, `DeleteArtifact` (404 rule in `artifactOp`), `PublicURL`, `NewHubPublicToken`. |
| `internal/artifacts/hubclient_access.go` | Mode B visitor access + invites: `SetArtifactAccess`, `AddArtifactInvite`, `HubInvite`. |
| `internal/server/artifact_hub.go` | Mode B orchestration: `publishArtifactToHub` runs EnsureArtifact, persists `hub_token` on a first publish, then SetArtifactAccess → UploadBlob (per file) → PublishVersion → AddArtifactInvite. `undoFreshHubPublish` and `answerLocalCommitFailure` roll back a first publish; `removeArtifactFromHub` serves hub-first unpublish and delete. |
| `internal/server/artifact_tokens.go` | Durable hashed capability tokens, `artifactAuthMiddleware(Impl)`, password cookies, invite cookies, per-visitor rate limiting. Starts no background work; the access-log prune runs on the serve path. |
| `internal/server/handler_artifacts.go` | Owner CRUD, publish (incl. the Mode B hand-off and `parseArtifactAccess`), unpublish, delete, token mint/revoke, allowlist add/remove, access log, `artifactLogRetention()`, and `artifactShareURL()` for the owner-visible hub link. |
| `internal/server/handler_artifact_invite.go` | Invite accept (magic link) + password gate. |
| `internal/server/handler_artifact_serve.go` | Static serve from the blob store (Mode A public path), access-log write + throttled retention prune. |
| `internal/server/tailscale_share.go` | Tailscale `serve`/`funnel` exposure, `newArtifactTailscaleShare`, `authConfigured`. |
| `internal/server/server.go` | Route registration. |

## Data model and storage

Machine-global store, **not** per project: `artifactStoreDir()` is
`<GlobalDataDir>/artifacts` (`internal/server/artifact_tokens.go`). The design
spec's per-project layout was not adopted.

SQLite (`artifacts.db`) opens with WAL, `busy_timeout=5000`, and
**`foreign_keys(ON)`** (`internal/artifacts/store.go`); the FK clauses in the schema
are vacuous without that pragma, so do not remove it. `Migrate` and its tables
(`internal/artifacts/store.go`):

- `artifacts(id PK, project_host, project_path, name, created_at, created_by, status, hosting_mode, hub_token)`
- `artifact_versions(id PK, artifact_id FK, version, entrypoint, blob_manifest_hash, size_bytes, created_at, build_cmd, build_log_path, schema_version)`
- `artifact_files(version_id FK, path, blob_hash, size, mode, mime)`
- `access_tokens(id PK, artifact_id FK, token_hash, kind, expires_at, created_at, revoked_at, password_hash, label)`
- `allowlist(artifact_id FK, email, invited_at, accepted_at, invite_token_hash)`
- `access_log(artifact_id, token_id, ip, ua, path, status, ts)`

`hub_token` (`internal/artifacts/store.go`) is the artifact's **hub public
capability token**, generated client-side on the first hub publish and persisted so a
re-publish keeps the same link; the hub stores only its sha256. `Migrate` is additive:
SQLite has no `ADD COLUMN IF NOT EXISTS`, so it probes
`pragma_table_info('artifacts')` and runs a guarded `ALTER TABLE … ADD COLUMN` for
stores created before hub publishing existed (`internal/artifacts/store.go`).
`SetArtifactHubToken` updates the row and returns `ErrNotFound` when no row
matched; `GetArtifactHubToken` returns `""` for an artifact that has never
been hub-published and `ErrNotFound` for an unknown id. Both run under `withLock`.
`GetArtifactHubTokens` (also under `withLock`) reads the tokens for a list of ids
in one query per chunk of 500 ids (`artifactHubTokenChunk`); the owner list uses it
so it never reads tokens one row at a time.

Every store mutation runs under `withLock` — an in-process mutex plus a **cross-process
file flock** (`artifacts.lock`, 5s timeout), and contention is surfaced as `ErrLocked`
rather than silently running unlocked (`internal/artifacts/store.go`). Keep that
contract: a new write path that skips `withLock` reintroduces last-writer-wins.

Blobs are content-addressed: `Put` hashes with sha256 and stores at
`blobs/<first 2 hex>/<hash>`, deduping an existing file (`internal/artifacts/blobstore.go`).
`validateHash` rejects empty, separator-bearing and `..` hashes, and `pathFor`
re-checks containment. `GCUnreferenced` deletes blobs not named by
`artifact_files`; `Store.GC()` computes the referenced set and is invoked
after `DeleteArtifact` (`internal/server/handler_artifacts.go`).

Versions are immutable. `PublishVersion` inserts the version + its file rows in one
transaction and refuses an artifact that does not exist
(`internal/artifacts/store.go`). The serve path picks the newest version by an
**explicit sort key** (created_at, then version label, then id), never by trusting
`ListVersions` order (`latestArtifactVersion` in `internal/server/handler_artifact_serve.go`;
pinned by `TestLatestArtifactVersionPicksNewestExplicitly`).

**`blob_manifest_hash` is computed at publish time.** `ManifestHash`
(`internal/artifacts/manifest.go`) hashes the sorted `path + "\x00" + blob_hash`
pairs, newline-joined, and the publish handler writes the result
(`internal/server/handler_artifacts.go`). Size, mode and MIME are deliberately
excluded from the input, so a chmod or MIME re-detection alone does not change the
hash, and walk order is irrelevant.

**Access-log retention is wired.** `Store.PruneAccessLogOlderThan(maxAge)` deletes rows
whose `ts` is older than the cutoff and is a no-op for `maxAge <= 0` (keep forever).
`Store.MaybePruneAccessLog(maxAge)` runs it at most once per `accessLogPruneInterval`
(1 hour), recording the **attempt** — success or failure — in the atomic
`lastAccessPrune` field before running, so concurrent callers do not stampede and a
failed prune retries on the next interval. **The serve path is the only live caller**:
`handler_artifact_serve.go` fires it via `crashguard.Go` after a successful serve writes
`LogAccess`. Construction (`newArtifactTokenStore`) deliberately starts no background
work, because an unowned fire-and-forget write outlives the handler and races a caller's
data-dir teardown — that is what broke temp-dir cleanup in tests. Behaviour is unchanged
for a server that actually serves artifacts: the first serve prunes instead of boot.

## Publish pipeline (what `POST /api/artifacts/{id}/publish` actually does)

`HandlePublish` (`internal/server/handler_artifacts.go`):

1. Remote project → the request is proxied to the owning host's
   `/api/artifacts/{id}/publish`; there is **no local fallback** (`proxyArtifactToRemote`).
2. **Server-side secret scan** — the client flag is ignored (`_ = scanHit`); the
   handler walks the project for `.env`/`.env.*`, `*.pem`, `*.key`, `credentials`,
   `secrets.{json,yaml,yml}`. A hit returns 409 with a machine-readable body
   `{"error":"secret scan hit","code":"secret_scan"}` (`writeErrorCode`) unless
   an `override_reason` is supplied, which is logged.
3. `build_cmd` is **parsed and stored, never executed** (and the `buildDir`
   block's "no run" comment). See deferrals.
4. `notify_on_publish` is read per request; see below.
5. Walk the project, skipping: symlinks, any dot-prefixed path segment,
   `node_modules`/`vendor`/`.git`, `.env*`, and any extension outside
   `allowedArtifactExt` (walk, check and helper in `internal/server/handler_artifacts.go`:
   html/htm/css/js/mjs/cjs/json/map, images, fonts, txt/xml/csv/pdf, audio/video, wasm).
   Files that fail the allowlist are silently omitted — a publisher asking "why is my
   file missing" is usually here.
6. Enforce `artifactMaxBytes()` — default 100 MiB, overridable by
   `OCODE_ARTIFACT_MAX_BYTES` (helper in `internal/server/handler_artifacts.go`).
7. **Mode B hand-off, before the local commit.** If `strings.EqualFold(a.HostingMode, "hub")`,
   the handler calls `publishArtifactToHub` and commits no version
   locally until it returns (the walk has already stored the blobs). Success sets `hub_url` to the hub share URL. A machine with no
   sync token is a **409 `hub_not_connected`**, and every other hub failure
   writes **HTTP 502** with body `hub publish: <err>`. In both cases the local version is
   never written, so a failed hub publish leaves no local version. There is deliberately
   **no local fallback**: a publish requested as hub-hosted never silently degrades to a
   self-hosted link that dies with this machine.
8. **Local commit.** Runs only after the hub accepts the version: `PublishVersion`,
   then `SetArtifactStatus("active")`. A new publish therefore **re-livens an
   unpublished artifact**. If either write fails, `answerLocalCommitFailure` answers. A fresh
   first publish is undone on the hub and answered 500 "rolled back". A re-publish cannot be
   undone, so the answer is 500 and asks the owner to re-run publish to resync the local
   store, and logs the failure at ERROR. If the hub undo of a fresh publish also fails, the answer is 500 saying the hub still holds the first version, and that is logged at ERROR too. The undo rules are in "Mode B client upload path".
9. If `notify`, email recipients are resolved **server-side**: the owner if
   `CreatedBy` looks like an email, plus every allowlist entry, deduped.
   The flag is not persisted on the artifact, so "remember last value" is
   never true server-side.
10. **Mail delivery is best-effort and reported.** With no mailer configured
   `sendEmail` returns `errNoMailer`; the publish response carries
   `email_sent`/`notify_sent` reflecting actual delivery, the invite response carries
   `email_sent`, and failures are logged. The invite token is always returned, so a
   failed email degrades to manual link sharing, never to a lost invite.

The JSON response carries `version`, `artifact`, `notify_sent`, `email_sent` and
`hub_url`. `hub_url` is `""` for a Mode A publish and the
`<hub>/p/<id>/<token>/` URL for a successful Mode B publish. The SPA type mirrors this
as an optional field (`web/src/api/artifacts.ts`, `PublishArtifactResponse.hub_url`).

## Visitor access on publish (`access_mode`)

A publish body may carry a visitor access mode. `parseArtifactAccess`
(`internal/server/handler_artifacts.go`) validates it **before** the snapshot walk, so
a bad request costs no blob work:

- `""` (absent) → leave the hub's current setting alone; `public` → no password;
  `password` → 8..200 chars; `allowlist` → 1..50 valid emails (`invite_emails`, a JSON
  array or a comma-separated string via `getStringSlice`); anything else → 400.
  `normalizeInviteEmails` trims, lowercases, requires `@` and no whitespace, and
  dedupes.
- **Mode A never carries an access mode.** `password`/`allowlist` on a non-hub artifact is
  a 400 — local access is capability tokens plus the allowlist, and a request
  that cannot take effect is refused rather than silently ignored.
- **Mode B ordering:** `SetArtifactAccess` runs after `EnsureArtifact` and *before* any
  blob upload or version publish (`internal/server/artifact_hub.go`), so an access
  failure leaves **no live version** behind (the artifact row alone serves nothing).
  Invites run after the hub accepts the version and before the local commit
  (`internal/server/artifact_hub.go`); a per-email failure is collected in
  `InviteErrors` and does not undo the publish.

Hub calls live in `internal/artifacts/hubclient_access.go`: `SetArtifactAccess`
(`PUT /access`) and `AddArtifactInvite` (`POST /invites`). The invite carries the
same public token the client already persists for the share link; the hub stores only its
sha256 and returns `inviteUrl` even with no mail provider. Neither the password nor the
token is logged or put in error text.

The publish response reports `access_mode`, `hub_invites` and `invite_errors`
(`handler_artifacts.go`). The SPA maps the access fields explicitly
(`web/src/api/artifacts.ts`, invites too) — **the existing `mode` field is a
legacy alias for `hosting_mode`, so the access mode must never travel as `mode`**.
The dialog adds the select (`ArtifactPublishDialog.tsx`), the password
field (client-side 8-char check) and the invite textarea (split
on commas/spaces/newlines).

Pinned by `TestParseArtifactAccess`,
`TestPublishArtifactToHubAppliesAccessAndCollectsInviteErrors`, the handler 400 case, and
`TestHubClientSetArtifactAccess`/`TestHubClientAddArtifactInvite` plus the dialog tests.

## Publish entry point in the SPA (`/publish`)

The SPA mounts the publish dialog. `handleCommand` (`web/src/App.tsx`) intercepts
`/publish [hub|local]` before the shared `dispatchCommand`, resolves the active
project's path/host, calls `ensureArtifact` (`web/src/api/artifacts.ts`) and stores
`{artifactId, projectPath, host}` in `publishTarget`. `ensureArtifact` lists the project's
artifacts (limit 100) and **reuses** the one whose `project_path` matches (preferring a
matching `project_host`) **and whose `hosting_mode` matches the requested mode** — hub
takes `hub`, local takes anything that is not `hub`, because the mode is fixed on the row
and one project can hold both; it creates one with `hosting_mode: mode` only when none
exists, and throws on an empty path.

`App.tsx` mounts `<ArtifactPublishDialog>` beside `<ShareDialog>`, wiring `onPublish`
→ `publishArtifact(id, payload, host, projectPath)`. The default hosting mode is **`hub`**;
`/publish local` is the escape hatch, because a `hub` publish from a server with no sync
login is refused (409 `hub_not_connected`) and never falls back (see "Mode B client
upload path"). On success the dialog stays open on a **result step** — share link with a
copy button, invite links, and `invite_errors` — instead of closing and discarding
`hub_url`; Done closes.

Server-side, "not logged in" is now a **409 `hub_not_connected`**, not a 502 after the
whole snapshot walk. `/publish` pre-checks it for a hub publish on a local project
(`hostingMode === "hub" && !targetHost` — a remote project's token lives on its own
server, so the local check is skipped): when `api.getSyncStatus()` reports
`loggedIn === false` it reports the message and returns **without creating an artifact
or opening the dialog** (`App.tsx`); a failed status probe is advisory
(`console.warn`) and does not block. `hubClient` wraps `errHubNotConnected`
(`artifact_hub.go`) and `HandlePublish` maps that sentinel with
`writeErrorCode(…, http.StatusConflict, "hub_not_connected", …)`
(`handler_artifacts.go`); every other hub failure stays 502. `writeErrorCode`
(`server.go`) adds a machine-readable `code`, so a client branches on the code
rather than on text. `/publish local` now uses **its own artifact row**: `ensureArtifact`
(`artifacts.ts`) filters by hosting mode — hub requires `hosting_mode === "hub"`,
local accepts anything else (existing rows may say `"self"`) — since the mode is fixed
on the row and one project can hold both. Before it, a local publish after a failed hub
publish reused the hub artifact and hit the same failure again.

A `409` from publish is not one behaviour: the **secret-scan refusal is the only
overridable one**, and the dialog — not the `/publish` command — owns the override. The
`catch` keys on the server's machine-readable code,
`status === 409 && (code === "secret_scan" || /secret scan/i.test(message))`
(`ArtifactPublishDialog.tsx` — it reads `e.body.code` and decides),
mirroring `writeErrorCode(…, "secret_scan", …)` in `handler_artifacts.go`. The
message test is only the fallback for an older server, so a wording change can no longer
close the override. It then renders a required override-reason textarea,
relabels the button **Publish anyway**, and refuses an empty reason client-side before
sending. A non-empty retry adds `override_reason` to the payload, which
`artifacts.ts` already maps
(`override_reason`/`overrideReason`/`reason`), so no API change was needed. **A
`hub_not_connected` 409 (its own `"code":"hub_not_connected"`) is deliberately NOT
overridable** — it stays a plain error and never opens the field, so a secret-scan
override cannot double as a way past a missing hub login.

`/publish` is registered in the command picker (`web/src/components/Chat/commands.ts`)
and marked **instant** (`web/src/lib/instantCommands.ts`), so it runs during a live
turn: it writes nothing to the transcript, the same rationale as `/btw`.

## Artifacts tab in the SPA (published-snapshot list)

A project-level **Artifacts** top tab (`web/src/components/Layout/TopTabs.tsx`,
after DB so DB stays beside Assets) renders `<ArtifactList>` (`App.tsx`); `"artifacts"` is part of
`ActiveView` (`web/src/lib/viewPersistence.ts`), so the selected tab survives a reload.

- **`share_url` is computed, never stored.** `Artifact.ShareURL`
  (`internal/artifacts/types.go`) is filled by `artifactShareURL`
  (`internal/server/handler_artifacts.go`), which returns `<sync_url>/p/<id>/<token>/`
  only for a hub artifact with a persisted `hub_token` and a resolvable sync base URL, and
  `""` otherwise (a local artifact's access is per capability token, so it has no
  canonical link). `HandleListArtifacts`/`HandleGetArtifact` fill it;
  both routes sit behind `authMiddleware`, and the URL carries the same public token the
  owner already distributes. **Copy link** is disabled when it is empty
  (`ArtifactList.tsx`); the old "Copy invite" that synthesized `/p/{id}` with no token
  is gone.
- **Scoping is client-side.** The owner list endpoint is global (`ListArtifacts(limit,
  offset)` is not project-filtered), so the panel keeps only rows whose `project_path`
  equals the active project's path (`ArtifactList.tsx`).
- **Four per-row lifecycle actions.** Each handler prop is optional: Publish
  (`ArtifactList.tsx`, only when `onPublish` is given — `App.tsx` gives it), Unpublish
  (hidden when `status === "unpublished"`), Copy link (disabled without a
  `share_url`) and Delete (red via `text-destructive`). Delete is destructive and
  removes the artifact's files; Unpublish only stops it serving and is reversible by
  publishing the same row again.
- **Version history per row, display-only.** A **Versions** toggle (`ArtifactList.tsx`)
  expands one row into its versions — label, `formatBytes` size (formats `B`/`KB`/`MB`,
  empty when absent) and relative age — inside `data-testid="version-list"`. The
  first expand calls `getArtifactDetail` (`web/src/api/artifacts.ts`) through a dynamic
  `import("../../api/artifacts")` so tests can mock it, caches the result per artifact id,
  and a collapse/re-expand does not refetch (pinned by `expands a row to show its versions
  and caches the fetch`). A failed fetch renders its error inline, an artifact with no
  versions shows "No versions yet.", and there is **no rollback endpoint**, so nothing
  here mutates state.
- **`getArtifact` unwraps the detail envelope.** `GET /api/artifacts/{id}` returns
  `{artifact, versions}`; `getArtifact` (`web/src/api/artifacts.ts`) now returns
  `res?.artifact ?? res` instead of casting the whole envelope as `Artifact`, which left
  its callers reading `undefined` fields.
- **Confirm-guarded, and a failure keeps the dialog open.** `App.tsx` holds
  `artifactAction {kind, artifact}` and `artifactRefreshKey`;
  `<ConfirmDialog>` guards both actions, and its `onConfirm` is
  `runArtifactAction` → `unpublishArtifact(id, host, projectPath)`
  (`web/src/api/artifacts.ts`; for a hub artifact the hub is taken offline first, then the tokens are revoked, so the artifact stops serving) or
  `deleteArtifact(...)` (in `web/src/api/artifacts.ts`; removes the artifact and the store GCs unreferenced
  blobs). A rejected promise renders the error inline and keeps the dialog open
  (`web/src/components/common/ConfirmDialog.tsx`), so a failed write is never reported as
  done; only success clears the target and bumps the refresh key.
- **Reload on activation and after an action.** The tab is `forceMount`ed from boot, so a
  mount-only load showed stale data (a publish from the chat tab did not appear); the
  panel takes `active` and reloads when it becomes true, and its load effect depends on
  `[active, refreshKey, host, resolvedProjectPath]` (`ArtifactList.tsx`), so the owner never mutates the list
  in place (pinned by `loads only when the tab becomes active` and
  `reloads when refreshKey changes`).

Verified live: the tab renders, a publish made from the chat tab appears on activation,
publish → Unpublish flips the row to `unpublished` and hides the button → Delete (after
the confirm) returns the empty "No artifacts yet" state, and Copy link is disabled for a
local artifact. Two publishes of the same artifact showed two rows in the Versions
expander, each with its own size and age.

## Mode B client upload path (`hosting_mode=hub`)

`publishArtifactToHub` (`internal/server/artifact_hub.go`) runs **before** the local
version is committed, and only for a hub-hosted artifact. It is the only publish path that
talks to the hub. Unpublish and delete also call the hub first (see "Hub-first unpublish and
delete" below):

1. **Bearer.** `ocodesync.LoadToken()` (`internal/sync/sync.go`); a missing file is
   not an error — it means "not logged in", surfaced as `errHubNotConnected`
   (`artifact_hub.go`), wrapped by `hubClient` (`artifact_hub.go`) with `run the sync login flow first`. `HandlePublish` maps that sentinel to **409 `hub_not_connected`**; every
   other hub failure is a 502. A hub publish therefore requires an active ocode sync
   login.
2. **Base URL.** `h.syncClient().BaseURL` — the **resolved `sync_url`** (per-machine
   config `OCODE.SyncURL`, then `OCODE_SYNC_URL`, then the production default;
   `internal/sync/client.go`). Never `backend_url`, and never
   `OCODE_ARTIFACT_HUB_URL` (still unread — see the env-var table).
3. **Public token, generated once.** `st.GetArtifactHubToken(a.ID)` is read first; a
   non-`ErrNotFound` error aborts the publish. An empty value means "never
   hub-published", so a fresh `artifacts.NewHubPublicToken()` (32 random bytes,
   base64url without padding, `internal/artifacts/hubclient.go`) is generated and
   persisted via `st.SetArtifactHubToken`, but only after step 4 succeeds, so a hub that
   refuses the artifact leaves no local token. A re-publish reuses the stored token, so the
   share link is stable; the hub keeps only its sha256.
4. **`EnsureArtifact`** — `POST /api/ocode/artifacts` with `{id, name, token}`
   (`hubclient.go`). The id is ocode's own artifact id, which makes the call
   idempotent: the hub returns the existing artifact when the client-supplied id
   already belongs to this user. The public token is sent here, on creation only; a first
   publish stores it locally only once this call succeeds.
5. **`UploadBlob` per file** (`hubclient.go`) — POST `/:id/blobs` with `{hash, size}`,
   which answers `{exists, uploadUrl}`. When `exists` is true the bytes are **not**
   sent (content-addressed dedupe across every artifact on the hub); otherwise the
   blob is PUT directly to the presigned `uploadUrl` with
   `Content-Type: application/octet-stream`. Bytes come from `st.GetBlob(f.BlobHash)`,
   so the hub receives exactly the content-addressed bytes the local store holds.
6. **`PublishVersion`** (`hubclient.go`) — POST `/:id/versions` with `entrypoint`,
   `buildCmd` and the file list (`path`,`blobHash`,`size`,`mode`,`mime`), finalising an
   immutable version from blobs already uploaded.
7. **`PublicURL`** (`hubclient.go`) returns `<base>/p/<id>/<token>/` — the hub serves
   `/p` on its own origin, so a Mode B link is **not** on this machine.

**Undo on failure.** A first publish (no stored public token) is undone on the hub when any
later step fails, including the local commit. `undoFreshHubPublish`
(`internal/server/artifact_hub.go`) deletes the hub artifact and then clears the local
token. It is best-effort, runs on a 30-second timeout detached from the request context
(`hubUndoTimeout`, `artifact_hub.go`), and clears the local token only when the hub
delete succeeded. A failed hub delete keeps the token, so unpublish and delete can still
reach the hub copy. A re-publish is never undone: the previous version keeps serving. If
the local commit fails after a hub re-publish, the answer asks the owner to re-run publish,
because the hub has no route to delete one version (`answerLocalCommitFailure`,
`artifact_hub.go`).

**Hub-first unpublish and delete.** For a hub-hosted artifact, `HandleUnpublish`
(`internal/server/handler_artifacts.go`) and `HandleDeleteArtifact` call the hub
before they change the local row, through `removeArtifactFromHub` (`artifact_hub.go`).
The hub calls are `POST /api/ocode/artifacts/:id/unpublish` (`HubClient.UnpublishArtifact`)
and `DELETE /api/ocode/artifacts/:id` (`HubClient.DeleteArtifact`), each acknowledged with
`{"ok": true}`. A hub failure answers 502, or 409 `hub_not_connected` without a sync login,
and leaves the local status and rows as they were. An artifact with no stored hub token has
no hub copy this machine knows about, so the hub is skipped and no login is needed. A hub
404 counts as already gone only when its body is JSON with `code: "not_found"`; any other
404, such as one from an older hub without the route, is an error. See
`gotchas/artifact-hub-404-needs-not-found-code.md`.

**Error handling and secret hygiene.** Every non-2xx JSON-API response goes through
`statusError` (`hubclient.go`), which reads the body through `readHubErrorBody`
(`hubclient.go`). That reader caps the body with `hubErrorBodyLimit`
(`hubclient.go`, 4096 bytes), so the error is actionable without dumping an arbitrary
response. The presigned S3 PUT reports its status only. The sync bearer exists solely in the
`Authorization: Bearer …` header (`hubclient.go`, set in `do`): `HubClient` has no logger
and never serialises `Token`, and no artifact code path logs it. Keep it that way.

**Hub contract this consumes** (implemented in the separate `kakiit` repo, not in this
tree): `POST /api/ocode/artifacts` accepts `{id,name,slug?,token?}` and is idempotent
for the same user; the blob and version endpoints are `POST
/api/ocode/artifacts/:id/blobs` and the matching `/versions`; the unpublish and delete routes are `POST /api/ocode/artifacts/:id/unpublish` and `DELETE /api/ocode/artifacts/:id`, each acknowledged with `{"ok": true}`, and a missing artifact answers 404 with a JSON body whose `code` is `not_found`; and the public route is
`GET /p/:id/:token/*` with a uniform 404. Treat that as an external contract: a change
breaks the client silently, because nothing in this repo exercises a live hub.

## Serving path and access enforcement (`/p/...`)

Routes (`internal/server/server.go`): owner routes under `authMiddleware`;
visitor routes under `artifactAuthMiddleware`:

- `GET /p/{id}/{token}/...` → `handleServeArtifact`
- `GET /p/{id}/invite/{inviteToken}` → `HandleInviteAccept`
- `POST /p/{id}/gate` → `HandlePasswordGate`

`artifactAuthMiddlewareImpl` (`internal/server/artifact_tokens.go`) enforces, in order:

1. **Rate limiter first**, before any token or Argon2 work — a blocked client is
   429'd without touching the token store. `artifactRL` is a limiter
   **separate** from the owner API's `s.rl` (`internal/server/handler.go`), shared
   by the gate handler and the middleware.
2. **Uniform 404.** Missing artifact, missing token, bad hash, wrong artifact, revoked,
   expired, failed password and failed invite are all `http.NotFound` — the public path
   never distinguishes 401/403/404 and so leaks no existence oracle
   (`internal/server/artifact_tokens.go`, `internal/server/handler_artifact_serve.go`).
   Tests assert 404 explicitly: `TestArtifactEmailGateFailsClosed`, `TestArtifactUnpublishedRefused`
   (`internal/server/artifact_security_test.go`).
3. `invite`/`gate` sub-routes get `accessLevel 0` and bypass token parsing.
4. Token validate: sha256 the presented value, look it up, then a
   **constant-time compare** (`subtle.ConstantTimeCompare`), artifact binding,
   revocation, and expiry. An expiry in the past fails
   closed; a zero `expires_at` means never expires.
5. **Password gate**: the derived cookie is checked first (cheap), then a presented
   `?password=` / `X-Artifact-Password` is verified under a 4-slot Argon2
   semaphore; a busy semaphore is 429 `Retry-After: 1`, and the attempt is counted
   against the key *before* hashing.
6. **Allowlist gate, fail-closed**: if the artifact has *any* allowlist row, a
   server-verified invite cookie for **this** artifact is required — a self-asserted
   email never passes, and a lookup error denies. This applies even to a
   token of kind `public`: the allowlist is independent of token kind.

Serving itself (`internal/server/handler_artifact_serve.go`):

- Only a live (`status == "active"`) artifact is served; unpublish revokes
  every token *and* sets status, so old links die immediately.
- Path handling rejects any whole `..` segment **before** `path.Clean` (checking after
  Clean is vacuous; a name like `a..b.js` is allowed), then resolves the
  request to an `artifact_files` row, falling back to `Version.Entrypoint` when the
  request is `/` or `index.html`. No directory listing, no live-tree reads.
- **Blob-only**: bytes come from `st.GetBlob(target.BlobHash)`; the project
  directory is never touched during serve.
- Response headers are set per file: `Content-Type` from the declared MIME,
  `X-Content-Type-Options: nosniff`, and
  `Content-Security-Policy: sandbox allow-scripts; default-src 'self'; script-src 'self'; frame-ancestors 'none'`.
  The `sandbox` directive gives published pages an **opaque origin**, so
  even on the same origin as the owner they cannot read owner storage or call the
  same-origin authenticated API. Do not drop `sandbox` from that header.
- Successful serves are written to `access_log`; the visitor key comes from
  `artifactClientKey`, which trusts `X-Forwarded-For` **only** from a loopback peer
  (tailscale serve/funnel arrives from loopback) and takes the right-most hop
  (`internal/server/artifact_tokens.go`).

## Tokens, passwords and invites (crypto invariants)

- Token value = **32 random bytes, base64url, no padding** (`newArtifactTokenValue`,
  `internal/server/artifact_tokens.go`), 256 bits.
- **Only the sha256 hex is stored** (`issue`); there is no plaintext token
  column. A lost token cannot be recovered, only rotated (mint a new one).
- Passwords use **Argon2id** (3 iterations, 64 MiB, 4 lanes, 32-byte output) with a
  random 16-byte salt, encoded `argon2id$salt$hash` (`hashPassword`), and
  verified with `subtle.ConstantTimeCompare`. Verifications are bounded to 4
  concurrent (`artifactArgon2Concurrency`) because each allocates 64 MiB.
- Password cookie = `tokenID + "." + HMAC-SHA256(key=PasswordHash, msg="artifactID|tokenID")`
  (`passwordCookieValue`). The raw password is never stored client-side, and
  changing the password changes the key, so old cookies stop verifying
  (`TestPasswordCookieDiesWithPasswordChange`).
- Invite cookie: acceptance stores the raw invite token client-side
  (`artifact_access_<id>`, HttpOnly/Secure/SameSite=Lax, path-scoped to
  `/p/<id>/`, `internal/server/handler_artifact_invite.go`) while the DB keeps
  only its sha256; the serve path re-hashes the cookie and looks up an **accepted** row
  (`inviteAccessOK`, `artifact_tokens.go`). Accepting an artifact invite does
  **not** clear `invite_token_hash` (only `accepted_at` is set,
  `internal/artifacts/store.go`), which is why the cookie keeps working;
  deleting the allowlist row removes it (`DeleteAllowEntry`,
  `internal/artifacts/store.go`) and the lookup then fails closed
  (`GetAcceptedAllowEntryByInviteHash`).
- **Invite TTL defaults to 7 days** (`const InviteTTL = 7 * 24 * time.Hour`,
  `internal/artifacts/store.go`) and `OCODE_ARTIFACT_INVITE_TTL_DAYS` overrides it
  through `inviteTTL()` (`store.go`); a missing, non-numeric or non-positive
  value falls back to the default, so a bad env var can never make invites permanent.
  Enforced on accept and on lookup.
- **No secret is ever logged.** `store.go` contains exactly one `log.Printf` — the
  access-log prune failure in `internal/artifacts/store.go` — and it carries only the
  error. The pre-existing artifact log lines record a secret-scan
  override reason (`handler_artifacts.go`) and mail-delivery failures
  (`handler_artifacts.go` publish/invite branches); none includes a token, password,
  invite hash, password cookie or the sync bearer. Keep it that way: a new log line
  must not print any of those values.
- `PruneExpiredTokens` deletes rows whose non-empty `expires_at` is past
  (`internal/artifacts/store.go`); `artifactTokenStore.pruneExpired` is the
  caller (`internal/server/artifact_tokens.go`).

## Mode A vs Mode B

- **Mode A (default, implemented): self-hosted.** Local ocode serves `/p/...`;
  optional tailscale `serve` (tailnet-only, default) or `funnel` (public, explicit
  confirmation). Visitor access there is capability tokens plus the allowlist; a
  publish-time `password`/`allowlist` is refused (see "Visitor access on publish").
- **Mode B (client implemented; hub-side API out of tree): hub-hosted** at the
  `sync_url` hub so artifacts stay up when the machine sleeps. A create request may
  pass `hosting_mode` (`handler_artifacts.go`, default `"self"`); the value is
  persisted and **is consulted at publish time** — only a case-insensitive `"hub"`
  takes the Mode B path (`handler_artifacts.go`), everything else is Mode A. The
  upload itself is described above; `hosting_mode=hub` with no sync login fails the
  publish with 409 `hub_not_connected` rather than falling back to Mode A. Use **`sync_url`, never
  `backend_url`** (`backend_url` is local-dev-only;
  `concepts/backend-sync-url-split.md`).
- The remote path in `handler_artifacts.go` proxies artifact requests to a **saved
  remote ocode host** (by `project_host`), not to the hub. Unreachable → 502
  (`stage "remote-connect"`), still connecting → 503 (`stage "remote-connecting"`);
  never a local fallback (`proxyArtifactToRemote`).

## DANGER: the tailscale upstream path must stay `/p`

Every artifact exposure instance **MUST** be built by `newArtifactTailscaleShare()`
(`internal/server/tailscale_share.go`). It is the only place that sets
`upstreamPath = "/p"`, and `upstreamTarget` appends it so tailscale proxies to
`localhost:<port>/p`.

Why: `tailscale serve --set-path /p` **strips** the mount prefix before forwarding. A bare
`&tailscaleShare{mount: artifactTailscalePath}` (no `upstreamPath`) therefore forwards a
public `/p/api/...` request to the server **root** `/api/...`, i.e. to the owner's
authenticated API. The `upstreamPath` is what keeps the public mount confined to `/p/*`
artifact routes.

There are **four** alias fields on `Server` — `artifactShare`,
`artifactTailscaleShare`, `tsArtifactShare`, `artifactsShare` — and all four must be
constructed via the helper (`internal/server/server.go`). The upstream trap is
pinned by `TestArtifactShareUpstreamKeepsPathPrefix` (iterates all four),
`TestArtifactShareFunnelTargetKeepsPathPrefix`, and `TestUpstreamTargetTable`
(`internal/server/tailscale_artifact_upstream_test.go`). The desktop share is
deliberately different: empty `upstreamPath`, bare root target
(`TestDesktopShareTargetUnchanged`).

Also: the mount is a **single global `/p/*`**, so a second expose reuses the live mount
instead of re-mounting (re-mounting would orphan the first and its routes) —
`TestArtifactExposureMountsOnce`, and it must never clobber `/desktop`
(`TestArtifactExposureDoesNotClobberDesktopMount`).

## Funnel (public) requires auth

`handleStartArtifactTailscaleShare` (`internal/server/tailscale_share.go`)
defaults to tailnet-only `serve`. A public request (`?funnel=1` / `?public=1`) becomes
`kind "full"` and is **refused with 403** unless `s.authConfigured()` is true:

```go
func (s *Server) authConfigured() bool {
	return s.remoteMode || s.username != "" || s.password != ""
}
```

Use **that one predicate** for the funnel gate and for
`authMiddleware`/`mediaAuthMiddleware`; never re-derive `remoteMode || username || password`
inline, or the two can disagree and a public mount lands on an unauthenticated server.
Pinned by `TestArtifactShareFunnelRefusedWithoutAuth` and
`TestArtifactShareFunnelAllowedWithAuth` (`tailscale_artifact_upstream_test.go`).
An unattended expose must never funnel (`TestArtifactExposureFunnelOnlyOnExplicitConfirmation`).
All exposure routes are owner-only (`authMiddleware`), so an unauthenticated caller
cannot start one (`TestArtifactTailscaleRoutesRequireAuth`).

## Environment variables (verified status)

`.env.example` declares five `OCODE_ARTIFACT_*` vars, and **three are wired**:

| Var | Status |
|---|---|
| `OCODE_ARTIFACT_MAX_BYTES` | Wired. Default 100 MiB (`handler_artifacts.go`). |
| `OCODE_ARTIFACT_INVITE_TTL_DAYS` | Wired. Default 7 days; invalid or `<=0` falls back (`store.go`). |
| `OCODE_ARTIFACT_LOG_RETENTION_DAYS` | Wired. Read by `artifactLogRetention()` (`handler_artifacts.go`) and applied by the store's throttled prune. Unset/invalid/negative → 30 days; `0` → keep forever. |
| `OCODE_ARTIFACT_ORIGIN` | **Declared only.** No artifact-origin plumbing. |
| `OCODE_ARTIFACT_HUB_URL` | **Declared only.** The hub client ignores it: the base URL is the resolved `sync_url` (`h.syncClient().BaseURL`), and `.env.example` marks it reserved ("hub base URL override; NOT read yet — Mode B uses sync_url"). |

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
  the request context (`artifact_tokens.go`) but **never read by the serve
  path** (`AccessLevelFromContext` has no non-test consumer). Model "private" as "no
  token shared", "public" as "token, no allowlist". On Mode B the visitor mode is a
  hub-side setting applied per publish (`access_mode`), not a local column.
- **Per-artifact subdomain / origin isolation.** Mode A is same-origin `/p/...`; the
  opaque-origin protection is the CSP `sandbox` directive, not a separate host. A Mode B
  link is on the hub's own origin, which is the hub's concern, not configured here.

## Invariants to preserve (checklist before touching the feature)

1. Build every artifact `tailscaleShare` with `newArtifactTailscaleShare()`; never a
   bare struct literal. (`server.go`, `tailscale_share.go`)
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
   `backend_url` and never `OCODE_ARTIFACT_HUB_URL`; a hub failure is a 409
   `hub_not_connected` (no sync login) or a 502 (any other hub error), with
   **no local fallback**. Unpublish and delete reach the hub first and leave the local row
   unchanged when the hub fails.
9. Build-command execution stays deferred and, when added, goes through the permission +
   sandbox-gated shell path.
10. New store writes hold `withLock`; keep `foreign_keys(ON)` and the FK clauses.
11. `ManifestHash` covers `path + blob_hash` only — never size/mode/MIME — and is
    order-independent (sorted input). A metadata-only change must not invalidate the
    manifest hash.
12. The hub public token is generated **once** and persisted in `artifacts.hub_token`;
    regenerating it moves every published link. The sync bearer goes only in the
    `Authorization` header and is never logged.
13. The hub upload runs before the local version is committed, so a hub failure leaves no
    local version. A fresh first publish is undone on the hub when a later step fails,
    including the local commit (`undoFreshHubPublish`). A re-publish is never undone; a
    local commit failure after a hub re-publish leaves the hub on the new version until
    publish is re-run.
14. `SetArtifactAccess` runs after `EnsureArtifact` and before any version publish, and
    invites run after the hub accepts the version and before the local commit; a per-email
    invite failure never undoes the
    publish. Mode A access stays capability tokens + allowlist — never silently accept a
    mode it cannot apply.
15. `Artifact.ShareURL`/`share_url` is **computed per request and never stored**; it is
    `""` for a local artifact. `HandleListArtifacts`/`HandleGetArtifact` fill it, both
    routes stay owner-authenticated, and the SPA must never synthesize a `/p/{id}` link
    without the capability token.

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
  password; invite success and error), and the unpublish and delete routes with the 404
  rule (`TestHubClientUnpublishAndDeleteArtifact`, `TestHubClientArtifactOpNotFoundWrapsErrNotFound`,
  `TestHubClientArtifactOpPlainNotFoundIsRouteMissing`).
- `internal/server/artifact_hub_test.go` pins the **server-level** hand-off against a
  local `httptest` hub stub — `TestPublishArtifactToHubReusesTokenOnRepublish` (first
  publish generates and persists `hub_token`; the re-publish reuses the same token and
  returns the same URL), `TestPublishArtifactToHubSurfacesErrorWithoutFallback` (a hub
  502 surfaces as an error carrying the status, and no version is accepted),
  `TestPublishArtifactToHubRequiresSyncToken` (no stored sync token fails with "not
  connected"), `TestHandlePublishHubNotConnectedReturns409` (full `HandlePublish` with
  no sync token returns 409, `"code":"hub_not_connected"` and `/publish local` in the
  message), `TestHandlePublishHubSuccessReturnsURL` (full `HandlePublish` with
  `hosting_mode=hub` returns 200 and a `hub_url` containing `/p/art_test/`), and
  `TestHandlePublishHubFailureReturns502` (full `HandlePublish` against a failing hub
  returns **502** and no version is accepted), plus
  `TestHandlePublishSecretScanRefusalAndOverride` (the secret scan refuses with 409 +
  `"code":"secret_scan"`, then the same publish with `override_reason` returns 200 and
  `"hub_url":""`). Access: `TestParseArtifactAccess`,
  `TestPublishArtifactToHubAppliesAccessAndCollectsInviteErrors` (access applied before
  publish; an invite failure is collected), `TestHandlePublishSendsAccessFieldsToHub`,
  and `TestHandlePublishRejectsAccessFieldsForLocalArtifact` (the 400).
- Hub-first ordering, rollback and removal (`internal/server/artifact_hub_test.go`):
  `TestPublishArtifactToHubFirstPublishUndoneOnLaterFailure`,
  `TestPublishArtifactToHubRepublishFailureKeepsHubAndToken`,
  `TestPublishArtifactToHubFailedUndoKeepsToken`, `TestHandlePublishHubFailureLeavesNoLocalVersion`,
  `TestHandlePublishHubNotConnectedLeavesNoLocalVersion`,
  `TestHandlePublishLocalCommitFailureUndoesFirstHubPublish`,
  `TestHandlePublishLocalCommitFailureOnRepublishKeepsHubCopy`,
  `TestHandleUnpublishHubArtifactReachesHubFirst`, `TestHandleUnpublishHubFailureKeepsLocalState`,
  `TestHandleDeleteHubArtifactReachesHubFirst`, `TestHandleDeleteHubArtifactHub404CountsAsGone`,
  `TestHandleDeleteHubFailureKeepsLocalRow`, `TestHandleHubRemovalNotConnectedReturns409`,
  `TestHandleHubRemovalSkipsHubWithoutPublicToken`, `TestHandleLocalArtifactNeverReachesHub`, and
  `TestHandleHubPlain404FromMissingRouteKeepsLocalState`.
- `TestHandleListArtifactsIncludesShareURL` (`internal/server/artifact_hub_test.go`)
  pins `share_url` end to end on the owner list: a hub artifact with a persisted token
  gets `https://hub.test/p/art_hub/pub-token/`, while a local artifact's is empty (it has
  no canonical link).
- `TestStoreHubToken` (`internal/artifacts/store_test.go`) pins `hub_token` end to
  end: empty before the first publish, set/get round-trip, idempotent `Migrate` after
  the additive column, and `ErrNotFound` for an unknown id. `TestStoreGetArtifactHubTokensBatch`
  pins the batched read.
- Web: `web/src/api/artifacts.publish.test.ts` pins the request-body mapping (access
  fields never sent as `mode`) and `ensureArtifact`'s mode filter (a local publish does
  not reuse a hub artifact; a hub publish reuses the hub one), and
  `ArtifactPublishDialog.test.tsx` pins the select, password floor and invite parsing.
  `ArtifactList.test.tsx` pins project scoping (another project's row is hidden),
  copying the server `share_url` instead of a synthesized path, Copy link disabled
  without one, per-row Publish only when a handler is given, load-on-activate, the
  unpublish/delete row handlers, Unpublish hidden for an already-unpublished artifact,
  the `refreshKey` reload, and the version expander (one cached `getArtifactDetail`
  fetch per artifact, inline error when it fails).

## Related

- `concepts/backend-sync-url-split.md` — `sync_url` vs `backend_url`.
- `concepts/desktop-share-token.md` — the different whole-desktop share.
- `concepts/auto-share-on-start.md` — tailnet-only boot exposure (shares the mechanism).
- `docs/superpowers/specs/2026-11-07-publish-share-project-design.md` — full design.