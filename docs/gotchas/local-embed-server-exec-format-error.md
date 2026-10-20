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

`ReinstallLocalServer` (`internal/discovery/localserver_repair.go`) removes the
`llama-*` extraction directories and the SHA markers from the cache dir, then
re-downloads and re-extracts from the pinned archive. `EnsureArtifact`
(`internal/discovery/download.go`) verifies SHA256 and writes atomically, so a
partial/corrupt download is also repaired this way.

## Restart caveat

On Unix, an already-running embed server process keeps the old (deleted) binary
open, so `/localserver fix` alone does not replace the running image. If a server
was running when you reinstalled, restart it — the fix command's own success
message says so, and `/localserver status` will confirm.
