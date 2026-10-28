---
type: Gotcha
title: Remote config sync clobbers `extra_allowed_paths`
description: >
  `BuildSyncPayload` (internal/remote/sync.go) pushes `ocodeconfig.json` to the
  remote host. Before the 2026-10-28 fix the local `extra_allowed_paths` was pushed verbatim (only `terminal_shell` was stripped), so macOS `/Users/...` paths arrived at a Linux host where they matched nothing; `WritePayload`'s full-file replacement also erased any grants the host had made locally. Two guards: sender strips (`stripMachineLocalConfig`); receiver preserves (`preserveMachineLocalConfig`) before writing.
  
resource: internal/remote/sync.go
tags:
  - remote
  - sync
  - config
  - ocodeconfig
  - extra_allowed_paths
  - machine-local
timestamp: 2026-10-28T00:00:00Z
---
# Remote config sync clobbers `extra_allowed_paths` (gotcha)

## Symptom

A remote session (SSH/WSL) whose user granted extra directories (`extra_allowed_paths` in `ocodeconfig.json`) starts asking for those paths after every reconnect/config sync. The host's `~/.config/opencode/ocodeconfig.json` loses the key; its mtime matches the config-sync event.

Verified: the stripped key contained only macOS `/Users/...` entries; the Linux host's `/home/james/www/aimsai2` grant disappeared after the sync wrote the frame (full-file replacement) without it.

## Cause (`internal/remote/sync.go`)

- `machineLocalConfigKeys` (line 130): `ocodeconfig.json` → `{ "terminal_shell", "extra_allowed_paths" }`.
- `stripMachineLocalConfig` (line 141, verified `func` declaration): removes those keys before sync so absolute machine-local paths (e.g. `/Users/james/www/ocode`) are not pushed where they match nothing.
- `WritePayload` (line 287) writes the frame's bytes as the whole file (`SyncDirConfig`, line 311: `content = preserveMachineLocalConfig(...)`). Without the preservation step a stripped key is erased permanently on the receiver.

## Fix — two guards, both load-bearing

Verified lines in `internal/remote/sync.go`:

- Line 179: `func preserveMachineLocalConfig(name string, content []byte, destPath string) []byte` — reads the destination's existing file (`os.ReadFile(destPath)`) and, for each stripped key missing from the incoming content, copies the current value back (`current[k]` wins; incoming values are never overwritten here).
- Line 130: the key set (`"terminal_shell"`, `"extra_allowed_paths"`) — only these; any other `ocodeconfig.json` key is still synchronized.
- Line 311 (`WritePayload`): `content = preserveMachineLocalConfig(f.Name, f.Content, destPath)` runs for every `SyncDirConfig` file before writing.

**Asymmetry is intentional:** the sender strips (its paths are meaningless on the host); the receiver preserves (its own grants must survive). A fresh host with no existing file falls through untouched (`os.ReadFile` error → return content unchanged).

The destination wins for these keys because it is the only machine that can evaluate them. A missing destination (new host) or an unparseable one leaves the content unchanged — failing a sync over a foreign config is not worth it; the previous (pre-fix) behaviour — write as-is, erasing — was worse.

## Regression

`internal/remote/sync_machine_local_test.go`: `TestStripMachineLocalConfigRemovesExtraAllowedPaths`, `TestSyncRoundTripKeepsHostGrantAndDropsLocalPaths` (round-trip: sender strips; receiver preserves host grants; local paths never reach remote; host grants survive) (verified: `extra_allowed_paths` removed, `models` and `terminal_font_size` preserved), plus preservation/rebuild tests.

Do not claim a rebuild/restart was performed — none is required for this library-level fix; `WritePayload` takes effect on the next remote-config receive.
