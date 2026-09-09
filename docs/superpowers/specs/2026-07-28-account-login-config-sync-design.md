# Account Login + Encrypted Config Sync (via kakiit) — Design

- **Date:** 2026-07-28
- **Status:** Implemented (ocode client-side in commit `1b4e252`; kakiit backend pending separate deploy)
- **Goal:** Let ocode users create an account and log in (`/login`), then have
  their local config (`ocodeconfig.json` and `auth.json`) sync across
  machines through a new API surface on `~/www/kakiit`, encrypted at rest,
  with a background deferred sync process and server-side concurrency
  control. This also lays groundwork for a future server-side LLM
  gateway/proxy that can reuse the same decrypted provider credentials.

## Context

ocode currently has no user-account concept — `internal/auth` handles only
LLM *provider* auth (Anthropic/OpenAI/etc. OAuth), and local config lives in
two plaintext files: `ocodeconfig.json` (prefs, permissions, pinned skills —
via targeted `Save*` functions in `internal/config/ocodeconfig.go`, see
memory note on avoiding whole-snapshot saves) and `auth.json` (provider
credentials, `internal/auth/store.go`, written at `0600`, no encryption
today beyond file permissions).

`kakiit` already has a full account system: better-auth
(`src/server/better-auth.ts`) with email+password, a `username` plugin, and
Postgres via Drizzle. Reusing this instead of building a separate auth
service avoids duplicating account infrastructure. kakiit runs as a single
PM2 process per environment (`instances: 1`), which is relevant to the
concurrency design below.

Key decisions made during brainstorming:
- Sync covers **everything**, including secrets — a server-decryptable
  model was chosen over end-to-end encryption specifically because a future
  gateway/proxy needs the server to be able to use live provider keys.
- Two blobs sync per user, mirroring the existing local file split
  (`ocodeconfig` / `authsecrets`) rather than merging them into one payload.
- Login uses a browser device-code flow (like `gh auth login`), not a
  terminal password prompt.
- Conflicts are resolved automatically via a 3-way JSON merge — sync must
  never surface a failure to the user for a routine conflicting edit.
- Server mutex is a Postgres advisory lock (not in-process), chosen even
  though kakiit is single-instance today, so it survives a future move to
  multiple instances/pods without rework.

## 1. Architecture overview

```
ocode CLI (Go)                          kakiit (Next.js, single PM2 instance)
─────────────────                       ───────────────────────────────────
internal/sync/                          /api/ocode/device/start
  - device login                        /api/ocode/device/token
  - debounced push                      /api/ocode/sync/:blobType  (GET/PUT)
  - pull + 3-way merge
  - local snapshot tracking             better-auth (existing) — session
                                         Postgres: ocode_sync_keys,
                                                   ocode_config_syncs
```

ocode gets a new `internal/sync` package. kakiit gets a new `/api/ocode/*`
route group sitting alongside its existing API, reusing better-auth for the
underlying user/session and two new tables.

## 2. Login flow (device-code)

1. `ocode /login` calls `POST /api/ocode/device/start` → kakiit returns
   `{device_code, user_code, verify_url, expires_in}` and stores a pending
   device record.
2. CLI prints `user_code` and opens `verify_url` in the browser (prints the
   URL as fallback if it can't launch one).
3. User logs into kakiit (or is already logged in via existing cookie
   session) and approves the device on a `/device` confirmation page —
   reuses better-auth's session, no new credential entry.
4. CLI polls `POST /api/ocode/device/token` with `device_code` until
   approved, then receives a long-lived **sync bearer token** (distinct
   from kakiit's browser cookie session; revocable, scoped to the
   `/api/ocode/sync/*` routes only).
5. CLI stores the token locally at `0600`, alongside `auth.json`.

## 3. Encryption (server-decryptable envelope model)

- kakiit holds one master key from env (`KAKIIT_OCODE_SYNC_MASTER_KEY`).
- On first sync for a user, kakiit generates a random per-user AES-256 DEK,
  wraps it with the master key (AES-GCM), and stores the wrapped DEK.
- Each blob (`ocodeconfig`, `authsecrets`) is encrypted with that user's DEK
  (AES-256-GCM) before being persisted; decrypted on any authenticated read.
  The same unwrap path is what a future gateway/proxy would reuse to get
  live provider keys server-side.
- Transport is HTTPS + bearer token. The CLI sends plaintext JSON over TLS;
  it does not manage any encryption itself — this mirrors how `auth.json`
  is unencrypted locally today and relies on transport/at-rest protection
  rather than a new client-side crypto layer.

## 4. Data model (kakiit — new tables)

- `ocode_sync_keys`: `user_id` (FK), `wrapped_dek`, `created_at`
- `ocode_config_syncs`: `user_id` (FK), `blob_type` (`ocode_blob_type_enum`:
  `ocodeconfig` | `authsecrets`), `ciphertext`, `version` (int), `updated_at`
  — unique index `ocode_config_syncs_user_id_blob_type_uniq`

Generated via `drizzle-kit generate` per project convention, with existence
guards added before commit.

## 5. Push flow — auto-merge, never fails to the user

Client keeps a **last-synced snapshot** per blob type (the JSON as of the
last successful push/pull), separate from the live local file. A brand-new
machine has no snapshot and no server row for the user yet — see the
bootstrap case below.

On push:
1. Route handler opens a transaction and takes
   `pg_advisory_xact_lock(hashtext(user_id)::bigint)` first — better-auth
   ids are strings (cuid/uuid), so they're reduced to an int64 via
   `hashtext()` before locking. This serializes all sync writes for that
   user, safe if kakiit later scales to multiple instances.
2. Client sends `{version: lastKnownVersion, blob: <current local JSON>}`.
   `lastKnownVersion` is `0` for a user's first-ever push of a blob type
   (no row exists yet); the server creates the row at `version: 1`.
3. If `version` matches the server's current version: write, `version + 1`,
   return success.
4. If stale: server responds with its current blob + version (not an
   error). Client performs a **3-way JSON merge** — `base` (its last-synced
   snapshot) vs `local` (current file) vs `remote` (server's latest) —
   applying the union of keys changed since `base` on top of `remote`.
   Conflict resolution when the same key changed on both sides since `base`
   differs by blob type:
   - `ocodeconfig`: **local wins** — the edit the user just made on this
     device takes precedence (prefs are cheap to re-diverge, and the
     device the user is actively using should reflect what they just did).
   - `authsecrets`: **remote wins** — provider credentials can carry
     rotated refresh tokens; blindly keeping a local value risks
     resurrecting a token another device already rotated past. The local
     device instead adopts the remote credential for that key.
   Client writes the merged JSON to disk and retries the push with the new
   version. This retry is bounded: up to 5 attempts with exponential
   backoff (250ms base). If still conflicting after 5 attempts, the sync
   cycle gives up silently (logged only) and tries again on the next
   debounce tick or the next startup pull — never surfaced to the user, and
   the local file is left in its last successfully-merged state either way.
5. On success, client updates its last-synced snapshot to the pushed JSON.

**Bootstrap (new machine, no local snapshot):** before any push, if the
client has no last-synced snapshot for a blob type, it first does a pull
(§6 startup pull). If the server has a row, that becomes the initial
snapshot and local file (merged with whatever's already on disk using the
same rules above, treating `base` as empty). If the server has no row
either (first device ever for this account), the local file becomes the
initial push with `version: 0`.

## 6. Background deferred sync (ocode client)

`internal/sync` hooks the existing `Save*` functions in
`internal/config/ocodeconfig.go` and `internal/auth/store.go`. Each local
write resets a ~5s idle debounce timer (independent per blob type); when it
fires, push runs in the background. Network failures are retried with
backoff and logged — never interrupt the user's session. On CLI/`ocode
serve` startup, one opportunistic pull-merge runs to catch up on changes
made from other devices.

## 7. Error handling

- Background push network/server errors: retried with backoff, logged only.
- Local file write failure during merge: fails only that sync cycle,
  retried on the next debounce tick.
- Device-code login errors (expired/denied code): surfaced directly, since
  `/login` is an explicit foreground action, not background sync.

## 8. Logout & account lifecycle

- `ocode /logout` calls `POST /api/ocode/device/revoke` with the stored sync
  bearer token, which kakiit deletes server-side; CLI deletes the local
  token file. Background sync stops (no token → push/pull skipped, logged
  once, not retried).
- Account deletion/deactivation on kakiit cascades: deleting a `user` row
  cascades to `ocode_sync_keys` and `ocode_config_syncs` via FK
  `ON DELETE CASCADE`, and any outstanding sync bearer tokens for that user
  are invalidated (checked at request time via the same session-token
  lookup already required for `/api/ocode/sync/*`).

## 9. Testing

- Go (`internal/sync`): 3-way JSON merge (base/local/remote fixtures,
  per-blob-type conflict policy — local-wins for `ocodeconfig`, remote-wins
  for `authsecrets`), bounded-retry-with-backoff exhaustion behavior,
  bootstrap flow (no local snapshot, with and without an existing server
  row), debounce timer behavior, device-code polling state machine,
  snapshot tracking across push/pull/merge cycles, logout clearing local
  token and halting background sync.
- kakiit (`/api/ocode/*` route tests): advisory lock serializes concurrent
  pushes from two simulated devices, stale-version push returns
  current-state (not an error), envelope encrypt/decrypt round-trip,
  device-code approve/deny/expire paths, revoke invalidates the token,
  account-deletion cascade removes sync rows.

## Out of scope (this spec)

- The LLM gateway/proxy itself — only the encryption model is chosen to not
  block it later.
- Multi-instance kakiit deployment — the advisory lock is chosen to survive
  it, but no scaling work happens here.
- Any UI in kakiit beyond the minimal `/device` approval page.
