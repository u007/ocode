# Part 06 — Server handlers, routes, per-surface grants

## Files

- Create: `internal/server/handler_vault.go`
- Create: `internal/server/handler_vault_test.go`
- Modify: `internal/server/handler.go` — add `vault` + `vaultGrants` fields,
  init in `NewHandler`, `SetVault` seam.
- Modify: `internal/server/server.go` — register `/api/vault/*` routes;
  `handleBrowseRevoke` calls `VaultLockSurface`.

## Interfaces

- **Consumes:** `internal/vault` (Parts 01–05).
- **Produces (on `*Handler`):**
  - `HandleVaultStatus`, `HandleVaultInit`, `HandleVaultUnlock`,
    `HandleVaultLock`, `HandleVaultList`, `HandleVaultReveal`,
    `HandleVaultCreate`, `HandleVaultUpdate`, `HandleVaultDelete`,
    `HandleVaultMatch`, `HandleVaultChangeMaster`, `HandleVaultGenerate`.
  - `VaultLockSurface(surface string)`.
  - `SetVault(v *vault.Vault)` (test seam).
  - Internal helpers `vaultGranted`, `vaultGrant`, `vaultLockAll`,
    `vaultAnyGrant`, `vaultRef`, `vaultErr`, `vaultStore`, `atoiDefault`.

## Routes (registered in `registerRoutes`, wrapped in `authMiddleware`)

- `GET /api/vault/status?surface=` → `{exists, unlocked}` (unlocked = vault
  unlocked **and** the surface granted).
- `POST /api/vault/init` `{master, surface}` → 200, 409 if exists.
- `POST /api/vault/unlock` `{master, surface}` → 200, 401 wrong master.
- `POST /api/vault/lock` `{surface?}` → 204; empty surface locks **all**.
- `GET /api/vault/items?surface=&sort=&limit=&offset=` → `{items, total}`.
- `GET /api/vault/items/{id}/reveal?surface=` → full item.
- `POST /api/vault/items?surface=` → created item.
- `PUT /api/vault/items/{id}?surface=` → updated item.
- `DELETE /api/vault/items/{id}?surface=` → 204.
- `POST /api/vault/match` `{url, surface}` → `{items}`.
- `POST /api/vault/change-master` `{old, new}` → 204 (needs any live grant).
- `POST /api/vault/generate` `{length, upper, digits, symbols}` → `{password}`.

## Test cases to write

- `TestVaultInitStatusAndLocked403` — pre-init status is `{false,false}`; a
  locked list call is 403; init succeeds; a second init is 409.
- `TestVaultUnlockWrongMaster401` — wrong master is 401 and stays locked.
- `TestVaultCRUDAndReveal` — create returns the item; reveal returns the
  password; delete is 204.
- `TestVaultLockAllClearsSurfaces` — grants for two surfaces both drop on a
  surface-less `lock`.
- `TestVaultLockSurface` — `VaultLockSurface` drops exactly that surface.
- `TestVaultGenerate` — returns a non-empty `password`.
- `TestVaultListRejectsMalformedPagination` — `limit=abc`, `limit=-1`,
  `limit=1001`, `offset=abc`, and `sort=username` each return **400** (not a
  coerced default).
- `TestVaultRevealUnknownId404` — an unknown id returns 404.

## Implementation notes

- **Explicit validation, no fallbacks.** `HandleVaultList` parses `limit` and
  `offset` with `strconv.Atoi`; a present-but-non-numeric value, a negative
  value, or `limit > 1000` returns 400. When `limit` is absent, apply the
  documented default 200 **explicitly** (not an optional-chaining fallback).
  `sort` must be empty or `site`; any other value returns 400 (never silently
  ignored). Do not use `??`/optional-chaining to swallow supplied values.
- `HandleVaultStatus`/`List`/`Reveal`/`Create`/`Update`/`Delete`/`Match`
  require `vaultGranted(surface)`; otherwise 403. `change-master` requires
  `vaultAnyGrant()`.
- `vaultErr` maps sentinels: `ErrWrongMaster`→401, `ErrLocked`→403,
  `ErrNotFound`→404, `ErrExists`→409, else 500. Every caught error is logged
  with the operation (`vault init`, `vault unlock`, `vault persist`, …).
- `NewHandler` resolves the path via `paths.GlobalDataDir()` +
  `browse/vault.json`; if the data dir fails, log and leave `vault` nil (handlers
  then 500 "vault unavailable"). `vaultStore("")` returns nil.
- `handleBrowseRevoke` calls `s.handler.VaultLockSurface(req.StateKey)` after
  `s.browse.Revoke`.
- Never log request bodies (master passwords / secrets).

## Steps

- [ ] Write `handler_vault_test.go` with the cases above.
- [ ] Run `go test ./internal/server/ -run TestVault -v` — expect FAIL.
- [ ] Add the `Handler` fields + `NewHandler` init + `SetVault`; write `handler_vault.go`; register routes; patch `handleBrowseRevoke`.
- [ ] Run `go test ./internal/server/ -run TestVault -v && go build ./... && go vet ./internal/server/` — expect PASS/clean.
- [ ] Mutation-verify: remove the surface check and the pagination 400, confirm the 403 and malformed-pagination tests fail, restore.
- [ ] Commit.

## Verify

```bash
cd /Users/james/www/ocode
go test ./internal/server/ -run TestVault -count=1 -v
go build ./... && go vet ./internal/vault/ ./internal/server/
```

## Commit

```bash
git add internal/server/handler_vault.go internal/server/handler_vault_test.go internal/server/handler.go internal/server/server.go
git commit -m "feat(vault): expose /api/vault endpoints with per-surface grants"
```
