---
type: Gotcha
title: Local embed server (llama-server) fails with "Exec format error"
description: When the cached llama-server binary's architecture doesn't match the host, the local embed server fails to spawn with "Exec format error" and discovery/embeddings break. Diagnose with /localserver status, fix with /localserver fix.
tags:
  - discovery
  - embeddings
  - llama-server
  - localserver
  - diagnosis
timestamp: 2026-10-20T05:01:09Z
---
## Symptom

The local embed server fails to spawn, discovery reports the embedding server as
unavailable, and the log shows the child process dying with `Exec format error`.

## Cause

ocode downloads `llama-server` from
`github.com/ggml-org/llama.cpp` releases and caches it under:

```
<GlobalDataDir>/discovery/local-<os>-<arch>/llama-b9777/llama-server
```

(`<GlobalDataDir>` is `~/.local/share/opencode` on macOS/Linux.)

The cached binary can end up with the wrong architecture — e.g. a darwin-arm64
binary sitting on an x86_64 host (a cache dir migrated/copied across machines),
or a partial/corrupt download. When the process is spawned, the OS kernel refuses
to run it and reports `Exec format error`, so the server never comes up and
embeddings are unavailable.

## Diagnosis

Run:

```
/localserver status
```

This reports the platform, cache dir, archive URL/SHA, extraction marker, and —
critically — probes the binary with `--version` under a 5s timeout
(`internal/discovery/localserver_repair.go`). On a wrong-arch binary the output is
`Binary execution: FAILED: ... exec format error`, with a hint pointing at the fix.
A valid binary reports `Binary execution: OK` plus its version.

## Fix

Run:

```
/localserver fix
```

`ReinstallLocalServer` (`internal/discovery/localserver_repair.go`) re-downloads
and re-extracts the pinned archive into a scratch directory
(`.reinstall-stage-*` inside the cache dir) and only then swaps it over the
installed tree (`installStaged`): the old `llama-*` directories and markers are
moved aside, the new ones renamed in, and everything is rolled back if any
rename fails. A failed download (offline, SHA mismatch) therefore leaves the
previous install untouched — `/localserver fix` can never turn a degraded
install into no install. `EnsureArtifact` (`internal/discovery/download.go`)
verifies SHA256, so a partial/corrupt download is also repaired this way. Large
raw artifacts (the `.gguf` model) are not re-fetched; they are re-verified after
the swap.

The reinstall holds `localMu` (a concurrent in-process `EnsureLocalServer` waits
rather than spawning from a half-swapped tree) and the cross-process embed start
lock; if another ocode is mid-spawn it refuses with "another ocode process is
starting the local embed server". On Windows a running `llama-server.exe` cannot
be renamed, which surfaces as an error before anything is lost.

In the TUI, `/localserver status` and `path` run on a `tea.Cmd` (status execs
`llama-server --version` under a 5 s timeout and must not run on the Update
goroutine) and stay available mid-stream; `fix`/`reinstall` replace the install
on disk, so they queue behind a running turn like other persistent-state
mutations.

## Restart caveat

On Unix, an already-running embed server process keeps the old (deleted) binary
open, so `/localserver fix` alone does not replace the running image. If a server
was running when you reinstalled, restart it — the fix command's own success
message says so, and `/localserver status` will confirm.
