---
type: Plan
timestamp: 2026-09-24T07:50:33Z
---
# Part 03 — Vault public API

## Files

- Create: `internal/vault/vault.go`
- Test: `internal/vault/vault_test.go`

## Interfaces

- **Consumes:** Parts 01–02 (`seal`, `open`, `wrapKey`, `unwrapKey`,
  `deriveKEK`, `readVaultFile`, `writeVaultFile`, `acquireFileLock`, consts).
- **Produces:**
  - `Item{ID, Site, URL, Title, Username, Password, Notes, Created, Updated string}`.
  - `ItemMeta{ID, Site, URL, Title, Username string}`.
  - `New(path string) *Vault`.
  - `(*Vault).Exists() bool`.
  - `(*Vault).Init(master string) error` — generate DK + salt, wrap, write, unlock.
  - `(*Vault).Unlock(master string) error` — unwrap DK, decrypt items.
  - `(*Vault).Lock()` — zero the DK, drop items.
  - `(*Vault).Unlocked() bool`.
  - `(*Vault).List(sortKey string, limit, offset int) ([]ItemMeta, error)`.
  - `(*Vault).Count() int`.
  - `(*Vault).Reveal(id string) (Item, error)`.
  - `(*Vault).Create(it Item) (Item, error)`.
  - `(*Vault).Update(id string, it Item) (Item, error)`.
  - `(*Vault).Delete(id string) error`.
  - `(*Vault).ChangeMaster(old, new string) error` — re-wraps the DK only.

## Test cases to write

- `TestInitUnlockRoundTrip` — init unlocks; create; lock relocks; unlock re-reads the item.
- `TestUnlockWrongMaster` — wrong password returns `ErrWrongMaster` and stays locked.
- `TestLockedOperationsBlocked` — `List`, `Reveal`, `Create` return `ErrLocked`.
- `TestAADBindingRejectsMovedBlob` — swapping two items' blobs on disk makes `Unlock` return `ErrCorrupt`.
- `TestListSortAndPagination` — case-insensitive site-then-username order; page of 2 from offset 1; offset past end ⇒ empty; huge limit ⇒ all.
- `TestChangeMasterRewrapsOnly` — after `ChangeMaster`, item blobs are byte-identical, `wrapped_key` differs, old password fails, new password works.
- `TestPersistMergeKeepsForeignItems` — two `Vault` instances over one file: item created by each is visible to a fresh reader.
- `TestChangeMasterRequiresUnlocked` — locked vault returns `ErrLocked`.
- `TestFileNeverContainsPlaintextPassword` — the raw file does not contain the password, username, or site strings.

## Implementation notes

- `Init` writes the envelope with `kdf.algo="argon2id"` and the consts; it
  refuses when the file exists (`ErrExists`).
- `Unlock` base64-decodes the salt, derives the KEK, unwraps the DK; any
  failure is `ErrWrongMaster`. `decryptItems` seals each item under the DK with
  AAD = its `id`; a decode/`open`/unmarshal failure returns `ErrCorrupt`
  **wrapped with the item id**, and is logged (operation = `decrypt item`).
- `List` sorts case-insensitively by site then username; explicit bounds —
  `offset<0` ⇒ 0; `limit<=0` ⇒ documented default 200; `limit>1000` ⇒ 1000;
  `offset>=len` ⇒ empty slice. `Count` returns 0 when locked.
- `Create`/`Update`/`Delete`/`ChangeMaster` mutate in memory then call
  `persistLocked`.
- **`persistLocked` is the concurrency core:** acquire the OS file lock, read
  the current file, decrypt its items, drop ids in the in-memory `deleted`
  set, overlay this process's items (appending ids not already present),
  re-seal every item under the DK with its id as AAD, write atomically,
  refresh `items`, clear `deleted`, release. A failure logs the operation
  (`persist vault`) before returning.
- `ChangeMaster` verifies the old password against the file's `wrapped_key`,
  derives a new salt+KEK, re-wraps the same DK, and rewrites only
  `kdf`+`wrapped_key` under the file lock — item blobs are untouched.

## Steps

- [ ] Write `vault_test.go` with the cases above.
- [ ] Run `go test ./internal/vault/ -run 'TestInit|TestUnlockWrong|TestLockedOps|TestAAD|TestList|TestChangeMaster|TestPersist|TestFileNever' -v` — expect FAIL (`undefined: New`).
- [ ] Write `vault.go`.
- [ ] Run `go test ./internal/vault/ -v` — expect PASS.
- [ ] Mutation-verify the AAD and merge tests: temporarily remove the AAD and the foreign-item merge, confirm both tests fail, restore.
- [ ] Commit.

## Verify

```bash
cd /Users/james/www/ocode
go test ./internal/vault/ -count=1 -v
```

## Commit

```bash
git add internal/vault/vault.go internal/vault/vault_test.go
git commit -m "feat(vault): add vault API with wrapped data key and AAD-bound items"
```
