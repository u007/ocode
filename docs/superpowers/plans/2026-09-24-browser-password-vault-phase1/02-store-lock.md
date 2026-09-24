---
type: Plan
timestamp: 2026-09-24T07:50:21Z
---
# Part 02 — File store + OS file lock

## Files

- Create: `internal/vault/store.go`
- Create: `internal/vault/lock_unix.go` (build tag `!windows`)
- Create: `internal/vault/lock_windows.go` (build tag `windows`)
- Test: `internal/vault/store_test.go`

## Interfaces

- **Consumes:** sentinels from Part 01 (`ErrNoVault`).
- **Produces:**
  - `kdfParams{Algo string; Salt string; Time uint32; MemoryKiB uint32; Threads uint8}` (json tags: `algo`, `salt`, `time`, `memory_kib`, `threads`).
  - `diskItem{ID string; Blob string}` (json: `id`, `blob`).
  - `vaultFile{Version int; KDF kdfParams; WrappedKey string; Items []diskItem}` (json: `version`, `kdf`, `wrapped_key`, `items`).
  - `readVaultFile(path string) (*vaultFile, error)` — missing file ⇒ `ErrNoVault`.
  - `writeVaultFile(path string, vf *vaultFile) error` — atomic, mode `0600`.
  - `acquireFileLock(path string) (*fileLock, error)` and `(*fileLock).release()`.

## Test cases to write

- `TestWriteReadVaultFileRoundTrip` — write then read returns the same envelope.
- `TestWriteVaultFileMode0600` — the file's permission bits are exactly `0600`.
- `TestReadVaultFileMissingIsErrNoVault` — missing path returns `ErrNoVault`.
- `TestFileLockSerializes` — while one lock is held, a second `acquireFileLock`
  on the same path does not complete; it completes after `release()`.

## Implementation notes

- `writeVaultFile` marshals indented JSON, `os.MkdirAll(dir, 0700)`, writes to
  `os.CreateTemp(dir, ".vault-*.tmp")`, `Chmod(0600)`, writes, closes, then
  `os.Rename` over the target; a deferred `os.Remove` cleans the temp on error.
- `lock_unix.go` uses `golang.org/x/sys/unix` `Flock(fd, LOCK_EX)` on a
  `<path>.lock` sidecar; `release` unlocks and closes.
- `lock_windows.go` uses `windows.LockFileEx` / `UnlockFileEx` with
  `LOCKFILE_EXCLUSIVE_LOCK` on the same sidecar.
- Both lock files carry a doc comment: this lock is what makes the
  load-modify-write in Part 03 safe across ocode processes.

## Steps

- [ ] Write `store_test.go` with the cases above.
- [ ] Run `go test ./internal/vault/ -run 'TestWrite|TestRead|TestFileLock' -v` — expect FAIL (`undefined: writeVaultFile`).
- [ ] Write `store.go`, `lock_unix.go`, `lock_windows.go`.
- [ ] Run `go test ./internal/vault/ -v` — expect PASS.
- [ ] Commit.

## Verify

```bash
cd /Users/james/www/ocode
go test ./internal/vault/ -v
GOOS=windows go build ./internal/vault/    # cross-compile check for the windows lock
```

## Commit

```bash
git add internal/vault/store.go internal/vault/store_test.go internal/vault/lock_unix.go internal/vault/lock_windows.go
git commit -m "feat(vault): add encrypted file store and OS file lock"
```
