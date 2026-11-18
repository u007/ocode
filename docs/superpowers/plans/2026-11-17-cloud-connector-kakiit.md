# Central Cloud Connectors via kakiit Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans (default) or superpowers:subagent-driven-development (long plans of independent tasks) to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make kakiit the central cloud-storage connector that brokers and refreshes tokens, keep only the encrypted access token in each project's local file, and refresh the Settings UI — shipping Dropbox as a complete vertical slice.

**Architecture:** kakiit gains four additive tables, a token-custody service, and an `/api/ocode/cloud` Hono group behind the existing sync-token auth, reusing its Dropbox OAuth pattern. ocode gains a raw-key `ENCv2:raw:` envelope, a native-keystore resolver, a v2 per-project file, a thin `cloudhub` client, and handler/UI wiring behind a `TokenSource` interface. Tasks 1–2 (kakiit) and Task 3 (ocode crypto/storage) are independent and may run in parallel; Tasks 4a → 4b → 5 are strictly sequential.

**Tech Stack:** Go 1.26.1 (`internal/encryption`, `internal/keychain`, `internal/config`, `internal/cloudconnect`, `internal/server`, `internal/sync`), React 19 + shadcn/Radix + lucide-react + Vitest (web SPA), TanStack Start + Hono + Drizzle/Postgres + `bun:test` (kakiit).

**Spec:** `docs/superpowers/specs/2026-11-17-cloud-connector-kakiit-design.md`

**Detail:** decisions, not code — the default; the implementer writes the implementation.

**Repo roots:** ocode = `/Users/james/www/ocode`; kakiit = `/Users/james/www/kakiit`. Paths below are relative to the named repo root.

## Global Constraints

- kakiit issues **and refreshes** tokens; it is the token custodian. OAuth app credentials live only in kakiit.
- The local file holds **access token + expiry + opaque `connection_id` only**. No refresh token, no client secret.
- Token credentials are encrypted with a raw 32-byte key under `ENCv2:raw:` (AES-GCM, **no KDF**). Leave `ENCv1` (password + Argon2id 64 MiB, t=3) and its pinned parameters untouched; its reader must keep working.
- No user password. Key from a **native** OS keychain only, or a 0600 file under `<GlobalDataDir>/cloud-connectors/key`. **Never** inside a project's `.ocode/`. Never from `keychain.New()`'s `machineID()` fallback store — `newPlatformStore` is unexported, so require a new exported `keychain.NativeAvailable()`.
- Redirect URI comes from the **registered** callback, never the request body. ocode's callback is a local listener, so loopback `http://127.0.0.1:<port>` is valid in production; require https only for non-loopback hosts. Do not reject a host change on re-registration — a valid sync token already authorizes it and one user may have several machines.
- PKCE on both hops. One-time code: single-use conditional consume, ~60 s TTL, bound to `userId` + `connection_id` + `client_state`.
- Two distinct states: `client_state` (ocode: project + host + nonce) and `provider_state` (kakiit: SHA-256 in the attempt). Callback carries no Bearer; `userId` comes from the sealed attempt.
- `/token/refresh` checks `connection_id` belongs to `syncUserId` and is not revoked. Refresh is single-flight (per-connection mutex).
- Hub base URL from `sync_url` only — never `backend_url`. No fallback to another hub backend. Sync Bearer in Authorization header only, never logged.
- Every `/api/cloud/*` request takes a validated `project` param via `allowedProjectRoots()`; remote projects use `?host=` and are served by the owning host. Never call `os.Getwd()` in a server handler.
- The new path never uses keychain account `_all` (`internal/cloudconnect/manager.go:79-80,127-151` is one global blob).
- **At most one active connection per (`userId`, `provider`)** in kakiit. Starting a new OAuth for a provider revokes the previous one; Disconnect revokes globally for that user/provider.
- Slice 1 ships **Dropbox only**. OneDrive / user-OAuth GCS are slice 2; S3 and service-account GCS stay local-only. Google Drive is a gated slice 1.5 with its own plan.
- Legacy local OAuth stays as a fallback for users with no hub login.
- Drizzle migrations are append-only, snake_case via `createTable`; prove the chain on a fresh DB.
- kakiit tests use `bun:test --serial --isolate --preload ./src/test-preload.ts` and must not import the full `src/server/api/index.ts`. ocode uses Go tests and `npx vitest run <file>` (web's `npm run test` is `vitest run --maxWorkers=4`). No new npm dependencies; use existing shadcn primitives and lucide icons.
- **A Go test that overrides `HOME` must use the package's `setHomeTree(t, home)` helper, never a bare `t.Setenv("HOME", …)`** — ocode's path resolution is platform-split and XDG variables are set package-wide in `TestMain`.

## Wire Contract (freeze once; both sides depend on it)

All kakiit routes are under `/api/ocode/cloud` with `Authorization: Bearer <sync-token>` except the provider redirect. Errors are `{ "error": string }` plus HTTP status.

| Route | Request JSON | Response JSON |
|-------|--------------|---------------|
| `PUT /callback` | `{install_id, callback_url}` | `{ok: true}` |
| `GET /connections` | — | `[{provider, connection_id, root_path, expires_at}]` |
| `POST /oauth/start` | `{provider, connector_id, install_id, client_state, code_challenge}` | `{auth_url}` |
| `POST /oauth/redeem` | `{code, code_verifier, client_state}` | `{connection_id, access_token, expires_at}` |
| `DELETE /connections/:connectionId` | — | `{ok: true}` |
| `POST /token/refresh` | `{connection_id}` | `{access_token, expires_at}` |

## Review Focus

These failure modes are implied by the spec but easy to miss in task tests. Each is pinned to a task below.

1. No native keychain and an unwritable home → key must not come from the guessable `machineID()` fallback; tokens stay in memory only. → Task 3 tests.
2. A callback whose `client_state` does not match (different project or host) → must be rejected, never applied. → Task 4b test.
3. kakiit unreachable at refresh → explicit error, never silent use of a stale token. → Task 4b test.
4. Disconnect from one project → global revoke; another project's cached copy goes stale and clears on next refresh. → Task 4b test + Task 5 confirm copy.
5. A one-time code replayed twice at `/oauth/redeem` → second attempt rejected. → Task 2 test.

---

## File Structure

**kakiit (`/Users/james/www/kakiit`)**
- `src/server/db/schema-ocode-cloud.ts` — new: four ocode-cloud tables (snake_case, userId-keyed, no RLS).
- `drizzle/<next>_ocode_cloud.sql` — generated additive migration.
- `src/server/services/ocode-cloud/token-service.ts` + `.test.ts` — new: sealed-token custody, attempt/code consume, connection CRUD, callback registration.
- `src/server/api/routes/ocode.cloud.ts` + `.test.ts` — new: `/api/ocode/cloud` Hono group.
- `src/server/api/index.ts` — modify: mount the new group.

**ocode (`/Users/james/www/ocode`)**
- `internal/keychain/keychain.go` — modify: add exported `NativeAvailable()`.
- `internal/encryption/envelope.go` + `envelope_test.go` — modify: raw-key `ENCv2:raw:` seal/open.
- `internal/cloudkeystore/keystore.go` + `keystore_test.go` — new: native-keystore/key-file/memory resolution.
- `internal/config/storage_connectors.go` + `storage_connectors_test.go` — modify: v2 schema, `LoadV2`/`SaveV2` (no migration here — import cycle).
- `internal/cloudhub/client.go` + `client_test.go` — new: thin hub client.
- `internal/cloudconnect/manager.go` + `migration_test.go` — modify: `TokenSource`, single-flight, v1→v2 migration, no `_all`.
- `internal/server/handler_cloudconnect_kakiit.go` + `_test.go` — new: handlers.
- `internal/server/handler.go`, `internal/server/server.go` — modify: per-root manager cache, routes, remove startup `os.Getwd()` binding.
- `web/src/api/client.ts` — modify: hub connector methods with `project`/`host` threading.
- `web/src/components/Settings/CloudConnectorsForm.tsx` + `.test.tsx` — modify/create: UI refresh.

---

### Task 1: kakiit cloud-token schema and custody service

**Risk:** high — persistence, encryption envelope, single-use atomic consume, cross-user isolation.

**Files:**
- Create (kakiit): `src/server/db/schema-ocode-cloud.ts`
- Create (kakiit): `src/server/services/ocode-cloud/token-service.ts`
- Test (kakiit): `src/server/services/ocode-cloud/token-service.test.ts`
- Generate (kakiit): `drizzle/<next>_ocode_cloud.sql`

**Interfaces:**
- Consumes: `createTable` (`src/server/db/schema-helpers.ts:11`); `aesGcmSeal`/`aesGcmOpen` (`src/server/envelope-crypto.ts:7-18`); `sealConnectorSecrets`/`openConnectorSecrets` (`src/server/services/storage/connector-secrets.ts:21-50`).
- Produces: tables `ocode_cloud_connection`, `ocode_cloud_oauth_attempt`, `ocode_cloud_one_time_code`, `ocode_cloud_callback` with the columns in spec §4.2. `ocode_cloud_connection` enforces one active row per (`userId`, `provider`).
- Produces: `cloudTokenService` methods `storeAttempt`, `consumeAttempt`, `issueCode`, `redeemCode`, `saveConnection`, `getConnection`, `listConnections`, `revokeConnection` (by user + provider), `updateAccessToken`, `registerCallback`, `getCallback`. `consumeAttempt` and `redeemCode` are single conditional UPDATEs; `redeemCode` checks the ~60 s expiry and consumes only once. `listConnections` returns `{provider, connectionId, rootPath, expiresAt}` and no token fields.

- [ ] **Step 1: Write the failing service tests**

Use `bun:test` and the repo's existing DB test helper.

```ts
it("consumes an attempt only once", async () => {
  await svc.storeAttempt({ stateHash: "h", userId: "u", provider: "dropbox",
    connectorId: "c", clientState: "cs", verifier: "v", expiresAt: soon });
  expect(await svc.consumeAttempt("h")).not.toBeNull();
  expect(await svc.consumeAttempt("h")).toBeNull();
});
it("redeems a one-time code at most once and rejects expiry", async () => {
  const code = await svc.issueCode({ userId: "u", connectionId: "c", clientState: "cs",
    challenge: "ch", expiresAt: in60s });
  expect(await svc.redeemCode(code)).not.toBeNull();
  expect(await svc.redeemCode(code)).toBeNull();
});
it("keeps one active connection per user and provider", async () => {
  const a = await svc.saveConnection({ userId: "u", provider: "dropbox" /* ... */ });
  const b = await svc.saveConnection({ userId: "u", provider: "dropbox" /* ... */ });
  expect((await svc.listConnections("u")).map(c => c.connectionId)).toEqual([b.connectionId]);
});
it("isolates by user and hides revoked connections", async () => { /* u1 vs u2; revoke; getConnection null */ });
it("registers and overwrites a callback per install", async () => { /* two installs, same user */ });
```

- [ ] **Step 2: Run them and confirm they fail**

Run: `cd /Users/james/www/kakiit && bun run test:file src/server/services/ocode-cloud/token-service.test.ts`
Expected: FAIL — modules/tables do not exist.

- [ ] **Step 3: Create the schema and generate the migration**

Define the four tables in `src/server/db/schema-ocode-cloud.ts`. Match the ocode neighbor tables (keyed by `userId`, no RLS). Token material only in sealed envelope columns. Add a uniqueness constraint for one active connection per (`userId`, `provider`). Generate with `bun run db:generate`; keep the SQL append-only; run against a fresh DB to prove the chain.

- [ ] **Step 4: Implement `cloudTokenService`**

Implement all eleven methods. `consumeAttempt`/`redeemCode` use one conditional UPDATE on `consumedAt IS NULL` (plus expiry for codes). Seal tokens with the repo's envelope crypto. `saveConnection` revokes the prior active row for that (`userId`, `provider`) in the same operation.

- [ ] **Step 5: Run the tests and confirm they pass**

Run: same command as Step 2.
Expected: PASS.

---

### Task 2: kakiit `/api/ocode/cloud` routes

**Risk:** high — auth boundary, redirect safety, one-time code, cross-user access.

**Files:**
- Create (kakiit): `src/server/api/routes/ocode.cloud.ts`
- Test (kakiit): `src/server/api/routes/ocode.cloud.test.ts`
- Modify (kakiit): `src/server/api/index.ts` (mount near `:748-762`)

**Interfaces:**
- Consumes: `requireSyncToken` (`src/server/api/lib/ocode-sync-auth.ts:25-42`), `cloudTokenService` (Task 1), Dropbox OAuth pattern (`src/server/services/storage/oauth-service.ts:75-259`), `DropboxAdapter` (`src/server/services/storage/dropbox/dropbox-adapter.ts:241`).
- Produces: the seven routes in the Wire Contract.

- [ ] **Step 1: Write the failing route tests**

Mount the sub-router on a fresh `Hono`; mock `requireSyncToken` and the provider service with `mock.module`. Do not import `api/index.ts`.

```ts
it("ignores a client-supplied redirect_uri", async () => {
  const res = await app.request("/ocode/cloud/oauth/start", { method: "POST",
    headers: bearer, body: JSON.stringify({ provider: "dropbox", connector_id: "c",
      client_state: "s", code_challenge: "ch", redirect_uri: "https://evil.example" }) });
  expect(res.status).toBe(400);
  expect(startProviderOAuth).not.toHaveBeenCalled();
});
it("rejects a reused one-time code", async () => {
  expect((await redeem(code)).status).toBe(200);
  expect((await redeem(code)).status).toBe(400);
});
it("rejects another user's connection_id on refresh and disconnect", async () => { /* 404 both */ });
it("401s every route without a sync token", async () => { /* loop all seven routes */ });
```

- [ ] **Step 2: Run them and confirm they fail**

Run: `cd /Users/james/www/kakiit && bun run test:file src/server/api/routes/ocode.cloud.test.ts`
Expected: FAIL — routes not mounted.

- [ ] **Step 3: Confirm the Dropbox adapter exposes its refresh token**

Read `DropboxAdapter` (`src/server/services/storage/dropbox/dropbox-adapter.ts:241`) and its `refresh` path. Add an explicit test asserting the refresh token can be returned for kakiit to custodian. Extend the adapter's contract if it cannot; do not proceed to OAuth wiring until this passes.

- [ ] **Step 4: Implement the route group**

Use `Hono<SyncEnv>` + `requireSyncToken` on every route. `PUT /callback` allows loopback http, requires https for non-loopback hosts, and overwrites the install's callback. `GET /oauth/callback` carries no Bearer: load `userId` from the sealed attempt. Start with PKCE + `provider_state` hash; redeem via `redeemCode` with verifier and Bearer. `DELETE /connections/:id` revokes by user + provider.

- [ ] **Step 5: Mount the group**

In `src/server/api/index.ts`, mount `ocode.cloud` under `/ocode/cloud` alongside the other ocode groups. Do not add a `/ocode/*` catch-all.

- [ ] **Step 6: Run the tests and confirm they pass**

Run: same command as Step 2.
Expected: PASS.

---

### Task 3: ocode raw-key envelope, native keychain detection, and v2 file

**Risk:** high — crypto format, key custody, migration-adjacent schema changes.

**Files:**
- Modify (ocode): `internal/keychain/keychain.go`
- Modify (ocode): `internal/encryption/envelope.go`, `internal/encryption/envelope_test.go`
- Create (ocode): `internal/cloudkeystore/keystore.go`, `internal/cloudkeystore/keystore_test.go`
- Modify (ocode): `internal/config/storage_connectors.go`, `internal/config/storage_connectors_test.go`

**Interfaces:**
- Consumes: `paths.GlobalDataDir()`, `internal/keychain.newPlatformStore` (via the new exported `NativeAvailable()`).
- Produces: `keychain.NativeAvailable() bool`.
- Produces: `encryption.SealWithKey(plaintext []byte, key []byte) (string, error)` and `encryption.OpenWithKey(envelope string, key []byte) ([]byte, error)` using prefix `ENCv2:raw:` and no KDF.
- Produces: `cloudkeystore.Resolve() (key []byte, persist bool, err error)` with injectable package vars `nativeAvailable` and `keyFilePath` so tests can force both failure modes. `persist=false` means memory-only.
- Produces: `config.StorageConnectorsFileV2` with `Version`, `Central{ConnectorID:"kakiit", Mode:"hub"|"local", Connections map[provider]ConnectionMeta}`, `CredentialsEnvelope`; plus `LoadV2(projectRoot string)` and `SaveV2(projectRoot string, f *StorageConnectorsFileV2) error`. **Do not put v1→v2 migration here** — `cloudconnect` imports `config`, and migration needs the old keychain secrets, so migration belongs in Task 4a.

- [ ] **Step 1: Write the failing tests**

```go
func TestSealWithKeyRoundTrip(t *testing.T) {
    sealed, err := SealWithKey([]byte("secret"), key32)
    if err != nil || !strings.HasPrefix(sealed, "ENCv2:raw:") { t.Fatal(sealed, err) }
    got, err := OpenWithKey(sealed, key32)
    if err != nil || string(got) != "secret" { t.Fatal(string(got), err) }
}
func TestENCv1StillDecrypts(t *testing.T) { /* fixed ENCv1 vector via DecryptJSON */ }
func TestResolveNeverUsesMachineIDFallback(t *testing.T) {
    // override nativeAvailable=false and keyFilePath to an unwritable path
    _, persist, _ := Resolve()
    if persist { t.Fatal("must be memory-only") }
}
func TestNativeAvailableDetectsPlatformStore(t *testing.T) { /* in internal/keychain */ }
func TestV2FileRoundTrip(t *testing.T) { /* LoadV2/SaveV2 with a hub connection; no RefreshToken */ }
```

- [ ] **Step 2: Run them and confirm they fail**

Run: `cd /Users/james/www/ocode && go test ./internal/keychain ./internal/encryption ./internal/cloudkeystore ./internal/config -run 'TestSealWithKeyRoundTrip|TestENCv1StillDecrypts|TestResolveNeverUsesMachineIDFallback|TestNativeAvailableDetectsPlatformStore|TestV2FileRoundTrip'`
Expected: FAIL — symbols do not exist.

- [ ] **Step 3: Export native keychain detection**

Add `NativeAvailable() bool` to `internal/keychain/keychain.go`, backed by `newPlatformStore(Options{})` without creating a fallback.

- [ ] **Step 4: Add the raw-key envelope**

Add `SealWithKey`/`OpenWithKey` to `internal/encryption/envelope.go`: AES-GCM over the supplied 32-byte key, `ENCv2:raw:` prefix, no Argon2id. Leave `EncryptJSON`/`DecryptJSON` and all `ENCv1` vectors unchanged.

- [ ] **Step 5: Add the keystore resolver**

Create `internal/cloudkeystore/keystore.go`. Call `keychain.NativeAvailable()`; never accept `keychain.New()`'s fallback. Fall back to a 0600 key file under `<GlobalDataDir>/cloud-connectors/key`, creating the parent dir first. Otherwise return `persist=false`. Expose `nativeAvailable`/`keyFilePath` as injectable package vars.

- [ ] **Step 6: Add the v2 file**

Define `StorageConnectorsFileV2` in `internal/config/storage_connectors.go`; keep the v1 reader exported for Task 4a. `SaveV2` writes atomically with 0600. No migration in this task.

- [ ] **Step 7: Run the tests and confirm they pass**

Run: `cd /Users/james/www/ocode && go test ./internal/keychain ./internal/encryption ./internal/cloudkeystore ./internal/config`
Expected: PASS.

---

### Task 4a: ocode hub client, `TokenSource`, and v1→v2 migration

**Risk:** high — external client contract, concurrency, migration of existing secrets.

**Files:**
- Create (ocode): `internal/cloudhub/client.go`, `internal/cloudhub/client_test.go`
- Modify (ocode): `internal/cloudconnect/manager.go`, `internal/cloudconnect/migration_test.go`

**Interfaces:**
- Consumes: `sync.DefaultBaseURL`/`ResolveBaseURL` (`internal/sync/sync.go:79-95`), sync token at `<GlobalDataDir>/sync/token`, the Wire Contract, `config.LoadV2/SaveV2` and the v1 reader (Task 3).
- Produces: `cloudhub.Client` with `RegisterCallback(ctx, installID, callbackURL string) error`, `StartOAuth(ctx, provider, connectorID, clientState, challenge string) (authURL string, err error)`, `Redeem(ctx, code, verifier, clientState string) (TokenBundle, error)`, `ListConnections(ctx) ([]ConnectionMeta, error)`, `Disconnect(ctx, connectionID string) error`, `RefreshToken(ctx, connectionID string) (accessToken string, expiresAt time.Time, err error)`. `TokenBundle` = `{ConnectionID, AccessToken, ExpiresAt}`; `ConnectionMeta` = `{Provider, ConnectionID, RootPath, ExpiresAt}`.
- Produces: `cloudconnect.TokenSource` with `Token(ctx, provider string) (string, time.Time, error)`; hub and legacy implementations. Hub refresh is single-flight per connection (mutex). `cloudconnect.MigrateV1ToV2(projectRoot string) error` lives here.

- [ ] **Step 1: Write the failing client and migration tests**

```go
func TestHubClientSendsBearerAndSyncURL(t *testing.T) { /* httptest server; assert headers + path */ }
func TestHubClientMapsNon2xxWithoutLoggingToken(t *testing.T) { /* 502 typed error */ }
func TestMigrateV1ToV2KeepsLegacyFields(t *testing.T) {
    // use setHomeTree(t, home); seed v1 + old keychain blob
    // assert Mode=="local", root_path/max_cache_bytes preserved,
    // RefreshToken only in legacy credentials, old blob untouched
}
func TestTokenSourceSingleFlight(t *testing.T) {
    // 10 concurrent Token() calls; assert mock RefreshToken called once
}
```

- [ ] **Step 2: Run them and confirm they fail**

Run: `cd /Users/james/www/ocode && go test ./internal/cloudhub ./internal/cloudconnect -run 'TestHubClient|TestMigrateV1ToV2|TestTokenSourceSingleFlight'`
Expected: FAIL — symbols do not exist.

- [ ] **Step 3: Build the hub client**

Create `internal/cloudhub/client.go` against the Wire Contract. Resolve base URL from `sync_url` only; attach the sync Bearer from `<GlobalDataDir>/sync/token`; no other backend fallback. Map non-2xx to typed errors without logging the token.

- [ ] **Step 4: Add `TokenSource`, single-flight, and migration to `cloudconnect`**

Define the interface in `internal/cloudconnect/manager.go`. Wrap hub refresh with a per-connection mutex. Move v1→v2 migration here (not in `config`) because `cloudconnect` already owns the old keychain blob and `cloudconnect-secrets.enc`; reuse `config`'s v1 reader and v2 writer. Ensure the new path never reads or writes keychain account `_all`.

- [ ] **Step 5: Run the tests and confirm they pass**

Run: `cd /Users/james/www/ocode && go test ./internal/cloudhub ./internal/cloudconnect`
Expected: PASS.

---

### Task 4b: ocode hub handlers and project threading

**Risk:** high — auth/security, refactor of 8 untested call sites, cross-project scoping.

**Files:**
- Create (ocode): `internal/server/handler_cloudconnect_kakiit.go`, `internal/server/handler_cloudconnect_kakiit_test.go`
- Modify (ocode): `internal/server/handler.go`, `internal/server/server.go`

**Interfaces:**
- Consumes: `cloudhub.Client` and `cloudconnect.TokenSource`/`MigrateV1ToV2` (Task 4a), `cloudkeystore.Resolve` and `config.LoadV2/SaveV2` (Task 3).
- Produces: ocode routes `/api/cloud/oauth/kakiit/start`, `/api/cloud/oauth/kakiit/return`, `/api/cloud/oauth/kakiit/callback/register`, `/api/cloud/connections`, `/api/cloud/oauth/kakiit/disconnect`, `/api/cloud/token/refresh` — all taking `project` (and `host` for remote).

- [ ] **Step 1: Write the failing handler tests**

Use `httptest` and stub `cloudhub.Client`. Use `setHomeTree(t, home)` for any HOME override.

```go
func TestKakiitStartRejectsProjectOutsideAllowedRoots(t *testing.T) { /* 400 */ }
func TestKakiitReturnRejectsMismatchedClientState(t *testing.T) { /* 400, no file written */ }
func TestKakiitRefreshErrorIsExplicitWhenHubUnreachable(t *testing.T) {
    // stub RefreshToken error; assert 502 names hub failure, no stale token used
}
func TestKakiitFirstUseFetchesFromHubInNewProject(t *testing.T) {
    // no local file; mock ListConnections + RefreshToken; assert V2 file written for that root
}
func TestKakiitDisconnectClearsOtherProjectCopy(t *testing.T) {
    // revoke centrally; next refresh from a second root errors and clears its V2 credentials
}
```

- [ ] **Step 2: Run them and confirm they fail**

Run: `cd /Users/james/www/ocode && go test ./internal/server -run 'TestKakiit'`
Expected: FAIL — handlers do not exist.

- [ ] **Step 3: Replace the single startup manager with a per-root cache**

In `internal/server/handler.go`, enumerate the 8 non-test `h.cloudManager()` call sites and the `NewManager` construction. Remove the startup `os.Getwd()` binding; resolve the project root from the validated `project` param and cache managers by root. Ensure no path writes keychain `_all`.

- [ ] **Step 4: Add the handlers and routes**

In `internal/server/handler_cloudconnect_kakiit.go`, implement start, fixed return callback, callback registration, connections list, disconnect, and refresh. Validate `project` against `allowedProjectRoots()` on every request; thread `host` for remote projects. Bind `project + host + nonce` into `client_state`; keep the PKCE verifier in memory keyed by that state with a short TTL. Never put a token in a response body. Register routes in `internal/server/server.go`.

- [ ] **Step 5: Run the tests and confirm they pass**

Run: `cd /Users/james/www/ocode && go test ./internal/server`
Expected: PASS.

---

### Task 5: Settings UI refresh

**Risk:** standard — presentation and API wiring over the handlers already built.

**Files:**
- Modify (ocode): `web/src/api/client.ts` (cloud methods near `:5216-5241`)
- Modify (ocode): `web/src/components/Settings/CloudConnectorsForm.tsx`
- Test (ocode): `web/src/components/Settings/CloudConnectorsForm.test.tsx`

**Interfaces:**
- Consumes: Task 4b ocode routes; `Badge`/`Separator`/`Select`/`Tooltip`/`Button` from `web/src/components/ui`; lucide icons.
- Produces: no interfaces for later tasks.

- [ ] **Step 1: Write the failing component test**

```tsx
it("shows the kakiit CTA when disconnected and hides it when local-only", () => { /* ... */ });
it("renders provider rows sorted by provider name with status badges", () => { /* ... */ });
it("opens a confirm dialog before disconnecting, stating revocation is global", () => { /* ... */ });
it("threads project and host into the hub start call", () => { /* assert query params */ });
```

- [ ] **Step 2: Run it and confirm it fails**

Run: `cd /Users/james/www/ocode/web && npx vitest run src/components/Settings/CloudConnectorsForm.test.tsx`
Expected: FAIL — assertions unmet.

- [ ] **Step 3: Add client methods and rebuild the form**

Add hub methods to `web/src/api/client.ts` with explicit `project`/`host` parameters. Rewrite `CloudConnectorsForm.tsx` per spec §7: status header + CTA, sorted provider rows with icons and `Badge` statuses, Browse/Refresh/Disconnect actions, confirm-on-disconnect, a collapsible legacy local-only section, and an icon empty state. Use semantic color tokens; no new npm dependency and no `card.tsx`.

- [ ] **Step 4: Run the test and the typecheck and confirm both pass**

Run: `cd /Users/james/www/ocode/web && npx vitest run src/components/Settings/CloudConnectorsForm.test.tsx && npm run typecheck`
Expected: PASS.

---

## Full verification (after all tasks)

Run once, in order:

1. `cd /Users/james/www/kakiit && bun run test:file src/server/services/ocode-cloud src/server/api/routes/ocode.cloud.test.ts`
2. `cd /Users/james/www/ocode && go test ./internal/keychain ./internal/encryption ./internal/cloudkeystore ./internal/config ./internal/cloudconnect ./internal/cloudhub ./internal/server`
3. `cd /Users/james/www/ocode/web && npx vitest run && npm run typecheck && npm run build`

Expected: all green.

## Deferred to separate plans (intentional, not gaps)

- **Slice 1.5 — Google Drive adapter:** hard external gate (verified Google OAuth app + `drive.file` scope). It cannot finish inside this plan, so it gets its own plan when the gate clears. Spec §4.3, §10.
- **Slice 2 — OneDrive and user-OAuth GCS.** Spec §2 decision 7.
- **Local-only providers — S3 and service-account GCS** keep static keys in the local envelope. Spec §2 decision 7.

Each deferred slice reuses the `cloudTokenService` and `TokenSource` interfaces produced here.
