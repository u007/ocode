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

- `machineLocalConfigKeys` (line 136): `ocodeconfig.json` → `{ "terminal_shell", "extra_allowed_paths" }`.
- `stripMachineLocalConfig` (line 147, verified `func` declaration): removes those keys before sync so absolute machine-local paths (e.g. `/Users/james/www/ocode`) are not pushed where they match nothing.
- `WritePayload` (line 334) writes the frame's bytes as the whole file (`SyncDirConfig`, line 358: `merged, perr := preserveMachineLocalConfig(...)`). Without the preservation step a stripped key is erased permanently on the receiver.

## Fix — two guards, both load-bearing

Verified lines in `internal/remote/sync.go`:

- Line 194: `func preserveMachineLocalConfig(name string, content []byte, destPath string) ([]byte, error)` — reads the destination's existing file (`os.ReadFile(destPath)`) and, for each stripped key missing from the incoming content, copies the current value back (`current[k]` wins; incoming values are never overwritten here).
- Line 136: the key set (`"terminal_shell"`, `"extra_allowed_paths"`) — only these; any other `ocodeconfig.json` key is still synchronized.
- Line 358 (inside `WritePayload`): `merged, perr := preserveMachineLocalConfig(f.Name, f.Content, destPath)` runs for every `SyncDirConfig` file before writing, and a non-nil error aborts the write.

**Asymmetry is intentional:** the sender strips (its paths are meaningless on the host); the receiver preserves (its own grants must survive). A fresh host with no existing file falls through untouched (`os.IsNotExist` → write the frame as-is). A read error other than a missing file is logged (`sync: preserve machine-local keys: read ...`) and returned, so the write is aborted; an unparseable DESTINATION is treated the same way, while an unparseable FRAME keeps the destination (see below).

The destination wins for these keys because it is the only machine that can evaluate them. The function FAILS CLOSED on the DESTINATION side: a destination that cannot be read or parsed returns an error and the write is ABORTED (`refusing to overwrite <path>: ...`), because writing the frame over it is exactly the data loss this guards against. The non-error exits are a missing (fresh host) or empty destination (nothing to lose for this file) and ANY frame-side problem — empty, `null`, or not a JSON object — which keeps the destination's own bytes instead of erasing them. The frame is the SENDER's file, so keeping the host's copy loses nothing; a hard error there would abort the whole sync, and `RemoteWorkspace.Connect` treats a sync error as fatal, so one corrupt local config would brick every remote connect (the CLI's `runPrepareStages` only logs and continues, `connect.go:193` — the two paths disagreed before this). The empty frame used to take an early return BEFORE the destination was read, and `null` unmarshalled to a nil map and panicked on the merge (`assignment to entry in nil map`), failing every remote connect.

**A destination-side refusal is actionable, not opaque.** `runSyncStage` folds the remote's stderr into the error it returns (it used to discard it and return a bare `exit 1`), and `RemoteWorkspace.Connect` runs the result through `syncFailureHint` (`workspace.go`), which recognises `refusing to overwrite` and appends the host plus the remedy (`chown`/`chmod ~/.config/opencode/ocodeconfig.json`, or delete it to resync). Connect still FAILS — there is no fallback to an unmerged write or a local agent — but the 502 now names the file to fix instead of leaving the user with nothing. Regression: `TestSyncFailureHintNamesTheRemedy`, `TestSyncFailureHintLeavesOtherErrorsAlone`, `TestRunSyncStageSurfacesRemoteStderr`.

## Regression

`internal/remote/sync_machine_local_test.go`: `TestStripMachineLocalConfigRemovesExtraAllowedPaths`, `TestSyncRoundTripKeepsHostGrantAndDropsLocalPaths`, `TestWritePayloadFreshHostWritesFrameUnchanged` (round-trip: sender strips; receiver preserves host grants; local paths never reach remote; host grants survive). `internal/remote/sync_roundtrip_test.go`: `TestBuildSyncPayloadRoundTripPreservesHostGrant` (the full `BuildSyncPayload` → `WritePayload` pipeline), `TestWritePayloadDoesNotSynthesiseMissingExtraAllowedPaths` (line 107). `internal/remote/sync_failclosed_test.go`: `TestWritePayloadRefusesToOverwriteCorruptDestination`, `TestWritePayloadRefusesUnreadableDestination` (the destination is left byte-identical / not replaced when the write is refused), `TestWritePayloadEmptyFrameKeepsDestination`, `TestWritePayloadNullFrameKeepsDestination`, `TestWritePayloadCorruptFrameKeepsDestination` (an empty, `null` or unparseable frame keeps the destination's bytes instead of erasing them).

## Rollout caveat (2026-10-28)

Nothing here is verified end to end: no real reconnect was executed. The evidence is the host config's mtime/content plus the library round trip (`TestBuildSyncPayloadRoundTripPreservesHostGrant`, `TestSyncRoundTripKeepsHostGrantAndDropsLocalPaths`).

The fix is inert until the HOST runs the rebuilt binary: `remote-receive-config` is executed from `RemoteBinaryPath(ver)` on the remote, so an old host still runs the old `WritePayload`.

A NEW sender against an OLD host receiver still loses grants: the sender strips `extra_allowed_paths`, and the old receiver writes the frame as the whole file, erasing the host's list. Uploading fixes it only when an upload actually happens, and BOTH upload paths short-circuit when the versioned binary already exists: `runPrepareStages` (`internal/remote/connect.go`:136) and `EnsureBinary` (`internal/remote/provision.go`:238) call `BinaryExists` (a bare `test -x ~/.ocode/bin/<ver>/ocode`, `provision.go`:64) and skip `UploadBinary`/`ActivateAndVerify` on success. `version.Version` is a compile-time const with no ldflag injection, so **rebuilding at the same version never replaces the host's receiver**: a host that already has that version keeps running the old `WritePayload`, and its grants are erased again on every reconnect while this fix silently does nothing. The fix therefore ships behind a version bump (0.9.21 → 0.9.22): with a new version the versioned path does not exist on the host, so `runPrepareStages`/`EnsureBinary` upload and activate it BEFORE `runSyncStage` runs (`connect.go`:154-187; `workspace.go`:106). A skipped or failed activation with a successful sync is still unsafe. Tracked in TODO.md as "VERSION GATE".
