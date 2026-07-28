# Account Login + Encrypted Config Sync (via kakiit) — Design

- **Date:** 2026-07-28
- **Status:** Approved (design) — pending implementation plan
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
last successful push/pull), separate from the live local file.

On push:
1. Route handler opens a transaction and takes
   `pg_advisory_xact_lock(hash(user_id))` first — serializes all sync writes
   for that user, safe if kakiit later scales to multiple instances.
2. Client sends `{version: lastKnownVersion, blob: <current local JSON>}`.
3. If `version` matches the server's current version: write, `version + 1`,
   return success.
4. If stale: server responds with its current blob + version (not an
   error). Client performs a **3-way JSON merge** — `base` (its last-synced
   snapshot) vs `local` (current file) vs `remote` (server's latest) —
   applying the union of keys changed since `base` on top of `remote`; a
   key changed on both sides since `base` resolves local-wins (the edit the
   user just made takes precedence). Client writes the merged JSON to disk
   and retries the push with the new version. This retry is internal to
   `internal/sync` — it never surfaces as a failure.
5. On success, client updates its last-synced snapshot to the pushed JSON.

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

## 8. Testing

- Go (`internal/sync`): 3-way JSON merge (base/local/remote fixtures,
  same-key-both-sides conflict resolution), debounce timer behavior,
  device-code polling state machine, snapshot tracking across
  push/pull/merge cycles.
- kakiit (`/api/ocode/*` route tests): advisory lock serializes concurrent
  pushes from two simulated devices, stale-version push returns
  current-state (not an error), envelope encrypt/decrypt round-trip,
  device-code approve/deny/expire paths.

## Out of scope (this spec)

- The LLM gateway/proxy itself — only the encryption model is chosen to not
  block it later.
- Multi-instance kakiit deployment — the advisory lock is chosen to survive
  it, but no scaling work happens here.
- Any UI in kakiit beyond the minimal `/device` approval page.
