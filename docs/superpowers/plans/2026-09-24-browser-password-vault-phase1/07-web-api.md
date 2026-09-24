---
type: Plan
timestamp: 2026-09-24T07:51:20Z
---
# Part 07 — Web API client + types

## Files

- Modify: `web/src/api/types.ts`
- Modify: `web/src/api/client.ts`
- Test: `web/src/api/client.vault.test.ts`

## Interfaces

- **Consumes:** `fetchJSON` in `web/src/api/client.ts`.
- **Produces (on the `api` object):**
  - `vaultStatus(surface: string)`
  - `vaultInit(master: string, surface: string)`
  - `vaultUnlock(master: string, surface: string)`
  - `vaultLock(surface?: string)`
  - `vaultList(surface: string, opts: { sort?: string; limit?: number; offset?: number })`
  - `vaultReveal(id: string, surface: string)`
  - `vaultCreate(item, surface: string)`
  - `vaultUpdate(id: string, item, surface: string)`
  - `vaultDelete(id: string, surface: string)`
  - `vaultMatch(url: string, surface: string)`
  - `vaultChangeMaster(oldMaster: string, newMaster: string)`
  - `vaultGenerate(opts)`
- **Produces (types):** `VaultItemMeta`, `VaultItem extends VaultItemMeta`,
  `VaultStatus{exists,unlocked}`, `VaultGenOptions{length,upper,digits,symbols}`.

## Test cases to write

- `vault api status encodes the surface` — `api.vaultStatus("settings")` hits
  `/api/vault/status?surface=settings` and returns the parsed body.
- `vault api create posts the item with the surface query` — method `POST`,
  URL contains `/api/vault/items?surface=settings`.
- `vault api list forwards sort/limit/offset` — a supplied `sort`, `limit`, and
  `offset` all appear in the query string (none silently dropped).

## Implementation notes

- Use `fetchJSON` with the exact query strings; `encodeURIComponent` the
  surface and id.
- **No silent coercion.** `vaultList` must include a supplied `sort`/`limit`/
  `offset` in the query. Do not use optional chaining in a way that drops a
  value the caller passed; if a value is supplied, it is sent.
- `vaultLock` with no argument sends `{}` (server locks all surfaces).
- Keep types in `types.ts`; import them into `client.ts` rather than inline
  `import(...)` types if the file already imports sibling types.

## Steps

- [ ] Write `client.vault.test.ts` with the cases above.
- [ ] Run `cd web && npx vitest run src/api/client.vault.test.ts` — expect FAIL.
- [ ] Add the types and the `api.vault*` methods.
- [ ] Run `cd web && npx vitest run src/api/client.vault.test.ts && npx tsgo --noEmit` — expect PASS + clean.
- [ ] Commit.

## Verify

```bash
cd /Users/james/www/ocode/web
npx vitest run src/api/client.vault.test.ts
npx tsgo --noEmit
```

## Commit

```bash
git add web/src/api/types.ts web/src/api/client.ts web/src/api/client.vault.test.ts
git commit -m "feat(web): add vault API client methods"
```
