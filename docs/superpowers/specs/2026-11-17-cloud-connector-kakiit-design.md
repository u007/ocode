---
type: Design
title: Central Cloud Connectors via kakiit Token Brokerage (design)
description: Design for making kakiit the central cloud-storage connector that brokers OAuth and refresh tokens, while ocode keeps the live connection encrypted in the per-project .ocode/storage-connectors.json and calls providers directly. Covers the two-repo change (kakiit hub endpoints plus the ocode client), a fixed-redirect PKCE handoff, passwordless key management, a staged provider rollout, project and remote-host scoping, the legacy local-OAuth fallback, Settings UI beautification, and the test and verification matrix.
tags:
  - design-spec
  - cloud-connectors
  - kakiit
  - oauth
  - pkce
  - hub
  - settings-ui
  - encryption
  - remote-ssh
timestamp: 2026-11-17T15:30:00Z
---
# Central Cloud Connectors via kakiit Token Brokerage (design)

**Status:** Written and under user review, 2026-11-17. Not yet approved; no product code is written by this page.

**Anchor convention:** every `file:line` citation below is **as-of 2026-11-17**. Re-derive each anchor by symbol before relying on it — line numbers drift when code above a citation changes.

## 1. Goal

Replace per-project cloud-connector identities with one **central kakiit connector**. kakiit owns the OAuth application credentials, runs the provider OAuth flow, and refreshes tokens. ocode stores the resulting **access token only**, encrypted, in the project's `.ocode/storage-connectors.json`, and talks directly to the provider for browse/read/write. The Settings surface is simultaneously beautified.

Success looks like:

1. A user with a kakiit login connects a cloud provider once from Settings and uses it in every project through one central connector.
2. The refresh token never touches disk; only kakiit can refresh.
3. A user without a kakiit login keeps working cloud connectors through the existing local-OAuth path.
4. The Settings cloud-connector section looks and behaves like the rest of the app.

## 2. Locked decisions

These came from explicit user answers during brainstorming, then an advisor course-correction. Do not re-litigate without a new user decision.

| # | Decision |
|---|----------|
| 1 | Scope is **both** the UI beautification and the kakiit rewire. |
| 2 | kakiit **issues and refreshes** tokens. It is a token custodian, not a stateless issuer. App credentials stay only in kakiit. |
| 3 | The local per-project file remains the connection store: `<projectRoot>/.ocode/storage-connectors.json`. |
| 4 | The local file holds **access token + expiry + opaque `connection_id` only**. No refresh token, no client secret. |
| 5 | Credentials are **encrypted** in that JSON. The key needs **no user password**. |
| 6 | Handoff uses a **fixed, pre-registered redirect callback** — never a client-supplied URL. PKCE is mandatory on both hops. |
| 7 | Provider rollout is **staged**: slice 1 Dropbox only; slice 1.5 Google Drive gated on Google app verification; slice 2 OneDrive + user-OAuth GCS; S3 and service-account GCS stay local-only. |
| 8 | The existing **local OAuth path stays** as a fallback for users with no hub login. |
| 9 | Every `/api/cloud/*` request takes a validated `project` param; remote projects route through the owning host via `?host=`. |
| 10 | UI work uses existing shadcn primitives and lucide icons. No new dependencies. |

## 3. Current state (as-of anchors)

### ocode (`/Users/james/www/ocode`)

- OAuth is local today: `web/src/components/Settings/CloudConnectorsForm.tsx:86-105` → `api.startCloudOAuth` (`web/src/api/client.ts:5240-5241`) → `window.open(auth_url)`.
- Start handler `internal/server/handler_cloudconnect_oauth.go:95-160`; callback `:162-241` builds `SecretBundle{ClientID, ClientSecret, AccessKey: accessToken, RefreshToken}` and calls `mgr.Register`. `oauthEndpoints` at `:71-82` maps only `gdrive`/`onedrive`/`dropbox`; note the existing bug that the UI default `"google-drive"` is not a key there.
- Routes: `internal/server/server.go:335,342-343`.
- Persistence: `internal/cloudconnect/manager.go:34-50` and `:127-151` — keychain service `ocode-cloud`, account `_all`, one JSON blob keyed by connector id; fallback encrypted file `<projectRoot>/.ocode/cloudconnect-secrets.enc` with `ENCv1` Argon2id+AES-GCM. Master password comes from `OCODE_CLOUD_MASTER_PASSWORD` (`internal/server/handler.go:791`); when empty the store is in-memory only.
- Metadata file: `internal/config/storage_connectors.go:17-41` — `StorageConnectorsFile{Settings map[string]ConnectorSettings}` keyed by connector id, currently **plaintext**; `ConnectorSettings.ProjectSlug` at `:20`.
- Manager is built once at startup with `projectRoot = defaultWorkDir` from `os.Getwd()` (`internal/server/handler.go:735,752,791`). Every cloud handler uses `h.cloudManager()` with no project or host param (`internal/server/handler_cloudconnect.go:14-19,25-243`).
- Provider refresh is local: `gdrive.go:59-95`, `onedrive.go:57-95`, `gcs.go:54-91`, `dropbox.go:57-89`; `s3.go` uses static HMAC keys and has no OAuth.
- Reusable hub pieces: sync base URL `internal/sync/sync.go:79-95` (`OCODE_SYNC_URL`, else `https://hub.mercstudio.com`); sync token at `<GlobalDataDir>/sync/token` (`sync.go:144-150`); `HubClient` with Bearer at `internal/artifacts/hubclient.go:20-84`; handler helpers `internal/server/handler_sync.go:60-77` and `internal/server/artifact_hub.go:144-153`.
- UI primitives present: `web/src/components/ui/` has `badge`, `button`, `command`, `context-menu`, `dialog`, `scoped-dialog`, `input`, `popover`, `progress`, `scroll-area`, `select`, `separator`, `tabs`, `tooltip`. No `card.tsx`, `label.tsx`, or `switch.tsx`. shadcn is configured in `web/components.json`; `lucide-react` is in `web/package.json`.
- Tests: Go coverage exists for manager/persist/providers/config and a register+list handler test (`internal/server/handler_cloudconnect_test.go:13`). **Zero** tests for the OAuth start/callback handlers and **zero** web tests for `CloudConnectorsForm` or the cloud API methods.

### kakiit (`/Users/james/www/kakiit`)

- **TanStack Start + Hono**, not Next.js: `src/server.ts:16-19,41-63`.
- Mount chain: `src/server/api/index.ts:299` creates the Hono app; ocode groups mount at `:748-762` (`/ocode/device`, `/ocode/sync`, `/messaging`, `/ocode/artifacts`, `/p`). New groups mount there.
- Machine auth: `requireSyncToken` at `src/server/api/lib/ocode-sync-auth.ts:25-42`, backed by `resolveSyncToken` (`src/server/services/ocode-sync/device-code-service.ts:115-131`). It sets `c.get('syncUserId')`. Reuse it unchanged; user-session auth (`requireAuth`, `lib/auth.ts:162-181`) is a different token.
- Best reusable OAuth pattern: Dropbox storage connector — `src/server/services/storage/oauth-service.ts:75-139` (start: random state `:98`, verifier `:99`, S256 challenge `:100`, state stored only as SHA-256 `:63-65,123`, sealed verifier+secret `:103-115`, 10-minute TTL) and `:156-259` (complete: single conditional UPDATE consume `:166-183`, open envelope `:185-194`, code exchange `:196-204`, refresh token required `:217-219`, persist `:229-258`). Routes `src/server/api/routes/storage.admin.ts:353-425`.
- Envelope crypto: `src/server/envelope-crypto.ts:7-18`; connector secret schema/seal/open: `src/server/services/storage/connector-secrets.ts:21-50`.
- Neighbor tables: `storage_connector` (`src/server/db/schema-storage.ts:94-169`) and `storage_oauth_attempt` (`:171-199`) are tenant-scoped with RLS; ocode tables (`src/server/db/schema.ts:6216-6314`) are keyed by `userId` with no RLS. `createTable` at `src/server/db/schema-helpers.ts:11` maps camelCase props to snake_case columns.
- Tests use **`bun:test`**, not vitest. Pattern: mount the sub-router on a fresh `Hono`, drive with `app.request()`, mock with `mock.module` — see `src/server/api/routes/ocode.device.test.ts`. Do **not** import the full `src/server/api/index.ts` in unit tests.
- No Google Drive adapter, no `GOOGLE_DRIVE_CLIENT_ID/SECRET` env, no cloud-token table. Dropbox is the only `StorageProvider` (`schema-storage.ts:52-56`). Hub base URL is `APP_URL` (`src/env-schema.ts:59`, default `http://localhost:3201`).

## 4. Architecture — kakiit side

### 4.1 New route group

Add `src/server/api/routes/ocode.cloud.ts`, mounted under `/api/ocode/cloud` in `src/server/api/index.ts` near `:748-762`, typed `Hono<SyncEnv>` with `requireSyncToken` on every route.

| Route | Purpose |
|-------|---------|
| `PUT /callback` | Register the ocode callback for this install (sync Bearer required). Allow loopback `http://127.0.0.1:<port>` — ocode's callback is a local listener in normal production, so an https-only rule would break every install. Require https for any non-loopback host. Do **not** reject a host change: one user may have a laptop and a VPS, and a valid sync token already authorizes every route. Overwrite the install's registered callback. |
| `GET /connections` | List this sync user's central connections: `provider`, `connection_id`, `root_path`, `expires_at`. ocode uses this to discover a central connection in a project that has no local file yet. |
| `POST /oauth/start` | Body `{provider, connector_id, install_id, client_state, code_challenge}`. Two distinct states: **client_state** is ocode's opaque value binding project + host + nonce; **provider_state** is kakiit's random value stored only as SHA-256 in the attempt. Derive redirect URI from the **registered** callback (never the body). Start provider OAuth with PKCE + provider_state, persist an `ocode_cloud_oauth_attempt`, return `{auth_url}`. |
| `GET /oauth/callback` | Browser redirect with `code` + provider_state and **no Bearer**: `userId` comes from the sealed attempt, never from the request. Consume the attempt with one conditional UPDATE, exchange the code, obtain access + refresh tokens, encrypt them with envelope crypto, persist an `ocode_cloud_connection`, redirect to the registered ocode callback with a one-time code bound to `userId`, `connection_id`, and `client_state`. |
| `POST /oauth/redeem` | Body `{code, code_verifier, client_state}`. Redeem a stored one-time code (code hash, `userId`, `connection_id`, `client_state`, code challenge, ~60 s TTL, single-use conditional consume). Verify all bound fields, the PKCE verifier, and the Bearer; return `{connection_id, access_token, expires_at}`. |
| `DELETE /connections/:connectionId` | Disconnect: revoke the connection for this sync user/provider. Requires the sync Bearer and ownership. |
| `POST /token/refresh` | Body `{connection_id}`. Verify the connection belongs to `syncUserId` and is not revoked. Return a **valid** access token + expiry: refresh at the provider with the stored refresh token only when the current token is expired or about to expire, update the stored token, and return it. Also used for first-use fetch in a new project. |

**Cross-cutting:** ocode keeps the PKCE verifier in memory keyed by `client_state` with a short TTL; it is never sent at start. Because one `ocode_cloud_connection` is shared by every project of a sync user, **Disconnect revokes globally** for that user/provider; each project's cached copy goes stale on next refresh and is then cleared. The Settings confirm dialog must say so.

### 4.2 New tables (additive Drizzle migration)

- `ocode_cloud_connection` — keyed by `userId` + opaque `connection_id`; stores `provider`, encrypted token envelope, `access_token_expires_at`, `created_at`, `revoked_at`. **At most one active connection per (`userId`, `provider`)** — starting a new OAuth for a provider revokes the previous one, which makes Disconnect global per user/provider as §6 states. No RLS, matching the neighboring ocode tables.
- `ocode_cloud_oauth_attempt` — keyed by SHA-256 `stateHash`; sealed `{verifier, connectorId, provider, userId, clientState}`, 10-minute TTL, `consumedAt`. Shape mirrors `storage_oauth_attempt` (`schema-storage.ts:171-199`).
- `ocode_cloud_one_time_code` — keyed by SHA-256 `codeHash`; stores `userId`, `connectionId`, `clientState`, `codeChallenge`, `expiresAt` (~60 s), `consumedAt`. Single-use conditional consume.
- `ocode_cloud_callback` — keyed by (`userId`, `installId`); stores the registered `callbackUrl`. Overwritten by `PUT /callback`.

Migration rules: append-only, snake_case via `createTable`, generate with `bun run db:generate`, and prove the chain on a fresh DB before pushing. Do not delete a committed migration.

### 4.3 Provider adapters

Slice 1 reuses the existing `DropboxAdapter` (`src/server/services/storage/dropbox/dropbox-adapter.ts:241`) against the provider-neutral contract in `src/server/services/storage/types.ts:3-4`. First confirm its refresh path exposes the refresh token kakiit must custodian; extend the adapter if it does not. Google Drive is **slice 1.5**, gated on a verified Google OAuth app: add a `GoogleDriveAdapter`, use the `drive.file` scope, add `GOOGLE_DRIVE_CLIENT_ID` / `GOOGLE_DRIVE_CLIENT_SECRET` to `src/env-schema.ts` and `.env.example`, and keep `APP_URL` as the redirect base. Confirm the adapter contract supports the refresh-token handoff before writing the Drive adapter.

## 5. Architecture — ocode side

### 5.1 Hub client

Add a small `cloudhub` client behind an interface so local-OAuth and hub paths share one `TokenSource` contract. Reuse `internal/sync` for base URL and bearer (`sync.go:79-95`, token file `:144-150`) and the Bearer-header pattern from `internal/artifacts/hubclient.go:20-84`. Use `sync_url` only — never the local-dev-only `backend_url`. No fallback to another hub backend on error.

### 5.2 Local file v2

Replace `StorageConnectorsFile` with a versioned shape in `internal/config/storage_connectors.go`:

- `Version: "v2"`.
- `Central`: single connector `{ConnectorID: "kakiit", Mode: "hub"|"local", Connections: map[provider]ConnectionMeta}`.
- `ConnectionMeta` (plaintext): `Provider`, `ConnectionID`, `ExpiresAt`, `RootPath`, `MaxCacheBytes`, `Status`.
- `CredentialsEnvelope`: a `SealWithKey` envelope (§5.3) that decrypts to `map[provider]Credentials{AccessToken, RefreshToken?}`. `RefreshToken` is present **only** for `Mode:"local"` (legacy) connections. Hub-mode connections never contain one.

`ConnectorSettings.ProjectSlug` and per-project connector ids (e.g. `gdrive-main`) are retired as identity. Provider becomes the key inside `Connections`.

Migration v1 → v2 is additive and non-destructive: fold the old `Settings` map into the central entry, preserve each provider's `root_path` and `max_cache_bytes`, move existing keychain secrets into the encrypted envelope as `mode:"local"`, keep the old keychain blob and `cloudconnect-secrets.enc` untouched until migration is verified. Hub mode is opt-in per connection; no old refresh token is transferred to kakiit.

### 5.3 Key management (no password)

Resolve a random 32-byte key per install through a new `keystore.Resolve()` adapter, in this order:

1. A **native** OS keychain only. `keychain.New()` (`internal/keychain/keychain.go:43-59`) is not sufficient: when no native tool exists it silently returns a fallback store whose key is derived from a guessable `machineID()` (`internal/keychain/fallback.go:23-26`). Detect native availability by calling `newPlatformStore` directly, or add an exported `keychain.NativeAvailable()`. **Never use the fallback store for this key** — use service `ocode-cloud-keystore`.
2. A 0600 key file under `<GlobalDataDir>/cloud-connectors/key` — **never** inside a project's `.ocode/`, because that directory may be committed.
3. If no native keychain and no writable key file: keep tokens in memory for the session only, persist nothing, and surface a clear "connection not saved" status in Settings.

This removes the `OCODE_CLOUD_MASTER_PASSWORD` dependency for the new path. Add `SealWithKey` / `OpenWithKey` to `internal/encryption` — AES-GCM over a raw 32-byte key with no KDF, under a new `ENCv2:raw:` prefix — and cover it in `internal/encryption/envelope_test.go`. `EncryptJSON`/`DecryptJSON` (`envelope.go:36,66`) are password + Argon2id (64 MiB, t=3) and must not run on every token refresh. Leave the `ENCv1` format and its pinned Argon2id parameters untouched; existing `ENCv1` data keeps its own reader.

### 5.4 Provider adapter and refresh

Replace each provider's local token refresh (`gdrive.go:59-95`, `onedrive.go:57-95`, `gcs.go:54-91`, `dropbox.go:57-89`) with a `TokenSource` call:

- Hub mode: call kakiit `POST /token/refresh` with `connection_id`. It returns a valid access token, refreshing at the provider only when needed. Write the token + expiry to the local file, then retry the provider request once.
- Local mode: keep the current refresh logic against the provider, reading credentials from the encrypted envelope.
- S3 and service-account GCS stay local-only and keep static keys in the envelope.

If kakiit is unreachable or has no token, surface an explicit error. Do not silently use a stale token past a short grace window. Guard refresh with a **per-connection mutex (single-flight)**: concurrent browse/read calls must share one refresh, not race on the token file. The new path never uses the keychain account `_all` — `Manager.load` / `saveSecrets` (`internal/cloudconnect/manager.go:79-80,127-151`) write one global blob, which would clobber per-project state. The migration surface is the `h.cloudManager()` call sites in `internal/server` (8 non-test) plus `NewManager` construction; enumerate them in the plan.

### 5.5 Project and remote scoping

- Add `project` to every `/api/cloud/*` request; validate against `allowedProjectRoots()` and resolve the file path from that root only. Never call `os.Getwd()` in a server handler (`internal/server/handler.go:752` is existing debt to remove on this path).
- Replace the single startup manager with a cache keyed by resolved project root.
- Remote projects: redeem and refresh on the server that owns the project via `?host=`; that server writes the file on the host. Never fall back to the local agent for a remote project.
- Bind `project` + `host` + a random nonce into `client_state` so a callback cannot be replayed against a different project. This is ocode's state, distinct from kakiit's `provider_state` (§4.1).

## 6. End-to-end handoff

1. User picks a provider in Settings and clicks **Connect via kakiit**. Web calls ocode `POST /api/cloud/oauth/kakiit/start?project=…&host=…` with `{provider, code_challenge, state}`.
2. ocode validates the project, then calls kakiit `POST /api/ocode/cloud/oauth/start` with the sync Bearer. kakiit derives the redirect URI from the registered callback, starts provider OAuth, returns `auth_url`.
3. Web opens `auth_url` in a popup. The provider redirects to kakiit; kakiit exchanges the code, stores tokens, then redirects the popup to the fixed ocode callback with a one-time code and `state`.
4. ocode's fixed callback route verifies `client_state` (project + host + nonce), calls kakiit `POST /oauth/redeem` with the one-time code, `client_state`, and its in-memory PKCE verifier, writes the encrypted local file, and returns a small HTML page that closes the popup.
5. A central connection is usable from **any** project: when a project has no local file yet, ocode calls kakiit `GET /connections` to discover the central connection, then `POST /token/refresh` to fetch a valid access token, and writes it to that project's file. This is what makes one central kakiit connection work across projects while each project keeps its own saved copy.
6. Browse/read/write use the access token directly against the provider. If it has expired, the step 5.4 refresh runs and the request is retried once.

Security invariants: redirect URI is server-registered (fixed host + path), never client-supplied; PKCE on both hops; the PKCE verifier stays in ocode memory keyed by `client_state` with a short TTL; the callback carries no Bearer and derives `userId` from the sealed attempt; one-time codes are single-use with a ~60 s TTL and bound to `userId`, `connection_id`, and `client_state`; `/token/refresh` checks ownership and revocation; refresh is single-flight; no bearer, refresh token, or access token in logs, SSE frames, transcript, or error strings; sync Bearer only in the Authorization header; tokens in URLs are one-time codes only and are useless without the verifier.

## 7. Settings UI beautification

Rewrite `web/src/components/Settings/CloudConnectorsForm.tsx` in place, matching the dominant Settings pattern (raw Tailwind + `Button`/`Input`, semantic tokens like `border-border` / `text-muted-foreground`) while using the primitives that already exist.

- Header: central connector status (`Connected` / `Not logged in to kakiit` / `Local only`) with a primary **Connect via kakiit** button.
- One row per provider: lucide provider icon, provider label, `Badge` status (`Active`, `Expired`, `Local-only`, `Not connected`), root path, expiry. Sort by provider name.
- Row actions: Browse, Refresh, Disconnect; destructive Remove behind the existing confirm pattern.
- Collapsible **Advanced — local-only connection** section retaining the current fields for the legacy fallback.
- Empty state with an icon and one line of copy instead of `None.`.
- Use `Badge`, `Separator`, `Select`, `Tooltip`, and `Button` variants; labeled inputs; no `card.tsx` (it does not exist) and no new npm dependency.
- Add `web/src/components/Settings/CloudConnectorsForm.test.tsx` — this surface has no tests today. Cover connect CTA visibility by status, sorted provider rows, status badges, browse action, and confirm-on-remove.

## 8. Test strategy

Write tests first for the high-risk paths — the advisor flagged that no OAuth handler tests exist today.

**kakiit (`bun:test`, serial/isolated):**
- `ocode.cloud.test.ts` — start rejects a client-supplied redirect URI; callback consumes the attempt once; redeem fails with wrong verifier or wrong bearer; refresh updates the stored token; all routes 401 without a sync token.
- Mock `requireSyncToken` and the provider service with `mock.module`; mount the sub-router on a fresh `Hono`; do not import `api/index.ts`.
- Prove the additive Drizzle migration on a fresh DB.

**ocode (Go + web):**
- `internal/server/handler_cloudconnect_oauth_test.go` — start/return/refresh handlers, project validation (reject outside `allowedProjectRoots`), state binding, no token in responses.
- `internal/config/storage_connectors_test.go` — v1→v2 migration, `SealWithKey`/`OpenWithKey` round-trip, `RefreshToken` absent in hub mode.
- `internal/encryption/envelope_test.go` — `ENCv2:raw:` round-trip, rejection of a truncated envelope, and existing `ENCv1` vectors still decrypt.
- `internal/keychain` — native-vs-fallback availability detection; prove the fallback store is never used for the connector key.
- `internal/cloudconnect` — key resolution chain, per-root manager cache, no persistence when no native keystore and no key file, single-flight refresh under concurrent calls.
- Web test per §7.

**Final verification:** `go test ./internal/cloudconnect ./internal/config ./internal/server`, `cd web && npm run test`, `cd web && npm run typecheck`, `cd web && npm run build`, and the affected `bun test` set in kakiit.

## 9. Implementation sequencing

Keep both repos buildable at every step:

1. **kakiit first** (additive): schema + migration, route group, Dropbox adapter wiring + refresh-token exposure check, `bun` tests. Drive is a separate slice 1.5 after Google verification.
2. **ocode hub client** behind the `TokenSource` interface, with unit tests; local path still works.
3. **Local file v2 + `ENCv2:raw:` key management + native-keystore detection + migration**, still serving local mode.
4. **Handoff handlers + project threading + refresh**, wire hub mode.
5. **Settings UI beautification** last, once the statuses it renders are real.

## 10. Consequences, risks, blockers

- kakiit is a token custodian: a kakiit outage blocks refresh. The cached access token still works until expiry; there is no silent fallback to stale credentials.
- Users without a hub login keep local-OAuth connections; hub mode is unavailable to them by design.
- Disconnect revokes the central connection for the whole sync user/provider (one `ocode_cloud_connection` is shared); local copies go stale and are cleared on next refresh. The UI confirm must state this.
- Google OAuth app verification and `drive.file` scope gate **slice 1.5** (Drive). Slice 1 ships Dropbox alone, so verification does not block the first vertical slice.
- No tests exist today for the OAuth handlers; they are written first, before behavior changes.
- Old plaintext metadata fields stay as-is; only the credentials envelope is encrypted. Never move secrets into `ocodeconfig.json`.
- Expect a large unrelated working tree in both repos from concurrent sessions; scope commits deliberately and commit by path.

## 11. Cross-links

- `docs/cloud-connectors-design.md` — original connector design (providers, keychain, per-project file).
- `docs/cloud-connectors-encryption.md` — `ENCv1` envelope and pinned Argon2id parameters; also records main-agent authorship for cloud-connector design docs.
- `docs/plans/2026-10-08-cloud-connectors-phase1.md` — phase-1 plan.
- `docs/concepts/artifact-publishing.md` — ocode↔kakiit hub contract, `sync_url`, Bearer, 409/502 rules, no fallback.
- `docs/concepts/backend-sync-url-split.md` — `sync_url` vs local-dev-only `backend_url`.
- `docs/concepts/web-connector-settings.md` — existing connector-settings surface.
- `docs/concepts/web-server-project-scoping.md` — session work follows the session project root; `h.workDir` is only the default.
