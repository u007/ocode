---
type: Gotcha
title: auth.json multi-process credential loss
description: Two ocode processes sharing auth.json each wrote their whole in-memory cache, so a key saved in one vanished; the fix is load-modify-write under a cross-process file lock, with reload-on-change readers.
resource: internal/auth/store.go
tags:
  - auth
  - credentials
  - concurrency
  - multi-process
  - oauth
timestamp: 2026-11-22T07:20:16Z
status: active
---
## Symptom

A provider key saved in one ocode process (the TUI, `ocode serve`, or the desktop app) disappears later. Nothing in that process removed it. Once two processes run at once this is routine: a background OAuth refresh in another process rewrites auth.json from its own older copy.

## Root Cause

- `auth.json` was loaded once per process into a cache (`cache` in `internal/auth/store.go`). `Set` and `Remove` changed that cache and wrote the whole cache back. A process holding an older copy therefore overwrote every key another process had added since it loaded.
- `storeMu` serialises writes only inside one process. Nothing serialised them across processes.
- The temp file had one shared name, `auth.json.tmp`, so two writers could clobber each other's temp file mid-write.
- `auth.profiles.json` (the per-profile overlays in `internal/auth/profile_store.go`) had the same shape: a whole-cache write.

## Fix (shipped)

- **Writes are load-modify-write under a cross-process lock.** `Set` and `Remove`, and the profile mutators (`SetProfileCredential`, `RemoveProfileCredential`, `DeleteProfileCredentials`, `RenameProfileCredentials`), re-read the file under the lock, change one entry, and write only when the content changed. The lock is `internal/filelock` on `<file>.lock` (`auth.json.lock`, `auth.profiles.json.lock`), taken through `withAuthFileLock`. Lock order is `storeMu` (or `profileStoreMu`), then the file lock.
- **Writes are atomic.** `writeFileAtomic` writes a temp file at mode 0600, fsyncs it, and renames it into place. The rename is retried 5 times, 20 ms apart.
- **Readers reload on change.** Each read stats the file and reloads only when the stamp moved (`ensureLoadedLocked`, `ensureProfileStoreLocked`). `CredentialVersion()` runs that check for both stores.
- **The credential version tracks real changes.** It bumps when a call wrote, or when the re-read differs from the cached state (`adoptAuthStateLocked`, `adoptProfileStateLocked`), so a peer's write that a no-op absorbed still invalidates cached clients.
- **A failed reload keeps the last good cache** (`loadFailed`, `profileLoadFailed`). Writers return the load error and write nothing.
- **A file that does not parse is never overwritten.** The read error is returned before any write.
- **No-op calls write nothing.** A `Set` identical to the stored credential, or a `Remove` of an absent provider, writes nothing, fires no hook and bumps no version.
- **internal/sync takes the same lock.** Its read-compare-write of auth.json runs through `auth.WithStoreFileLock` (`withLocalFileLock` in `internal/sync/sync.go`).

Related fixes in the same change:

- **Legacy wrapper.** An auth.json written as `{"credentials": {…}}` was read as one provider named `credentials`, and the next save dropped the real keys. `readLegacyOcodeFormat` now detects the wrapper by its shape. A wrapper whose values mix objects and non-objects is a parse error. A zero-byte file is an empty store.
- **Opencode import, once.** Opencode's own auth.json is folded into ocode's file once, under the lock, by `importLegacyOnce`. The sentinel `auth.json.legacy-imported` records it. Nothing happens when the legacy path is auth.json itself. A provider removed from ocode no longer comes back from the legacy file.
- **Refresh saves through its own store.** `CredentialStore` (the base account or one profile), `RefreshLocked` and `RefreshHook` take the store the credential was read from. The Grok plugin's `grokRefreshHook` passes it on. A profile's refresh no longer overwrites the base account's token, and the reverse.
- **Refresh re-reads under the mutex.** `RefreshLocked`, and the generic path in `refreshIfExpiring`, re-read the current credential after taking the per-provider refresh mutex, so a token another goroutine already refreshed is reused.

## Test Rule

- `internal/auth` and `internal/sync` each have a `TestMain` in `testmain_test.go`. It points HOME and the XDG and Windows variables at one package temp root, and exits non-zero before any test runs if a credential path resolves outside that root. `internal/auth` checks the AuthPath, ProfileAuthPath and legacy opencode paths. `internal/sync` checks AuthPath.
- Before this change three `internal/auth` tests wrote the developer's real auth.json. One of them removed every provider and restored them only in cleanup.
- New tests in these packages use `setHomeTree(t, home)`, never a bare `t.Setenv("HOME", …)`.

## Rules for Future Work

- Any new writer of `auth.json` or `auth.profiles.json` goes through `Set`, `Remove`, the profile mutators, or `auth.WithStoreFileLock`.
- Never write either file from the in-memory cache.
- Never make a network refresh while holding `storeMu` (or `profileStoreMu`) or the file lock.

## Known Limits

- Two processes can still both spend the same rotating refresh token. The refresh mutex is per process, so it does not serialise across processes.
- `storeMu` is held while waiting for the file lock. A stuck peer that holds the file lock stalls this process's credential access for up to the filelock timeout (10 s).
- A crash between the legacy import's auth.json write and its sentinel re-imports the legacy file on the next start. That can bring back a provider removed in that window.
- A rewrite normalises opencode's `access`, `refresh` and `expires` fields to ocode's names (`expires` milliseconds become seconds) and drops fields ocode does not know.

## Verified

- Re-run on 2026-11-22: `go test -race ./internal/auth/ ./internal/sync/ ./internal/plugin/...` passes. The real auth.json has the same SHA-256 before and after the run.
- The change record says the cross-process cases fail on the code before this fix and that two independent reviews were done. Neither was repeated when this page was written.
- Multi-writer tests use goroutines and direct lock holders, not separate OS processes.

## Related

- [Shared tabs.json is written by every ocode server process](shared-tabs-json-multi-writer-clobber.md): the same bug class in tabs.json, a per-process cache and a whole-file write.
- [Data storage layout](../concepts/data-storage-layout.md): where auth.json and auth.profiles.json live.
